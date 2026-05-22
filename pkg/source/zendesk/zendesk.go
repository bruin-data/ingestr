package zendesk

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	// Zendesk Support API: 700 req/min on Enterprise, 400 on Professional.
	// Using ~5.3 req/s (~320/min) to stay safely under Professional tier.
	rateLimit      = 5.3
	rateLimitBurst = 5
)

var supportedTables = []string{
	"tickets",
	"ticket_metrics",
	"ticket_metric_events",
	"ticket_forms",
	"users",
	"groups",
	"organizations",
	"brands",
	"sla_policies",
	"activities",
	"automations",
	"targets",
	"calls",
	"calls_incremental",
	"addresses",
	"greetings",
	"phone_numbers",
	"settings",
	"lines",
	"agents_activity",
	"legs_incremental",
	"chats",
	"macros",
	"recipient_addresses",
	"requests",
	"triggers",
	"user_fields",
	"current_queue_activity",
}

type authMethod int

const (
	authAPIToken authMethod = iota
	authOAuth
)

type ZendeskSource struct {
	client     *httpclient.Client
	chatClient *httpclient.Client // separate client for chat API (zopim.com)
}

func NewZendeskSource() *ZendeskSource {
	return &ZendeskSource{}
}

func (s *ZendeskSource) HandlesIncrementality() bool {
	return true
}

func (s *ZendeskSource) Schemes() []string {
	return []string{"zendesk"}
}

func (s *ZendeskSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseZendeskURI(uri)
	if err != nil {
		return err
	}

	baseURL := fmt.Sprintf("https://%s.zendesk.com", creds.subdomain)

	var auth httpclient.Authenticator
	switch creds.authType {
	case authOAuth:
		auth = httpclient.NewBearerAuth(creds.oauthToken)
		config.Debug("[ZENDESK] Using OAuth token auth")
	case authAPIToken:
		auth = httpclient.NewBasicAuth(creds.email+"/token", creds.apiToken)
		config.Debug("[ZENDESK] Using API token auth with email: %s", creds.email)
	}

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(auth),
	)

	// Chat API uses a different base URL (zopim.com) and requires OAuth
	if creds.authType == authOAuth {
		s.chatClient = httpclient.New(
			httpclient.WithBaseURL("https://www.zopim.com"),
			httpclient.WithTimeout(60*time.Second),
			httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
			httpclient.WithDebug(config.DebugMode),
			httpclient.WithAuth(auth),
		)
	}

	config.Debug("[ZENDESK] Connected to subdomain: %s", creds.subdomain)
	return nil
}

