package g2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://data.g2.com/api/v2"
	// G2 API allows 100 requests/second.
	rateLimit      = 80.0
	rateLimitBurst = 5
	maxPageSize    = 100
	maxPages       = 10000
	maxWorkers     = 5
)

var supportedTables = []string{
	"products",
	"my_products",
	"vendors",
	"categories",
	"category_features",
	"product_features",
	"buyer_intent",
	"competitors",
	"discussions",
	"downloads",
	"integration_reviews",
	"questions",
	"reviews",
	"screenshots",
	"videos",
}

type G2Source struct {
	client   *httpclient.Client
	apiToken string
}

func NewG2Source() *G2Source {
	return &G2Source{}
}

func (s *G2Source) HandlesIncrementality() bool {
	return true
}

func (s *G2Source) Schemes() []string {
	return []string{"g2"}
}

func (s *G2Source) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.apiToken = creds.apiToken

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(s.apiToken)),
		httpclient.WithHeader("Content-Type", "application/vnd.api+json"),
	)

	config.Debug("[G2] Connected successfully")
	return nil
}

func (s *G2Source) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

type g2Credentials struct {
	apiToken string
}

func parseURI(uri string) (g2Credentials, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return g2Credentials{}, fmt.Errorf("invalid g2 URI: %w", err)
	}

	if parsed.Scheme != "g2" {
		return g2Credentials{}, fmt.Errorf("invalid g2 URI: must start with g2://")
	}

	apiToken := parsed.Query().Get("api_token")
	if apiToken == "" {
		return g2Credentials{}, fmt.Errorf("api_token query parameter is required in g2 URI: g2://?api_token=<token>")
	}

	return g2Credentials{apiToken: apiToken}, nil
}

func (s *G2Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	incrementalKey := ""
	strategy := config.StrategyReplace
	var primaryKeys []string

	switch tableName {
	case "products":
		primaryKeys = []string{"id"}
		strategy = config.StrategyReplace
	case "my_products":
		primaryKeys = []string{"id"}
		strategy = config.StrategyReplace
	case "vendors":
		primaryKeys = []string{"id"}
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	case "categories":
		primaryKeys = []string{"id"}
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	case "category_features":
		primaryKeys = []string{"id"}
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	case "product_features":
		primaryKeys = []string{"id"}
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	case "buyer_intent":
		primaryKeys = []string{"id"}
		strategy = config.StrategyReplace
	case "competitors":
		primaryKeys = []string{"id"}
		strategy = config.StrategyReplace
	case "discussions":
		primaryKeys = []string{"id"}
		strategy = config.StrategyReplace
	case "downloads":
		primaryKeys = []string{"id"}
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	case "integration_reviews":
		primaryKeys = []string{"id"}
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	case "questions":
		primaryKeys = []string{"id"}
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	case "reviews":
		primaryKeys = []string{"id"}
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	case "screenshots":
		primaryKeys = []string{"id"}
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	case "videos":
		primaryKeys = []string{"id"}
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("g2 source does not have a predefined schema; schema inference is required")
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

func (s *G2Source) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "products":
			err = s.readProducts(ctx, opts, results)
		case "my_products":
			err = s.readMyProducts(ctx, opts, results)
		case "vendors":
			err = s.readVendors(ctx, opts, results)
		case "categories":
			err = s.readCategories(ctx, opts, results)
		case "category_features":
			err = s.readCategoryFeatures(ctx, opts, results)
		case "product_features":
			err = s.readProductFeatures(ctx, opts, results)
		case "buyer_intent":
			err = s.readBuyerIntent(ctx, opts, results)
		case "competitors":
			err = s.readCompetitors(ctx, opts, results)
		case "discussions":
			err = s.readDiscussions(ctx, opts, results)
		case "downloads":
			err = s.readDownloads(ctx, opts, results)
		case "integration_reviews":
			err = s.readIntegrationReviews(ctx, opts, results)
		case "questions":
			err = s.readQuestions(ctx, opts, results)
		case "reviews":
			err = s.readReviews(ctx, opts, results)
		case "screenshots":
			err = s.readScreenshots(ctx, opts, results)
		case "videos":
			err = s.readVideos(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

// flattenJSONAPIData converts JSON:API "data" array items into flat maps.
// Each item has "id", "type", and "attributes"; we merge "id" into attributes.
func flattenJSONAPIData(data []interface{}) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(data))
	for _, d := range data {
		raw, ok := d.(map[string]interface{})
		if !ok {
			continue
		}

		attrs, ok := raw["attributes"].(map[string]interface{})
		if !ok {
			attrs = make(map[string]interface{})
		}

		if id, ok := raw["id"]; ok {
			attrs["id"] = id
		}

		items = append(items, attrs)
	}
	return items
}

func filterByInterval(items []map[string]interface{}, field string, start, end *time.Time) []map[string]interface{} {
	if start == nil && end == nil {
		return items
	}

	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		ts, ok := item[field].(string)
		if !ok {
			filtered = append(filtered, item)
			continue
		}

		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05.000-07:00", ts)
			if err != nil {
				filtered = append(filtered, item)
				continue
			}
		}

		if start != nil && !t.After(*start) {
			continue
		}
		if end != nil && !t.Before(*end) {
			continue
		}

		filtered = append(filtered, item)
	}
	return filtered
}

