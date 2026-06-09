package sendgrid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
	maxBouncesPageSize     = 500
	maxListsPageSize       = 1000 // GET /marketing/lists allows up to 1000
	maxSingleSendsPageSize = 100  // GET /marketing/singlesends caps page_size at 100
	maxPages               = 10000

	defaultActivityStart   = "1970-01-01T00:00:00Z"
	defaultStatsAggregated = "day"

	// SendGrid Email Activity API is documented at 6 req/min: (6 * 0.8) / 60 = 0.08 req/sec.
	emailActivityRateLimit = 0.08
	// SendGrid Web API v3 documents endpoint-specific rate-limit headers but no global numeric limit.
	// Keep a conservative local cap for non-Email-Activity endpoints and rely on retries for 429s.
	generalRateLimit = 5.0
	rateLimitBurst   = 5
)

type paginationStyle int

const (
	paginateSingle paginationStyle = iota // one request, no pagination (messages)
	paginateOffset                        // limit + offset, top-level array response (global_stats, bounces)
	paginateToken                         // page_size + page_token via _metadata.next (lists, single_sends)
)

type filterMode int

const (
	filterNone            filterMode = iota
	filterActivityQuery              // messages: Email Activity query on last_event_time
	filterStatsDateRange             // global_stats: start_date/end_date query params (requires --interval-start)
	filterBounceUnixRange            // bounces: start_time/end_time Unix query params
	filterClientSide                 // single_sends: client-side filter on incrementalKey
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
}

var supportedTables = map[string]tableConfig{
	"messages": {
		endpoint:       "/messages",
		dataKey:        "messages",
		primaryKeys:    []string{"msg_id"},
		incrementalKey: "last_event_time",
		strategy:       config.StrategyMerge,
		pageSize:       maxMessagesPageSize,
		pagination:     paginateSingle,
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
		pageSize:       maxBouncesPageSize,
		pagination:     paginateOffset,
		filter:         filterBounceUnixRange,
		tier:           tierGeneral,
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
	apiKey             string
	onBehalfOf         string
	emailActivityQuery string
	statsAggregatedBy  string
}

type SendGridSource struct {
	apiKey             string
	onBehalfOf         string
	emailActivityQuery string
	statsAggregatedBy  string
	client             *ingestrhttp.Client
	activityClient     *ingestrhttp.Client
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
	s.emailActivityQuery = creds.emailActivityQuery
	s.statsAggregatedBy = creds.statsAggregatedBy

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

	statsAggregatedBy := values.Get("stats_aggregated_by")
	if statsAggregatedBy == "" {
		statsAggregatedBy = defaultStatsAggregated
	}
	if !validAggregations[statsAggregatedBy] {
		return credentials{}, fmt.Errorf("invalid stats_aggregated_by %q in sendgrid URI: supported values are day, week, month", statsAggregatedBy)
	}

	return credentials{
		apiKey:             apiKey,
		onBehalfOf:         values.Get("on_behalf_of"),
		emailActivityQuery: strings.TrimSpace(values.Get("email_activity_query")),
		statsAggregatedBy:  statsAggregatedBy,
	}, nil
}

func (s *SendGridSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, supportedTableList())
	}
	tc := supportedTables[tableName]

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    tc.primaryKeys,
		TableIncrementalKey: tc.incrementalKey,
		TableStrategy:       tc.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("sendgrid source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
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

func (s *SendGridSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		tc, ok := supportedTables[table]
		if !ok {
			results <- source.RecordBatchResult{Err: fmt.Errorf("unsupported table: %s", table)}
			return
		}

		config.Debug("[SENDGRID] reading %s", table)
		if err := s.fetch(ctx, table, tc, opts, results); err != nil {
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

func (s *SendGridSource) fetch(ctx context.Context, table string, tc tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	params, err := s.serverParams(tc, opts)
	if err != nil {
		return err
	}

	switch tc.pagination {
	case paginateSingle:
		return s.fetchSingle(ctx, table, tc, params, opts, results)
	case paginateOffset:
		return s.fetchOffset(ctx, table, tc, params, opts, results)
	case paginateToken:
		return s.fetchToken(ctx, table, tc, params, opts, results)
	default:
		return fmt.Errorf("sendgrid %s: unknown pagination style", table)
	}
}

// serverParams builds the server-side query parameters for a table based on its filter mode.
func (s *SendGridSource) serverParams(tc tableConfig, opts source.ReadOptions) (map[string]string, error) {
	params := map[string]string{}

	switch tc.filter {
	case filterActivityQuery:
		params["query"] = s.buildMessagesQuery(opts)
	case filterStatsDateRange:
		if opts.IntervalStart == nil {
			return nil, fmt.Errorf("sendgrid global_stats requires --interval-start because /stats requires start_date")
		}
		params["start_date"] = opts.IntervalStart.UTC().Format("2006-01-02")
		params["end_date"] = time.Now().UTC().Format("2006-01-02")
		if opts.IntervalEnd != nil {
			params["end_date"] = opts.IntervalEnd.UTC().Format("2006-01-02")
		}
		params["aggregated_by"] = s.statsAggregatedBy
	case filterBounceUnixRange:
		if opts.IntervalStart != nil {
			params["start_time"] = strconv.FormatInt(opts.IntervalStart.UTC().Unix(), 10)
		}
		if opts.IntervalEnd != nil {
			params["end_time"] = strconv.FormatInt(opts.IntervalEnd.UTC().Unix(), 10)
		}
	}

	return params, nil
}

// fetchSingle issues one request (no pagination) and sends the resulting items.
func (s *SendGridSource) fetchSingle(ctx context.Context, table string, tc tableConfig, params map[string]string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	req := s.clientForTier(tc.tier).R(ctx).
		SetQueryParam("limit", strconv.Itoa(tc.pageSize)).
		SetQueryParams(params)

	body, err := s.doRequest(table, tc, req)
	if err != nil {
		return err
	}

	items := extractItems(body, tc.dataKey)
	return sendFiltered(table, tc, items, opts, results)
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

func (s *SendGridSource) buildMessagesQuery(opts source.ReadOptions) string {
	clauses := make([]string, 0, 2)

	switch {
	case opts.IntervalStart != nil && opts.IntervalEnd != nil:
		clauses = append(clauses, fmt.Sprintf(`last_event_time BETWEEN TIMESTAMP "%s" AND TIMESTAMP "%s"`, formatMessageTime(*opts.IntervalStart), formatMessageTime(*opts.IntervalEnd)))
	case opts.IntervalStart != nil:
		clauses = append(clauses, fmt.Sprintf(`last_event_time>=TIMESTAMP "%s"`, formatMessageTime(*opts.IntervalStart)))
	case opts.IntervalEnd != nil:
		clauses = append(clauses, fmt.Sprintf(`last_event_time<=TIMESTAMP "%s"`, formatMessageTime(*opts.IntervalEnd)))
	default:
		clauses = append(clauses, fmt.Sprintf(`last_event_time>=TIMESTAMP "%s"`, defaultActivityStart))
	}

	if s.emailActivityQuery != "" {
		clauses = append(clauses, fmt.Sprintf("(%s)", s.emailActivityQuery))
	}

	return strings.Join(clauses, " AND ")
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
		if end != nil && itemTime.After(end.UTC()) {
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
