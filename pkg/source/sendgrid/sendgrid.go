package sendgrid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://api.sendgrid.com/v3"

	maxMessagesPageSize    = 1000
	maxStatsPageSize       = 500
	maxSuppressionPageSize = 500  // shared by the /suppression/* endpoints (bounces, blocks, ...)
	maxListsPageSize       = 1000 // GET /marketing/lists allows up to 1000
	maxSingleSendsPageSize = 100  // GET /marketing/singlesends caps page_size at 100
	maxTemplatesPageSize   = 200  // GET /templates caps page_size at 200
	maxPages               = 10000

	defaultActivityStart   = "1970-01-01T00:00:00Z"
	defaultStatsAggregated = "day"

	// messagesMinWindow is the smallest Email Activity window the time bisection splits to;
	// last_event_time has second granularity, so a 1s window cannot be divided further by time.
	// A still-full 1s window is then partitioned by msg_id prefix instead.
	messagesMinWindow = time.Second
	// messagesIDAlphabet is the case-folded character set msg_ids draw from (base64url + dot);
	// LIKE is case-insensitive, so one case covers both. Used to partition a saturated second.
	messagesIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz-_."
	// maxMessagesPrefixLen caps msg_id prefix recursion depth as a safety stop.
	maxMessagesPrefixLen = 16

	// SendGrid Email Activity API is documented at 6 req/min: (6 * 0.8) / 60 = 0.08 req/sec.
	emailActivityRateLimit = 0.08
	// SendGrid Web API v3 documents endpoint-specific rate-limit headers but no global numeric limit.
	// Keep a conservative local cap for non-Email-Activity endpoints and rely on retries for 429s.
	generalRateLimit = 5.0
	rateLimitBurst   = 5
)

type paginationStyle int

const (
	paginateBisect       paginationStyle = iota // recursive date-range bisection for Email Activity (messages)
	paginateOffset                              // limit + offset, top-level array response (global_stats, bounces)
	paginateToken                               // page_size + page_token via _metadata.next (lists, single_sends)
	paginateGroupMembers                        // fan out over /asm/groups, emit {group_id, email} per member
)

type filterMode int

const (
	filterNone                 filterMode = iota
	filterActivityQuery                   // messages: Email Activity query on last_event_time
	filterStatsDateRange                  // global_stats: start_date/end_date query params (requires --interval-start)
	filterSuppressionUnixRange            // suppression tables: start_time/end_time Unix query params
	filterClientSide                      // single_sends: client-side filter on incrementalKey
)

type rateLimitTier int

const (
	tierGeneral rateLimitTier = iota
	tierEmailActivity
)

type tableConfig struct {
	endpoint       string
	dataKey        string // JSON key holding the array; "" means the response itself is the array
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
	pageSize       int
	pagination     paginationStyle
	filter         filterMode
	tier           rateLimitTier
	extraParams    map[string]string // static query params always sent with the request
}