// extractNextCursor extracts the cursor value from the "next" link in the response.
// G2 v2 API uses cursor-based pagination: links.next contains a URL with page[after]=<cursor>.
func extractNextCursor(body map[string]interface{}) string {
	links, ok := body["links"].(map[string]interface{})
	if !ok {
		return ""
	}

	next, ok := links["next"].(string)
	if !ok || next == "" {
		return ""
	}

	parsed, err := url.Parse(next)
	if err != nil {
		return ""
	}

	return parsed.Query().Get("page[after]")
}

// paginateAndSend handles cursor-based pagination for v2 endpoints.
func (s *G2Source) paginateAndSend(ctx context.Context, endpoint, label, intervalField string, opts source.ReadOptions, results chan<- source.RecordBatchResult, serverFilters map[string]string) error {
	totalProcessed := 0
	cursor := ""
	pageCount := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("page[size]", strconv.Itoa(maxPageSize))

		if cursor != "" {
			req.SetQueryParam("page[after]", cursor)
		}

		for k, v := range serverFilters {
			req.SetQueryParam(k, v)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("g2 API %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		var body map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		dataRaw, ok := body["data"].([]interface{})
		if !ok || len(dataRaw) == 0 {
			break
		}

		items := flattenJSONAPIData(dataRaw)

		if intervalField != "" && len(serverFilters) == 0 {
			items = filterByInterval(items, intervalField, opts.IntervalStart, opts.IntervalEnd)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for %s: %w", label, err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			totalProcessed += len(items)
		}

		nextCursor := extractNextCursor(body)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor

		pageCount++
		if pageCount >= maxPages {
			config.Debug("[G2] reached max page limit (%d) for %s, stopping", maxPages, label)
			break
		}
	}

	config.Debug("[G2] finished reading %s: %d total records", label, totalProcessed)
	return nil
}

func (s *G2Source) buildServerFilters(opts source.ReadOptions) map[string]string {
	filters := make(map[string]string)
	if opts.IntervalStart != nil {
		filters["filter[updated_at_gt]"] = opts.IntervalStart.Format(time.RFC3339)
	}
	if opts.IntervalEnd != nil {
		filters["filter[updated_at_lt]"] = opts.IntervalEnd.Format(time.RFC3339)
	}
	return filters
}

// buildServerFiltersPlural builds server-side filters using the plural "filters" key.
// The /products/features endpoint uses "filters[...]" instead of "filter[...]".
func (s *G2Source) buildServerFiltersPlural(opts source.ReadOptions) map[string]string {
	filters := make(map[string]string)
	if opts.IntervalStart != nil {
		filters["filters[updated_at_gt]"] = opts.IntervalStart.Format(time.RFC3339)
	}
	if opts.IntervalEnd != nil {
		filters["filters[updated_at_lt]"] = opts.IntervalEnd.Format(time.RFC3339)
	}
	return filters
}

func (s *G2Source) readVendors(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading vendors")
	return s.paginateAndSend(ctx, "/vendors", "vendors", "updated_at", opts, results, nil)
}

func (s *G2Source) readProducts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading products")
	return s.paginateAndSend(ctx, "/products", "products", "", opts, results, nil)
}

func (s *G2Source) readMyProducts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading my_products")
	return s.paginateAndSend(ctx, "/users/me/products", "my_products", "", opts, results, nil)
}

func (s *G2Source) readCategoryFeatures(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading category_features")
	filters := s.buildServerFilters(opts)
	return s.paginateAndSend(ctx, "/categories/features", "category_features", "updated_at", opts, results, filters)
}

func (s *G2Source) readProductFeatures(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading product_features")
	filters := s.buildServerFiltersPlural(opts)
	return s.paginateAndSend(ctx, "/products/features", "product_features", "updated_at", opts, results, filters)
}

func (s *G2Source) readCategories(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading categories")
	filters := s.buildServerFilters(opts)
	return s.paginateAndSend(ctx, "/categories", "categories", "updated_at", opts, results, filters)
}

func (s *G2Source) readQuestions(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading questions")
	filters := s.buildServerFilters(opts)
	return s.paginateAndSend(ctx, "/questions", "questions", "updated_at", opts, results, filters)
}

func (s *G2Source) readReviews(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading reviews")
	filters := s.buildServerFilters(opts)
	return s.readPerProduct(ctx, "reviews", opts, results, filters, "")
}

func (s *G2Source) readVideos(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading videos")
	return s.readPerProduct(ctx, "videos", opts, results, nil, "updated_at")
}

func (s *G2Source) readScreenshots(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading screenshots")
	filters := s.buildServerFilters(opts)
	return s.paginateAndSend(ctx, "/screenshots", "screenshots", "updated_at", opts, results, filters)
}

// readCompetitors fetches competitors per product using /products/{id}/competitors.
func (s *G2Source) readCompetitors(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading competitors")
	return s.readPerProduct(ctx, "competitors", opts, results, nil, "")
}

