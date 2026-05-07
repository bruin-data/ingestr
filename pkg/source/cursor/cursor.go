package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL         = "https://api.cursor.com"
	defaultPageSize = 100

	// All Admin API endpoints: 20 req/min per team
	// Use 15 req/min to stay safely under the limit
	rateLimit      = 18.0 / 60.0
	rateLimitBurst = 5
)

var supportedTables = []string{
	"team_members",
	"daily_usage_data",
	"team_spend",
	"filtered_usage_events",
}

type CursorSource struct {
	apiKey string
	client *ingestrhttp.Client
}

func NewCursorSource() *CursorSource {
	return &CursorSource{}
}

func (s *CursorSource) HandlesIncrementality() bool {
	return true
}

func (s *CursorSource) Schemes() []string {
	return []string{"cursor"}
}

func (s *CursorSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseCursorURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey

	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithRetry(5, 5*time.Second, 60*time.Second),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithAuth(ingestrhttp.NewBasicAuth(apiKey, "")),
	)
	config.Debug("[CURSOR] Connected successfully")
	return nil
}

func (s *CursorSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseCursorURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "cursor://") {
		return "", fmt.Errorf("invalid cursor URI: must start with cursor://")
	}

	rest := strings.TrimPrefix(uri, "cursor://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in cursor URI (cursor://?api_key=...)")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse cursor URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in cursor URI (cursor://?api_key=...)")
	}

	return apiKey, nil
}

func (s *CursorSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	return &source.DynamicSourceTable{
		TableName:     tableName,
		KnownSchema:   false,
		TableStrategy: config.StrategyReplace,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("cursor source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func (s *CursorSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "team_members":
			err = s.readTeamMembers(ctx, opts, results)
		case "daily_usage_data":
			err = s.readDailyUsageData(ctx, opts, results)
		case "team_spend":
			err = s.readTeamSpend(ctx, opts, results)
		case "filtered_usage_events":
			err = s.readUsageEvents(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *CursorSource) readTeamMembers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[Cursor] Fetching team members")

	resp, err := s.client.R(ctx).Get("teams/members")
	if err != nil {
		return fmt.Errorf("failed to fetch teams/members: %w", err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("failed to fetch teams/members: status %d: %s", resp.StatusCode(), resp.String())
	}

	var raw map[string]json.RawMessage
	if err := resp.JSON(&raw); err != nil {
		return fmt.Errorf("failed to parse teams/members response: %w", err)
	}

	itemsRaw, ok := raw["teamMembers"]
	if !ok {
		return fmt.Errorf("response missing 'teamMembers' field")
	}

	var items []map[string]interface{}
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		return fmt.Errorf("failed to parse teams/members items: %w", err)
	}

	if len(items) == 0 {
		config.Debug("[CURSOR] No team members found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert teams/members to Arrow: %w", err)
	}

	config.Debug("[CURSOR] Sending %d team members", len(items))
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

func (s *CursorSource) readDailyUsageData(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[Cursor] Fetching daily usage data")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > defaultPageSize {
		pageSize = defaultPageSize
	}

	start, end := resolveTimeRange(opts.IntervalStart, opts.IntervalEnd)
	const maxDays = 30

	type chunk struct {
		start time.Time
		end   time.Time
	}

	var chunks []chunk
	for cs := start; cs.Before(end); {
		ce := cs.AddDate(0, 0, maxDays)
		if ce.After(end) {
			ce = end
		}
		chunks = append(chunks, chunk{start: cs, end: ce})
		cs = ce
	}

	for _, c := range chunks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		extraBody := map[string]interface{}{
			"startDate": c.start.UnixMilli(),
			"endDate":   c.end.UnixMilli(),
		}
		if err := s.paginatePostAndSend(ctx, opts, results, "teams/daily-usage-data", "data", pageSize, extraBody, nil, hasMoreByPageSize); err != nil {
			return err
		}
	}

	return nil
}

func (s *CursorSource) readTeamSpend(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[Cursor] Fetching team spend")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > defaultPageSize {
		pageSize = defaultPageSize
	}

	return s.paginatePostAndSend(ctx, opts, results, "teams/spend", "teamMemberSpend", pageSize, nil, nil, hasMoreByTotalPages)
}

func (s *CursorSource) readUsageEvents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[Cursor] Fetching filtered usage events")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > defaultPageSize {
		pageSize = defaultPageSize
	}

	start, end := resolveTimeRange(opts.IntervalStart, opts.IntervalEnd)
	const maxDays = 30

	type chunk struct {
		start time.Time
		end   time.Time
	}

	var chunks []chunk
	for cs := start; cs.Before(end); {
		ce := cs.AddDate(0, 0, maxDays)
		if ce.After(end) {
			ce = end
		}
		chunks = append(chunks, chunk{start: cs, end: ce})
		cs = ce
	}

	for _, c := range chunks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		extraBody := map[string]interface{}{
			"startDate": c.start.UnixMilli(),
			"endDate":   c.end.UnixMilli(),
		}
		if err := s.paginatePostAndSend(ctx, opts, results, "teams/filtered-usage-events", "usageEvents", pageSize, extraBody, nil, hasMoreByHasNextPage); err != nil {
			return err
		}
	}

	return nil
}

