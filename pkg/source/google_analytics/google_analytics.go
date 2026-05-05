package google_analytics

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	analyticsdata "google.golang.org/api/analyticsdata/v1beta"
	"google.golang.org/api/option"
)

const (
	maxRowsPerRequest  = 100000
	defaultParallelism = 5
)

var supportedReportTypes = map[string]tableMeta{
	"custom":   {strategy: config.StrategyMerge},
	"realtime": {strategy: config.StrategyMerge, mergeKey: []string{"ingested_at"}},
}

type propertyInfo struct {
	id       int64
	resource string // "properties/<id>"
}

type GoogleAnalyticsSource struct {
	client     *analyticsdata.Service
	properties []propertyInfo
}

func NewGoogleAnalyticsSource() *GoogleAnalyticsSource {
	return &GoogleAnalyticsSource{}
}

func (s *GoogleAnalyticsSource) Schemes() []string {
	return []string{"googleanalytics"}
}

func (s *GoogleAnalyticsSource) Connect(ctx context.Context, uri string) error {
	credJSON, propertyIDs, err := parseConnectionURI(uri)
	if err != nil {
		return err
	}

	seen := make(map[int64]struct{}, len(propertyIDs))
	properties := make([]propertyInfo, 0, len(propertyIDs))
	for _, raw := range propertyIDs {
		pid, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("%s is an invalid google property id, please use a numeric id and not your Measurement ID like G-7F1AE12JLR", raw)
		}
		if pid <= 0 {
			return fmt.Errorf("google analytics property id must be a positive integer, got %d", pid)
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		properties = append(properties, propertyInfo{
			id:       pid,
			resource: fmt.Sprintf("properties/%d", pid),
		})
	}

	client, err := analyticsdata.NewService(ctx, option.WithAuthCredentialsJSON(option.ServiceAccount, credJSON))
	if err != nil {
		return fmt.Errorf("failed to create analytics data service: %w", err)
	}

	s.client = client
	s.properties = properties

	config.Debug("[GOOGLE ANALYTICS] Connected to %d properties", len(properties))
	return nil
}

func (s *GoogleAnalyticsSource) Close(ctx context.Context) error {
	return nil
}

func (s *GoogleAnalyticsSource) HandlesIncrementality() bool {
	return true
}

type tableMeta struct {
	strategy config.IncrementalStrategy
	mergeKey []string
}

func (s *GoogleAnalyticsSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.IncrementalKey != "" {
		return nil, fmt.Errorf("incremental loads are not yet supported for Google Analytics")
	}

	if req.Name == "" {
		return nil, fmt.Errorf("table name is required for google analytics source")
	}

	cfg, err := buildReportConfig(req.Name)
	if err != nil {
		return nil, err
	}

	meta, ok := supportedReportTypes[cfg.reportType]
	if !ok {
		return nil, fmt.Errorf("unsupported report type %q (supported: custom, realtime)", cfg.reportType)
	}

	pks := append([]string{"property_id"}, meta.mergeKey...)
	if cfg.reportType == "custom" {
		pks = []string{"property_id", cfg.datetime}
	}

	return &source.DynamicSourceTable{
		TableName:        req.Name,
		TablePrimaryKeys: pks,
		TableStrategy:    meta.strategy,
		KnownSchema:      false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("google analytics source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.fetchReport(ctx, cfg, opts)
		},
	}, nil
}

type reportConfig struct {
	reportType   string
	dimensions   []string
	metrics      []string
	datetime     string
	startDate    time.Time
	endDate      time.Time
	minuteRanges []*analyticsdata.MinuteRange
}

func (rc *reportConfig) apiDimensions() []*analyticsdata.Dimension {
	dims := make([]*analyticsdata.Dimension, len(rc.dimensions))
	for i, d := range rc.dimensions {
		dims[i] = &analyticsdata.Dimension{Name: d}
	}
	return dims
}

func (rc *reportConfig) apiMetrics() []*analyticsdata.Metric {
	mets := make([]*analyticsdata.Metric, len(rc.metrics))
	for i, m := range rc.metrics {
		mets[i] = &analyticsdata.Metric{Name: m}
	}
	return mets
}

