package applovin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL               = "https://r.applovin.com"
	defaultParallelism    = 5
	parallelThresholdDays = 7 // if date range > this, use parallel fetching
)

type ReportType string

const (
	ReportTypePublisher  ReportType = "publisher"
	ReportTypeAdvertiser ReportType = "advertiser"
)

var publisherColumns = []string{
	"ad_type", "application", "application_is_hidden", "bidding_integration",
	"clicks", "country", "ctr", "day", "device_type", "ecpm", "hour",
	"impressions", "package_name", "placement_type", "platform", "revenue",
	"size", "store_id", "zone", "zone_id",
}

var advertiserColumns = []string{
	"ad", "ad_creative_type", "ad_id", "ad_type", "average_cpa", "average_cpc",
	"campaign", "campaign_ad_type", "campaign_bid_goal", "campaign_id_external",
	"campaign_package_name", "campaign_roas_goal", "campaign_store_id",
	"campaign_type", "clicks", "conversions", "conversion_rate", "cost",
	"country", "creative_set", "creative_set_id", "ctr", "custom_page_id",
	"day", "device_type", "external_placement_id", "first_purchase", "hour",
	"impressions", "installs", "optimization_day_target", "placement_type",
	"platform", "redownloads", "sales", "size", "target_event", "traffic_source",
}

// probabilisticColumns excludes "installs" and "redownloads" from advertiser columns
var probabilisticColumns = []string{
	"ad", "ad_creative_type", "ad_id", "ad_type", "average_cpa", "average_cpc",
	"campaign", "campaign_ad_type", "campaign_bid_goal", "campaign_id_external",
	"campaign_package_name", "campaign_roas_goal", "campaign_store_id",
	"campaign_type", "clicks", "conversions", "conversion_rate", "cost",
	"country", "creative_set", "creative_set_id", "ctr", "custom_page_id",
	"day", "device_type", "external_placement_id", "first_purchase", "hour",
	"impressions", "optimization_day_target", "placement_type",
	"platform", "sales", "size", "target_event", "traffic_source",
}

// skaColumns same as advertiser columns
var skaColumns = []string{
	"ad", "ad_creative_type", "ad_id", "ad_type", "average_cpa", "average_cpc",
	"campaign", "campaign_ad_type", "campaign_bid_goal", "campaign_id_external",
	"campaign_package_name", "campaign_roas_goal", "campaign_store_id",
	"campaign_type", "clicks", "conversions", "conversion_rate", "cost",
	"country", "creative_set", "creative_set_id", "ctr", "custom_page_id",
	"day", "device_type", "external_placement_id", "first_purchase", "hour",
	"impressions", "installs", "optimization_day_target", "placement_type",
	"platform", "redownloads", "sales", "size", "target_event", "traffic_source",
}

// Dimensions (non-metric columns) for merge key
var dimensions = map[string]bool{
	"ad_type": true, "application": true, "application_is_hidden": true,
	"bidding_integration": true, "country": true, "day": true, "device_type": true,
	"hour": true, "package_name": true, "placement_type": true, "platform": true,
	"size": true, "store_id": true, "zone": true, "zone_id": true,
	"ad": true, "ad_creative_type": true, "ad_id": true, "campaign": true,
	"campaign_ad_type": true, "campaign_id_external": true, "campaign_package_name": true,
	"campaign_store_id": true, "campaign_type": true, "creative_set": true,
	"creative_set_id": true, "custom_page_id": true, "external_placement_id": true,
	"optimization_day_target": true, "target_event": true, "traffic_source": true,
}

// typeHints defines the expected data types for columns
// AppLovin API returns all values as strings, so we need to convert them
var typeHints = map[string]schema.DataType{
	"application_is_hidden": schema.TypeBoolean,
	"average_cpa":           schema.TypeFloat64,
	"average_cpc":           schema.TypeFloat64,
	"campaign_bid_goal":     schema.TypeFloat64,
	"campaign_roas_goal":    schema.TypeFloat64,
	"clicks":                schema.TypeInt64,
	"conversions":           schema.TypeInt64,
	"conversion_rate":       schema.TypeFloat64,
	"cost":                  schema.TypeFloat64,
	"ctr":                   schema.TypeFloat64,
	"day":                   schema.TypeDate,
	"ecpm":                  schema.TypeFloat64,
	"first_purchase":        schema.TypeInt64,
	"impressions":           schema.TypeInt64,
	"installs":              schema.TypeInt64,
	"redownloads":           schema.TypeInt64,
	"revenue":               schema.TypeFloat64,
	"sales":                 schema.TypeFloat64,
}

