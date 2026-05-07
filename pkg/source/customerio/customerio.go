package customerio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	defaultBaseURL = "https://api.customer.io" // US
	euBaseURL      = "https://api-eu.customer.io"
	// Customer.io API: 10 req/s. Using 8 req/s (80%) to stay safely under.
	rateLimit          = 8.0
	rateLimitBurst     = 5
	activitiesPageSize = 100
	messagesPageSize   = 1000
	membersPageSize    = 1000
	objectsPageSize    = 1000
	defaultParallelism = 5
)

type filterType int

const (
	filterNone filterType = iota
	filterClientSideUnixTS
	filterServerSideStartEnd
)

type tableConfig struct {
	endpoint       string
	dataKey        string
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
	filterType     filterType
	defaultParams  map[string]string
	isNonPaginated bool
	parentTable    string
}

var tables = map[string]tableConfig{
	"activities": {
		endpoint:    "/v1/activities",
		dataKey:     "activities",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		filterType:  filterNone,
		defaultParams: map[string]string{
			"limit": strconv.Itoa(activitiesPageSize),
		},
	},
	"broadcasts": {
		endpoint:       "/v1/broadcasts",
		dataKey:        "broadcasts",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated",
		strategy:       config.StrategyMerge,
		filterType:     filterClientSideUnixTS,
	},
	"campaigns": {
		endpoint:       "/v1/campaigns",
		dataKey:        "campaigns",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated",
		strategy:       config.StrategyMerge,
		filterType:     filterClientSideUnixTS,
	},
	"collections": {
		endpoint:       "/v1/collections",
		dataKey:        "collections",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		filterType:     filterClientSideUnixTS,
	},
	"exports": {
		endpoint:       "/v1/exports",
		dataKey:        "exports",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		filterType:     filterClientSideUnixTS,
	},
	"info_ip_addresses": {
		endpoint:       "/v1/info/ip_addresses",
		dataKey:        "ips",
		primaryKeys:    []string{"ip"},
		strategy:       config.StrategyReplace,
		filterType:     filterNone,
		isNonPaginated: true,
	},
	"messages": {
		endpoint:    "/v1/messages",
		dataKey:     "messages",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyMerge,
		filterType:  filterServerSideStartEnd,
		defaultParams: map[string]string{
			"limit": strconv.Itoa(messagesPageSize),
		},
	},
	"newsletters": {
		endpoint:       "/v1/newsletters",
		dataKey:        "newsletters",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated",
		strategy:       config.StrategyMerge,
		filterType:     filterClientSideUnixTS,
		defaultParams: map[string]string{
			"limit": strconv.Itoa(activitiesPageSize),
		},
	},
	"reporting_webhooks": {
		endpoint:       "/v1/reporting_webhooks",
		dataKey:        "reporting_webhooks",
		primaryKeys:    []string{"id"},
		strategy:       config.StrategyReplace,
		filterType:     filterNone,
		isNonPaginated: true,
	},
	"segments": {
		endpoint:       "/v1/segments",
		dataKey:        "segments",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated_at",
		strategy:       config.StrategyMerge,
		filterType:     filterClientSideUnixTS,
	},
	"sender_identities": {
		endpoint:    "/v1/sender_identities",
		dataKey:     "sender_identities",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		filterType:  filterNone,
	},
	"subscription_topics": {
		endpoint:       "/v1/subscription_topics",
		dataKey:        "topics",
		primaryKeys:    []string{"id"},
		strategy:       config.StrategyReplace,
		filterType:     filterNone,
		isNonPaginated: true,
	},
	"transactional_messages": {
		endpoint:    "/v1/transactional",
		dataKey:     "transactional",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		filterType:  filterNone,
	},
	"workspaces": {
		endpoint:       "/v1/workspaces",
		dataKey:        "workspaces",
		primaryKeys:    []string{"id"},
		strategy:       config.StrategyReplace,
		filterType:     filterNone,
		isNonPaginated: true,
	},
	"object_types": {
		endpoint:       "/v1/object_types",
		dataKey:        "types",
		primaryKeys:    []string{"id"},
		strategy:       config.StrategyReplace,
		filterType:     filterNone,
		isNonPaginated: true,
	},
	"broadcast_actions": {
		endpoint:       "/v1/broadcasts/{id}/actions",
		dataKey:        "actions",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated",
		strategy:       config.StrategyMerge,
		filterType:     filterClientSideUnixTS,
		parentTable:    "broadcasts",
	},
	"broadcast_messages": {
		endpoint:    "/v1/broadcasts/{id}/messages",
		dataKey:     "messages",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyMerge,
		filterType:  filterServerSideStartEnd,
		parentTable: "broadcasts",
	},
	"campaign_actions": {
		endpoint:       "/v1/campaigns/{id}/actions",
		dataKey:        "actions",
		primaryKeys:    []string{"id"},
		incrementalKey: "updated",
		strategy:       config.StrategyMerge,
		filterType:     filterClientSideUnixTS,
		parentTable:    "campaigns",
	},
	"campaign_messages": {
		endpoint:    "/v1/campaigns/{id}/messages",
		dataKey:     "messages",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyMerge,
		filterType:  filterServerSideStartEnd,
		parentTable: "campaigns",
	},
	"newsletter_test_groups": {
		endpoint:       "/v1/newsletters/{id}/test_groups",
		dataKey:        "test_groups",
		primaryKeys:    []string{"id"},
		strategy:       config.StrategyReplace,
		filterType:     filterNone,
		isNonPaginated: true,
		parentTable:    "newsletters",
	},
	"customers": {
		primaryKeys: []string{"cio_id"},
		strategy:    config.StrategyReplace,
		filterType:  filterNone,
	},
	"customer_attributes": {
		endpoint:       "/v1/customers/{id}/attributes",
		dataKey:        "customer",
		primaryKeys:    []string{"customer_id"},
		strategy:       config.StrategyReplace,
		filterType:     filterNone,
		isNonPaginated: true,
		parentTable:    "customers",
	},
	"customer_messages": {
		endpoint:    "/v1/customers/{id}/messages",
		dataKey:     "messages",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyMerge,
		filterType:  filterServerSideStartEnd,
		parentTable: "customers",
	},
	"customer_activities": {
		endpoint:    "/v1/customers/{id}/activities",
		dataKey:     "activities",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		filterType:  filterNone,
		parentTable: "customers",
	},
	"customer_relationships": {
		endpoint:    "/v1/customers/{id}/relationships",
		dataKey:     "cio_relationships",
		primaryKeys: []string{"customer_id", "object_type_id", "object_id"},
		strategy:    config.StrategyReplace,
		filterType:  filterNone,
		parentTable: "customers",
	},
	"objects": {
		primaryKeys: []string{"object_type_id", "object_id"},
		strategy:    config.StrategyReplace,
		filterType:  filterNone,
	},
	"broadcast_metrics": {
		parentTable: "broadcasts",
		primaryKeys: []string{"broadcast_id", "period", "step_index"},
		strategy:    config.StrategyReplace,
	},
	"broadcast_action_metrics": {
		parentTable: "broadcast_actions",
		primaryKeys: []string{"broadcast_id", "action_id", "period", "step_index"},
		strategy:    config.StrategyReplace,
	},
	"campaign_metrics": {
		parentTable: "campaigns",
		primaryKeys: []string{"campaign_id", "period", "step_index"},
		strategy:    config.StrategyReplace,
	},
	"campaign_action_metrics": {
		parentTable: "campaign_actions",
		primaryKeys: []string{"campaign_id", "action_id", "period", "step_index"},
		strategy:    config.StrategyReplace,
	},
	"newsletter_metrics": {
		parentTable: "newsletters",
		primaryKeys: []string{"newsletter_id", "period", "step_index"},
		strategy:    config.StrategyReplace,
	},
}

