package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
)

const (
	defaultBaseURL      = "https://api.openai.com"
	usageEndpoint       = "/v1/organization/usage/completions"
	defaultLookbackDays = 30
	maxPageSize         = 31
	maxPages            = 100000
	defaultResultBuffer = 8

	// OpenAI does not publish a fixed Admin Usage API quota; keep requests conservative.
	rateLimit      = 1.0
	rateLimitBurst = 1
)

var supportedTables = []string{"api_usage"}

var supportedGroupBy = []string{
	"project_id",
	"user_id",
	"api_key_id",
	"model",
	"batch",
	"service_tier",
}

var apiUsageColumns = []schema.Column{
	{Name: "bucket_start", DataType: schema.TypeTimestampTZ},
	{Name: "bucket_end", DataType: schema.TypeTimestampTZ},
}

type OpenAISource struct {
	baseURL        string
	platformClient *httpclient.Client
	codexClient    *httpclient.Client
}

type openAICredentials struct {
	apiKey      string
	codexAPIKey string
}

type apiUsageParams struct {
	GroupBy []string `mapstructure:"group_by"`
}

type usagePage struct {
	Data     []usageBucket `json:"data"`
	HasMore  bool          `json:"has_more"`
	NextPage string        `json:"next_page"`
}

type usageBucket struct {
	StartTime json.Number              `json:"start_time"`
	EndTime   json.Number              `json:"end_time"`
	Results   []map[string]interface{} `json:"results"`
}

func NewOpenAISource() *OpenAISource {
	return newOpenAISource(defaultBaseURL)
}

func newOpenAISource(baseURL string) *OpenAISource {
	return &OpenAISource{baseURL: strings.TrimRight(baseURL, "/")}
}

func (s *OpenAISource) Schemes() []string {
	return []string{"openai"}
}

func (s *OpenAISource) HandlesIncrementality() bool {
	return true
}

func (s *OpenAISource) Connect(ctx context.Context, uri string) error {
	credentials, err := parseURI(uri)
	if err != nil {
		return err
	}

	if credentials.apiKey != "" {
		s.platformClient = s.newClient(credentials.apiKey)
	}
	if credentials.codexAPIKey != "" {
		s.codexClient = s.newClient(credentials.codexAPIKey)
	}

	config.Debug("[OPENAI] Connected successfully")
	return nil
}

func (s *OpenAISource) newClient(apiKey string) *httpclient.Client {
	return httpclient.New(
		httpclient.WithBaseURL(s.baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithAuth(httpclient.NewBearerAuth(apiKey)),
		httpclient.WithDebug(config.DebugMode),
	)
}

func parseURI(raw string) (openAICredentials, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return openAICredentials{}, fmt.Errorf("failed to parse openai URI: %w", err)
	}
	if parsed.Scheme != "openai" {
		return openAICredentials{}, fmt.Errorf("invalid openai URI: must start with openai://")
	}
	if parsed.Host != "" || parsed.Path != "" || parsed.User != nil || parsed.Fragment != "" {
		return openAICredentials{}, fmt.Errorf("invalid openai URI: credentials must be query parameters (openai://?api_key=...&codex_api_key=...)")
	}

	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return openAICredentials{}, fmt.Errorf("failed to parse openai URI query: %w", err)
	}
	for key := range values {
		if key != "api_key" && key != "codex_api_key" {
			return openAICredentials{}, fmt.Errorf("unknown openai URI parameter: %s", key)
		}
	}

	apiKey := values.Get("api_key")
	codexAPIKey := values.Get("codex_api_key")
	if apiKey == "" && codexAPIKey == "" {
		return openAICredentials{}, fmt.Errorf("at least one of api_key or codex_api_key is required in openai URI")
	}

	return openAICredentials{apiKey: apiKey, codexAPIKey: codexAPIKey}, nil
}

func (s *OpenAISource) Close(ctx context.Context) error {
	var errs []error
	if s.platformClient != nil {
		errs = append(errs, s.platformClient.Close())
	}
	if s.codexClient != nil {
		errs = append(errs, s.codexClient.Close())
	}
	return errors.Join(errs...)
}

