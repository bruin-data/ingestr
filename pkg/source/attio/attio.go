package attio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
	baseURL            = "https://api.attio.com/v2"
	defaultParallelism = 5
)

type AttioSource struct {
	client *gonghttp.Client
	apiKey string
}

func NewAttioSource() *AttioSource {
	return &AttioSource{}
}

func (s *AttioSource) Schemes() []string {
	return []string{"attio"}
}

func (s *AttioSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseAttioURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(100, 10),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(apiKey)),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithHeader("Accept", "application/json"),
	)

	config.Debug("[ATTIO] Connected successfully")
	return nil
}

func parseAttioURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "attio://") {
		return "", fmt.Errorf("invalid attio URI: must start with attio://")
	}

	rest := strings.TrimPrefix(uri, "attio://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in attio URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse attio URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in attio URI")
	}

	return apiKey, nil
}

func (s *AttioSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *AttioSource) HandlesIncrementality() bool {
	return false
}

func (s *AttioSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: "",
		TableStrategy:       config.StrategyReplace,
		TablePartitionBy:    "created_at",
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("attio source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return parts[0], ""
}

func (s *AttioSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)
	name, param := parseTableName(table)

	go func() {
		defer close(results)

		var err error
		switch name {
		case "objects":
			if param != "" {
				err = fmt.Errorf("objects does not accept a parameter, use just 'objects'")
			} else {
				err = s.readObjects(ctx, opts, results)
			}
		case "records":
			if param == "" {
				err = fmt.Errorf("records requires an object slug, e.g. records:companies")
			} else {
				err = s.readRecords(ctx, param, opts, results)
			}
		case "lists":
			if param != "" {
				err = fmt.Errorf("lists does not accept a parameter, use just 'lists'")
			} else {
				err = s.readLists(ctx, opts, results)
			}
		case "list_entries":
			if param == "" {
				err = fmt.Errorf("list_entries requires a list ID, e.g. list_entries:8abc-123-456-789d-123")
			} else {
				err = s.readListEntries(ctx, param, opts, results)
			}
		case "all_list_entries":
			if param == "" {
				err = fmt.Errorf("all_list_entries requires an object slug, e.g. all_list_entries:companies")
			} else {
				err = s.readAllListEntries(ctx, param, opts, results)
			}
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *AttioSource) getPages(ctx context.Context, method string, endpoint string, body map[string]interface{}, transform func(map[string]interface{}) map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	offset := 0
	limit := 1000
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp *gonghttp.Response
		var err error

		switch method {
		case "GET":
			resp, err = s.client.R(ctx).Get(endpoint)
		case "POST":
			reqBody := make(map[string]interface{})
			for k, v := range body {
				reqBody[k] = v
			}
			reqBody["offset"] = offset
			reqBody["limit"] = limit
			resp, err = s.client.R(ctx).SetBody(reqBody).Post(endpoint)
		default:
			return fmt.Errorf("unsupported method: %s", method)
		}

		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("%s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var response struct {
			Data []map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(resp.Body(), &response); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		if len(response.Data) == 0 {
			break
		}

		items := make([]map[string]interface{}, 0, len(response.Data))
		for _, raw := range response.Data {
			if transform != nil {
				raw = transform(raw)
			}
			items = append(items, raw)
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", endpoint, err)
		}
		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(items)

		config.Debug("[ATTIO] Fetched %d items from %s (total: %d)", len(items), endpoint, totalSent)

		if method == "GET" || len(response.Data) < limit {
			break
		}

		offset += limit
	}

	return nil
}

func flattenID(item map[string]interface{}) map[string]interface{} {
	if id, ok := item["id"].(map[string]interface{}); ok {
		for k, v := range id {
			item[k] = v
		}
	}
	return item
}

func (s *AttioSource) readObjects(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.getPages(ctx, "GET", "/objects", nil, flattenID, opts, results)
}

func (s *AttioSource) readLists(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.getPages(ctx, "GET", "/lists", nil, flattenID, opts, results)
}

func (s *AttioSource) readRecords(ctx context.Context, objectSlug string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/objects/%s/records/query", objectSlug)
	return s.getPages(ctx, "POST", endpoint, nil, flattenID, opts, results)
}

func (s *AttioSource) readListEntries(ctx context.Context, listID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/lists/%s/entries/query", listID)
	return s.getPages(ctx, "POST", endpoint, nil, flattenID, opts, results)
}

func (s *AttioSource) readAllListEntries(ctx context.Context, objectSlug string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	resp, err := s.client.R(ctx).Get("/lists")
	if err != nil {
		return fmt.Errorf("failed to fetch lists: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("lists returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var listsResponse struct {
		Data []struct {
			ID           map[string]interface{} `json:"id"`
			ParentObject []string               `json:"parent_object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &listsResponse); err != nil {
		return fmt.Errorf("failed to parse lists response: %w", err)
	}

	listIDCh := make(chan string, defaultParallelism)
	var wg sync.WaitGroup
	errs := make(chan error, 1)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i := 0; i < defaultParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for listID := range listIDCh {
				config.Debug("[ATTIO] Fetching entries for list %s", listID)
				endpoint := fmt.Sprintf("/lists/%s/entries/query", listID)
				if err := s.getPages(ctx, "POST", endpoint, nil, flattenID, opts, results); err != nil {
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

	for _, lst := range listsResponse.Data {
		for _, parent := range lst.ParentObject {
			if parent == objectSlug {
				listID, _ := lst.ID["list_id"].(string)
				if listID != "" {
					select {
					case listIDCh <- listID:
					case <-ctx.Done():
					}
				}
				break
			}
		}
	}
	close(listIDCh)

	wg.Wait()
	close(errs)

	if err := <-errs; err != nil {
		return err
	}

	return nil
}
