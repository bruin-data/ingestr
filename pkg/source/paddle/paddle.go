package paddle

import (
	"context"
	"fmt"
	"net/url"
	"sort"
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
	prodBaseURL    = "https://api.paddle.com"
	sandboxBaseURL = "https://sandbox-api.paddle.com"

	rateLimitPerSecond = 4
	rateLimitBurst     = 4
)

type endpoint struct {
	path         string
	statuses     string
	maxPageSize  int
	serverFilter bool
}

var endpoints = map[string]endpoint{
	"customers":     {path: "/customers", statuses: "active,archived", maxPageSize: 200},
	"products":      {path: "/products", statuses: "active,archived", maxPageSize: 200},
	"prices":        {path: "/prices", statuses: "active,archived", maxPageSize: 200},
	"discounts":     {path: "/discounts", statuses: "active,archived", maxPageSize: 200},
	"transactions":  {path: "/transactions", maxPageSize: 30, serverFilter: true},
	"subscriptions": {path: "/subscriptions", maxPageSize: 200},
	"adjustments":   {path: "/adjustments", maxPageSize: 50},
}

type listResponse struct {
	Data []map[string]interface{} `json:"data"`
	Meta struct {
		Pagination struct {
			HasMore bool   `json:"has_more"`
			Next    string `json:"next"`
		} `json:"pagination"`
	} `json:"meta"`
}

type PaddleSource struct {
	client *httpclient.Client
	apiKey string
}

func NewPaddleSource() *PaddleSource {
	return &PaddleSource{}
}

func (s *PaddleSource) Schemes() []string {
	return []string{"paddle"}
}

func (s *PaddleSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parsePaddleURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURLForKey(s.apiKey)),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(s.apiKey)),
		httpclient.WithRateLimiter(rateLimitPerSecond, rateLimitBurst),
	)

	config.Debug("[PADDLE] Connected successfully")
	return nil
}

func parsePaddleURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "paddle://") {
		return "", fmt.Errorf("invalid paddle URI: must start with paddle://")
	}

	rest := strings.TrimPrefix(uri, "paddle://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse paddle URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in paddle URI")
	}

	return apiKey, nil
}

// baseURLForKey selects the API host from the key prefix.
func baseURLForKey(apiKey string) string {
	if strings.HasPrefix(apiKey, "pdl_sdbx_") {
		return sandboxBaseURL
	}
	return prodBaseURL
}

func (s *PaddleSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *PaddleSource) HandlesIncrementality() bool {
	return true
}

func (s *PaddleSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	ep, ok := endpoints[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported paddle table %q, supported tables are: %s", req.Name, supportedTables())
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: "updated_at",
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("paddle source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, ep, opts)
		},
	}, nil
}

func supportedTables() string {
	names := make([]string, 0, len(endpoints))
	for name := range endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (s *PaddleSource) read(ctx context.Context, table string, ep endpoint, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 4)

	go func() {
		defer close(results)
		if err := s.readEndpoint(ctx, table, ep, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *PaddleSource) readEndpoint(ctx context.Context, table string, ep endpoint, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	start := toTime(opts.IntervalStart)
	end := toTime(opts.IntervalEnd)
	clientFilter := !ep.serverFilter && (start != nil || end != nil)

	firstParams := url.Values{}
	firstParams.Set("per_page", strconv.Itoa(ep.maxPageSize))
	if ep.statuses != "" {
		firstParams.Set("status", ep.statuses)
	}
	if ep.serverFilter {
		if start != nil {
			firstParams.Set("updated_at[GTE]", start.UTC().Format(time.RFC3339))
		}
		if end != nil {
			firstParams.Set("updated_at[LTE]", end.UTC().Format(time.RFC3339))
		}
	}

	totalSent := 0
	requestURL := ep.path
	useParams := true
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp listResponse
		req := s.client.R(ctx).SetResult(&resp)
		if useParams {
			req.SetQueryParamValues(firstParams)
		}
		httpResp, err := req.Get(requestURL)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", table, err)
		}
		if !httpResp.IsSuccess() {
			return fmt.Errorf("paddle %s request failed with status %d: %s", table, httpResp.StatusCode(), httpResp.String())
		}

		if len(resp.Data) == 0 {
			break
		}

		items := resp.Data
		if clientFilter {
			items = make([]map[string]interface{}, 0, len(resp.Data))
			for _, item := range resp.Data {
				if inRange(item, start, end) {
					items = append(items, item)
				}
			}
		}
		if len(items) > 0 {
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return err
			}
			totalSent += len(items)
			config.Debug("[PADDLE] Sending batch of %d %s (total: %d)", len(items), table, totalSent)
			results <- source.RecordBatchResult{Batch: rec}
		}

		if !resp.Meta.Pagination.HasMore {
			break
		}

		if next := resp.Meta.Pagination.Next; next != "" {
			requestURL = next
			useParams = false
			continue
		}

		// Fallback when Paddle omits the next URL: page with the id cursor.
		lastID, _ := resp.Data[len(resp.Data)-1]["id"].(string)
		if lastID == "" {
			break
		}
		firstParams.Set("after", lastID)
		requestURL = ep.path
		useParams = true
	}

	config.Debug("[PADDLE] Finished reading %s, total: %d", table, totalSent)
	return nil
}

func toTime(v *time.Time) *time.Time {
	if v == nil || v.IsZero() {
		return nil
	}
	return v
}

func inRange(item map[string]interface{}, start, end *time.Time) bool {
	raw, ok := item["updated_at"].(string)
	if !ok || raw == "" {
		return true
	}
	t, err := parsePaddleTime(raw)
	if err != nil {
		return true
	}
	if start != nil && t.Before(*start) {
		return false
	}
	if end != nil && t.After(*end) {
		return false
	}
	return true
}

func parsePaddleTime(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, v)
}

var _ source.Source = (*PaddleSource)(nil)
