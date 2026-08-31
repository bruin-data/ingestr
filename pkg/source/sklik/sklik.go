// Package sklik implements a source for the Sklik JSON API.
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

	maxReportPageSize = 50
	maxStatRowsPerReq = 1500
	httpTimeout       = 120 * time.Second

	reportReadAttempts = 8
	reportReadBackoff  = 15 * time.Second
	accountColumn      = "_user_id"
)

type tableConfig struct {
	kind             string
	method           string
	bareList         bool // conversions.list accepts no filter or display options
	resultKey        string
	scopeToCampaigns bool // queries.createReport returns no rows without a scope
	displayColumns   []string
	primaryKeys      []string
	incrementalKey   string
}

// queries.readReport rejects ctr even though campaigns.readReport accepts it.
var searchQueryColumns = []string{
	"query",
	"keyword.id", "keyword.name", "keyword.matchType",
	"group.id", "group.name",
	"campaign.id", "campaign.name",
	"impressions", "clicks", "avgCpc", "conversions",
	"conversionValue", "transactions", "clickMoney", "impressionMoney", "totalMoney",
}

var campaignStatColumns = []string{
	"id", "name",
	"impressions", "clicks", "ctr", "avgCpc",
	"conversions", "conversionValue", "transactions",
	"clickMoney", "impressionMoney", "totalMoney",
	"pno",
	"ish", "exhaustedBudget", "stoppedBySchedule",
}

var supportedTables = map[string]tableConfig{
	"campaigns":   {kind: "list", method: "campaigns", resultKey: "campaigns", primaryKeys: []string{"id"}},
	"groups":      {kind: "list", method: "groups", resultKey: "groups", primaryKeys: []string{"id"}},
	"ads":         {kind: "list", method: "ads", resultKey: "ads", primaryKeys: []string{"id"}},
	"keywords":    {kind: "list", method: "keywords", resultKey: "keywords", primaryKeys: []string{"id"}},
	"conversions": {kind: "list", method: "conversions", resultKey: "conversions", primaryKeys: []string{"id"}, bareList: true},
	"campaign_stats_daily": {
		kind: "report", method: "campaigns", displayColumns: campaignStatColumns,
		primaryKeys: []string{"id", "date"}, incrementalKey: "date",
	},
	"search_queries": {
		kind: "report", method: "queries", displayColumns: searchQueryColumns,
		scopeToCampaigns: true,
		primaryKeys:      []string{"query", "keyword_id", "date"}, incrementalKey: "date",
	},
}

func supportedTableNames() string {
	names := make([]string, 0, len(supportedTables))
	for n := range supportedTables {
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

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

	accountID int64

	lastEmptyPayload string

	mu      sync.Mutex
	session string
}

func NewSklikSource() *Source {
	return &Source{client: &http.Client{Timeout: httpTimeout}}
}

func (s *Source) Schemes() []string { return []string{"sklik"} }

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
	if _, err = s.ensureSession(ctx, true); err != nil {
		return err
	}
	return s.resolveAccountID(ctx)
}

type clientGetResponse struct {
	User struct {
		UserID int64 `json:"userId"`
	} `json:"user"`
}

func (s *Source) resolveAccountID(ctx context.Context) error {
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
		return fmt.Errorf("sklik client.get returned no userId; refusing to emit unattributable rows")
	}
	s.accountID = r.User.UserID
	return nil
}

func (s *Source) Close(ctx context.Context) error { return nil }

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
	env.Report = raw
	return &env, nil
}

func (s *Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tc, ok := supportedTables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported sklik table %q, supported tables are: %s", req.Name, supportedTableNames())
	}
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

func (s *Source) readList(ctx context.Context, tc tableConfig, results chan<- source.RecordBatchResult, opts source.ReadOptions) error {
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

	createOpts := map[string]any{
		"statGranularity": "daily",
		// Sklik rejects this option for ranges that end before today.
		"includeCurrentDayStats": windowIncludesToday(opts.IntervalEnd),
	}

	env, err := s.call(ctx, tc.method+".createReport", restriction, createOpts)
	if err != nil {
		return err
	}
	if env.ReportID == "" {
		return fmt.Errorf("%s.createReport returned no reportId", tc.method)
	}

	expected := env.TotalCount

	limit := reportLimitFor(opts.IntervalStart, opts.IntervalEnd)
	emitted := 0
	for offset := 0; ; offset += limit {
		readOpts := map[string]any{
			"offset":         offset,
			"limit":          limit,
			"displayColumns": tc.displayColumns,
			// The default drops entities whose statistics block is empty.
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

	if emitted == 0 && expected > 0 {
		return fmt.Errorf(
			"%s.createReport reported totalCount=%d but readReport returned no rows after %d attempts over ~%s; "+
				"the report may still be materialising, have no rows for this window, or be throttled; last payload: %s",
			tc.method, expected, reportReadAttempts, totalReportPollBudget(), s.lastEmptyPayload)
	}
	return nil
}

func totalReportPollBudget() time.Duration {
	n := reportReadAttempts - 1
	if n < 1 {
		return 0
	}
	return reportReadBackoff * time.Duration(n*(n+1)/2)
}

// Sklik returns an empty successful response while a report is still materialising.
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

func truncate(raw json.RawMessage, n int) string {
	if len(raw) <= n {
		return string(raw)
	}
	return string(raw[:n]) + "…"
}

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
			// Sklik returns daily dates as YYYYMMDD numbers.
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

func (s *Source) emit(ctx context.Context, items []map[string]any, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}
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

func windowIncludesToday(end *time.Time) bool {
	if end == nil {
		return true
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return !end.UTC().Truncate(24 * time.Hour).Before(today)
}
