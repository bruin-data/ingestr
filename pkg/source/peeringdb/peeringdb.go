package peeringdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
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
	baseURL     = "https://www.peeringdb.com/api"
	maxPageSize = 5000
	// Caps a window's id range, and so its row count, after it widens over sparse ids.
	maxWindowWidth = 8 * maxPageSize
	// PeeringDB sets `updated` slightly before a change becomes visible, so each window starts
	// earlier to catch rows that appeared after the previous run; merge absorbs the overlap.
	lookback = 30 * time.Second
	// PeeringDB allows 20 queries/minute per IP anonymously and 40/minute per API key; 80% of each.
	anonymousRateLimit      = 16.0 / 60.0
	anonymousRateLimitBurst = 2
	apiKeyRateLimit         = 32.0 / 60.0
	apiKeyRateLimitBurst    = 5
	// The limit is per IP or key, so concurrent runs share it and can still be throttled.
	maxAttempts  = 6
	retryWait    = 5 * time.Second
	retryMaxWait = 2 * time.Minute
)

var supportedTables = []string{
	"organizations",
	"campuses",
	"facilities",
	"carriers",
	"carrier_facilities",
	"exchanges",
	"exchange_facilities",
	"exchange_lans",
	"exchange_prefixes",
	"networks",
	"network_facilities",
	"network_exchange_lans",
	"network_contacts",
}

// endpoints maps each incremental table to its PeeringDB object type.
var endpoints = map[string]string{
	"organizations":         "org",
	"campuses":              "campus",
	"facilities":            "fac",
	"carriers":              "carrier",
	"carrier_facilities":    "carrierfac",
	"exchanges":             "ix",
	"exchange_facilities":   "ixfac",
	"exchange_lans":         "ixlan",
	"exchange_prefixes":     "ixpfx",
	"networks":              "net",
	"network_facilities":    "netfac",
	"network_exchange_lans": "netixlan",
	"network_contacts":      "poc",
}

type PeeringDBSource struct {
	client *httpclient.Client
}

func NewPeeringDBSource() *PeeringDBSource {
	return &PeeringDBSource{}
}

func (s *PeeringDBSource) Schemes() []string {
	return []string{"peeringdb"}
}

func (s *PeeringDBSource) HandlesIncrementality() bool {
	return true
}

func (s *PeeringDBSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}

	opts := []httpclient.Option{
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(120 * time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithDisableRetry(),
	}
	if apiKey != "" {
		opts = append(opts,
			httpclient.WithHeader("Authorization", "Api-Key "+apiKey),
			httpclient.WithRateLimiter(apiKeyRateLimit, apiKeyRateLimitBurst),
		)
	} else {
		opts = append(opts, httpclient.WithRateLimiter(anonymousRateLimit, anonymousRateLimitBurst))
	}
	s.client = httpclient.New(opts...)

	config.Debug("[PEERINGDB] Connected successfully (authenticated: %t)", apiKey != "")
	return nil
}

func (s *PeeringDBSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("failed to parse PeeringDB URI: %w", err)
	}
	if parsed.Scheme != "peeringdb" {
		return "", fmt.Errorf("invalid PeeringDB URI: must start with peeringdb://")
	}
	if parsed.Host != "" || parsed.Path != "" || parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid PeeringDB URI: use peeringdb:// or peeringdb://?api_key=<key>")
	}

	query := parsed.Query()
	for key := range query {
		if key != "api_key" {
			return "", fmt.Errorf("invalid PeeringDB URI: unknown parameter %q (supported: api_key)", key)
		}
	}
	if query.Has("api_key") && strings.TrimSpace(query.Get("api_key")) == "" {
		return "", fmt.Errorf("invalid PeeringDB URI: api_key is empty")
	}
	return strings.TrimSpace(query.Get("api_key")), nil
}

func (s *PeeringDBSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	table := req.Name
	if !isValidTable(table) {
		return nil, fmt.Errorf("unsupported PeeringDB table: %s (supported: %s)", table, strings.Join(supportedTables, ", "))
	}

	return &source.DynamicSourceTable{
		TableName:           table,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: "updated",
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("PeeringDB source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, endpoints[table], opts), nil
		},
	}, nil
}

func isValidTable(table string) bool {
	return slices.Contains(supportedTables, table)
}

func (s *PeeringDBSource) read(ctx context.Context, table string, opts source.ReadOptions) <-chan source.RecordBatchResult {
	results := make(chan source.RecordBatchResult, 2)

	go func() {
		defer close(results)

		config.Debug("[PEERINGDB] reading %s", table)
		live := intervalParams(opts)
		if err := s.readIDWindows(ctx, table, live, opts, results); err != nil {
			sendErr(ctx, results, err)
			return
		}

		// PeeringDB only returns deleted rows when `since` is set.
		deleted := intervalParams(opts)
		deleted.Set("status", "deleted")
		since := int64(1)
		if opts.IntervalStart != nil {
			since = max(opts.IntervalStart.Add(-lookback).Unix(), since)
		}
		deleted.Set("since", strconv.FormatInt(since, 10))
		if err := s.readIDWindows(ctx, table, deleted, opts, results); err != nil {
			sendErr(ctx, results, err)
		}
	}()

	return results
}