func rowsToItems(rows []*analyticsdata.Row, headers []*analyticsdata.MetricHeader, cfg *reportConfig, extra map[string]any) []map[string]any {
	numRanges := len(cfg.minuteRanges)
	if numRanges <= 1 {
		items := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			item := make(map[string]any, len(cfg.dimensions)+len(cfg.metrics)+len(extra))
			for i, val := range row.DimensionValues {
				if i < len(cfg.dimensions) {
					item[cfg.dimensions[i]] = coerceDimensionValue(cfg.dimensions[i], val.Value)
				}
			}
			for i, val := range row.MetricValues {
				if i < len(cfg.metrics) && i < len(headers) {
					item[cfg.metrics[i]] = coerceMetricValue(headers[i].Type, val.Value)
				}
			}
			maps.Copy(item, extra)
			items = append(items, item)
		}
		return items
	}

	numMetrics := len(cfg.metrics)
	items := make([]map[string]any, 0, len(rows)*numRanges)
	for _, row := range rows {
		for rangeIdx := range numRanges {
			item := make(map[string]any, len(cfg.dimensions)+numMetrics+len(extra)+1)
			for i, val := range row.DimensionValues {
				if i < len(cfg.dimensions) {
					item[cfg.dimensions[i]] = coerceDimensionValue(cfg.dimensions[i], val.Value)
				}
			}
			for metricIdx := range numMetrics {
				valIdx := rangeIdx*numMetrics + metricIdx
				if valIdx < len(row.MetricValues) && valIdx < len(headers) {
					item[cfg.metrics[metricIdx]] = coerceMetricValue(headers[valIdx].Type, row.MetricValues[valIdx].Value)
				}
			}
			mr := cfg.minuteRanges[rangeIdx]
			item["date_range"] = fmt.Sprintf("%d-%d minutes ago", mr.EndMinutesAgo, mr.StartMinutesAgo)
			maps.Copy(item, extra)
			items = append(items, item)
		}
	}
	return items
}

func (s *GoogleAnalyticsSource) fetchReport(ctx context.Context, cfg *reportConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if opts.IntervalStart != nil {
		cfg.startDate = *opts.IntervalStart
	}
	if opts.IntervalEnd != nil {
		cfg.endDate = *opts.IntervalEnd
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}
	if parallelism > len(s.properties) {
		parallelism = len(s.properties)
	}

	ctx, cancel := context.WithCancel(ctx)
	taskChan := make(chan propertyInfo, len(s.properties))
	results := make(chan source.RecordBatchResult, parallelism*2)

	var wg sync.WaitGroup
	for range parallelism {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for prop := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				var err error
				switch cfg.reportType {
				case "custom":
					err = s.fetchCustomReport(ctx, cfg, opts, prop, results)
				case "realtime":
					err = s.fetchRealtimeReport(ctx, cfg, opts, prop, results)
				default:
					err = fmt.Errorf("unsupported report type: %s", cfg.reportType)
				}

				if err != nil {
					select {
					case results <- source.RecordBatchResult{Err: err}:
					case <-ctx.Done():
					}
					cancel()
					return
				}
			}
		}()
	}

	go func() {
		defer close(taskChan)
		for _, prop := range s.properties {
			select {
			case taskChan <- prop:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
		cancel()
	}()

	return results, nil
}

func (s *GoogleAnalyticsSource) fetchCustomReport(ctx context.Context, cfg *reportConfig, opts source.ReadOptions, prop propertyInfo, out chan<- source.RecordBatchResult) error {
	dims := cfg.apiDimensions()
	mets := cfg.apiMetrics()

	for offset := int64(0); ; offset += maxRowsPerRequest {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.Properties.RunReport(prop.resource, &analyticsdata.RunReportRequest{
			Dimensions: dims,
			Metrics:    mets,
			Limit:      maxRowsPerRequest,
			Offset:     offset,
			DateRanges: []*analyticsdata.DateRange{{
				StartDate: cfg.startDate.Format("2006-01-02"),
				EndDate:   cfg.endDate.Format("2006-01-02"),
			}},
		}).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("failed to run report for property %d: %w", prop.id, err)
		}

		if len(resp.Rows) == 0 {
			break
		}

		items := rowsToItems(resp.Rows, resp.MetricHeaders, cfg, map[string]any{
			"property_id": strconv.FormatInt(prop.id, 10),
		})
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert to arrow: %w", err)
		}
		select {
		case out <- source.RecordBatchResult{Batch: rec}:
		case <-ctx.Done():
			return ctx.Err()
		}

		config.Debug("[GOOGLE ANALYTICS] Property %d: fetched %d rows (offset: %d)", prop.id, len(resp.Rows), offset)

		if int64(len(resp.Rows)) < maxRowsPerRequest {
			break
		}
	}

	return nil
}

func (s *GoogleAnalyticsSource) fetchRealtimeReport(ctx context.Context, cfg *reportConfig, opts source.ReadOptions, prop propertyInfo, out chan<- source.RecordBatchResult) error {
	resp, err := s.client.Properties.RunRealtimeReport(prop.resource, &analyticsdata.RunRealtimeReportRequest{
		Dimensions:   cfg.apiDimensions(),
		Metrics:      cfg.apiMetrics(),
		Limit:        maxRowsPerRequest,
		MinuteRanges: cfg.minuteRanges,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to run realtime report for property %d: %w", prop.id, err)
	}

	if len(resp.Rows) == 0 {
		return nil
	}

	items := rowsToItems(resp.Rows, resp.MetricHeaders, cfg, map[string]any{
		"ingested_at": time.Now().UTC(),
		"property_id": strconv.FormatInt(prop.id, 10),
	})
	rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert to arrow: %w", err)
	}
	select {
	case out <- source.RecordBatchResult{Batch: rec}:
	case <-ctx.Done():
		return ctx.Err()
	}

	config.Debug("[GOOGLE ANALYTICS] Property %d: fetched %d rows from realtime report", prop.id, len(resp.Rows))

	return nil
}

