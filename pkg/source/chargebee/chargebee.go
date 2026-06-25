package chargebee

import (
	"context"
	"encoding/json"
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
	baseURLFormat = "https://%s.chargebee.com/api/v2"

	maxPageSize        = 100
	rateLimitPerSecond = 2
	rateLimitBurst     = 1
)

type endpoint struct {
	path           string
	resourceKey    string
	incrementalKey string
}

var endpoints = map[string]endpoint{
	"customers":     {path: "/customers", resourceKey: "customer", incrementalKey: "updated_at"},
	"subscriptions": {path: "/subscriptions", resourceKey: "subscription", incrementalKey: "updated_at"},
	"invoices":      {path: "/invoices", resourceKey: "invoice", incrementalKey: "updated_at"},
	"transactions":  {path: "/transactions", resourceKey: "transaction", incrementalKey: "updated_at"},
	"orders":        {path: "/orders", resourceKey: "order", incrementalKey: "updated_at"},
	"events":        {path: "/events", resourceKey: "event", incrementalKey: "occurred_at"},
}

type listResponse struct {
	List       []map[string]json.RawMessage `json:"list"`
	NextOffset string                       `json:"next_offset"`
}

type ChargebeeSource struct {
	client *httpclient.Client
	site   string
	apiKey string
}

func NewChargebeeSource() *ChargebeeSource {
	return &ChargebeeSource{}
}

func (s *ChargebeeSource) Schemes() []string {
	return []string{"chargebee"}
}

func (s *ChargebeeSource) Connect(ctx context.Context, uri string) error {
	site, apiKey, err := parseChargebeeURI(uri)
	if err != nil {
		return err
	}

	s.site = site
	s.apiKey = apiKey

	s.client = httpclient.New(
		httpclient.WithBaseURL(fmt.Sprintf(baseURLFormat, site)),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBasicAuth(apiKey, "")),
		httpclient.WithRateLimiter(rateLimitPerSecond, rateLimitBurst),
	)

	config.Debug("[CHARGEBEE] Connected to site: %s", site)
	return nil
}

func parseChargebeeURI(uri string) (site, apiKey string, err error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse chargebee URI: %w", err)
	}

	if parsed.Scheme != "chargebee" {
		return "", "", fmt.Errorf("invalid chargebee URI: must start with chargebee://")
	}

	site = parsed.Host
	if site == "" {
		return "", "", fmt.Errorf("site is required in the Chargebee URI, expected format: chargebee://<site>?api_key=<api_key>")
	}
	site = strings.TrimSuffix(site, ".chargebee.com")

	apiKey = parsed.Query().Get("api_key")
	if apiKey == "" {
		return "", "", fmt.Errorf("api_key is required in the Chargebee URI")
	}

	return site, apiKey, nil
}

func (s *ChargebeeSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *ChargebeeSource) HandlesIncrementality() bool {
	return true
}

func (s *ChargebeeSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	ep, ok := endpoints[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported chargebee table %q, supported tables are: %s", req.Name, supportedTables())
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: ep.incrementalKey,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("chargebee source does not have a predefined schema; schema inference is required")
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

func (s *ChargebeeSource) read(ctx context.Context, table string, ep endpoint, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 4)

	go func() {
		defer close(results)
		if err := s.readEndpoint(ctx, table, ep, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *ChargebeeSource) readEndpoint(ctx context.Context, table string, ep endpoint, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	params := url.Values{}
	params.Set("limit", strconv.Itoa(maxPageSize))
	params.Set("sort_by[asc]", ep.incrementalKey)
	applyTimeFilter(params, ep.incrementalKey, toTime(opts.IntervalStart), toTime(opts.IntervalEnd))

	totalSent := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp listResponse
		httpResp, err := s.client.R(ctx).SetResult(&resp).SetQueryParamValues(params).Get(ep.path)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", table, err)
		}
		if !httpResp.IsSuccess() {
			return fmt.Errorf("chargebee %s request failed with status %d: %s", table, httpResp.StatusCode(), httpResp.String())
		}

		items := make([]map[string]interface{}, 0, len(resp.List))
		for _, entry := range resp.List {
			raw, ok := entry[ep.resourceKey]
			if !ok {
				continue
			}
			var obj map[string]interface{}
			if err := json.Unmarshal(raw, &obj); err != nil {
				return fmt.Errorf("failed to decode %s record: %w", table, err)
			}
			items = append(items, obj)
		}

		if len(items) > 0 {
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return err
			}
			totalSent += len(items)
			config.Debug("[CHARGEBEE] Sending batch of %d %s (total: %d)", len(items), table, totalSent)
			results <- source.RecordBatchResult{Batch: rec}
		}

		if resp.NextOffset == "" {
			break
		}
		params.Set("offset", resp.NextOffset)
	}

	config.Debug("[CHARGEBEE] Finished reading %s, total: %d", table, totalSent)
	return nil
}

func applyTimeFilter(params url.Values, field string, start, end *time.Time) {
	switch {
	case start != nil && end != nil:
		params.Set(field+"[between]", fmt.Sprintf("[%d,%d]", start.Unix(), end.Unix()))
	case start != nil:
		params.Set(field+"[after]", strconv.FormatInt(start.Unix()-1, 10))
	case end != nil:
		params.Set(field+"[before]", strconv.FormatInt(end.Unix()+1, 10))
	}
}

func toTime(v *time.Time) *time.Time {
	if v == nil || v.IsZero() {
		return nil
	}
	return v
}

var _ source.Source = (*ChargebeeSource)(nil)