func sendErr(ctx context.Context, results chan<- source.RecordBatchResult, err error) {
	select {
	case results <- source.RecordBatchResult{Err: err}:
	case <-ctx.Done():
	}
}

// readIDWindows pages through (lo, hi] id ranges because the API has no sort parameter and its default
// order differs per object (net sorts by allow_ixp_update first). Windows carry no limit: poc applies
// limit before hiding non-public contacts, so a short limited page would not mean the window is complete.
func (s *PeeringDBSource) readIDWindows(ctx context.Context, table string, base url.Values, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	lo, width := int64(0), int64(maxPageSize)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		hi := lo + width
		params := maps.Clone(base)
		params.Set("id__gt", strconv.FormatInt(lo, 10))
		params.Set("id__lte", strconv.FormatInt(hi, 10))
		resp, err := s.fetchPage(ctx, "/"+table, params)
		if err != nil {
			return err
		}

		if len(resp.Data) > 0 {
			if err := sendBatches(ctx, table, resp.Data, opts, results); err != nil {
				return err
			}
		} else {
			// No limit here either, so hidden contacts can't make the probe look empty;
			// it returns only ids, and the next window starts at the first one.
			probe := maps.Clone(base)
			probe.Set("id__gt", strconv.FormatInt(hi, 10))
			probe.Set("fields", "id")
			rest, err := s.fetchPage(ctx, "/"+table, probe)
			if err != nil {
				return err
			}
			if len(rest.Data) == 0 {
				return nil
			}
			next, err := minID(rest.Data)
			if err != nil {
				return fmt.Errorf("failed to read ids from PeeringDB %s: %w", table, err)
			}
			hi = next - 1
		}

		lo = hi
		switch {
		case len(resp.Data) > maxPageSize:
			width = max(width/2, maxPageSize)
		case len(resp.Data) < maxPageSize/2:
			width = min(width*2, maxWindowWidth)
		}
	}
}

func minID(items []map[string]interface{}) (int64, error) {
	var lowest int64
	for i, item := range items {
		raw, ok := item["id"].(json.Number)
		if !ok {
			return 0, fmt.Errorf("row has no numeric id")
		}
		id, err := raw.Int64()
		if err != nil {
			return 0, err
		}
		if i == 0 || id < lowest {
			lowest = id
		}
	}
	return lowest, nil
}

func intervalParams(opts source.ReadOptions) url.Values {
	params := url.Values{}
	if opts.IntervalStart != nil {
		params.Set("updated__gte", opts.IntervalStart.Add(-lookback).UTC().Format(time.RFC3339))
	}
	if opts.IntervalEnd != nil {
		params.Set("updated__lte", opts.IntervalEnd.UTC().Format(time.RFC3339))
	}
	return params
}

type page struct {
	Data []map[string]interface{} `json:"data"`
}

func (s *PeeringDBSource) fetchPage(ctx context.Context, path string, params url.Values) (*page, error) {
	var resp *httpclient.Response
	var err error
	for attempt := 1; ; attempt++ {
		resp, err = s.client.R(ctx).SetQueryParamValues(params).Get(path)
		retryable := err != nil || resp.StatusCode() == http.StatusTooManyRequests || resp.StatusCode() >= 500
		if !retryable || attempt == maxAttempts {
			break
		}

		wait := min(retryWait<<(attempt-1), retryMaxWait)
		if err == nil && resp.StatusCode() == http.StatusTooManyRequests {
			// Guests that repeat a large request are throttled for up to an hour; fail instead of hanging.
			if secs, convErr := strconv.Atoi(resp.Header().Get("Retry-After")); convErr == nil {
				wait = time.Duration(secs+1) * time.Second
				if wait > retryMaxWait {
					return nil, fmt.Errorf("PeeringDB throttled %s for %d seconds; set api_key in the URI for higher limits or retry later: %s", path, secs, resp.String())
				}
			}
		}
		config.Debug("[PEERINGDB] retrying %s in %s (attempt %d/%d)", path, wait, attempt, maxAttempts)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PeeringDB %s: %w", path, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("PeeringDB %s returned HTTP status %d: %s", path, resp.StatusCode(), resp.String())
	}

	var result page
	decoder := json.NewDecoder(bytes.NewReader(resp.Body()))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse PeeringDB %s response: %w", path, err)
	}
	return &result, nil
}

func sendBatches(ctx context.Context, table string, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	start, accBytes := 0, int64(0)
	for i, item := range items {
		if opts.MaxBatchBytes > 0 {
			rowBytes := arrowconv.RowBytes(item)
			if i > start && accBytes+rowBytes > opts.MaxBatchBytes {
				if err := sendBatch(ctx, table, items[start:i], opts, results); err != nil {
					return err
				}
				start, accBytes = i, 0
			}
			accBytes += rowBytes
		}
	}
	return sendBatch(ctx, table, items[start:], opts, results)
}

func sendBatch(ctx context.Context, table string, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert PeeringDB %s to Arrow: %w", table, err)
	}
	select {
	case results <- source.RecordBatchResult{Batch: record}:
		return nil
	case <-ctx.Done():
		record.Release()
		return ctx.Err()
	}
}