func parseConnectionURI(uri string) (credJSON []byte, propertyIDs []string, err error) {
	if !strings.HasPrefix(uri, "googleanalytics://") {
		return nil, nil, fmt.Errorf("invalid google analytics URI: must start with googleanalytics://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse google analytics URI: %w", err)
	}

	params := parsed.Query()

	credPath := params.Get("credentials_path")
	credBase64 := params.Get("credentials_base64")

	switch {
	case credPath != "":
		credJSON, err = os.ReadFile(credPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read credentials file: %w", err)
		}
	case credBase64 != "":
		credJSON, err = base64.StdEncoding.DecodeString(credBase64)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decode credentials_base64: %w", err)
		}
	default:
		return nil, nil, fmt.Errorf("credentials_path or credentials_base64 is required to connect to Google Analytics")
	}

	rawPID := params.Get("property_id")
	if rawPID == "" {
		return nil, nil, fmt.Errorf("property_id is required to connect to Google Analytics")
	}

	for _, pid := range strings.Split(rawPID, ",") {
		pid = strings.TrimSpace(pid)
		if pid != "" {
			propertyIDs = append(propertyIDs, pid)
		}
	}
	if len(propertyIDs) == 0 {
		return nil, nil, fmt.Errorf("property_id is required to connect to Google Analytics")
	}

	return credJSON, propertyIDs, nil
}

var dateTimeDimensions = map[string]string{
	"date":           "20060102",
	"dateHour":       "2006010215",
	"dateHourMinute": "200601021504",
}

func buildReportConfig(table string) (*reportConfig, error) {
	parts := strings.Split(table, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return nil, fmt.Errorf("invalid table format, expected <report_type>:<dimensions>:<metrics> or <report_type>:<dimensions>:<metrics>:<minute_ranges>")
	}

	rType := parts[0]
	if _, ok := supportedReportTypes[rType]; !ok {
		return nil, fmt.Errorf("invalid report type %q, available report types: custom, realtime", rType)
	}

	dims := strings.Split(strings.ReplaceAll(parts[1], " ", ""), ",")
	mets := strings.Split(strings.ReplaceAll(parts[2], " ", ""), ",")

	var datetime string
	if rType == "custom" {
		for _, dim := range dims {
			if _, ok := dateTimeDimensions[dim]; ok {
				datetime = dim
				break
			}
		}
		if datetime == "" {
			dtKeys := make([]string, 0, len(dateTimeDimensions))
			for k := range dateTimeDimensions {
				dtKeys = append(dtKeys, k)
			}
			return nil, fmt.Errorf("custom reports must include at least one datetime dimension: %v", dtKeys)
		}
	}

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -30).Truncate(24 * time.Hour)
	end := now

	var minuteRanges []*analyticsdata.MinuteRange
	if len(parts) == 4 {
		var err error
		minuteRanges, err = buildMinuteRanges(parts[3])
		if err != nil {
			return nil, err
		}
	}

	return &reportConfig{
		reportType:   rType,
		dimensions:   dims,
		metrics:      mets,
		datetime:     datetime,
		startDate:    start,
		endDate:      end,
		minuteRanges: minuteRanges,
	}, nil
}

func buildMinuteRanges(raw string) ([]*analyticsdata.MinuteRange, error) {
	segments := strings.Split(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""), ",")
	if len(segments) == 0 || segments[0] == "" {
		return nil, fmt.Errorf("minute ranges must be in start-end format, e.g. 1-2,5-6")
	}

	out := make([]*analyticsdata.MinuteRange, 0, len(segments))
	for _, seg := range segments {
		parts := strings.SplitN(seg, "-", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("minute range %q must be in start-end format", seg)
		}

		endVal, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid minute range %q: values must be numeric", seg)
		}
		startVal, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid minute range %q: values must be numeric", seg)
		}

		out = append(out, &analyticsdata.MinuteRange{
			Name:            fmt.Sprintf("%d_to_%d_minutes_ago", startVal, endVal),
			StartMinutesAgo: startVal,
			EndMinutesAgo:   endVal,
		})
	}

	return out, nil
}

func coerceDimensionValue(name, raw string) any {
	switch name {
	case "date", "dateHour", "dateHourMinute":
		if t, err := time.ParseInLocation(dateTimeDimensions[name], raw, time.UTC); err == nil {
			return t
		}
	}
	return raw
}

func coerceMetricValue(metricType, raw string) any {
	switch metricType {
	case "TYPE_INTEGER":
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return v
		}
	case "METRIC_TYPE_UNSPECIFIED", "TYPE_STRING":
		// string types are returned as-is
	default:
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return v
		}
	}
	return raw
}

var _ source.Source = (*GoogleAnalyticsSource)(nil)