var supportedTables = map[string]tableConfig{
	"messages": {
		endpoint:       "/messages",
		dataKey:        "messages",
		primaryKeys:    []string{"msg_id"},
		incrementalKey: "last_event_time",
		strategy:       config.StrategyMerge,
		pageSize:       maxMessagesPageSize,
		pagination:     paginateBisect,
		filter:         filterActivityQuery,
		tier:           tierEmailActivity,
	},
	"global_stats": {
		endpoint:       "/stats",
		dataKey:        "",
		primaryKeys:    []string{"date"},
		incrementalKey: "date",
		strategy:       config.StrategyMerge,
		pageSize:       maxStatsPageSize,
		pagination:     paginateOffset,
		filter:         filterStatsDateRange,
		tier:           tierGeneral,
	},
	"bounces": {
		endpoint:       "/suppression/bounces",
		dataKey:        "",
		primaryKeys:    []string{"email", "created"},
		incrementalKey: "created",
		strategy:       config.StrategyMerge,
		pageSize:       maxSuppressionPageSize,
		pagination:     paginateOffset,
		filter:         filterSuppressionUnixRange,
		tier:           tierGeneral,
	},
	"blocks": {
		endpoint:       "/suppression/blocks",
		dataKey:        "",
		primaryKeys:    []string{"email", "created"},
		incrementalKey: "created",
		strategy:       config.StrategyMerge,
		pageSize:       maxSuppressionPageSize,
		pagination:     paginateOffset,
		filter:         filterSuppressionUnixRange,
		tier:           tierGeneral,
	},
	"invalid_emails": {
		endpoint:       "/suppression/invalid_emails",
		dataKey:        "",
		primaryKeys:    []string{"email", "created"},
		incrementalKey: "created",
		strategy:       config.StrategyMerge,
		pageSize:       maxSuppressionPageSize,
		pagination:     paginateOffset,
		filter:         filterSuppressionUnixRange,
		tier:           tierGeneral,
	},
	"spam_reports": {
		endpoint:       "/suppression/spam_reports",
		dataKey:        "",
		primaryKeys:    []string{"email", "created"},
		incrementalKey: "created",
		strategy:       config.StrategyMerge,
		pageSize:       maxSuppressionPageSize,
		pagination:     paginateOffset,
		filter:         filterSuppressionUnixRange,
		tier:           tierGeneral,
	},
	"unsubscribes": {
		endpoint:       "/suppression/unsubscribes",
		dataKey:        "",
		primaryKeys:    []string{"email", "created"},
		incrementalKey: "created",
		strategy:       config.StrategyMerge,
		pageSize:       maxSuppressionPageSize,
		pagination:     paginateOffset,
		filter:         filterSuppressionUnixRange,
		tier:           tierGeneral,
	},
	"suppression_groups": {
		endpoint:    "/asm/groups",
		dataKey:     "",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		pageSize:    maxSuppressionPageSize,
		pagination:  paginateOffset,
		filter:      filterNone,
		tier:        tierGeneral,
	},
	"suppression_group_members": {
		endpoint:    "/asm/groups",
		primaryKeys: []string{"group_id", "email"},
		strategy:    config.StrategyReplace,
		pagination:  paginateGroupMembers,
		filter:      filterNone,
		tier:        tierGeneral,
	},
	"templates": {
		endpoint:       "/templates",
		dataKey:        "result",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		pageSize:       maxTemplatesPageSize,
		pagination:     paginateToken,
		filter:         filterClientSide,
		tier:           tierGeneral,
		extraParams:    map[string]string{"generations": "legacy,dynamic"},
	},
	"lists": {
		endpoint:    "/marketing/lists",
		dataKey:     "result",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		pageSize:    maxListsPageSize,
		pagination:  paginateToken,
		filter:      filterNone,
		tier:        tierGeneral,
	},
	"single_sends": {
		endpoint:       "/marketing/singlesends",
		dataKey:        "result",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		pageSize:       maxSingleSendsPageSize,
		pagination:     paginateToken,
		filter:         filterClientSide,
		tier:           tierGeneral,
	},
}

var validAggregations = map[string]bool{
	"day":   true,
	"week":  true,
	"month": true,
}

type credentials struct {
	apiKey     string
	onBehalfOf string
}

type SendGridSource struct {
	apiKey         string
	onBehalfOf     string
	client         *ingestrhttp.Client
	activityClient *ingestrhttp.Client
}

func NewSendGridSource() *SendGridSource {
	return &SendGridSource{}
}

func (s *SendGridSource) HandlesIncrementality() bool {
	return true
}

func (s *SendGridSource) Schemes() []string {
	return []string{"sendgrid"}
}

