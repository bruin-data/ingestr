package phantombuster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL         = "https://api.phantombuster.com/api/v2/"
	retryStatusCode = 429
)

type PhantombusterSource struct {
	client *httpclient.Client
	apiKey string
}

func NewPhantombusterSource() *PhantombusterSource {
	return &PhantombusterSource{}
}

func (s *PhantombusterSource) Schemes() []string {
	return []string{"phantombuster"}
}

func (s *PhantombusterSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parsePhantombusterURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithRetryCondition(func(resp *httpclient.Response, err error) bool {
			return resp != nil && resp.StatusCode() == retryStatusCode
		}),
	)

	config.Debug("[PHANTOMBUSTER] Connected successfully")
	return nil
}

func parsePhantombusterURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "phantombuster://") {
		return "", fmt.Errorf("invalid phantombuster URI: must start with phantombuster://")
	}

	rest := strings.TrimPrefix(uri, "phantombuster://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in phantombuster URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse phantombuster URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in phantombuster URI")
	}

	return apiKey, nil
}

func (s *PhantombusterSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *PhantombusterSource) HandlesIncrementality() bool {
	return true
}

type tableMeta struct {
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
}

var supportedTables = map[string]tableMeta{
	"completed_phantoms": {primaryKeys: []string{"container_id"}, incrementalKey: "ended_at", strategy: config.StrategyMerge},
}

func (s *PhantombusterSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	var meta tableMeta
	switch {
	case strings.HasPrefix(tableName, "completed_phantoms:"):
		agentID := strings.TrimPrefix(tableName, "completed_phantoms:")
		if agentID == "" {
			return nil, fmt.Errorf("agent_id is required: use completed_phantoms:<agent_id>")
		}
		meta = supportedTables["completed_phantoms"]
	default:
		tables := make([]string, 0, len(supportedTables))
		for t := range supportedTables {
			tables = append(tables, t)
		}
		return nil, fmt.Errorf("unsupported table: %s (supported: %v)", tableName, tables)
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    meta.primaryKeys,
		TableIncrementalKey: meta.incrementalKey,
		TableStrategy:       meta.strategy,
		TablePartitionBy:    "partition_dt",
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("phantombuster source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *PhantombusterSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch {
		case strings.HasPrefix(table, "completed_phantoms:"):
			agentID := strings.TrimPrefix(table, "completed_phantoms:")
			err = s.readCompletedPhantoms(ctx, agentID, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *PhantombusterSource) readCompletedPhantoms(ctx context.Context, agentID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	startMs, err := toMillis(opts.IntervalStart)
	if err != nil {
		return fmt.Errorf("interval_start is required")
	}
	endMs, err := toMillis(opts.IntervalEnd)
	if err != nil {
		return fmt.Errorf("interval_end is required")
	}

	var beforeEndedAt string
	limit := "100"

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := map[string]string{
			"agentId": agentID,
			"limit":   limit,
			"mode":    "finalized",
		}
		if beforeEndedAt != "" {
			params["beforeEndedAt"] = beforeEndedAt
		}

		resp, err := s.client.R(ctx).
			SetHeader("X-Phantombuster-Key-1", s.apiKey).
			SetHeader("accept", "application/json").
			SetQueryParams(params).
			Get("containers/fetch-all")
		if err != nil {
			return fmt.Errorf("failed to fetch containers: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("containers/fetch-all returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var body struct {
			Containers      []map[string]interface{} `json:"containers"`
			MaxLimitReached bool                     `json:"maxLimitReached"`
		}
		if err := json.Unmarshal(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse containers response: %w", err)
		}

		if len(body.Containers) == 0 {
			break
		}

		var filtered []map[string]interface{}
		for _, c := range body.Containers {
			endedAt, _ := c["endedAt"].(float64)
			endedAtMs := int64(endedAt)

			beforeEndedAtMs, _ := strconv.ParseInt(beforeEndedAt, 10, 64)
			if beforeEndedAt == "" || endedAtMs < beforeEndedAtMs {
				beforeEndedAt = fmt.Sprintf("%d", endedAtMs)
			}

			if endedAtMs < startMs || endedAtMs > endMs {
				continue
			}
			filtered = append(filtered, c)
		}

		if len(filtered) > 0 {
			rows, err := s.fetchResultObjects(ctx, filtered, opts)
			if err != nil {
				return err
			}
			if len(rows) > 0 {
				record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
				if err != nil {
					return fmt.Errorf("failed to convert to Arrow: %w", err)
				}
				results <- source.RecordBatchResult{Batch: record}
				config.Debug("[PHANTOMBUSTER] Sent %d items", len(rows))
			}
		}

		if !body.MaxLimitReached {
			break
		}
	}

	return nil
}

func (s *PhantombusterSource) fetchResultObjects(ctx context.Context, containers []map[string]interface{}, opts source.ReadOptions) ([]map[string]interface{}, error) {
	type result struct {
		idx int
		row map[string]interface{}
		err error
	}

	resultsCh := make(chan result, len(containers))
	var wg sync.WaitGroup

	for i, c := range containers {
		wg.Add(1)
		go func(idx int, container map[string]interface{}) {
			defer wg.Done()

			containerID, _ := container["id"].(string)
			if containerID == "" {
				if idFloat, ok := container["id"].(float64); ok {
					containerID = fmt.Sprintf("%.0f", idFloat)
				}
			}

			resp, err := s.client.R(ctx).
				SetHeader("X-Phantombuster-Key-1", s.apiKey).
				SetHeader("accept", "application/json").
				SetQueryParams(map[string]string{"id": containerID}).
				Get("containers/fetch-result-object")
			if err != nil {
				resultsCh <- result{idx: idx, err: fmt.Errorf("failed to fetch result for container %s: %w", containerID, err)}
				return
			}
			if !resp.IsSuccess() {
				resultsCh <- result{idx: idx, err: fmt.Errorf("fetch-result-object for %s returned status %d", containerID, resp.StatusCode())}
				return
			}

			var resultObj interface{}
			if err := json.Unmarshal(resp.Body(), &resultObj); err != nil {
				resultsCh <- result{idx: idx, err: fmt.Errorf("failed to parse result for container %s: %w", containerID, err)}
				return
			}

			endedAt, _ := container["endedAt"].(float64)
			endedAtTime := time.UnixMilli(int64(endedAt)).UTC()

			row := map[string]interface{}{
				"container_id": containerID,
				"container":    container,
				"result":       resultObj,
				"partition_dt": endedAtTime.Format("2006-01-02"),
				"ended_at":     endedAtTime.Format(time.RFC3339Nano),
			}
			resultsCh <- result{idx: idx, row: row}
		}(i, c)
	}

	wg.Wait()
	close(resultsCh)

	rows := make([]map[string]interface{}, 0, len(containers))
	for r := range resultsCh {
		if r.err != nil {
			config.Debug("[PHANTOMBUSTER] %v", r.err)
			continue
		}
		rows = append(rows, r.row)
	}

	return rows, nil
}

func toMillis(v interface{}) (int64, error) {
	switch t := v.(type) {
	case time.Time:
		return t.UTC().UnixMilli(), nil
	case *time.Time:
		if t != nil {
			return t.UTC().UnixMilli(), nil
		}
	}
	return 0, fmt.Errorf("invalid time value: %v", v)
}

var _ source.Source = (*PhantombusterSource)(nil)
