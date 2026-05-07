package gorgias

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const defaultPageSize = 100

type tableMeta struct {
	tableColumns   []schema.Column
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
	endpoint       string
	orderBy        string
}

var supportedTables = map[string]tableMeta{
	"customers":            {tableColumns: customerFields, primaryKeys: []string{"id"}, incrementalKey: "updated_datetime", strategy: config.StrategyMerge, endpoint: "customers", orderBy: "updated_datetime:desc"},
	"tickets":              {tableColumns: ticketFields, primaryKeys: []string{"id"}, incrementalKey: "updated_datetime", strategy: config.StrategyMerge, endpoint: "tickets", orderBy: "updated_datetime:desc"},
	"ticket_messages":      {tableColumns: ticketMessageFields, primaryKeys: []string{"id"}, incrementalKey: "updated_datetime", strategy: config.StrategyMerge, endpoint: "messages", orderBy: "created_datetime:desc"},
	"satisfaction_surveys": {tableColumns: satisfactionSurveyFields, primaryKeys: []string{"id"}, incrementalKey: "updated_datetime", strategy: config.StrategyMerge, endpoint: "satisfaction-surveys", orderBy: "created_datetime:desc"},
}

var customerFields = []schema.Column{
	{Name: "id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "external_id", DataType: schema.TypeString, Nullable: true},
	{Name: "active", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "email", DataType: schema.TypeString, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "firstname", DataType: schema.TypeString, Nullable: true},
	{Name: "lastname", DataType: schema.TypeString, Nullable: true},
	{Name: "language", DataType: schema.TypeString, Nullable: true},
	{Name: "timezone", DataType: schema.TypeString, Nullable: true},
	{Name: "created_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updated_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "meta", DataType: schema.TypeJSON, Nullable: true},
	{Name: "data", DataType: schema.TypeJSON, Nullable: true},
	{Name: "note", DataType: schema.TypeString, Nullable: true},
}

var ticketFields = []schema.Column{
	{Name: "id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "uri", DataType: schema.TypeString, Nullable: true},
	{Name: "external_id", DataType: schema.TypeString, Nullable: true},
	{Name: "language", DataType: schema.TypeString, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "priority", DataType: schema.TypeString, Nullable: true},
	{Name: "channel", DataType: schema.TypeString, Nullable: true},
	{Name: "via", DataType: schema.TypeString, Nullable: true},
	{Name: "from_agent", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "customer", DataType: schema.TypeJSON, Nullable: true},
	{Name: "assignee_user", DataType: schema.TypeJSON, Nullable: true},
	{Name: "assignee_team", DataType: schema.TypeJSON, Nullable: true},
	{Name: "subject", DataType: schema.TypeString, Nullable: true},
	{Name: "excerpt", DataType: schema.TypeString, Nullable: true},
	{Name: "integrations", DataType: schema.TypeJSON, Nullable: true},
	{Name: "meta", DataType: schema.TypeJSON, Nullable: true},
	{Name: "tags", DataType: schema.TypeJSON, Nullable: true},
	{Name: "messages_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "is_unread", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "spam", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "created_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "opened_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "last_received_message_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "last_message_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updated_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "closed_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "snooze_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "trashed_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var ticketMessageFields = []schema.Column{
	{Name: "id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "uri", DataType: schema.TypeString, Nullable: true},
	{Name: "message_id", DataType: schema.TypeString, Nullable: true},
	{Name: "ticket_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "external_id", DataType: schema.TypeString, Nullable: true},
	{Name: "public", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "channel", DataType: schema.TypeString, Nullable: true},
	{Name: "via", DataType: schema.TypeString, Nullable: true},
	{Name: "sender", DataType: schema.TypeJSON, Nullable: true},
	{Name: "integration_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "intents", DataType: schema.TypeJSON, Nullable: true},
	{Name: "rule_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "from_agent", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "receiver", DataType: schema.TypeJSON, Nullable: true},
	{Name: "subject", DataType: schema.TypeString, Nullable: true},
	{Name: "body_text", DataType: schema.TypeString, Nullable: true},
	{Name: "body_html", DataType: schema.TypeString, Nullable: true},
	{Name: "stripped_text", DataType: schema.TypeString, Nullable: true},
	{Name: "stripped_html", DataType: schema.TypeString, Nullable: true},
	{Name: "stripped_signature", DataType: schema.TypeString, Nullable: true},
	{Name: "headers", DataType: schema.TypeJSON, Nullable: true},
	{Name: "attachments", DataType: schema.TypeJSON, Nullable: true},
	{Name: "actions", DataType: schema.TypeJSON, Nullable: true},
	{Name: "macros", DataType: schema.TypeJSON, Nullable: true},
	{Name: "meta", DataType: schema.TypeJSON, Nullable: true},
	{Name: "created_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "sent_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "failed_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "deleted_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "opened_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "last_sending_error", DataType: schema.TypeString, Nullable: true},
	{Name: "is_retriable", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "updated_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var satisfactionSurveyFields = []schema.Column{
	{Name: "id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "body_text", DataType: schema.TypeString, Nullable: true},
	{Name: "created_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "customer_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "meta", DataType: schema.TypeJSON, Nullable: true},
	{Name: "score", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "scored_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "sent_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "should_send_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "ticket_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "uri", DataType: schema.TypeString, Nullable: true},
	{Name: "updated_datetime", DataType: schema.TypeTimestampTZ, Nullable: true},
}

type GorgiasSource struct {
	client  *gonghttp.Client
	domain  string
	apiKey  string
	email   string
	baseURL string
}

func NewGorgiasSource() *GorgiasSource {
	return &GorgiasSource{}
}

func (s *GorgiasSource) Schemes() []string {
	return []string{"gorgias"}
}

func (s *GorgiasSource) Connect(ctx context.Context, uri string) error {
	domain, apiKey, email, err := parseGorgiasURI(uri)
	if err != nil {
		return err
	}

	s.domain = domain
	s.apiKey = apiKey
	s.email = email
	s.baseURL = fmt.Sprintf("https://%s.gorgias.com/api", s.domain)

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(s.baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(2, 4),
		gonghttp.WithRetry(5, 5*time.Second, 60*time.Second),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBasicAuth(s.email, s.apiKey)),
	)

	config.Debug("[GORGIAS] Connected to domain: %s", s.domain)
	return nil
}

func parseGorgiasURI(uri string) (domain, apiKey, email string, err error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse Gorgias URI: %w", err)
	}

	domain = parsed.Host
	if domain == "" {
		return "", "", "", fmt.Errorf("domain is required in the Gorgias URI, expected format: gorgias://<domain>?api_key=<api_key>&email=<email>")
	}
	domain = strings.TrimSuffix(domain, ".gorgias.com")

	params := parsed.Query()

	apiKey = params.Get("api_key")
	if apiKey == "" {
		return "", "", "", fmt.Errorf("api_key is required in the Gorgias URI")
	}

	email = params.Get("email")
	if email == "" {
		return "", "", "", fmt.Errorf("email is required in the Gorgias URI")
	}

	return domain, apiKey, email, nil
}

func (s *GorgiasSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *GorgiasSource) HandlesIncrementality() bool {
	return true
}

func (s *GorgiasSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	meta, exists := supportedTables[req.Name]
	if !exists {
		tables := make([]string, 0, len(supportedTables))
		for t := range supportedTables {
			tables = append(tables, t)
		}
		return nil, fmt.Errorf("unsupported table: %s, supported tables: %v", req.Name, tables)
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    meta.primaryKeys,
		TableIncrementalKey: meta.incrementalKey,
		TableStrategy:       meta.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("gorgias source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
		},
	}, nil
}

func (s *GorgiasSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	meta, exists := supportedTables[table]
	if !exists {
		return nil, fmt.Errorf("unsupported table: %s", table)
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		if err := s.paginateAndSend(ctx, meta, table, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *GorgiasSource) paginateAndSend(ctx context.Context, meta tableMeta, table string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cursor := ""
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("limit", strconv.Itoa(defaultPageSize)).
			SetQueryParam("order_by", meta.orderBy)

		if cursor != "" {
			req = req.SetQueryParam("cursor", cursor)
		}

		config.Debug("[GORGIAS] Fetching %s cursor=%q limit=%d", table, cursor, defaultPageSize)

		resp, err := req.Get(meta.endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", table, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("gorgias API %s returned status %d: %s", meta.endpoint, resp.StatusCode(), resp.String())
		}

		var result struct {
			Data []map[string]any `json:"data"`
			Meta struct {
				NextCursor string `json:"next_cursor"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(resp.Body(), &result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", table, err)
		}

		if len(result.Data) == 0 {
			break
		}

		sortKey := strings.SplitN(meta.orderBy, ":", 2)[0]

		var pageMax time.Time
		filtered := make([]map[string]any, 0, len(result.Data))
		for _, item := range result.Data {
			ensureUpdatedDatetime(item)
			if t := parseTime(item[sortKey]); t != nil && t.After(pageMax) {
				pageMax = *t
			}
			if !inRange(item["updated_datetime"], opts.IntervalStart, opts.IntervalEnd) {
				continue
			}
			filtered = append(filtered, item)
		}

		if opts.IntervalStart != nil && !pageMax.IsZero() && pageMax.Before(*opts.IntervalStart) {
			config.Debug("[GORGIAS] Page max %s (%s) < start %s, stopping", pageMax, sortKey, opts.IntervalStart)
			break
		}

		if len(filtered) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(filtered, meta.tableColumns, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for %s: %w", table, err)
			}

			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(filtered)
			config.Debug("[GORGIAS] Sent %d %s records (total: %d)", len(filtered), table, totalSent)
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}

		if result.Meta.NextCursor == "" {
			break
		}
		cursor = result.Meta.NextCursor
	}

	config.Debug("[GORGIAS] Finished reading %s: %d total records", table, totalSent)
	return nil
}

func parseTime(v any) *time.Time {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

func ensureUpdatedDatetime(item map[string]any) {
	if _, ok := item["updated_datetime"]; ok {
		return
	}
	var best *time.Time
	for field, val := range item {
		if !strings.HasSuffix(field, "_datetime") {
			continue
		}
		if t := parseTime(val); t != nil && (best == nil || t.After(*best)) {
			best = t
		}
	}
	if best != nil {
		item["updated_datetime"] = best.Format(time.RFC3339)
	}
}

func inRange(v any, start, end *time.Time) bool {
	if start == nil && end == nil {
		return true
	}
	t := parseTime(v)
	if t == nil {
		return true
	}
	if start != nil && t.Before(*start) {
		return false
	}
	if end != nil && t.After(*end) {
		return false
	}
	return true
}

var _ source.Source = (*GorgiasSource)(nil)