func (s *SendGridSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = creds.apiKey
	s.onBehalfOf = creds.onBehalfOf

	commonOpts := []ingestrhttp.Option{
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60 * time.Second),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth(s.apiKey)),
	}
	if s.onBehalfOf != "" {
		commonOpts = append(commonOpts, ingestrhttp.WithHeader("on-behalf-of", s.onBehalfOf))
	}

	generalOpts := append(commonOpts[:len(commonOpts):len(commonOpts)], ingestrhttp.WithRateLimiter(generalRateLimit, rateLimitBurst))
	activityOpts := append(commonOpts[:len(commonOpts):len(commonOpts)], ingestrhttp.WithRateLimiter(emailActivityRateLimit, rateLimitBurst))

	s.client = ingestrhttp.New(generalOpts...)
	s.activityClient = ingestrhttp.New(activityOpts...)

	config.Debug("[SENDGRID] Connected successfully")
	return nil
}

func (s *SendGridSource) Close(ctx context.Context) error {
	if s.client != nil {
		if err := s.client.Close(); err != nil {
			return err
		}
	}
	if s.activityClient != nil {
		return s.activityClient.Close()
	}
	return nil
}

func parseURI(uri string) (credentials, error) {
	if !strings.HasPrefix(uri, "sendgrid://") {
		return credentials{}, fmt.Errorf("invalid sendgrid URI: must start with sendgrid://")
	}

	rest := strings.TrimPrefix(uri, "sendgrid://")
	if rest == "" || rest == "?" {
		return credentials{}, fmt.Errorf("api_key is required in sendgrid URI")
	}

	rest = strings.TrimPrefix(rest, "?")
	values, err := url.ParseQuery(rest)
	if err != nil {
		return credentials{}, fmt.Errorf("failed to parse sendgrid URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return credentials{}, fmt.Errorf("api_key is required in sendgrid URI")
	}

	return credentials{
		apiKey:     apiKey,
		onBehalfOf: values.Get("on_behalf_of"),
	}, nil
}

// parseTableName splits an optional granularity suffix from the table name, e.g.
// "global_stats:week". Only global_stats accepts a suffix; it controls the SendGrid
// `aggregated_by` parameter and defaults to "day". The returned aggregatedBy is empty
// for tables that do not use it.
func parseTableName(name string) (table string, aggregatedBy string, err error) {
	table = name
	suffix := ""
	if strings.Contains(name, ":") {
		parts := strings.SplitN(name, ":", 2)
		table, suffix = parts[0], parts[1]
	}

	if !isValidTable(table) {
		return "", "", fmt.Errorf("unsupported table: %s (supported: %s)", name, supportedTableList())
	}

	if table != "global_stats" {
		if suffix != "" {
			return "", "", fmt.Errorf("table %q does not support a granularity suffix", table)
		}
		return table, "", nil
	}

	if suffix == "" {
		return table, defaultStatsAggregated, nil
	}
	if !validAggregations[suffix] {
		return "", "", fmt.Errorf("invalid granularity %q for global_stats: supported values are day, week, month", suffix)
	}
	return table, suffix, nil
}

func (s *SendGridSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName, aggregatedBy, err := parseTableName(req.Name)
	if err != nil {
		return nil, err
	}
	tc := supportedTables[tableName]

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    tc.primaryKeys,
		TableIncrementalKey: tc.incrementalKey,
		TableStrategy:       tc.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("sendgrid source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, aggregatedBy, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	_, ok := supportedTables[table]
	return ok
}

func supportedTableList() string {
	tables := make([]string, 0, len(supportedTables))
	for table := range supportedTables {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return strings.Join(tables, ", ")
}

func (s *SendGridSource) read(ctx context.Context, table, aggregatedBy string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		tc, ok := supportedTables[table]
		if !ok {
			results <- source.RecordBatchResult{Err: fmt.Errorf("unsupported table: %s", table)}
			return
		}

		config.Debug("[SENDGRID] reading %s", table)
		if err := s.fetch(ctx, table, tc, aggregatedBy, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *SendGridSource) clientForTier(tier rateLimitTier) *ingestrhttp.Client {
	if tier == tierEmailActivity {
		return s.activityClient
	}
	return s.client
}

func (s *SendGridSource) fetch(ctx context.Context, table string, tc tableConfig, aggregatedBy string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if tc.pagination == paginateBisect {
		return s.fetchMessages(ctx, table, tc, opts, results)
	}

	if tc.pagination == paginateGroupMembers {
		return s.fetchGroupMembers(ctx, table, tc, opts, results)
	}

	params, err := serverParams(tc, aggregatedBy, opts)
	if err != nil {
		return err
	}

	switch tc.pagination {
	case paginateOffset:
		return s.fetchOffset(ctx, table, tc, params, opts, results)
	case paginateToken:
		return s.fetchToken(ctx, table, tc, params, opts, results)
	default:
		return fmt.Errorf("sendgrid %s: unknown pagination style", table)
	}
}

// serverParams builds the server-side query parameters for a table based on its filter mode,
// starting from any static extraParams configured for the table.
func serverParams(tc tableConfig, aggregatedBy string, opts source.ReadOptions) (map[string]string, error) {
	params := map[string]string{}
	for k, v := range tc.extraParams {
		params[k] = v
	}

	switch tc.filter {
	case filterStatsDateRange:
		if opts.IntervalStart == nil {
			return nil, fmt.Errorf("sendgrid global_stats requires --interval-start because /stats requires start_date")
		}
		params["start_date"] = opts.IntervalStart.UTC().Format("2006-01-02")
		params["end_date"] = time.Now().UTC().Format("2006-01-02")
		if opts.IntervalEnd != nil {
			// end is exclusive: /stats end_date is an inclusive day, so use the last day
			// strictly before the end boundary.
			params["end_date"] = opts.IntervalEnd.UTC().Add(-time.Nanosecond).Format("2006-01-02")
		}
		params["aggregated_by"] = aggregatedBy
	case filterSuppressionUnixRange:
		if opts.IntervalStart != nil {
			params["start_time"] = strconv.FormatInt(opts.IntervalStart.UTC().Unix(), 10)
		}
		if opts.IntervalEnd != nil {
			// end is exclusive: SendGrid's end_time is an inclusive Unix second, so subtract one.
			params["end_time"] = strconv.FormatInt(opts.IntervalEnd.UTC().Unix()-1, 10)
		}
	}

	return params, nil
}

// fetchMessages reads the Email Activity endpoint, which caps each query at maxMessagesPageSize
// and supports no pagination. To retrieve more than one page, it recursively bisects the time
// range: a window that comes back full is split in half until each piece fits under the cap.
// The query window is half-open [from, to), so splits are disjoint and no message is fetched twice.
func (s *SendGridSource) fetchMessages(ctx context.Context, table string, tc tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	start, end := resolveMessagesRange(opts)

	// fetch queries a half-open [from, to) window, optionally constrained to msg_ids beginning
	// with msgIDPrefix (used to sub-partition a single saturated second).
	fetch := func(from, to time.Time, msgIDPrefix string) ([]map[string]interface{}, error) {
		query := buildMessagesQuery(source.ReadOptions{IntervalStart: &from, IntervalEnd: &to})
		if msgIDPrefix != "" {
			query += fmt.Sprintf(` AND msg_id LIKE "%s%%"`, escapeLike(msgIDPrefix))
		}
		req := s.clientForTier(tc.tier).R(ctx).
			SetQueryParam("limit", strconv.Itoa(tc.pageSize)).
			SetQueryParam("query", query)
		body, err := s.doRequest(table, tc, req)
		if err != nil {
			return nil, err
		}
		return extractItems(body, tc.dataKey), nil
	}

	emit := func(items []map[string]interface{}) error {
		return sendBatch(table, items, opts, results)
	}

	onExhausted := func(from, to time.Time, prefix string) {
		fmt.Fprintf(os.Stderr, "[WARNING] sendgrid messages: more than %d events fall within %s..%s under msg_id prefix %q and cannot be partitioned further; some events are not retrieved.\n", tc.pageSize, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), prefix)
	}

	return bisectWindows(ctx, start, end, tc.pageSize, fetch, emit, onExhausted)
}

// bisectWindows retrieves every message in [start, end) under SendGrid's 1000-row, no-pagination
// cap. It first bisects the time range: a window that comes back full is split in half until each
// piece is under the cap. Once a window is down to the timestamp granularity (messagesMinWindow)
// and is still full, more than pageSize events share that instant, so it switches to partitioning
// by msg_id prefix — recursively extending the prefix over the id alphabet until each bucket fits
// under the cap. Because msg_id is unique and uniformly distributed, this always converges; the
// onExhausted guard only fires in the degenerate case of >pageSize ids identical up to the prefix
// depth cap.
func bisectWindows(
	ctx context.Context,
	start, end time.Time,
	pageSize int,
	fetch func(from, to time.Time, msgIDPrefix string) ([]map[string]interface{}, error),
	emit func(items []map[string]interface{}) error,
	onExhausted func(from, to time.Time, prefix string),
) error {
	calls := 0
	guard := func() bool {
		calls++
		if calls > maxPages {
			config.Debug("[SENDGRID] maxPages guard reached for messages")
			return false
		}
		return true
	}

	// partitionByPrefix splits a single saturated time window by msg_id prefix.
	var partitionByPrefix func(from, to time.Time, prefix string) error
	partitionByPrefix = func(from, to time.Time, prefix string) error {
		for _, c := range messagesIDAlphabet {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if !guard() {
				return nil
			}

			sub := prefix + string(c)
			items, err := fetch(from, to, sub)
			if err != nil {
				return err
			}
			if len(items) < pageSize {
				if err := emit(items); err != nil {
					return err
				}
				continue
			}
			if len(sub) >= maxMessagesPrefixLen {
				onExhausted(from, to, sub)
				if err := emit(items); err != nil {
					return err
				}
				continue
			}
			if err := partitionByPrefix(from, to, sub); err != nil {
				return err
			}
		}
		return nil
	}

	var walk func(from, to time.Time) error
	walk = func(from, to time.Time) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if !guard() {
			return nil
		}

		items, err := fetch(from, to, "")
		if err != nil {
			return err
		}
		if len(items) < pageSize {
			return emit(items)
		}

		if to.Sub(from) <= messagesMinWindow {
			// Can't split time finer than the timestamp granularity; partition this instant by id.
			return partitionByPrefix(from, to, "")
		}

		mid := from.Add(to.Sub(from) / 2)
		if err := walk(from, mid); err != nil {
			return err
		}
		return walk(mid, to)
	}

	return walk(start, end)
}

// escapeLike escapes the SendGrid LIKE special characters so a literal msg_id prefix is matched
// exactly. SendGrid uses backslash as the default escape character.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// resolveMessagesRange resolves a concrete [start, end] window to bisect, defaulting the start
// to the Unix epoch and the end to now when the interval bounds are not provided.
func resolveMessagesRange(opts source.ReadOptions) (time.Time, time.Time) {
	start := time.Unix(0, 0).UTC()
	if opts.IntervalStart != nil {
		start = opts.IntervalStart.UTC()
	}
	end := time.Now().UTC()
	if opts.IntervalEnd != nil {
		end = opts.IntervalEnd.UTC()
	}
	return start, end
}

// fetchOffset paginates with limit+offset over a top-level array response.
func (s *SendGridSource) fetchOffset(ctx context.Context, table string, tc tableConfig, params map[string]string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	offset := 0
	for page := 1; page <= maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.clientForTier(tc.tier).R(ctx).
			SetQueryParam("limit", strconv.Itoa(tc.pageSize)).
			SetQueryParam("offset", strconv.Itoa(offset)).
			SetQueryParams(params)

		body, err := s.doRequest(table, tc, req)
		if err != nil {
			return err
		}

		items := extractItems(body, tc.dataKey)
		if err := sendFiltered(table, tc, items, opts, results); err != nil {
			return err
		}
		if len(items) < tc.pageSize {
			return nil
		}
		offset += tc.pageSize
	}

	config.Debug("[SENDGRID] maxPages guard reached for %s", table)
	return nil
}

// fetchToken paginates with page_size+page_token, following _metadata.next.
func (s *SendGridSource) fetchToken(ctx context.Context, table string, tc tableConfig, params map[string]string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageToken := ""
	for page := 1; page <= maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.clientForTier(tc.tier).R(ctx).
			SetQueryParam("page_size", strconv.Itoa(tc.pageSize)).
			SetQueryParams(params)
		if pageToken != "" {
			req.SetQueryParam("page_token", pageToken)
		}

		body, err := s.doRequest(table, tc, req)
		if err != nil {
			return err
		}

		items := extractItems(body, tc.dataKey)
		if err := sendFiltered(table, tc, items, opts, results); err != nil {
			return err
		}

		pageToken = nextPageToken(body)
		if pageToken == "" {
			return nil
		}
	}

	config.Debug("[SENDGRID] maxPages guard reached for %s", table)
	return nil
}

