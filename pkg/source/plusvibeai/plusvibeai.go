package plusvibeai

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
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL         = "https://api.plusvibe.ai/api/v1"
	requestTimeout  = 300 * time.Second
	defaultPageSize = 100
)

var campaignFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "camp_name", DataType: schema.TypeString, Nullable: true},
	{Name: "parent_camp_id", DataType: schema.TypeString, Nullable: true},
	{Name: "campaign_type", DataType: schema.TypeString, Nullable: true},
	{Name: "organization_id", DataType: schema.TypeString, Nullable: true},
	{Name: "workspace_id", DataType: schema.TypeString, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "modified_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "last_lead_sent", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "last_paused_at_bounced", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "tags", DataType: schema.TypeJSON, Nullable: true},
	{Name: "template_id", DataType: schema.TypeString, Nullable: true},
	{Name: "email_accounts", DataType: schema.TypeJSON, Nullable: true},
	{Name: "daily_limit", DataType: schema.TypeInt64, Nullable: true},
	{Name: "interval_limit_in_min", DataType: schema.TypeInt64, Nullable: true},
	{Name: "send_priority", DataType: schema.TypeString, Nullable: true},
	{Name: "send_as_txt", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "is_emailopened_tracking", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "is_unsubscribed_link", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "exclude_ooo", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "is_acc_based_sending", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "send_risky_email", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "unsub_blocklist", DataType: schema.TypeJSON, Nullable: true},
	{Name: "other_email_acc", DataType: schema.TypeJSON, Nullable: true},
	{Name: "is_esp_match", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "stop_on_lead_replied", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "is_pause_on_bouncerate", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "bounce_rate_limit", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "is_paused_at_bounced", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "schedule", DataType: schema.TypeJSON, Nullable: true},
	{Name: "first_wait_time", DataType: schema.TypeInt64, Nullable: true},
	{Name: "camp_st_date", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "camp_end_date", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "events", DataType: schema.TypeJSON, Nullable: true},
	{Name: "sequences", DataType: schema.TypeJSON, Nullable: true},
	{Name: "sequence_steps", DataType: schema.TypeJSON, Nullable: true},
	{Name: "camp_emails", DataType: schema.TypeJSON, Nullable: true},
	{Name: "lead_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "completed_lead_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "lead_contacted_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "sent_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "opened_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "unique_opened_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "replied_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "bounced_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "unsubscribed_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "positive_reply_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "negative_reply_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "neutral_reply_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "email_sent_today", DataType: schema.TypeInt64, Nullable: true},
	{Name: "opportunity_val", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "open_rate", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "replied_rate", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "custom_fields", DataType: schema.TypeJSON, Nullable: true},
}

var leadFields = []schema.Column{
	{Name: "_id", DataType: schema.TypeString, Nullable: false},
	{Name: "organization_id", DataType: schema.TypeString, Nullable: true},
	{Name: "campaign_id", DataType: schema.TypeString, Nullable: true},
	{Name: "workspace_id", DataType: schema.TypeString, Nullable: true},
	{Name: "is_completed", DataType: schema.TypeInt64, Nullable: true},
	{Name: "current_step", DataType: schema.TypeInt64, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "label", DataType: schema.TypeString, Nullable: true},
	{Name: "email_account_id", DataType: schema.TypeString, Nullable: true},
	{Name: "email_acc_name", DataType: schema.TypeString, Nullable: true},
	{Name: "camp_name", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "modified_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "last_sent_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "sent_step", DataType: schema.TypeInt64, Nullable: true},
	{Name: "replied_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "opened_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "is_mx", DataType: schema.TypeInt64, Nullable: true},
	{Name: "mx", DataType: schema.TypeString, Nullable: true},
	{Name: "email", DataType: schema.TypeString, Nullable: true},
	{Name: "first_name", DataType: schema.TypeString, Nullable: true},
	{Name: "last_name", DataType: schema.TypeString, Nullable: true},
	{Name: "phone_number", DataType: schema.TypeString, Nullable: true},
	{Name: "address_line", DataType: schema.TypeString, Nullable: true},
	{Name: "city", DataType: schema.TypeString, Nullable: true},
	{Name: "state", DataType: schema.TypeString, Nullable: true},
	{Name: "country", DataType: schema.TypeString, Nullable: true},
	{Name: "country_code", DataType: schema.TypeString, Nullable: true},
	{Name: "job_title", DataType: schema.TypeString, Nullable: true},
	{Name: "department", DataType: schema.TypeString, Nullable: true},
	{Name: "company_name", DataType: schema.TypeString, Nullable: true},
	{Name: "company_website", DataType: schema.TypeString, Nullable: true},
	{Name: "industry", DataType: schema.TypeString, Nullable: true},
	{Name: "linkedin_person_url", DataType: schema.TypeString, Nullable: true},
	{Name: "linkedin_company_url", DataType: schema.TypeString, Nullable: true},
	{Name: "total_steps", DataType: schema.TypeInt64, Nullable: true},
	{Name: "bounce_msg", DataType: schema.TypeString, Nullable: true},
}