var maxStepsByPeriod = map[string]int{
	"hours":  24,
	"days":   45,
	"weeks":  12,
	"months": 12,
}

func supportedTableNames() string {
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

type CustomerIOSource struct {
	client  *gonghttp.Client
	baseURL string
}

func NewCustomerIOSource() *CustomerIOSource {
	return &CustomerIOSource{}
}

func (s *CustomerIOSource) Schemes() []string {
	return []string{"customerio"}
}

func (s *CustomerIOSource) HandlesIncrementality() bool {
	return true
}

func (s *CustomerIOSource) Connect(ctx context.Context, uri string) error {
	apiKey, baseURL, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.baseURL = baseURL
	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(apiKey)),
	)

	config.Debug("[CUSTOMERIO] Connected to %s", baseURL)
	return nil
}

func (s *CustomerIOSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *CustomerIOSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	period := ""

	if strings.Contains(tableName, ":") {
		parts := strings.SplitN(tableName, ":", 2)
		tableName = parts[0]
		period = parts[1]
	}

	cfg, ok := tables[tableName]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, supportedTableNames())
	}

	isMetrics := strings.HasSuffix(tableName, "_metrics")
	if isMetrics && period == "" {
		return nil, fmt.Errorf("metrics tables require a period suffix, e.g. %s:days", tableName)
	}
	if isMetrics {
		if _, ok := maxStepsByPeriod[period]; !ok {
			return nil, fmt.Errorf("invalid period '%s' for %s: must be one of: hours, days, weeks, months", period, tableName)
		}
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    cfg.primaryKeys,
		TableIncrementalKey: cfg.incrementalKey,
		TableStrategy:       cfg.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("customerio source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, period, opts)
		},
	}, nil
}

func parseURI(uri string) (string, string, error) {
	if !strings.HasPrefix(uri, "customerio://") {
		return "", "", fmt.Errorf("invalid customerio URI: must start with customerio://")
	}

	rest := strings.TrimPrefix(uri, "customerio://")
	rest = strings.TrimPrefix(rest, "?")
	if rest == "" {
		return "", "", fmt.Errorf("api_key is required in customerio URI")
	}

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse customerio URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", "", fmt.Errorf("api_key is required in customerio URI")
	}

	region := strings.ToLower(values.Get("region"))
	if region == "" {
		region = "us"
	}
	if region != "us" && region != "eu" {
		return "", "", fmt.Errorf("invalid region '%s' for customerio URI: must be one of: us, eu", region)
	}

	baseURL := defaultBaseURL
	if region == "eu" {
		baseURL = euBaseURL
	}

	return apiKey, baseURL, nil
}