// fetchGroupMembers fans out over the unsubscribe groups from /asm/groups and emits one row
// {group_id, email} per suppressed address. The per-group endpoint returns a bare array of email
// strings, so the rows are constructed here.
func (s *SendGridSource) fetchGroupMembers(ctx context.Context, table string, tc tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	groupsBody, err := s.doRequest(table, tableConfig{endpoint: "/asm/groups", dataKey: ""}, s.clientForTier(tc.tier).R(ctx))
	if err != nil {
		return err
	}

	for _, group := range extractItems(groupsBody, "") {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		groupID, ok := idToString(group["id"])
		if !ok {
			continue
		}

		resp, err := s.clientForTier(tc.tier).R(ctx).Get(fmt.Sprintf("/asm/groups/%s/suppressions", groupID))
		if err != nil {
			return fmt.Errorf("failed to fetch sendgrid %s for group %s: %w", table, groupID, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("sendgrid %s request for group %s failed with status %d: %s", table, groupID, resp.StatusCode(), resp.String())
		}

		var emails []string
		if err := jsonUseNumber(resp.Body(), &emails); err != nil {
			return fmt.Errorf("failed to parse sendgrid %s response for group %s: %w", table, groupID, err)
		}

		items := make([]map[string]interface{}, 0, len(emails))
		for _, email := range emails {
			items = append(items, map[string]interface{}{"group_id": group["id"], "email": email})
		}
		if err := sendBatch(table, items, opts, results); err != nil {
			return err
		}
	}

	return nil
}

// idToString renders a JSON id (number or string) as a string for use in a URL path.
func idToString(v interface{}) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, t != ""
	case json.Number:
		return t.String(), true
	case float64:
		return strconv.FormatInt(int64(t), 10), true
	default:
		return "", false
	}
}

