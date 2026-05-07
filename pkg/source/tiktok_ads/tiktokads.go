package tiktokads

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
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
	baseURL            = "https://business-api.tiktok.com/open_api/v1.3"
	defaultParallelism = 5
	defaultPageSize    = 1000
	defaultLookback    = 30
)

var typeHints = map[string]schema.DataType{
	"spend":                          schema.TypeDecimal,
	"billed_cost":                    schema.TypeDecimal,
	"cash_spend":                     schema.TypeDecimal,
	"voucher_spend":                  schema.TypeDecimal,
	"cpc":                            schema.TypeDecimal,
	"cpm":                            schema.TypeDecimal,
	"impressions":                    schema.TypeInt64,
	"gross_impressions":              schema.TypeInt64,
	"clicks":                         schema.TypeInt64,
	"ctr":                            schema.TypeDecimal,
	"reach":                          schema.TypeInt64,
	"cost_per_1000_reached":          schema.TypeDecimal,
	"frequency":                      schema.TypeDecimal,
	"conversion":                     schema.TypeInt64,
	"cost_per_conversion":            schema.TypeDecimal,
	"conversion_rate":                schema.TypeDecimal,
	"conversion_rate_v2":             schema.TypeDecimal,
	"real_time_conversion":           schema.TypeInt64,
	"real_time_cost_per_conversion":  schema.TypeDecimal,
	"real_time_conversion_rate":      schema.TypeDecimal,
	"real_time_conversion_rate_v2":   schema.TypeDecimal,
	"result":                         schema.TypeInt64,
	"cost_per_result":                schema.TypeDecimal,
	"result_rate":                    schema.TypeDecimal,
	"real_time_result":               schema.TypeInt64,
	"real_time_cost_per_result":      schema.TypeDecimal,
	"real_time_result_rate":          schema.TypeDecimal,
	"secondary_goal_result":          schema.TypeInt64,
	"cost_per_secondary_goal_result": schema.TypeDecimal,
	"secondary_goal_result_rate":     schema.TypeDecimal,
}

type TiktokAdsSource struct {
	client        *gonghttp.Client
	accessToken   string
	advertiserIDs []string
	timezone      string
}

func NewTiktokAdsSource() *TiktokAdsSource {
	return &TiktokAdsSource{}
}

func (s *TiktokAdsSource) Schemes() []string {
	return []string{"tiktok"}
}

func (s *TiktokAdsSource) Connect(ctx context.Context, uri string) error {
	accessToken, advertiserIDs, timezone, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.accessToken = accessToken
	s.advertiserIDs = advertiserIDs
	s.timezone = timezone

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRetry(12, 2*time.Second, 4096*time.Second),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithHeader("Access-Token", accessToken),
		gonghttp.WithHeader("Content-Type", "application/json"),
	)

	config.Debug("[TIKTOK] Connected with %d advertiser(s), timezone=%s", len(s.advertiserIDs), s.timezone)
	return nil
}

func parseURI(uri string) (accessToken string, advertiserIDs []string, timezone string, err error) {
	if !strings.HasPrefix(uri, "tiktok://") {
		return "", nil, "", fmt.Errorf("invalid tiktok URI: must start with tiktok://")
	}

	rest := strings.TrimPrefix(uri, "tiktok://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to parse tiktok URI query: %w", err)
	}

	accessToken = values.Get("access_token")
	if accessToken == "" {
		return "", nil, "", fmt.Errorf("access_token is required in tiktok URI")
	}

	adsRaw := values.Get("advertiser_ids")
	if adsRaw == "" {
		return "", nil, "", fmt.Errorf("advertiser_ids is required in tiktok URI")
	}
	advertiserIDs = strings.Split(strings.ReplaceAll(adsRaw, " ", ""), ",")

	timezone = values.Get("timezone")
	if timezone == "" {
		timezone = "UTC"
	}

	return accessToken, advertiserIDs, timezone, nil
}

func (s *TiktokAdsSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *TiktokAdsSource) HandlesIncrementality() bool {
	return true
}

func (s *TiktokAdsSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.IncrementalKey != "" {
		return nil, fmt.Errorf("tiktok takes care of incrementality on its own, you should not provide incremental_key")
	}

	tableName := req.Name

	if !strings.HasPrefix(tableName, "custom:") {
		return nil, fmt.Errorf("unsupported table: %s (expected format: custom:<dimensions>:<metrics> or custom:<dimensions>:<metrics>:<filters>)", tableName)
	}

	dimensions, metrics, filterName, filterValues, err := parseCustomTable(tableName)
	if err != nil {
		return nil, err
	}

	primaryKeys := append([]string{"advertiser_id"}, dimensions...)

	schemaCols := buildSchemaColumns(dimensions, metrics)

	incrementalKey := ""
	if slices.Contains(dimensions, "stat_time_day") {
		incrementalKey = "stat_time_day"
	}
	if slices.Contains(dimensions, "stat_time_hour") {
		incrementalKey = "stat_time_hour"
	}

	return &source.DynamicSourceTable{
		TableName:           "custom_reports",
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("tiktok source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, dimensions, metrics, schemaCols, filterName, filterValues, opts)
		},
	}, nil
}