func (s *CustomerIOSource) read(ctx context.Context, table, period string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if cfg, ok := tables[table]; ok {
		if cfg.filterType == filterClientSideUnixTS && opts.IntervalStart == nil && opts.IntervalEnd == nil {
			fmt.Fprintf(os.Stderr, "[WARNING] table %s supports client-side filtering by %q, but no --interval-start or --interval-end was provided — all records will be fetched\n", table, cfg.incrementalKey)
		}
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "activities":
			err = s.readActivities(ctx, opts, results)
		case "broadcasts":
			err = s.readBroadcasts(ctx, opts, results)
		case "broadcast_actions":
			err = s.readBroadcastActions(ctx, opts, results)
		case "broadcast_messages":
			err = s.readBroadcastMessages(ctx, opts, results)
		case "broadcast_metrics":
			err = s.readBroadcastMetrics(ctx, period, opts, results)
		case "broadcast_action_metrics":
			err = s.readBroadcastActionMetrics(ctx, period, opts, results)
		case "campaigns":
			err = s.readCampaigns(ctx, opts, results)
		case "campaign_actions":
			err = s.readCampaignActions(ctx, opts, results)
		case "campaign_messages":
			err = s.readCampaignMessages(ctx, opts, results)
		case "campaign_metrics":
			err = s.readCampaignMetrics(ctx, period, opts, results)
		case "campaign_action_metrics":
			err = s.readCampaignActionMetrics(ctx, period, opts, results)
		case "collections":
			err = s.readCollections(ctx, opts, results)
		case "customers":
			err = s.readCustomers(ctx, opts, results)
		case "customer_attributes":
			err = s.readCustomerAttributes(ctx, opts, results)
		case "customer_messages":
			err = s.readCustomerMessages(ctx, opts, results)
		case "customer_activities":
			err = s.readCustomerActivities(ctx, opts, results)
		case "customer_relationships":
			err = s.readCustomerRelationships(ctx, opts, results)
		case "exports":
			err = s.readExports(ctx, opts, results)
		case "info_ip_addresses":
			err = s.readInfoIPAddresses(ctx, opts, results)
		case "messages":
			err = s.readMessages(ctx, opts, results)
		case "newsletters":
			err = s.readNewsletters(ctx, opts, results)
		case "newsletter_metrics":
			err = s.readNewsletterMetrics(ctx, period, opts, results)
		case "newsletter_test_groups":
			err = s.readNewsletterTestGroups(ctx, opts, results)
		case "objects":
			err = s.readObjects(ctx, opts, results)
		case "object_types":
			err = s.readObjectTypes(ctx, opts, results)
		case "reporting_webhooks":
			err = s.readReportingWebhooks(ctx, opts, results)
		case "segments":
			err = s.readSegments(ctx, opts, results)
		case "sender_identities":
			err = s.readSenderIdentities(ctx, opts, results)
		case "subscription_topics":
			err = s.readSubscriptionTopics(ctx, opts, results)
		case "transactional_messages":
			err = s.readTransactionalMessages(ctx, opts, results)
		case "workspaces":
			err = s.readWorkspaces(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// --- Helper functions ---

func (s *CustomerIOSource) paginateAndSend(ctx context.Context, endpoint, dataKey string, params map[string]string, ft filterType, incrementalKey string, opts source.ReadOptions, results chan<- source.RecordBatchResult, injectFields map[string]interface{}) error {
	nextToken := ""

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx)
		for k, v := range params {
			req.SetQueryParam(k, v)
		}
		if nextToken != "" {
			req.SetQueryParam("start", nextToken)
		}

		if ft == filterServerSideStartEnd {
			if opts.IntervalStart != nil {
				req.SetQueryParam("start_ts", strconv.FormatInt(opts.IntervalStart.Unix(), 10))
			}
			if opts.IntervalEnd != nil {
				req.SetQueryParam("end_ts", strconv.FormatInt(opts.IntervalEnd.Unix(), 10))
			}
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("customerio API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		body, err := jsonDecode(resp.Body())
		if err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		items := extractItems(body, dataKey)
		if items == nil {
			break
		}

		for i := range items {
			for k, v := range injectFields {
				items[i][k] = v
			}
		}

		if ft == filterClientSideUnixTS && incrementalKey != "" {
			items = filterByUnixTimestamp(items, incrementalKey, opts.IntervalStart, opts.IntervalEnd)
			convertUnixTSField(items, incrementalKey)
		}

		if len(items) > 0 {
			if err := sendBatch(items, opts, results); err != nil {
				return err
			}
		}

		next, _ := body["next"].(string)
		if next == "" {
			break
		}
		nextToken = next
	}

	return nil
}

func (s *CustomerIOSource) paginateAndSendPost(ctx context.Context, endpoint string, postBody map[string]interface{}, dataKey string, opts source.ReadOptions, results chan<- source.RecordBatchResult, injectFields map[string]interface{}) error {
	nextToken := ""

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetHeader("Content-Type", "application/json").
			SetBody(postBody)
		if nextToken != "" {
			req.SetQueryParam("start", nextToken)
		}

		resp, err := req.Post(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("customerio API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		body, err := jsonDecode(resp.Body())
		if err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		items := extractItems(body, dataKey)
		if items == nil {
			break
		}

		for i := range items {
			for k, v := range injectFields {
				items[i][k] = v
			}
		}

		if len(items) > 0 {
			if err := sendBatch(items, opts, results); err != nil {
				return err
			}
		}

		next, _ := body["next"].(string)
		if next == "" {
			break
		}
		nextToken = next
	}

	return nil
}

func (s *CustomerIOSource) fetchSingle(ctx context.Context, endpoint, dataKey string) ([]map[string]interface{}, error) {
	resp, err := s.client.R(ctx).Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", endpoint, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("customerio API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	body, err := jsonDecode(resp.Body())
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", endpoint, err)
	}

	items := extractItems(body, dataKey)
	return items, nil
}

func (s *CustomerIOSource) fetchAll(ctx context.Context, endpoint, dataKey string, params map[string]string) ([]map[string]interface{}, error) {
	var allItems []map[string]interface{}
	nextToken := ""

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req := s.client.R(ctx)
		for k, v := range params {
			req.SetQueryParam(k, v)
		}
		if nextToken != "" {
			req.SetQueryParam("start", nextToken)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("customerio API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		body, err := jsonDecode(resp.Body())
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		items := extractItems(body, dataKey)
		if items != nil {
			allItems = append(allItems, items...)
		}

		next, _ := body["next"].(string)
		if next == "" {
			break
		}
		nextToken = next
	}

	return allItems, nil
}

func extractItems(body map[string]interface{}, dataKey string) []map[string]interface{} {
	raw, ok := body[dataKey]
	if !ok {
		return nil
	}

	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}

	items := make([]map[string]interface{}, 0, len(arr))
	for _, v := range arr {
		if m, ok := v.(map[string]interface{}); ok {
			items = append(items, m)
		}
	}
	return items
}

func filterByUnixTimestamp(items []map[string]interface{}, field string, start, end *time.Time) []map[string]interface{} {
	if start == nil && end == nil {
		return items
	}

	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		ts, ok := getUnixTimestamp(item, field)
		if !ok {
			fmt.Fprintf(os.Stderr, "[WARNING] record missing timestamp field %q (id=%v), excluding from results\n", field, item["id"])
			continue
		}
		if start != nil && ts < start.Unix() {
			continue
		}
		if end != nil && ts > end.Unix() {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func getUnixTimestamp(item map[string]interface{}, field string) (int64, bool) {
	raw, ok := item[field]
	if !ok || raw == nil {
		return 0, false
	}

	switch v := raw.(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			f, err := v.Float64()
			if err != nil {
				return 0, false
			}
			return int64(f), true
		}
		return n, true
	case float64:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}

func jsonDecode(data []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func convertUnixTSField(items []map[string]interface{}, field string) {
	for _, item := range items {
		switch v := item[field].(type) {
		case json.Number:
			if i, err := v.Int64(); err == nil && i != 0 {
				item[field] = time.Unix(i, 0).UTC()
			}
		case float64:
			if v != 0 {
				item[field] = time.Unix(int64(v), 0).UTC()
			}
		case int64:
			if v != 0 {
				item[field] = time.Unix(v, 0).UTC()
			}
		}
	}
}

func sendBatch(items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert items to Arrow: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

// --- Top-level resource read functions ---

func (s *CustomerIOSource) readActivities(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["activities"]
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.dataKey, cfg.defaultParams, cfg.filterType, cfg.incrementalKey, opts, results, nil)
}

func (s *CustomerIOSource) readBroadcasts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["broadcasts"]
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.dataKey, cfg.defaultParams, cfg.filterType, cfg.incrementalKey, opts, results, nil)
}

func (s *CustomerIOSource) readCampaigns(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["campaigns"]
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.dataKey, cfg.defaultParams, cfg.filterType, cfg.incrementalKey, opts, results, nil)
}

func (s *CustomerIOSource) readCollections(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["collections"]
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.dataKey, cfg.defaultParams, cfg.filterType, cfg.incrementalKey, opts, results, nil)
}

func (s *CustomerIOSource) readExports(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["exports"]
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.dataKey, cfg.defaultParams, cfg.filterType, cfg.incrementalKey, opts, results, nil)
}

func (s *CustomerIOSource) readMessages(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["messages"]
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.dataKey, cfg.defaultParams, cfg.filterType, cfg.incrementalKey, opts, results, nil)
}

