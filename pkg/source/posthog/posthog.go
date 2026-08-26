package posthog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	defaultBaseURL       = "https://us.posthog.com"
	defaultPageSize      = 100
	eventsQueryPageSize  = 1000
	hogQLTimestampLayout = "2006-01-02 15:04:05.000000"
)

type PostHogSource struct {
	baseURL   string
	projectID string
	client    *httpclient.Client
}

type posthogCredentials struct {
	baseURL        string
	projectID      string
	personalAPIKey string
}

type tableConfig struct {
	endpoint           string
	primaryKeys        []string
	incrementalKey     string
	strategy           config.IncrementalStrategy
	intervalFields     []string
	defaultQueryParams map[string]string
	useQueryAPI        bool
}

type paginatedResponse struct {
	Count    int                      `json:"count"`
	Next     string                   `json:"next"`
	Previous string                   `json:"previous"`
	Results  []map[string]interface{} `json:"results"`
}

type hogQLResponse struct {
	Columns []string        `json:"columns"`
	Results [][]interface{} `json:"results"`
	Error   string          `json:"error"`
}

type eventCursor struct {
	timestamp time.Time
	uuid      string
}

var baseTables = map[string]tableConfig{
	"annotations": {
		endpoint:       "annotations",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"updated_at", "date_marker", "created_at"},
	},
	"cohorts": {
		endpoint:       "cohorts",
		primaryKeys:    []string{"id"},
		incrementalKey: "last_calculation",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"last_calculation", "created_at"},
	},
	"event_definitions": {
		endpoint:       "event_definitions",
		primaryKeys:    []string{"id"},
		incrementalKey: "last_updated_at",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"last_updated_at", "last_seen_at", "created_at"},
	},
	"events": {
		endpoint:       "events",
		primaryKeys:    []string{"id"},
		incrementalKey: "timestamp",
		strategy:       config.StrategyAppend,
		intervalFields: []string{"timestamp"},
		useQueryAPI:    true,
	},
	"feature_flags": {
		endpoint:       "feature_flags",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"updated_at", "created_at", "last_called_at"},
	},
	"persons": {
		endpoint:       "persons",
		primaryKeys:    []string{"id"},
		incrementalKey: "last_seen_at",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"last_seen_at", "created_at"},
	},
}

var propertyDefinitionTypes = map[string]string{
	"event":   "event",
	"person":  "person",
	"session": "session",
}

func NewPostHogSource() *PostHogSource {
	return &PostHogSource{}
}

func (s *PostHogSource) Schemes() []string {
	return []string{"posthog"}
}

func (s *PostHogSource) Connect(ctx context.Context, uri string) error {
	creds, err := parsePostHogURI(uri)
	if err != nil {
		return err
	}

	s.baseURL = creds.baseURL
	s.projectID = creds.projectID
	s.client = httpclient.New(
		httpclient.WithBaseURL(s.baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(creds.personalAPIKey)),
	)

	config.Debug("[POSTHOG] Connected to %s for project %s", s.baseURL, s.projectID)
	return nil
}

func (s *PostHogSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *PostHogSource) HandlesIncrementality() bool {
	return true
}

func (s *PostHogSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	cfg, err := resolveTableConfig(tableName)
	if err != nil {
		return nil, err
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    cfg.primaryKeys,
		TableIncrementalKey: cfg.incrementalKey,
		TableStrategy:       cfg.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("posthog source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readTable(ctx, cfg, opts)
		},
	}, nil
}

func parsePostHogURI(uri string) (posthogCredentials, error) {
	if !strings.HasPrefix(uri, "posthog://") {
		return posthogCredentials{}, fmt.Errorf("invalid posthog URI: must start with posthog://")
	}

	rest := strings.TrimPrefix(uri, "posthog://")
	if rest == "" || rest == "?" {
		return posthogCredentials{}, fmt.Errorf("personal_api_key and project_id are required in posthog URI")
	}

	rest = strings.TrimPrefix(rest, "?")
	values, err := url.ParseQuery(rest)
	if err != nil {
		return posthogCredentials{}, fmt.Errorf("failed to parse posthog URI query: %w", err)
	}

	apiKey := values.Get("personal_api_key")
	if apiKey == "" {
		apiKey = values.Get("api_key")
	}
	if apiKey == "" {
		return posthogCredentials{}, fmt.Errorf("personal_api_key is required in posthog URI")
	}

	projectID := values.Get("project_id")
	if projectID == "" {
		return posthogCredentials{}, fmt.Errorf("project_id is required in posthog URI")
	}

	baseURL := values.Get("base_url")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return posthogCredentials{}, fmt.Errorf("invalid base_url in posthog URI")
	}

	return posthogCredentials{
		baseURL:        baseURL,
		projectID:      projectID,
		personalAPIKey: apiKey,
	}, nil
}

