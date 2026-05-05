package intercom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	apiVersion = "2.14"
	// Intercom API: 1000 req/min across all endpoints, distributed into 10-second windows (~166 per 10s).
	// Using ~13.3 req/s (~800/min) to stay safely under the limit.
	rateLimit             = 13.3
	rateLimitBurst        = 5
	maxPageSize           = 150
	maxCompanyPerPage     = 50
	defaultStartTimestamp = 1577836800 // 2020-01-01T00:00:00Z
)

var regionBaseURLs = map[string]string{
	"us": "https://api.intercom.io",
	"eu": "https://api.eu.intercom.io",
	"au": "https://api.au.intercom.io",
}

var supportedTables = []string{
	"contacts",
	"companies",
	"conversations",
	"articles",
	"tags",
	"segments",
	"admins",
	"teams",
	"data_attributes",
}

type IntercomSource struct {
	client      *gonghttp.Client
	accessToken string
	region      string
}

func NewIntercomSource() *IntercomSource {
	return &IntercomSource{}
}

func (s *IntercomSource) HandlesIncrementality() bool {
	return true
}

func (s *IntercomSource) Schemes() []string {
	return []string{"intercom"}
}

func (s *IntercomSource) Connect(ctx context.Context, uri string) error {
	accessToken, region, err := parseURI(uri)
	if err != nil {
		return err
	}

	baseURL, ok := regionBaseURLs[region]
	if !ok {
		return fmt.Errorf("unsupported region: %s (supported: us, eu, au)", region)
	}

	s.accessToken = accessToken
	s.region = region

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(accessToken)),
		gonghttp.WithHeaders(map[string]string{
			"Accept":           "application/json",
			"Content-Type":     "application/json",
			"Intercom-Version": apiVersion,
		}),
	)

	config.Debug("[INTERCOM] Connected to region: %s", region)
	return nil
}

