package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	defaultBaseURL        = "https://gitlab.com/api/v4"
	defaultPerPage        = 100
	rateLimit             = 10
	rateLimitBurst        = 5
	maxRateLimitRetries   = 5
	rateLimitFallbackWait = 5 * time.Second
	rateLimitMaxWait      = 5 * time.Minute
	defaultParallelism    = 5
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
	perProjectResource    string
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
		primaryKeys:        []string{"id"},
		strategy:           config.StrategyReplace,
		perProjectResource: "users",
	},
	"issues": {
		queryParams:           map[string]string{"order_by": "updated_at", "sort": "asc"},
		primaryKeys:           []string{"id"},
		strategy:              config.StrategyMerge,
		incrementalKey:        "updated_at",
		supportsUpdatedFilter: true,
		perProjectResource:    "issues",
	},
	"merge_requests": {
		queryParams:           map[string]string{"order_by": "updated_at", "sort": "asc"},
		primaryKeys:           []string{"id"},
		strategy:              config.StrategyMerge,
		incrementalKey:        "updated_at",
		supportsUpdatedFilter: true,
		perProjectResource:    "merge_requests",
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
		var err error
		if cfg.perProjectResource != "" {
			err = s.streamPerProject(ctx, cfg, opts, results)
		} else {
			err = s.paginate(ctx, cfg.endpoint, cfg.queryParams, cfg.supportsUpdatedFilter, opts, func(items []map[string]interface{}) error {
				record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
				if err != nil {
					return fmt.Errorf("failed to convert gitlab data to Arrow: %w", err)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case results <- source.RecordBatchResult{Batch: record}:
				}
				return nil
			})
		}
		if err != nil {
			select {
			case <-ctx.Done():
			case results <- source.RecordBatchResult{Err: err}:
			}
		}
	}()

	return results, nil
}

// streamPerProject lists the membership projects, then fans out the per-project endpoint
func (s *GitLabSource) streamPerProject(ctx context.Context, cfg tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if s.client == nil {
		return fmt.Errorf("gitlab source is not connected")
	}

	var projectIDs []string
	listParams := map[string]string{"membership": "true", "order_by": "id", "sort": "asc"}
	err := s.paginate(ctx, "/projects", listParams, false, source.ReadOptions{}, func(items []map[string]interface{}) error {
		for _, item := range items {
			if id, ok := item["id"]; ok && id != nil {
				projectIDs = append(projectIDs, fmt.Sprint(id))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}
	config.Debug("[GITLAB] %s: fanning out over %d projects with parallelism %d", cfg.perProjectResource, len(projectIDs), parallelism)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	projectChan := make(chan string, len(projectIDs))
	errChan := make(chan error, 1)
	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range projectChan {
				select {
				case <-ctx.Done():
					return
				default:
				}
				endpoint := "/projects/" + id + "/" + cfg.perProjectResource
				err := s.paginate(ctx, endpoint, cfg.queryParams, cfg.supportsUpdatedFilter, opts, func(items []map[string]interface{}) error {
					record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
					if err != nil {
						return fmt.Errorf("failed to convert gitlab data to Arrow: %w", err)
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					case results <- source.RecordBatchResult{Batch: record}:
					}
					return nil
				})
				if err != nil {
					// Member projects routinely disable issues/MRs (404) or
					// restrict them (403); skip those instead of failing the run.
					var se *statusError
					if errors.As(err, &se) && (se.status == http.StatusNotFound || se.status == http.StatusForbidden) {
						config.Debug("[GITLAB] skipping project %s %s: %s", id, cfg.perProjectResource, err)
						continue
					}
					select {
					case errChan <- err:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}

	for _, id := range projectIDs {
		projectChan <- id
	}
	close(projectChan)

	wg.Wait()
	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

// paginate walks every page of endpoint (honoring 429 Retry-After)
func (s *GitLabSource) paginate(ctx context.Context, endpoint string, params map[string]string, applyInterval bool, opts source.ReadOptions, emit func([]map[string]interface{}) error) error {
	if s.client == nil {
		return fmt.Errorf("gitlab source is not connected")
	}
	config.Debug("[GITLAB] reading %s", endpoint)

	perPage := defaultPerPage
	if opts.PageSize > 0 && opts.PageSize < perPage {
		perPage = opts.PageSize
	}

	page := "1"
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		var resp *httpclient.Response
		for attempt := 0; ; attempt++ {
			req := s.client.R(ctx).
				SetQueryParam("per_page", strconv.Itoa(perPage)).
				SetQueryParam("page", page)
			for key, value := range params {
				req.SetQueryParam(key, value)
			}
			if applyInterval {
				if opts.IntervalStart != nil {
					req.SetQueryParam("updated_after", opts.IntervalStart.UTC().Format(time.RFC3339))
				}
				if opts.IntervalEnd != nil {
					req.SetQueryParam("updated_before", opts.IntervalEnd.UTC().Format(time.RFC3339))
				}
			}

			var err error
			resp, err = req.Get(endpoint)
			if err != nil {
				return fmt.Errorf("failed to fetch gitlab endpoint %s: %w", endpoint, err)
			}
			// GitLab returns 429 with a Retry-After header when the rate limit is
			// hit. Honor it and retry the same page instead of failing the run.
			if resp.StatusCode() == http.StatusTooManyRequests && attempt < maxRateLimitRetries {
				wait := rateLimitFallbackWait
				if secs, perr := strconv.Atoi(strings.TrimSpace(resp.Header().Get("Retry-After"))); perr == nil && secs > 0 {
					if wait = time.Duration(secs) * time.Second; wait > rateLimitMaxWait {
						wait = rateLimitMaxWait
					}
				}
				config.Debug("[GITLAB] 429 from %s, waiting %v before retry %d/%d", endpoint, wait, attempt+1, maxRateLimitRetries)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
			break
		}
		if err := checkResponse(endpoint, resp); err != nil {
			return err
		}

		dec := json.NewDecoder(bytes.NewReader(resp.Body()))
		dec.UseNumber()
		var raw []interface{}
		if err := dec.Decode(&raw); err != nil {
			return fmt.Errorf("malformed gitlab response from %s: %w", endpoint, err)
		}
		items := make([]map[string]interface{}, 0, len(raw))
		for i, rawItem := range raw {
			item, ok := rawItem.(map[string]interface{})
			if !ok {
				return fmt.Errorf("data item %d from %s is not an object", i, endpoint)
			}
			items = append(items, item)
		}
		if len(items) > 0 {
			if err := emit(items); err != nil {
				return err
			}
		}

		page = strings.TrimSpace(resp.Header().Get("X-Next-Page"))
		if page == "" {
			return nil
		}
	}
}

type statusError struct {
	status  int
	message string
}

func (e *statusError) Error() string { return e.message }

func checkResponse(endpoint string, resp *httpclient.Response) error {
	if resp.IsSuccess() {
		return nil
	}
	status := resp.StatusCode()
	var message string
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		message = fmt.Sprintf("gitlab API authentication or access failed for %s (status %d)", endpoint, status)
	case http.StatusTooManyRequests:
		message = fmt.Sprintf("gitlab API rate limit exceeded for %s (status 429)", endpoint)
	default:
		message = fmt.Sprintf("gitlab API error for %s (status %d): %s", endpoint, status, resp.String())
	}
	return &statusError{status: status, message: message}
}

var _ source.Source = (*GitLabSource)(nil)
