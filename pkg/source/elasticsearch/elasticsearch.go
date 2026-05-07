package elasticsearch

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	elasticsearch "github.com/elastic/go-elasticsearch/v9"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

const (
	scrollTimeout = "5m"
	scrollSize    = 1000
)

type durationString string

//nolint:unused // used by elasticsearch library
func (d durationString) DurationCaster() *types.Duration {
	dur := types.Duration(string(d))
	return &dur
}

type elasticsearchConfig struct {
	baseURL     string
	username    string
	password    string
	apiKey      string
	verifyCerts bool
}

type ElasticsearchSource struct {
	config *elasticsearchConfig
	client *elasticsearch.TypedClient
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

	esCfg := elasticsearch.Config{
		Addresses: []string{cfg.baseURL},
	}

	if cfg.apiKey != "" {
		esCfg.APIKey = cfg.apiKey
	} else if cfg.username != "" && cfg.password != "" {
		esCfg.Username = cfg.username
		esCfg.Password = cfg.password
	}

	if !cfg.verifyCerts {
		esCfg.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // user-configured option to disable TLS verification
			},
		}
	}

	client, err := elasticsearch.NewTypedClient(esCfg)
	if err != nil {
		return fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	_, err = client.Info().Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to elasticsearch: %w", err)
	}

	s.client = client
	config.Debug("[ELASTICSEARCH] Connected successfully to %s", cfg.baseURL)
	return nil
}

func (s *ElasticsearchSource) Close(_ context.Context) error {
	return nil
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

	query := buildQuery(opts)

	res, err := s.client.Search().
		Index(index).
		Query(query).
		Scroll(scrollTimeout).
		Size(scrollSize).
		Perform(ctx)
	if err != nil {
		return fmt.Errorf("failed to search index %s: %w", index, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= 400 {
		return fmt.Errorf("elasticsearch search on %s returned status %d", index, res.StatusCode)
	}

	var searchResult searchResponse
	decoder := json.NewDecoder(res.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&searchResult); err != nil {
		return fmt.Errorf("failed to parse search response for %s: %w", index, err)
	}

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

		res, err := s.client.Scroll().
			ScrollId(scrollID).
			Scroll(durationString(scrollTimeout)).
			Perform(ctx)
		if err != nil {
			return fmt.Errorf("failed to scroll index %s: %w", index, err)
		}

		if res.StatusCode >= 400 {
			_ = res.Body.Close()
			return fmt.Errorf("elasticsearch scroll on %s returned status %d", index, res.StatusCode)
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

func buildQuery(opts source.ReadOptions) *types.Query {
	if opts.IncrementalKey == "" || opts.IntervalStart == nil {
		return &types.Query{
			MatchAll: &types.MatchAllQuery{},
		}
	}

	rangeQuery := types.DateRangeQuery{}
	if opts.IntervalStart != nil {
		gte := opts.IntervalStart.Format(time.RFC3339)
		rangeQuery.Gte = &gte
	}
	if opts.IntervalEnd != nil {
		lt := opts.IntervalEnd.Format(time.RFC3339)
		rangeQuery.Lt = &lt
	}

	return &types.Query{
		Range: map[string]types.RangeQuery{
			opts.IncrementalKey: rangeQuery,
		},
	}
}

func (s *ElasticsearchSource) clearScroll(ctx context.Context, scrollID string) {
	_, err := s.client.ClearScroll().ScrollId(scrollID).Do(ctx)
	if err != nil {
		config.Debug("[ELASTICSEARCH] Failed to clear scroll: %v", err)
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