func (s *CustomerIOSource) readNewsletters(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["newsletters"]
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.dataKey, cfg.defaultParams, cfg.filterType, cfg.incrementalKey, opts, results, nil)
}

func (s *CustomerIOSource) readSegments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["segments"]
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.dataKey, cfg.defaultParams, cfg.filterType, cfg.incrementalKey, opts, results, nil)
}

func (s *CustomerIOSource) readTransactionalMessages(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["transactional_messages"]
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.dataKey, cfg.defaultParams, cfg.filterType, cfg.incrementalKey, opts, results, nil)
}

// --- Non-paginated top-level resources ---

func (s *CustomerIOSource) readInfoIPAddresses(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	resp, err := s.client.R(ctx).Get("/v1/info/ip_addresses")
	if err != nil {
		return fmt.Errorf("failed to fetch info/ip_addresses: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("customerio API info/ip_addresses returned status %d: %s", resp.StatusCode(), resp.String())
	}

	body, err := jsonDecode(resp.Body())
	if err != nil {
		return fmt.Errorf("failed to parse info/ip_addresses response: %w", err)
	}

	raw, ok := body["ips"]
	if !ok {
		return nil
	}

	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}

	items := make([]map[string]interface{}, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			items = append(items, map[string]interface{}{"ip": s})
		}
	}

	if len(items) > 0 {
		return sendBatch(items, opts, results)
	}
	return nil
}

func (s *CustomerIOSource) readReportingWebhooks(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["reporting_webhooks"]
	items, err := s.fetchSingle(ctx, cfg.endpoint, cfg.dataKey)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		return sendBatch(items, opts, results)
	}
	return nil
}

func (s *CustomerIOSource) readSenderIdentities(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["sender_identities"]
	return s.paginateAndSend(ctx, cfg.endpoint, cfg.dataKey, cfg.defaultParams, cfg.filterType, cfg.incrementalKey, opts, results, nil)
}