// doRequest performs the GET, checks the status, and decodes the JSON body into a map.
// Top-level array responses (dataKey == "") are wrapped under the "" key so extractItems can read them.
func (s *SendGridSource) doRequest(table string, tc tableConfig, req *ingestrhttp.Request) (map[string]interface{}, error) {
	resp, err := req.Get(tc.endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sendgrid %s from %s: %w", table, tc.endpoint, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("sendgrid %s request to %s failed with status %d: %s", table, tc.endpoint, resp.StatusCode(), resp.String())
	}

	if tc.dataKey == "" {
		var arr []interface{}
		if err := jsonUseNumber(resp.Body(), &arr); err != nil {
			return nil, fmt.Errorf("failed to parse sendgrid %s response from %s: %w", table, tc.endpoint, err)
		}
		return map[string]interface{}{"": arr}, nil
	}

	var body map[string]interface{}
	if err := jsonUseNumber(resp.Body(), &body); err != nil {
		return nil, fmt.Errorf("failed to parse sendgrid %s response from %s: %w", table, tc.endpoint, err)
	}
	return body, nil
}

// sendFiltered applies client-side interval filtering when configured, then sends the batch.
func sendFiltered(table string, tc tableConfig, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if tc.filter == filterClientSide && tc.incrementalKey != "" {
		items = filterByTimestamp(items, tc.incrementalKey, opts.IntervalStart, opts.IntervalEnd)
	}
	return sendBatch(table, items, opts, results)
}

