package dune

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL         = "https://api.dune.com/api/v1"
	defaultPageSize = 1000
	pollInterval    = 5 * time.Second
	maxPollRetries  = 8640 // 12 hours at 5s intervals
	rateLimit       = 0.53
	rateLimitBurst  = 5
)

var supportedTables = []string{
	"queries",
}

type DuneSource struct {
	client      *httpclient.Client
	apiKey      string
	performance string
}

func NewDuneSource() *DuneSource {
	return &DuneSource{}
}

func (s *DuneSource) Schemes() []string {
	return []string{"dune"}
}

func (s *DuneSource) HandlesIncrementality() bool {
	return false
}

func (s *DuneSource) Connect(ctx context.Context, uri string) error {
	apiKey, performance, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey
	s.performance = performance

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(120*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithRetry(5, 5*time.Second, 60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewAPIKeyAuth("X-DUNE-API-KEY", s.apiKey, true)),
	)

	config.Debug("[DUNE] Connected with performance=%s", s.performance)
	return nil
}

func parseURI(uri string) (apiKey, performance string, err error) {
	if !strings.HasPrefix(uri, "dune://") {
		return "", "", fmt.Errorf("invalid dune URI: must start with dune://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse Dune URI: %w", err)
	}

	params := parsed.Query()

	apiKey = params.Get("api_key")
	if apiKey == "" {
		return "", "", fmt.Errorf("api_key is required in the Dune URI, expected format: dune://?api_key=<api_key>&performance=<medium|large>")
	}

	performance = params.Get("performance")
	if performance == "" {
		performance = "medium"
	}

	return apiKey, performance, nil
}