func (s *CustomerIOSource) readSubscriptionTopics(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["subscription_topics"]
	items, err := s.fetchSingle(ctx, cfg.endpoint, cfg.dataKey)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		return sendBatch(items, opts, results)
	}
	return nil
}

func (s *CustomerIOSource) readWorkspaces(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["workspaces"]
	items, err := s.fetchSingle(ctx, cfg.endpoint, cfg.dataKey)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		return sendBatch(items, opts, results)
	}
	return nil
}

func (s *CustomerIOSource) readObjectTypes(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables["object_types"]
	items, err := s.fetchSingle(ctx, cfg.endpoint, cfg.dataKey)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		return sendBatch(items, opts, results)
	}
	return nil
}

// --- Parallel helper for nested resources ---

type parentItem struct {
	id   string
	data map[string]interface{}
}

func (s *CustomerIOSource) runParallel(ctx context.Context, parents []parentItem, worker func(ctx context.Context, p parentItem) error) error {
	taskCh := make(chan parentItem, defaultParallelism)
	errs := make(chan error, 1)
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i := 0; i < defaultParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range taskCh {
				if err := worker(ctx, p); err != nil {
					select {
					case errs <- err:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}

	for _, p := range parents {
		select {
		case taskCh <- p:
		case <-ctx.Done():
		}
	}
	close(taskCh)

	wg.Wait()
	close(errs)

	if err := <-errs; err != nil {
		return err
	}
	return nil
}

func toParentItems(items []map[string]interface{}, idField string) []parentItem {
	parents := make([]parentItem, 0, len(items))
	for _, item := range items {
		id := extractID(item, idField)
		if id == "" {
			continue
		}
		parents = append(parents, parentItem{id: id, data: item})
	}
	return parents
}

// --- Nested resource read functions ---

func (s *CustomerIOSource) readBroadcastActions(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	broadcasts, err := s.fetchAll(ctx, "/v1/broadcasts", "broadcasts", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch parent broadcasts: %w", err)
	}

	return s.runParallel(ctx, toParentItems(broadcasts, "id"), func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/broadcasts/%s/actions", p.id)
		items, err := s.fetchSingle(ctx, endpoint, "actions")
		if err != nil {
			config.Debug("[CUSTOMERIO] Failed to fetch actions for broadcast %s: %v", p.id, err)
			return nil
		}

		for i := range items {
			items[i]["broadcast_id"] = p.data["id"]
		}

		items = filterByUnixTimestamp(items, "updated", opts.IntervalStart, opts.IntervalEnd)
		convertUnixTSField(items, "updated")

		if len(items) > 0 {
			return sendBatch(items, opts, results)
		}
		return nil
	})
}

func (s *CustomerIOSource) readBroadcastMessages(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	broadcasts, err := s.fetchAll(ctx, "/v1/broadcasts", "broadcasts", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch parent broadcasts: %w", err)
	}

	return s.runParallel(ctx, toParentItems(broadcasts, "id"), func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/broadcasts/%s/messages", p.id)
		inject := map[string]interface{}{"broadcast_id": p.data["id"]}
		return s.paginateAndSend(ctx, endpoint, "messages", nil, filterServerSideStartEnd, "", opts, results, inject)
	})
}

func (s *CustomerIOSource) readCampaignActions(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	campaigns, err := s.fetchAll(ctx, "/v1/campaigns", "campaigns", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch parent campaigns: %w", err)
	}

	return s.runParallel(ctx, toParentItems(campaigns, "id"), func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/campaigns/%s/actions", p.id)
		items, err := s.fetchSingle(ctx, endpoint, "actions")
		if err != nil {
			config.Debug("[CUSTOMERIO] Failed to fetch actions for campaign %s: %v", p.id, err)
			return nil
		}

		for i := range items {
			items[i]["campaign_id"] = p.data["id"]
		}

		items = filterByUnixTimestamp(items, "updated", opts.IntervalStart, opts.IntervalEnd)
		convertUnixTSField(items, "updated")

		if len(items) > 0 {
			return sendBatch(items, opts, results)
		}
		return nil
	})
}

func (s *CustomerIOSource) readCampaignMessages(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	campaigns, err := s.fetchAll(ctx, "/v1/campaigns", "campaigns", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch parent campaigns: %w", err)
	}

	return s.runParallel(ctx, toParentItems(campaigns, "id"), func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/campaigns/%s/messages", p.id)
		inject := map[string]interface{}{"campaign_id": p.data["id"]}
		return s.paginateAndSend(ctx, endpoint, "messages", nil, filterServerSideStartEnd, "", opts, results, inject)
	})
}

func (s *CustomerIOSource) readNewsletterTestGroups(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	newsletters, err := s.fetchAll(ctx, "/v1/newsletters", "newsletters", map[string]string{
		"limit": strconv.Itoa(activitiesPageSize),
	})
	if err != nil {
		return fmt.Errorf("failed to fetch parent newsletters: %w", err)
	}

	return s.runParallel(ctx, toParentItems(newsletters, "id"), func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/newsletters/%s/test_groups", p.id)
		items, err := s.fetchSingle(ctx, endpoint, "test_groups")
		if err != nil {
			config.Debug("[CUSTOMERIO] Failed to fetch test_groups for newsletter %s: %v", p.id, err)
			return nil
		}

		for i := range items {
			items[i]["newsletter_id"] = p.data["id"]
		}

		if len(items) > 0 {
			return sendBatch(items, opts, results)
		}
		return nil
	})
}