type dateFetchTask struct {
	startDate string
	endDate   string
}

type dateFetchResult struct {
	data []map[string]interface{}
	err  error
}

type AppLovinSource struct {
	apiKey string
	client *gonghttp.Client
	tables map[string]source.SourceTable
}

func NewAppLovinSource() *AppLovinSource {
	return &AppLovinSource{}
}

func (s *AppLovinSource) Schemes() []string {
	return []string{"applovin"}
}

func (s *AppLovinSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseAppLovinURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey
	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(5*time.Minute),
		gonghttp.WithRateLimiter(5, 2),
		gonghttp.WithDebug(config.DebugMode),
	)

	s.tables = s.getTables()
	config.Debug("[APPLOVIN] Connected successfully")
	return nil
}

func parseAppLovinURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "applovin://") {
		return "", fmt.Errorf("invalid applovin URI: must start with applovin://")
	}

	rest := strings.TrimPrefix(uri, "applovin://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in applovin URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse applovin URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in applovin URI")
	}

	return apiKey, nil
}

func (s *AppLovinSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *AppLovinSource) HandlesIncrementality() bool {
	return true
}

func (s *AppLovinSource) getTables() map[string]source.SourceTable {
	schemaFn := func(ctx context.Context) (*schema.TableSchema, error) {
		return nil, fmt.Errorf("applovin source does not have a predefined schema; schema inference is required")
	}

	return map[string]source.SourceTable{
		"publisher-report": &source.DynamicSourceTable{
			TableName:           "publisher-report",
			TablePrimaryKeys:    getDimensionColumns(publisherColumns),
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyMerge,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, "report", publisherColumns, ReportTypePublisher, opts)
			},
		},
		"advertiser-report": &source.DynamicSourceTable{
			TableName:           "advertiser-report",
			TablePrimaryKeys:    getDimensionColumns(advertiserColumns),
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyMerge,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, "report", advertiserColumns, ReportTypeAdvertiser, opts)
			},
		},
		"advertiser-probabilistic-report": &source.DynamicSourceTable{
			TableName:           "advertiser-probabilistic-report",
			TablePrimaryKeys:    []string{"day"},
			TableIncrementalKey: "day",
			TableStrategy:       config.StrategyDeleteInsert,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, "probabilisticReport", probabilisticColumns, ReportTypeAdvertiser, opts)
			},
		},
		"advertiser-ska-report": &source.DynamicSourceTable{
			TableName:           "advertiser-ska-report",
			TablePrimaryKeys:    []string{"day"},
			TableIncrementalKey: "day",
			TableStrategy:       config.StrategyDeleteInsert,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, "skaReport", skaColumns, ReportTypeAdvertiser, opts)
			},
		},
	}
}

func (s *AppLovinSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	// Handle custom reports: custom:{endpoint}:{report_type}:{dimensions}
	if strings.HasPrefix(req.Name, "custom:") {
		return s.createCustomReportTable(req.Name)
	}

	table, ok := s.tables[req.Name]
	if !ok {
		tables := make([]string, 0, len(s.tables))
		for t := range s.tables {
			tables = append(tables, t)
		}
		return nil, fmt.Errorf("unsupported table: %s (supported: %v, or custom:{endpoint}:{report_type}:{dimensions})", req.Name, tables)
	}
	return table, nil
}

func (s *AppLovinSource) createCustomReportTable(spec string) (source.SourceTable, error) {
	parts := strings.Split(spec, ":")
	if len(parts) != 4 {
		return nil, fmt.Errorf("custom report should be in format 'custom:{endpoint}:{report_type}:{dimensions}'")
	}

	endpoint := strings.TrimSpace(parts[1])
	reportTypeStr := strings.TrimSpace(parts[2])
	dimensionsStr := strings.TrimSpace(parts[3])

	var reportType ReportType
	switch reportTypeStr {
	case "publisher":
		reportType = ReportTypePublisher
	case "advertiser":
		reportType = ReportTypeAdvertiser
	default:
		return nil, fmt.Errorf("invalid report_type: %s (must be 'publisher' or 'advertiser')", reportTypeStr)
	}

	columns := strings.Split(dimensionsStr, ",")
	for i := range columns {
		columns[i] = strings.TrimSpace(columns[i])
	}

	// Ensure "day" is included for API request
	if !slices.Contains(columns, "day") {
		columns = append(columns, "day")
	}

	return &source.DynamicSourceTable{
		TableName:           "custom_report",
		TablePrimaryKeys:    getDimensionColumns(columns),
		TableIncrementalKey: "",
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("applovin source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readTable(ctx, endpoint, columns, reportType, opts)
		},
	}, nil
}

