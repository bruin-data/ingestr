// Package sklik implements an ingestr source for the Seznam Sklik "Drak"
// JSON-RPC API (v5). Sklik is the dominant paid-search platform in Czechia and
// has no upstream ingestr connector.
//
// Docs: https://api.sklik.cz/drak/
//
// AUTH MODEL — this is the part that trips people up:
//   - Each account has one permanent API token, generated in the Sklik UI.
//   - client.loginByToken(<token>) returns a ROLLING session string.
//   - Every subsequent call's first positional parameter is {session, userId?}.
//   - Every response carries a REFRESHED session which MUST replace the old one.
//     A connector that logs in once and reuses the first session will start
//     failing with status 401 partway through a long run.
//   - status 401 mid-run means "session expired": re-login once and retry.
//   - Managed accounts: pass the foreign account's userId. Omit it (0) to target
//     the token owner's own account.
//
// Transport is JSON-over-HTTP: the method name is the URL path and the params
// are a positional JSON array in the body, mirroring the XML-RPC semantics 1:1.
//
// REPORT TABLES are a two-step flow, not a single GET:
//
//	<entity>.createReport(restrictionFilter, displayOptions) -> {reportId, totalCount}
//	<entity>.readReport(reportId, {offset, limit, displayColumns, ...}) -> rows
//
// The report is materialised server-side, then paged. Stats come back nested as
// a per-period array on each row, so we flatten them into one row per
// (entity, date) — otherwise the destination gets an unusable array column.
package sklik

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	apiVersion = "v5"
	baseURL    = "https://api.sklik.cz/drak/json/" + apiVersion
	pageSize   = 500
	// Report pages are far smaller than list pages: readReport returns
	// limit x (days in range) stat rows, so the safe page size DEPENDS ON THE
	// WINDOW. 50 entities is fine over a month (~1.5k rows) and a 406 "You are
	// requiring too much data in one request" over three years (~55k). See
	// reportLimitFor.
	maxReportPageSize = 50
	maxStatRowsPerReq = 1500
	httpTimeout       = 120 * time.Second
	// Reports are materialised asynchronously and read as empty until ready. An
	// unfinished report and a genuinely empty one are the same payload,
	// {"status":200,"report":[]}, so the only safe signal is createReport's
	// totalCount: retry while it promises rows the read has not produced.
	//
	// The budget is nearly free. The retry loop only engages when a page comes
	// back empty while createReport promised rows (see readReportPage's
	// retryWhenEmpty), so a healthy report returns on the first read with no
	// sleep at all. Only a failing window pays the full wait.
	//
	// Linear backoff: 15s, 30s, 45s, 60s, 75s, 90s, 105s — ~7min total.
	reportReadAttempts = 8
	reportReadBackoff  = 15 * time.Second

	// accountColumn is the Sklik account stamped onto every row. Underscore-prefixed
	// because WE add it — Sklik never returns it in a row payload — matching
	// _ingestr_loaded_at and raw_hubspot._portal_id, the same fix for the same bug.
	accountColumn = "_user_id"
)

// tableConfig describes one exposed table.
//
// kind "list"   -> a *.list method returning entities (a snapshot, no dates)
// kind "report" -> the createReport/readReport pair (a per-day series)
type tableConfig struct {
	kind   string
	method string // "campaigns", "groups", ... — the RPC namespace
	// bareList: the .list method takes ONLY the user struct. conversions.list is
	// the odd one out — passing the usual (restrictionFilter, displayOptions)
	// pair gets a bare "Bad arguments".
	bareList  bool
	resultKey string // key holding the array in a *.list response
	// scopeToCampaigns: createReport MUST carry a campaign restriction or the
	// report comes back EMPTY with status 200. queries.createReport does; see
	// the ⚠️ block on searchQueryColumns.
	scopeToCampaigns bool
	displayColumns   []string // report tables only
	primaryKeys      []string
	incrementalKey   string // report tables only; "" for snapshots
}