// --- Customers (special: iterate segments for membership) ---

func (s *CustomerIOSource) readCustomers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	segments, err := s.fetchAll(ctx, "/v1/segments", "segments", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch segments: %w", err)
	}

	seen := make(map[string]bool)
	var batch []map[string]interface{}

	for _, segment := range segments {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		segmentID := extractID(segment, "id")
		if segmentID == "" {
			continue
		}

		nextToken := ""
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			endpoint := fmt.Sprintf("/v1/segments/%s/membership", segmentID)
			req := s.client.R(ctx).
				SetQueryParam("limit", strconv.Itoa(membersPageSize))
			if nextToken != "" {
				req.SetQueryParam("start", nextToken)
			}

			resp, err := req.Get(endpoint)
			if err != nil {
				return fmt.Errorf("failed to fetch segment %s membership: %w", segmentID, err)
			}
			if !resp.IsSuccess() {
				config.Debug("[CUSTOMERIO] Segment %s membership returned status %d, skipping", segmentID, resp.StatusCode())
				break
			}

			body, err := jsonDecode(resp.Body())
			if err != nil {
				return fmt.Errorf("failed to parse segment %s membership response: %w", segmentID, err)
			}

			items := extractItems(body, "identifiers")
			for _, item := range items {
				cioID := extractStringField(item, "cio_id")
				if cioID == "" {
					continue
				}
				if seen[cioID] {
					continue
				}
				seen[cioID] = true
				batch = append(batch, item)

				if len(batch) >= membersPageSize {
					if err := sendBatch(batch, opts, results); err != nil {
						return err
					}
					batch = nil
				}
			}

			next, _ := body["next"].(string)
			if next == "" {
				break
			}
			nextToken = next
		}
	}

	if len(batch) > 0 {
		return sendBatch(batch, opts, results)
	}
	return nil
}

func (s *CustomerIOSource) readCustomerAttributes(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	customerIDs, err := s.getAllCustomerIDs(ctx)
	if err != nil {
		return err
	}

	parents := make([]parentItem, 0, len(customerIDs))
	for _, id := range customerIDs {
		parents = append(parents, parentItem{id: id})
	}

	return s.runParallel(ctx, parents, func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/customers/%s/attributes", p.id)
		resp, err := s.client.R(ctx).Get(endpoint)
		if err != nil {
			config.Debug("[CUSTOMERIO] Failed to fetch attributes for customer %s: %v", p.id, err)
			return nil
		}
		if !resp.IsSuccess() {
			config.Debug("[CUSTOMERIO] Customer %s attributes returned status %d, skipping", p.id, resp.StatusCode())
			return nil
		}

		body, err := jsonDecode(resp.Body())
		if err != nil {
			config.Debug("[CUSTOMERIO] Failed to parse attributes for customer %s: %v", p.id, err)
			return nil
		}

		raw, ok := body["customer"]
		if !ok {
			return nil
		}
		customerData, ok := raw.(map[string]interface{})
		if !ok {
			return nil
		}

		customerData["customer_id"] = p.id
		return sendBatch([]map[string]interface{}{customerData}, opts, results)
	})
}

func (s *CustomerIOSource) readCustomerMessages(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	customerIDs, err := s.getAllCustomerIDs(ctx)
	if err != nil {
		return err
	}

	parents := make([]parentItem, 0, len(customerIDs))
	for _, id := range customerIDs {
		parents = append(parents, parentItem{id: id})
	}

	return s.runParallel(ctx, parents, func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/customers/%s/messages", p.id)
		inject := map[string]interface{}{"customer_id": p.id}
		return s.paginateAndSend(ctx, endpoint, "messages", nil, filterServerSideStartEnd, "", opts, results, inject)
	})
}

func (s *CustomerIOSource) readCustomerActivities(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	customerIDs, err := s.getAllCustomerIDs(ctx)
	if err != nil {
		return err
	}

	parents := make([]parentItem, 0, len(customerIDs))
	for _, id := range customerIDs {
		parents = append(parents, parentItem{id: id})
	}

	return s.runParallel(ctx, parents, func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/customers/%s/activities", p.id)
		inject := map[string]interface{}{"customer_id": p.id}
		return s.paginateAndSend(ctx, endpoint, "activities", nil, filterNone, "", opts, results, inject)
	})
}

func (s *CustomerIOSource) readCustomerRelationships(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	customerIDs, err := s.getAllCustomerIDs(ctx)
	if err != nil {
		return err
	}

	parents := make([]parentItem, 0, len(customerIDs))
	for _, id := range customerIDs {
		parents = append(parents, parentItem{id: id})
	}

	return s.runParallel(ctx, parents, func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/customers/%s/relationships", p.id)
		allItems, err := s.fetchAll(ctx, endpoint, "cio_relationships", nil)
		if err != nil {
			config.Debug("[CUSTOMERIO] Failed to fetch relationships for customer %s: %v", p.id, err)
			return nil
		}

		var processed []map[string]interface{}
		for _, item := range allItems {
			rel := map[string]interface{}{}
			for k, v := range item {
				rel[k] = v
			}

			rel["customer_id"] = p.id
			if identifiers, ok := item["identifiers"].(map[string]interface{}); ok {
				if oid, ok := identifiers["object_id"]; ok {
					rel["object_id"] = oid
				} else if oid, ok := identifiers["cio_object_id"]; ok {
					rel["object_id"] = oid
				}
			}

			processed = append(processed, rel)
		}

		if len(processed) > 0 {
			return sendBatch(processed, opts, results)
		}
		return nil
	})
}

