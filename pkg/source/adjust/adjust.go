package adjust

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
)

const (
	baseURL        = "https://automate.adjust.com/reports-service"
	rateLimit      = 10
	rateLimitBurst = 5

	// defaultAttributionTypes pins campaigns/creatives to ingestr's historical
	// behaviour; Adjust's API-side default changes (2026-07-13) to include all types.
	defaultAttributionTypes = "click,engaged_ad"
)

var supportedTables = []string{
	"events",
	"campaigns",
	"creatives",
}

type AdjustSource struct {
	apiKey       string
	lookBackDays string
	client       *ingestrhttp.Client
}

func NewAdjustSource() *AdjustSource {
	return &AdjustSource{}
}

func (s *AdjustSource) HandlesIncrementality() bool {
	return true
}

func (s *AdjustSource) Schemes() []string {
	return []string{"adjust"}
}

func (s *AdjustSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseAdjustURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = creds.apiKey
	s.lookBackDays = creds.lookBackDays

	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(1000*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth(s.apiKey)),
	)
	config.Debug("[ADJUST] Connected successfully")
	return nil
}

func (s *AdjustSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// adjustParams is the URL-style query-parameter form of the source table and
// the single source of truth for which parameters are accepted.
type adjustParams struct {
	AppToken         []string `mapstructure:"app_token"`
	AttributionTypes []string `mapstructure:"attribution_types"`
}

// parseTableSpec parses a source table in URL-style form ("creatives?app_token=abc&attribution_types=click")
// or the legacy "creatives:<app_token>" colon form (app token only); custom tables are returned verbatim.
func parseTableSpec(table string) (baseName, appTokens, attributionTypes string, err error) {
	if strings.HasPrefix(table, "custom:") {
		return table, "", "", nil
	}

	var p adjustParams
	path, hasParams, err := tablespec.Parse(table, &p, tablespec.WithListSeparator(","))
	if err != nil {
		return "", "", "", err
	}
	if hasParams {
		return path, strings.Join(p.AppToken, ","), strings.Join(p.AttributionTypes, ","), nil
	}

	parts := strings.SplitN(table, ":", 2)
	baseName = parts[0]
	if len(parts) == 2 {
		appTokens = parts[1]
	}
	return baseName, appTokens, "", nil
}

// resolveAttributionTypes returns the attribution_types to send (empty = let the
// API decide). DEPRECATED(2026-07-13): to adopt Adjust's API default, change the
// pinned return below to `return ""`; callers already skip the param when empty.
func resolveAttributionTypes(attributionTypes string) string {
	if attributionTypes == "" {
		return defaultAttributionTypes
	}
	return attributionTypes
}

func (s *AdjustSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName, appTokens, attributionTypes, err := parseTableSpec(req.Name)
	if err != nil {
		return nil, err
	}

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	if attributionTypes != "" && tableName != "campaigns" && tableName != "creatives" {
		return nil, fmt.Errorf("attribution_types is not supported for the %q table; use it on campaigns or creatives (for custom tables, pass it in the filters section)", tableName)
	}

	var primaryKeys []string
	var mergeKey string
	strategy := config.StrategyReplace

	switch {
	case tableName == "events":
		primaryKeys = []string{"id"}
		strategy = config.StrategyReplace
	case tableName == "campaigns":
		primaryKeys = defaultPrimaryKeys
		mergeKey = "day"
		strategy = config.StrategyMerge
	case tableName == "creatives":
		primaryKeys = creativePrimaryKeys
		mergeKey = "day"
		strategy = config.StrategyMerge
	case strings.HasPrefix(tableName, "custom:"):
		dims, _, _, parseErr := parseCustomTable(tableName)
		if parseErr != nil {
			return nil, parseErr
		}
		primaryKeys = strings.Split(dims, ",")
		strategy = config.StrategyDeleteInsert

		dimSet := make(map[string]bool, len(primaryKeys))
		for _, d := range primaryKeys {
			dimSet[d] = true
		}
		for _, req := range requiredCustomDimensions {
			if dimSet[req] {
				mergeKey = req
				break
			}
		}
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: mergeKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("adjust source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, appTokens, attributionTypes, opts)
		},
	}, nil
}

func (s *AdjustSource) read(ctx context.Context, table string, appTokens, attributionTypes string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, source.RecordBatchBufferSize(opts, 1))

	go func() {
		defer close(results)

		var err error
		switch {
		case table == "events":
			err = s.readEvents(ctx, appTokens, opts, results)
		case table == "campaigns":
			err = s.readCampaigns(ctx, appTokens, attributionTypes, opts, results)
		case table == "creatives":
			err = s.readCreatives(ctx, appTokens, attributionTypes, opts, results)
		case strings.HasPrefix(table, "custom:"):
			err = s.readCustom(ctx, table, appTokens, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func isValidTable(table string) bool {
	if strings.HasPrefix(table, "custom:") {
		return true
	}
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

type adjustCredentials struct {
	apiKey       string
	lookBackDays string
}

func parseAdjustURI(uri string) (adjustCredentials, error) {
	if !strings.HasPrefix(uri, "adjust://") {
		return adjustCredentials{}, fmt.Errorf("invalid adjust URI: must start with adjust://")
	}

	rest := strings.TrimPrefix(uri, "adjust://")
	parts := strings.SplitN(rest, "?", 2)

	if len(parts) < 2 {
		return adjustCredentials{}, fmt.Errorf("adjust URI must include query parameters (adjust://?api_key=...)")
	}

	values, err := url.ParseQuery(parts[1])
	if err != nil {
		return adjustCredentials{}, fmt.Errorf("failed to parse adjust URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return adjustCredentials{}, fmt.Errorf("api_key is required in adjust URI (adjust://?api_key=...)")
	}

	return adjustCredentials{
		apiKey:       apiKey,
		lookBackDays: values.Get("lookback_days"),
	}, nil
}

func (s *AdjustSource) readEvents(ctx context.Context, appTokens string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ADJUST] Fetching events")

	req := s.client.R(ctx)
	if appTokens != "" {
		req.SetQueryParam("app_token__in", appTokens)
	}

	resp, err := req.Get("events")
	if err != nil {
		return fmt.Errorf("failed to fetch events: %w", err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("failed to fetch events: status %d: %s", resp.StatusCode(), resp.String())
	}

	var items []map[string]interface{}
	if err := resp.JSON(&items); err != nil {
		return fmt.Errorf("failed to parse events response: %w", err)
	}

	if len(items) == 0 {
		config.Debug("[ADJUST] No events found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert events to Arrow: %w", err)
	}

	config.Debug("[ADJUST] Sending %d events", len(items))
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

var defaultPrimaryKeys = []string{
	"campaign", "day", "app", "store_type", "channel", "country",
}

var defaultDimensions = []string{
	"campaign", "day", "app", "app_token", "store_type", "channel", "country",
}

var revenueCohortDays = []int{0, 1, 3, 7, 14, 21, 30, 60, 90, 120}

var revenueMetricPrefixes = []string{
	"all_revenue_total_d",
	"ad_revenue_total_d",
	"revenue_total_d",
}

var defaultMetrics = buildDefaultMetrics()

func buildDefaultMetrics() []string {
	metrics := []string{"installs", "network_cost"}
	for _, day := range revenueCohortDays {
		for _, prefix := range revenueMetricPrefixes {
			metrics = append(metrics, fmt.Sprintf("%s%d", prefix, day))
		}
	}
	return metrics
}

func (s *AdjustSource) readCampaigns(ctx context.Context, appTokens, attributionTypes string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	params := make(map[string]string)

	if at := resolveAttributionTypes(attributionTypes); at != "" {
		params["attribution_types"] = at
	}

	if appTokens != "" {
		params["app_token__in"] = appTokens
	}

	return s.readReport(ctx, "campaigns", strings.Join(defaultDimensions, ","), strings.Join(defaultMetrics, ","), params, opts, results)
}

var creativePrimaryKeys = []string{"campaign", "day", "app", "store_type", "channel", "country", "adgroup", "creative"}

var creativeDimensions = []string{"campaign", "day", "app", "app_token", "store_type", "channel", "country", "adgroup", "creative"}

func (s *AdjustSource) readCreatives(ctx context.Context, appTokens, attributionTypes string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	params := make(map[string]string)

	if at := resolveAttributionTypes(attributionTypes); at != "" {
		params["attribution_types"] = at
	}

	if appTokens != "" {
		params["app_token__in"] = appTokens
	}

	return s.readReport(ctx, "creatives", strings.Join(creativeDimensions, ","), strings.Join(defaultMetrics, ","), params, opts, results)
}

func (s *AdjustSource) readCustom(ctx context.Context, table string, appTokens string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	dimensions, metrics, filters, err := parseCustomTable(table)
	if err != nil {
		return err
	}

	if appTokens != "" {
		if filters == nil {
			filters = make(map[string]string)
		}
		filters["app_token__in"] = appTokens
	}

	return s.readReport(ctx, "custom report", dimensions, metrics, filters, opts, results)
}

func (s *AdjustSource) readReport(ctx context.Context, name, dimensions, metrics string, params map[string]string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	datePeriod, err := s.buildDatePeriod(&opts)
	if err != nil {
		return fmt.Errorf("failed to build date period for %s: %w", name, err)
	}

	datePeriods := []string{datePeriod}
	if hasDailyDimension(dimensions) {
		datePeriods, err = splitDatePeriodByDay(datePeriod)
		if err != nil {
			return fmt.Errorf("failed to split date period for %s: %w", name, err)
		}
	}

	workerCount := reportWorkerCount(opts.Parallelism, len(datePeriods))
	config.Debug("[ADJUST] Fetching %s in %d date window(s) with %d worker(s)", name, len(datePeriods), workerCount)
	cols := buildTypeHintColumns(dimensions, metrics)

	type periodResult struct {
		period   string
		record   arrow.RecordBatch
		rowCount int
		err      error
	}

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	periods := make(chan string)
	fetched := make(chan periodResult)

	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-workerCtx.Done():
					return
				case period, ok := <-periods:
					if !ok {
						return
					}

					record, rowCount, err := s.fetchReportPeriod(workerCtx, name, dimensions, metrics, period, params, cols, opts.ExcludeColumns)
					result := periodResult{period: period, record: record, rowCount: rowCount, err: err}
					select {
					case fetched <- result:
					case <-workerCtx.Done():
						if record != nil {
							record.Release()
						}
						return
					}
					if err != nil {
						return
					}
				}
			}
		}()
	}

	go func() {
		defer close(periods)
		for _, period := range datePeriods {
			select {
			case periods <- period:
			case <-workerCtx.Done():
				return
			}
		}
	}()

	go func() {
		workers.Wait()
		close(fetched)
	}()

	totalRows := 0
	var firstErr error
	for result := range fetched {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
				cancelWorkers()
			}
			continue
		}
		if result.record == nil {
			continue
		}
		if firstErr != nil {
			result.record.Release()
			continue
		}

		totalRows += result.rowCount
		config.Debug("[ADJUST] Sending %d %s rows for %s", result.rowCount, name, result.period)
		select {
		case results <- source.RecordBatchResult{Batch: result.record}:
		case <-ctx.Done():
			result.record.Release()
			if firstErr == nil {
				firstErr = ctx.Err()
				cancelWorkers()
			}
		}
	}

	if firstErr == nil && ctx.Err() != nil {
		firstErr = ctx.Err()
	}
	if firstErr != nil {
		return firstErr
	}
	if totalRows == 0 {
		config.Debug("[ADJUST] No %s data found", name)
	}
	return nil
}

func reportWorkerCount(parallelism, periodCount int) int {
	if parallelism <= 0 {
		parallelism = config.DefaultExtractParallelism
	}
	return min(parallelism, periodCount)
}

func (s *AdjustSource) fetchReportPeriod(ctx context.Context, name, dimensions, metrics, datePeriod string, params map[string]string, cols []schema.Column, excludeColumns []string) (arrow.RecordBatch, int, error) {
	req := s.client.R(ctx).
		SetQueryParam("dimensions", dimensions).
		SetQueryParam("metrics", metrics).
		SetQueryParam("date_period", datePeriod)

	for key, value := range params {
		req.SetQueryParam(key, value)
	}

	resp, err := req.Get("report")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch %s for %s: %w", name, datePeriod, err)
	}

	if !resp.IsSuccess() {
		return nil, 0, fmt.Errorf("failed to fetch %s for %s: status %d: %s", name, datePeriod, resp.StatusCode(), resp.String())
	}

	var result struct {
		Rows []map[string]interface{} `json:"rows"`
	}
	if err := resp.JSON(&result); err != nil {
		return nil, 0, fmt.Errorf("failed to parse %s response for %s: %w", name, datePeriod, err)
	}

	if len(result.Rows) == 0 {
		return nil, 0, nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(result.Rows, cols, excludeColumns)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to convert %s to Arrow for %s: %w", name, datePeriod, err)
	}
	return record, len(result.Rows), nil
}

var requiredCustomDimensions = []string{
	"hour", "day", "week", "month", "quarter", "year",
}

var knownTypeHints = map[string]schema.DataType{
	"hour":         schema.TypeTimestampTZ,
	"day":          schema.TypeDate,
	"week":         schema.TypeString,
	"month":        schema.TypeString,
	"quarter":      schema.TypeString,
	"year":         schema.TypeString,
	"campaign":     schema.TypeString,
	"app":          schema.TypeString,
	"app_token":    schema.TypeString,
	"store_type":   schema.TypeString,
	"channel":      schema.TypeString,
	"country":      schema.TypeString,
	"adgroup":      schema.TypeString,
	"creative":     schema.TypeString,
	"installs":     schema.TypeInt64,
	"clicks":       schema.TypeInt64,
	"cost":         schema.TypeDecimal,
	"network_cost": schema.TypeDecimal,
	"impressions":  schema.TypeInt64,
	"ad_revenue":   schema.TypeDecimal,
	"all_revenue":  schema.TypeDecimal,
}

func lookupTypeHint(name string) (schema.DataType, bool) {
	if dt, ok := knownTypeHints[name]; ok {
		return dt, true
	}
	for _, prefix := range revenueMetricPrefixes {
		if suffix, ok := strings.CutPrefix(name, prefix); ok && isCohortDaySuffix(suffix) {
			return schema.TypeDecimal, true
		}
	}
	return schema.TypeUnknown, false
}

func isCohortDaySuffix(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func typeHintColumn(name string) (schema.Column, bool) {
	dt, ok := lookupTypeHint(name)
	if !ok {
		return schema.Column{}, false
	}
	col := schema.Column{Name: name, DataType: dt, Nullable: true}
	if dt == schema.TypeDecimal {
		col.Precision = 38
		col.Scale = 9
	}
	return col, true
}

func buildTypeHintColumns(dimensions, metrics string) []schema.Column {
	var cols []schema.Column
	for _, name := range strings.Split(dimensions, ",") {
		if col, ok := typeHintColumn(name); ok {
			cols = append(cols, col)
		}
	}
	for _, name := range strings.Split(metrics, ",") {
		if col, ok := typeHintColumn(name); ok {
			cols = append(cols, col)
		}
	}
	return cols
}

func parseCustomTable(table string) (dimensions, metrics string, filters map[string]string, err error) {
	parts := strings.SplitN(table, ":", 4)
	if len(parts) != 3 && len(parts) != 4 {
		return "", "", nil, fmt.Errorf("invalid custom table format: expected custom:<dimensions>:<metrics> or custom:<dimensions>:<metrics>:<filters>, got %q", table)
	}

	dimensions = parts[1]
	metrics = parts[2]

	if dimensions == "" {
		return "", "", nil, fmt.Errorf("dimensions cannot be empty in custom table")
	}
	if metrics == "" {
		return "", "", nil, fmt.Errorf("metrics cannot be empty in custom table")
	}

	dims := strings.Split(dimensions, ",")
	hasRequired := false
	for _, d := range dims {
		for _, req := range requiredCustomDimensions {
			if d == req {
				hasRequired = true
				break
			}
		}
		if hasRequired {
			break
		}
	}
	if !hasRequired {
		return "", "", nil, fmt.Errorf("at least one of the required dimensions is missing for custom Adjust report: %v", requiredCustomDimensions)
	}

	if len(parts) == 4 && parts[3] != "" {
		filters = parseFilters(parts[3])
	}

	return dimensions, metrics, filters, nil
}

// parseFilters parses a filter string like "key1=value1,value2,key2=value3"
// into a map where each key maps to its comma-separated values.
// Items with "=" start a new key; items without "=" are additional values for the current key.
func parseFilters(raw string) map[string]string {
	result := make(map[string]string)
	var currentKey string

	for _, item := range strings.Split(raw, ",") {
		if idx := strings.Index(item, "="); idx >= 0 {
			currentKey = item[:idx]
			result[currentKey] = item[idx+1:]
		} else if currentKey != "" {
			result[currentKey] = result[currentKey] + "," + item
		}
	}

	return result
}

func hasDailyDimension(dimensions string) bool {
	for _, dimension := range strings.Split(dimensions, ",") {
		if dimension == "day" || dimension == "hour" {
			return true
		}
	}
	return false
}

func splitDatePeriodByDay(datePeriod string) ([]string, error) {
	startText, endText, ok := strings.Cut(datePeriod, ":")
	if !ok {
		return nil, fmt.Errorf("invalid date period %q", datePeriod)
	}

	start, err := time.Parse("2006-01-02", startText)
	if err != nil {
		return nil, fmt.Errorf("invalid date period start %q: %w", startText, err)
	}
	end, err := time.Parse("2006-01-02", endText)
	if err != nil {
		return nil, fmt.Errorf("invalid date period end %q: %w", endText, err)
	}
	if start.After(end) {
		return nil, fmt.Errorf("date period start %s must not be after end %s", startText, endText)
	}

	periods := make([]string, 0, int(end.Sub(start).Hours()/24)+1)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		periods = append(periods, date+":"+date)
	}
	return periods, nil
}

// buildDatePeriod constructs the Adjust API date_period parameter and applies lookback_days.
// NOTE: This method intentionally mutates opts.IntervalStart to expand it by lookback_days.
// This is necessary so the delete-insert strategy's delete scope matches the expanded fetch range.
// The mutation propagates through shared pointer aliasing (opts.IntervalStart points to the same
// time.Time as job.Config.IntervalStart in the pipeline). This coupling is fragile — if any
// intermediate code deep-copies IntervalStart, the delete scope will no longer match the fetch
// range, causing duplicate rows in destinations that don't enforce primary keys (e.g., BigQuery).
// It does the job, but is not readable and may cause hard to debug problems later.
func (s *AdjustSource) buildDatePeriod(opts *source.ReadOptions) (string, error) {
	days := 30
	if s.lookBackDays != "" {
		if d, err := strconv.Atoi(s.lookBackDays); err == nil && d >= 0 {
			days = d
		}
	}

	now := time.Now().UTC()
	startDate := now.AddDate(0, 0, -days)
	endDate := now

	if opts.IntervalStart != nil {
		startDate = opts.IntervalStart.AddDate(0, 0, -days)
		*opts.IntervalStart = startDate
	}
	if opts.IntervalEnd != nil {
		endDate = *opts.IntervalEnd
	}

	start := startDate.Format("2006-01-02")
	end := endDate.Format("2006-01-02")

	if !startDate.Before(endDate) {
		return "", fmt.Errorf("adjust date_period start (%s) must be before end (%s)", start, end)
	}

	return start + ":" + end, nil
}