// `ctr` is deliberately absent here. queries.readReport rejects it with a bare
// 400 even though campaigns.readReport accepts it — the displayColumns enums are
// per-method and asymmetric. Callers can derive CTR from clicks and impressions.
//
// queries.createReport also returns an EMPTY report unless the restriction filter
// scopes it to campaigns, groups or keywords. It is not an error but
// `{"status":200,"statusMessage":"OK","report":[]}`, which reads as "this account
// has no search terms": measured on a live account, no restriction returned 0 rows
// while restricting to one campaign over the same window returned 165. Hence
// scopeToCampaigns. campaigns.createReport does not share this behaviour, so a
// working campaign report proves nothing about this one.
var searchQueryColumns = []string{
	"query",
	// Entity columns make the raw rows joinable against the snapshot tables and
	// are what makes the primary key unique — the same query is reported once
	// per keyword that matched it.
	"keyword.id", "keyword.name", "keyword.matchType",
	"group.id", "group.name",
	"campaign.id", "campaign.name",
	"impressions", "clicks", "avgCpc", "conversions",
	"conversionValue", "transactions", "clickMoney", "impressionMoney", "totalMoney",
}

// ⚠️ Verified against https://api.sklik.cz/drak/campaigns.readReport.html.
// DO NOT add columns without checking that enum: Sklik rejects the ENTIRE report
// with an opaque "Bad arguments" if any single column is invalid. The impression
// -share columns use the readReport vocabulary (`ish`), NOT the ish-prefixed
// createReport restriction forms — mixing them up is exactly the 2026-04-26 bug.
var campaignStatColumns = []string{
	"id", "name",
	"impressions", "clicks", "ctr", "avgCpc",
	"conversions", "conversionValue", "transactions",
	"clickMoney", "impressionMoney", "totalMoney",
	"pno",
	"ish", "exhaustedBudget", "stoppedBySchedule",
}

// `_user_id` is stamped on every row but is deliberately not part of any primary
// key. One token reaches one account, so a single load cannot collide; keys stay on
// Sklik's own ids. If several accounts are loaded into one destination table and two
// ever share an entity id, the merge would keep only one of the rows — load each
// account into its own table to avoid the question.
var supportedTables = map[string]tableConfig{
	// Entity snapshots — no date dimension. Sklik returns the current state.
	"campaigns":   {kind: "list", method: "campaigns", resultKey: "campaigns", primaryKeys: []string{"id"}},
	"groups":      {kind: "list", method: "groups", resultKey: "groups", primaryKeys: []string{"id"}},
	"ads":         {kind: "list", method: "ads", resultKey: "ads", primaryKeys: []string{"id"}},
	"keywords":    {kind: "list", method: "keywords", resultKey: "keywords", primaryKeys: []string{"id"}},
	"conversions": {kind: "list", method: "conversions", resultKey: "conversions", primaryKeys: []string{"id"}, bareList: true},

	// Report tables — one row per (entity, day).
	"campaign_stats_daily": {
		kind: "report", method: "campaigns", displayColumns: campaignStatColumns,
		primaryKeys: []string{"id", "date"}, incrementalKey: "date",
	},
	"search_queries": {
		kind: "report", method: "queries", displayColumns: searchQueryColumns,
		scopeToCampaigns: true,
		// keyword_id is part of the key because Sklik reports a query once per
		// matching keyword; on (query, date) alone the merge would keep one
		// arbitrary keyword's row and silently drop the rest of the spend.
		primaryKeys: []string{"query", "keyword_id", "date"}, incrementalKey: "date",
	},
}