func resolveTableConfig(tableName string) (tableConfig, error) {
	if cfg, ok := baseTables[tableName]; ok {
		return cfg, nil
	}

	baseName, variant, hasVariant := strings.Cut(tableName, ":")
	if !hasVariant {
		return tableConfig{}, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, supportedTableList())
	}

	if baseName != "property_definitions" {
		return tableConfig{}, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, supportedTableList())
	}

	propertyType, ok := propertyDefinitionTypes[variant]
	if !ok {
		return tableConfig{}, fmt.Errorf("unsupported property_definitions variant: %s (supported: %s)", variant, supportedPropertyDefinitionList())
	}

	return tableConfig{
		endpoint:       "property_definitions",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"updated_at"},
		defaultQueryParams: map[string]string{
			"type": propertyType,
		},
	}, nil
}

func supportedTableList() string {
	names := make([]string, 0, len(baseTables)+len(propertyDefinitionTypes))
	for name := range baseTables {
		names = append(names, name)
	}
	for variant := range propertyDefinitionTypes {
		names = append(names, "property_definitions:"+variant)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func supportedPropertyDefinitionList() string {
	names := make([]string, 0, len(propertyDefinitionTypes))
	for name := range propertyDefinitionTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (s *PostHogSource) readTable(ctx context.Context, cfg tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		if cfg.useQueryAPI {
			err = s.queryEventsAndSend(ctx, opts, results)
		} else {
			err = s.paginateAndSend(ctx, cfg, opts, results)
		}
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *PostHogSource) paginateAndSend(ctx context.Context, cfg tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if s.client == nil {
		return fmt.Errorf("posthog source is not connected")
	}

	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	initialURL := fmt.Sprintf("/api/projects/%s/%s/", url.PathEscape(s.projectID), cfg.endpoint)
	params := cloneStringMap(cfg.defaultQueryParams)
	params["limit"] = strconv.Itoa(pageSize)

	nextURL := initialURL
	firstRequest := true
	remaining := opts.Limit

	for nextURL != "" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx)
		if firstRequest {
			for key, value := range params {
				req.SetQueryParam(key, value)
			}
		}

		resp, err := req.Get(nextURL)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", cfg.endpoint, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("posthog API %s returned status %d: %s", cfg.endpoint, resp.StatusCode(), resp.String())
		}

		page, err := decodePaginatedResponse(resp.Body())
		if err != nil {
			return fmt.Errorf("failed to parse %s response: %w", cfg.endpoint, err)
		}

		items := filterItemsByInterval(page.Results, cfg.intervalFields, opts.IntervalStart, opts.IntervalEnd)
		if remaining > 0 && len(items) > remaining {
			items = items[:remaining]
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", cfg.endpoint, err)
			}
			results <- source.RecordBatchResult{Batch: record}

			if remaining > 0 {
				remaining -= len(items)
				if remaining == 0 {
					return nil
				}
			}
		}

		nextURL = page.Next
		firstRequest = false
	}

	return nil
}

// queryEventsAndSend reads the events table through PostHog's /query/ (HogQL)
// endpoint. The REST /events/ endpoint silently truncates multi-day windows,
// so we page through events with keyset pagination on (timestamp, uuid).
func (s *PostHogSource) queryEventsAndSend(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if s.client == nil {
		return fmt.Errorf("posthog source is not connected")
	}

	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = eventsQueryPageSize
	}

	queryURL := fmt.Sprintf("/api/projects/%s/query/", url.PathEscape(s.projectID))
	remaining := opts.Limit
	var cursor *eventCursor

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		limit := pageSize
		if remaining > 0 && remaining < limit {
			limit = remaining
		}

		page, err := s.executeHogQLQuery(ctx, queryURL, buildEventsQuery(opts.IntervalStart, opts.IntervalEnd, cursor, limit))
		if err != nil {
			return err
		}

		items, last := hogQLRowsToItems(page)
		if len(items) == 0 {
			return nil
		}

		foldEventPersonFields(normalizeEventItems(items))
		addElementsFromChain(items)
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert events to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}

		if remaining > 0 {
			remaining -= len(items)
			if remaining <= 0 {
				return nil
			}
		}

		if len(items) < limit || last == nil {
			return nil
		}
		cursor = last
	}
}

