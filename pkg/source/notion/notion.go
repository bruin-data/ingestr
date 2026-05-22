package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
	baseURL            = "https://api.notion.com/v1"
	notionVersion      = "2022-06-28"
	maxPageSize        = 100
	rateLimit          = 2.4 // Notion allows 3 req/s average
	rateLimitBurst     = 5
	defaultParallelism = 5
)

type NotionSource struct {
	apiKey string
	client *httpclient.Client
}

func NewNotionSource() *NotionSource {
	return &NotionSource{}
}

func (s *NotionSource) Schemes() []string {
	return []string{"notion"}
}

func (s *NotionSource) HandlesIncrementality() bool {
	return true
}

func (s *NotionSource) Connect(_ context.Context, uri string) error {
	apiKey, err := parseNotionURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(apiKey)),
		httpclient.WithHeader("Notion-Version", notionVersion),
	)

	config.Debug("[notion] connected successfully")
	return nil
}

func (s *NotionSource) Close(_ context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseNotionURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "notion://") {
		return "", fmt.Errorf("invalid notion URI: must start with notion://")
	}

	rest := strings.TrimPrefix(uri, "notion://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in notion URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse notion URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in notion URI")
	}

	return apiKey, nil
}

func (s *NotionSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	databaseID := req.Name

	if databaseID == "*" {
		databases, err := s.discoverDatabases(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to discover notion databases: %w", err)
		}
		if len(databases) == 0 {
			return nil, fmt.Errorf("no databases found in the notion workspace; ensure the integration has access to at least one database")
		}

		config.Debug("[notion] discovered %d databases", len(databases))
		for _, db := range databases {
			config.Debug("[notion]   - %s (%s)", db.name, db.id)
		}

		return &source.DynamicSourceTable{
			TableName:        "all_databases",
			TablePrimaryKeys: []string{"id"},
			TableStrategy:    config.StrategyReplace,
			KnownSchema:      false,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return nil, fmt.Errorf("notion source does not have a predefined schema; schema inference is required")
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readAll(ctx, databases, opts)
			},
		}, nil
	}

	return &source.DynamicSourceTable{
		TableName:        databaseID,
		TablePrimaryKeys: []string{"id"},
		TableStrategy:    config.StrategyReplace,
		KnownSchema:      false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("notion source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, databaseID, opts)
		},
	}, nil
}

type notionDatabase struct {
	id   string
	name string
}

func (s *NotionSource) discoverDatabases(ctx context.Context) ([]notionDatabase, error) {
	var databases []notionDatabase
	var nextCursor *string

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		body := map[string]interface{}{
			"filter": map[string]string{
				"value":    "database",
				"property": "object",
			},
		}
		if nextCursor != nil {
			body["start_cursor"] = *nextCursor
		}

		resp, err := s.client.R(ctx).
			SetBody(body).
			Post("/search")
		if err != nil {
			return nil, fmt.Errorf("failed to search notion databases: %w", err)
		}

		if !resp.IsSuccess() {
			return nil, fmt.Errorf("notion search API returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var searchResult struct {
			Results    []json.RawMessage `json:"results"`
			HasMore    bool              `json:"has_more"`
			NextCursor *string           `json:"next_cursor"`
		}

		if err := json.Unmarshal(resp.Body(), &searchResult); err != nil {
			return nil, fmt.Errorf("failed to parse notion search response: %w", err)
		}

		for _, raw := range searchResult.Results {
			var db struct {
				ID    string `json:"id"`
				Title []struct {
					PlainText string `json:"plain_text"`
				} `json:"title"`
			}
			if err := json.Unmarshal(raw, &db); err != nil {
				continue
			}
			name := ""
			if len(db.Title) > 0 && db.Title[0].PlainText != "" {
				name = db.Title[0].PlainText
			}
			if name == "" {
				name, _ = s.fetchDatabaseName(ctx, db.ID)
			}
			if name == "" {
				name = db.ID
			}
			databases = append(databases, notionDatabase{id: db.ID, name: name})
		}

		if !searchResult.HasMore || searchResult.NextCursor == nil {
			break
		}
		nextCursor = searchResult.NextCursor
	}

	return databases, nil
}

func (s *NotionSource) fetchDatabaseName(ctx context.Context, databaseID string) (string, error) {
	resp, err := s.client.R(ctx).Get(fmt.Sprintf("/databases/%s", databaseID))
	if err != nil {
		return "", err
	}
	if !resp.IsSuccess() {
		return "", fmt.Errorf("notion API returned status %d", resp.StatusCode())
	}

	var db struct {
		Title []struct {
			PlainText string `json:"plain_text"`
		} `json:"title"`
	}
	if err := json.Unmarshal(resp.Body(), &db); err != nil {
		return "", err
	}
	if len(db.Title) > 0 && db.Title[0].PlainText != "" {
		return db.Title[0].PlainText, nil
	}
	return "", nil
}

func (s *NotionSource) readAll(ctx context.Context, databases []notionDatabase, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}

	config.Debug("[notion] fetching %d databases with parallelism %d", len(databases), parallelism)

	go func() {
		defer close(results)

		workerCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		sem := make(chan struct{}, parallelism)
		var wg sync.WaitGroup

		for _, db := range databases {
			select {
			case <-workerCtx.Done():
				return
			default:
			}

			wg.Add(1)
			sem <- struct{}{}

			go func(db notionDatabase) {
				defer wg.Done()
				defer func() { <-sem }()

				config.Debug("[notion] reading database %s (%s)", db.name, db.id)
				if err := s.queryDatabase(workerCtx, db.id, opts, results); err != nil {
					cancel()
					select {
					case results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read database %s (%s): %w", db.name, db.id, err)}:
					case <-workerCtx.Done():
					}
				}
			}(db)
		}

		wg.Wait()
	}()

	return results, nil
}

func (s *NotionSource) read(ctx context.Context, databaseID string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		if err := s.queryDatabase(ctx, databaseID, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *NotionSource) queryDatabase(ctx context.Context, databaseID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	var nextCursor *string
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		body := map[string]interface{}{
			"page_size": maxPageSize,
		}
		if nextCursor != nil {
			body["start_cursor"] = *nextCursor
		}

		resp, err := s.client.R(ctx).
			SetBody(body).
			Post(fmt.Sprintf("/databases/%s/query", databaseID))
		if err != nil {
			return fmt.Errorf("failed to query notion database %s: %w", databaseID, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("notion API returned status %d for database %s: %s", resp.StatusCode(), databaseID, resp.String())
		}

		var queryResult struct {
			Results    []json.RawMessage `json:"results"`
			HasMore    bool              `json:"has_more"`
			NextCursor *string           `json:"next_cursor"`
		}

		if err := json.Unmarshal(resp.Body(), &queryResult); err != nil {
			return fmt.Errorf("failed to parse notion query response: %w", err)
		}

		if len(queryResult.Results) == 0 {
			break
		}

		items := make([]map[string]interface{}, 0, len(queryResult.Results))
		for _, raw := range queryResult.Results {
			var item map[string]interface{}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			if err := dec.Decode(&item); err != nil {
				return fmt.Errorf("failed to parse notion page object: %w", err)
			}
			items = append(items, item)
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert notion results to Arrow: %w", err)
		}

		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(items)

		config.Debug("[notion] fetched %d pages from database %s (total: %d)", len(items), databaseID, totalSent)

		if !queryResult.HasMore || queryResult.NextCursor == nil {
			break
		}

		nextCursor = queryResult.NextCursor
	}

	config.Debug("[notion] finished querying database %s, total pages: %d", databaseID, totalSent)
	return nil
}
