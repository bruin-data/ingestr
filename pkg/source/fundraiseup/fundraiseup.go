package fundraiseup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://api.fundraiseup.com/v1"
	// FundraiseUp API: concurrency-based limit of 3 parallel requests per account.
	rateLimit      = 2.0
	rateLimitBurst = 3
	maxPageSize    = 100
)

var supportedTables = []string{
	"donations",
	"donations:incremental",
	"events",
	"events:incremental",
	"fundraisers",
	"fundraisers:incremental",
	"recurring_plans",
	"recurring_plans:incremental",
	"supporters",
	"supporters:incremental",
}

type tableConfig struct {
	endpoint       string
	incrementalKey string
	strategy       config.IncrementalStrategy
	hasDateFilter  bool
}

var tableConfigs = map[string]tableConfig{
	"donations":                   {endpoint: "/donations", incrementalKey: "", strategy: config.StrategyReplace, hasDateFilter: false},
	"donations:incremental":       {endpoint: "/donations", incrementalKey: "created_at", strategy: config.StrategyMerge, hasDateFilter: true},
	"events":                      {endpoint: "/events", incrementalKey: "", strategy: config.StrategyReplace, hasDateFilter: false},
	"events:incremental":          {endpoint: "/events", incrementalKey: "created_at", strategy: config.StrategyMerge, hasDateFilter: true},
	"fundraisers":                 {endpoint: "/fundraisers", incrementalKey: "", strategy: config.StrategyReplace, hasDateFilter: false},
	"fundraisers:incremental":     {endpoint: "/fundraisers", incrementalKey: "created_at", strategy: config.StrategyMerge, hasDateFilter: true},
	"recurring_plans":             {endpoint: "/recurring_plans", incrementalKey: "", strategy: config.StrategyReplace, hasDateFilter: false},
	"recurring_plans:incremental": {endpoint: "/recurring_plans", incrementalKey: "created_at", strategy: config.StrategyMerge, hasDateFilter: true},
	"supporters":                  {endpoint: "/supporters", incrementalKey: "", strategy: config.StrategyReplace, hasDateFilter: false},
	"supporters:incremental":      {endpoint: "/supporters", incrementalKey: "created_at", strategy: config.StrategyMerge, hasDateFilter: true},
}

type FundraiseUpSource struct {
	client *gonghttp.Client
}

func NewFundraiseUpSource() *FundraiseUpSource {
	return &FundraiseUpSource{}
}

func (s *FundraiseUpSource) HandlesIncrementality() bool {
	return true
}

func (s *FundraiseUpSource) Schemes() []string {
	return []string{"fundraiseup"}
}

func parseURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "fundraiseup://") {
		return "", fmt.Errorf("invalid fundraiseup URI: must start with fundraiseup://")
	}

	rest := strings.TrimPrefix(uri, "fundraiseup://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse fundraiseup URI: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in fundraiseup URI: fundraiseup://?api_key=<key>")
	}

	return apiKey, nil
}

func (s *FundraiseUpSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(apiKey)),
	)

	config.Debug("[FUNDRAISEUP] Connected successfully")
	return nil
}

func (s *FundraiseUpSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *FundraiseUpSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	tc := tableConfigs[tableName]

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: tc.incrementalKey,
		TableStrategy:       tc.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("fundraiseup source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func (s *FundraiseUpSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		tc, ok := tableConfigs[table]
		if !ok {
			results <- source.RecordBatchResult{Err: fmt.Errorf("unsupported table: %s", table)}
			return
		}

		params := map[string]string{}
		if tc.hasDateFilter {
			if start := toRFC3339(opts.IntervalStart); start != "" {
				params["created[gte]"] = start
			}
			if end := toRFC3339(opts.IntervalEnd); end != "" {
				params["created[lte]"] = end
			}
		}

		config.Debug("[FUNDRAISEUP] reading %s", table)
		if err := s.paginateAndSend(ctx, tc.endpoint, opts, results, params); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// jsonUseNumber decodes JSON with UseNumber to preserve large integer precision.
func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

func (s *FundraiseUpSource) paginateAndSend(
	ctx context.Context,
	endpoint string,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	extraParams map[string]string,
) error {
	totalSent := 0
	startingAfter := ""

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("limit", fmt.Sprintf("%d", maxPageSize)).
			SetQueryParam("livemode", "true")

		for k, v := range extraParams {
			req.SetQueryParam(k, v)
		}

		if startingAfter != "" {
			req.SetQueryParam("starting_after", startingAfter)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("fundraiseup %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var result struct {
			Data    []map[string]any `json:"data"`
			HasMore bool             `json:"has_more"`
		}
		if err := jsonUseNumber(resp.Body(), &result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		items := result.Data
		if len(items) == 0 {
			break
		}

		if opts.Limit > 0 && totalSent+len(items) > opts.Limit {
			items = items[:opts.Limit-totalSent]
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", endpoint, err)
		}

		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(items)

		config.Debug("[FUNDRAISEUP] fetched %d records from %s (total: %d)", len(items), endpoint, totalSent)

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}

		if !result.HasMore {
			break
		}

		lastItem := result.Data[len(result.Data)-1]
		if id, ok := lastItem["id"].(string); ok {
			startingAfter = id
		} else {
			break
		}
	}

	if totalSent == 0 {
		config.Debug("[FUNDRAISEUP] no records found for %s", endpoint)
	}

	return nil
}

func toRFC3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