func (s *IntercomSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *IntercomSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	incrementalKey := ""
	strategy := config.StrategyReplace
	primaryKeys := []string{"id"}

	switch tableName {
	case "contacts", "companies", "conversations", "articles":
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("intercom source does not have a predefined schema; schema inference is required")
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

func parseURI(uri string) (accessToken, region string, err error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", fmt.Errorf("invalid intercom URI: %w", err)
	}

	if parsed.Scheme != "intercom" {
		return "", "", fmt.Errorf("invalid intercom URI: must start with intercom://")
	}

	q := parsed.Query()
	accessToken = q.Get("access_token")
	if accessToken == "" {
		accessToken = q.Get("oauth_token")
	}
	if accessToken == "" {
		return "", "", fmt.Errorf("access_token or oauth_token is required in intercom URI: intercom://?access_token=<token>")
	}

	region = q.Get("region")
	if region == "" {
		region = "us"
	}

	return accessToken, region, nil
}

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

func (s *IntercomSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "contacts":
			err = s.readContacts(ctx, opts, results)
		case "companies":
			err = s.readCompanies(ctx, opts, results)
		case "conversations":
			err = s.readConversations(ctx, opts, results)
		case "articles":
			err = s.readArticles(ctx, opts, results)
		case "tags":
			err = s.readTags(ctx, opts, results)
		case "segments":
			err = s.readSegments(ctx, opts, results)
		case "admins":
			err = s.readAdmins(ctx, opts, results)
		case "teams":
			err = s.readTeams(ctx, opts, results)
		case "data_attributes":
			err = s.readDataAttributes(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func sendBatch(ctx context.Context, items []map[string]interface{}, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return 0, fmt.Errorf("failed to build arrow record for %s: %w", label, err)
	}

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case results <- source.RecordBatchResult{Batch: record}:
	}

	return len(items), nil
}

// transformContact flattens nested objects in a contact record, matching ingestr behavior.
func transformContact(item map[string]interface{}) {
	if loc, ok := item["location"].(map[string]interface{}); ok {
		item["location_country"] = loc["country"]
		item["location_region"] = loc["region"]
		item["location_city"] = loc["city"]
		delete(item, "location")
	}

	if companies, ok := item["companies"].(map[string]interface{}); ok {
		if data, ok := companies["data"].([]interface{}); ok {
			ids := make([]interface{}, 0, len(data))
			for _, c := range data {
				if cm, ok := c.(map[string]interface{}); ok {
					if id, ok := cm["id"]; ok {
						ids = append(ids, id)
					}
				}
			}
			item["company_ids"] = ids
			item["companies_count"] = len(data)
		} else {
			item["companies_count"] = 0
		}
	}

	if _, ok := item["custom_attributes"]; !ok {
		item["custom_attributes"] = map[string]interface{}{}
	}
}

// transformCompany flattens nested objects in a company record, matching ingestr behavior.
func transformCompany(item map[string]interface{}) {
	if plan, ok := item["plan"].(map[string]interface{}); ok {
		item["plan_id"] = plan["id"]
		item["plan_name"] = plan["name"]
		delete(item, "plan")
	}

	if _, ok := item["custom_attributes"]; !ok {
		item["custom_attributes"] = map[string]interface{}{}
	}
}

// transformConversation flattens nested objects in a conversation record, matching ingestr behavior.
func transformConversation(item map[string]interface{}) {
	if stats, ok := item["statistics"].(map[string]interface{}); ok {
		item["first_contact_reply_at"] = stats["first_contact_reply_at"]
		item["first_admin_reply_at"] = stats["first_admin_reply_at"]
		item["last_contact_reply_at"] = stats["last_contact_reply_at"]
		item["last_admin_reply_at"] = stats["last_admin_reply_at"]
		item["median_admin_reply_time"] = stats["median_admin_reply_time"]
		item["mean_admin_reply_time"] = stats["mean_admin_reply_time"]
		delete(item, "statistics")
	}

	if parts, ok := item["conversation_parts"].(map[string]interface{}); ok {
		item["conversation_parts_count"] = parts["total_count"]
		delete(item, "conversation_parts")
	}
}

// searchAndPaginate handles POST-based search endpoints (contacts, conversations).
// Server-side filtering via the search query on updated_at.
func (s *IntercomSource) searchAndPaginate(ctx context.Context, endpoint, label, dataKey string, transformFn func(map[string]interface{}), opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	totalProcessed := 0
	var cursor string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		body := s.buildSearchBody(opts, cursor)

		resp, err := s.client.R(ctx).
			SetBody(body).
			Post(endpoint)
		if err != nil {
			return fmt.Errorf("failed to search %s: %w", label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("intercom %s search returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		var result map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &result); err != nil {
			return fmt.Errorf("failed to parse %s search response: %w", label, err)
		}

		data, ok := result[dataKey].([]interface{})
		if !ok || len(data) == 0 {
			break
		}

		items := make([]map[string]interface{}, 0, len(data))
		for _, d := range data {
			if item, ok := d.(map[string]interface{}); ok {
				if transformFn != nil {
					transformFn(item)
				}
				items = append(items, item)
			}
		}

		sent, err := sendBatch(ctx, items, label, opts, results)
		if err != nil {
			return err
		}
		totalProcessed += sent

		cursor = extractNextCursor(result)
		if cursor == "" {
			break
		}
	}

	config.Debug("[INTERCOM] finished reading %s: %d total records", label, totalProcessed)
	return nil
}

func (s *IntercomSource) buildSearchBody(opts source.ReadOptions, cursor string) map[string]interface{} {
	body := map[string]interface{}{
		"pagination": map[string]interface{}{
			"per_page": maxPageSize,
		},
	}

	if cursor != "" {
		body["pagination"].(map[string]interface{})["starting_after"] = cursor
	}

	startValue := int64(defaultStartTimestamp)
	if opts.IntervalStart != nil {
		startValue = opts.IntervalStart.Unix()
	}

	var filters []interface{}
	filters = append(filters, map[string]interface{}{
		"field":    "updated_at",
		"operator": ">",
		"value":    startValue,
	})

	if opts.IntervalEnd != nil {
		filters = append(filters, map[string]interface{}{
			"field":    "updated_at",
			"operator": "<",
			"value":    opts.IntervalEnd.Unix(),
		})
	}

	if len(filters) == 1 {
		body["query"] = filters[0]
	} else {
		body["query"] = map[string]interface{}{
			"operator": "AND",
			"value":    filters,
		}
	}

	return body
}

func extractNextCursor(result map[string]interface{}) string {
	pages, ok := result["pages"].(map[string]interface{})
	if !ok {
		return ""
	}
	next, ok := pages["next"].(map[string]interface{})
	if !ok {
		return ""
	}
	cursor, _ := next["starting_after"].(string)
	return cursor
}

// cursorPaginate handles GET-based cursor pagination.
// filterField controls client-side interval filtering: pass "updated_at" for incremental tables, "" for replace tables.
func (s *IntercomSource) cursorPaginate(ctx context.Context, endpoint, label, dataKey, filterField string, perPage int, transformFn func(map[string]interface{}), opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	totalProcessed := 0
	var cursor string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx)
		if perPage > 0 {
			req.SetQueryParam("per_page", fmt.Sprintf("%d", perPage))
		}
		if cursor != "" {
			req.SetQueryParam("starting_after", cursor)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("intercom %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		var result map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		rawData, ok := result[dataKey].([]interface{})
		if !ok || len(rawData) == 0 {
			break
		}

		items := make([]map[string]interface{}, 0, len(rawData))
		for _, d := range rawData {
			item, ok := d.(map[string]interface{})
			if !ok {
				continue
			}
			if transformFn != nil {
				transformFn(item)
			}
			if filterField != "" && !filterByInterval(item, filterField, opts) {
				continue
			}
			items = append(items, item)
		}

		sent, err := sendBatch(ctx, items, label, opts, results)
		if err != nil {
			return err
		}
		totalProcessed += sent

		cursor = extractNextCursor(result)
		if cursor == "" {
			break
		}
	}

	config.Debug("[INTERCOM] finished reading %s: %d total records", label, totalProcessed)
	return nil
}

// filterByInterval applies client-side interval filtering on a Unix timestamp field.
func filterByInterval(item map[string]interface{}, field string, opts source.ReadOptions) bool {
	if opts.IntervalStart == nil && opts.IntervalEnd == nil {
		return true
	}

	ts := getUnixTimestamp(item, field)
	if ts == 0 {
		return true
	}

	t := time.Unix(ts, 0)
	if opts.IntervalStart != nil && !t.After(*opts.IntervalStart) {
		return false
	}
	if opts.IntervalEnd != nil && t.After(*opts.IntervalEnd) {
		return false
	}
	return true
}

func getUnixTimestamp(item map[string]interface{}, field string) int64 {
	val, ok := item[field]
	if !ok {
		return 0
	}

	switch v := val.(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0
		}
		return n
	case float64:
		return int64(v)
	default:
		return 0
	}
}

// simpleGet handles non-paginated GET endpoints (tags, admins, teams).
func (s *IntercomSource) simpleGet(ctx context.Context, endpoint, label, dataKey string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	resp, err := s.client.R(ctx).Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", label, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("intercom %s returned status %d: %s", label, resp.StatusCode(), resp.String())
	}

	var result map[string]interface{}
	if err := jsonUseNumber(resp.Body(), &result); err != nil {
		return fmt.Errorf("failed to parse %s response: %w", label, err)
	}

	rawData, ok := result[dataKey].([]interface{})
	if !ok || len(rawData) == 0 {
		config.Debug("[INTERCOM] %s: no records found", label)
		return nil
	}

	items := make([]map[string]interface{}, 0, len(rawData))
	for _, d := range rawData {
		if item, ok := d.(map[string]interface{}); ok {
			items = append(items, item)
		}
	}

	sent, err := sendBatch(ctx, items, label, opts, results)
	if err != nil {
		return err
	}

	config.Debug("[INTERCOM] finished reading %s: %d total records", label, sent)
	return nil
}