func (s *ZendeskSource) Close(ctx context.Context) error {
	if s.chatClient != nil {
		_ = s.chatClient.Close()
	}
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *ZendeskSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	tableConfig := getTableConfig(tableName)

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    tableConfig.primaryKeys,
		TableIncrementalKey: tableConfig.incrementalKey,
		TableStrategy:       tableConfig.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("zendesk source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

type tableConfig struct {
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
}

func getTableConfig(table string) tableConfig {
	switch table {
	case "tickets":
		return tableConfig{primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge}
	case "ticket_metric_events":
		return tableConfig{primaryKeys: []string{"id"}, incrementalKey: "time", strategy: config.StrategyAppend}
	case "calls":
		return tableConfig{strategy: config.StrategyReplace}
	case "calls_incremental":
		return tableConfig{primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge}
	case "legs_incremental":
		return tableConfig{primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge}
	case "chats":
		return tableConfig{primaryKeys: []string{"id"}, incrementalKey: "update_timestamp", strategy: config.StrategyMerge}
	default:
		return tableConfig{strategy: config.StrategyReplace}
	}
}

func (s *ZendeskSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "tickets":
			err = s.readTickets(ctx, opts, results)
		case "ticket_metrics":
			err = s.readTicketMetrics(ctx, opts, results)
		case "ticket_metric_events":
			err = s.readTicketMetricEvents(ctx, opts, results)
		case "ticket_forms":
			err = s.readTicketForms(ctx, opts, results)
		case "users":
			err = s.readUsers(ctx, opts, results)
		case "groups":
			err = s.readGroups(ctx, opts, results)
		case "organizations":
			err = s.readOrganizations(ctx, opts, results)
		case "brands":
			err = s.readBrands(ctx, opts, results)
		case "sla_policies":
			err = s.readSLAPolicies(ctx, opts, results)
		case "activities":
			err = s.readActivities(ctx, opts, results)
		case "automations":
			err = s.readAutomations(ctx, opts, results)
		case "targets":
			err = s.readTargets(ctx, opts, results)
		case "calls":
			err = s.readCalls(ctx, opts, results)
		case "calls_incremental":
			err = s.readCallsIncremental(ctx, opts, results)
		case "addresses":
			err = s.readAddresses(ctx, opts, results)
		case "greetings":
			err = s.readGreetings(ctx, opts, results)
		case "phone_numbers":
			err = s.readPhoneNumbers(ctx, opts, results)
		case "settings":
			err = s.readSettings(ctx, opts, results)
		case "lines":
			err = s.readLines(ctx, opts, results)
		case "agents_activity":
			err = s.readAgentsActivity(ctx, opts, results)
		case "legs_incremental":
			err = s.readLegsIncremental(ctx, opts, results)
		case "chats":
			err = s.readChats(ctx, opts, results)
		case "macros":
			err = s.readMacros(ctx, opts, results)
		case "recipient_addresses":
			err = s.readRecipientAddresses(ctx, opts, results)
		case "requests":
			err = s.readRequests(ctx, opts, results)
		case "triggers":
			err = s.readTriggers(ctx, opts, results)
		case "user_fields":
			err = s.readUserFields(ctx, opts, results)
		case "current_queue_activity":
			err = s.readCurrentQueueActivity(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

type zendeskCredentials struct {
	subdomain  string
	email      string
	apiToken   string
	oauthToken string
	authType   authMethod
}

// parseZendeskURI parses a Zendesk URI supporting two auth methods:
//   - OAuth:     zendesk://:oauth_token@subdomain
//   - API token: zendesk://email:api_token@subdomain
func parseZendeskURI(uri string) (zendeskCredentials, error) {
	if !strings.HasPrefix(uri, "zendesk://") {
		return zendeskCredentials{}, fmt.Errorf("invalid zendesk URI: must start with zendesk://")
	}

	rest := strings.TrimPrefix(uri, "zendesk://")

	atIdx := strings.LastIndex(rest, "@")
	if atIdx < 0 {
		return zendeskCredentials{}, fmt.Errorf("invalid zendesk URI: expected email:token@subdomain or :oauth_token@subdomain")
	}

	userInfo := rest[:atIdx]
	subdomain := rest[atIdx+1:]

	if subdomain == "" {
		return zendeskCredentials{}, fmt.Errorf("subdomain is required in zendesk URI")
	}

	parts := strings.SplitN(userInfo, ":", 2)
	if len(parts) != 2 {
		return zendeskCredentials{}, fmt.Errorf("invalid zendesk URI: expected email:token@subdomain or :oauth_token@subdomain")
	}

	email := parts[0]
	token := parts[1]

	var creds zendeskCredentials
	creds.subdomain = subdomain

	switch {
	case email == "" && token != "":
		creds.oauthToken = token
		creds.authType = authOAuth
	case email != "" && token != "":
		creds.email = email
		creds.apiToken = token
		creds.authType = authAPIToken
	default:
		return zendeskCredentials{}, fmt.Errorf("invalid zendesk credentials: provide :oauth_token@subdomain or email:api_token@subdomain")
	}

	return creds, nil
}

type ticketField struct {
	title   string
	options map[string]string // value -> display name
}

func (s *ZendeskSource) fetchTicketFields(ctx context.Context) (map[string]ticketField, error) {
	fields := make(map[string]ticketField)
	nextURL := "/api/v2/ticket_fields.json"

	for nextURL != "" {
		resp, err := s.client.R(ctx).Get(nextURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch ticket_fields: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("zendesk ticket_fields returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var result map[string]interface{}
		if err := resp.JSON(&result); err != nil {
			return nil, fmt.Errorf("failed to parse ticket_fields response: %w", err)
		}

		rawFields, _ := result["ticket_fields"].([]interface{})
		for _, rf := range rawFields {
			f, ok := rf.(map[string]interface{})
			if !ok {
				continue
			}
			id := fmt.Sprintf("%v", f["id"])
			title, _ := f["title"].(string)
			opts := make(map[string]string)
			if customOpts, ok := f["custom_field_options"].([]interface{}); ok {
				for _, o := range customOpts {
					if opt, ok := o.(map[string]interface{}); ok {
						val, _ := opt["value"].(string)
						name, _ := opt["name"].(string)
						if val != "" {
							opts[val] = name
						}
					}
				}
			}
			fields[id] = ticketField{title: title, options: opts}
		}

		nextURL, _ = result["next_page"].(string)
	}

	config.Debug("[ZENDESK] fetched %d ticket fields", len(fields))
	return fields, nil
}

func pivotCustomFields(ticket map[string]interface{}, fields map[string]ticketField) {
	customFields, ok := ticket["custom_fields"].([]interface{})
	if !ok {
		return
	}

	for _, cf := range customFields {
		entry, ok := cf.(map[string]interface{})
		if !ok {
			continue
		}
		id := fmt.Sprintf("%v", entry["id"])
		field, exists := fields[id]
		if !exists {
			continue
		}
		colName := naming.ToSnakeCase(field.title)
		value := entry["value"]
		if value == nil || len(field.options) == 0 {
			ticket[colName] = value
			continue
		}
		if strVal, ok := value.(string); ok && len(field.options) > 0 {
			if mapped, ok := field.options[strVal]; ok {
				ticket[colName] = mapped
				continue
			}
		}
		ticket[colName] = value
	}

	delete(ticket, "custom_fields")
	delete(ticket, "fields") // duplicate of custom_fields with numeric IDs
}

func (s *ZendeskSource) readTickets(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading tickets")

	fields, err := s.fetchTicketFields(ctx)
	if err != nil {
		return fmt.Errorf("failed to load ticket fields: %w", err)
	}

	transform := func(row map[string]interface{}) bool {
		pivotCustomFields(row, fields)
		return true
	}

	return s.readIncremental(ctx, "/api/v2/incremental/tickets/cursor.json", "tickets", opts, results, transform)
}

// readIncremental handles cursor-based incremental endpoints (e.g. tickets, ticket_metric_events).
// First request uses start_time, subsequent requests use after_cursor. Stops when end_of_stream is true.
// An optional transform filters/modifies rows; returning false triggers early stop (end_out_of_range).
func (s *ZendeskSource) readIncremental(ctx context.Context, endpoint, key string, opts source.ReadOptions, results chan<- source.RecordBatchResult, transform func(map[string]interface{}) bool) error {
	startTime := time.Now().Add(-30 * 24 * time.Hour).Unix()
	if opts.IntervalStart != nil {
		startTime = opts.IntervalStart.Unix()
	}

	cursor := ""
	totalProcessed := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx)
		if cursor != "" {
			req.SetQueryParam("cursor", cursor)
		} else {
			req.SetQueryParam("start_time", strconv.FormatInt(startTime, 10))
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", key, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("zendesk API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var result map[string]interface{}
		if err := resp.JSON(&result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", key, err)
		}

		items, ok := result[key].([]interface{})
		if !ok || len(items) == 0 {
			config.Debug("[ZENDESK] no more %s to fetch", key)
			break
		}

		rows := make([]map[string]interface{}, 0, len(items))
		endOutOfRange := false
		for _, item := range items {
			if row, ok := item.(map[string]interface{}); ok {
				if transform != nil && !transform(row) {
					endOutOfRange = true
					continue
				}
				rows = append(rows, row)
			}
		}

		if len(rows) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for %s: %w", key, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalProcessed += len(rows)
		}

		if endOutOfRange {
			config.Debug("[ZENDESK] %s: end date out of range, stopping pagination", key)
			break
		}

		endOfStream, _ := result["end_of_stream"].(bool)
		if endOfStream {
			break
		}

		nextCursor, _ := result["after_cursor"].(string)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	config.Debug("[ZENDESK] finished reading %s: %d total records", key, totalProcessed)
	return nil
}

// paginateAndSend handles cursor-paginated endpoints that return links.next (e.g. users, groups, brands, activities).
// Used for all replace-strategy tables. Follows cursor links until no more pages are returned.
func (s *ZendeskSource) paginateAndSend(ctx context.Context, endpoint, key string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	nextURL := endpoint
	totalProcessed := 0

	for nextURL != "" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).Get(nextURL)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", key, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("zendesk API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var result map[string]interface{}
		if err := resp.JSON(&result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", key, err)
		}

		items, ok := result[key].([]interface{})
		if !ok || len(items) == 0 {
			break
		}

		rows := make([]map[string]interface{}, 0, len(items))
		for _, item := range items {
			if row, ok := item.(map[string]interface{}); ok {
				rows = append(rows, row)
			}
		}

		if len(rows) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for %s: %w", key, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalProcessed += len(rows)
		}

		nextURL = getCursorNextPage(result)
	}

	config.Debug("[ZENDESK] finished reading %s: %d total records", key, totalProcessed)
	return nil
}

// getCursorNextPage extracts the next page URL from a Zendesk cursor-paginated response.
// It checks meta.has_more and returns links.next if there are more pages, falling back to next_page for legacy endpoints.
func getCursorNextPage(result map[string]interface{}) string {
	if meta, ok := result["meta"].(map[string]interface{}); ok {
		if hasMore, ok := meta["has_more"].(bool); ok && hasMore {
			if links, ok := result["links"].(map[string]interface{}); ok {
				if next, ok := links["next"].(string); ok && next != "" {
					return next
				}
			}
			config.Debug("[ZENDESK] has_more is true but links.next is missing, stopping pagination")
		}
		return ""
	}

	if next, ok := result["next_page"].(string); ok {
		return next
	}
	return ""
}

// readTalkIncremental handles Talk incremental endpoints that use start_time + next_page pagination.
// Pagination stops when count == 0. Used for calls and legs incremental exports.
func (s *ZendeskSource) readTalkIncremental(ctx context.Context, endpoint, key string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	startTime := time.Now().Add(-30 * 24 * time.Hour).Unix()
	if opts.IntervalStart != nil {
		startTime = opts.IntervalStart.Unix()
	}

	endTime := opts.IntervalEnd

	nextURL := ""
	totalProcessed := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp *httpclient.Response
		var err error

		if nextURL != "" {
			resp, err = s.client.R(ctx).Get(nextURL)
		} else {
			resp, err = s.client.R(ctx).
				SetQueryParam("start_time", strconv.FormatInt(startTime, 10)).
				Get(endpoint)
		}
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", key, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("zendesk API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var result map[string]interface{}
		if err := resp.JSON(&result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", key, err)
		}

		items, ok := result[key].([]interface{})
		if !ok || len(items) == 0 {
			config.Debug("[ZENDESK] no more %s to fetch", key)
			break
		}

		rows := make([]map[string]interface{}, 0, len(items))
		endOutOfRange := false
		for _, item := range items {
			row, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			if endTime != nil {
				if ts, ok := row["updated_at"].(string); ok {
					if t, err := time.Parse(time.RFC3339, ts); err == nil && t.After(*endTime) {
						endOutOfRange = true
						continue
					}
				}
			}

			rows = append(rows, row)
		}

		if len(rows) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for %s: %w", key, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalProcessed += len(rows)
		}

		if endOutOfRange {
			config.Debug("[ZENDESK] %s: end date out of range, stopping pagination", key)
			break
		}

		count, _ := result["count"].(float64)
		if count == 0 {
			break
		}

		// Talk incremental API signals end-of-data when end_time equals start_time
		// (next_page keeps returning the same URL with count > 0).
		if et, ok := result["end_time"].(float64); ok && et == float64(startTime) {
			config.Debug("[ZENDESK] %s: end_time == start_time, no more new data", key)
			break
		}
		if et, ok := result["end_time"].(float64); ok {
			startTime = int64(et)
		}

		next, _ := result["next_page"].(string)
		if next == "" {
			break
		}
		nextURL = next
	}

	config.Debug("[ZENDESK] finished reading %s: %d total records", key, totalProcessed)
	return nil
}

