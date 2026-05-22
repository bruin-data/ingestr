package mailchimp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/araddon/dateparse"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	maxPageSize = 1000

	// Mailchimp allows 10 simultaneous connections with 120s timeout.
	// We use ~8 rps as a safe limit (80% of 10 connections).
	rateLimit      = 8.0
	rateLimitBurst = 5
)

type tableConfig struct {
	endpoint       string
	responseKey    string
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
	intervalFields []string
}

var mergeTableConfigs = map[string]tableConfig{
	"audiences": {
		endpoint:       "lists",
		responseKey:    "lists",
		primaryKeys:    []string{"id"},
		incrementalKey: "date_created",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"date_created"},
	},
	"automations": {
		endpoint:       "automations",
		responseKey:    "automations",
		primaryKeys:    []string{"id"},
		incrementalKey: "create_time",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"create_time"},
	},
	"campaigns": {
		endpoint:       "campaigns",
		responseKey:    "campaigns",
		primaryKeys:    []string{"id"},
		incrementalKey: "create_time",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"create_time"},
	},
	"connected_sites": {
		endpoint:       "connected-sites",
		responseKey:    "sites",
		primaryKeys:    []string{"foreign_id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"updated_at"},
	},
	"conversations": {
		endpoint:       "conversations",
		responseKey:    "conversations",
		primaryKeys:    []string{"id"},
		incrementalKey: "last_message.timestamp",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"last_message.timestamp"},
	},
	"ecommerce_stores": {
		endpoint:       "ecommerce/stores",
		responseKey:    "stores",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"updated_at"},
	},
	"facebook_ads": {
		endpoint:       "facebook-ads",
		responseKey:    "facebook_ads",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"updated_at"},
	},
	"landing_pages": {
		endpoint:       "landing-pages",
		responseKey:    "landing_pages",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"updated_at"},
	},
	"reports": {
		endpoint:       "reports",
		responseKey:    "reports",
		primaryKeys:    []string{"id"},
		incrementalKey: "send_time",
		strategy:       config.StrategyMerge,
		intervalFields: []string{"send_time"},
	},
}

