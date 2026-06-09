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

/*
SendGrid source design notes (official Twilio SendGrid docs are the source of truth):
  - Authentication uses Web API v3 bearer API keys. Docs: https://www.twilio.com/docs/sendgrid/for-developers/sending-email/api-getting-started
  - messages uses GET /v3/messages. The API requires query syntax and caps limit at 1000; it does not document cursor/offset pagination, so ingestr returns one page. Docs: https://www.twilio.com/docs/sendgrid/api-reference/email-activity/filter-all-messages
  - messages interval sync uses Email Activity query syntax on last_event_time. BETWEEN is documented for date ranges; >= and <= are documented operators. Docs: https://www.twilio.com/docs/sendgrid/for-developers/sending-email/getting-started-email-activity-api
  - global_stats uses GET /v3/stats with start_date, optional end_date, aggregated_by, limit, and offset. Docs: https://www.twilio.com/docs/sendgrid/api-reference/stats/retrieve-global-email-statistics
  - bounces uses GET /v3/suppression/bounces with inclusive start_time/end_time Unix timestamps and offset pagination. Docs: https://www.twilio.com/docs/sendgrid/api-reference/bounces-api/retrieve-all-bounces
  - lists uses GET /v3/marketing/lists with page_size/page_token and _metadata.next. Docs: https://www.twilio.com/docs/sendgrid/api-reference/lists/get-all-lists
  - single_sends uses the current Marketing Campaigns Single Sends endpoint, not obsolete legacy campaigns, and client-side filters updated_at because the list endpoint documents no time filter. Docs: https://www.twilio.com/docs/sendgrid/api-reference/single-sends/get-all-single-sends
  - Nested objects and arrays are sent unchanged as JSON values; arrowconv serializes them as JSON strings, matching max_table_nesting=0 behavior.
  - No table has independent documented child-resource fanout, so parallelism is intentionally off for the initial implementation.
*/

const (
	baseURL = "https://api.sendgrid.com/v3"

	maxMessagesPageSize    = 1000
	maxStatsPageSize       = 500
	maxBouncesPageSize     = 500
	maxMarketingPageSize   = 1000
	maxPages               = 10000
	defaultActivityStart   = "1970-01-01T00:00:00Z"
	defaultStatsAggregated = "day"

	// SendGrid Email Activity API is documented at 6 req/min: (6 * 0.8) / 60 = 0.08 req/sec.
	emailActivityRateLimit = 0.08
	// SendGrid Web API v3 documents endpoint-specific rate-limit headers, but no global numeric limit.
	// Keep a conservative local cap for non-Email-Activity endpoints and rely on retries for 429s.
	generalRateLimit = 5.0
	rateLimitBurst   = 5
)

var supportedTables = map[string]tableConfig{
	"messages": {
		primaryKeys:    []string{"msg_id"},
		incrementalKey: "last_event_time",
		strategy:       config.StrategyMerge,
		rateLimitTier:  "email_activity",
	},
	"global_stats": {
		primaryKeys:    []string{"date"},
		incrementalKey: "date",
		strategy:       config.StrategyMerge,
		rateLimitTier:  "general",
	},
	"bounces": {
		primaryKeys:    []string{"email", "created"},
		incrementalKey: "created",
		strategy:       config.StrategyMerge,
		rateLimitTier:  "general",
	},
	"lists": {
		primaryKeys:   []string{"id"},
		strategy:      config.StrategyReplace,
		rateLimitTier: "general",
	},
	"single_sends": {
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		rateLimitTier:  "general",
	},
}

var validAggregations = map[string]bool{
	"day":   true,
	"week":  true,
	"month": true,
}