func supportedTableNames() string {
	names := make([]string, 0, len(supportedTables))
	for n := range supportedTables {
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

// envelope is the common response wrapper. `session` is present on every
// response and supersedes the one we sent.
type envelope struct {
	Status        int             `json:"status"`
	StatusMessage string          `json:"statusMessage"`
	Session       string          `json:"session"`
	ReportID      string          `json:"reportId"`
	TotalCount    int             `json:"totalCount"`
	Report        json.RawMessage `json:"report"`
}

type userArg struct {
	Session string `json:"session"`
	UserID  int64  `json:"userId,omitempty"`
}

type Source struct {
	token  string
	userID int64
	client *http.Client

	// accountID is the Sklik account every row of this run belongs to, stamped
	// onto each emitted row as `user_id`. Resolved once in Connect.
	accountID int64

	lastEmptyPayload string

	mu      sync.Mutex
	session string
}

func NewSklikSource() *Source {
	return &Source{client: &http.Client{Timeout: httpTimeout}}
}

func (s *Source) Schemes() []string { return []string{"sklik"} }

// Sklik reports are filtered server-side by dateFrom/dateTo, and the entity
// lists are snapshots with no date at all — so the source owns incrementality.
func (s *Source) HandlesIncrementality() bool { return true }

func (s *Source) Connect(ctx context.Context, uri string) error {
	parsed, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid sklik URI: %w", err)
	}
	q := parsed.Query()

	s.token = q.Get("token")
	if s.token == "" {
		return fmt.Errorf("token is required in sklik URI: sklik://?token=<api_token>")
	}
	if raw := q.Get("user_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("user_id must be numeric, got %q", raw)
		}
		s.userID = id
	}
	// Fail fast on bad credentials rather than at first read.
	if _, err = s.ensureSession(ctx, true); err != nil {
		return err
	}
	return s.resolveAccountID(ctx)
}

// clientGetResponse is the client.get shape. Only the token owner's own userId
// is read here; `foreignAccounts` lists managed accounts and is deliberately
// ignored — one ingest run targets one account.
type clientGetResponse struct {
	User struct {
		UserID int64 `json:"userId"`
	} `json:"user"`
}

// resolveAccountID determines which Sklik account this run's rows belong to.
//
// It cannot simply echo s.userID: a URI that carries only a token leaves that
// field 0, and stamping 0 would leave every row unattributed. When user_id is
// omitted the account is the token owner's, and only client.get knows its id.
func (s *Source) resolveAccountID(ctx context.Context) error {
	// An explicitly targeted managed account is already the answer.
	if s.userID != 0 {
		s.accountID = s.userID
		return nil
	}
	env, err := s.call(ctx, "client.get")
	if err != nil {
		return fmt.Errorf("resolve sklik account: %w", err)
	}
	var r clientGetResponse
	if err := json.Unmarshal(env.Report, &r); err != nil {
		return fmt.Errorf("decode client.get: %w", err)
	}
	if r.User.UserID == 0 {
		// Fail rather than emit rows that cannot be attributed to a brand —
		// silently unattributable rows are the defect this field exists to fix.
		return fmt.Errorf("sklik client.get returned no userId; refusing to emit unattributable rows")
	}
	s.accountID = r.User.UserID
	return nil
}

func (s *Source) Close(ctx context.Context) error { return nil }

// post sends a positional-array JSON-RPC request.
func (s *Source) post(ctx context.Context, method string, params []any) ([]byte, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sklik %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("read %s response: %w", method, err)
	}
	return buf.Bytes(), nil
}

// ensureSession returns a live session, logging in when needed. force=true
// discards any cached session first (used after a 401).
func (s *Source) ensureSession(ctx context.Context, force bool) (string, error) {
	s.mu.Lock()
	if !force && s.session != "" {
		sess := s.session
		s.mu.Unlock()
		return sess, nil
	}
	s.mu.Unlock()

	raw, err := s.post(ctx, "client.loginByToken", []any{s.token})
	if err != nil {
		return "", err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("decode loginByToken: %w", err)
	}
	if env.Status < 200 || env.Status >= 300 {
		// Deliberately does not echo statusMessage — it is a credential error.
		return "", fmt.Errorf("sklik login failed with status %d (check the API token)", env.Status)
	}
	if env.Session == "" {
		return "", fmt.Errorf("sklik loginByToken returned an empty session")
	}
	s.mu.Lock()
	s.session = env.Session
	s.mu.Unlock()
	return env.Session, nil
}