func (s *AppLovinSource) readTable(ctx context.Context, endpoint string, columns []string, reportType ReportType, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	schemaCols := buildSchemaColumns(columns)
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		if err := s.fetch(ctx, endpoint, columns, schemaCols, reportType, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// fetch handles both single and parallel fetching based on date range
func (s *AppLovinSource) fetch(ctx context.Context, endpoint string, columns []string, schemaCols []schema.Column, reportType ReportType, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	start, end, err := parseTimeInterval(opts.IntervalStart, opts.IntervalEnd)
	if err != nil {
		return err
	}

	startDate := start.Format("2006-01-02")
	endDate := end.Format("2006-01-02")
	config.Debug("[APPLOVIN] Reading %s report from %s to %s", endpoint, startDate, endDate)

	daysDiff := int(end.Sub(start).Hours() / 24)

	// Build date ranges to fetch
	var dateRanges []dateFetchTask
	if daysDiff > parallelThresholdDays {
		// Split into individual days for parallel fetching
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			dateStr := d.Format("2006-01-02")
			dateRanges = append(dateRanges, dateFetchTask{startDate: dateStr, endDate: dateStr})
		}
	} else {
		// Single range fetch
		dateRanges = append(dateRanges, dateFetchTask{startDate: startDate, endDate: endDate})
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}

	if len(dateRanges) == 1 {
		parallelism = 1
	}

	config.Debug("[APPLOVIN] Fetching %d range(s) with parallelism %d", len(dateRanges), parallelism)

	taskChan := make(chan dateFetchTask, len(dateRanges))
	resultChan := make(chan dateFetchResult, parallelism*2)

	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				data, err := s.fetchReport(ctx, endpoint, columns, reportType, task.startDate, task.endDate)
				if err != nil {
					config.Debug("[APPLOVIN] Error fetching %s to %s: %v", task.startDate, task.endDate, err)
				} else {
					config.Debug("[APPLOVIN] Fetched %d records for %s to %s", len(data), task.startDate, task.endDate)
				}
				resultChan <- dateFetchResult{data: data, err: err}
			}
		}()
	}

	// Send tasks
	go func() {
		for _, dr := range dateRanges {
			taskChan <- dr
		}
		close(taskChan)
	}()

	// Close result channel when workers done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect and send results
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	totalLimit := opts.Limit
	totalSent := 0
	var pendingData []map[string]interface{}

	sendBatch := func() error {
		if len(pendingData) == 0 {
			return nil
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(pendingData, schemaCols, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert to Arrow: %w", err)
		}

		results <- source.RecordBatchResult{Batch: record}
		config.Debug("[APPLOVIN] Sent batch with %d records (total: %d)", len(pendingData), totalSent+len(pendingData))
		totalSent += len(pendingData)
		pendingData = nil
		return nil
	}

	var fetchErrors []error
	for result := range resultChan {
		if result.err != nil {
			config.Debug("[APPLOVIN] Error fetching: %v", result.err)
			fetchErrors = append(fetchErrors, result.err)
			continue
		}

		for _, item := range result.data {
			if totalLimit > 0 && totalSent+len(pendingData) >= totalLimit {
				break
			}
			pendingData = append(pendingData, item)

			if len(pendingData) >= batchSize {
				if err := sendBatch(); err != nil {
					return err
				}
			}
		}

		if totalLimit > 0 && totalSent >= totalLimit {
			break
		}
	}

	// If we have no data and there were errors, return the first error
	if totalSent == 0 && len(pendingData) == 0 && len(fetchErrors) > 0 {
		return fmt.Errorf("all fetch requests failed: %w", fetchErrors[0])
	}

	return sendBatch()
}

