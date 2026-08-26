package applovinmax

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://r.applovin.com"
	// No documented rate limit for the MAX reporting API; using a conservative default.
	rateLimit      = 5.0
	rateLimitBurst = 5
	defaultDays    = 30
	workerCount    = 5
	maxBatchRows   = 5_000
	maxBatchBytes  = 16 << 20
)

var supportedTables = []string{"user_ad_revenue"}

var platforms = []string{"ios", "android", "fireos"}

type AppLovinMaxSource struct {
	apiKey       string
	applications []string
	client       *httpclient.Client
}

func NewAppLovinMaxSource() *AppLovinMaxSource {
	return &AppLovinMaxSource{}
}

func (s *AppLovinMaxSource) Schemes() []string {
	return []string{"applovinmax"}
}

func (s *AppLovinMaxSource) HandlesIncrementality() bool {
	return true
}

func (s *AppLovinMaxSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey
	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
	)

	config.Debug("[APPLOVINMAX] Connected successfully")
	return nil
}

func (s *AppLovinMaxSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "applovinmax://") {
		return "", fmt.Errorf("invalid applovinmax URI: must start with applovinmax://")
	}

	rest := strings.TrimPrefix(uri, "applovinmax://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in applovinmax URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse applovinmax URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in applovinmax URI")
	}

	return apiKey, nil
}

func parseTableName(table string) (string, []string, error) {
	parts := strings.SplitN(table, ":", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid table format: expected user_ad_revenue:<app_id1>,<app_id2>, got %q", table)
	}

	tableName := parts[0]
	if !isValidTable(tableName) {
		return "", nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	rawApps := strings.ReplaceAll(parts[1], " ", "")
	var apps []string
	for _, a := range strings.Split(rawApps, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			apps = append(apps, a)
		}
	}

	if len(apps) == 0 {
		return "", nil, fmt.Errorf("at least one application id is required")
	}

	seen := make(map[string]bool, len(apps))
	for _, a := range apps {
		if seen[a] {
			return "", nil, fmt.Errorf("duplicate application id: %s", a)
		}
		seen[a] = true
	}

	return tableName, apps, nil
}

func isValidTable(name string) bool {
	for _, t := range supportedTables {
		if t == name {
			return true
		}
	}
	return false
}

func (s *AppLovinMaxSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName, apps, err := parseTableName(req.Name)
	if err != nil {
		return nil, err
	}

	s.applications = apps

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    nil,
		TableIncrementalKey: "partition_date",
		TableStrategy:       config.StrategyDeleteInsert,
		TablePartitionBy:    "partition_date",
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("applovinmax source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, opts)
		},
	}, nil
}

func (s *AppLovinMaxSource) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		if err := s.readUserAdRevenue(ctx, opts, results); err != nil {
			select {
			case results <- source.RecordBatchResult{Err: err}:
			case <-ctx.Done():
			}
		}
	}()

	return results, nil
}

type fetchTask struct {
	app      string
	date     string
	platform string
}

type rowLimiter struct {
	limit int64
	used  atomic.Int64
	done  chan struct{}
	once  sync.Once
}

func newRowLimiter(limit int) *rowLimiter {
	return &rowLimiter{
		limit: int64(limit),
		done:  make(chan struct{}),
	}
}

func (l *rowLimiter) take() bool {
	if l.limit <= 0 {
		return true
	}

	for {
		used := l.used.Load()
		if used >= l.limit {
			return false
		}
		if l.used.CompareAndSwap(used, used+1) {
			if used+1 == l.limit {
				l.once.Do(func() { close(l.done) })
			}
			return true
		}
	}
}

func (l *rowLimiter) exhausted() bool {
	return l.limit > 0 && l.used.Load() >= l.limit
}

func (l *rowLimiter) doneCh() <-chan struct{} {
	if l.limit <= 0 {
		return nil
	}
	return l.done
}