// call issues an authenticated method, rotating the session from the response
// and retrying once on 401 (expired session).
func (s *Source) call(ctx context.Context, method string, extra ...any) (*envelope, error) {
	return s.callOnce(ctx, method, extra, true)
}

func (s *Source) callOnce(ctx context.Context, method string, extra []any, canRetry bool) (*envelope, error) {
	sess, err := s.ensureSession(ctx, false)
	if err != nil {
		return nil, err
	}
	params := append([]any{userArg{Session: sess, UserID: s.userID}}, extra...)
	raw, err := s.post(ctx, method, params)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode %s: %w", method, err)
	}
	// The refreshed session supersedes ours on EVERY response.
	if env.Session != "" {
		s.mu.Lock()
		s.session = env.Session
		s.mu.Unlock()
	}
	if env.Status == 401 && canRetry {
		if _, err := s.ensureSession(ctx, true); err != nil {
			return nil, err
		}
		return s.callOnce(ctx, method, extra, false)
	}
	if env.Status < 200 || env.Status >= 300 {
		return nil, fmt.Errorf("sklik %s status %d: %s", method, env.Status, env.StatusMessage)
	}
	// Keep the raw payload for list responses, which put the array under a
	// method-specific key rather than in `report`.
	env.Report = raw
	return &env, nil
}

func (s *Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tc, ok := supportedTables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported sklik table %q, supported tables are: %s", req.Name, supportedTableNames())
	}
	// merge for both kinds: report rows dedup on (entity, date), and entity
	// snapshots dedup on id so a re-run refreshes rather than duplicates.
	strategy := config.StrategyMerge
	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    tc.primaryKeys,
		TableIncrementalKey: tc.incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("sklik source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, tc, opts)
		},
	}, nil
}

