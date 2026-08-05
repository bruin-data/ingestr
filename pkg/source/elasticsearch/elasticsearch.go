package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/esclient"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	scrollTimeout = "5m"
	scrollSize    = 1000
)

type elasticsearchConfig struct {
	baseURL     string
	username    string
	password    string
	apiKey      string
	verifyCerts bool
}

type ElasticsearchSource struct {
	config *elasticsearchConfig
	client *esclient.Client
}

func NewElasticsearchSource() *ElasticsearchSource {
	return &ElasticsearchSource{}
}

func (s *ElasticsearchSource) HandlesIncrementality() bool {
	return false
}

func (s *ElasticsearchSource) Schemes() []string {
	return []string{"elasticsearch"}
}

func (s *ElasticsearchSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.config = cfg

	client, err := esclient.New(esclient.Config{
		BaseURL:     cfg.baseURL,
		Username:    cfg.username,
		Password:    cfg.password,
		APIKey:      cfg.apiKey,
		VerifyCerts: cfg.verifyCerts,
	})
	if err != nil {
		return fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	res, err := client.Perform(ctx, http.MethodGet, "/", nil, nil, "")
	if err != nil {
		_ = client.Close(ctx)
		return fmt.Errorf("failed to connect to elasticsearch: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= http.StatusMultipleChoices {
		_ = client.Close(ctx)
		return fmt.Errorf("failed to connect to elasticsearch: %s", esclient.StatusMessage(res))
	}

	s.client = client
	config.Debug("[ELASTICSEARCH] Connected successfully to %s", cfg.baseURL)
	return nil
}

func (s *ElasticsearchSource) Close(ctx context.Context) error {
	if s.client == nil {
		return nil
	}
	return s.client.Close(ctx)
}

func parseURI(uri string) (*elasticsearchConfig, error) {
	if !strings.HasPrefix(uri, "elasticsearch://") {
		return nil, fmt.Errorf("invalid elasticsearch URI: must start with elasticsearch://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to parse elasticsearch URI: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("host is required in elasticsearch URI")
	}

	port := parsed.Port()
	if port == "" {
		port = "9200"
	}

	secure := true
	if v := parsed.Query().Get("secure"); v != "" {
		secure = v == "true" || v == "1"
	}

	verifyCerts := true
	if v := parsed.Query().Get("verify_certs"); v != "" {
		verifyCerts = v == "true" || v == "1"
	}

	scheme := "https"
	if !secure {
		scheme = "http"
	}

	username := ""
	password := ""
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}

	apiKey := parsed.Query().Get("api_key")

	return &elasticsearchConfig{
		baseURL:     fmt.Sprintf("%s://%s:%s", scheme, host, port),
		username:    username,
		password:    password,
		apiKey:      apiKey,
		verifyCerts: verifyCerts,
	}, nil
}

func (s *ElasticsearchSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	indexName := req.Name
	if indexName == "" {
		return nil, fmt.Errorf("index name (source-table) is required for elasticsearch")
	}

	return &source.DynamicSourceTable{
		TableName:           indexName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("elasticsearch source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, indexName, opts)
		},
	}, nil
}

func (s *ElasticsearchSource) read(ctx context.Context, index string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		err := s.readIndex(ctx, index, opts, results)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *ElasticsearchSource) readIndex(ctx context.Context, index string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ELASTICSEARCH] reading index %s", index)

	requestBody, err := json.Marshal(map[string]any{
		"query": buildQuery(opts),
		"size":  scrollSize,
	})
	if err != nil {
		return fmt.Errorf("failed to build search request for %s: %w", index, err)
	}

	res, err := s.client.Perform(
		ctx,
		http.MethodPost,
		"/"+url.PathEscape(index)+"/_search",
		url.Values{"scroll": {scrollTimeout}},
		bytes.NewReader(requestBody),
		esclient.JSONContentType,
	)
	if err != nil {
		return fmt.Errorf("failed to search index %s: %w", index, err)
	}

	if res.StatusCode >= 400 {
		msg := esclient.StatusMessage(res)
		_ = res.Body.Close()
		return fmt.Errorf("elasticsearch search on %s failed: %s", index, msg)
	}

	var searchResult searchResponse
	decoder := json.NewDecoder(res.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&searchResult); err != nil {
		_ = res.Body.Close()
		return fmt.Errorf("failed to parse search response for %s: %w", index, err)
	}
	_ = res.Body.Close()

	scrollID := searchResult.ScrollID
	defer func() {
		if scrollID != "" {
			s.clearScroll(ctx, scrollID)
		}
	}()

	totalSent := 0

	for {
		hits := searchResult.Hits.Hits
		if len(hits) == 0 {
			break
		}

		items := make([]map[string]any, 0, len(hits))
		for _, hit := range hits {
			doc := make(map[string]any, len(hit.Source)+1)
			doc["id"] = hit.ID
			maps.Copy(doc, hit.Source)
			items = append(items, doc)
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert documents to Arrow: %w", err)
		}

		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(items)
		config.Debug("[ELASTICSEARCH] Sent %d documents from %s (total: %d)", len(items), index, totalSent)

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		scrollBody, err := json.Marshal(map[string]any{
			"scroll":    scrollTimeout,
			"scroll_id": scrollID,
		})
		if err != nil {
			return fmt.Errorf("failed to build scroll request for %s: %w", index, err)
		}

		res, err := s.client.Perform(
			ctx,
			http.MethodPost,
			"/_search/scroll",
			nil,
			bytes.NewReader(scrollBody),
			esclient.JSONContentType,
		)
		if err != nil {
			return fmt.Errorf("failed to scroll index %s: %w", index, err)
		}

		if res.StatusCode >= 400 {
			msg := esclient.StatusMessage(res)
			_ = res.Body.Close()
			return fmt.Errorf("elasticsearch scroll on %s failed: %s", index, msg)
		}

		searchResult = searchResponse{}
		decoder = json.NewDecoder(res.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&searchResult); err != nil {
			_ = res.Body.Close()
			return fmt.Errorf("failed to parse scroll response for %s: %w", index, err)
		}
		_ = res.Body.Close()

		scrollID = searchResult.ScrollID
	}

	config.Debug("[ELASTICSEARCH] Finished reading index %s, total documents: %d", index, totalSent)
	return nil
}

func buildQuery(opts source.ReadOptions) map[string]any {
	if opts.IncrementalKey == "" || opts.IntervalStart == nil {
		return map[string]any{
			"match_all": map[string]any{},
		}
	}

	rangeQuery := make(map[string]any, 2)
	if opts.IntervalStart != nil {
		rangeQuery["gte"] = opts.IntervalStart.Format(time.RFC3339)
	}
	if opts.IntervalEnd != nil {
		rangeQuery["lt"] = opts.IntervalEnd.Format(time.RFC3339)
	}

	return map[string]any{
		"range": map[string]any{
			opts.IncrementalKey: rangeQuery,
		},
	}
}

func (s *ElasticsearchSource) clearScroll(ctx context.Context, scrollID string) {
	body, err := json.Marshal(map[string]any{"scroll_id": []string{scrollID}})
	if err != nil {
		config.Debug("[ELASTICSEARCH] Failed to build clear scroll request: %v", err)
		return
	}

	res, err := s.client.Perform(
		ctx,
		http.MethodDelete,
		"/_search/scroll",
		nil,
		bytes.NewReader(body),
		esclient.JSONContentType,
	)
	if err != nil {
		config.Debug("[ELASTICSEARCH] Failed to clear scroll: %v", err)
		return
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= http.StatusMultipleChoices && res.StatusCode != http.StatusNotFound {
		config.Debug("[ELASTICSEARCH] Failed to clear scroll: %s", res.Status)
	}
}

type searchResponse struct {
	ScrollID string `json:"_scroll_id"`
	Hits     struct {
		Hits []searchHit `json:"hits"`
	} `json:"hits"`
}

type searchHit struct {
	ID     string         `json:"_id"`
	Source map[string]any `json:"_source"`
}