func (s *AppLovinSource) fetchAPI(ctx context.Context, endpoint string, params map[string]string) (map[string]interface{}, error) {
	// Build URL with params
	fullURL := "/" + endpoint
	if len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			values.Set(k, v)
		}
		fullURL = fullURL + "?" + values.Encode()
	}

	config.Debug("[APPLOVIN] Fetching: %s%s", baseURL, endpoint)

	var result map[string]interface{}
	resp, err := s.client.R(ctx).SetResult(&result).Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", endpoint, errors.New(sanitizeError(err, s.apiKey)))
	}

	config.Debug("[APPLOVIN] Response status: %d, body length: %d", resp.StatusCode(), len(resp.String()))
	if len(resp.String()) < 2000 {
		config.Debug("[APPLOVIN] Response body: %s", resp.String())
	}

	if resp.StatusCode() >= 400 {
		return nil, fmt.Errorf("API returned status %d for %s: %s", resp.StatusCode(), endpoint, resp.String())
	}

	return result, nil
}

func (s *AppLovinSource) fetchReport(ctx context.Context, endpoint string, columns []string, reportType ReportType, startDate, endDate string) ([]map[string]interface{}, error) {
	params := map[string]string{
		"api_key":     s.apiKey,
		"format":      "json",
		"report_type": string(reportType),
		"columns":     strings.Join(columns, ","),
		"start":       startDate,
		"end":         endDate,
	}

	result, err := s.fetchAPI(ctx, endpoint, params)
	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, nil
	}

	// Extract results array from response
	if results, ok := result["results"].([]interface{}); ok {
		var data []map[string]interface{}
		for _, item := range results {
			if m, ok := item.(map[string]interface{}); ok {
				data = append(data, m)
			}
		}
		config.Debug("[APPLOVIN] Parsed %d records from response", len(data))
		return data, nil
	}

	return nil, fmt.Errorf("unexpected response format: missing 'results' array")
}

func parseTimeInterval(intervalStart, intervalEnd interface{}) (startTime, endTime time.Time, err error) {
	config.Debug("[APPLOVIN] parseTimeInterval called with start=%v (%T), end=%v (%T)", intervalStart, intervalStart, intervalEnd, intervalEnd)

	// interval_start is required
	if intervalStart == nil || isNilPointer(intervalStart) {
		return time.Time{}, time.Time{}, fmt.Errorf("interval_start is required for AppLovin source. Use --interval-start flag (e.g., --interval-start 2024-01-01)")
	}

	// interval_end is required
	if intervalEnd == nil || isNilPointer(intervalEnd) {
		return time.Time{}, time.Time{}, fmt.Errorf("interval_end is required for AppLovin source. Use --interval-end flag (e.g., --interval-end 2024-12-31)")
	}

	startTime = parseTimestamp(intervalStart, time.Time{})
	endTime = parseTimestamp(intervalEnd, time.Time{})

	if startTime.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid interval_start value")
	}
	if endTime.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid interval_end value")
	}

	config.Debug("[APPLOVIN] Parsed time: start=%s, end=%s", startTime.Format("2006-01-02"), endTime.Format("2006-01-02"))
	return startTime, endTime, nil
}

func parseTimestamp(value interface{}, defaultVal time.Time) time.Time {
	if value == nil {
		return defaultVal
	}

	switch v := value.(type) {
	case time.Time:
		return v
	case *time.Time:
		if v != nil {
			return *v
		}
	}
	return defaultVal
}

func isNilPointer(v interface{}) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case *time.Time:
		return val == nil
	}
	return false
}

func sanitizeError(err error, apiKey string) string {
	return strings.ReplaceAll(err.Error(), apiKey, "***")
}

func excludeColumns(columns []string, exclude map[string]bool) []string {
	result := make([]string, 0, len(columns))
	for _, col := range columns {
		if !exclude[col] {
			result = append(result, col)
		}
	}
	return result
}

func getDimensionColumns(columns []string) []string {
	result := make([]string, 0)
	// Always include "day" first if present
	for _, col := range columns {
		if col == "day" {
			result = append(result, col)
			break
		}
	}
	// Add other dimensions
	for _, col := range columns {
		if dimensions[col] && col != "day" {
			result = append(result, col)
		}
	}
	return result
}

func buildSchemaColumns(columns []string) []schema.Column {
	cols := make([]schema.Column, len(columns))
	for i, name := range columns {
		dt, ok := typeHints[name]
		if !ok {
			dt = schema.TypeString
		}
		cols[i] = schema.Column{
			Name:     name,
			DataType: dt,
			Nullable: true,
		}
	}
	return cols
}

var _ source.Source = (*AppLovinSource)(nil)