// --- Objects (POST-based) ---

func (s *CustomerIOSource) readObjects(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	objectTypes, err := s.fetchSingle(ctx, "/v1/object_types", "types")
	if err != nil {
		return fmt.Errorf("failed to fetch object types: %w", err)
	}

	return s.runParallel(ctx, toParentItems(objectTypes, "id"), func(ctx context.Context, p parentItem) error {
		typeID := fmt.Sprintf("%v", p.data["id"])
		postBody := map[string]interface{}{
			"object_type_id": typeID,
			"limit":          objectsPageSize,
			"filter": map[string]interface{}{
				"and": []map[string]interface{}{
					{
						"object_attribute": map[string]interface{}{
							"field":    "object_id",
							"operator": "exists",
							"type_id":  typeID,
						},
					},
				},
			},
		}
		inject := map[string]interface{}{
			"object_type_id": p.data["id"],
		}
		return s.paginateAndSendPost(ctx, "/v1/objects", postBody, "identifiers", opts, results, inject)
	})
}

// --- Metrics read functions ---

func (s *CustomerIOSource) readBroadcastMetrics(ctx context.Context, period string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	broadcasts, err := s.fetchAll(ctx, "/v1/broadcasts", "broadcasts", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch parent broadcasts: %w", err)
	}

	steps := getMaxSteps(period, false)

	return s.runParallel(ctx, toParentItems(broadcasts, "id"), func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/broadcasts/%s/metrics", p.id)
		items, err := s.fetchMetrics(ctx, endpoint, period, steps, nil)
		if err != nil {
			config.Debug("[CUSTOMERIO] Failed to fetch metrics for broadcast %s: %v", p.id, err)
			return nil
		}

		for i := range items {
			items[i]["broadcast_id"] = p.data["id"]
		}

		if len(items) > 0 {
			return sendBatch(items, opts, results)
		}
		return nil
	})
}

