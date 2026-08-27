package cloudflareradar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	baseURL = "https://api.cloudflare.com/client/v4"

	// Cloudflare's global Client API limit is 1,200 requests per five minutes.
	rateLimit         = 3.2
	rateLimitBurst    = 5
	maxOffsetPageSize = 1000
	maxPageNumberSize = 5000
)

type tableConfig struct {
	endpoint         string
	resultField      string
	primaryKey       string
	incrementalKey   string
	defaultDateRange string
	pageNumber       bool
	maxPageSize      int
}

var supportedTables = map[string]tableConfig{
	"annotations":             {endpoint: "/radar/annotations", resultField: "annotations", primaryKey: "id", incrementalKey: "startDate", defaultDateRange: "364d"},
	"autonomous_systems":      {endpoint: "/radar/entities/asns", resultField: "asns", primaryKey: "asn", maxPageSize: 100},
	"bgp_hijacks":             {endpoint: "/radar/bgp/hijacks/events", resultField: "events", primaryKey: "id", incrementalKey: "min_hijack_ts", pageNumber: true},
	"bgp_leaks":               {endpoint: "/radar/bgp/leaks/events", resultField: "events", primaryKey: "id", incrementalKey: "detected_ts", pageNumber: true},
	"bots":                    {endpoint: "/radar/bots", resultField: "bots", primaryKey: "slug"},
	"certificate_authorities": {endpoint: "/radar/ct/authorities", resultField: "certificateAuthorities", primaryKey: "sha256Fingerprint"},
	"certificate_logs":        {endpoint: "/radar/ct/logs", resultField: "certificateLogs", primaryKey: "slug"},
	"datasets":                {endpoint: "/radar/datasets", resultField: "datasets", primaryKey: "id", maxPageSize: 100},
	"geolocations":            {endpoint: "/radar/geolocations", resultField: "geolocations", primaryKey: "geoId"},
	"locations":               {endpoint: "/radar/entities/locations", resultField: "locations", primaryKey: "alpha2"},
	"outages":                 {endpoint: "/radar/annotations/outages", resultField: "annotations", primaryKey: "id", incrementalKey: "startDate", defaultDateRange: "364d"},
	"origins":                 {endpoint: "/radar/origins", resultField: "origins", primaryKey: "slug", maxPageSize: 10},
	"tlds":                    {endpoint: "/radar/tlds", resultField: "tlds", primaryKey: "tld", maxPageSize: 2000},
	"traffic_anomalies":       {endpoint: "/radar/traffic_anomalies", resultField: "trafficAnomalies", primaryKey: "uuid", incrementalKey: "startDate", defaultDateRange: "7d"},
}

type CloudflareRadarSource struct {
	client *httpclient.Client
}

func NewCloudflareRadarSource() *CloudflareRadarSource {
	return &CloudflareRadarSource{}
}

func (s *CloudflareRadarSource) HandlesIncrementality() bool {
	return true
}

func (s *CloudflareRadarSource) Schemes() []string {
	return []string{"cloudflare-radar", "cloudflareradar"}
}

func (s *CloudflareRadarSource) Connect(ctx context.Context, uri string) error {
	apiToken, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithRetry(5, 2*time.Second, 30*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(apiToken)),
	)
	return nil
}

func (s *CloudflareRadarSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("failed to parse Cloudflare Radar URI: %w", err)
	}
	if parsed.Scheme != "cloudflare-radar" && parsed.Scheme != "cloudflareradar" {
		return "", fmt.Errorf("invalid Cloudflare Radar URI: must start with cloudflare-radar:// or cloudflareradar://")
	}
	if parsed.Host != "" || parsed.Path != "" {
		return "", fmt.Errorf("invalid Cloudflare Radar URI: credentials must be query parameters")
	}

	apiToken := parsed.Query().Get("api_token")
	if apiToken == "" {
		return "", fmt.Errorf("api_token is required in Cloudflare Radar URI")
	}
	return apiToken, nil
}

func (s *CloudflareRadarSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if strings.HasPrefix(req.Name, apiTablePrefix) {
		return s.getAPITable(req)
	}

	table, ok := supportedTables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s (supported named tables: %s; use api:<endpoint> for any Radar GET endpoint)", req.Name, strings.Join(tableNames(), ", "))
	}

	strategy := config.StrategyReplace
	if table.incrementalKey != "" {
		strategy = config.StrategyMerge
	}
	var primaryKeys []string
	if table.primaryKey != "" {
		primaryKeys = []string{table.primaryKey}
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: table.incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("Cloudflare Radar source uses schema inference")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, table, opts)
		},
	}, nil
}