func (s *Source) read(ctx context.Context, table string, tc tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 4)
	go func() {
		defer close(results)
		var err error
		if tc.kind == "report" {
			err = s.readReport(ctx, tc, opts, results)
		} else {
			err = s.readList(ctx, tc, results, opts)
		}
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

// readList pulls an entity snapshot via <method>.list.
func (s *Source) readList(ctx context.Context, tc tableConfig, results chan<- source.RecordBatchResult, opts source.ReadOptions) error {
	// displayOptions MUST carry offset/limit. An empty map is rejected with a
	// bare "Bad arguments" — the same opaque 400 Sklik uses for every argument
	// problem, which is why this is spelled out rather than left to defaults.
	for offset := 0; ; offset += pageSize {
		var env *envelope
		var err error
		if tc.bareList {
			env, err = s.call(ctx, tc.method+".list")
		} else {
			env, err = s.call(ctx, tc.method+".list",
				map[string]any{},
				map[string]any{"offset": offset, "limit": pageSize})
		}
		if err != nil {
			return err
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(env.Report, &payload); err != nil {
			return fmt.Errorf("decode %s.list: %w", tc.method, err)
		}
		rawItems, ok := payload[tc.resultKey]
		if !ok {
			// An account with nothing configured returns no key at all.
			return nil
		}
		var items []map[string]any
		if err := json.Unmarshal(rawItems, &items); err != nil {
			return fmt.Errorf("decode %s.list items: %w", tc.method, err)
		}
		if len(items) == 0 {
			return nil
		}
		if err := s.emit(ctx, items, opts, results); err != nil {
			return err
		}
		if tc.bareList || len(items) < pageSize {
			return nil
		}
	}
}

// readReport runs createReport then pages readReport, flattening the nested
// per-period stats into one row per (entity, date).
func (s *Source) readReport(ctx context.Context, tc tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	from, to := "", ""
	if opts.IntervalStart != nil {
		from = opts.IntervalStart.Format("2006-01-02")
	}
	if opts.IntervalEnd != nil {
		to = opts.IntervalEnd.Format("2006-01-02")
	}
	restriction := map[string]any{}
	if from != "" {
		restriction["dateFrom"] = from
	}
	if to != "" {
		restriction["dateTo"] = to
	}
	if tc.scopeToCampaigns {
		ids, err := s.campaignIDs(ctx)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		restriction["campaign"] = map[string]any{"ids": ids}
	}

	// statGranularity=daily is what turns this into a time series. Without it
	// Sklik returns ONE aggregate row for the whole range — the same trap Apple
	// Search Ads has, and just as silent.
	// includeCurrentDayStats MUST follow the data: Sklik rejects the report with
	// "Current day's stats were requested, but today's date is not included in
	// the date range" whenever it is true for a window that ends in the past.
	// That makes it a backfill-only landmine — every historical chunk 400s while
	// the nightly window works fine.
	createOpts := map[string]any{
		"statGranularity":        "daily",
		"includeCurrentDayStats": windowIncludesToday(opts.IntervalEnd),
	}

	env, err := s.call(ctx, tc.method+".createReport", restriction, createOpts)
	if err != nil {
		return err
	}
	if env.ReportID == "" {
		return fmt.Errorf("%s.createReport returned no reportId", tc.method)
	}

	// createReport's totalCount is the ONLY way to tell "this window genuinely
	// has no rows" from "the report is not materialised / we were throttled".
	// Both look identical at readReport: {"status":200,"report":[]}. Without
	// this check a throttled run is a GREEN run that ingests nothing, which is
	// exactly how search_queries stayed empty across every brand and year.
	expected := env.TotalCount

	limit := reportLimitFor(opts.IntervalStart, opts.IntervalEnd)
	emitted := 0
	for offset := 0; ; offset += limit {
		// statGranularity belongs to createReport ONLY. Repeating it here is
		// another "Bad arguments" — the two calls take different vocabularies.
		readOpts := map[string]any{
			"offset":         offset,
			"limit":          limit,
			"displayColumns": tc.displayColumns,
			// allowEmptyStatistics defaults to FALSE, which makes readReport drop
			// every row whose statistics block is empty — returning `report: []`
			// while createReport still advertises totalCount>0. That is the exact
			// shape that made search_queries look like "this account has no search
			// terms" for months. keywords.negative.readReport needs the same flag.
			"allowEmptyStatistics": true,
		}
		rows, err := s.readReportPage(ctx, tc, env.ReportID, readOpts, offset == 0 && expected > 0)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		if err := s.emit(ctx, rows, opts, results); err != nil {
			return err
		}
		emitted += len(rows)
		if len(rows) < limit {
			break
		}
	}

	// A report that promised rows and delivered none is a failure, not an empty
	// window. Surfacing it as an error is the whole point: a silent 0 here is
	// indistinguishable from success in the job log.
	if emitted == 0 && expected > 0 {
		// ⚠️ DO NOT re-word this as "throttled". The previous text asserted
		// throttling as the cause and cost a full investigation on 2026-08-19
		// chasing a rate limit that was not there. Throttling is ONE cause of an
		// empty-with-totalCount>0 report; an unfinished async report is another,
		// and so is a window that genuinely has no rows.
		//
		// ⚠️ totalCount IS NOT A ROW COUNT FOR THIS WINDOW. Measured 2026-08-19:
		// queries.createReport returned the SAME totalCount=5716 for a 2-day and a
		// 30-day window, i.e. it is ~the account's keyword count and is window-
		// INDEPENDENT. So `expected > 0` does not prove this window has rows, and
		// this error can fire on a legitimately empty window — an account whose
		// campaigns ran no search traffic at all cannot produce search terms.
		//
		// The window-scoped oracle is search impressions for the same account and
		// window: empty report + impressions > 0 is a real fault, empty + zero
		// impressions is not. That comparison needs the destination, which this
		// source cannot reach, so treat this error as "look at it" rather than as
		// proof of a fault.
		return fmt.Errorf(
			"%s.createReport reported totalCount=%d but readReport returned no rows after %d attempts over ~%s; "+
				"causes, in order of likelihood: report not materialised in time, this window genuinely has no rows "+
				"(totalCount is window-INDEPENDENT, ~the keyword count — it does NOT prove otherwise), or throttling "+
				"(Sklik answers a throttled report with an empty 200, not a 429). Cross-check search impressions for "+
				"this account+window before treating it as a fault. last payload: %s",
			tc.method, expected, reportReadAttempts, totalReportPollBudget(), s.lastEmptyPayload)
	}
	return nil
}

// totalReportPollBudget is the wall-clock the empty-report retry loop can spend,
// derived from the same constants the loop uses so the error message can never
// quote a stale number. Linear backoff means the sleeps are
// 1*backoff .. (attempts-1)*backoff, i.e. backoff * n(n+1)/2 for n = attempts-1.
func totalReportPollBudget() time.Duration {
	n := reportReadAttempts - 1
	if n < 1 {
		return 0
	}
	return reportReadBackoff * time.Duration(n*(n+1)/2)
}

// readReportPage pages readReport once, retrying while the report is still
// empty but createReport said it has rows. Sklik materialises reports
// asynchronously and answers reads against an unfinished report with an empty
// (not erroring) payload, so a single read is a race.
func (s *Source) readReportPage(ctx context.Context, tc tableConfig, reportID string, readOpts map[string]any, retryWhenEmpty bool) ([]map[string]any, error) {
	attempts := 1
	if retryWhenEmpty {
		attempts = reportReadAttempts
	}
	var rows []map[string]any
	var lastRaw json.RawMessage
	defer func() {
		if len(rows) == 0 && retryWhenEmpty {
			s.lastEmptyPayload = truncate(lastRaw, 500)
		}
	}()
	for attempt := range attempts {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * reportReadBackoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		page, err := s.call(ctx, tc.method+".readReport", reportID, readOpts)
		if err != nil {
			return nil, err
		}
		rows, err = flattenReport(page.Report)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return rows, nil
		}
		lastRaw = page.Report
	}
	return rows, nil
}

// truncate keeps an error message readable when it carries a raw API payload.
func truncate(raw json.RawMessage, n int) string {
	if len(raw) <= n {
		return string(raw)
	}
	return string(raw[:n]) + "…"
}

// campaignIDs pages campaigns.list for the ids a scoped report needs. The
// account's campaign count is small (tens), so this is one extra call, and it
// includes paused/ended campaigns deliberately — a backfill covers windows in
// which they were still running.
func (s *Source) campaignIDs(ctx context.Context) ([]int64, error) {
	var ids []int64
	for offset := 0; ; offset += pageSize {
		env, err := s.call(ctx, "campaigns.list",
			map[string]any{},
			map[string]any{"offset": offset, "limit": pageSize})
		if err != nil {
			return nil, err
		}
		var payload struct {
			Campaigns []struct {
				ID int64 `json:"id"`
			} `json:"campaigns"`
		}
		if err := json.Unmarshal(env.Report, &payload); err != nil {
			return nil, fmt.Errorf("decode campaigns.list: %w", err)
		}
		for _, c := range payload.Campaigns {
			ids = append(ids, c.ID)
		}
		if len(payload.Campaigns) < pageSize {
			return ids, nil
		}
	}
}

// flattenEntityRefs expands the nested entity objects the queries report
// returns (`campaign`:{id,name}, `keyword`:{...}, `group`:{...}) into scalar
// `campaign_id` / `campaign_name` / ... columns. Left nested, schema inference
// lands them as JSON blobs, which are neither joinable nor usable as a primary
// key. Sub-key spelling is kept verbatim (`keyword_matchType`) because raw
// tables carry the vendor's vocabulary.
func flattenEntityRefs(row map[string]any) {
	for _, entity := range [...]string{"campaign", "group", "keyword"} {
		nested, ok := row[entity].(map[string]any)
		if !ok {
			continue
		}
		delete(row, entity)
		for k, v := range nested {
			row[entity+"_"+k] = v
		}
	}
}

// flattenReport turns Sklik's nested report shape into flat rows.
//
// A readReport response looks like:
//
//	{"report":[{"id":123,"name":"X","stats":[{"date":"2026-01-01","clicks":5}, ...]}]}
//
// so each entity carries an array of per-day stats. We emit one row per stat,
// merging the entity-level fields in. Rows without stats are skipped rather
// than emitted with a null date, which would collide on the primary key.
func flattenReport(raw json.RawMessage) ([]map[string]any, error) {
	var payload struct {
		Report []map[string]any `json:"report"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode readReport: %w", err)
	}
	out := make([]map[string]any, 0, len(payload.Report))
	for _, item := range payload.Report {
		statsRaw, ok := item["stats"]
		if !ok {
			continue
		}
		stats, ok := statsRaw.([]any)
		if !ok {
			continue
		}
		base := make(map[string]any, len(item))
		for k, v := range item {
			if k != "stats" {
				base[k] = v
			}
		}
		for _, st := range stats {
			stat, ok := st.(map[string]any)
			if !ok {
				continue
			}
			// Sklik returns the granularity date as a YYYYMMDD *integer*
			// (20260804), which schema inference would otherwise land as a bare
			// Int64 — unpartitionable, and not joinable against the real Date
			// columns every other source produces. Normalise to time.Time so the
			// Arrow layer maps it to a timestamp, per the repo's convention.
			if d, ok := normaliseSklikDate(stat["date"]); ok {
				stat["date"] = d
			}
			row := make(map[string]any, len(base)+len(stat))
			for k, v := range base {
				row[k] = v
			}
			for k, v := range stat {
				row[k] = v
			}
			flattenEntityRefs(row)
			out = append(out, row)
		}
	}
	return out, nil
}

// emit converts rows to Arrow, stamping the Sklik account onto every one.
//
// Sklik's payloads carry no account field of any kind, so rows from two accounts
// are indistinguishable once they share a destination table. Both the list and the
// report paths funnel through here, so this is the one place that needs the stamp.
// The column is named for Sklik's own vocabulary for an account
// (client.get -> user.userId).
func (s *Source) emit(ctx context.Context, items []map[string]any, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}
	// Stamped as a string rather than the int Sklik returns: account identifiers
	// from other ad platforms are strings, and a string/int join is a type error at
	// best and a silently empty result at worst.
	account := strconv.FormatInt(s.accountID, 10)
	for _, row := range items {
		row[accountColumn] = account
	}
	rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("convert sklik rows to arrow: %w", err)
	}
	select {
	case results <- source.RecordBatchResult{Batch: rec}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// normaliseSklikDate converts Sklik's YYYYMMDD date representation to a
// time.Time. Sklik has returned it as a JSON number in practice, but accept the
// string form too rather than silently passing an unusable value through.
func normaliseSklikDate(v any) (time.Time, bool) {
	var raw string
	switch t := v.(type) {
	case float64:
		raw = strconv.FormatInt(int64(t), 10)
	case int64:
		raw = strconv.FormatInt(t, 10)
	case string:
		raw = strings.ReplaceAll(t, "-", "")
	default:
		return time.Time{}, false
	}
	parsed, err := time.Parse("20060102", raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// reportLimitFor sizes a readReport page so that limit x days stays under what
// Sklik will answer. readReport returns one stat row per entity PER DAY, so a
// page size that is fine for a nightly 30-day pull produces a 406 over a
// multi-year backfill. Callers therefore never have to think about it.
func reportLimitFor(start, end *time.Time) int {
	days := 31
	if start != nil && end != nil {
		if d := int(end.Sub(*start).Hours()/24) + 1; d > 0 {
			days = d
		}
	}
	limit := maxStatRowsPerReq / days
	if limit > maxReportPageSize {
		limit = maxReportPageSize
	}
	if limit < 1 {
		limit = 1
	}
	return limit
}

// windowIncludesToday reports whether the requested range reaches today, which is
// the only case where Sklik accepts includeCurrentDayStats.
func windowIncludesToday(end *time.Time) bool {
	if end == nil {
		return true
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return !end.UTC().Truncate(24 * time.Hour).Before(today)
}