func parseCustomTable(table string) (dimensions, metrics []string, filterName string, filterValues []int, err error) {
	fields := strings.SplitN(table, ":", 4)
	if len(fields) != 3 && len(fields) != 4 {
		return nil, nil, "", nil, fmt.Errorf("invalid TikTok custom table format. Expected: custom:<dimensions>:<metrics> or custom:<dimensions>:<metrics>:<filters>")
	}

	dimensions = strings.Split(strings.ReplaceAll(fields[1], " ", ""), ",")

	hasIDDimension := false
	for _, d := range dimensions {
		if d == "campaign_id" || d == "adgroup_id" || d == "ad_id" {
			hasIDDimension = true
			break
		}
	}
	if !hasIDDimension {
		return nil, nil, "", nil, fmt.Errorf("TikTok API requires at least one ID dimension: [campaign_id, adgroup_id, ad_id]")
	}

	filtered := dimensions[:0]
	for _, d := range dimensions {
		if d != "advertiser_id" {
			filtered = append(filtered, d)
		}
	}
	dimensions = filtered

	metrics = strings.Split(strings.ReplaceAll(fields[2], " ", ""), ",")

	if len(fields) == 4 {
		filterName, filterValues, err = parseFilters(fields[3])
		if err != nil {
			return nil, nil, "", nil, err
		}
	}

	return dimensions, metrics, filterName, filterValues, nil
}

func parseFilters(raw string) (string, []int, error) {
	filters := make(map[string][]string)
	var currentKey string

	for _, item := range strings.Split(raw, ",") {
		if strings.Contains(item, "=") {
			parts := strings.SplitN(item, "=", 2)
			currentKey = parts[0]
			filters[currentKey] = []string{parts[1]}
		} else if currentKey != "" {
			filters[currentKey] = append(filters[currentKey], item)
		}
	}

	if len(filters) > 1 {
		return "", nil, fmt.Errorf("only one filter is allowed for TikTok custom reports")
	}

	for name, vals := range filters {
		intVals := make([]int, 0, len(vals))
		for _, v := range vals {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return "", nil, fmt.Errorf("invalid filter value %q: must be an integer", v)
			}
			intVals = append(intVals, n)
		}
		return name, intVals, nil
	}

	return "", nil, nil
}

func buildSchemaColumns(dimensions, metrics []string) []schema.Column {
	allCols := make([]string, 0, len(dimensions)+len(metrics)+1)
	allCols = append(allCols, "advertiser_id")
	allCols = append(allCols, dimensions...)
	allCols = append(allCols, metrics...)

	var cols []schema.Column
	for _, name := range allCols {
		dt, ok := typeHints[name]
		if !ok {
			continue
		}
		col := schema.Column{
			Name:     name,
			DataType: dt,
			Nullable: true,
		}
		if dt == schema.TypeDecimal {
			col.Precision = 38
			col.Scale = 9
		}
		cols = append(cols, col)
	}
	return cols
}

type dateInterval struct {
	start time.Time
	end   time.Time
}

func findIntervals(start, end time.Time, intervalDays int) []dateInterval {
	var intervals []dateInterval
	current := start
	for !current.After(end) {
		intervalEnd := current.AddDate(0, 0, intervalDays)
		if intervalEnd.After(end) {
			intervalEnd = end
		}
		intervals = append(intervals, dateInterval{start: current, end: intervalEnd})
		current = intervalEnd.AddDate(0, 0, 1)
	}
	return intervals
}

