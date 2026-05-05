package allium

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	baseURL = "https://api.allium.so"

	// Allium advertises up to 1K+ RPS at peak. We use a conservative limit.
	rateLimit      = 10
	rateLimitBurst = 5

	maxRows      = 250000
	pollInterval = 5 * time.Second
	pollTimeout  = 12 * time.Hour
)

var supportedTables = []string{
	"query",
}

type AlliumSource struct {
	apiKey string
	client *ingestrhttp.Client
}

func NewAlliumSource() *AlliumSource {
	return &AlliumSource{}
}

func (s *AlliumSource) HandlesIncrementality() bool {
	return true
}

func (s *AlliumSource) Schemes() []string {
	return []string{"allium"}
}

func (s *AlliumSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey

	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithHeader("X-API-KEY", s.apiKey),
		ingestrhttp.WithHeader("Content-Type", "application/json"),
	)
	config.Debug("[ALLIUM] Connected successfully")
	return nil
}

func (s *AlliumSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *AlliumSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table format: %s (expected: query:<query_id> or query:<query_id>:<params>)", tableName)
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    nil,
		TableIncrementalKey: "",
		TableStrategy:       config.StrategyReplace,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("allium source does not have a known schema")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func parseURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "allium://") {
		return "", fmt.Errorf("invalid allium URI: must start with allium://")
	}

	rest := strings.TrimPrefix(uri, "allium://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in URI query parameters")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse allium URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key query parameter is required")
	}

	return apiKey, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		prefix := t + ":"
		if strings.HasPrefix(table, prefix) && len(table) > len(prefix) {
			return true
		}
	}
	return false
}

