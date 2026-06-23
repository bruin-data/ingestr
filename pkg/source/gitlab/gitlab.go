package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	defaultBaseURL = "https://gitlab.com/api/v4"
	defaultPerPage = 100
	rateLimit      = 10
	rateLimitBurst = 5
)

type GitLabSource struct {
	client  *httpclient.Client
	token   string
	baseURL string
}

func NewGitLabSource() *GitLabSource {
	return &GitLabSource{}
}

func (s *GitLabSource) Schemes() []string {
	return []string{"gitlab"}
}

type tableConfig struct {
	endpoint              string
	queryParams           map[string]string
	primaryKeys           []string
	strategy              config.IncrementalStrategy
	incrementalKey        string
	supportsUpdatedFilter bool
}

var tables = map[string]tableConfig{
	"projects": {
		endpoint:              "/projects",
		queryParams:           map[string]string{"membership": "true", "order_by": "updated_at", "sort": "asc"},
		primaryKeys:           []string{"id"},
		strategy:              config.StrategyMerge,
		incrementalKey:        "updated_at",
		supportsUpdatedFilter: true,
	},
	"groups": {
		endpoint:    "/groups",
		queryParams: map[string]string{"order_by": "id", "sort": "asc"},
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"users": {
		endpoint:    "/users",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"issues": {
		endpoint:              "/issues",
		queryParams:           map[string]string{"scope": "created_by_me", "order_by": "updated_at", "sort": "asc"},
		primaryKeys:           []string{"id"},
		strategy:              config.StrategyMerge,
		incrementalKey:        "updated_at",
		supportsUpdatedFilter: true,
	},
	"merge_requests": {
		endpoint:              "/merge_requests",
		queryParams:           map[string]string{"scope": "created_by_me", "order_by": "updated_at", "sort": "asc"},
		primaryKeys:           []string{"id"},
		strategy:              config.StrategyMerge,
		incrementalKey:        "updated_at",
		supportsUpdatedFilter: true,
	},
}

func (s *GitLabSource) Connect(ctx context.Context, uri string) error {
	token, baseURL, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.token = token
	s.baseURL = baseURL
	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithHeader("PRIVATE-TOKEN", token),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
	)
	return nil
}

func parseURI(raw string) (token, baseURL string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse gitlab URI: %w", err)
	}
	if parsed.Scheme != "gitlab" {
		return "", "", fmt.Errorf("invalid gitlab URI: must start with gitlab://")
	}

	values := parsed.Query()
	token = values.Get("access_token")
	if token == "" {
		return "", "", fmt.Errorf("invalid gitlab URI: access_token query parameter is required")
	}

	baseURL = strings.TrimRight(values.Get("base_url"), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return token, baseURL, nil
}

func (s *GitLabSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *GitLabSource) HandlesIncrementality() bool {
	return true
}

func (s *GitLabSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	cfg, ok := tables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s, supported tables are: projects, groups, users, issues, merge_requests", req.Name)
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    cfg.primaryKeys,
		TableStrategy:       cfg.strategy,
		TableIncrementalKey: cfg.incrementalKey,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("gitlab source relies on schema inference")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, cfg, opts)
		},
	}, nil
}

func (s *GitLabSource) read(ctx context.Context, cfg tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 1)

	go func() {
		defer close(results)
		if err := s.stream(ctx, cfg, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *GitLabSource) stream(ctx context.Context, cfg tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if s.client == nil {
		return fmt.Errorf("gitlab source is not connected")
	}
	config.Debug("[GITLAB] reading %s", cfg.endpoint)

	perPage := defaultPerPage
	if opts.PageSize > 0 && opts.PageSize < perPage {
		perPage = opts.PageSize
	}

	page := "1"
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		req := s.client.R(ctx).
			SetQueryParam("per_page", strconv.Itoa(perPage)).
			SetQueryParam("page", page)
		for key, value := range cfg.queryParams {
			req.SetQueryParam(key, value)
		}
		if cfg.supportsUpdatedFilter {
			if opts.IntervalStart != nil {
				req.SetQueryParam("updated_after", opts.IntervalStart.UTC().Format(time.RFC3339))
			}
			if opts.IntervalEnd != nil {
				req.SetQueryParam("updated_before", opts.IntervalEnd.UTC().Format(time.RFC3339))
			}
		}

		resp, err := req.Get(cfg.endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch gitlab endpoint %s: %w", cfg.endpoint, err)
		}
		if err := checkResponse(cfg.endpoint, resp); err != nil {
			return err
		}

		dec := json.NewDecoder(bytes.NewReader(resp.Body()))
		dec.UseNumber()
		var raw []interface{}
		if err := dec.Decode(&raw); err != nil {
			return fmt.Errorf("malformed gitlab response from %s: %w", cfg.endpoint, err)
		}
		items := make([]map[string]interface{}, 0, len(raw))
		for i, rawItem := range raw {
			item, ok := rawItem.(map[string]interface{})
			if !ok {
				return fmt.Errorf("malformed gitlab response from %s: data item %d is not an object", cfg.endpoint, i)
			}
			items = append(items, item)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert gitlab data to Arrow: %w", err)
			}
			results <- source.RecordBatchResult{Batch: record}
		}

		next := strings.TrimSpace(resp.Header().Get("X-Next-Page"))
		if next == "" {
			return nil
		}
		page = next
	}
}

func checkResponse(endpoint string, resp *httpclient.Response) error {
	if resp.IsSuccess() {
		return nil
	}
	switch resp.StatusCode() {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("gitlab API authentication or access failed for %s (status %d)", endpoint, resp.StatusCode())
	case http.StatusTooManyRequests:
		return fmt.Errorf("gitlab API rate limit exceeded for %s (status 429)", endpoint)
	default:
		return fmt.Errorf("gitlab API error for %s (status %d): %s", endpoint, resp.StatusCode(), resp.String())
	}
}

var _ source.Source = (*GitLabSource)(nil)