type tableConfig struct {
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
	rateLimitTier  string
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
	cfg := supportedTables[tableName]

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    cfg.primaryKeys,
		TableIncrementalKey: cfg.incrementalKey,
		TableStrategy:       cfg.strategy,
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

		var err error
		switch table {
		case "messages":
			err = s.readMessages(ctx, opts, results)
		case "global_stats":
			err = s.readGlobalStats(ctx, opts, results)
		case "bounces":
			err = s.readBounces(ctx, opts, results)
		case "lists":
			err = s.readLists(ctx, opts, results)
		case "single_sends":
			err = s.readSingleSends(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *SendGridSource) readMessages(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SENDGRID] reading messages")

	query := s.buildMessagesQuery(opts)
	var response messagesResponse
	resp, err := s.activityClient.R(ctx).
		SetQueryParam("limit", strconv.Itoa(maxMessagesPageSize)).
		SetQueryParam("query", query).
		Get("/messages")
	if err != nil {
		return fmt.Errorf("failed to fetch sendgrid messages from /messages: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("sendgrid messages request to /messages failed with status %d: %s", resp.StatusCode(), resp.String())
	}
	if err := decodeJSON(resp.Body(), &response); err != nil {
		return fmt.Errorf("failed to parse sendgrid messages response from /messages: %w", err)
	}

	return sendItems("messages", response.Messages, opts, results)
}

func (s *SendGridSource) readGlobalStats(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SENDGRID] reading global_stats")

	if opts.IntervalStart == nil {
		return fmt.Errorf("sendgrid global_stats requires --interval-start because /stats requires start_date")
	}

	startDate := opts.IntervalStart.UTC().Format("2006-01-02")
	endDate := time.Now().UTC().Format("2006-01-02")
	if opts.IntervalEnd != nil {
		endDate = opts.IntervalEnd.UTC().Format("2006-01-02")
	}

	offset := 0
	for page := 1; page <= maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var items []map[string]interface{}
		resp, err := s.client.R(ctx).
			SetQueryParam("start_date", startDate).
			SetQueryParam("end_date", endDate).
			SetQueryParam("aggregated_by", s.statsAggregatedBy).
			SetQueryParam("limit", strconv.Itoa(maxStatsPageSize)).
			SetQueryParam("offset", strconv.Itoa(offset)).
			Get("/stats")
		if err != nil {
			return fmt.Errorf("failed to fetch sendgrid global_stats from /stats: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("sendgrid global_stats request to /stats failed with status %d: %s", resp.StatusCode(), resp.String())
		}
		if err := decodeJSON(resp.Body(), &items); err != nil {
			return fmt.Errorf("failed to parse sendgrid global_stats response from /stats: %w", err)
		}

		if err := sendItems("global_stats", items, opts, results); err != nil {
			return err
		}
		if len(items) < maxStatsPageSize {
			return nil
		}
		offset += maxStatsPageSize
	}

	config.Debug("[SENDGRID] maxPages guard reached for global_stats")
	return nil
}

func (s *SendGridSource) readBounces(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SENDGRID] reading bounces")

	offset := 0
	for page := 1; page <= maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("limit", strconv.Itoa(maxBouncesPageSize)).
			SetQueryParam("offset", strconv.Itoa(offset))
		if opts.IntervalStart != nil {
			req.SetQueryParam("start_time", strconv.FormatInt(opts.IntervalStart.UTC().Unix(), 10))
		}
		if opts.IntervalEnd != nil {
			req.SetQueryParam("end_time", strconv.FormatInt(opts.IntervalEnd.UTC().Unix(), 10))
		}

		var items []map[string]interface{}
		resp, err := req.Get("/suppression/bounces")
		if err != nil {
			return fmt.Errorf("failed to fetch sendgrid bounces from /suppression/bounces: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("sendgrid bounces request to /suppression/bounces failed with status %d: %s", resp.StatusCode(), resp.String())
		}
		if err := decodeJSON(resp.Body(), &items); err != nil {
			return fmt.Errorf("failed to parse sendgrid bounces response from /suppression/bounces: %w", err)
		}

		if err := sendItems("bounces", items, opts, results); err != nil {
			return err
		}
		if len(items) < maxBouncesPageSize {
			return nil
		}
		offset += maxBouncesPageSize
	}

	config.Debug("[SENDGRID] maxPages guard reached for bounces")
	return nil
}

func (s *SendGridSource) readLists(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SENDGRID] reading lists")
	return s.paginateAndSend(ctx, paginationConfig{
		table:       "lists",
		endpoint:    "/marketing/lists",
		pageSize:    maxMarketingPageSize,
		filterField: "",
	}, opts, results)
}

func (s *SendGridSource) readSingleSends(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SENDGRID] reading single_sends")
	return s.paginateAndSend(ctx, paginationConfig{
		table:       "single_sends",
		endpoint:    "/marketing/singlesends",
		pageSize:    maxMarketingPageSize,
		filterField: "updated_at",
	}, opts, results)
}

func (s *SendGridSource) paginateAndSend(ctx context.Context, cfg paginationConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageToken := ""
	for page := 1; page <= maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).SetQueryParam("page_size", strconv.Itoa(cfg.pageSize))
		if pageToken != "" {
			req.SetQueryParam("page_token", pageToken)
		}

		var response resultResponse
		resp, err := req.Get(cfg.endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch sendgrid %s from %s: %w", cfg.table, cfg.endpoint, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("sendgrid %s request to %s failed with status %d: %s", cfg.table, cfg.endpoint, resp.StatusCode(), resp.String())
		}
		if err := decodeJSON(resp.Body(), &response); err != nil {
			return fmt.Errorf("failed to parse sendgrid %s response from %s: %w", cfg.table, cfg.endpoint, err)
		}

		items := response.Result
		if cfg.filterField != "" {
			items = filterItemsByInterval(items, cfg.filterField, opts.IntervalStart, opts.IntervalEnd)
		}
		if err := sendItems(cfg.table, items, opts, results); err != nil {
			return err
		}

		nextToken := nextPageToken(response.Metadata.Next)
		if nextToken == "" {
			return nil
		}
		pageToken = nextToken
	}

	config.Debug("[SENDGRID] maxPages guard reached for %s", cfg.table)
	return nil
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

func sendItems(table string, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
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

func filterItemsByInterval(items []map[string]interface{}, field string, start, end *time.Time) []map[string]interface{} {
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

func decodeJSON(body []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func nextPageToken(nextURL string) string {
	if nextURL == "" {
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

type paginationConfig struct {
	table       string
	endpoint    string
	pageSize    int
	filterField string
}

type messagesResponse struct {
	Messages []map[string]interface{} `json:"messages"`
}

type resultResponse struct {
	Result   []map[string]interface{} `json:"result"`
	Metadata metadata                 `json:"_metadata"`
}

type metadata struct {
	Next string `json:"next"`
}

var _ source.Source = (*SendGridSource)(nil)