func (s *CustomerIOSource) readBroadcastActionMetrics(ctx context.Context, period string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	broadcasts, err := s.fetchAll(ctx, "/v1/broadcasts", "broadcasts", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch parent broadcasts: %w", err)
	}

	steps := getMaxSteps(period, false)

	return s.runParallel(ctx, toParentItems(broadcasts, "id"), func(ctx context.Context, p parentItem) error {
		actions, err := s.fetchSingle(ctx, fmt.Sprintf("/v1/broadcasts/%s/actions", p.id), "actions")
		if err != nil {
			config.Debug("[CUSTOMERIO] Failed to fetch actions for broadcast %s: %v", p.id, err)
			return nil
		}

		for _, action := range actions {
			actionID := extractID(action, "id")
			if actionID == "" {
				continue
			}

			endpoint := fmt.Sprintf("/v1/broadcasts/%s/actions/%s/metrics", p.id, actionID)
			items, err := s.fetchMetrics(ctx, endpoint, period, steps, nil)
			if err != nil {
				config.Debug("[CUSTOMERIO] Failed to fetch metrics for broadcast %s action %s: %v", p.id, actionID, err)
				continue
			}

			for i := range items {
				items[i]["broadcast_id"] = p.data["id"]
				items[i]["action_id"] = action["id"]
			}

			if len(items) > 0 {
				if err := sendBatch(items, opts, results); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *CustomerIOSource) readCampaignMetrics(ctx context.Context, period string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	campaigns, err := s.fetchAll(ctx, "/v1/campaigns", "campaigns", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch parent campaigns: %w", err)
	}

	return s.runParallel(ctx, toParentItems(campaigns, "id"), func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/campaigns/%s/metrics", p.id)
		items, err := s.fetchCampaignMetrics(ctx, endpoint, period, opts)
		if err != nil {
			config.Debug("[CUSTOMERIO] Failed to fetch metrics for campaign %s: %v", p.id, err)
			return nil
		}

		for i := range items {
			items[i]["campaign_id"] = p.data["id"]
		}

		if len(items) > 0 {
			return sendBatch(items, opts, results)
		}
		return nil
	})
}

func (s *CustomerIOSource) readCampaignActionMetrics(ctx context.Context, period string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	campaigns, err := s.fetchAll(ctx, "/v1/campaigns", "campaigns", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch parent campaigns: %w", err)
	}

	return s.runParallel(ctx, toParentItems(campaigns, "id"), func(ctx context.Context, p parentItem) error {
		actions, err := s.fetchSingle(ctx, fmt.Sprintf("/v1/campaigns/%s/actions", p.id), "actions")
		if err != nil {
			config.Debug("[CUSTOMERIO] Failed to fetch actions for campaign %s: %v", p.id, err)
			return nil
		}

		for _, action := range actions {
			actionID := extractID(action, "id")
			if actionID == "" {
				continue
			}

			endpoint := fmt.Sprintf("/v1/campaigns/%s/actions/%s/metrics", p.id, actionID)
			items, err := s.fetchCampaignMetrics(ctx, endpoint, period, opts)
			if err != nil {
				config.Debug("[CUSTOMERIO] Failed to fetch metrics for campaign %s action %s: %v", p.id, actionID, err)
				continue
			}

			for i := range items {
				items[i]["campaign_id"] = p.data["id"]
				items[i]["action_id"] = action["id"]
			}

			if len(items) > 0 {
				if err := sendBatch(items, opts, results); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *CustomerIOSource) readNewsletterMetrics(ctx context.Context, period string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	newsletters, err := s.fetchAll(ctx, "/v1/newsletters", "newsletters", map[string]string{
		"limit": strconv.Itoa(activitiesPageSize),
	})
	if err != nil {
		return fmt.Errorf("failed to fetch parent newsletters: %w", err)
	}

	isNewsletterMonths := period == "months"
	steps := getMaxSteps(period, isNewsletterMonths)

	return s.runParallel(ctx, toParentItems(newsletters, "id"), func(ctx context.Context, p parentItem) error {
		endpoint := fmt.Sprintf("/v1/newsletters/%s/metrics", p.id)
		items, err := s.fetchMetrics(ctx, endpoint, period, steps, nil)
		if err != nil {
			config.Debug("[CUSTOMERIO] Failed to fetch metrics for newsletter %s: %v", p.id, err)
			return nil
		}

		for i := range items {
			items[i]["newsletter_id"] = p.data["id"]
		}

		if len(items) > 0 {
			return sendBatch(items, opts, results)
		}
		return nil
	})
}

// --- Metrics helpers ---

func (s *CustomerIOSource) fetchMetrics(ctx context.Context, endpoint, period string, steps int, extraParams map[string]string) ([]map[string]interface{}, error) {
	req := s.client.R(ctx).
		SetQueryParam("period", period).
		SetQueryParam("steps", strconv.Itoa(steps))

	for k, v := range extraParams {
		req.SetQueryParam(k, v)
	}

	resp, err := req.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", endpoint, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("customerio API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	body, err := jsonDecode(resp.Body())
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", endpoint, err)
	}

	return flattenMetricsSeries(body, period)
}

func (s *CustomerIOSource) fetchCampaignMetrics(ctx context.Context, endpoint, period string, opts source.ReadOptions) ([]map[string]interface{}, error) {
	req := s.client.R(ctx).
		SetQueryParam("version", "2").
		SetQueryParam("res", period)

	if opts.IntervalStart != nil {
		req.SetQueryParam("start", strconv.FormatInt(opts.IntervalStart.Unix(), 10))
	}
	if opts.IntervalEnd != nil {
		req.SetQueryParam("end", strconv.FormatInt(opts.IntervalEnd.Unix(), 10))
	}

	resp, err := req.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", endpoint, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("customerio API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	body, err := jsonDecode(resp.Body())
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", endpoint, err)
	}

	return flattenMetricsSeries(body, period)
}

func flattenMetricsSeries(body map[string]interface{}, period string) ([]map[string]interface{}, error) {
	seriesOuter, ok := body["series"]
	if !ok {
		return nil, nil
	}

	seriesMap, ok := seriesOuter.(map[string]interface{})
	if !ok {
		return nil, nil
	}

	seriesInner, ok := seriesMap["series"]
	if !ok {
		return nil, nil
	}

	seriesArr, ok := seriesInner.([]interface{})
	if !ok {
		return nil, nil
	}

	var items []map[string]interface{}
	for stepIndex, entry := range seriesArr {
		item, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		item["period"] = period
		item["step_index"] = stepIndex
		items = append(items, item)
	}

	return items, nil
}

func getMaxSteps(period string, isNewsletterMonths bool) int {
	if isNewsletterMonths {
		return 121
	}
	if steps, ok := maxStepsByPeriod[period]; ok {
		return steps
	}
	return 12
}

// --- Utility functions ---

func extractID(item map[string]interface{}, field string) string {
	raw, ok := item[field]
	if !ok || raw == nil {
		return ""
	}

	switch v := raw.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	}
	return fmt.Sprintf("%v", raw)
}

func extractStringField(item map[string]interface{}, field string) string {
	raw, ok := item[field]
	if !ok || raw == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", raw)
}

func (s *CustomerIOSource) getAllCustomerIDs(ctx context.Context) ([]string, error) {
	segments, err := s.fetchAll(ctx, "/v1/segments", "segments", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch segments: %w", err)
	}

	seen := make(map[string]bool)
	var ids []string

	for _, segment := range segments {
		segmentID := extractID(segment, "id")
		if segmentID == "" {
			continue
		}

		nextToken := ""
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			endpoint := fmt.Sprintf("/v1/segments/%s/membership", segmentID)
			req := s.client.R(ctx).
				SetQueryParam("limit", strconv.Itoa(membersPageSize))
			if nextToken != "" {
				req.SetQueryParam("start", nextToken)
			}

			resp, err := req.Get(endpoint)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch segment %s membership: %w", segmentID, err)
			}
			if !resp.IsSuccess() {
				config.Debug("[CUSTOMERIO] Segment %s membership returned status %d, skipping", segmentID, resp.StatusCode())
				break
			}

			body, err := jsonDecode(resp.Body())
			if err != nil {
				return nil, fmt.Errorf("failed to parse segment %s membership: %w", segmentID, err)
			}

			items := extractItems(body, "identifiers")
			for _, item := range items {
				cioID := extractStringField(item, "cio_id")
				if cioID == "" {
					continue
				}
				if !seen[cioID] {
					seen[cioID] = true
					ids = append(ids, cioID)
				}
			}

			next, _ := body["next"].(string)
			if next == "" {
				break
			}
			nextToken = next
		}
	}

	config.Debug("[CUSTOMERIO] Found %d unique customers across %d segments", len(ids), len(segments))
	return ids, nil
}

var _ source.Source = (*CustomerIOSource)(nil)