type hasMoreFn func(raw map[string]json.RawMessage, itemCount int, pageSize int) bool

func hasMoreByTotalPages(raw map[string]json.RawMessage, _ int, _ int) bool {
	var currentPage, totalPages int
	if cp, ok := raw["currentPage"]; ok {
		_ = json.Unmarshal(cp, &currentPage)
	}
	if tp, ok := raw["totalPages"]; ok {
		_ = json.Unmarshal(tp, &totalPages)
	}
	return currentPage < totalPages
}

func hasMoreByHasNextPage(raw map[string]json.RawMessage, _ int, _ int) bool {
	paginationRaw, ok := raw["pagination"]
	if !ok {
		return false
	}
	var pagination struct {
		HasNextPage bool `json:"hasNextPage"`
	}
	if err := json.Unmarshal(paginationRaw, &pagination); err != nil {
		return false
	}
	return pagination.HasNextPage
}

func hasMoreByPageSize(_ map[string]json.RawMessage, itemCount int, pageSize int) bool {
	return itemCount >= pageSize
}

func resolveTimeRange(intervalStart, intervalEnd interface{}) (time.Time, time.Time) {
	now := time.Now().UTC()
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := now

	if t := toTime(intervalStart); !t.IsZero() {
		start = t
	}
	if t := toTime(intervalEnd); !t.IsZero() {
		end = t
	}

	if start.After(now) {
		start = now
	}
	if end.After(now) {
		end = now
	}

	return start, end
}

func toTime(v interface{}) time.Time {
	if v == nil {
		return time.Time{}
	}
	switch t := v.(type) {
	case time.Time:
		return t
	case *time.Time:
		if t != nil {
			return *t
		}
	case string:
		if parsed, err := time.Parse("2006-01-02", t); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func (s *CursorSource) paginatePostAndSend(
	ctx context.Context,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	endpoint string,
	responseKey string,
	pageSize int,
	extraBody map[string]interface{},
	injectFields map[string]interface{},
	hasMore hasMoreFn,
) error {
	items, raw, err := s.fetchPostPage(ctx, endpoint, responseKey, pageSize, 1, extraBody)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		config.Debug("[CURSOR] No %s found", endpoint)
		return nil
	}

	if err := s.sendItems(items, opts, results, endpoint, injectFields); err != nil {
		return err
	}

	if !hasMore(raw, len(items), pageSize) {
		return nil
	}

	for page := 2; ; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		items, raw, err := s.fetchPostPage(ctx, endpoint, responseKey, pageSize, page, extraBody)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			break
		}
		if err := s.sendItems(items, opts, results, endpoint, injectFields); err != nil {
			return err
		}
		if !hasMore(raw, len(items), pageSize) {
			break
		}
	}

	return nil
}

func (s *CursorSource) fetchPostPage(
	ctx context.Context,
	endpoint string,
	responseKey string,
	pageSize int,
	page int,
	extraBody map[string]interface{},
) ([]map[string]interface{}, map[string]json.RawMessage, error) {
	body := map[string]interface{}{
		"pageSize": pageSize,
		"page":     page,
	}
	for k, v := range extraBody {
		body[k] = v
	}

	resp, err := s.client.R(ctx).SetBody(body).Post(endpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch %s: %w", endpoint, err)
	}

	if !resp.IsSuccess() {
		return nil, nil, fmt.Errorf("failed to fetch %s: status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	var raw map[string]json.RawMessage
	if err := resp.JSON(&raw); err != nil {
		return nil, nil, fmt.Errorf("failed to parse %s response: %w", endpoint, err)
	}

	itemsRaw, ok := raw[responseKey]
	if !ok {
		return nil, nil, fmt.Errorf("response missing '%s' field", responseKey)
	}

	var items []map[string]interface{}
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		return nil, nil, fmt.Errorf("failed to parse %s items: %w", endpoint, err)
	}

	return items, raw, nil
}

func (s *CursorSource) sendItems(
	items []map[string]interface{},
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	endpoint string,
	injectFields map[string]interface{},
) error {
	for _, item := range items {
		for k, v := range injectFields {
			item[k] = v
		}
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert %s to Arrow: %w", endpoint, err)
	}

	config.Debug("[CURSOR] Sending %d %s", len(items), endpoint)
	results <- source.RecordBatchResult{Batch: record}
	return nil
}
