package bruin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	baseURL        = "https://cloud.getbruin.com/api/v1"
	rateLimit      = 80.0 / 60.0
	rateLimitBurst = 5
)

var supportedTables = []string{
	"pipelines",
	"assets",
}

var pipelineFields = []string{
	"name", "description", "project", "owner", "default_connections", "schedule", "commit", "start_date",
}

var assetFields = []string{
	"name", "type", "pipeline", "project", "uri", "description", "upstreams", "downstream", "owner", "content", "columns", "materialization", "parameters",
}

type BruinSource struct {
	client *gonghttp.Client
	apiKey string
}

func NewBruinSource() *BruinSource {
	return &BruinSource{}
}

func (s *BruinSource) Schemes() []string {
	return []string{"bruin"}
}

func (s *BruinSource) HandlesIncrementality() bool {
	return false
}

func (s *BruinSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey
	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(30*time.Second),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(s.apiKey)),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
	)

	config.Debug("[BRUIN] Connected")
	return nil
}

func parseURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "bruin://") {
		return "", fmt.Errorf("invalid bruin URI: must start with bruin://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("failed to parse bruin URI: %w", err)
	}

	apiKey := parsed.Query().Get("api_key")
	if apiKey == "" {
		apiKey = parsed.Query().Get("api_token")
	}
	if apiKey == "" {
		return "", fmt.Errorf("api_key (or api_token) is required in the bruin URI, expected format: bruin://?api_key=<api_key>")
	}

	return apiKey, nil
}

func (s *BruinSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *BruinSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s, supported tables: %v", tableName, supportedTables)
	}

	return &source.DynamicSourceTable{
		TableName:     tableName,
		TableStrategy: config.StrategyReplace,
		KnownSchema:   false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("bruin source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(name string) bool {
	for _, t := range supportedTables {
		if t == name {
			return true
		}
	}
	return false
}

func (s *BruinSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "pipelines":
			err = s.readPipelines(ctx, opts, results)
		case "assets":
			err = s.readAssets(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *BruinSource) fetchPipelines(ctx context.Context) ([]map[string]any, error) {
	resp, err := s.client.R(ctx).Get("/pipelines")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pipelines: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("bruin API /pipelines returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var pipelines []map[string]any
	decoder := json.NewDecoder(strings.NewReader(resp.String()))
	decoder.UseNumber()
	if err := decoder.Decode(&pipelines); err != nil {
		return nil, fmt.Errorf("failed to parse pipelines response: %w", err)
	}

	return pipelines, nil
}

func (s *BruinSource) readPipelines(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BRUIN] reading pipelines")

	pipelines, err := s.fetchPipelines(ctx)
	if err != nil {
		return err
	}

	items := make([]map[string]any, 0, len(pipelines))
	for _, p := range pipelines {
		item := pickFields(p, pipelineFields)
		items = append(items, item)
	}

	if len(items) == 0 {
		config.Debug("[BRUIN] No pipelines found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record for pipelines: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[BRUIN] Sent %d pipelines", len(items))
	return nil
}

func (s *BruinSource) readAssets(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BRUIN] reading assets")

	pipelines, err := s.fetchPipelines(ctx)
	if err != nil {
		return err
	}

	var items []map[string]any
	for _, p := range pipelines {
		assets, ok := p["assets"].([]any)
		if !ok {
			config.Debug("[BRUIN] pipeline %v has no assets field", p["name"])
			continue
		}
		for _, a := range assets {
			assetMap, ok := a.(map[string]any)
			if !ok {
				continue
			}
			item := pickFields(assetMap, assetFields)
			items = append(items, item)
		}
	}

	if len(items) == 0 {
		config.Debug("[BRUIN] No assets found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record for assets: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[BRUIN] Sent %d assets", len(items))
	return nil
}

func pickFields(src map[string]any, fields []string) map[string]any {
	result := make(map[string]any, len(fields))
	for _, f := range fields {
		if v, ok := src[f]; ok {
			result[f] = v
		}
	}
	return result
}

var _ source.Source = (*BruinSource)(nil)
