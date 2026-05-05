package applovinmax

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	baseURL = "https://r.applovin.com"
	// No documented rate limit for the MAX reporting API; using a conservative default.
	rateLimit      = 5.0
	rateLimitBurst = 5
	defaultDays    = 30
	workerCount    = 5
)

var supportedTables = []string{"user_ad_revenue"}

var platforms = []string{"ios", "android", "fireos"}

type AppLovinMaxSource struct {
	apiKey       string
	applications []string
	client       *gonghttp.Client
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
	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
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
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

type fetchTask struct {
	app      string
	date     string
	platform string
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

	taskCh := make(chan fetchTask, len(tasks))
	var wg sync.WaitGroup

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = workerCount
	}

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

				items, err := s.fetchDayPlatform(ctx, task.app, task.date, task.platform)
				if err != nil {
					errCh <- err
					cancel()
					return
				}

				if len(items) == 0 {
					continue
				}

				schemaCols := []schema.Column{
					{Name: "partition_date", DataType: schema.TypeDate, Nullable: false},
				}

				record, err := arrowconv.ItemsToArrowRecordWithSchema(items, schemaCols, opts.ExcludeColumns)
				if err != nil {
					errCh <- fmt.Errorf("failed to convert user_ad_revenue to Arrow: %w", err)
					cancel()
					return
				}

				select {
				case results <- source.RecordBatchResult{Batch: record}:
				case <-ctx.Done():
					return
				}
				config.Debug("[APPLOVINMAX] sent %d records for app=%s date=%s platform=%s", len(items), task.app, task.date, task.platform)
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

func (s *AppLovinMaxSource) fetchDayPlatform(ctx context.Context, app, date, platform string) ([]map[string]interface{}, error) {
	resp, err := s.client.R(ctx).
		SetQueryParam("api_key", s.apiKey).
		SetQueryParam("date", date).
		SetQueryParam("platform", platform).
		SetQueryParam("application", app).
		SetQueryParam("aggregated", "false").
		Get("/max/userAdRevenueReport")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user_ad_revenue for app=%s date=%s platform=%s: %w", app, date, platform, err)
	}

	if resp.StatusCode() == http.StatusNotFound {
		if strings.Contains(resp.String(), "No Mediation App Id found for platform") {
			config.Debug("[APPLOVINMAX] no data for app=%s platform=%s (not configured), skipping", app, platform)
			return nil, nil
		}
		if strings.Contains(resp.String(), "Data does not exist for specified date") {
			config.Debug("[APPLOVINMAX] no data for app=%s date=%s platform=%s (no data for date), skipping", app, date, platform)
			return nil, nil
		}
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("applovinmax API returned status %d for app=%s date=%s platform=%s: %s", resp.StatusCode(), app, date, platform, resp.String())
	}

	var body map[string]interface{}
	if err := resp.JSON(&body); err != nil {
		return nil, fmt.Errorf("failed to parse user_ad_revenue response: %w", err)
	}

	csvURL, ok := body["ad_revenue_report_url"].(string)
	if !ok || csvURL == "" {
		config.Debug("[APPLOVINMAX] no ad_revenue_report_url for app=%s date=%s platform=%s", app, date, platform)
		return nil, nil
	}

	items, err := s.downloadCSV(ctx, csvURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download CSV for app=%s date=%s platform=%s: %w", app, date, platform, err)
	}

	for _, item := range items {
		item["partition_date"] = date
		item["platform"] = platform
	}

	return items, nil
}

func (s *AppLovinMaxSource) downloadCSV(ctx context.Context, csvURL string) ([]map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, csvURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSV request: %w", err)
	}

	httpClient := &http.Client{Timeout: 120 * time.Second}
	httpResp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download CSV: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CSV download returned status %d", httpResp.StatusCode)
	}

	reader := csv.NewReader(httpResp.Body)

	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	var items []map[string]any
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV row: %w", err)
		}

		item := make(map[string]any, len(headers)+2)
		for i, header := range headers {
			if i < len(record) {
				val := strings.TrimSpace(record[i])
				if val == "" {
					continue
				}
				item[header] = tryParseNumeric(val)
			}
		}
		items = append(items, item)
	}

	return items, nil
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