func (s *ZendeskSource) readTicketMetrics(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading ticket_metrics")
	return s.paginateAndSend(ctx, "/api/v2/ticket_metrics.json", "ticket_metrics", opts, results)
}

func (s *ZendeskSource) readTicketMetricEvents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading ticket_metric_events")

	endTime := opts.IntervalEnd

	var transform func(map[string]interface{}) bool
	if endTime != nil {
		endUnix := endTime.Unix()
		transform = func(row map[string]interface{}) bool {
			if ts, ok := row["time"].(string); ok {
				if t, err := time.Parse(time.RFC3339, ts); err == nil && t.Unix() > endUnix {
					return false
				}
			}
			return true
		}
	}

	return s.readIncremental(ctx, "/api/v2/incremental/ticket_metric_events.json", "ticket_metric_events", opts, results, transform)
}

func (s *ZendeskSource) readTicketForms(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading ticket_forms")
	return s.paginateAndSend(ctx, "/api/v2/ticket_forms.json", "ticket_forms", opts, results)
}

func (s *ZendeskSource) readUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading users")
	return s.paginateAndSend(ctx, "/api/v2/users.json", "users", opts, results)
}

func (s *ZendeskSource) readGroups(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading groups")
	return s.paginateAndSend(ctx, "/api/v2/groups.json", "groups", opts, results)
}