func (s *AppLovinMaxSource) readUserAdRevenue(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[APPLOVINMAX] reading user_ad_revenue")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	startDate, endDate := resolveDateRange(opts.IntervalStart, opts.IntervalEnd)
	config.Debug("[APPLOVINMAX] date range: %s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	var tasks []fetchTask
	for _, app := range s.applications {
		for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
			dateStr := d.Format("2006-01-02")
			for _, platform := range platforms {
				tasks = append(tasks, fetchTask{app: app, date: dateStr, platform: platform})
			}
		}
	}

	config.Debug("[APPLOVINMAX] %d fetch tasks across %d apps, %d platforms", len(tasks), len(s.applications), len(platforms))

	taskCh := make(chan fetchTask)
	var wg sync.WaitGroup

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = workerCount
	}
	parallelism = min(parallelism, workerCount)
	limiter := newRowLimiter(opts.Limit)

	errCh := make(chan error, parallelism)
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if limiter.exhausted() {
					return
				}

				rows, err := s.fetchDayPlatform(ctx, task.app, task.date, task.platform, opts, limiter, results)
				if err != nil {
					select {
					case errCh <- err:
					case <-ctx.Done():
					}
					cancel()
					return
				}
				config.Debug("[APPLOVINMAX] sent %d records for app=%s date=%s platform=%s", rows, task.app, task.date, task.platform)
			}
		}()
	}

	go func() {
		defer close(taskCh)
		for _, task := range tasks {
			select {
			case taskCh <- task:
			case <-ctx.Done():
				return
			case <-limiter.doneCh():
				return
			}
		}
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *AppLovinMaxSource) fetchDayPlatform(
	ctx context.Context,
	app, date, platform string,
	opts source.ReadOptions,
	limiter *rowLimiter,
	results chan<- source.RecordBatchResult,
) (int, error) {
	resp, err := s.client.R(ctx).
		SetQueryParam("api_key", s.apiKey).
		SetQueryParam("date", date).
		SetQueryParam("platform", platform).
		SetQueryParam("application", app).
		SetQueryParam("aggregated", "false").
		Get("/max/userAdRevenueReport")
	if err != nil {
		return 0, fmt.Errorf("failed to fetch user_ad_revenue for app=%s date=%s platform=%s: %w", app, date, platform, err)
	}

	if resp.StatusCode() == http.StatusNotFound {
		if strings.Contains(resp.String(), "No Mediation App Id found for platform") {
			config.Debug("[APPLOVINMAX] no data for app=%s platform=%s (not configured), skipping", app, platform)
			return 0, nil
		}
		if strings.Contains(resp.String(), "Data does not exist for specified date") {
			config.Debug("[APPLOVINMAX] no data for app=%s date=%s platform=%s (no data for date), skipping", app, date, platform)
			return 0, nil
		}
	}

	if !resp.IsSuccess() {
		return 0, fmt.Errorf("applovinmax API returned status %d for app=%s date=%s platform=%s: %s", resp.StatusCode(), app, date, platform, resp.String())
	}

	var body map[string]interface{}
	if err := resp.JSON(&body); err != nil {
		return 0, fmt.Errorf("failed to parse user_ad_revenue response: %w", err)
	}

	csvURL, ok := body["ad_revenue_report_url"].(string)
	if !ok || csvURL == "" {
		config.Debug("[APPLOVINMAX] no ad_revenue_report_url for app=%s date=%s platform=%s", app, date, platform)
		return 0, nil
	}

	rows, err := s.downloadCSV(ctx, csvURL, date, platform, opts, limiter, results)
	if err != nil {
		return 0, fmt.Errorf("failed to download CSV for app=%s date=%s platform=%s: %w", app, date, platform, err)
	}

	return rows, nil
}

func (s *AppLovinMaxSource) downloadCSV(
	ctx context.Context,
	csvURL, date, platform string,
	opts source.ReadOptions,
	limiter *rowLimiter,
	results chan<- source.RecordBatchResult,
) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, csvURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create CSV request: %w", err)
	}

	httpClient := &http.Client{Timeout: 120 * time.Second}
	httpResp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to download CSV: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("CSV download returned status %d", httpResp.StatusCode)
	}

	return streamCSV(ctx, httpResp.Body, date, platform, opts, limiter, results)
}