func (s *PostHogSource) executeHogQLQuery(ctx context.Context, queryURL, query string) (hogQLResponse, error) {
	body := map[string]interface{}{
		"query": map[string]string{
			"kind":  "HogQLQuery",
			"query": query,
		},
	}

	resp, err := s.client.R(ctx).SetHeader("Content-Type", "application/json").SetBody(body).Post(queryURL)
	if err != nil {
		return hogQLResponse{}, fmt.Errorf("failed to query events: %w", err)
	}
	if !resp.IsSuccess() {
		return hogQLResponse{}, fmt.Errorf("posthog query API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	page, err := decodeHogQLResponse(resp.Body())
	if err != nil {
		return hogQLResponse{}, fmt.Errorf("failed to parse events query response: %w", err)
	}
	if page.Error != "" {
		return hogQLResponse{}, fmt.Errorf("posthog query API error: %s", page.Error)
	}
	return page, nil
}

func buildEventsQuery(start, end *time.Time, cursor *eventCursor, limit int) string {
	conditions := make([]string, 0, 2)
	switch {
	case cursor != nil:
		conditions = append(conditions, fmt.Sprintf(
			"(timestamp, uuid) > (toDateTime64('%s', 6), toUUID('%s'))",
			cursor.timestamp.UTC().Format(hogQLTimestampLayout), cursor.uuid))
	case start != nil:
		conditions = append(conditions, fmt.Sprintf(
			"timestamp > toDateTime64('%s', 6)", start.UTC().Format(hogQLTimestampLayout)))
	}
	if end != nil {
		conditions = append(conditions, fmt.Sprintf(
			"timestamp < toDateTime64('%s', 6)", end.UTC().Format(hogQLTimestampLayout)))
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	return fmt.Sprintf(
		"SELECT uuid AS id, event, distinct_id, timestamp, properties, elements_chain, person_id, person.properties AS person_properties FROM events%s ORDER BY timestamp, uuid LIMIT %d",
		where, limit)
}

func hogQLRowsToItems(page hogQLResponse) ([]map[string]interface{}, *eventCursor) {
	items := make([]map[string]interface{}, 0, len(page.Results))
	var cursor *eventCursor
	for _, row := range page.Results {
		item := make(map[string]interface{}, len(page.Columns))
		for i, col := range page.Columns {
			if i < len(row) {
				item[col] = row[i]
			}
		}
		items = append(items, item)

		if ts, ok := firstTimestamp(item, []string{"timestamp"}); ok {
			if id, ok := item["id"].(string); ok && id != "" {
				cursor = &eventCursor{timestamp: ts, uuid: id}
			}
		}
	}
	return items, cursor
}

func decodeHogQLResponse(body []byte) (hogQLResponse, error) {
	var response hogQLResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil {
		return hogQLResponse{}, err
	}
	return response, nil
}

func decodePaginatedResponse(body []byte) (paginatedResponse, error) {
	var response paginatedResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil {
		return paginatedResponse{}, err
	}
	return response, nil
}

// foldEventPersonFields collapses the flat person_id/person_properties columns
// returned by HogQL into a single nested person object, matching the shape the
// REST /events/ endpoint used to return.
func foldEventPersonFields(items []map[string]interface{}) {
	for _, item := range items {
		person := make(map[string]interface{}, 2)
		if id, ok := item["person_id"]; ok {
			person["id"] = id
		}
		if props, ok := item["person_properties"]; ok {
			person["properties"] = props
		}
		delete(item, "person_id")
		delete(item, "person_properties")
		if len(person) > 0 {
			item["person"] = person
		}
	}
}

func normalizeEventItems(items []map[string]interface{}) []map[string]interface{} {
	for _, item := range items {
		for _, field := range []string{"properties", "person", "person_properties", "elements", "elements_chain"} {
			raw, ok := item[field]
			if !ok {
				continue
			}
			text, ok := raw.(string)
			if !ok || text == "" {
				continue
			}
			var decoded interface{}
			decoder := json.NewDecoder(strings.NewReader(text))
			decoder.UseNumber()
			if err := decoder.Decode(&decoded); err == nil {
				item[field] = decoded
			}
		}
	}
	return items
}

func filterItemsByInterval(items []map[string]interface{}, fields []string, start, end *time.Time) []map[string]interface{} {
	if len(fields) == 0 || (start == nil && end == nil) {
		return items
	}

	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		ts, ok := firstTimestamp(item, fields)
		if ok && start != nil && ts.Before(start.UTC()) {
			continue
		}
		if ok && end != nil && ts.After(end.UTC()) {
			continue
		}
		filtered = append(filtered, item)
	}

	return filtered
}

func firstTimestamp(item map[string]interface{}, fields []string) (time.Time, bool) {
	for _, field := range fields {
		raw, ok := item[field]
		if !ok || raw == nil {
			continue
		}

		switch value := raw.(type) {
		case time.Time:
			return value.UTC(), true
		case string:
			ts, err := dateparse.ParseAny(value)
			if err == nil {
				return ts.UTC(), true
			}
		}
	}

	return time.Time{}, false
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}

	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