// buildMessagesQuery builds the Email Activity filter with a half-open [start, end) window:
// the start is inclusive and the end is exclusive. This also makes bisection splits disjoint,
// so a message on a split boundary is fetched exactly once.
func buildMessagesQuery(opts source.ReadOptions) string {
	switch {
	case opts.IntervalStart != nil && opts.IntervalEnd != nil:
		return fmt.Sprintf(`last_event_time>=TIMESTAMP "%s" AND last_event_time<TIMESTAMP "%s"`, formatMessageTime(*opts.IntervalStart), formatMessageTime(*opts.IntervalEnd))
	case opts.IntervalStart != nil:
		return fmt.Sprintf(`last_event_time>=TIMESTAMP "%s"`, formatMessageTime(*opts.IntervalStart))
	case opts.IntervalEnd != nil:
		return fmt.Sprintf(`last_event_time<TIMESTAMP "%s"`, formatMessageTime(*opts.IntervalEnd))
	default:
		return fmt.Sprintf(`last_event_time>=TIMESTAMP "%s"`, defaultActivityStart)
	}
}

func extractItems(body map[string]interface{}, dataKey string) []map[string]interface{} {
	raw, ok := body[dataKey]
	if !ok {
		return nil
	}

	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}

	items := make([]map[string]interface{}, 0, len(arr))
	for _, v := range arr {
		if m, ok := v.(map[string]interface{}); ok {
			items = append(items, m)
		}
	}
	return items
}

