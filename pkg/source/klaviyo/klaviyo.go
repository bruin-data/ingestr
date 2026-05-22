package klaviyo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
	baseURL     = "https://a.klaviyo.com/api"
	apiRevision = "2024-07-15"

	// Klaviyo API rate limits per tier (steady req/min at 80% safety margin).
	// https://developers.klaviyo.com/en/docs/rate_limits_and_error_handling
	rateLimitXL      = 46.67 // XL: 3500/min → 3500*0.8/60 ≈ 46.67 req/s
	rateLimitXLBurst = 280   // XL burst: 350/s → 350*0.8 = 280
	rateLimitL       = 9.33  // L: 700/min → 700*0.8/60 ≈ 9.33 req/s
	rateLimitLBurst  = 60    // L burst: 75/s → 75*0.8 = 60
	rateLimitM       = 2.0   // M: 150/min → 150*0.8/60 = 2.0 req/s
	rateLimitMBurst  = 8     // M burst: 10/s → 10*0.8 = 8
	rateLimitS       = 0.8   // S: 60/min → 60*0.8/60 = 0.8 req/s
	rateLimitSBurst  = 2     // S burst: 3/s → 3*0.8 ≈ 2

	defaultParallelism = 4
)

var supportedTables = []string{
	"events",
	"profiles",
	"campaigns",
	"metrics",
	"tags",
	"coupons",
	"catalog-variants",
	"catalog-categories",
	"catalog-items",
	"flows",
	"lists",
	"images",
	"segments",
	"forms",
	"templates",
}

type KlaviyoSource struct {
	apiKey   string
	clientXL *httpclient.Client // events, catalog-variants, catalog-categories, catalog-items
	clientL  *httpclient.Client // profiles, coupons, lists, segments, templates
	clientM  *httpclient.Client // campaigns, metrics, images
	clientS  *httpclient.Client // tags, flows, forms
}

func NewKlaviyoSource() *KlaviyoSource {
	return &KlaviyoSource{}
}

func (s *KlaviyoSource) HandlesIncrementality() bool {
	return true
}

func (s *KlaviyoSource) Schemes() []string {
	return []string{"klaviyo"}
}

func (s *KlaviyoSource) newClient(rl float64, burst int) *httpclient.Client {
	return httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rl, burst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Authorization", fmt.Sprintf("Klaviyo-API-Key %s", s.apiKey)),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithHeader("revision", apiRevision),
	)
}

func (s *KlaviyoSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey

	s.clientXL = s.newClient(rateLimitXL, rateLimitXLBurst)
	s.clientL = s.newClient(rateLimitL, rateLimitLBurst)
	s.clientM = s.newClient(rateLimitM, rateLimitMBurst)
	s.clientS = s.newClient(rateLimitS, rateLimitSBurst)

	config.Debug("[KLAVIYO] Connected successfully")
	return nil
}

func (s *KlaviyoSource) Close(ctx context.Context) error {
	var closeErr error
	for _, c := range []*httpclient.Client{s.clientXL, s.clientL, s.clientM, s.clientS} {
		if c != nil {
			if err := c.Close(); err != nil && closeErr == nil {
				closeErr = err
			}
		}
	}
	return closeErr
}

func parseURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "klaviyo://") {
		return "", fmt.Errorf("invalid klaviyo URI: must start with klaviyo://")
	}

	rest := strings.TrimPrefix(uri, "klaviyo://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in klaviyo URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse klaviyo URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in klaviyo URI")
	}

	return apiKey, nil
}

