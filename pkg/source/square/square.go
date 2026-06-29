package square

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	productionBaseURL = "https://connect.squareup.com"
	sandboxBaseURL    = "https://connect.squareupsandbox.com"

	// Square is date-versioned and requires Square-Version on every request.
	defaultAPIVersion = "2025-01-23"

	maxPageSize = 100

	// 10 rps with a small burst stays under Square's per-application QPS cap (429s).
	rateLimit      = 10.0
	rateLimitBurst = 5

	// Bounded concurrency for per-location fan-outs (e.g. cash_drawers). The
	// shared HTTP rate limiter handles backpressure, so this just caps in-flight.
	locationWorkers = 5
)

type tableReadFunc func(s *SquareSource, ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error

type tableConfig struct {
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
	read           tableReadFunc
}

func getList(path, key string, baseQuery map[string]string) tableReadFunc {
	return func(s *SquareSource, ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
		return s.readGetList(ctx, path, key, baseQuery, opts, results)
	}
}

func search(path, key string, baseBody map[string]interface{}) tableReadFunc {
	return func(s *SquareSource, ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
		return s.readSearch(ctx, path, key, baseBody, opts, results)
	}
}

func limitQuery() map[string]string {
	return map[string]string{"limit": strconv.Itoa(maxPageSize)}
}

var supportedTables = map[string]tableConfig{
	"payments": {
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		read: func(s *SquareSource, ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
			return s.readUpdatedRangeList(ctx, "/v2/payments", "payments", opts, results)
		},
	},
	"refunds": {
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		read: func(s *SquareSource, ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
			return s.readUpdatedRangeList(ctx, "/v2/refunds", "refunds", opts, results)
		},
	},
	"orders": {
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		read:           (*SquareSource).readOrders,
	},
	"customers": {
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		read:           (*SquareSource).readCustomers,
	},
	"catalog_objects": {
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		read:           (*SquareSource).readCatalog,
	},
	"locations": {
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		read:        getList("/v2/locations", "locations", nil),
	},
	"team_members": {
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		read:           (*SquareSource).readTeamMembers,
	},
	"team_member_wages": {
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		read:        getList("/v2/labor/team-member-wages", "team_member_wages", limitQuery()),
	},
	"shifts": {
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		read: search("/v2/labor/shifts/search", "shifts", map[string]interface{}{
			"limit": maxPageSize,
			"query": map[string]interface{}{
				"sort": map[string]interface{}{"field": "UPDATED_AT", "order": "ASC"},
			},
		}),
	},
	"inventory": {
		primaryKeys:    []string{"catalog_object_id", "location_id", "state"},
		incrementalKey: "calculated_at",
		strategy:       config.StrategyMerge,
		read:           (*SquareSource).readInventory,
	},
	"bank_accounts": {
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		read:        getList("/v2/bank-accounts", "bank_accounts", limitQuery()),
	},
	"cash_drawers": {
		primaryKeys: []string{"id", "location_id"},
		strategy:    config.StrategyReplace,
		read:        (*SquareSource).readCashDrawerShifts,
	},
	"loyalty": {
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		read:        search("/v2/loyalty/accounts/search", "loyalty_accounts", map[string]interface{}{"limit": maxPageSize}),
	},
}

type squareCredentials struct {
	accessToken string
	baseURL     string
}

type SquareSource struct {
	client *httpclient.Client
}

func NewSquareSource() *SquareSource {
	return &SquareSource{}
}

func (s *SquareSource) Schemes() []string {
	return []string{"square"}
}

func (s *SquareSource) HandlesIncrementality() bool {
	return true
}

func (s *SquareSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.client = httpclient.New(
		httpclient.WithBaseURL(creds.baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(creds.accessToken)),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithHeader("Square-Version", defaultAPIVersion),
	)

	config.Debug("[SQUARE] connected (%s, version %s)", creds.baseURL, defaultAPIVersion)
	return nil
}

func (s *SquareSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (squareCredentials, error) {
	if !strings.HasPrefix(uri, "square://") {
		return squareCredentials{}, fmt.Errorf("invalid square URI: must start with square://")
	}

	rest := strings.TrimPrefix(uri, "square://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return squareCredentials{}, fmt.Errorf("failed to parse square URI query: %w", err)
	}

	creds := squareCredentials{
		accessToken: values.Get("access_token"),
		baseURL:     productionBaseURL,
	}

	if creds.accessToken == "" {
		return squareCredentials{}, fmt.Errorf("access_token is required in square URI")
	}

	switch env := strings.ToLower(values.Get("environment")); env {
	case "", "production":
		creds.baseURL = productionBaseURL
	case "sandbox":
		creds.baseURL = sandboxBaseURL
	default:
		return squareCredentials{}, fmt.Errorf("invalid environment %q in square URI (supported: production, sandbox)", env)
	}

	return creds, nil
}

func (s *SquareSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, supportedTableNames())
	}
	cfg := supportedTables[tableName]

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    cfg.primaryKeys,
		TableIncrementalKey: cfg.incrementalKey,
		TableStrategy:       cfg.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("square source does not have a predefined schema; schema inference is required")
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

func supportedTableNames() string {
	names := make([]string, 0, len(supportedTables))
	for name := range supportedTables {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (s *SquareSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	cfg, ok := supportedTables[table]
	if !ok {
		go func() {
			defer close(results)
			results <- source.RecordBatchResult{Err: fmt.Errorf("unsupported table: %s", table)}
		}()
		return results, nil
	}

	go func() {
		defer close(results)
		if err := cfg.read(s, ctx, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// cursor is "" on the first call and "" again once the API stops returning one.
type fetchPageFunc func(cursor string) (items []map[string]interface{}, nextCursor string, err error)

func (s *SquareSource) paginate(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, table string, fetch fetchPageFunc) error {
	cursor := ""
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		items, nextCursor, err := fetch(cursor)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", table, err)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", table, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
			config.Debug("[SQUARE] %s: sent %d records (total: %d)", table, len(items), totalSent)
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	if totalSent == 0 {
		config.Debug("[SQUARE] no records found for %s", table)
	}
	return nil
}

// baseQuery is sent on the first request only; Square requires subsequent pages
// to send the cursor alone, with no other params.
func (s *SquareSource) readGetList(ctx context.Context, path, key string, baseQuery map[string]string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.paginate(ctx, opts, results, key, func(cursor string) ([]map[string]interface{}, string, error) {
		req := s.client.R(ctx)
		if cursor == "" {
			if len(baseQuery) > 0 {
				req = req.SetQueryParams(baseQuery)
			}
		} else {
			req = req.SetQueryParam("cursor", cursor)
		}

		resp, err := req.Get(path)
		if err != nil {
			return nil, "", err
		}
		if !resp.IsSuccess() {
			return nil, "", fmt.Errorf("square API %s returned status %d: %s", path, resp.StatusCode(), resp.String())
		}
		return decodeListResponse(resp.Body(), key)
	})
}

// readSearchFiltered is readSearch with a per-page client-side filter on dateField,
// used where Square's endpoint doesn't expose a matching server-side filter.
func (s *SquareSource) readSearchFiltered(ctx context.Context, path, key string, baseBody map[string]interface{}, dateField string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if dateField == "" || (opts.IntervalStart == nil && opts.IntervalEnd == nil) {
		return s.readSearch(ctx, path, key, baseBody, opts, results)
	}
	return s.paginate(ctx, opts, results, key, func(cursor string) ([]map[string]interface{}, string, error) {
		body := make(map[string]interface{}, len(baseBody)+1)
		for k, v := range baseBody {
			body[k] = v
		}
		if cursor != "" {
			body["cursor"] = cursor
		}
		resp, err := s.client.R(ctx).SetBody(body).Post(path)
		if err != nil {
			return nil, "", err
		}
		if !resp.IsSuccess() {
			return nil, "", fmt.Errorf("square API %s returned status %d: %s", path, resp.StatusCode(), resp.String())
		}
		items, next, err := decodeListResponse(resp.Body(), key)
		if err != nil {
			return nil, "", err
		}
		return filterItemsByInterval(items, dateField, opts.IntervalStart, opts.IntervalEnd), next, nil
	})
}

// Square's search endpoints require baseBody to be resent on every page.
func (s *SquareSource) readSearch(ctx context.Context, path, key string, baseBody map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.paginate(ctx, opts, results, key, func(cursor string) ([]map[string]interface{}, string, error) {
		body := make(map[string]interface{}, len(baseBody)+1)
		for k, v := range baseBody {
			body[k] = v
		}
		if cursor != "" {
			body["cursor"] = cursor
		}

		resp, err := s.client.R(ctx).SetBody(body).Post(path)
		if err != nil {
			return nil, "", err
		}
		if !resp.IsSuccess() {
			return nil, "", fmt.Errorf("square API %s returned status %d: %s", path, resp.StatusCode(), resp.String())
		}
		return decodeListResponse(resp.Body(), key)
	})
}

// sort_field=UPDATED_AT pairs with updated_at_{begin,end}_time so a status flip
// gets picked up by a later run even when the row was created outside the interval.
func (s *SquareSource) readUpdatedRangeList(ctx context.Context, path, key string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	baseQuery := map[string]string{
		"limit":      strconv.Itoa(maxPageSize),
		"sort_field": "UPDATED_AT",
		"sort_order": "ASC",
	}
	if opts.IntervalStart != nil {
		baseQuery["updated_at_begin_time"] = opts.IntervalStart.UTC().Format(time.RFC3339)
	}
	if opts.IntervalEnd != nil {
		baseQuery["updated_at_end_time"] = opts.IntervalEnd.UTC().Format(time.RFC3339)
	}
	return s.readGetList(ctx, path, key, baseQuery, opts, results)
}

// SearchOrders requires location_ids; passing the full set lets Square scope
// to every location server-side in one cursor stream.
func (s *SquareSource) readOrders(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	locationIDs, err := s.listLocationIDs(ctx)
	if err != nil {
		return err
	}
	if len(locationIDs) == 0 {
		config.Debug("[SQUARE] no locations found; skipping orders")
		return nil
	}

	query := map[string]interface{}{
		"sort": map[string]interface{}{
			"sort_field": "UPDATED_AT",
			"sort_order": "ASC",
		},
	}
	if opts.IntervalStart != nil || opts.IntervalEnd != nil {
		updatedAt := map[string]interface{}{}
		if opts.IntervalStart != nil {
			updatedAt["start_at"] = opts.IntervalStart.UTC().Format(time.RFC3339)
		}
		if opts.IntervalEnd != nil {
			updatedAt["end_at"] = opts.IntervalEnd.UTC().Format(time.RFC3339)
		}
		query["filter"] = map[string]interface{}{
			"date_time_filter": map[string]interface{}{
				"updated_at": updatedAt,
			},
		}
	}

	baseBody := map[string]interface{}{
		"location_ids": locationIDs,
		"limit":        maxPageSize,
		"query":        query,
	}
	return s.readSearch(ctx, "/v2/orders/search", "orders", baseBody, opts, results)
}

// Square caps customer sort to CREATED_AT, so paging stays creation-ordered
// while filtering by updated_at.
func (s *SquareSource) readCustomers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	query := map[string]interface{}{
		"sort": map[string]interface{}{"field": "CREATED_AT", "order": "ASC"},
	}
	if opts.IntervalStart != nil || opts.IntervalEnd != nil {
		updatedAt := map[string]interface{}{}
		if opts.IntervalStart != nil {
			updatedAt["start_at"] = opts.IntervalStart.UTC().Format(time.RFC3339)
		}
		if opts.IntervalEnd != nil {
			updatedAt["end_at"] = opts.IntervalEnd.UTC().Format(time.RFC3339)
		}
		query["filter"] = map[string]interface{}{"updated_at": updatedAt}
	}

	baseBody := map[string]interface{}{
		"limit": maxPageSize,
		"query": query,
	}
	return s.readSearch(ctx, "/v2/customers/search", "customers", baseBody, opts, results)
}

// team-members/search has no server-side updated_at filter, so we filter
// client-side. Soft-deletes appear as status="INACTIVE" with a bumped updated_at.
func (s *SquareSource) readTeamMembers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	baseBody := map[string]interface{}{"limit": maxPageSize}
	return s.readSearchFiltered(ctx, "/v2/team-members/search", "team_members", baseBody, "updated_at", opts, results)
}

// batch-retrieve has updated_after (server-side) but no upper bound, so end
// intervals are applied client-side on calculated_at.
func (s *SquareSource) readInventory(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	baseBody := map[string]interface{}{}
	if opts.IntervalStart != nil {
		baseBody["updated_after"] = opts.IntervalStart.UTC().Format(time.RFC3339)
	}
	endField := ""
	if opts.IntervalEnd != nil {
		endField = "calculated_at"
	}
	return s.readSearchFiltered(ctx, "/v2/inventory/counts/batch-retrieve", "counts", baseBody, endField, opts, results)
}

// include_deleted_objects is always set so tombstones (is_deleted=true) land on
// every run; merge then propagates deletes even on a full refresh or end-only
// bounded run. catalog/search has no end bound, so end is applied client-side.
func (s *SquareSource) readCatalog(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	baseBody := map[string]interface{}{
		"limit":                   maxPageSize,
		"include_deleted_objects": true,
	}
	if opts.IntervalStart != nil {
		baseBody["begin_time"] = opts.IntervalStart.UTC().Format(time.RFC3339)
	}
	endField := ""
	if opts.IntervalEnd != nil {
		endField = "updated_at"
	}
	return s.readSearchFiltered(ctx, "/v2/catalog/search", "objects", baseBody, endField, opts, results)
}

// The endpoint requires a location_id, so iterate every location. Locations are
// independent and fetched concurrently; the shared rate limiter throttles them.
func (s *SquareSource) readCashDrawerShifts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	locationIDs, err := s.listLocationIDs(ctx)
	if err != nil {
		return err
	}
	if len(locationIDs) == 0 {
		config.Debug("[SQUARE] no locations found; skipping cash_drawers")
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, locationWorkers)
	errCh := make(chan error, len(locationIDs))
	var wg sync.WaitGroup
	for _, loc := range locationIDs {
		sem <- struct{}{}
		wg.Add(1)
		go func(loc string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.readCashDrawerShiftsForLocation(ctx, loc, opts, results); err != nil {
				errCh <- err
				cancel()
			}
		}(loc)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// Square's CashDrawerShiftSummary doesn't include location_id in the response
// body, so inject it per row to keep the composite PK [id, location_id] populated.
func (s *SquareSource) readCashDrawerShiftsForLocation(ctx context.Context, loc string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	query := map[string]string{"location_id": loc, "limit": strconv.Itoa(maxPageSize)}
	return s.paginate(ctx, opts, results, "cash_drawer_shifts", func(cursor string) ([]map[string]interface{}, string, error) {
		req := s.client.R(ctx)
		if cursor == "" {
			req = req.SetQueryParams(query)
		} else {
			req = req.SetQueryParam("cursor", cursor)
		}
		resp, err := req.Get("/v2/cash-drawers/shifts")
		if err != nil {
			return nil, "", err
		}
		if !resp.IsSuccess() {
			return nil, "", fmt.Errorf("square API /v2/cash-drawers/shifts returned status %d: %s", resp.StatusCode(), resp.String())
		}
		items, next, err := decodeListResponse(resp.Body(), "cash_drawer_shifts")
		if err != nil {
			return nil, "", err
		}
		injectFieldIfMissing(items, "location_id", loc)
		return items, next, nil
	})
}

// Sets field to value on rows that don't already have it; existing values are
// preserved so we never override what Square actually sent.
func injectFieldIfMissing(items []map[string]interface{}, field string, value interface{}) {
	for _, item := range items {
		if _, exists := item[field]; !exists {
			item[field] = value
		}
	}
}

func (s *SquareSource) listLocationIDs(ctx context.Context) ([]string, error) {
	resp, err := s.client.R(ctx).Get("/v2/locations")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch locations: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("square API /v2/locations returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var payload struct {
		Locations []struct {
			ID string `json:"id"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(resp.Body(), &payload); err != nil {
		return nil, fmt.Errorf("failed to parse locations response: %w", err)
	}

	ids := make([]string, 0, len(payload.Locations))
	for _, loc := range payload.Locations {
		if loc.ID != "" {
			ids = append(ids, loc.ID)
		}
	}
	return ids, nil
}

// Rows with a missing or unparseable timestamp are kept rather than dropped.
func filterItemsByInterval(items []map[string]interface{}, field string, start, end *time.Time) []map[string]interface{} {
	if field == "" || (start == nil && end == nil) {
		return items
	}
	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		ts, ok := parseTimestamp(item[field])
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if start != nil && ts.Before(start.UTC()) {
			continue
		}
		if end != nil && !ts.Before(end.UTC()) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// Square emits both RFC3339 and RFC3339Nano (with/without fractional seconds).
func parseTimestamp(raw interface{}) (time.Time, bool) {
	s, ok := raw.(string)
	if !ok || s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func decodeListResponse(body []byte, key string) ([]map[string]interface{}, string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var raw map[string]interface{}
	if err := decoder.Decode(&raw); err != nil {
		return nil, "", err
	}

	cursor := ""
	if c, ok := raw["cursor"].(string); ok {
		cursor = c
	}

	arr, ok := raw[key].([]interface{})
	if !ok {
		return nil, cursor, nil
	}

	items := make([]map[string]interface{}, 0, len(arr))
	for _, v := range arr {
		if m, ok := v.(map[string]interface{}); ok {
			items = append(items, m)
		}
	}

	return items, cursor, nil
}
