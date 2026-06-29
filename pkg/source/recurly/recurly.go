package recurly

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
	usBaseURL = "https://v3.recurly.com"
	euBaseURL = "https://v3.eu.recurly.com"

	apiVersion   = "v2021-02-25"
	acceptHeader = "application/vnd.recurly." + apiVersion

	maxPageSize        = 200
	rateLimitPerSecond = 5
	rateLimitBurst     = 5
)

type endpoint struct {
	path string
}

var endpoints = map[string]endpoint{
	"accounts":      {path: "/accounts"},
	"subscriptions": {path: "/subscriptions"},
	"invoices":      {path: "/invoices"},
	"transactions":  {path: "/transactions"},
	"plans":         {path: "/plans"},
}

type listResponse struct {
	HasMore bool                     `json:"has_more"`
	Next    string                   `json:"next"`
	Data    []map[string]interface{} `json:"data"`
}

type RecurlySource struct {
	client *httpclient.Client
	apiKey string
}

func NewRecurlySource() *RecurlySource {
	return &RecurlySource{}
}

func (s *RecurlySource) Schemes() []string {
	return []string{"recurly"}
}

func (s *RecurlySource) Connect(ctx context.Context, uri string) error {
	apiKey, baseURL, err := parseRecurlyURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBasicAuth(apiKey, "")),
		httpclient.WithHeader("Accept", acceptHeader),
		httpclient.WithRateLimiter(rateLimitPerSecond, rateLimitBurst),
	)

	config.Debug("[RECURLY] Connected successfully")
	return nil
}

func parseRecurlyURI(uri string) (apiKey, baseURL string, err error) {
	if !strings.HasPrefix(uri, "recurly://") {
		return "", "", fmt.Errorf("invalid recurly URI: must start with recurly://")
	}

	rest := strings.TrimPrefix(uri, "recurly://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse recurly URI query: %w", err)
	}

	apiKey = values.Get("api_key")
	if apiKey == "" {
		return "", "", fmt.Errorf("api_key is required in recurly URI")
	}

	switch strings.ToLower(values.Get("region")) {
	case "", "us":
		baseURL = usBaseURL
	case "eu":
		baseURL = euBaseURL
	default:
		return "", "", fmt.Errorf("invalid recurly region %q, supported regions are: us, eu", values.Get("region"))
	}

	return apiKey, baseURL, nil
}

func (s *RecurlySource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *RecurlySource) HandlesIncrementality() bool {
	return true
}

func (s *RecurlySource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	ep, ok := endpoints[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported recurly table %q, supported tables are: %s", req.Name, supportedTables())
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: "updated_at",
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("recurly source does not have a predefined schema; schema inference is required")
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

func (s *RecurlySource) read(ctx context.Context, table string, ep endpoint, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 4)

	go func() {
		defer close(results)
		if err := s.readEndpoint(ctx, table, ep, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *RecurlySource) readEndpoint(ctx context.Context, table string, ep endpoint, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	firstParams := url.Values{}
	firstParams.Set("limit", strconv.Itoa(maxPageSize))
	firstParams.Set("sort", "updated_at")
	firstParams.Set("order", "asc")
	applyTimeFilter(firstParams, toTime(opts.IntervalStart), toTime(opts.IntervalEnd))

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
			return fmt.Errorf("recurly %s request failed with status %d: %s", table, httpResp.StatusCode(), httpResp.String())
		}

		if len(resp.Data) > 0 {
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(resp.Data, nil, opts.ExcludeColumns)
			if err != nil {
				return err
			}
			totalSent += len(resp.Data)
			config.Debug("[RECURLY] Sending batch of %d %s (total: %d)", len(resp.Data), table, totalSent)
			results <- source.RecordBatchResult{Batch: rec}
		}

		if !resp.HasMore || resp.Next == "" {
			break
		}

		requestURL = resp.Next
		useParams = false
	}

	config.Debug("[RECURLY] Finished reading %s, total: %d", table, totalSent)
	return nil
}

func applyTimeFilter(params url.Values, start, end *time.Time) {
	if start != nil {
		params.Set("begin_time", start.UTC().Format(time.RFC3339))
	}
	if end != nil {
		params.Set("end_time", end.UTC().Format(time.RFC3339))
	}
}

func toTime(v *time.Time) *time.Time {
	if v == nil || v.IsZero() {
		return nil
	}
	return v
}

var _ source.Source = (*RecurlySource)(nil)