func (s *OpenAISource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName, params, err := parseTableSpec(req.Name)
	if err != nil {
		return nil, err
	}
	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TableIncrementalKey: "bucket_start",
		TableStrategy:       config.StrategyDeleteInsert,
		TablePartitionBy:    "bucket_start",
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("openai source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readAPIUsage(ctx, params, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	return slices.Contains(supportedTables, table)
}

func parseTableSpec(raw string) (string, apiUsageParams, error) {
	var params apiUsageParams
	path, _, err := tablespec.Parse(raw, &params, tablespec.WithListSeparator(","))
	if err != nil {
		return "", apiUsageParams{}, err
	}
	if !isValidTable(path) {
		return path, apiUsageParams{}, nil
	}

	if len(params.GroupBy) == 0 {
		params.GroupBy = []string{"user_id"}
	}

	seen := make(map[string]struct{}, len(params.GroupBy))
	for _, field := range params.GroupBy {
		if !slices.Contains(supportedGroupBy, field) {
			return "", apiUsageParams{}, fmt.Errorf("invalid group_by field %q (supported: %s)", field, strings.Join(supportedGroupBy, ", "))
		}
		if _, ok := seen[field]; ok {
			return "", apiUsageParams{}, fmt.Errorf("duplicate group_by field %q", field)
		}
		seen[field] = struct{}{}
	}

	return path, params, nil
}

func (s *OpenAISource) readAPIUsage(ctx context.Context, params apiUsageParams, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if s.platformClient == nil {
		return nil, fmt.Errorf("api_key is required for table api_usage")
	}

	results := make(chan source.RecordBatchResult, source.RecordBatchBufferSize(opts, defaultResultBuffer))

	go func() {
		defer close(results)
		if err := s.paginateAPIUsage(ctx, s.platformClient, params, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *OpenAISource) paginateAPIUsage(ctx context.Context, client *httpclient.Client, tableParams apiUsageParams, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	start, end, err := usageInterval(opts)
	if err != nil {
		return err
	}
	config.Debug("[OPENAI] Reading api_usage from %s to %s", start.Format(time.RFC3339), end.Format(time.RFC3339))

	params := url.Values{
		"start_time":   {strconv.FormatInt(start.Unix(), 10)},
		"end_time":     {strconv.FormatInt(end.Unix(), 10)},
		"bucket_width": {"1d"},
		"limit":        {strconv.Itoa(maxPageSize)},
		"group_by":     tableParams.GroupBy,
	}

	var pageCursor string
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if pageCursor != "" {
			params.Set("page", pageCursor)
		}

		resp, err := client.R(ctx).SetQueryParamValues(params).Get(usageEndpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch api_usage: %w", err)
		}
		if err := checkResponse(resp); err != nil {
			return err
		}

		payload, err := decodeUsagePage(resp.Body())
		if err != nil {
			return fmt.Errorf("failed to decode api_usage response: %w", err)
		}
		items, err := flattenUsagePage(payload)
		if err != nil {
			return fmt.Errorf("failed to normalize api_usage response: %w", err)
		}
		if err := sendBatch(ctx, items, opts, results); err != nil {
			return err
		}

		if !payload.HasMore {
			return nil
		}
		if payload.NextPage == "" {
			return fmt.Errorf("api_usage response has_more=true but next_page is empty")
		}
		pageCursor = payload.NextPage
	}

	config.Debug("[OPENAI] Stopped api_usage pagination after %d pages", maxPages)
	return fmt.Errorf("api_usage pagination exceeded the %d-page safety limit", maxPages)
}

func usageInterval(opts source.ReadOptions) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -defaultLookbackDays)
	end := now
	if opts.IntervalStart != nil {
		start = opts.IntervalStart.UTC()
	}
	if opts.IntervalEnd != nil {
		end = opts.IntervalEnd.UTC()
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("api_usage interval end must be after interval start")
	}
	return start, end, nil
}

func decodeUsagePage(body []byte) (usagePage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var page usagePage
	if err := decoder.Decode(&page); err != nil {
		return usagePage{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return usagePage{}, fmt.Errorf("response contains multiple JSON values")
		}
		return usagePage{}, err
	}
	return page, nil
}

func flattenUsagePage(page usagePage) ([]map[string]interface{}, error) {
	items := make([]map[string]interface{}, 0)
	for _, bucket := range page.Data {
		start, err := unixTime(bucket.StartTime)
		if err != nil {
			return nil, fmt.Errorf("invalid bucket start_time %q: %w", bucket.StartTime, err)
		}
		end, err := unixTime(bucket.EndTime)
		if err != nil {
			return nil, fmt.Errorf("invalid bucket end_time %q: %w", bucket.EndTime, err)
		}
		for _, result := range bucket.Results {
			item := make(map[string]interface{}, len(result)+2)
			for key, value := range result {
				item[key] = value
			}
			item["bucket_start"] = start
			item["bucket_end"] = end
			items = append(items, item)
		}
	}
	return items, nil
}

func unixTime(value json.Number) (time.Time, error) {
	seconds, err := value.Int64()
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(seconds, 0).UTC(), nil
}

func sendBatch(ctx context.Context, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}
	columns := apiUsageColumns
	if opts.Schema != nil {
		columns = opts.Schema.Columns
	}
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, columns, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert api_usage data to Arrow: %w", err)
	}

	select {
	case <-ctx.Done():
		record.Release()
		return ctx.Err()
	case results <- source.RecordBatchResult{Batch: record}:
		return nil
	}
}

func checkResponse(resp *httpclient.Response) error {
	if resp.IsSuccess() {
		return nil
	}
	switch resp.StatusCode() {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("OpenAI API authentication or organization access failed for api_usage (status %d): %s", resp.StatusCode(), resp.String())
	case http.StatusTooManyRequests:
		return fmt.Errorf("OpenAI API rate limit exceeded for api_usage (status 429): %s", resp.String())
	default:
		return fmt.Errorf("OpenAI API error for api_usage (status %d): %s", resp.StatusCode(), resp.String())
	}
}