func (s *KlaviyoSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	incrementalKey := ""
	strategy := config.StrategyMerge

	switch tableName {
	case "events":
		incrementalKey = "datetime"
	case "campaigns", "forms", "images":
		incrementalKey = "updated_at"
	case "tags", "coupons":
		strategy = config.StrategyReplace
	default:
		incrementalKey = "updated"
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("klaviyo source does not have a predefined schema; schema inference is required")
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

func (s *KlaviyoSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "events":
			err = s.readEvents(ctx, opts, results)
		case "profiles":
			err = s.readProfiles(ctx, opts, results)
		case "campaigns":
			err = s.readCampaigns(ctx, opts, results)
		case "metrics":
			err = s.readMetrics(ctx, opts, results)
		case "tags":
			err = s.readTags(ctx, opts, results)
		case "coupons":
			err = s.readCoupons(ctx, opts, results)
		case "catalog-variants":
			err = s.readCatalogVariants(ctx, opts, results)
		case "catalog-categories":
			err = s.readCatalogCategories(ctx, opts, results)
		case "catalog-items":
			err = s.readCatalogItems(ctx, opts, results)
		case "flows":
			err = s.readFlows(ctx, opts, results)
		case "lists":
			err = s.readLists(ctx, opts, results)
		case "images":
			err = s.readImages(ctx, opts, results)
		case "segments":
			err = s.readSegments(ctx, opts, results)
		case "forms":
			err = s.readForms(ctx, opts, results)
		case "templates":
			err = s.readTemplates(ctx, opts, results)
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

type klaviyoResponse struct {
	Data  []map[string]interface{} `json:"data"`
	Links map[string]interface{}   `json:"links"`
}

func flattenAttributes(items []map[string]interface{}) []map[string]interface{} {
	for _, item := range items {
		attrs, ok := item["attributes"].(map[string]interface{})
		if !ok {
			continue
		}
		for k, v := range attrs {
			item[k] = v
		}
		delete(item, "attributes")
	}
	return items
}

func paginateAndSend(ctx context.Context, client *httpclient.Client, initialURL, label string, flat bool, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	currentURL := initialURL
	totalProcessed := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.R(ctx).Get(currentURL)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("klaviyo %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		var body klaviyoResponse
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		if len(body.Data) == 0 {
			break
		}

		items := body.Data
		if flat {
			items = flattenAttributes(items)
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
			config.Debug("[KLAVIYO] %s: sent %d records (total: %d)", label, len(items), totalProcessed)
		}

		nextURL, ok := body.Links["next"].(string)
		if !ok || nextURL == "" {
			break
		}
		currentURL = nextURL
	}

	config.Debug("[KLAVIYO] finished reading %s: %d total records", label, totalProcessed)
	return nil
}

func paginateWithClientFilter(ctx context.Context, client *httpclient.Client, endpoint, label, dateField string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	currentURL := endpoint
	totalProcessed := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.R(ctx).Get(currentURL)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("klaviyo %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		var body klaviyoResponse
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		if len(body.Data) == 0 {
			break
		}

		items := flattenAttributes(body.Data)

		var filtered []map[string]interface{}
		for _, item := range items {
			if dateField != "" && opts.IntervalStart != nil {
				if ts, ok := item[dateField].(string); ok {
					if t, err := time.Parse(time.RFC3339, ts); err == nil {
						if !t.After(*opts.IntervalStart) {
							continue
						}
					}
				}
			}
			if dateField != "" && opts.IntervalEnd != nil {
				if ts, ok := item[dateField].(string); ok {
					if t, err := time.Parse(time.RFC3339, ts); err == nil {
						if t.After(*opts.IntervalEnd) {
							continue
						}
					}
				}
			}
			filtered = append(filtered, item)
		}

		if len(filtered) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(filtered, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for %s: %w", label, err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			totalProcessed += len(filtered)
			config.Debug("[KLAVIYO] %s: sent %d records (total: %d)", label, len(filtered), totalProcessed)
		}

		nextURL, ok := body.Links["next"].(string)
		if !ok || nextURL == "" {
			break
		}
		currentURL = nextURL
	}

	config.Debug("[KLAVIYO] finished reading %s: %d total records", label, totalProcessed)
	return nil
}

func formatTime(t *time.Time) string {
	return t.Format(time.RFC3339)
}

func buildFilterURL(endpoint string, params map[string]string) string {
	if len(params) == 0 {
		return endpoint
	}

	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	return endpoint + "?" + values.Encode()
}

func paginateWithServerFilter(ctx context.Context, client *httpclient.Client, endpoint, label, dateField, startOp, sortField string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	params := map[string]string{"sort": sortField}

	var filters []string
	if opts.IntervalStart != nil {
		filters = append(filters, fmt.Sprintf("%s(%s,%s)", startOp, dateField, formatTime(opts.IntervalStart)))
	}
	if opts.IntervalEnd != nil {
		filters = append(filters, fmt.Sprintf("less-than(%s,%s)", dateField, formatTime(opts.IntervalEnd)))
	}
	if len(filters) > 0 {
		params["filter"] = fmt.Sprintf("and(%s)", strings.Join(filters, ","))
	}

	return paginateAndSend(ctx, client, buildFilterURL(endpoint, params), label, true, opts, results)
}

type timeWindow struct {
	start time.Time
	end   time.Time
}

func splitTimeRange(start, end time.Time, n int) []timeWindow {
	duration := end.Sub(start)
	if duration <= 0 || n <= 1 {
		return []timeWindow{{start: start, end: end}}
	}

	windowSize := duration / time.Duration(n)
	if windowSize < time.Hour {
		return []timeWindow{{start: start, end: end}}
	}

	windows := make([]timeWindow, 0, n)
	current := start
	for i := 0; i < n; i++ {
		windowEnd := current.Add(windowSize)
		if i == n-1 {
			windowEnd = end
		}
		windows = append(windows, timeWindow{start: current, end: windowEnd})
		current = windowEnd
	}
	return windows
}

type parallelReadFn func(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error

func readParallel(ctx context.Context, opts source.ReadOptions, readFn parallelReadFn, label string, results chan<- source.RecordBatchResult) error {
	if opts.IntervalStart == nil || opts.IntervalEnd == nil {
		return readFn(ctx, opts, results)
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}

	windows := splitTimeRange(*opts.IntervalStart, *opts.IntervalEnd, parallelism)
	if len(windows) <= 1 {
		return readFn(ctx, opts, results)
	}

	config.Debug("[KLAVIYO] Parallel sync for %s: %d workers from %s to %s", label, len(windows), opts.IntervalStart.Format(time.RFC3339), opts.IntervalEnd.Format(time.RFC3339))

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	var wg sync.WaitGroup
	for i, w := range windows {
		wg.Add(1)
		go func(idx int, window timeWindow) {
			defer wg.Done()

			select {
			case <-workerCtx.Done():
				return
			default:
			}

			windowOpts := opts
			windowOpts.IntervalStart = &window.start
			windowOpts.IntervalEnd = &window.end

			config.Debug("[KLAVIYO] Worker %d: %s [%s, %s)", idx, label, window.start.Format(time.RFC3339), window.end.Format(time.RFC3339))
			if err := readFn(workerCtx, windowOpts, results); err != nil {
				cancelWorkers()
				results <- source.RecordBatchResult{Err: err}
			}
		}(i, w)
	}
	wg.Wait()
	return nil
}

// XL tier: events, catalog-variants, catalog-categories, catalog-items

func (s *KlaviyoSource) readEvents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading events")
	return readParallel(ctx, opts, s.readEventsWindow, "events", results)
}

func (s *KlaviyoSource) readEventsWindow(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return paginateWithServerFilter(ctx, s.clientXL, "/events/", "events", "datetime", "greater-or-equal", "-datetime", opts, results)
}

func (s *KlaviyoSource) readCatalogVariants(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading catalog-variants")
	return paginateWithClientFilter(ctx, s.clientXL, "/catalog-variants", "catalog-variants", "updated", opts, results)
}

func (s *KlaviyoSource) readCatalogCategories(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading catalog-categories")
	return paginateWithClientFilter(ctx, s.clientXL, "/catalog-categories", "catalog-categories", "updated", opts, results)
}

func (s *KlaviyoSource) readCatalogItems(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading catalog-items")
	return paginateWithClientFilter(ctx, s.clientXL, "/catalog-items", "catalog-items", "updated", opts, results)
}

// L tier: profiles, coupons, lists, segments, templates

func (s *KlaviyoSource) readProfiles(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading profiles")
	return readParallel(ctx, opts, s.readProfilesWindow, "profiles", results)
}

func (s *KlaviyoSource) readProfilesWindow(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return paginateWithServerFilter(ctx, s.clientL, "/profiles/", "profiles", "updated", "greater-than", "updated", opts, results)
}

func (s *KlaviyoSource) readCoupons(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading coupons")
	return paginateAndSend(ctx, s.clientL, "/coupons", "coupons", false, opts, results)
}

func (s *KlaviyoSource) readLists(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading lists")
	params := map[string]string{"sort": "-updated"}
	if opts.IntervalStart != nil {
		params["filter"] = fmt.Sprintf("greater-than(updated,%s)", formatTime(opts.IntervalStart))
	}
	return paginateAndSend(ctx, s.clientL, buildFilterURL("/lists/", params), "lists", true, opts, results)
}

func (s *KlaviyoSource) readSegments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading segments")
	params := map[string]string{"sort": "-updated"}
	if opts.IntervalStart != nil {
		params["filter"] = fmt.Sprintf("greater-than(updated,%s)", formatTime(opts.IntervalStart))
	}
	return paginateAndSend(ctx, s.clientL, buildFilterURL("/segments/", params), "segments", true, opts, results)
}

func (s *KlaviyoSource) readTemplates(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading templates")
	return readParallel(ctx, opts, s.readTemplatesWindow, "templates", results)
}

func (s *KlaviyoSource) readTemplatesWindow(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return paginateWithServerFilter(ctx, s.clientL, "/templates/", "templates", "updated", "greater-or-equal", "-updated", opts, results)
}

// M tier: campaigns, metrics, images

func (s *KlaviyoSource) readCampaigns(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading campaigns")

	for _, channelType := range []string{"email", "sms"} {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := map[string]string{"sort": "updated_at"}

		var filters []string
		filters = append(filters, fmt.Sprintf("equals(messages.channel,'%s')", channelType))
		if opts.IntervalStart != nil {
			filters = append(filters, fmt.Sprintf("greater-or-equal(updated_at,%s)", formatTime(opts.IntervalStart)))
		}
		if opts.IntervalEnd != nil {
			filters = append(filters, fmt.Sprintf("less-than(updated_at,%s)", formatTime(opts.IntervalEnd)))
		}
		params["filter"] = fmt.Sprintf("and(%s)", strings.Join(filters, ","))

		currentURL := buildFilterURL("/campaigns/", params)
		totalProcessed := 0

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			resp, err := s.clientM.R(ctx).Get(currentURL)
			if err != nil {
				return fmt.Errorf("failed to fetch campaigns (%s): %w", channelType, err)
			}
			if !resp.IsSuccess() {
				return fmt.Errorf("klaviyo campaigns (%s) returned status %d: %s", channelType, resp.StatusCode(), resp.String())
			}

			var body klaviyoResponse
			if err := jsonUseNumber(resp.Body(), &body); err != nil {
				return fmt.Errorf("failed to parse campaigns (%s) response: %w", channelType, err)
			}

			if len(body.Data) == 0 {
				break
			}

			items := flattenAttributes(body.Data)
			for _, item := range items {
				item["campaign_type"] = channelType
			}

			if len(items) > 0 {
				record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
				if err != nil {
					return fmt.Errorf("failed to build arrow record for campaigns (%s): %w", channelType, err)
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case results <- source.RecordBatchResult{Batch: record}:
				}

				totalProcessed += len(items)
				config.Debug("[KLAVIYO] campaigns (%s): sent %d records (total: %d)", channelType, len(items), totalProcessed)
			}

			nextURL, ok := body.Links["next"].(string)
			if !ok || nextURL == "" {
				break
			}
			currentURL = nextURL
		}
	}

	return nil
}

func (s *KlaviyoSource) readMetrics(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading metrics")
	return paginateWithClientFilter(ctx, s.clientM, "/metrics", "metrics", "updated", opts, results)
}

func (s *KlaviyoSource) readImages(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading images")
	return paginateWithServerFilter(ctx, s.clientM, "/images/", "images", "updated_at", "greater-or-equal", "-updated_at", opts, results)
}

// S tier: tags, flows, forms

func (s *KlaviyoSource) readTags(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading tags")
	return paginateAndSend(ctx, s.clientS, "/tags", "tags", false, opts, results)
}

func (s *KlaviyoSource) readFlows(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading flows")
	return paginateWithServerFilter(ctx, s.clientS, "/flows/", "flows", "updated", "greater-or-equal", "-updated", opts, results)
}

func (s *KlaviyoSource) readForms(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[KLAVIYO] reading forms")
	return paginateWithServerFilter(ctx, s.clientS, "/forms/", "forms", "updated_at", "greater-or-equal", "-updated_at", opts, results)
}
