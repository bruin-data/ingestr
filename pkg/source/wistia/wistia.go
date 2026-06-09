package wistia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	defaultBaseURL     = "https://api.wistia.com/modern"
	defaultAPIVersion  = "2026-03"
	defaultPageSize    = 100
	maxPageSize        = 100
	rateLimit          = 8.0
	rateLimitBurst     = 5
	apiVersionHeader   = "X-Wistia-API-Version"
	authorizationError = "access_token or api_key is required in wistia URI"
)

type tableConfig struct {
	endpoint         string
	paginated        bool
	arrayResponse    bool
	requiresParam    bool
	allowsParam      bool
	queryParam       string
	paramColumn      string
	dateFilter       bool
	defaultDateRange bool
	primaryKeys      []string
	incrementalKey   string
	strategy         config.IncrementalStrategy
	partitionBy      string
}

var tableConfigs = map[string]tableConfig{
	"account": {
		endpoint:    "/account",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"token": {
		endpoint: "/token",
		strategy: config.StrategyReplace,
	},
	"allowed_domains": {
		endpoint:      "/allowed_domains",
		paginated:     true,
		arrayResponse: true,
		primaryKeys:   []string{"domain"},
		strategy:      config.StrategyReplace,
	},
	"folders": {
		endpoint:      "/folders",
		paginated:     true,
		arrayResponse: true,
		primaryKeys:   []string{"hashed_id"},
		strategy:      config.StrategyReplace,
	},
	"folder": {
		endpoint:      "/folders/{id}",
		requiresParam: true,
		primaryKeys:   []string{"hashed_id"},
		strategy:      config.StrategyReplace,
	},
	"folder_sharings": {
		endpoint:      "/folders/{id}/sharings",
		paginated:     true,
		arrayResponse: true,
		requiresParam: true,
		paramColumn:   "folder_id",
		primaryKeys:   []string{"folder_id", "id"},
		strategy:      config.StrategyReplace,
	},
	"subfolders": {
		endpoint:      "/folders/{id}/subfolders",
		paginated:     true,
		arrayResponse: true,
		requiresParam: true,
		paramColumn:   "folder_id",
		primaryKeys:   []string{"folder_id", "id"},
		strategy:      config.StrategyReplace,
	},
	"medias": {
		endpoint:      "/medias",
		paginated:     true,
		arrayResponse: true,
		primaryKeys:   []string{"hashed_id"},
		strategy:      config.StrategyReplace,
	},
	"media": {
		endpoint:      "/medias/{id}",
		requiresParam: true,
		primaryKeys:   []string{"hashed_id"},
		strategy:      config.StrategyReplace,
	},
	"captions": {
		endpoint:      "/captions",
		paginated:     true,
		arrayResponse: true,
		allowsParam:   true,
		queryParam:    "media_id",
		paramColumn:   "media_id",
		primaryKeys:   []string{"id"},
		strategy:      config.StrategyReplace,
	},
	"media_captions": {
		endpoint:      "/medias/{id}/captions",
		paginated:     true,
		arrayResponse: true,
		requiresParam: true,
		paramColumn:   "media_id",
		primaryKeys:   []string{"media_id", "language"},
		strategy:      config.StrategyReplace,
	},
	"media_localizations": {
		endpoint:      "/medias/{id}/localizations",
		paginated:     true,
		arrayResponse: true,
		requiresParam: true,
		paramColumn:   "media_id",
		primaryKeys:   []string{"media_id", "hashed_id"},
		strategy:      config.StrategyReplace,
	},
	"media_customizations": {
		endpoint:      "/medias/{id}/customizations",
		requiresParam: true,
		paramColumn:   "media_id",
		primaryKeys:   []string{"media_id"},
		strategy:      config.StrategyReplace,
	},
	"media_stats": {
		endpoint:      "/medias/{id}/stats",
		requiresParam: true,
		paramColumn:   "media_id",
		primaryKeys:   []string{"media_id"},
		strategy:      config.StrategyReplace,
	},
	"channels": {
		endpoint:      "/channels",
		paginated:     true,
		arrayResponse: true,
		primaryKeys:   []string{"hashed_id"},
		strategy:      config.StrategyReplace,
	},
	"channel": {
		endpoint:      "/channels/{id}",
		requiresParam: true,
		primaryKeys:   []string{"hashed_id"},
		strategy:      config.StrategyReplace,
	},
	"channel_episodes": {
		endpoint:      "/channel_episodes",
		paginated:     true,
		arrayResponse: true,
		primaryKeys:   []string{"hashed_id"},
		strategy:      config.StrategyReplace,
	},
	"channel_episodes_by_channel": {
		endpoint:      "/channels/{id}/channel_episodes",
		paginated:     true,
		arrayResponse: true,
		requiresParam: true,
		paramColumn:   "channel_id",
		primaryKeys:   []string{"channel_id", "hashed_id"},
		strategy:      config.StrategyReplace,
	},
	"tags": {
		endpoint:      "/tags",
		paginated:     true,
		arrayResponse: true,
		primaryKeys:   []string{"name"},
		strategy:      config.StrategyReplace,
	},
	"webinars": {
		endpoint:      "/webinars",
		paginated:     true,
		arrayResponse: true,
		primaryKeys:   []string{"id"},
		strategy:      config.StrategyReplace,
	},
	"webinar": {
		endpoint:      "/webinars/{id}",
		requiresParam: true,
		primaryKeys:   []string{"id"},
		strategy:      config.StrategyReplace,
	},
	"stats_account": {
		endpoint: "/stats/account",
		strategy: config.StrategyReplace,
	},
	"stats_account_by_date": {
		endpoint:         "/stats/account/by_date",
		arrayResponse:    true,
		dateFilter:       true,
		defaultDateRange: true,
		primaryKeys:      []string{"date"},
		incrementalKey:   "date",
		strategy:         config.StrategyMerge,
		partitionBy:      "date",
	},
	"stats_events": {
		endpoint:       "/stats/events",
		paginated:      true,
		arrayResponse:  true,
		allowsParam:    true,
		queryParam:     "media_id",
		paramColumn:    "media_id",
		dateFilter:     true,
		primaryKeys:    []string{"event_key"},
		incrementalKey: "received_at",
		strategy:       config.StrategyMerge,
		partitionBy:    "received_at",
	},
	"stats_events_by_visitor": {
		endpoint:       "/stats/events",
		paginated:      true,
		arrayResponse:  true,
		requiresParam:  true,
		queryParam:     "visitor_key",
		paramColumn:    "visitor_key",
		dateFilter:     true,
		primaryKeys:    []string{"event_key"},
		incrementalKey: "received_at",
		strategy:       config.StrategyMerge,
		partitionBy:    "received_at",
	},
	"stats_visitors": {
		endpoint:      "/stats/visitors",
		paginated:     true,
		arrayResponse: true,
		primaryKeys:   []string{"visitor_key"},
		strategy:      config.StrategyReplace,
	},
	"stats_event": {
		endpoint:      "/stats/events/{id}",
		requiresParam: true,
		primaryKeys:   []string{"event_key"},
		strategy:      config.StrategyReplace,
	},
	"stats_visitor": {
		endpoint:      "/stats/visitors/{id}",
		requiresParam: true,
		primaryKeys:   []string{"visitor_key"},
		strategy:      config.StrategyReplace,
	},
	"stats_media": {
		endpoint:      "/stats/medias/{id}",
		requiresParam: true,
		paramColumn:   "media_id",
		primaryKeys:   []string{"media_id"},
		strategy:      config.StrategyReplace,
	},
	"stats_media_by_date": {
		endpoint:       "/stats/medias/{id}/by_date",
		arrayResponse:  true,
		requiresParam:  true,
		paramColumn:    "media_id",
		dateFilter:     true,
		primaryKeys:    []string{"media_id", "date"},
		incrementalKey: "date",
		strategy:       config.StrategyMerge,
		partitionBy:    "date",
	},
	"stats_media_engagement": {
		endpoint:      "/stats/medias/{id}/engagement",
		requiresParam: true,
		paramColumn:   "media_id",
		primaryKeys:   []string{"media_id"},
		strategy:      config.StrategyReplace,
	},
	"stats_project": {
		endpoint:      "/stats/projects/{id}",
		requiresParam: true,
		paramColumn:   "project_id",
		primaryKeys:   []string{"project_id"},
		strategy:      config.StrategyReplace,
	},
}

type WistiaSource struct {
	accessToken string
	apiVersion  string
	client      *ingestrhttp.Client
}

type wistiaCredentials struct {
	accessToken string
	apiVersion  string
	apiURL      string
}

func NewWistiaSource() *WistiaSource {
	return &WistiaSource{}
}

func (s *WistiaSource) HandlesIncrementality() bool {
	return true
}

func (s *WistiaSource) Schemes() []string {
	return []string{"wistia"}
}

func parseWistiaURI(uri string) (*wistiaCredentials, error) {
	if !strings.HasPrefix(uri, "wistia://") {
		return nil, fmt.Errorf("invalid wistia URI: must start with wistia://")
	}

	rest := strings.TrimPrefix(uri, "wistia://")
	if rest == "" || rest == "?" {
		return nil, errors.New(authorizationError)
	}

	var values url.Values
	var accessToken string
	if !strings.Contains(rest, "=") && !strings.HasPrefix(rest, "?") {
		token, err := url.QueryUnescape(strings.Trim(rest, "/"))
		if err != nil {
			return nil, fmt.Errorf("failed to parse wistia token: %w", err)
		}
		accessToken = token
		values = url.Values{}
	} else {
		rest = strings.TrimPrefix(rest, "?")
		parsed, err := url.ParseQuery(rest)
		if err != nil {
			return nil, fmt.Errorf("failed to parse wistia URI query: %w", err)
		}
		values = parsed
		accessToken = firstNonEmpty(values.Get("access_token"), values.Get("api_key"), values.Get("token"))
	}

	if accessToken == "" {
		return nil, errors.New(authorizationError)
	}

	apiVersion := values.Get("api_version")
	if apiVersion == "" {
		apiVersion = defaultAPIVersion
	}

	apiURL := values.Get("base_url")
	if apiURL == "" {
		apiURL = defaultBaseURL
	}

	return &wistiaCredentials{
		accessToken: accessToken,
		apiVersion:  apiVersion,
		apiURL:      strings.TrimRight(apiURL, "/"),
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *WistiaSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseWistiaURI(uri)
	if err != nil {
		return err
	}

	s.accessToken = creds.accessToken
	s.apiVersion = creds.apiVersion
	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(creds.apiURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth(s.accessToken)),
		ingestrhttp.WithHeader("Accept", "application/json"),
		ingestrhttp.WithHeader(apiVersionHeader, s.apiVersion),
	)

	config.Debug("[WISTIA] Connected successfully with API version %s", s.apiVersion)
	return nil
}

func (s *WistiaSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *WistiaSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	cfg, tableName, err := resolveTable(req.Name)
	if err != nil {
		return nil, err
	}

	primaryKeys := cfg.primaryKeys
	if len(primaryKeys) == 0 {
		primaryKeys = req.PrimaryKeys
	}

	strategy := cfg.strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: cfg.incrementalKey,
		TableStrategy:       strategy,
		TablePartitionBy:    cfg.partitionBy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("wistia source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func resolveTable(table string) (tableConfig, string, error) {
	base, param := parseTableName(table)
	cfg, ok := tableConfigs[base]
	if !ok {
		return tableConfig{}, "", fmt.Errorf("unsupported table: %s (supported: %s)", table, strings.Join(supportedTableNames(), ", "))
	}
	if cfg.requiresParam && param == "" {
		return tableConfig{}, "", fmt.Errorf("%s requires an id parameter, e.g. %s:abc123", base, base)
	}
	if param != "" && !cfg.requiresParam && !cfg.allowsParam {
		return tableConfig{}, "", fmt.Errorf("%s does not accept an id parameter", base)
	}
	return cfg, table, nil
}

func parseTableName(table string) (string, string) {
	base, param, found := strings.Cut(table, ":")
	if !found {
		return table, ""
	}
	return base, param
}

func supportedTableNames() []string {
	names := make([]string, 0, len(tableConfigs))
	for name, cfg := range tableConfigs {
		if cfg.requiresParam {
			names = append(names, name+":<id>")
		} else if cfg.allowsParam {
			names = append(names, name+"[:<id>]")
		} else {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (s *WistiaSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	cfg, _, err := resolveTable(table)
	if err != nil {
		return nil, err
	}

	go func() {
		defer close(results)

		var err error
		if cfg.paginated {
			err = s.readPaginated(ctx, table, cfg, opts, results)
		} else {
			err = s.readOnce(ctx, table, cfg, opts, results)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *WistiaSource) readPaginated(ctx context.Context, table string, cfg tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	_, param := parseTableName(table)
	pageSize := pageSizeFromOptions(opts)
	total := 0

	for page := 1; ; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("page", strconv.Itoa(page)).
			SetQueryParam("per_page", strconv.Itoa(pageSize))
		applyConfiguredParams(req, cfg, param, opts)

		resp, err := req.Get(endpointFor(cfg, param))
		if err != nil {
			return fmt.Errorf("failed to fetch %s page %d: %w", table, page, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("%s page %d returned status %d: %s", table, page, resp.StatusCode(), resp.String())
		}

		items, err := responseItems(resp.Body(), true)
		if err != nil {
			return fmt.Errorf("failed to parse %s page %d response: %w", table, page, err)
		}
		addParamColumn(items, cfg, param)

		if opts.Limit > 0 {
			remaining := opts.Limit - total
			if len(items) > remaining {
				items = items[:remaining]
			}
		}

		if len(items) > 0 {
			if err := sendBatch(ctx, items, opts, results); err != nil {
				return err
			}
			total += len(items)
			config.Debug("[WISTIA] Fetched %d rows from %s page %d (total: %d)", len(items), table, page, total)
		}

		if len(items) < pageSize || (opts.Limit > 0 && total >= opts.Limit) {
			break
		}
	}

	return nil
}

func (s *WistiaSource) readOnce(ctx context.Context, table string, cfg tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	_, param := parseTableName(table)
	req := s.client.R(ctx)
	applyConfiguredParams(req, cfg, param, opts)

	resp, err := req.Get(endpointFor(cfg, param))
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", table, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("%s returned status %d: %s", table, resp.StatusCode(), resp.String())
	}

	items, err := responseItems(resp.Body(), cfg.arrayResponse)
	if err != nil {
		return fmt.Errorf("failed to parse %s response: %w", table, err)
	}
	addParamColumn(items, cfg, param)

	if opts.Limit > 0 && len(items) > opts.Limit {
		items = items[:opts.Limit]
	}
	if len(items) == 0 {
		return nil
	}

	config.Debug("[WISTIA] Fetched %d rows from %s", len(items), table)
	return sendBatch(ctx, items, opts, results)
}

func endpointFor(cfg tableConfig, param string) string {
	endpoint := cfg.endpoint
	if param != "" {
		endpoint = strings.ReplaceAll(endpoint, "{id}", url.PathEscape(param))
	}
	return endpoint
}

func applyConfiguredParams(req *ingestrhttp.Request, cfg tableConfig, param string, opts source.ReadOptions) {
	if cfg.queryParam != "" && param != "" {
		req.SetQueryParam(cfg.queryParam, param)
	}
	if cfg.dateFilter {
		applyDateParams(req, cfg, opts)
	}
}

func applyDateParams(req *ingestrhttp.Request, cfg tableConfig, opts source.ReadOptions) {
	start, end := wistiaDateRange(cfg, opts)
	if start != "" {
		req.SetQueryParam("start_date", start)
	}
	if end != "" {
		req.SetQueryParam("end_date", end)
	}
}

func wistiaDateRange(cfg tableConfig, opts source.ReadOptions) (string, string) {
	start := opts.IntervalStart
	end := opts.IntervalEnd

	if start == nil && end == nil {
		if !cfg.defaultDateRange {
			return "", ""
		}
		today := truncateUTCDate(time.Now().UTC())
		yesterday := today.AddDate(0, 0, -1)
		return yesterday.Format("2006-01-02"), today.Format("2006-01-02")
	}

	if start == nil && end != nil {
		endDate := truncateUTCDate(*end)
		startDate := endDate.AddDate(0, 0, -1)
		return startDate.Format("2006-01-02"), endDate.Format("2006-01-02")
	}

	if start != nil && end == nil {
		startDate := truncateUTCDate(*start)
		endDate := truncateUTCDate(time.Now().UTC())
		if endDate.Before(startDate) {
			endDate = startDate
		}
		return startDate.Format("2006-01-02"), endDate.Format("2006-01-02")
	}

	return start.Format("2006-01-02"), end.Format("2006-01-02")
}

func truncateUTCDate(t time.Time) time.Time {
	utc := t.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func pageSizeFromOptions(opts source.ReadOptions) int {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	if opts.Limit > 0 && opts.Limit < pageSize {
		pageSize = opts.Limit
	}
	return pageSize
}

func responseItems(body []byte, expectArray bool) ([]map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var decoded interface{}
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}

	switch value := decoded.(type) {
	case []interface{}:
		return interfaceSliceToItems(value), nil
	case map[string]interface{}:
		if expectArray {
			if data, ok := value["data"].([]interface{}); ok {
				return interfaceSliceToItems(data), nil
			}
			if data, ok := value["items"].([]interface{}); ok {
				return interfaceSliceToItems(data), nil
			}
			if data, ok := value["results"].([]interface{}); ok {
				return interfaceSliceToItems(data), nil
			}
			return nil, fmt.Errorf("expected array response or object envelope with data, items, or results")
		}
		return []map[string]interface{}{value}, nil
	case nil:
		return nil, nil
	default:
		return []map[string]interface{}{{"value": value}}, nil
	}
}

func interfaceSliceToItems(values []interface{}) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(values))
	for _, value := range values {
		if item, ok := value.(map[string]interface{}); ok {
			items = append(items, item)
			continue
		}
		items = append(items, map[string]interface{}{"value": value})
	}
	return items
}

func addParamColumn(items []map[string]interface{}, cfg tableConfig, param string) {
	if cfg.paramColumn == "" || param == "" {
		return
	}
	for _, item := range items {
		if _, ok := item[cfg.paramColumn]; !ok {
			item[cfg.paramColumn] = param
		}
	}
}

func sendBatch(ctx context.Context, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	var cols []schema.Column
	if opts.Schema != nil {
		cols = opts.Schema.Columns
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, cols, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert Wistia response to Arrow: %w", err)
	}

	select {
	case results <- source.RecordBatchResult{Batch: record}:
		return nil
	case <-ctx.Done():
		record.Release()
		return ctx.Err()
	}
}