func sendBatch(table string, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert sendgrid %s to Arrow: %w", table, err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[SENDGRID] sent %d %s records", len(items), table)
	return nil
}

func filterByTimestamp(items []map[string]interface{}, field string, start, end *time.Time) []map[string]interface{} {
	if start == nil && end == nil {
		return items
	}

	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		itemTime, ok := parseItemTime(item[field])
		if !ok {
			continue
		}
		if start != nil && itemTime.Before(start.UTC()) {
			continue
		}
		// end is exclusive: drop items at or after the end boundary.
		if end != nil && !itemTime.Before(end.UTC()) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func parseItemTime(value interface{}) (time.Time, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return time.Time{}, false
		}
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, false
		}
		return parsed.UTC(), true
	case json.Number:
		unix, err := v.Int64()
		if err != nil {
			return time.Time{}, false
		}
		return time.Unix(unix, 0).UTC(), true
	case float64:
		return time.Unix(int64(v), 0).UTC(), true
	default:
		return time.Time{}, false
	}
}

// jsonUseNumber decodes JSON with UseNumber to preserve large integer precision.
func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

// nextPageToken extracts the page_token query parameter from a _metadata.next URL.
func nextPageToken(body map[string]interface{}) string {
	meta, ok := body["_metadata"].(map[string]interface{})
	if !ok {
		return ""
	}
	nextURL, ok := meta["next"].(string)
	if !ok || nextURL == "" {
		return ""
	}
	parsed, err := url.Parse(nextURL)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("page_token")
}

func formatMessageTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

var _ source.Source = (*SendGridSource)(nil)
