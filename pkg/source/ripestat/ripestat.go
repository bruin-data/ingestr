package ripestat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
)

const baseURL = "https://stat.ripe.net"

var endpointPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var intervalEndpoints = map[string]struct{}{
	"allocation-history":     {},
	"announced-prefixes":     {},
	"asn-neighbours-history": {},
	"atlas-probe-deployment": {},
	"bgp-update-activity":    {},
	"bgp-updates":            {},
	"bgplay":                 {},
	"country-resource-stats": {},
	"prefix-count":           {},
	"rir":                    {},
	"ris-peer-count":         {},
	"routing-history":        {},
}

type RIPEstatSource struct {
	client *httpclient.Client
}

func NewRIPEstatSource() *RIPEstatSource {
	return &RIPEstatSource{}
}

func (s *RIPEstatSource) Schemes() []string {
	return []string{"ripestat"}
}

func (s *RIPEstatSource) Connect(ctx context.Context, uri string) error {
	if err := parseURI(uri); err != nil {
		return err
	}

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(120*time.Second),
		httpclient.WithDebug(config.DebugMode),
	)

	config.Debug("[RIPESTAT] Connected successfully")
	return nil
}

func parseURI(uri string) error {
	parsed, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("failed to parse RIPEstat URI: %w", err)
	}
	if parsed.Scheme != "ripestat" {
		return fmt.Errorf("invalid RIPEstat URI: must be ripestat://")
	}
	if parsed.Host != "" || parsed.Path != "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return fmt.Errorf("invalid RIPEstat URI: use ripestat:// and supply API parameters on the source table")
	}
	return nil
}

func (s *RIPEstatSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *RIPEstatSource) HandlesIncrementality() bool {
	return true
}

func (s *RIPEstatSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	endpoint, params, err := parseTable(req.Name)
	if err != nil {
		return nil, err
	}
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           endpoint,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: "",
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("RIPEstat source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			query, err := applyIntervalParams(endpoint, params, opts)
			if err != nil {
				return nil, err
			}
			return s.read(ctx, endpoint, query, opts), nil
		},
	}, nil
}

func parseTable(raw string) (string, url.Values, error) {
	endpoint, params, _, err := tablespec.Split(raw)
	if err != nil {
		return "", nil, err
	}
	endpoint = strings.TrimSpace(endpoint)
	if !isValidEndpoint(endpoint) {
		return "", nil, fmt.Errorf("invalid RIPEstat endpoint %q: use the lowercase endpoint name from the RIPEstat Data API documentation", endpoint)
	}
	if params.Has("callback") {
		return "", nil, fmt.Errorf("callback is not supported because the source requires a JSON response")
	}
	if !params.Has("sourceapp") {
		params.Set("sourceapp", "ingestr")
	}
	if !params.Has("data_overload_limit") {
		params.Set("data_overload_limit", "ignore")
	}
	return endpoint, params, nil
}

func isValidEndpoint(endpoint string) bool {
	return endpointPattern.MatchString(endpoint)
}

func applyIntervalParams(endpoint string, params url.Values, opts source.ReadOptions) (url.Values, error) {
	if opts.IntervalStart == nil && opts.IntervalEnd == nil {
		return params, nil
	}
	if opts.IntervalStart == nil || opts.IntervalEnd == nil {
		return nil, fmt.Errorf("RIPEstat intervals require both --interval-start and --interval-end")
	}
	if _, ok := intervalEndpoints[endpoint]; !ok {
		return nil, fmt.Errorf("RIPEstat endpoint %q does not support starttime/endtime; omit --interval-start and --interval-end", endpoint)
	}

	query := make(url.Values, len(params))
	for key, values := range params {
		query[key] = values
	}
	query.Set("starttime", opts.IntervalStart.UTC().Format(time.RFC3339))
	query.Set("endtime", opts.IntervalEnd.UTC().Format(time.RFC3339))
	return query, nil
}

func (s *RIPEstatSource) read(ctx context.Context, endpoint string, params url.Values, opts source.ReadOptions) <-chan source.RecordBatchResult {
	results := make(chan source.RecordBatchResult, 1)

	go func() {
		defer close(results)

		item, err := s.fetch(ctx, endpoint, params)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}
		if len(item) == 0 {
			return
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{item}, nil, opts.ExcludeColumns)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert RIPEstat endpoint %q to Arrow: %w", endpoint, err)}
			return
		}
		results <- source.RecordBatchResult{Batch: record}
	}()

	return results
}

func (s *RIPEstatSource) fetch(ctx context.Context, endpoint string, params url.Values) (map[string]interface{}, error) {
	path := fmt.Sprintf("/data/%s/data.json", endpoint)
	config.Debug("[RIPESTAT] Fetching endpoint: %s", endpoint)

	resp, err := s.client.R(ctx).SetQueryParamValues(params).Get(path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch RIPEstat endpoint %q: %w", endpoint, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("RIPEstat endpoint %q returned HTTP status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	var envelope struct {
		Status     string          `json:"status"`
		StatusCode int             `json:"status_code"`
		Data       json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse RIPEstat endpoint %q response: %w", endpoint, err)
	}
	if envelope.Status != "ok" || envelope.StatusCode != 200 {
		return nil, fmt.Errorf("RIPEstat endpoint %q returned API status %q (%d): %s", endpoint, envelope.Status, envelope.StatusCode, resp.String())
	}

	item, err := decodeData(envelope.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse data from RIPEstat endpoint %q: %w", endpoint, err)
	}
	return item, nil
}

func decodeData(data json.RawMessage) (map[string]interface{}, error) {
	var item map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&item); err != nil {
		return nil, err
	}
	return item, nil
}