type csvFieldSource struct {
	indexes []int
	value   any
	static  bool
}

type csvBatchBuilder struct {
	schema  *arrow.Schema
	rb      *array.RecordBuilder
	sources []csvFieldSource
	buf     []byte
	rows    int
	bytes   int64
}

func newCSVBatchBuilder(headers []string, date, platform string, excludeColumns []string) *csvBatchBuilder {
	exclude := make(map[string]bool, len(excludeColumns))
	for _, column := range excludeColumns {
		exclude[strings.ToLower(column)] = true
	}

	headerIndexes := make(map[string][]int, len(headers))
	for i, header := range headers {
		headerIndexes[header] = append(headerIndexes[header], i)
	}

	columnNames := make([]string, 0, len(headerIndexes)+1)
	for name := range headerIndexes {
		if name != "partition_date" && name != "platform" {
			columnNames = append(columnNames, name)
		}
	}
	columnNames = append(columnNames, "platform")
	sort.Strings(columnNames)

	fields := make([]arrow.Field, 0, len(columnNames)+1)
	sources := make([]csvFieldSource, 0, len(columnNames)+1)
	if !exclude["partition_date"] {
		fields = append(fields, arrow.Field{Name: "partition_date", Type: schema.DataTypeToArrowType(schema.Column{DataType: schema.TypeDate}), Nullable: false})
		sources = append(sources, csvFieldSource{value: date, static: true})
	}
	for _, name := range columnNames {
		if exclude[strings.ToLower(name)] {
			continue
		}
		fields = append(fields, arrow.Field{Name: name, Type: schema.UnknownArrowType, Nullable: true})
		if name == "platform" {
			sources = append(sources, csvFieldSource{value: platform, static: true})
		} else {
			sources = append(sources, csvFieldSource{indexes: headerIndexes[name]})
		}
	}

	arrowSchema := arrow.NewSchema(fields, nil)
	return &csvBatchBuilder{
		schema:  arrowSchema,
		rb:      array.NewRecordBuilder(memory.NewGoAllocator(), arrowSchema),
		sources: sources,
	}
}

func (b *csvBatchBuilder) appendRow(record []string, rowBytes int64) {
	for i, fieldSource := range b.sources {
		builder := b.rb.Field(i)
		if fieldSource.static {
			b.appendValue(builder, fieldSource.value)
			continue
		}

		value, ok := lastNonEmptyCSVValue(record, fieldSource.indexes)
		if !ok {
			builder.AppendNull()
			continue
		}
		b.appendValue(builder, tryParseNumeric(strings.TrimSpace(value)))
	}
	b.rows++
	b.bytes += rowBytes
}

func (b *csvBatchBuilder) appendValue(builder array.Builder, value any) {
	extensionBuilder, ok := builder.(*array.ExtensionBuilder)
	if !ok {
		arrowconv.AppendValue(builder, value)
		return
	}
	stringBuilder := extensionBuilder.StorageBuilder().(*array.StringBuilder)
	switch typed := value.(type) {
	case json.Number:
		stringBuilder.Append(string(typed))
	case string:
		b.appendJSONString(stringBuilder, typed)
	default:
		arrowconv.AppendValue(builder, value)
	}
}

func (b *csvBatchBuilder) appendJSONString(builder *array.StringBuilder, value string) {
	if !utf8.ValidString(value) {
		arrowconv.AppendUnknownValue(builder, value)
		return
	}
	for i := 0; i < len(value); i++ {
		if char := value[i]; char == '"' || char == '\\' || char < 0x20 {
			arrowconv.AppendUnknownValue(builder, value)
			return
		}
	}
	b.buf = append(b.buf[:0], '"')
	b.buf = append(b.buf, value...)
	b.buf = append(b.buf, '"')
	builder.BinaryBuilder.Append(b.buf)
}

