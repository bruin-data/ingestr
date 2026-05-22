package appsflyer

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const apiBaseURL = "https://hq1.appsflyer.com"

var dimensionResponseMapping = map[string]string{
	"c":           "campaign",
	"af_adset_id": "adset_id",
	"af_adset":    "adset",
	"af_ad_id":    "ad_id",
}

var typeHints = map[string]schema.DataType{
	"app_id":       schema.TypeString,
	"campaign":     schema.TypeString,
	"geo":          schema.TypeString,
	"install_time": schema.TypeDate,
	"adset_id":     schema.TypeString,
	"adset":        schema.TypeString,
	"ad_id":        schema.TypeString,

	"cost":             schema.TypeDecimal,
	"average_ecpi":     schema.TypeDecimal,
	"retention_day_7":  schema.TypeDecimal,
	"retention_day_14": schema.TypeDecimal,
	"revenue":          schema.TypeDecimal,
	"roi":              schema.TypeDecimal,

	"clicks":      schema.TypeInt64,
	"impressions": schema.TypeInt64,
	"installs":    schema.TypeInt64,
	"loyal_users": schema.TypeInt64,
	"uninstalls":  schema.TypeInt64,

	"cohort_day_1_revenue_per_user":        schema.TypeDecimal,
	"cohort_day_1_total_revenue_per_user":  schema.TypeDecimal,
	"cohort_day_3_revenue_per_user":        schema.TypeDecimal,
	"cohort_day_3_total_revenue_per_user":  schema.TypeDecimal,
	"cohort_day_7_revenue_per_user":        schema.TypeDecimal,
	"cohort_day_7_total_revenue_per_user":  schema.TypeDecimal,
	"cohort_day_14_revenue_per_user":       schema.TypeDecimal,
	"cohort_day_14_total_revenue_per_user": schema.TypeDecimal,
	"cohort_day_21_revenue_per_user":       schema.TypeDecimal,
	"cohort_day_21_total_revenue_per_user": schema.TypeDecimal,
}

var (
	campaignsDimensions = []string{"c", "geo", "app_id", "install_time"}
	campaignsMetrics    = []string{
		"average_ecpi", "clicks",
		"cohort_day_1_revenue_per_user", "cohort_day_1_total_revenue_per_user",
		"cohort_day_14_revenue_per_user", "cohort_day_14_total_revenue_per_user",
		"cohort_day_21_revenue_per_user", "cohort_day_21_total_revenue_per_user",
		"cohort_day_3_revenue_per_user", "cohort_day_3_total_revenue_per_user",
		"cohort_day_7_revenue_per_user", "cohort_day_7_total_revenue_per_user",
		"cost", "impressions", "installs", "loyal_users",
		"retention_day_7", "revenue", "roi", "uninstalls",
	}
)

var (
	creativesDimensions = []string{"c", "geo", "app_id", "install_time", "af_adset_id", "af_adset", "af_ad_id"}
	creativesMetrics    = []string{
		"impressions", "clicks", "installs", "cost", "revenue",
		"average_ecpi", "loyal_users", "uninstalls", "roi",
	}
)

type AppsflyerSource struct {
	client *httpclient.Client
	apiKey string
}

func NewAppsflyerSource() *AppsflyerSource {
	return &AppsflyerSource{}
}

func (s *AppsflyerSource) Schemes() []string {
	return []string{"appsflyer"}
}

func (s *AppsflyerSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseAppsflyerURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey

	s.client = httpclient.New(
		httpclient.WithBaseURL(apiBaseURL),
		httpclient.WithTimeout(120*time.Second),
		// Master API limit: 1 call/min per app (short ranges), 120 calls/day (long ranges)
		httpclient.WithRateLimiter(0.8, 1),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("accept", "application/json"),
		httpclient.WithAuth(httpclient.NewBearerAuth(apiKey)),
	)

	config.Debug("[APPSFLYER] Connected successfully")
	return nil
}

func parseAppsflyerURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "appsflyer://") {
		return "", fmt.Errorf("invalid appsflyer URI: must start with appsflyer://")
	}

	rest := strings.TrimPrefix(uri, "appsflyer://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in appsflyer URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse appsflyer URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in appsflyer URI")
	}

	return apiKey, nil
}

func (s *AppsflyerSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *AppsflyerSource) HandlesIncrementality() bool {
	return true
}

type tableMeta struct {
	dimensions     []string
	metrics        []string
	primaryKeys    []string
	incrementalKey string
}

var supportedTables = map[string]tableMeta{
	"campaigns": {
		dimensions:     campaignsDimensions,
		metrics:        campaignsMetrics,
		primaryKeys:    buildPrimaryKeys(campaignsDimensions),
		incrementalKey: "install_time",
	},
	"creatives": {
		dimensions:     creativesDimensions,
		metrics:        creativesMetrics,
		primaryKeys:    buildPrimaryKeys(creativesDimensions),
		incrementalKey: "install_time",
	},
}

func (s *AppsflyerSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	var meta tableMeta
	if strings.HasPrefix(tableName, "custom:") {
		fields := strings.SplitN(tableName, ":", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid custom table format, expected custom:<dimensions>:<metrics>")
		}

		dimensions := strings.Split(fields[1], ",")
		metrics := strings.Split(fields[2], ",")
		for i := range dimensions {
			dimensions[i] = strings.TrimSpace(dimensions[i])
		}
		for i := range metrics {
			metrics[i] = strings.TrimSpace(metrics[i])
		}
		if !slices.Contains(dimensions, "install_time") {
			dimensions = append(dimensions, "install_time")
		}

		tableName = "custom"
		meta = tableMeta{
			dimensions:     dimensions,
			metrics:        metrics,
			primaryKeys:    buildPrimaryKeys(dimensions),
			incrementalKey: "install_time",
		}
	} else {
		m, ok := supportedTables[tableName]
		if !ok {
			tables := make([]string, 0, len(supportedTables))
			for t := range supportedTables {
				tables = append(tables, t)
			}
			return nil, fmt.Errorf("unsupported table: %s (supported: %v, or custom:<dimensions>:<metrics>)", req.Name, tables)
		}
		meta = m
	}

	schemaCols := buildSchemaColumns(meta.dimensions, meta.metrics)

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    meta.primaryKeys,
		TableIncrementalKey: meta.incrementalKey,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("appsflyer source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, meta, schemaCols, opts)
		},
	}, nil
}

func (s *AppsflyerSource) read(ctx context.Context, meta tableMeta, schemaCols []schema.Column, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if opts.IntervalStart == nil {
		return nil, fmt.Errorf("interval_start is required for AppsFlyer source, use --interval-start flag (e.g., --interval-start 2024-01-01)")
	}
	if opts.IntervalEnd == nil {
		return nil, fmt.Errorf("interval_end is required for AppsFlyer source, use --interval-end flag (e.g., --interval-end 2024-12-31)")
	}

	startDate := opts.IntervalStart.Format("2006-01-02")
	endDate := opts.IntervalEnd.Format("2006-01-02")

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		config.Debug("[APPSFLYER] Reading from %s to %s", startDate, endDate)

		data, err := s.fetchData(ctx, startDate, endDate, meta.dimensions, meta.metrics, 0)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}

		if len(data) == 0 {
			return
		}

		batchSize := opts.PageSize
		if batchSize <= 0 {
			batchSize = 1000
		}

		for i := 0; i < len(data); i += batchSize {
			select {
			case <-ctx.Done():
				results <- source.RecordBatchResult{Err: ctx.Err()}
				return
			default:
			}

			end := min(i+batchSize, len(data))
			batch := data[i:end]
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, schemaCols, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert to Arrow: %w", err)}
				return
			}
			results <- source.RecordBatchResult{Batch: record}
			config.Debug("[APPSFLYER] Sent batch with %d records", len(batch))
		}
	}()

	return results, nil
}

