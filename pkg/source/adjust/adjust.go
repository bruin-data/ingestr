package adjust

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

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

var validAttributionTypes = map[string]struct{}{
	"click":      {},
	"impression": {},
	"engaged_ad": {},
}

// validateAttributionTypes rejects any value outside Adjust's allowed set so a
// typo fails fast instead of returning an opaque API error.
func validateAttributionTypes(attributionTypes string) error {
	for _, t := range strings.Split(attributionTypes, ",") {
		if t = strings.TrimSpace(t); t == "" {
			continue
		}
		if _, ok := validAttributionTypes[t]; !ok {
			return fmt.Errorf("unknown attribution_types value %q; valid values are click, impression, engaged_ad", t)
		}
	}
	return nil
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
	if err := validateAttributionTypes(attributionTypes); err != nil {
		return nil, err
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
	results := make(chan source.RecordBatchResult, 8)

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

var defaultMetrics = []string{
	"installs",
	"network_cost",
	"all_revenue_total_d0",
	"ad_revenue_total_d0",
	"revenue_total_d0",
	"all_revenue_total_d1",
	"ad_revenue_total_d1",
	"revenue_total_d1",
	"all_revenue_total_d3",
	"ad_revenue_total_d3",
	"revenue_total_d3",
	"all_revenue_total_d7",
	"ad_revenue_total_d7",
	"revenue_total_d7",
	"all_revenue_total_d14",
	"ad_revenue_total_d14",
	"revenue_total_d14",
	"all_revenue_total_d21",
}

func (s *AdjustSource) readCampaigns(ctx context.Context, appTokens, attributionTypes string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ADJUST] Fetching campaigns")

	datePeriod, err := s.buildDatePeriod(&opts)
	if err != nil {
		return fmt.Errorf("failed to build date period for campaigns: %w", err)
	}

	req := s.client.R(ctx).
		SetQueryParam("dimensions", strings.Join(defaultDimensions, ",")).
		SetQueryParam("metrics", strings.Join(defaultMetrics, ",")).
		SetQueryParam("date_period", datePeriod)

	if at := resolveAttributionTypes(attributionTypes); at != "" {
		req.SetQueryParam("attribution_types", at)
	}

	if appTokens != "" {
		req.SetQueryParam("app_token__in", appTokens)
	}

	resp, err := req.Get("report")
	if err != nil {
		return fmt.Errorf("failed to fetch campaigns: %w", err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("failed to fetch campaigns: status %d: %s", resp.StatusCode(), resp.String())
	}

	var result struct {
		Rows []map[string]interface{} `json:"rows"`
	}
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse campaigns response: %w", err)
	}

	if len(result.Rows) == 0 {
		config.Debug("[ADJUST] No campaigns found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(result.Rows, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert campaigns to Arrow: %w", err)
	}

	config.Debug("[ADJUST] Sending %d campaigns", len(result.Rows))
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

var creativePrimaryKeys = []string{"campaign", "day", "app", "store_type", "channel", "country", "adgroup", "creative"}

var creativeDimensions = []string{"campaign", "day", "app", "app_token", "store_type", "channel", "country", "adgroup", "creative"}

func (s *AdjustSource) readCreatives(ctx context.Context, appTokens, attributionTypes string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ADJUST] Fetching creatives")

	datePeriod, err := s.buildDatePeriod(&opts)
	if err != nil {
		return fmt.Errorf("failed to build date period for creatives: %w", err)
	}

	req := s.client.R(ctx).
		SetQueryParam("dimensions", strings.Join(creativeDimensions, ",")).
		SetQueryParam("metrics", strings.Join(defaultMetrics, ",")).
		SetQueryParam("date_period", datePeriod)

	if at := resolveAttributionTypes(attributionTypes); at != "" {
		req.SetQueryParam("attribution_types", at)
	}

	if appTokens != "" {
		req.SetQueryParam("app_token__in", appTokens)
	}

	resp, err := req.Get("report")
	if err != nil {
		return fmt.Errorf("failed to fetch creatives: %w", err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("failed to fetch creatives: status %d: %s", resp.StatusCode(), resp.String())
	}

	var result struct {
		Rows []map[string]interface{} `json:"rows"`
	}
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse creatives response: %w", err)
	}

	if len(result.Rows) == 0 {
		config.Debug("[ADJUST] No creatives found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(result.Rows, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert creatives to Arrow: %w", err)
	}

	config.Debug("[ADJUST] Sending %d creatives", len(result.Rows))
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

func (s *AdjustSource) readCustom(ctx context.Context, table string, appTokens string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ADJUST] Fetching custom report")

	dimensions, metrics, filters, err := parseCustomTable(table)
	if err != nil {
		return err
	}

	datePeriod, err := s.buildDatePeriod(&opts)
	if err != nil {
		return fmt.Errorf("failed to build date period for custom report: %w", err)
	}

	req := s.client.R(ctx).
		SetQueryParam("dimensions", dimensions).
		SetQueryParam("metrics", metrics).
		SetQueryParam("date_period", datePeriod)

	if appTokens != "" {
		req.SetQueryParam("app_token__in", appTokens)
	}

	for k, v := range filters {
		req.SetQueryParam(k, v)
	}

	resp, err := req.Get("report")
	if err != nil {
		return fmt.Errorf("failed to fetch custom report: %w", err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("failed to fetch custom report: status %d: %s", resp.StatusCode(), resp.String())
	}

	var result struct {
		Rows []map[string]interface{} `json:"rows"`
	}
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse custom report response: %w", err)
	}

	if len(result.Rows) == 0 {
		config.Debug("[ADJUST] No custom report data found")
		return nil
	}

	cols := buildTypeHintColumns(dimensions, metrics)
	record, err := arrowconv.ItemsToArrowRecordWithSchema(result.Rows, cols, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert custom report to Arrow: %w", err)
	}

	config.Debug("[ADJUST] Sending %d custom report rows", len(result.Rows))
	results <- source.RecordBatchResult{Batch: record}
	return nil
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

func buildTypeHintColumns(dimensions, metrics string) []schema.Column {
	var cols []schema.Column
	for _, name := range strings.Split(dimensions, ",") {
		if dt, ok := knownTypeHints[name]; ok {
			col := schema.Column{Name: name, DataType: dt}
			if dt == schema.TypeDecimal {
				col.Precision = 38
				col.Scale = 9
			}
			cols = append(cols, col)
		}
	}
	for _, name := range strings.Split(metrics, ",") {
		if dt, ok := knownTypeHints[name]; ok {
			col := schema.Column{Name: name, DataType: dt}
			if dt == schema.TypeDecimal {
				col.Precision = 38
				col.Scale = 9
			}
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