func (b *csvBatchBuilder) finish() arrow.RecordBatch {
	rows := b.rows
	b.rows = 0
	b.bytes = 0
	if len(b.sources) == 0 {
		return array.NewRecordBatch(b.schema, nil, int64(rows))
	}
	return b.rb.NewRecordBatch()
}

func (b *csvBatchBuilder) release() {
	b.rb.Release()
}

func streamCSV(
	ctx context.Context,
	input io.Reader,
	date, platform string,
	opts source.ReadOptions,
	limiter *rowLimiter,
	results chan<- source.RecordBatchResult,
) (int, error) {
	reader := csv.NewReader(input)

	headers, err := reader.Read()
	if err != nil {
		return 0, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	batchRows, batchBytes := effectiveBatchLimits(opts)
	builder := newCSVBatchBuilder(headers, date, platform, opts.ExcludeColumns)
	defer builder.release()

	totalRows := 0
	flush := func() error {
		if builder.rows == 0 {
			return nil
		}
		record := builder.finish()
		rows := int(record.NumRows())
		select {
		case results <- source.RecordBatchResult{Batch: record}:
			totalRows += rows
			return nil
		case <-ctx.Done():
			record.Release()
			return ctx.Err()
		}
	}

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return totalRows, fmt.Errorf("failed to read CSV row: %w", err)
		}

		if !limiter.take() {
			break
		}

		rowBytes := estimateCSVRowBytes(record, date, platform)
		if builder.rows > 0 && (builder.rows >= batchRows || builder.bytes+rowBytes > batchBytes) {
			if err := flush(); err != nil {
				return totalRows, err
			}
		}
		builder.appendRow(record, rowBytes)

		if builder.rows >= batchRows || limiter.exhausted() {
			if err := flush(); err != nil {
				return totalRows, err
			}
		}
		if limiter.exhausted() {
			break
		}
	}

	if err := flush(); err != nil {
		return totalRows, err
	}
	return totalRows, nil
}

func effectiveBatchLimits(opts source.ReadOptions) (int, int64) {
	rows := opts.PageSize
	if rows <= 0 || rows > maxBatchRows {
		rows = maxBatchRows
	}
	bytes := opts.MaxBatchBytes
	if bytes <= 0 || bytes > maxBatchBytes {
		bytes = maxBatchBytes
	}
	return rows, bytes
}

func lastNonEmptyCSVValue(record []string, indexes []int) (string, bool) {
	for i := len(indexes) - 1; i >= 0; i-- {
		index := indexes[i]
		if index < len(record) && strings.TrimSpace(record[index]) != "" {
			return record[index], true
		}
	}
	return "", false
}

// estimateCSVRowBytes approximates a row's content size for the byte-bounded
// batcher: the CSV cells plus the injected partition_date/platform values.
func estimateCSVRowBytes(record []string, date, platform string) int64 {
	return arrowconv.CellsBytes(record) + int64(len(date)+len(platform))
}

func resolveDateRange(intervalStart, intervalEnd interface{}) (time.Time, time.Time) {
	now := time.Now().UTC()
	yesterday := now.AddDate(0, 0, -1)

	start := parseTimestamp(intervalStart)
	end := parseTimestamp(intervalEnd)

	if start.IsZero() {
		start = now.AddDate(0, 0, -defaultDays)
	}

	if end.IsZero() {
		end = yesterday
	}

	start = truncateToDate(start)
	end = truncateToDate(end)

	if end.Before(start) {
		end = start
	}

	return start, end
}

func parseTimestamp(value interface{}) time.Time {
	if value == nil {
		return time.Time{}
	}
	switch v := value.(type) {
	case time.Time:
		return v
	case *time.Time:
		if v != nil {
			return *v
		}
	}
	return time.Time{}
}

func tryParseNumeric(s string) any {
	if s == "" {
		return s
	}
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return json.Number(s)
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return json.Number(s)
	}
	return s
}

func truncateToDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

var _ source.Source = (*AppLovinMaxSource)(nil)