func tableNames() []string {
	return []string{
		"annotations", "autonomous_systems", "bgp_hijacks", "bgp_leaks", "bots",
		"certificate_authorities", "certificate_logs", "datasets", "geolocations", "locations",
		"origins", "outages", "tlds", "traffic_anomalies",
	}
}

func isValidTable(name string) bool {
	_, ok := supportedTables[name]
	return ok
}

func (s *CloudflareRadarSource) read(ctx context.Context, name string, table tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)

		var err error
		if name == "datasets" {
			err = s.readDatasets(ctx, table, opts, results)
		} else {
			err = s.paginateAndSend(ctx, name, table, opts, nil, results)
		}
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

func (s *CloudflareRadarSource) readDatasets(ctx context.Context, table tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	for _, datasetType := range []string{"RANKING_BUCKET", "REPORT"} {
		if err := s.paginateAndSend(ctx, "datasets", table, opts, map[string]string{"datasetType": datasetType}, results); err != nil {
			return err
		}
	}
	return nil
}

func (s *CloudflareRadarSource) paginateAndSend(ctx context.Context, name string, table tableConfig, opts source.ReadOptions, extra map[string]string, results chan<- source.RecordBatchResult) error {
	maxPageSize := maxOffsetPageSize
	if table.pageNumber {
		maxPageSize = maxPageNumberSize
	} else if table.maxPageSize > 0 {
		maxPageSize = table.maxPageSize
	}
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	for page := 0; ; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		params := make(map[string]string, len(extra)+4)
		for key, value := range extra {
			params[key] = value
		}
		if table.pageNumber {
			params["page"] = strconv.Itoa(page + 1)
			params["per_page"] = strconv.Itoa(pageSize)
		} else {
			params["offset"] = strconv.Itoa(page * pageSize)
			params["limit"] = strconv.Itoa(pageSize)
		}
		if table.incrementalKey != "" || table.defaultDateRange != "" {
			setDateRange(params, opts, table.defaultDateRange)
		}

		resp, err := s.client.R(ctx).SetQueryParams(params).Get(table.endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch Cloudflare Radar %s: %w", name, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("failed to fetch Cloudflare Radar %s: status %d: %s", name, resp.StatusCode(), resp.String())
		}

		items, err := decodeItems(resp.Body(), table.resultField)
		if err != nil {
			return fmt.Errorf("failed to decode Cloudflare Radar %s: %w", name, err)
		}
		if len(items) == 0 {
			return nil
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert Cloudflare Radar %s to Arrow: %w", name, err)
		}
		results <- source.RecordBatchResult{Batch: record}

		if len(items) < pageSize {
			return nil
		}
	}
}

func setDateRange(params map[string]string, opts source.ReadOptions, defaultDateRange string) {
	if opts.IntervalStart != nil && opts.IntervalEnd != nil {
		params["dateStart"] = opts.IntervalStart.UTC().Format(time.RFC3339)
		params["dateEnd"] = opts.IntervalEnd.UTC().Format(time.RFC3339)
		return
	}
	if opts.IntervalStart != nil {
		params["dateStart"] = opts.IntervalStart.UTC().Format(time.RFC3339)
		params["dateEnd"] = time.Now().UTC().Format(time.RFC3339)
		return
	}
	if opts.IntervalEnd != nil {
		params["dateStart"] = opts.IntervalEnd.AddDate(-1, 0, 0).UTC().Format(time.RFC3339)
		params["dateEnd"] = opts.IntervalEnd.UTC().Format(time.RFC3339)
		return
	}
	if defaultDateRange != "" {
		params["dateRange"] = defaultDateRange
	}
}

func decodeItems(body []byte, field string) ([]map[string]interface{}, error) {
	var envelope struct {
		Success bool                       `json:"success"`
		Result  map[string]json.RawMessage `json:"result"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		return nil, fmt.Errorf("API response was not successful")
	}

	raw, ok := envelope.Result[field]
	if !ok {
		return nil, fmt.Errorf("response is missing result.%s", field)
	}
	var items []map[string]interface{}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&items); err != nil {
		return nil, err
	}
	return items, nil
}