func (s *AppsflyerSource) fetchData(ctx context.Context, fromDate, toDate string, dimensions, metrics []string, maxRows int) ([]map[string]any, error) {
	excludedMetrics := excludeMetricsForDateRange(metrics, toDate)
	includedMetrics := make([]string, 0, len(metrics))
	for _, m := range metrics {
		if !slices.Contains(excludedMetrics, m) {
			includedMetrics = append(includedMetrics, m)
		}
	}

	if maxRows <= 0 {
		maxRows = 1000000
	}

	params := url.Values{}
	params.Set("from", fromDate)
	params.Set("to", toDate)
	params.Set("groupings", strings.Join(dimensions, ","))
	params.Set("kpis", strings.Join(includedMetrics, ","))
	params.Set("format", "json")
	params.Set("maximum_rows", fmt.Sprintf("%d", maxRows))

	endpoint := fmt.Sprintf("/api/master-agg-data/v4/app/all?%s", params.Encode())

	config.Debug("[APPSFLYER] Fetching data from %s to %s", fromDate, toDate)

	resp, err := s.client.R(ctx).
		Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch appsflyer data: %w", err)
	}

	if resp.StatusCode() == 429 {
		return nil, fmt.Errorf("appsflyer API rate limit exceeded, please retry later")
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("appsflyer API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var result []map[string]any
	if err := resp.JSON(&result); err != nil {
		return nil, fmt.Errorf("failed to parse appsflyer response: %w", err)
	}

	standardized := standardizeKeys(result, excludedMetrics)
	config.Debug("[APPSFLYER] Fetched %d records", len(standardized))
	return standardized, nil
}

func standardizeKeys(data []map[string]any, excludedMetrics []string) []map[string]any {
	standardized := make([]map[string]any, 0, len(data))
	for _, item := range data {
		stdItem := make(map[string]any, len(item)+len(excludedMetrics))
		for key, value := range item {
			stdItem[fixKey(key)] = value
		}
		for _, metric := range excludedMetrics {
			k := fixKey(metric)
			if _, exists := stdItem[k]; !exists {
				stdItem[k] = nil
			}
		}
		standardized = append(standardized, stdItem)
	}
	return standardized
}

func fixKey(key string) string {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "-", "")
	key = strings.ReplaceAll(key, "  ", "_")
	key = strings.ReplaceAll(key, " ", "_")
	return key
}

func excludeMetricsForDateRange(metrics []string, toDate string) []string {
	endDate, err := time.Parse("2006-01-02", toDate)
	if err != nil {
		return nil
	}

	daysSinceEnd := int(time.Since(endDate).Hours() / 24)

	var excluded []string
	for _, metric := range metrics {
		if strings.Contains(metric, "cohort_day_") {
			parts := strings.Split(metric, "_")
			if len(parts) >= 3 {
				var dayCount int
				if _, err := fmt.Sscanf(parts[2], "%d", &dayCount); err == nil {
					if daysSinceEnd <= dayCount {
						excluded = append(excluded, metric)
					}
				}
			}
		}
	}
	return excluded
}

func buildPrimaryKeys(dimensions []string) []string {
	keys := make([]string, 0, len(dimensions))
	for _, d := range dimensions {
		if mapped, ok := dimensionResponseMapping[d]; ok {
			keys = append(keys, mapped)
		} else {
			keys = append(keys, d)
		}
	}
	return keys
}

func buildSchemaColumns(dimensions, metrics []string) []schema.Column {
	seen := make(map[string]bool)
	var cols []schema.Column

	addCol := func(name string) {
		respName := name
		if mapped, ok := dimensionResponseMapping[name]; ok {
			respName = mapped
		}
		if seen[respName] {
			return
		}
		seen[respName] = true

		dt, ok := typeHints[respName]
		if !ok {
			dt = schema.TypeUnknown
		}
		col := schema.Column{
			Name:     respName,
			DataType: dt,
			Nullable: true,
		}
		if dt == schema.TypeDecimal {
			col.Precision = 30
			col.Scale = 5
		}
		cols = append(cols, col)
	}

	for _, d := range dimensions {
		addCol(d)
	}
	for _, m := range metrics {
		addCol(m)
	}

	return cols
}

var _ source.Source = (*AppsflyerSource)(nil)