var emailAccountFields = []schema.Column{
	{Name: "_id", DataType: schema.TypeString, Nullable: false},
	{Name: "email", DataType: schema.TypeString, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "warmup_status", DataType: schema.TypeString, Nullable: true},
	{Name: "provider", DataType: schema.TypeString, Nullable: true},
	{Name: "timestamp_created", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "timestamp_updated", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "payload", DataType: schema.TypeJSON, Nullable: true},
}

var emailFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "message_id", DataType: schema.TypeString, Nullable: true},
	{Name: "is_unread", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "lead", DataType: schema.TypeJSON, Nullable: true},
	{Name: "lead_id", DataType: schema.TypeString, Nullable: true},
	{Name: "campaign_id", DataType: schema.TypeString, Nullable: true},
	{Name: "from_address_email", DataType: schema.TypeString, Nullable: true},
	{Name: "from_address_json", DataType: schema.TypeJSON, Nullable: true},
	{Name: "subject", DataType: schema.TypeString, Nullable: true},
	{Name: "content_preview", DataType: schema.TypeString, Nullable: true},
	{Name: "body", DataType: schema.TypeString, Nullable: true},
	{Name: "headers", DataType: schema.TypeJSON, Nullable: true},
	{Name: "label", DataType: schema.TypeString, Nullable: true},
	{Name: "thread_id", DataType: schema.TypeString, Nullable: true},
	{Name: "eaccount", DataType: schema.TypeString, Nullable: true},
	{Name: "to_address_email_list", DataType: schema.TypeJSON, Nullable: true},
	{Name: "to_address_json", DataType: schema.TypeJSON, Nullable: true},
	{Name: "cc_address_email_list", DataType: schema.TypeJSON, Nullable: true},
	{Name: "cc_address_json", DataType: schema.TypeJSON, Nullable: true},
	{Name: "bcc_address_email_list", DataType: schema.TypeJSON, Nullable: true},
	{Name: "timestamp_created", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "source_modified_at", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var blocklistFields = []schema.Column{
	{Name: "_id", DataType: schema.TypeString, Nullable: false},
	{Name: "workspace_id", DataType: schema.TypeString, Nullable: true},
	{Name: "value", DataType: schema.TypeString, Nullable: true},
	{Name: "created_by_label", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var webhookFields = []schema.Column{
	{Name: "_id", DataType: schema.TypeString, Nullable: false},
	{Name: "workspace_id", DataType: schema.TypeString, Nullable: true},
	{Name: "org_id", DataType: schema.TypeString, Nullable: true},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "secret", DataType: schema.TypeString, Nullable: true},
	{Name: "camp_ids", DataType: schema.TypeJSON, Nullable: true},
	{Name: "evt_types", DataType: schema.TypeJSON, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "integration_type", DataType: schema.TypeString, Nullable: true},
	{Name: "ignore_ooo", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "ignore_automatic", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "modified_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "last_run", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "last_resp", DataType: schema.TypeJSON, Nullable: true},
	{Name: "last_recv_resp", DataType: schema.TypeJSON, Nullable: true},
	{Name: "created_by", DataType: schema.TypeString, Nullable: true},
	{Name: "modified_by", DataType: schema.TypeString, Nullable: true},
}

var tagFields = []schema.Column{
	{Name: "_id", DataType: schema.TypeString, Nullable: false},
	{Name: "workspace_id", DataType: schema.TypeString, Nullable: true},
	{Name: "org_id", DataType: schema.TypeString, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "color", DataType: schema.TypeString, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "modified_at", DataType: schema.TypeTimestampTZ, Nullable: true},
}

type paginationStyle int

const (
	paginationSkip  paginationStyle = iota // uses skip + limit
	paginationPage                         // uses page + limit
	paginationToken                        // uses page_trail cursor token
	paginationNone                         // fetches all results in a single request
)

type tableMeta struct {
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
	tableSchema    []schema.Column
	endpoint       string
	pagination     paginationStyle
	responseKey    string
}

var supportedTables = map[string]tableMeta{
	"campaigns":      {primaryKeys: []string{"id"}, incrementalKey: "modified_at", strategy: config.StrategyMerge, tableSchema: campaignFields, endpoint: "campaign/list-all", pagination: paginationSkip},
	"leads":          {primaryKeys: []string{"_id"}, incrementalKey: "modified_at", strategy: config.StrategyMerge, tableSchema: leadFields, endpoint: "lead/workspace-leads", pagination: paginationPage},
	"email_accounts": {primaryKeys: []string{"_id"}, incrementalKey: "timestamp_updated", strategy: config.StrategyMerge, tableSchema: emailAccountFields, endpoint: "account/list", pagination: paginationSkip, responseKey: "accounts"},
	"emails":         {primaryKeys: []string{"id"}, incrementalKey: "timestamp_created", strategy: config.StrategyMerge, tableSchema: emailFields, endpoint: "unibox/emails", pagination: paginationToken},
	"blocklists":     {primaryKeys: []string{"_id"}, incrementalKey: "created_at", strategy: config.StrategyMerge, tableSchema: blocklistFields, endpoint: "blocklist/list", pagination: paginationSkip},
	"webhooks":       {primaryKeys: []string{"_id"}, incrementalKey: "modified_at", strategy: config.StrategyMerge, tableSchema: webhookFields, endpoint: "hook/list", pagination: paginationNone, responseKey: "hooks"},
	"tags":           {primaryKeys: []string{"_id"}, incrementalKey: "modified_at", strategy: config.StrategyMerge, tableSchema: tagFields, endpoint: "tags/list", pagination: paginationSkip},
}

type PlusVibeAI struct {
	client      *gonghttp.Client
	apiKey      string
	workspaceID string
}

func NewPlusVibeAI() *PlusVibeAI {
	return &PlusVibeAI{}
}

func (s *PlusVibeAI) Schemes() []string {
	return []string{"plusvibeai"}
}

func (s *PlusVibeAI) HandlesIncrementality() bool {
	return true
}

func (s *PlusVibeAI) Connect(ctx context.Context, uri string) error {
	apiKey, workspaceId, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey
	s.workspaceID = workspaceId

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(requestTimeout),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithHeader("x-api-key", apiKey),
		gonghttp.WithHeader("Accept", "application/json"),
		gonghttp.WithHeader("Content-Type", "application/json"),
		gonghttp.WithRateLimiter(5, 1),
	)

	config.Debug("[PLUSVIBEAI] Connected successfully")
	return nil
}

func parseURI(uri string) (apiKey, workspaceID string, err error) {
	if !strings.HasPrefix(uri, "plusvibeai://") {
		return "", "", fmt.Errorf("invalid plusvibeai URI: must start with plusvibeai://")
	}

	rest := strings.TrimPrefix(uri, "plusvibeai://")
	if rest == "" || rest == "?" {
		return "", "", fmt.Errorf("api_key and workspace_id are required in plusvibeai URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse plusvibeai URI query: %w", err)
	}

	apiKey = values.Get("api_key")
	if apiKey == "" {
		return "", "", fmt.Errorf("api_key is required in plusvibeai URI")
	}

	workspaceID = values.Get("workspace_id")
	if workspaceID == "" {
		return "", "", fmt.Errorf("workspace_id is required in plusvibeai URI")
	}

	return apiKey, workspaceID, nil
}

func (s *PlusVibeAI) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *PlusVibeAI) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	meta, ok := supportedTables[tableName]
	if !ok {
		tables := make([]string, 0, len(supportedTables))
		for t := range supportedTables {
			tables = append(tables, t)
		}
		return nil, fmt.Errorf("unsupported table: %s (supported: %v)", tableName, tables)
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    meta.primaryKeys,
		TableIncrementalKey: meta.incrementalKey,
		TableStrategy:       meta.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("plusvibeai source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *PlusVibeAI) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	meta, ok := supportedTables[table]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s", table)
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		if err := s.paginateAndSend(ctx, meta.endpoint, table, meta.tableSchema, meta.pagination, meta.responseKey, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

type listResponse struct {
	Data      json.RawMessage `json:"data"`
	Total     int             `json:"total"`
	PageTrail string          `json:"page_trail"`
}

func indexedMapToSlice(data []byte) ([]map[string]any, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		var item map[string]any
		if err := json.Unmarshal(raw[k], &item); err != nil {
			return nil, fmt.Errorf("failed to unmarshal item at key %q: %w", k, err)
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *PlusVibeAI) paginateAndSend(ctx context.Context, endpoint string, table string, schema []schema.Column, pagination paginationStyle, responseKey string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	skip := 0
	page := 1
	pageTrail := ""
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("workspace_id", s.workspaceID)

		switch pagination {
		case paginationSkip:
			config.Debug("[PLUSVIBEAI] Fetching %s skip=%d limit=%d", table, skip, pageSize)
			req = req.SetQueryParam("limit", strconv.Itoa(pageSize)).SetQueryParam("skip", strconv.Itoa(skip))
		case paginationPage:
			config.Debug("[PLUSVIBEAI] Fetching %s page=%d limit=%d", table, page, pageSize)
			req = req.SetQueryParam("limit", strconv.Itoa(pageSize)).SetQueryParam("page", strconv.Itoa(page))
		case paginationToken:
			config.Debug("[PLUSVIBEAI] Fetching %s page_trail=%q", table, pageTrail)
			if pageTrail != "" {
				req = req.SetQueryParam("page_trail", pageTrail)
			}
		case paginationNone:
			config.Debug("[PLUSVIBEAI] Fetching %s (single request)", table)
		}

		httpResp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", table, err)
		}
		if !httpResp.IsSuccess() {
			return fmt.Errorf("failed to fetch %s: status %d: %s", table, httpResp.StatusCode(), httpResp.String())
		}

		body := httpResp.Body()

		var resp listResponse
		_ = json.Unmarshal(body, &resp)

		var items []map[string]any
		if responseKey != "" {
			var wrapper map[string]json.RawMessage
			if err := json.Unmarshal(body, &wrapper); err != nil {
				return fmt.Errorf("failed to unmarshal %s response: %w", table, err)
			}
			raw, ok := wrapper[responseKey]
			if !ok {
				return fmt.Errorf("failed to unmarshal %s response: key %q not found", table, responseKey)
			}
			if err := json.Unmarshal(raw, &items); err != nil {
				return fmt.Errorf("failed to unmarshal %s response items: %w", table, err)
			}
		} else if err := json.Unmarshal(resp.Data, &items); err != nil {
			if err2 := json.Unmarshal(body, &items); err2 != nil {
				// indexed object {"0":{...},"1":{...}}
				if indexed, err3 := indexedMapToSlice(body); err3 == nil {
					items = indexed
				} else {
					// single object response
					var single map[string]any
					if err4 := json.Unmarshal(body, &single); err4 != nil {
						return fmt.Errorf("failed to unmarshal %s response: %w", table, err2)
					}
					items = []map[string]any{single}
				}
			}
		}

		if len(items) == 0 {
			break
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, schema, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build arrow record for %s: %w", table, err)
		}

		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(items)
		config.Debug("[PLUSVIBEAI] Sent %d %s records (total: %d)", len(items), table, totalSent)

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}

		done := false
		switch pagination {
		case paginationNone:
			done = true
		case paginationSkip:
			if len(items) < pageSize {
				done = true
			} else {
				skip += len(items)
			}
		case paginationPage:
			if len(items) < pageSize {
				done = true
			} else {
				page++
			}
		case paginationToken:
			if resp.PageTrail == "" {
				done = true
			} else {
				pageTrail = resp.PageTrail
			}
		}
		if done {
			break
		}
	}

	config.Debug("[PLUSVIBEAI] Finished reading %s: %d total records", table, totalSent)
	return nil
}