func (s *DuneSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *DuneSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s, supported tables: %v, or use 'query:<id>' / 'sql:<query>' formats", tableName, supportedTables)
	}

	return &source.DynamicSourceTable{
		TableName:        tableName,
		TablePrimaryKeys: primaryKeysForTable(tableName),
		TableStrategy:    config.StrategyReplace,
		KnownSchema:      false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("dune source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(name string) bool {
	if strings.HasPrefix(name, "query:") || strings.HasPrefix(name, "sql:") {
		return true
	}
	for _, t := range supportedTables {
		if t == name {
			return true
		}
	}
	return false
}

func primaryKeysForTable(name string) []string {
	if name == "queries" {
		return []string{"id"}
	}
	return nil
}

func (s *DuneSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch {
		case strings.HasPrefix(table, "query:"):
			err = s.readExecuteQuery(ctx, table, opts, results)
		case strings.HasPrefix(table, "sql:"):
			err = s.readExecuteSQL(ctx, table, opts, results)
		case table == "queries":
			err = s.readQueries(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *DuneSource) readExecuteSQL(ctx context.Context, table string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	sql := strings.TrimPrefix(table, "sql:")
	if sql == "" {
		return fmt.Errorf("SQL query cannot be empty in 'sql:' table format")
	}

	config.Debug("[DUNE] executing SQL query")

	payload := map[string]any{
		"sql":         sql,
		"performance": s.performance,
	}

	resp, err := s.client.R(ctx).SetBody(payload).Post("/sql/execute")
	if err != nil {
		return fmt.Errorf("failed to execute SQL: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("dune API sql/execute returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var execResult struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := json.Unmarshal(resp.Body(), &execResult); err != nil {
		return fmt.Errorf("failed to parse execution response: %w", err)
	}
	if execResult.ExecutionID == "" {
		return fmt.Errorf("failed to start query execution: no execution_id returned")
	}

	if err := s.pollExecution(ctx, execResult.ExecutionID); err != nil {
		return err
	}

	return s.fetchResults(ctx, execResult.ExecutionID, opts, results)
}

func (s *DuneSource) readExecuteQuery(ctx context.Context, table string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	rest := strings.TrimPrefix(table, "query:")
	parts := strings.SplitN(rest, ":", 2)
	queryID := parts[0]
	if queryID == "" {
		return fmt.Errorf("query ID cannot be empty in 'query:<id>' table format")
	}

	config.Debug("[DUNE] executing saved query %s", queryID)

	now := time.Now().UTC()
	defaultStart := now.AddDate(0, 0, -2).Truncate(24 * time.Hour)
	defaultEnd := now.AddDate(0, 0, -1).Truncate(24 * time.Hour)

	if opts.IntervalStart != nil {
		defaultStart = *opts.IntervalStart
	}
	if opts.IntervalEnd != nil {
		defaultEnd = *opts.IntervalEnd
	}

	queryParams := map[string]string{
		"start_date":      defaultStart.Format("2006-01-02"),
		"end_date":        defaultEnd.Format("2006-01-02"),
		"start_timestamp": strconv.FormatInt(defaultStart.Unix(), 10),
		"end_timestamp":   strconv.FormatInt(defaultEnd.Unix(), 10),
	}

	if len(parts) == 2 && parts[1] != "" {
		parsed, err := url.ParseQuery(parts[1])
		if err == nil {
			for k, vs := range parsed {
				if len(vs) > 0 {
					queryParams[k] = vs[0]
				}
			}
		}
	}

	payload := map[string]any{
		"performance":      s.performance,
		"query_parameters": queryParams,
	}

	resp, err := s.client.R(ctx).SetBody(payload).Post(fmt.Sprintf("/query/%s/execute", queryID))
	if err != nil {
		return fmt.Errorf("failed to execute query %s: %w", queryID, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("dune API query/%s/execute returned status %d: %s", queryID, resp.StatusCode(), resp.String())
	}

	var execResult struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := json.Unmarshal(resp.Body(), &execResult); err != nil {
		return fmt.Errorf("failed to parse execution response: %w", err)
	}
	if execResult.ExecutionID == "" {
		return fmt.Errorf("failed to start query execution: no execution_id returned")
	}

	if err := s.pollExecution(ctx, execResult.ExecutionID); err != nil {
		return err
	}

	return s.fetchResults(ctx, execResult.ExecutionID, opts, results)
}

func (s *DuneSource) readQueries(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[DUNE] reading queries")

	offset := 0
	pageLimit := 100
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("limit", strconv.Itoa(pageLimit)).
			SetQueryParam("offset", strconv.Itoa(offset)).
			Get("/queries")
		if err != nil {
			return fmt.Errorf("failed to fetch queries: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("dune API queries returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var result struct {
			Queries []map[string]any `json:"queries"`
			Total   int              `json:"total"`
		}
		if err := json.Unmarshal(resp.Body(), &result); err != nil {
			return fmt.Errorf("failed to parse queries response: %w", err)
		}

		if len(result.Queries) == 0 {
			break
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(result.Queries, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build arrow record for queries: %w", err)
		}

		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(result.Queries)
		config.Debug("[DUNE] Sent %d queries (total: %d)", len(result.Queries), totalSent)

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}

		offset += len(result.Queries)
		if offset >= result.Total {
			break
		}
	}

	config.Debug("[DUNE] Finished reading queries: %d total records", totalSent)
	return nil
}

func (s *DuneSource) pollExecution(ctx context.Context, executionID string) error {
	config.Debug("[DUNE] Polling execution %s", executionID)

	for i := 0; i < maxPollRetries; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).Get(fmt.Sprintf("/execution/%s/status", executionID))
		if err != nil {
			return fmt.Errorf("failed to poll execution status: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("dune API execution status returned %d: %s", resp.StatusCode(), resp.String())
		}

		var status struct {
			State string `json:"state"`
			Error any    `json:"error"`
		}
		if err := json.Unmarshal(resp.Body(), &status); err != nil {
			return fmt.Errorf("failed to parse execution status: %w", err)
		}

		switch status.State {
		case "QUERY_STATE_COMPLETED":
			config.Debug("[DUNE] Execution %s completed", executionID)
			return nil
		case "QUERY_STATE_FAILED":
			errMsg := "Unknown error"
			if m, ok := status.Error.(map[string]any); ok {
				if msg, ok := m["message"].(string); ok {
					errMsg = msg
				}
			} else if errStr, ok := status.Error.(string); ok {
				errMsg = errStr
			}
			return fmt.Errorf("query execution failed: %s", errMsg)
		case "QUERY_STATE_CANCELLED":
			return fmt.Errorf("query execution was cancelled")
		case "QUERY_STATE_EXPIRED":
			return fmt.Errorf("query execution expired")
		case "QUERY_STATE_PENDING", "QUERY_STATE_EXECUTING":
			config.Debug("[DUNE] Execution %s state: %s, waiting...", executionID, status.State)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pollInterval):
			}
		default:
			return fmt.Errorf("unknown query state: %s", status.State)
		}
	}

	return fmt.Errorf("query execution timed out after %v", time.Duration(maxPollRetries)*pollInterval)
}

func (s *DuneSource) fetchResults(ctx context.Context, executionID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	offset := 0
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("limit", strconv.Itoa(defaultPageSize)).
			SetQueryParam("offset", strconv.Itoa(offset)).
			Get(fmt.Sprintf("/execution/%s/results", executionID))
		if err != nil {
			return fmt.Errorf("failed to fetch results: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("dune API results returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var resultData struct {
			Result struct {
				Rows []map[string]any `json:"rows"`
			} `json:"result"`
			NextOffset *int `json:"next_offset"`
		}
		if err := json.Unmarshal(resp.Body(), &resultData); err != nil {
			return fmt.Errorf("failed to parse results response: %w", err)
		}

		rows := resultData.Result.Rows
		if len(rows) == 0 {
			break
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build arrow record: %w", err)
		}

		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(rows)
		config.Debug("[DUNE] Sent %d result rows (total: %d)", len(rows), totalSent)

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}

		if resultData.NextOffset == nil {
			break
		}
		offset = *resultData.NextOffset
	}

	config.Debug("[DUNE] Finished fetching results: %d total rows", totalSent)
	return nil
}

var _ source.Source = (*DuneSource)(nil)