func (s *ZendeskSource) readOrganizations(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading organizations")
	return s.paginateAndSend(ctx, "/api/v2/organizations.json", "organizations", opts, results)
}

func (s *ZendeskSource) readBrands(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading brands")
	return s.paginateAndSend(ctx, "/api/v2/brands.json", "brands", opts, results)
}

func (s *ZendeskSource) readSLAPolicies(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading sla_policies")
	return s.paginateAndSend(ctx, "/api/v2/slas/policies.json", "sla_policies", opts, results)
}

func (s *ZendeskSource) readActivities(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading activities")
	return s.paginateAndSend(ctx, "/api/v2/activities.json", "activities", opts, results)
}

func (s *ZendeskSource) readAutomations(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading automations")
	return s.paginateAndSend(ctx, "/api/v2/automations.json", "automations", opts, results)
}

func (s *ZendeskSource) readTargets(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading targets")
	return s.paginateAndSend(ctx, "/api/v2/targets.json", "targets", opts, results)
}

func (s *ZendeskSource) readCalls(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading calls")
	return s.paginateAndSend(ctx, "/api/v2/channels/voice/calls", "calls", opts, results)
}

func (s *ZendeskSource) readCallsIncremental(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading calls_incremental")
	return s.readTalkIncremental(ctx, "/api/v2/channels/voice/stats/incremental/calls.json", "calls", opts, results)
}