func (s *TiktokAdsSource) read(ctx context.Context, dimensions, metrics []string, schemaCols []schema.Column, filterName string, filterValues []int, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		if err := s.fetchIntervals(ctx, dimensions, metrics, schemaCols, filterName, filterValues, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *TiktokAdsSource) fetchIntervals(ctx context.Context, dimensions, metrics []string, schemaCols []schema.Column, filterName string, filterValues []int, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	loc, err := time.LoadLocation(s.timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", s.timezone, err)
	}

	now := time.Now().In(loc)
	startDate := now.AddDate(0, 0, -defaultLookback)
	endDate := now

	if opts.IntervalStart != nil {
		startDate = opts.IntervalStart.In(loc)
	}
	if opts.IntervalEnd != nil {
		endDate = opts.IntervalEnd.In(loc)
	}

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > defaultPageSize {
		pageSize = defaultPageSize
	}

	intervalDays := 365
	if slices.Contains(dimensions, "stat_time_day") {
		intervalDays = 30
	}
	if slices.Contains(dimensions, "stat_time_hour") {
		intervalDays = 0
	}

	intervals := findIntervals(startDate, endDate, intervalDays)

	config.Debug("[TIKTOK] Fetching from %s to %s for %d advertiser(s), %d interval(s)", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"), len(s.advertiserIDs), len(intervals))

	taskCh := make(chan dateInterval, len(intervals))
	for _, iv := range intervals {
		taskCh <- iv
	}
	close(taskCh)

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}
	if parallelism > len(intervals) {
		parallelism = len(intervals)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iv := range taskCh {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if err := s.fetchPages(ctx, dimensions, metrics, schemaCols, filterName, filterValues, iv.start, iv.end, pageSize, loc, opts, results); err != nil {
					select {
					case errs <- err:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errs)

	if err := <-errs; err != nil {
		return err
	}

	return nil
}

func (s *TiktokAdsSource) fetchPages(ctx context.Context, dimensions, metrics []string, schemaCols []schema.Column, filterName string, filterValues []int, startDate, endDate time.Time, pageSize int, loc *time.Location, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	page := 1
	startStr := startDate.Format("2006-01-02")
	endStr := endDate.Format("2006-01-02")

	advertiserIDsJSON, _ := json.Marshal(s.advertiserIDs)
	dimensionsJSON, _ := json.Marshal(dimensions)
	metricsJSON, _ := json.Marshal(metrics)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := url.Values{}
		params.Set("advertiser_ids", string(advertiserIDsJSON))
		params.Set("report_type", "BASIC")
		params.Set("data_level", dataLevelFromDimensions(dimensions))
		params.Set("start_date", startStr)
		params.Set("end_date", endStr)
		params.Set("page_size", strconv.Itoa(pageSize))
		params.Set("dimensions", string(dimensionsJSON))
		params.Set("metrics", string(metricsJSON))
		params.Set("page", strconv.Itoa(page))

		if filterName != "" && len(filterValues) > 0 {
			valuesJSON, _ := json.Marshal(filterValues)
			filterJSON, _ := json.Marshal([]map[string]any{
				{
					"field_name":   filterName,
					"filter_type":  "IN",
					"filter_value": string(valuesJSON),
				},
			})
			params.Set("filtering", string(filterJSON))
		}

		endpoint := "/report/integrated/get/?" + params.Encode()
		config.Debug("[TIKTOK] Fetching page %d (%s to %s)", page, startStr, endStr)

		var apiResp apiResponse
		backoff := 2 * time.Second

		for attempt := range 12 {
			resp, err := s.client.R(ctx).Get(endpoint)
			if err != nil {
				return fmt.Errorf("failed to fetch report: %w", err)
			}

			if !resp.IsSuccess() {
				return fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
			}

			if err := json.Unmarshal(resp.Body(), &apiResp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if apiResp.Message == "OK" {
				break
			}

			if !strings.Contains(apiResp.Message, "QPS limit") || attempt == 11 {
				return fmt.Errorf("API error: %s", apiResp.Message)
			}

			config.Debug("[TIKTOK] QPS limit hit, retrying in %v (attempt %d/12)", backoff, attempt+1)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}

		if len(apiResp.Data.List) == 0 {
			break
		}

		items := flattenItems(apiResp.Data.List, loc)

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, schemaCols, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert to Arrow: %w", err)
		}

		results <- source.RecordBatchResult{Batch: record}

		config.Debug("[TIKTOK] Sent %d records (page %d/%d)", len(items), page, apiResp.Data.PageInfo.TotalPage)

		if page >= apiResp.Data.PageInfo.TotalPage {
			break
		}

		page++
	}

	return nil
}

func flattenItems(list []apiReportRow, loc *time.Location) []map[string]any {
	items := make([]map[string]any, 0, len(list))
	for _, row := range list {
		item := make(map[string]any, len(row.Dimensions)+len(row.Metrics))
		for k, v := range row.Dimensions {
			switch k {
			case "stat_time_day", "stat_time_hour":
				if s, ok := v.(string); ok {
					if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
						item[k] = t.In(loc)
						continue
					}
					if t, err := time.Parse("2006-01-02", s); err == nil {
						item[k] = t.In(loc)
						continue
					}
				}
				item[k] = v
			default:
				item[k] = v
			}
		}
		maps.Copy(item, row.Metrics)
		items = append(items, item)
	}
	return items
}

var dataLevelMapping = map[string]string{
	"advertiser_id": "AUCTION_ADVERTISER",
	"campaign_id":   "AUCTION_CAMPAIGN",
	"adgroup_id":    "AUCTION_ADGROUP",
}

func dataLevelFromDimensions(dimensions []string) string {
	for _, d := range dimensions {
		if level, ok := dataLevelMapping[d]; ok {
			return level
		}
	}
	return "AUCTION_AD"
}

type apiReportRow struct {
	Dimensions map[string]any `json:"dimensions"`
	Metrics    map[string]any `json:"metrics"`
}

type apiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		List     []apiReportRow `json:"list"`
		PageInfo struct {
			TotalNumber int `json:"total_number"`
			Page        int `json:"page"`
			PageSize    int `json:"page_size"`
			TotalPage   int `json:"total_page"`
		} `json:"page_info"`
	} `json:"data"`
}

var _ source.Source = (*TiktokAdsSource)(nil)