func (s *AlliumSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if !isValidTable(table) {
		return nil, fmt.Errorf("unsupported table format: %s (supported prefixes: %s)", table, strings.Join(supportedTables, ", "))
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		tableType := table[:strings.Index(table, ":")]
		switch tableType {
		case "query":
			err = s.readQuery(ctx, opts, results, table)
		default:
			err = fmt.Errorf("unsupported table type: %s", tableType)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func parseTable(table string) (queryID string, params map[string]interface{}) {
	parts := strings.SplitN(table, ":", 3)
	queryID = parts[1]
	params = make(map[string]interface{})
	if len(parts) == 3 && parts[2] != "" {
		values, err := url.ParseQuery(parts[2])
		if err == nil {
			for k, v := range values {
				if len(v) > 0 {
					params[k] = v[0]
				}
			}
		}
	}
	return queryID, params
}

func (s *AlliumSource) buildParameters(opts source.ReadOptions, userParams map[string]interface{}) map[string]interface{} {
	params := make(map[string]interface{})

	if opts.IntervalStart != nil {
		params["start_date"] = opts.IntervalStart.Format("2006-01-02")
		params["start_timestamp"] = fmt.Sprintf("%d", opts.IntervalStart.Unix())
	}
	if opts.IntervalEnd != nil {
		params["end_date"] = opts.IntervalEnd.Format("2006-01-02")
		params["end_timestamp"] = fmt.Sprintf("%d", opts.IntervalEnd.Unix())
	}

	for k, v := range userParams {
		params[k] = v
	}

	return params
}

type runAsyncRequest struct {
	Parameters map[string]interface{} `json:"parameters"`
	RunConfig  map[string]interface{} `json:"run_config,omitempty"`
}

type runAsyncResponse struct {
	RunID string `json:"run_id"`
}

type queryResultsResponse struct {
	Data []map[string]interface{} `json:"data"`
	Meta struct {
		Columns []struct {
			Name     string `json:"name"`
			DataType string `json:"data_type"`
		} `json:"columns"`
	} `json:"meta"`
}

func (s *AlliumSource) readQuery(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, table string) error {
	config.Debug("[ALLIUM] reading %s", table)

	queryID, userParams := parseTable(table)

	runConfigKeys := map[string]bool{"limit": true, "compute_profile": true}
	limit := maxRows
	if opts.Limit > 0 && opts.Limit < maxRows {
		limit = opts.Limit
	}
	runConfig := map[string]interface{}{
		"limit": limit,
	}
	queryParams := make(map[string]interface{})
	for k, v := range userParams {
		if runConfigKeys[k] {
			if k == "limit" {
				if n, err := strconv.Atoi(v.(string)); err == nil {
					runConfig[k] = n
				} else {
					runConfig[k] = v
				}
			} else {
				runConfig[k] = v
			}
		} else {
			queryParams[k] = v
		}
	}

	params := s.buildParameters(opts, queryParams)

	reqBody := runAsyncRequest{
		Parameters: params,
		RunConfig:  runConfig,
	}

	var runResp runAsyncResponse
	resp, err := s.client.R(ctx).
		SetBody(reqBody).
		SetResult(&runResp).
		Post(fmt.Sprintf("/api/v1/explorer/queries/%s/run-async", queryID))
	if err != nil {
		return fmt.Errorf("failed to start query: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("failed to start query (status %d): %s", resp.StatusCode(), resp.String())
	}

	if runResp.RunID == "" {
		if err := json.Unmarshal(resp.Body(), &runResp); err != nil {
			return fmt.Errorf("failed to parse run response: %w", err)
		}
	}

	config.Debug("[ALLIUM] query started, run_id=%s", runResp.RunID)

	if err := s.pollUntilDone(ctx, runResp.RunID); err != nil {
		return err
	}

	return s.fetchResults(ctx, runResp.RunID, opts, results)
}

func (s *AlliumSource) pollUntilDone(ctx context.Context, runID string) error {
	deadline := time.Now().Add(pollTimeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("query timed out after %v", pollTimeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}

		var statusResp string
		resp, err := s.client.R(ctx).
			SetResult(&statusResp).
			Get(fmt.Sprintf("/api/v1/explorer/query-runs/%s/status", runID))
		if err != nil {
			return fmt.Errorf("failed to poll query status: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("failed to poll query status (status %d): %s", resp.StatusCode(), resp.String())
		}

		if statusResp == "" {
			if err := json.Unmarshal(resp.Body(), &statusResp); err != nil {
				return fmt.Errorf("failed to parse status response: %w", err)
			}
		}

		config.Debug("[ALLIUM] run %s status: %s", runID, statusResp)

		switch statusResp {
		case "success":
			return nil
		case "failed":
			return fmt.Errorf("query execution failed (run_id=%s)", runID)
		case "canceled":
			return fmt.Errorf("query was canceled (run_id=%s)", runID)
		case "created", "queued", "running":
			continue
		default:
			return fmt.Errorf("unexpected query status: %s", statusResp)
		}
	}
}

func (s *AlliumSource) fetchResults(ctx context.Context, runID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	var queryResults queryResultsResponse
	resp, err := s.client.R(ctx).
		SetQueryParam("f", "json").
		SetResult(&queryResults).
		Get(fmt.Sprintf("/api/v1/explorer/query-runs/%s/results", runID))
	if err != nil {
		return fmt.Errorf("failed to fetch query results: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("failed to fetch query results (status %d): %s", resp.StatusCode(), resp.String())
	}

	if queryResults.Data == nil {
		if err := json.Unmarshal(resp.Body(), &queryResults); err != nil {
			return fmt.Errorf("failed to parse query results: %w", err)
		}
	}

	if len(queryResults.Data) == 0 {
		config.Debug("[ALLIUM] query returned 0 rows")
		return nil
	}

	config.Debug("[ALLIUM] fetched %d rows", len(queryResults.Data))

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}

	totalSent := 0
	for i := 0; i < len(queryResults.Data); i += batchSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := min(i+batchSize, len(queryResults.Data))

		record, err := arrowconv.ItemsToArrowRecordWithSchema(queryResults.Data[i:end], nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert to Arrow: %w", err)
		}

		results <- source.RecordBatchResult{Batch: record}
		totalSent += end - i
		config.Debug("[ALLIUM] sent batch of %d rows (total: %d/%d)", end-i, totalSent, len(queryResults.Data))
	}

	return nil
}