func (s *ZendeskSource) readAddresses(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading addresses")
	return s.paginateAndSend(ctx, "/api/v2/channels/voice/addresses", "addresses", opts, results)
}

func (s *ZendeskSource) readGreetings(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading greetings")
	return s.paginateAndSend(ctx, "/api/v2/channels/voice/greetings", "greetings", opts, results)
}

func (s *ZendeskSource) readPhoneNumbers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading phone_numbers")
	return s.paginateAndSend(ctx, "/api/v2/channels/voice/phone_numbers", "phone_numbers", opts, results)
}

// readSettings handles the Talk settings endpoint which returns a single object, not an array.
func (s *ZendeskSource) readSettings(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading settings")

	resp, err := s.client.R(ctx).Get("/api/v2/channels/voice/settings")
	if err != nil {
		return fmt.Errorf("failed to fetch settings: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("zendesk API settings returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var result map[string]interface{}
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse settings response: %w", err)
	}

	settings, ok := result["settings"].(map[string]interface{})
	if !ok {
		config.Debug("[ZENDESK] no settings data found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{settings}, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record for settings: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}

	config.Debug("[ZENDESK] finished reading settings: 1 record")
	return nil
}

func (s *ZendeskSource) readLines(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading lines")
	return s.paginateAndSend(ctx, "/api/v2/channels/voice/lines", "lines", opts, results)
}

func (s *ZendeskSource) readAgentsActivity(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading agents_activity")
	return s.paginateAndSend(ctx, "/api/v2/channels/voice/stats/agents_activity", "agents_activity", opts, results)
}

func (s *ZendeskSource) readMacros(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading macros")
	return s.paginateAndSend(ctx, "/api/v2/macros.json", "macros", opts, results)
}

func (s *ZendeskSource) readRecipientAddresses(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading recipient_addresses")
	return s.paginateAndSend(ctx, "/api/v2/recipient_addresses.json", "recipient_addresses", opts, results)
}

func (s *ZendeskSource) readRequests(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading requests")
	return s.paginateAndSend(ctx, "/api/v2/requests.json", "requests", opts, results)
}

func (s *ZendeskSource) readTriggers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading triggers")
	return s.paginateAndSend(ctx, "/api/v2/triggers.json", "triggers", opts, results)
}

func (s *ZendeskSource) readUserFields(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading user_fields")
	return s.paginateAndSend(ctx, "/api/v2/user_fields.json", "user_fields", opts, results)
}

func (s *ZendeskSource) readCurrentQueueActivity(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading current_queue_activity")

	resp, err := s.client.R(ctx).Get("/api/v2/channels/voice/stats/current_queue_activity")
	if err != nil {
		return fmt.Errorf("failed to fetch current_queue_activity: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("zendesk API current_queue_activity returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var result map[string]interface{}
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse current_queue_activity response: %w", err)
	}

	data, ok := result["current_queue_activity"].(map[string]interface{})
	if !ok {
		config.Debug("[ZENDESK] no current_queue_activity data found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{data}, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record for current_queue_activity: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}

	config.Debug("[ZENDESK] finished reading current_queue_activity: 1 record")
	return nil
}

func (s *ZendeskSource) readLegsIncremental(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading legs_incremental")
	return s.readTalkIncremental(ctx, "/api/v2/channels/voice/stats/incremental/legs.json", "legs", opts, results)
}

// readChats handles the Zendesk Chat incremental endpoint on zopim.com (requires OAuth).
// Uses start_time-based pagination with next_page URLs, stops when count == 0.
// Checks both update_timestamp and updated_timestamp fields for end time filtering.
func (s *ZendeskSource) readChats(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ZENDESK] reading chats")

	if s.chatClient == nil {
		return fmt.Errorf("chats require OAuth authentication; API token auth is not supported for Zendesk Chat")
	}

	startTime := time.Now().Add(-30 * 24 * time.Hour).Unix()
	if opts.IntervalStart != nil {
		startTime = opts.IntervalStart.Unix()
	}

	endTime := opts.IntervalEnd

	nextURL := ""
	totalProcessed := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp *httpclient.Response
		var err error

		if nextURL != "" {
			resp, err = s.chatClient.R(ctx).Get(nextURL)
		} else {
			resp, err = s.chatClient.R(ctx).
				SetQueryParam("start_time", strconv.FormatInt(startTime, 10)).
				SetQueryParam("fields", "chats(*)").
				Get("/api/v2/incremental/chats")
		}
		if err != nil {
			return fmt.Errorf("failed to fetch chats: %w", err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("zendesk chat API returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var result map[string]interface{}
		if err := resp.JSON(&result); err != nil {
			return fmt.Errorf("failed to parse chats response: %w", err)
		}

		items, ok := result["chats"].([]interface{})
		if !ok || len(items) == 0 {
			config.Debug("[ZENDESK] no more chats to fetch")
			break
		}

		rows := make([]map[string]interface{}, 0, len(items))
		endOutOfRange := false
		for _, item := range items {
			row, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			if endTime != nil {
				outOfRange := false
				for _, tsKey := range []string{"update_timestamp", "updated_timestamp"} {
					if ts, ok := row[tsKey].(string); ok {
						if t, err := time.Parse(time.RFC3339, ts); err == nil && t.After(*endTime) {
							outOfRange = true
							endOutOfRange = true
						}
						break
					}
				}
				if outOfRange {
					continue
				}
			}

			rows = append(rows, row)
		}

		if len(rows) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for chats: %w", err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalProcessed += len(rows)
		}

		if endOutOfRange {
			config.Debug("[ZENDESK] chats: end date out of range, stopping pagination")
			break
		}

		count, _ := result["count"].(float64)
		if count == 0 {
			break
		}

		next, _ := result["next_page"].(string)
		if next == "" {
			break
		}
		nextURL = next
	}

	config.Debug("[ZENDESK] finished reading chats: %d total records", totalProcessed)
	return nil
}