var replaceTableConfigs = map[string]tableConfig{
	"account_exports": {
		endpoint:    "account-exports",
		responseKey: "exports",
		primaryKeys: nil,
		strategy:    config.StrategyReplace,
	},
	"authorized_apps": {
		endpoint:    "authorized-apps",
		responseKey: "apps",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"batches": {
		endpoint:    "batches",
		responseKey: "batches",
		primaryKeys: nil,
		strategy:    config.StrategyReplace,
	},
	"campaign_folders": {
		endpoint:    "campaign-folders",
		responseKey: "folders",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"chimp_chatter": {
		endpoint:    "activity-feed/chimp-chatter",
		responseKey: "chimp_chatter",
		primaryKeys: nil,
		strategy:    config.StrategyReplace,
	},
}

type nestedTableConfig struct {
	parentEndpoint    string
	parentResponseKey string
	childEndpoint     string
	childResponseKey  string
	parentIDField     string
	primaryKeys       []string
	strategy          config.IncrementalStrategy
}

var nestedTableConfigs = map[string]nestedTableConfig{
	"reports_advice": {
		parentEndpoint:    "reports",
		parentResponseKey: "reports",
		childEndpoint:     "advice",
		childResponseKey:  "",
		parentIDField:     "reports_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"reports_domain_performance": {
		parentEndpoint:    "reports",
		parentResponseKey: "reports",
		childEndpoint:     "domain-performance",
		childResponseKey:  "domains",
		parentIDField:     "reports_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"reports_locations": {
		parentEndpoint:    "reports",
		parentResponseKey: "reports",
		childEndpoint:     "locations",
		childResponseKey:  "locations",
		parentIDField:     "reports_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"reports_sent_to": {
		parentEndpoint:    "reports",
		parentResponseKey: "reports",
		childEndpoint:     "sent-to",
		childResponseKey:  "sent_to",
		parentIDField:     "reports_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"reports_sub_reports": {
		parentEndpoint:    "reports",
		parentResponseKey: "reports",
		childEndpoint:     "sub-reports",
		childResponseKey:  "",
		parentIDField:     "reports_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"reports_unsubscribed": {
		parentEndpoint:    "reports",
		parentResponseKey: "reports",
		childEndpoint:     "unsubscribed",
		childResponseKey:  "unsubscribes",
		parentIDField:     "reports_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"lists_activity": {
		parentEndpoint:    "lists",
		parentResponseKey: "lists",
		childEndpoint:     "activity",
		childResponseKey:  "activity",
		parentIDField:     "audiences_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"lists_clients": {
		parentEndpoint:    "lists",
		parentResponseKey: "lists",
		childEndpoint:     "clients",
		childResponseKey:  "clients",
		parentIDField:     "audiences_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"lists_growth_history": {
		parentEndpoint:    "lists",
		parentResponseKey: "lists",
		childEndpoint:     "growth-history",
		childResponseKey:  "history",
		parentIDField:     "audiences_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"lists_interest_categories": {
		parentEndpoint:    "lists",
		parentResponseKey: "lists",
		childEndpoint:     "interest-categories",
		childResponseKey:  "categories",
		parentIDField:     "audiences_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"lists_locations": {
		parentEndpoint:    "lists",
		parentResponseKey: "lists",
		childEndpoint:     "locations",
		childResponseKey:  "locations",
		parentIDField:     "audiences_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"lists_merge_fields": {
		parentEndpoint:    "lists",
		parentResponseKey: "lists",
		childEndpoint:     "merge-fields",
		childResponseKey:  "merge_fields",
		parentIDField:     "audiences_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
	"lists_segments": {
		parentEndpoint:    "lists",
		parentResponseKey: "lists",
		childEndpoint:     "segments",
		childResponseKey:  "segments",
		parentIDField:     "audiences_id",
		primaryKeys:       nil,
		strategy:          config.StrategyReplace,
	},
}

var supportedTables []string

func init() {
	supportedTables = append(supportedTables, "account")
	for name := range mergeTableConfigs {
		supportedTables = append(supportedTables, name)
	}
	for name := range replaceTableConfigs {
		supportedTables = append(supportedTables, name)
	}
	for name := range nestedTableConfigs {
		supportedTables = append(supportedTables, name)
	}
}

type mailchimpCredentials struct {
	apiKey string
	server string
}

type MailchimpSource struct {
	client *httpclient.Client
	apiKey string
	server string
}

func NewMailchimpSource() *MailchimpSource {
	return &MailchimpSource{}
}

func (s *MailchimpSource) Schemes() []string {
	return []string{"mailchimp"}
}

func (s *MailchimpSource) HandlesIncrementality() bool {
	return true
}

func (s *MailchimpSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = creds.apiKey
	s.server = creds.server

	baseURL := fmt.Sprintf("https://%s.api.mailchimp.com/3.0", s.server)

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(120*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBasicAuth("anystring", s.apiKey)),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
	)

	return nil
}

func (s *MailchimpSource) Close(ctx context.Context) error {
	return nil
}

func parseURI(uri string) (mailchimpCredentials, error) {
	if !strings.HasPrefix(uri, "mailchimp://") {
		return mailchimpCredentials{}, fmt.Errorf("invalid mailchimp URI: must start with mailchimp://")
	}

	rest := strings.TrimPrefix(uri, "mailchimp://")
	if rest == "" || rest == "?" {
		return mailchimpCredentials{}, fmt.Errorf("api_key is required in mailchimp URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return mailchimpCredentials{}, fmt.Errorf("failed to parse mailchimp URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return mailchimpCredentials{}, fmt.Errorf("api_key is required in mailchimp URI")
	}

	server := values.Get("server")
	if server == "" {
		parts := strings.Split(apiKey, "-")
		if len(parts) == 2 && parts[1] != "" {
			server = parts[1]
		} else {
			return mailchimpCredentials{}, fmt.Errorf("server is required in mailchimp URI (either as ?server= or as suffix of api_key like key-us16)")
		}
	}

	return mailchimpCredentials{
		apiKey: apiKey,
		server: server,
	}, nil
}

func (s *MailchimpSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	var primaryKeys []string
	var incrementalKey string
	strategy := config.StrategyReplace

	if tableName == "account" {
		primaryKeys = []string{"account_id"}
		strategy = config.StrategyReplace
	} else if cfg, ok := mergeTableConfigs[tableName]; ok {
		primaryKeys = cfg.primaryKeys
		incrementalKey = cfg.incrementalKey
		strategy = cfg.strategy
	} else if cfg, ok := replaceTableConfigs[tableName]; ok {
		primaryKeys = cfg.primaryKeys
		strategy = cfg.strategy
	} else if cfg, ok := nestedTableConfigs[tableName]; ok {
		primaryKeys = cfg.primaryKeys
		strategy = cfg.strategy
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("mailchimp source does not have a predefined schema; schema inference is required")
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

func (s *MailchimpSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch {
		case table == "account":
			err = s.readAccount(ctx, opts, results)
		case mergeTableConfigs[table].endpoint != "":
			err = s.readMergeTable(ctx, table, opts, results)
		case replaceTableConfigs[table].endpoint != "":
			err = s.readReplaceTable(ctx, table, opts, results)
		default:
			if _, ok := nestedTableConfigs[table]; ok {
				err = s.readNestedTable(ctx, table, opts, results)
			} else {
				err = fmt.Errorf("unsupported table: %s", table)
			}
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *MailchimpSource) readAccount(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[Mailchimp] reading account")

	resp, err := s.client.R(ctx).Get("/")
	if err != nil {
		return fmt.Errorf("failed to fetch account: %w", err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("mailchimp API / returned status %d: %s", resp.StatusCode(), resp.String())
	}

	item, err := decodeItem(resp.Body())
	if err != nil {
		return fmt.Errorf("failed to parse account response: %w", err)
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{item}, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert account to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[Mailchimp] sent account record")
	return nil
}

func (s *MailchimpSource) readMergeTable(ctx context.Context, table string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := mergeTableConfigs[table]
	config.Debug("[Mailchimp] reading %s", table)
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.responseKey, cfg.intervalFields, opts, results)
}

func (s *MailchimpSource) readReplaceTable(ctx context.Context, table string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := replaceTableConfigs[table]
	config.Debug("[Mailchimp] reading %s", table)
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.responseKey, nil, opts, results)
}

func (s *MailchimpSource) paginateAndSend(ctx context.Context, endpoint, responseKey string, intervalFields []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	offset := 0
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("count", strconv.Itoa(maxPageSize)).
			SetQueryParam("offset", strconv.Itoa(offset)).
			Get("/" + endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("mailchimp API /%s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		items, err := decodeItems(resp.Body(), responseKey)
		if err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		if len(items) == 0 {
			break
		}

		rawCount := len(items)
		items = filterItemsByInterval(items, intervalFields, opts.IntervalStart, opts.IntervalEnd)

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", endpoint, err)
			}

			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
			config.Debug("[Mailchimp] %s offset %d: sent %d records (total: %d)", endpoint, offset, len(items), totalSent)
		}

		if rawCount < maxPageSize {
			break
		}

		offset += maxPageSize
	}

	if totalSent == 0 {
		config.Debug("[Mailchimp] no records found for %s", endpoint)
	}

	return nil
}

func (s *MailchimpSource) readNestedTable(ctx context.Context, table string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := nestedTableConfigs[table]
	config.Debug("[Mailchimp] reading %s", table)

	parentIDs, err := s.fetchParentIDs(ctx, cfg.parentEndpoint, cfg.parentResponseKey)
	if err != nil {
		return err
	}

	if len(parentIDs) == 0 {
		config.Debug("[Mailchimp] no parent records found for %s", table)
		return nil
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	sem := make(chan struct{}, 5)

	for _, parentID := range parentIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(pid string) {
			defer wg.Done()
			defer func() { <-sem }()

			err := s.fetchNestedItems(ctx, cfg, pid, opts, results)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(parentID)
	}

	wg.Wait()
	return firstErr
}

func (s *MailchimpSource) fetchParentIDs(ctx context.Context, endpoint, responseKey string) ([]string, error) {
	var ids []string
	offset := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("count", strconv.Itoa(maxPageSize)).
			SetQueryParam("offset", strconv.Itoa(offset)).
			Get("/" + endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch parent %s: %w", endpoint, err)
		}

		if !resp.IsSuccess() {
			return nil, fmt.Errorf("mailchimp API /%s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		items, err := decodeItems(resp.Body(), responseKey)
		if err != nil {
			return nil, fmt.Errorf("failed to parse parent %s response: %w", endpoint, err)
		}

		if len(items) == 0 {
			break
		}

		for _, item := range items {
			if id, ok := item["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}

		if len(items) < maxPageSize {
			break
		}

		offset += maxPageSize
	}

	return ids, nil
}

func (s *MailchimpSource) fetchNestedItems(ctx context.Context, cfg nestedTableConfig, parentID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/%s/%s/%s", cfg.parentEndpoint, parentID, cfg.childEndpoint)

	// When childResponseKey is empty, the endpoint returns a single object (not a list).
	// Yield the whole response as one record, matching ingestr's nested_key=None behavior.
	if cfg.childResponseKey == "" {
		resp, err := s.client.R(ctx).Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s for parent %s: %w", cfg.childEndpoint, parentID, err)
		}

		if !resp.IsSuccess() {
			if resp.StatusCode() == 404 {
				config.Debug("[Mailchimp] %s for parent %s not found, skipping", cfg.childEndpoint, parentID)
				return nil
			}
			return fmt.Errorf("mailchimp API %s for parent %s returned status %d: %s", cfg.childEndpoint, parentID, resp.StatusCode(), resp.String())
		}

		item, err := decodeItem(resp.Body())
		if err != nil {
			return fmt.Errorf("failed to parse %s response for parent %s: %w", cfg.childEndpoint, parentID, err)
		}

		item[cfg.parentIDField] = parentID

		record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{item}, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", cfg.childEndpoint, err)
		}

		results <- source.RecordBatchResult{Batch: record}
		config.Debug("[Mailchimp] %s for parent %s: sent 1 record (whole response)", cfg.childEndpoint, parentID)
		return nil
	}

	offset := 0
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("count", strconv.Itoa(maxPageSize)).
			SetQueryParam("offset", strconv.Itoa(offset)).
			Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s for parent %s: %w", cfg.childEndpoint, parentID, err)
		}

		if !resp.IsSuccess() {
			if resp.StatusCode() == 404 {
				config.Debug("[Mailchimp] %s for parent %s not found, skipping", cfg.childEndpoint, parentID)
				return nil
			}
			return fmt.Errorf("mailchimp API %s for parent %s returned status %d: %s", cfg.childEndpoint, parentID, resp.StatusCode(), resp.String())
		}

		items, err := decodeItems(resp.Body(), cfg.childResponseKey)
		if err != nil {
			return fmt.Errorf("failed to parse %s response for parent %s: %w", cfg.childEndpoint, parentID, err)
		}

		if len(items) == 0 {
			break
		}

		for i := range items {
			items[i][cfg.parentIDField] = parentID
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", cfg.childEndpoint, err)
			}

			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
		}

		if len(items) < maxPageSize {
			break
		}

		offset += maxPageSize
	}

	config.Debug("[Mailchimp] %s for parent %s: sent %d records", cfg.childEndpoint, parentID, totalSent)
	return nil
}

func decodeItems(body []byte, key string) ([]map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var raw map[string]interface{}
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}

	itemsRaw, ok := raw[key]
	if !ok {
		return nil, nil
	}

	arr, ok := itemsRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected array for key %q, got %T", key, itemsRaw)
	}

	items := make([]map[string]interface{}, 0, len(arr))
	for _, v := range arr {
		if m, ok := v.(map[string]interface{}); ok {
			items = append(items, m)
		}
	}

	return items, nil
}

func decodeItem(body []byte) (map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var item map[string]interface{}
	if err := decoder.Decode(&item); err != nil {
		return nil, err
	}

	return item, nil
}

func filterItemsByInterval(items []map[string]interface{}, fields []string, start, end *time.Time) []map[string]interface{} {
	if len(fields) == 0 || (start == nil && end == nil) {
		return items
	}

	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		ts, ok := firstTimestamp(item, fields)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if start != nil && ts.Before(start.UTC()) {
			continue
		}
		if end != nil && !ts.Before(end.UTC()) {
			continue
		}
		filtered = append(filtered, item)
	}

	return filtered
}

func firstTimestamp(item map[string]interface{}, fields []string) (time.Time, bool) {
	for _, field := range fields {
		var raw interface{}
		if strings.Contains(field, ".") {
			raw = getNestedField(item, field)
		} else {
			raw = item[field]
		}

		if raw == nil {
			continue
		}

		switch v := raw.(type) {
		case string:
			if v == "" {
				continue
			}
			ts, err := dateparse.ParseAny(v)
			if err == nil {
				return ts.UTC(), true
			}
		case time.Time:
			return v.UTC(), true
		}
	}

	return time.Time{}, false
}

func getNestedField(item map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	current := interface{}(item)

	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m[part]
	}

	return current
}