// Search-based tables: contacts, conversations

func (s *IntercomSource) readContacts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INTERCOM] reading contacts")
	return s.searchAndPaginate(ctx, "/contacts/search", "contacts", "data", transformContact, opts, results)
}

func (s *IntercomSource) readConversations(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INTERCOM] reading conversations")
	return s.searchAndPaginate(ctx, "/conversations/search", "conversations", "conversations", transformConversation, opts, results)
}

// Cursor-paginated tables: companies, articles

func (s *IntercomSource) readCompanies(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INTERCOM] reading companies")
	return s.cursorPaginate(ctx, "/companies", "companies", "data", "updated_at", maxCompanyPerPage, transformCompany, opts, results)
}

func (s *IntercomSource) readArticles(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INTERCOM] reading articles")
	return s.cursorPaginate(ctx, "/articles", "articles", "data", "updated_at", 0, nil, opts, results)
}

// Simple replace tables: tags, admins, teams

func (s *IntercomSource) readTags(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INTERCOM] reading tags")
	return s.simpleGet(ctx, "/tags", "tags", "data", opts, results)
}

func (s *IntercomSource) readAdmins(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INTERCOM] reading admins")
	return s.simpleGet(ctx, "/admins", "admins", "admins", opts, results)
}

func (s *IntercomSource) readTeams(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INTERCOM] reading teams")
	return s.simpleGet(ctx, "/teams", "teams", "teams", opts, results)
}

// Cursor-paginated replace tables: segments, data_attributes

func (s *IntercomSource) readSegments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INTERCOM] reading segments")
	return s.cursorPaginate(ctx, "/segments", "segments", "segments", "", 0, nil, opts, results)
}

func (s *IntercomSource) readDataAttributes(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INTERCOM] reading data_attributes")
	return s.cursorPaginate(ctx, "/data_attributes", "data_attributes", "data", "", 0, nil, opts, results)
}