func (s *G2Source) readDiscussions(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading discussions")
	return s.readPerProduct(ctx, "discussions", opts, results, nil, "")
}

func (s *G2Source) readDownloads(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading downloads")
	filters := s.buildServerFilters(opts)
	return s.readPerProduct(ctx, "downloads", opts, results, filters, "")
}

func (s *G2Source) readIntegrationReviews(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading integration_reviews")
	filters := s.buildServerFilters(opts)
	return s.readPerProduct(ctx, "integration_reviews", opts, results, filters, "")
}

// readBuyerIntent fetches buyer intent data per product using /products/{id}/buyer_intent.
func (s *G2Source) readBuyerIntent(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[G2] reading buyer_intent")
	return s.readPerProduct(ctx, "buyer_intent", opts, results, nil, "")
}

// readPerProduct fetches a per-product resource in parallel across all products.
// serverFilters are query params for server-side filtering. clientFilterField is the field name for client-side interval filtering.
func (s *G2Source) readPerProduct(ctx context.Context, resource string, opts source.ReadOptions, results chan<- source.RecordBatchResult, serverFilters map[string]string, clientFilterField string) error {
	productIDs, err := s.getProductIDs(ctx)
	if err != nil {
		return err
	}

	if len(productIDs) == 0 {
		config.Debug("[G2] no products found, skipping %s", resource)
		return nil
	}

	var totalProcessed atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorkers)
	errCh := make(chan error, len(productIDs))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, productID := range productIDs {
		wg.Add(1)
		go func(pid string) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}

			count, err := s.readResourceForProduct(ctx, resource, pid, opts, results, serverFilters, clientFilterField)
			if err != nil {
				errCh <- err
				cancel()
				return
			}
			totalProcessed.Add(int64(count))
		}(productID)
	}

	wg.Wait()
	close(errCh)

	if err := <-errCh; err != nil {
		return err
	}

	config.Debug("[G2] finished reading %s: %d total records", resource, totalProcessed.Load())
	return nil
}

func (s *G2Source) readResourceForProduct(ctx context.Context, resource, productID string, opts source.ReadOptions, results chan<- source.RecordBatchResult, serverFilters map[string]string, clientFilterField string) (int, error) {
	endpoint := fmt.Sprintf("/products/%s/%s", productID, resource)
	cursor := ""
	pageCount := 0
	processed := 0

	for {
		select {
		case <-ctx.Done():
			return processed, ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("page[size]", strconv.Itoa(maxPageSize))

		if cursor != "" {
			req.SetQueryParam("page[after]", cursor)
		}

		for k, v := range serverFilters {
			req.SetQueryParam(k, v)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return processed, fmt.Errorf("failed to fetch %s for product %s: %w", resource, productID, err)
		}

		if !resp.IsSuccess() {
			return processed, fmt.Errorf("g2 API %s for product %s returned status %d: %s", resource, productID, resp.StatusCode(), resp.String())
		}

		var body map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return processed, fmt.Errorf("failed to parse %s response for product %s: %w", resource, productID, err)
		}

		dataRaw, ok := body["data"].([]interface{})
		if !ok || len(dataRaw) == 0 {
			break
		}

		items := flattenJSONAPIData(dataRaw)

		if clientFilterField != "" {
			items = filterByInterval(items, clientFilterField, opts.IntervalStart, opts.IntervalEnd)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return processed, fmt.Errorf("failed to build arrow record for %s: %w", resource, err)
			}

			select {
			case <-ctx.Done():
				return processed, ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			processed += len(items)
		}

		nextCursor := extractNextCursor(body)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor

		pageCount++
		if pageCount >= maxPages {
			config.Debug("[G2] reached max page limit (%d) for %s product %s, stopping", maxPages, resource, productID)
			break
		}
	}

	config.Debug("[G2] finished reading %s for product %s: %d records", resource, productID, processed)
	return processed, nil
}

func (s *G2Source) getProductIDs(ctx context.Context) ([]string, error) {
	var ids []string
	cursor := ""
	pageCount := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("page[size]", strconv.Itoa(maxPageSize))

		if cursor != "" {
			req.SetQueryParam("page[after]", cursor)
		}

		resp, err := req.Get("/products")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch products for ID lookup: %w", err)
		}

		if !resp.IsSuccess() {
			return nil, fmt.Errorf("g2 API /products returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var body map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return nil, fmt.Errorf("failed to parse products response: %w", err)
		}

		dataRaw, ok := body["data"].([]interface{})
		if !ok || len(dataRaw) == 0 {
			break
		}

		for _, d := range dataRaw {
			raw, ok := d.(map[string]interface{})
			if !ok {
				continue
			}
			if id, ok := raw["id"].(string); ok {
				ids = append(ids, id)
			}
		}

		nextCursor := extractNextCursor(body)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor

		pageCount++
		if pageCount >= maxPages {
			config.Debug("[G2] reached max page limit (%d) for product ID lookup, stopping", maxPages)
			break
		}
	}

	config.Debug("[G2] found %d products", len(ids))
	return ids, nil
}
