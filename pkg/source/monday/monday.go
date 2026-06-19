package monday

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
)

const (
	graphqlBaseUrl   = "https://api.monday.com/v2"
	maxPageSize      = 100
	defaultBatchSize = 50
	rateLimit        = 10
	rateLimitBurst   = 5
)

var supportedTables = []string{
	"account",
	"account_roles",
	"users",
	"boards",
	"items",
	"workspaces",
	"webhooks",
	"updates",
	"teams",
	"tags",
	"custom_activities",
	"board_columns",
	"board_views",
}

var accountFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: false},
	{Name: "slug", DataType: schema.TypeString, Nullable: false},
	{Name: "tier", DataType: schema.TypeString, Nullable: true},
	{Name: "country_code", DataType: schema.TypeString, Nullable: true},
	{Name: "first_day_of_the_week", DataType: schema.TypeString, Nullable: true},
	{Name: "show_timeline_weekends", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "sign_up_product_kind", DataType: schema.TypeString, Nullable: true},
	{Name: "active_members_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "logo", DataType: schema.TypeString, Nullable: true},
	{Name: "plan_max_users", DataType: schema.TypeInt64, Nullable: true},
	{Name: "plan_period", DataType: schema.TypeString, Nullable: true},
	{Name: "plan_tier", DataType: schema.TypeString, Nullable: true},
	{Name: "plan_version", DataType: schema.TypeInt64, Nullable: true},
}

var accountRolesFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "role_type", DataType: schema.TypeString, Nullable: true},
}

var usersFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "email", DataType: schema.TypeString, Nullable: true},
	{Name: "enabled", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "is_admin", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "is_guest", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "is_pending", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "is_view_only", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "birthday", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "country_code", DataType: schema.TypeString, Nullable: true},
	{Name: "join_date", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "location", DataType: schema.TypeString, Nullable: true},
	{Name: "mobile_phone", DataType: schema.TypeString, Nullable: true},
	{Name: "phone", DataType: schema.TypeString, Nullable: true},
	{Name: "photo_original", DataType: schema.TypeString, Nullable: true},
	{Name: "photo_thumb", DataType: schema.TypeString, Nullable: true},
	{Name: "photo_tiny", DataType: schema.TypeString, Nullable: true},
	{Name: "time_zone_identifier", DataType: schema.TypeString, Nullable: true},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "utc_hours_diff", DataType: schema.TypeInt64, Nullable: true},
	{Name: "current_language", DataType: schema.TypeString, Nullable: true},
	{Name: "account_id", DataType: schema.TypeString, Nullable: true},
}

var boardsFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "state", DataType: schema.TypeString, Nullable: true},
	{Name: "board_kind", DataType: schema.TypeString, Nullable: true},
	{Name: "board_folder_id", DataType: schema.TypeString, Nullable: true},
	{Name: "workspace_id", DataType: schema.TypeString, Nullable: true},
	{Name: "permissions", DataType: schema.TypeString, Nullable: true},
	{Name: "item_terminology", DataType: schema.TypeString, Nullable: true},
	{Name: "items_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "communication", DataType: schema.TypeJSON, Nullable: true},
	{Name: "object_type_unique_key", DataType: schema.TypeString, Nullable: true},
	{Name: "type", DataType: schema.TypeString, Nullable: true},
	{Name: "creator_id", DataType: schema.TypeString, Nullable: true},
	{Name: "owners", DataType: schema.TypeJSON, Nullable: true},
	{Name: "subscribers", DataType: schema.TypeJSON, Nullable: true},
	{Name: "team_owners", DataType: schema.TypeString, Nullable: true},
	{Name: "team_subscribers", DataType: schema.TypeString, Nullable: true},
	{Name: "tags", DataType: schema.TypeString, Nullable: true},
}

var customActivitiesFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "type", DataType: schema.TypeString, Nullable: true},
	{Name: "color", DataType: schema.TypeString, Nullable: true},
	{Name: "icon_id", DataType: schema.TypeString, Nullable: true},
}

var boardColumnsFields = []schema.Column{
	{Name: "board_id", DataType: schema.TypeString, Nullable: false},
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "type", DataType: schema.TypeString, Nullable: true},
	{Name: "archived", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "settings_str", DataType: schema.TypeString, Nullable: true},
	{Name: "width", DataType: schema.TypeInt64, Nullable: true},
}

var boardViewsFields = []schema.Column{
	{Name: "board_id", DataType: schema.TypeString, Nullable: false},
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "type", DataType: schema.TypeString, Nullable: true},
	{Name: "settings_str", DataType: schema.TypeString, Nullable: true},
	{Name: "view_specific_data_str", DataType: schema.TypeString, Nullable: true},
	{Name: "access_level", DataType: schema.TypeString, Nullable: true},
}

var itemsFields = []schema.Column{
	{Name: "board_id", DataType: schema.TypeString, Nullable: false},
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "state", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "creator_id", DataType: schema.TypeString, Nullable: true},
	{Name: "group_id", DataType: schema.TypeString, Nullable: true},
	{Name: "group_title", DataType: schema.TypeString, Nullable: true},
	// Raw cell data: a JSON array of {id, title, text, type, value} per column.
	// Pivot downstream (join board_columns) into named columns.
	{Name: "column_values", DataType: schema.TypeJSON, Nullable: true},
}

var workspacesFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "kind", DataType: schema.TypeString, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "is_default_workspace", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "state", DataType: schema.TypeString, Nullable: true},
	{Name: "account_product_id", DataType: schema.TypeString, Nullable: true},
	{Name: "owners_subscribers", DataType: schema.TypeJSON, Nullable: true},
	{Name: "team_owners_subscribers", DataType: schema.TypeString, Nullable: true},
	{Name: "teams_subscribers", DataType: schema.TypeString, Nullable: true},
	{Name: "users_subscribers", DataType: schema.TypeJSON, Nullable: true},
}

var webhooksFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "event", DataType: schema.TypeString, Nullable: true},
	{Name: "board_id", DataType: schema.TypeString, Nullable: true},
	{Name: "config", DataType: schema.TypeString, Nullable: true},
}

var updatesFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "body", DataType: schema.TypeString, Nullable: true},
	{Name: "text_body", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "edited_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "creator_id", DataType: schema.TypeString, Nullable: true},
	{Name: "creator_name", DataType: schema.TypeString, Nullable: true},
	{Name: "item_id", DataType: schema.TypeString, Nullable: true},
	{Name: "item_name", DataType: schema.TypeString, Nullable: true},
	{Name: "creator", DataType: schema.TypeJSON, Nullable: true},
	{Name: "item", DataType: schema.TypeJSON, Nullable: true},
	{Name: "assets", DataType: schema.TypeString, Nullable: true},
	{Name: "replies", DataType: schema.TypeString, Nullable: true},
	{Name: "likes", DataType: schema.TypeString, Nullable: true},
	{Name: "pinned_to_top", DataType: schema.TypeString, Nullable: true},
	{Name: "viewers", DataType: schema.TypeString, Nullable: true},
}

var teamsFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "picture_url", DataType: schema.TypeString, Nullable: true},
	{Name: "users", DataType: schema.TypeString, Nullable: true},
}

var tagsFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "color", DataType: schema.TypeString, Nullable: true},
}

type MondaySource struct {
	apiToken string
	client   *httpclient.Client
}

func NewMondaySource() *MondaySource {
	return &MondaySource{}
}

func (s *MondaySource) Schemes() []string {
	return []string{"monday"}
}

func (s *MondaySource) HandlesIncrementality() bool {
	return false
}

func (s *MondaySource) Connect(ctx context.Context, uri string) error {
	apiToken, err := ParseMondayUri(uri)
	if err != nil {
		return err
	}

	s.apiToken = apiToken
	s.client = httpclient.New(
		httpclient.WithBaseURL(graphqlBaseUrl),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Authorization", s.apiToken),
		httpclient.WithHeader("Content-Type", "application/json"),
	)

	config.Debug("[MONDAY] Connected successfully")
	return nil
}

func ParseMondayUri(uri string) (string, error) {
	if !strings.HasPrefix(uri, "monday://") {
		return "", fmt.Errorf("invalid monday URI: must start with monday://")
	}

	rest := strings.TrimPrefix(uri, "monday://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_token is required in URI query parameters")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse monday URI query: %w", err)
	}

	apiToken := values.Get("api_token")
	if apiToken == "" {
		return "", fmt.Errorf("api_token query parameter is required")
	}

	return apiToken, nil
}

func (s *MondaySource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *MondaySource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	spec, err := parseMondaySpec(req.Name)
	if err != nil {
		return nil, err
	}
	if !isValidTable(spec.table) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	tableSchema, err := s.getSchema(ctx, spec.table)
	if err != nil {
		return nil, err
	}

	// Default strategy is replace; only board and updates use merge with incremental loading
	incrementalKey := ""
	strategy := config.StrategyReplace

	if spec.table == "boards" || spec.table == "updates" {
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           spec.table,
		TablePrimaryKeys:    tableSchema.PrimaryKeys, // Use primary keys from schema
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, spec, opts)
		},
	}, nil
}

func (s *MondaySource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	var fields []schema.Column
	var primaryKeys []string

	switch table {
	case "account":
		fields = accountFields
		primaryKeys = nil
	case "account_roles":
		fields = accountRolesFields
		primaryKeys = nil
	case "users":
		fields = usersFields
		primaryKeys = nil
	case "boards":
		fields = boardsFields
		primaryKeys = []string{"id"}
	case "items":
		fields = itemsFields
		primaryKeys = []string{"id"}
	case "workspaces":
		fields = workspacesFields
		primaryKeys = nil
	case "webhooks":
		fields = webhooksFields
		primaryKeys = nil
	case "updates":
		fields = updatesFields
		primaryKeys = []string{"id"}
	case "teams":
		fields = teamsFields
		primaryKeys = nil
	case "tags":
		fields = tagsFields
		primaryKeys = nil
	case "custom_activities":
		fields = customActivitiesFields
		primaryKeys = nil
	case "board_columns":
		fields = boardColumnsFields
		primaryKeys = nil
	case "board_views":
		fields = boardViewsFields
		primaryKeys = nil
	default:
		return nil, fmt.Errorf("unsupported table: %s", table)
	}

	return &schema.TableSchema{
		Name:        table,
		Columns:     fields,
		PrimaryKeys: primaryKeys,
	}, nil
}

func (s *MondaySource) read(ctx context.Context, spec mondayReadSpec, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch spec.table {
		case "account":
			err = s.readAccount(ctx, opts, results)
		case "account_roles":
			err = s.readAccountRoles(ctx, opts, results)
		case "users":
			err = s.readUsers(ctx, opts, results)
		case "boards":
			err = s.readBoards(ctx, opts, results, spec.boardIDs)
		case "items":
			err = s.readItems(ctx, opts, results, spec.boardIDs, spec.linked)
		case "workspaces":
			err = s.readWorkspaces(ctx, opts, results)
		case "webhooks":
			err = s.readWebhooks(ctx, opts, results)
		case "updates":
			err = s.readUpdates(ctx, opts, results, spec.boardIDs)
		case "teams":
			err = s.readTeams(ctx, opts, results)
		case "tags":
			err = s.readTags(ctx, opts, results)
		case "custom_activities":
			err = s.readCustomActivities(ctx, opts, results)
		case "board_columns":
			err = s.readBoardColumns(ctx, opts, results, spec.boardIDs)
		case "board_views":
			err = s.readBoardViews(ctx, opts, results, spec.boardIDs)
		default:
			err = fmt.Errorf("unsupported table: %s", spec.table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func parseTableSpec(table string) (string, []string) {
	parts := strings.Split(strings.TrimSpace(table), ":")
	base := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return base, nil
	}
	return base, parts[1:]
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

// mondayReadSpec is the parsed source-table: the base table plus an optional
// board-id scope and the items-only "linked" flag.
type mondayReadSpec struct {
	table    string
	boardIDs []string
	linked   bool
}

// mondayParamKeys are the query parameters recognized by the URL-style
// source-table form (see parseMondaySpec).
var mondayParamKeys = []string{"board_ids", "linked"}

// boardAwareTables accept a board-id scope.
var boardAwareTables = map[string]bool{
	"items":         true,
	"updates":       true,
	"board_columns": true,
	"board_views":   true,
	"boards":        true,
}

// parseMondaySpec parses a source-table string in either form:
//
//	items?board_ids=12345&board_ids=67890&linked=true   (URL-style; preferred)
//	items:12345,67890   /   items:<id>:linked            (legacy colon form)
//
// Board-id scoping is only valid for board-aware tables and the linked flag only
// for items, matching the legacy contract. The query form is selected only when
// the table string carries a parameter block (see tablespec.Split).
func parseMondaySpec(name string) (mondayReadSpec, error) {
	path, params, hasQuery, err := tablespec.Split(name)
	if err != nil {
		return mondayReadSpec{}, err
	}

	if hasQuery {
		if err := tablespec.ValidateKeys(params, mondayParamKeys...); err != nil {
			return mondayReadSpec{}, err
		}
		spec := mondayReadSpec{table: strings.TrimSpace(path)}
		for _, v := range params["board_ids"] {
			spec.boardIDs = append(spec.boardIDs, splitBoardIDs(v)...)
		}
		if params.Has("linked") {
			linked, err := parseLinkedParam(params.Get("linked"))
			if err != nil {
				return mondayReadSpec{}, err
			}
			spec.linked = linked
		}
		if len(spec.boardIDs) > 0 && !boardAwareTables[spec.table] {
			return mondayReadSpec{}, fmt.Errorf("%s table does not accept a board_ids parameter", spec.table)
		}
		if spec.linked && spec.table != "items" {
			return mondayReadSpec{}, fmt.Errorf("linked parameter is only valid for the items table")
		}
		return spec, nil
	}

	// Legacy colon form, preserved exactly.
	base, segs := parseTableSpec(name)
	spec := mondayReadSpec{table: base}
	if len(segs) > 0 {
		if !boardAwareTables[base] {
			return mondayReadSpec{}, fmt.Errorf("%s table must be in the format `%s`", base, base)
		}
		if base == "items" && segs[len(segs)-1] == "linked" {
			spec.linked = true
			segs = segs[:len(segs)-1]
		}
		for _, p := range segs {
			spec.boardIDs = append(spec.boardIDs, splitBoardIDs(p)...)
		}
	}
	return spec, nil
}

// splitBoardIDs splits a comma-separated id segment into trimmed, non-empty ids.
func splitBoardIDs(v string) []string {
	var ids []string
	for _, id := range strings.Split(v, ",") {
		if t := strings.TrimSpace(id); t != "" {
			ids = append(ids, t)
		}
	}
	return ids
}

// parseLinkedParam reads the linked query value; a bare key (empty value) is
// treated as true to keep flag ergonomics.
func parseLinkedParam(val string) (bool, error) {
	val = strings.TrimSpace(val)
	if val == "" {
		return true, nil
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return false, fmt.Errorf("invalid linked parameter %q: expected a boolean (true/false)", val)
	}
	return b, nil
}

// GraphQL types
type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

func (s *MondaySource) executeGraphQL(ctx context.Context, query string, variables map[string]interface{}) (json.RawMessage, error) {
	reqBody := graphQLRequest{
		Query:     query,
		Variables: variables,
	}

	config.Debug("[Monday] Executing GraphQL query")

	var resp graphQLResponse
	httpResp, err := s.client.R(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		SetResult(&resp).
		Post("")
	if err != nil {
		return nil, fmt.Errorf("graphql request failed: %w", err)
	}

	if !httpResp.IsSuccess() {
		return nil, fmt.Errorf("graphql request failed with status %d: %s", httpResp.StatusCode(), httpResp.String())
	}

	if len(resp.Errors) > 0 {
		var errMsgs []string
		for _, e := range resp.Errors {
			errMsgs = append(errMsgs, e.Message)
		}
		return nil, fmt.Errorf("graphql errors: %s", strings.Join(errMsgs, "; "))
	}

	return resp.Data, nil
}

// readAccount reads the account information and sends it as a single record batch
func (s *MondaySource) readAccount(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[Monday] Reading account")

	const query = `
	query {
		account {
			id
			name
			slug
			tier
			country_code
			first_day_of_the_week
			show_timeline_weekends
			sign_up_product_kind
			active_members_count
			logo
			plan {
				max_users
				period
				tier
				version
			}
		}
	}
	`

	data, err := s.executeGraphQL(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to execute account query: %w", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("failed to unmarshal account response: %w", err)
	}

	account, ok := response["account"].(map[string]interface{})
	if !ok || len(account) == 0 {
		return nil
	}

	item := normalizeDictionaries(account)
	record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{item}, accountFields, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build account record: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[Monday] Finished reading account: 1 total record")
	return nil
}

// readAccountRoles reads the account roles information and sends it as a single record batch
func (s *MondaySource) readAccountRoles(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	const query = `
	query {
		account_roles {
			id
			name
			roleType
		}
	}
	`
	return s.readSimpleList(ctx, results, query, "account_roles", accountRolesFields, s.transformAccountRoles, "account roles", opts.ExcludeColumns)
}

func (s *MondaySource) transformAccountRoles(node map[string]interface{}) map[string]interface{} {
	normalized := normalizeDictionaries(node)
	// Map camelCase API fields to the snake_case column names we defined.
	if v, ok := normalized["roleType"]; ok {
		normalized["role_type"] = v
		delete(normalized, "roleType")
	}
	return normalized
}

// paginateGraphQL paginates through Monday.com API results
func (s *MondaySource) paginateGraphQL(ctx context.Context, query string, fieldName string, limit int, extraVariables map[string]interface{}) (<-chan map[string]interface{}, <-chan error) {
	itemsChan := make(chan map[string]interface{}, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(itemsChan)
		defer close(errChan)

		page := 1
		totalItems := 0

		for {
			variables := map[string]interface{}{
				"limit": min(limit, maxPageSize),
				"page":  page,
			}

			for k, v := range extraVariables {
				variables[k] = v
			}

			data, err := s.executeGraphQL(ctx, query, variables)
			if err != nil {
				errChan <- fmt.Errorf("failed to execute paginated query (page %d): %w", page, err)
				return
			}

			var response map[string]interface{}
			if err := json.Unmarshal(data, &response); err != nil {
				errChan <- fmt.Errorf("failed to unmarshal paginated response (page %d): %w", page, err)
				return
			}

			items, ok := response[fieldName].([]interface{})
			if !ok || len(items) == 0 {
				break
			}

			for _, item := range items {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				itemsChan <- itemMap
				totalItems++
			}

			if len(items) < limit {
				break
			}

			page++
		}

		config.Debug("[Monday] Pagination completed: %d total items across %d pages", totalItems, page)
	}()

	return itemsChan, errChan
}

func (s *MondaySource) readPaginatedList(
	ctx context.Context,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	query string,
	fieldName string,
	fields []schema.Column,
	transform func(map[string]interface{}) map[string]interface{},
	logName string,
) error {
	config.Debug("[Monday] Reading %s", logName)

	itemsChan, errChan := s.paginateGraphQL(ctx, query, fieldName, maxPageSize, nil)
	batch := make([]map[string]interface{}, 0, defaultBatchSize)
	totalProcessed := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, fields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build %s record: %w", logName, err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = batch[:0]
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
				continue
			}
			if err != nil {
				return err
			}
		case item, ok := <-itemsChan:
			if !ok {
				if err := flush(); err != nil {
					return err
				}
				config.Debug("[Monday] Finished reading %s: %d total records", logName, totalProcessed)
				return nil
			}
			batch = append(batch, transform(item))
			totalProcessed++
			if len(batch) >= defaultBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func chunkStrings(items []string, size int) [][]string {
	if size <= 0 || len(items) == 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(items)+size-1)/size)
	for start := 0; start < len(items); start += size {
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[start:end])
	}
	return chunks
}

type dateRange struct {
	from string
	to   string
}

func formatDateParam(v interface{}) (string, bool) {
	switch t := v.(type) {
	case time.Time:
		if t.IsZero() {
			return "", false
		}
		return t.Format("2006-01-02"), true
	case *time.Time:
		if t == nil || t.IsZero() {
			return "", false
		}
		return t.Format("2006-01-02"), true
	case string:
		if strings.TrimSpace(t) == "" {
			return "", false
		}
		return strings.TrimSpace(t), true
	default:
		return "", false
	}
}

func parseDateRange(startStr, endStr string) (time.Time, time.Time, bool) {
	start, err := parseTimestampString(startStr)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	end, err := parseTimestampString(endStr)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	return start, end, true
}

func splitDateRanges(start, end time.Time, chunkDays int) []dateRange {
	if chunkDays <= 0 {
		chunkDays = 1
	}
	if start.After(end) {
		return nil
	}
	ranges := make([]dateRange, 0, (int(end.Sub(start).Hours()/24)+chunkDays-1)/chunkDays)
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, chunkDays) {
		chunkEnd := cur.AddDate(0, 0, chunkDays-1)
		if chunkEnd.After(end) {
			chunkEnd = end
		}
		ranges = append(ranges, dateRange{
			from: cur.Format("2006-01-02"),
			to:   chunkEnd.Format("2006-01-02"),
		})
	}
	return ranges
}

func (s *MondaySource) sendUpdatesBatches(
	updates []map[string]interface{},
	results chan<- source.RecordBatchResult,
	excludeColumns []string,
) error {
	batch := make([]map[string]interface{}, 0, defaultBatchSize)
	totalProcessed := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, updatesFields, excludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build updates record: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = batch[:0]
		return nil
	}

	for _, update := range updates {
		batch = append(batch, normalizeDictionaries(update))
		totalProcessed++
		if len(batch) >= defaultBatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	config.Debug("[Monday] Finished reading updates: %d total records", totalProcessed)
	return nil
}

func (s *MondaySource) consumeUpdatesChan(
	ctx context.Context,
	updatesCh <-chan map[string]interface{},
	errCh <-chan error,
	excludeColumns []string,
	results chan<- source.RecordBatchResult,
) error {
	batch := make([]map[string]interface{}, 0, defaultBatchSize)
	totalProcessed := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, updatesFields, excludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build updates record: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = batch[:0]
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil && err != context.Canceled {
				return err
			}
		case err := <-errCh:
			if err != nil {
				return err
			}
		case update, ok := <-updatesCh:
			if !ok {
				if err := flush(); err != nil {
					return err
				}
				config.Debug("[Monday] Finished reading updates: %d total records", totalProcessed)
				return nil
			}
			batch = append(batch, normalizeDictionaries(update))
			totalProcessed++
			if len(batch) >= defaultBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
}

// readUsers reads the users information with pagination and sends it as record batches
func (s *MondaySource) readUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	const query = `
		query ($limit: Int!, $page: Int!) {
		users(limit: $limit, page: $page) {
			id
			name
			email
			enabled
			is_admin
			is_guest
			is_pending
			is_view_only
			created_at
			birthday
			country_code
			join_date
			location
			mobile_phone
			phone
			photo_original
			photo_thumb
			photo_tiny
			time_zone_identifier
			title
			url
			utc_hours_diff
			current_language
			account {
				id
			}
		}
	}
	`
	return s.readPaginatedList(ctx, opts, results, query, "users", usersFields, normalizeDictionaries, "users")
}

// readUpdates reads updates with optional date filtering and sends them as record batches.
func (s *MondaySource) readUpdates(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, boardIDs []string) error {
	config.Debug("[Monday] Reading updates")

	// Board-scoped mode: `updates:<board_id>` pulls only the given boards' item
	// updates (Monday's top-level updates query can't filter by board, so this uses
	// the nested boards->items->updates path) and supplies the item name.
	if len(boardIDs) > 0 {
		return s.readBoardScopedUpdates(ctx, opts, results, boardIDs)
	}

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatchSize
	}

	const query = `
		query ($limit: Int!, $from_date: String, $to_date: String) {
		updates(limit: $limit, from_date: $from_date, to_date: $to_date) {
			id
			body
			text_body
			created_at
			updated_at
			edited_at
			creator_id
			item_id
			creator {
				id
				name
			}
			item {
				id
				name
			}
			assets {
				id
				name
				file_extension
				file_size
				public_url
				url
				url_thumbnail
				created_at
				original_geometry
				uploaded_by {
					id
				}
			}
			replies {
				id
				body
				text_body
				created_at
				updated_at
				creator_id
				creator {
					id
				}
			}
			likes {
				id
			}
			pinned_to_top {
				item_id
			}
			viewers {
				medium
				user_id
				user {
					id
				}
			}
		}
	}
	`

	limit := min(pageSize, maxPageSize)
	startStr, hasStart := formatDateParam(opts.IntervalStart)
	endStr, hasEnd := formatDateParam(opts.IntervalEnd)

	fetchUpdates := func(ctx context.Context, fromDate, toDate string) ([]map[string]interface{}, error) {
		variables := map[string]interface{}{
			"limit": limit,
		}
		if fromDate != "" {
			variables["from_date"] = fromDate
		}
		if toDate != "" {
			variables["to_date"] = toDate
		}

		data, err := s.executeGraphQL(ctx, query, variables)
		if err != nil {
			return nil, fmt.Errorf("failed to execute updates query: %w", err)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("failed to unmarshal updates response: %w", err)
		}

		updatesRaw, ok := response["updates"].([]interface{})
		if !ok || len(updatesRaw) == 0 {
			return nil, nil
		}

		out := make([]map[string]interface{}, 0, len(updatesRaw))
		for _, update := range updatesRaw {
			updateMap, ok := update.(map[string]interface{})
			if !ok {
				continue
			}
			out = append(out, updateMap)
		}
		return out, nil
	}

	// If we don't have both bounds, fall back to a single request (same as Python).
	if !hasStart || !hasEnd {
		updates, err := fetchUpdates(ctx, startStr, endStr)
		if err != nil {
			return err
		}
		if len(updates) == 0 {
			return nil
		}
		return s.sendUpdatesBatches(updates, results, opts.ExcludeColumns)
	}

	startTime, endTime, ok := parseDateRange(startStr, endStr)
	if !ok {
		// Fallback to single request if parsing fails.
		updates, err := fetchUpdates(ctx, startStr, endStr)
		if err != nil {
			return err
		}
		if len(updates) == 0 {
			return nil
		}
		return s.sendUpdatesBatches(updates, results, opts.ExcludeColumns)
	}
	if startTime.After(endTime) {
		return fmt.Errorf("invalid date range: from_date after to_date")
	}

	// Split the date range into chunks and fetch in parallel.
	const updateChunkDays = 7
	ranges := splitDateRanges(startTime, endTime, updateChunkDays)
	if len(ranges) == 0 {
		return nil
	}

	workerCount := opts.Parallelism
	if workerCount <= 0 {
		workerCount = 3
	}
	workerCount = min(workerCount, 5)
	workerCount = min(workerCount, len(ranges))

	ctxFetch, cancel := context.WithCancel(ctx)
	defer cancel()

	rangesCh := make(chan dateRange)
	updatesCh := make(chan map[string]interface{}, defaultBatchSize)
	workerErrCh := make(chan error, 1)

	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for dr := range rangesCh {
			updates, err := fetchUpdates(ctxFetch, dr.from, dr.to)
			if err != nil {
				select {
				case workerErrCh <- err:
				default:
				}
				cancel()
				return
			}
			for _, updateMap := range updates {
				select {
				case <-ctxFetch.Done():
					return
				case updatesCh <- updateMap:
				}
			}
		}
	}

	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go worker()
	}

	go func() {
		defer close(rangesCh)
		for _, dr := range ranges {
			select {
			case <-ctxFetch.Done():
				return
			case rangesCh <- dr:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(updatesCh)
	}()

	return s.consumeUpdatesChan(ctxFetch, updatesCh, workerErrCh, opts.ExcludeColumns, results)
}

// readTeams reads teams in a single request and sends them as record batches.
func (s *MondaySource) readTeams(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	const query = `
		query {
		teams {
			id
			name
			picture_url
			users {
				id
				created_at
				phone
			}
		}
	}
	`
	return s.readSimpleList(ctx, results, query, "teams", teamsFields, normalizeDictionaries, "teams", opts.ExcludeColumns)
}

// readTags reads tags in a single request and sends them as record batches.
func (s *MondaySource) readTags(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	const query = `
		query {
		tags {
			id
			name
			color
		}
	}
	`
	return s.readSimpleList(ctx, results, query, "tags", tagsFields, normalizeDictionaries, "tags", opts.ExcludeColumns)
}

// readCustomActivities reads custom activities in a single request and sends them as record batches.
func (s *MondaySource) readCustomActivities(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	const query = `
		query {
		custom_activity {
			id
			name
			type
			color
			icon_id
		}
	}
	`
	return s.readSimpleList(ctx, results, query, "custom_activity", customActivitiesFields, normalizeDictionaries, "custom activities", opts.ExcludeColumns)
}

func (s *MondaySource) readSimpleList(
	ctx context.Context,
	results chan<- source.RecordBatchResult,
	query string,
	fieldName string,
	fields []schema.Column,
	transform func(map[string]interface{}) map[string]interface{},
	logName string,
	excludeColumns []string,
) error {
	config.Debug("[Monday] Reading %s", logName)

	data, err := s.executeGraphQL(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to execute %s query: %w", logName, err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("failed to unmarshal %s response: %w", logName, err)
	}

	items, ok := response[fieldName].([]interface{})
	if !ok || len(items) == 0 {
		return nil
	}

	batch := make([]map[string]interface{}, 0, defaultBatchSize)
	totalProcessed := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, fields, excludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build %s record: %w", logName, err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = batch[:0]
		return nil
	}

	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		batch = append(batch, transform(itemMap))
		totalProcessed++
		if len(batch) >= defaultBatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	config.Debug("[Monday] Finished reading %s: %d total records", logName, totalProcessed)
	return nil
}

// itemFieldsFragment is the item selection shared by the first-page and
// next-page item queries. group{} and creator{} are flattened by
// normalizeDictionaries into group_id/group_title/creator_id; column_values
// (a list of multi-key objects) is JSON-encoded into the column_values column.
const itemFieldsFragment = `
	id
	name
	state
	created_at
	updated_at
	creator {
		id
	}
	group {
		id
		title
	}
	column_values {
		id
		text
		value
		type
		column {
			title
		}
	}`

func cursorString(v interface{}) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// readItems streams the rows (items) of one or more boards, with cursor-based
// items_page pagination. boardIDs empty -> every board in the account.
// linked -> treat boardIDs as master boards and fan out to their linked sub-boards
// (board name == master item title).
func (s *MondaySource) readItems(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, boardIDs []string, linked bool) error {
	config.Debug("[Monday] Reading items")

	if linked {
		if len(boardIDs) == 0 {
			return fmt.Errorf("items:<master_board_id>:linked requires at least one master board id")
		}
		resolved, err := s.resolveLinkedBoardIDs(ctx, boardIDs)
		if err != nil {
			return err
		}
		config.Debug("[Monday] Resolved %d linked sub-boards from %d master board(s)", len(resolved), len(boardIDs))
		// Include the master board(s) themselves alongside their linked sub-boards,
		// so a single `items:<master>:linked` returns both the master's own rows and
		// every sub-board's rows. Dedup preserves master-first ordering.
		seen := make(map[string]struct{}, len(boardIDs)+len(resolved))
		combined := make([]string, 0, len(boardIDs)+len(resolved))
		for _, id := range boardIDs {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				combined = append(combined, id)
			}
		}
		for _, id := range resolved {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				combined = append(combined, id)
			}
		}
		boardIDs = combined
	} else if len(boardIDs) == 0 {
		ids, err := s.getAllBoardIDs(ctx, opts)
		if err != nil {
			return err
		}
		boardIDs = ids
	}
	if len(boardIDs) == 0 {
		return nil
	}

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	limit := min(pageSize, maxPageSize)

	firstPageQuery := `
		query ($board_id: ID!, $limit: Int!) {
		boards(ids: [$board_id]) {
			items_page(limit: $limit) {
				cursor
				items {` + itemFieldsFragment + `
				}
			}
		}
	}
	`
	nextPageQuery := `
		query ($cursor: String!, $limit: Int!) {
		next_items_page(cursor: $cursor, limit: $limit) {
			cursor
			items {` + itemFieldsFragment + `
			}
		}
	}
	`

	batch := make([]map[string]interface{}, 0, defaultBatchSize)
	totalProcessed := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, itemsFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build items record: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = batch[:0]
		return nil
	}

	emit := func(boardID string, rawItems []interface{}) error {
		for _, raw := range rawItems {
			itemMap, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			itemMap["board_id"] = boardID
			batch = append(batch, normalizeDictionaries(itemMap))
			totalProcessed++
			if len(batch) >= defaultBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, boardID := range boardIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, err := s.executeGraphQL(ctx, firstPageQuery, map[string]interface{}{
			"board_id": boardID,
			"limit":    limit,
		})
		if err != nil {
			return fmt.Errorf("failed to execute items query for board %s: %w", boardID, err)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(data, &response); err != nil {
			return fmt.Errorf("failed to unmarshal items response: %w", err)
		}

		boards, ok := response["boards"].([]interface{})
		if !ok || len(boards) == 0 {
			continue
		}
		boardMap, ok := boards[0].(map[string]interface{})
		if !ok {
			continue
		}
		itemsPage, ok := boardMap["items_page"].(map[string]interface{})
		if !ok {
			continue
		}

		rawItems, _ := itemsPage["items"].([]interface{})
		if err := emit(boardID, rawItems); err != nil {
			return err
		}

		cursor := cursorString(itemsPage["cursor"])
		for cursor != "" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			data, err := s.executeGraphQL(ctx, nextPageQuery, map[string]interface{}{
				"cursor": cursor,
				"limit":  limit,
			})
			if err != nil {
				return fmt.Errorf("failed to execute next_items_page for board %s: %w", boardID, err)
			}

			var nextResp map[string]interface{}
			if err := json.Unmarshal(data, &nextResp); err != nil {
				return fmt.Errorf("failed to unmarshal next_items_page response: %w", err)
			}

			nextPage, ok := nextResp["next_items_page"].(map[string]interface{})
			if !ok {
				break
			}
			rawItems, _ := nextPage["items"].([]interface{})
			if err := emit(boardID, rawItems); err != nil {
				return err
			}
			cursor = cursorString(nextPage["cursor"])
		}
	}

	if err := flush(); err != nil {
		return err
	}
	config.Debug("[Monday] Finished reading items: %d total records", totalProcessed)
	return nil
}

// readBoardScopedUpdates streams one row per update on each board's items (with the
// item name), cursor-paginated. Used when `updates` is scoped to specific boards;
// Monday's top-level updates query cannot filter by board, so it walks
// boards -> items_page -> updates instead.
func (s *MondaySource) readBoardScopedUpdates(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, boardIDs []string) error {
	config.Debug("[Monday] Reading board-scoped updates for %d board(s)", len(boardIDs))

	const itemUpdates = `
				id
				name
				updates(limit: 100) {
					id
					body
					text_body
					created_at
					updated_at
					creator { id name }
				}`
	firstQuery := `
		query ($board_id: ID!, $limit: Int!) {
		boards(ids: [$board_id]) {
			items_page(limit: $limit) {
				cursor
				items {` + itemUpdates + `
				}
			}
		}
	}
	`
	nextQuery := `
		query ($cursor: String!, $limit: Int!) {
		next_items_page(cursor: $cursor, limit: $limit) {
			cursor
			items {` + itemUpdates + `
			}
		}
	}
	`

	batch := make([]map[string]interface{}, 0, defaultBatchSize)
	totalProcessed := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, updatesFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build updates record: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = batch[:0]
		return nil
	}

	emit := func(rawItems []interface{}) error {
		for _, raw := range rawItems {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			updates, _ := item["updates"].([]interface{})
			for _, u := range updates {
				upd, ok := u.(map[string]interface{})
				if !ok {
					continue
				}
				// Shape each update like an account-wide updates record, then let
				// normalizeDictionaries flatten creator{id,name}/item{id,name} into
				// creator_id/creator_name/item_id/item_name (matching updatesFields).
				rec := map[string]interface{}{
					"id":         upd["id"],
					"body":       upd["body"],
					"text_body":  upd["text_body"],
					"created_at": upd["created_at"],
					"updated_at": upd["updated_at"],
					"creator":    upd["creator"],
					"item":       map[string]interface{}{"id": item["id"], "name": item["name"]},
				}
				batch = append(batch, normalizeDictionaries(rec))
				totalProcessed++
				if len(batch) >= defaultBatchSize {
					if err := flush(); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}

	for _, boardID := range boardIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, err := s.executeGraphQL(ctx, firstQuery, map[string]interface{}{"board_id": boardID, "limit": maxPageSize})
		if err != nil {
			return fmt.Errorf("failed to execute board-scoped updates query for board %s: %w", boardID, err)
		}
		var response map[string]interface{}
		if err := json.Unmarshal(data, &response); err != nil {
			return fmt.Errorf("failed to unmarshal board-scoped updates response: %w", err)
		}
		boards, ok := response["boards"].([]interface{})
		if !ok || len(boards) == 0 {
			continue
		}
		boardMap, ok := boards[0].(map[string]interface{})
		if !ok {
			continue
		}
		itemsPage, ok := boardMap["items_page"].(map[string]interface{})
		if !ok {
			continue
		}
		rawItems, _ := itemsPage["items"].([]interface{})
		if err := emit(rawItems); err != nil {
			return err
		}
		cursor := cursorString(itemsPage["cursor"])
		for cursor != "" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			data, err := s.executeGraphQL(ctx, nextQuery, map[string]interface{}{"cursor": cursor, "limit": maxPageSize})
			if err != nil {
				return fmt.Errorf("failed to execute next_items_page (board_updates) for board %s: %w", boardID, err)
			}
			var nextResp map[string]interface{}
			if err := json.Unmarshal(data, &nextResp); err != nil {
				return fmt.Errorf("failed to unmarshal next_items_page (board_updates) response: %w", err)
			}
			nextPage, ok := nextResp["next_items_page"].(map[string]interface{})
			if !ok {
				break
			}
			rawItems, _ := nextPage["items"].([]interface{})
			if err := emit(rawItems); err != nil {
				return err
			}
			cursor = cursorString(nextPage["cursor"])
		}
	}

	if err := flush(); err != nil {
		return err
	}
	config.Debug("[Monday] Finished reading board-scoped updates: %d total records", totalProcessed)
	return nil
}

// resolveLinkedBoardIDs maps each master board's item titles to boards of the same
// name (board name == master item title) and returns the deduped set of linked
// sub-board ids.
func (s *MondaySource) resolveLinkedBoardIDs(ctx context.Context, masterIDs []string) ([]string, error) {
	nameToID, err := s.getBoardNameToID(ctx)
	if err != nil {
		return nil, err
	}

	linked := make(map[string]struct{})
	for _, master := range masterIDs {
		names, err := s.getBoardItemNames(ctx, master)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if id, ok := nameToID[name]; ok {
				linked[id] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(linked))
	for id := range linked {
		out = append(out, id)
	}
	return out, nil
}

// getBoardNameToID returns a board name -> id map for every board in the account.
func (s *MondaySource) getBoardNameToID(ctx context.Context) (map[string]string, error) {
	const query = `query ($limit: Int!, $page: Int!) { boards(limit: $limit, page: $page) { id name } }`
	nameToID := make(map[string]string)
	itemsChan, errChan := s.paginateGraphQL(ctx, query, "boards", maxPageSize, nil)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
				continue
			}
			if err != nil {
				return nil, err
			}
		case board, ok := <-itemsChan:
			if !ok {
				return nameToID, nil
			}
			name, _ := board["name"].(string)
			if id := board["id"]; name != "" && id != nil {
				nameToID[name] = fmt.Sprint(id)
			}
		}
	}
}

// getBoardItemNames returns the names of every item on a board (cursor-paginated).
func (s *MondaySource) getBoardItemNames(ctx context.Context, boardID string) ([]string, error) {
	const firstQuery = `
		query ($board_id: ID!, $limit: Int!) {
		boards(ids: [$board_id]) {
			items_page(limit: $limit) {
				cursor
				items { name }
			}
		}
	}
	`
	const nextQuery = `
		query ($cursor: String!, $limit: Int!) {
		next_items_page(cursor: $cursor, limit: $limit) {
			cursor
			items { name }
		}
	}
	`

	var names []string
	collect := func(rawItems []interface{}) {
		for _, raw := range rawItems {
			if m, ok := raw.(map[string]interface{}); ok {
				if name, ok := m["name"].(string); ok && name != "" {
					names = append(names, name)
				}
			}
		}
	}

	data, err := s.executeGraphQL(ctx, firstQuery, map[string]interface{}{"board_id": boardID, "limit": maxPageSize})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch items for master board %s: %w", boardID, err)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	boards, ok := resp["boards"].([]interface{})
	if !ok || len(boards) == 0 {
		return names, nil
	}
	boardMap, _ := boards[0].(map[string]interface{})
	itemsPage, ok := boardMap["items_page"].(map[string]interface{})
	if !ok {
		return names, nil
	}
	rawItems, _ := itemsPage["items"].([]interface{})
	collect(rawItems)

	cursor := cursorString(itemsPage["cursor"])
	for cursor != "" {
		data, err := s.executeGraphQL(ctx, nextQuery, map[string]interface{}{"cursor": cursor, "limit": maxPageSize})
		if err != nil {
			return nil, err
		}
		var nresp map[string]interface{}
		if err := json.Unmarshal(data, &nresp); err != nil {
			return nil, err
		}
		nextPage, ok := nresp["next_items_page"].(map[string]interface{})
		if !ok {
			break
		}
		rawItems, _ := nextPage["items"].([]interface{})
		collect(rawItems)
		cursor = cursorString(nextPage["cursor"])
	}
	return names, nil
}

func (s *MondaySource) readBoardColumns(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, boardIDs []string) error {
	const query = `
		query ($board_ids: [ID!]) {
		boards(ids: $board_ids) {
			id
			columns {
				id
				title
				type
				archived
				description
				settings_str
				width
			}
		}
	}
	`
	config.Debug("[Monday] Reading board columns")

	if len(boardIDs) == 0 {
		ids, err := s.getAllBoardIDs(ctx, opts)
		if err != nil {
			return err
		}
		boardIDs = ids
	}
	if len(boardIDs) == 0 {
		return nil
	}

	batch := make([]map[string]interface{}, 0, defaultBatchSize)
	totalProcessed := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, boardColumnsFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build board columns record: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = batch[:0]
		return nil
	}

	for _, boardID := range boardIDs {
		data, err := s.executeGraphQL(ctx, query, map[string]interface{}{"board_ids": []string{boardID}})
		if err != nil {
			return fmt.Errorf("failed to execute board columns query for board %s: %w", boardID, err)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(data, &response); err != nil {
			return fmt.Errorf("failed to unmarshal board columns response: %w", err)
		}

		boards, ok := response["boards"].([]interface{})
		if !ok || len(boards) == 0 {
			continue
		}

		for _, board := range boards {
			boardMap, ok := board.(map[string]interface{})
			if !ok {
				continue
			}
			cols, ok := boardMap["columns"].([]interface{})
			if !ok || len(cols) == 0 {
				continue
			}
			for _, col := range cols {
				colMap, ok := col.(map[string]interface{})
				if !ok {
					continue
				}
				row := map[string]interface{}{
					"board_id":     boardID,
					"title":        colMap["title"],
					"type":         colMap["type"],
					"archived":     colMap["archived"],
					"description":  colMap["description"],
					"settings_str": colMap["settings_str"],
					"width":        colMap["width"],
				}
				if v, ok := colMap["id"]; ok && v != nil {
					row["id"] = fmt.Sprint(v)
				}
				batch = append(batch, row)
				totalProcessed++
				if len(batch) >= defaultBatchSize {
					if err := flush(); err != nil {
						return err
					}
				}
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	config.Debug("[Monday] Finished reading board columns: %d total records", totalProcessed)
	return nil
}

func (s *MondaySource) readBoardViews(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, boardIDs []string) error {
	const query = `
		query ($board_ids: [ID!]) {
		boards(ids: $board_ids) {
			id
			views {
				id
				name
				type
				settings_str
				view_specific_data_str
				access_level
			}
		}
	}
	`
	config.Debug("[Monday] Reading board views")

	if len(boardIDs) == 0 {
		ids, err := s.getAllBoardIDs(ctx, opts)
		if err != nil {
			return err
		}
		boardIDs = ids
	}
	if len(boardIDs) == 0 {
		return nil
	}

	batch := make([]map[string]interface{}, 0, defaultBatchSize)
	totalProcessed := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, boardViewsFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build board views record: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = batch[:0]
		return nil
	}

	for _, boardID := range boardIDs {
		data, err := s.executeGraphQL(ctx, query, map[string]interface{}{"board_ids": []string{boardID}})
		if err != nil {
			return fmt.Errorf("failed to execute board views query for board %s: %w", boardID, err)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(data, &response); err != nil {
			return fmt.Errorf("failed to unmarshal board views response: %w", err)
		}

		boards, ok := response["boards"].([]interface{})
		if !ok || len(boards) == 0 {
			continue
		}

		for _, board := range boards {
			boardMap, ok := board.(map[string]interface{})
			if !ok {
				continue
			}
			views, ok := boardMap["views"].([]interface{})
			if !ok || len(views) == 0 {
				continue
			}
			for _, view := range views {
				viewMap, ok := view.(map[string]interface{})
				if !ok {
					continue
				}
				row := map[string]interface{}{
					"board_id":               boardID,
					"name":                   viewMap["name"],
					"type":                   viewMap["type"],
					"settings_str":           viewMap["settings_str"],
					"view_specific_data_str": viewMap["view_specific_data_str"],
					"access_level":           viewMap["access_level"],
				}
				if v, ok := viewMap["id"]; ok && v != nil {
					row["id"] = fmt.Sprint(v)
				}
				batch = append(batch, row)
				totalProcessed++
				if len(batch) >= defaultBatchSize {
					if err := flush(); err != nil {
						return err
					}
				}
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	config.Debug("[Monday] Finished reading board views: %d total records", totalProcessed)
	return nil
}

func (s *MondaySource) getAllBoardIDs(ctx context.Context, opts source.ReadOptions) ([]string, error) {
	const boardsQuery = `
		query ($limit: Int!, $page: Int!) {
		boards(limit: $limit, page: $page) {
			id
		}
	}
	`

	ids := make(map[string]struct{})
	itemsChan, errChan := s.paginateGraphQL(ctx, boardsQuery, "boards", maxPageSize, nil)

collectLoop:
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
				continue
			}
			if err != nil {
				return nil, err
			}
		case item, ok := <-itemsChan:
			if !ok {
				break collectLoop
			}
			if id, ok := item["id"]; ok && id != nil {
				ids[fmt.Sprint(id)] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out, nil
}

// readBoards reads the boards information with pagination and sends it as record batches
const boardFieldsFragment = `
			id
			name
			description
			state
			board_kind
			board_folder_id
			workspace_id
			permissions
			item_terminology
			items_count
			updated_at
			url
			communication
			object_type_unique_key
			type
			creator {
				id
			}
			owners {
				id
			}
			subscribers {
				id
			}
			team_owners {
				id
			}
			team_subscribers {
				id
			}
			tags {
				id
			}`

func (s *MondaySource) readBoards(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, boardIDs []string) error {
	// Default: paginate all boards in the account.
	if len(boardIDs) == 0 {
		query := `
		query ($limit: Int!, $page: Int!) {
		boards(limit: $limit, page: $page) {` + boardFieldsFragment + `
		}
	}
	`
		return s.readPaginatedList(ctx, opts, results, query, "boards", boardsFields, normalizeDictionaries, "boards")
	}

	// Scoped: only the requested boards.
	config.Debug("[Monday] Reading %d scoped board(s)", len(boardIDs))
	query := `
		query ($board_ids: [ID!]) {
		boards(ids: $board_ids) {` + boardFieldsFragment + `
		}
	}
	`
	data, err := s.executeGraphQL(ctx, query, map[string]interface{}{"board_ids": boardIDs})
	if err != nil {
		return fmt.Errorf("failed to execute scoped boards query: %w", err)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("failed to unmarshal scoped boards response: %w", err)
	}
	boards, ok := response["boards"].([]interface{})
	if !ok || len(boards) == 0 {
		return nil
	}
	batch := make([]map[string]interface{}, 0, len(boards))
	for _, raw := range boards {
		if boardMap, ok := raw.(map[string]interface{}); ok {
			batch = append(batch, normalizeDictionaries(boardMap))
		}
	}
	record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, boardsFields, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build scoped boards record: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

// readWorkspaces reads the workspaces information with pagination and sends it as record batches
// First gets all boards to extract unique workspace IDs, then fetches workspace details.
func (s *MondaySource) readWorkspaces(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[Monday] Reading workspaces")

	const boardsQuery = `
		query ($limit: Int!, $page: Int!) {
		boards(limit: $limit, page: $page) {
			workspace_id
		}
	}
	`

	workspaceIDs := make(map[string]struct{})
	itemsChan, errChan := s.paginateGraphQL(ctx, boardsQuery, "boards", maxPageSize, nil)

collectLoop:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
				continue
			}
			if err != nil {
				return err
			}
		case item, ok := <-itemsChan:
			if !ok {
				break collectLoop
			}
			if wsID, ok := item["workspace_id"]; ok && wsID != nil {
				workspaceIDs[fmt.Sprint(wsID)] = struct{}{}
			}
		}
	}

	if len(workspaceIDs) == 0 {
		config.Debug("[Monday] No workspace IDs found on boards")
		return nil
	}

	ids := make([]string, 0, len(workspaceIDs))
	for id := range workspaceIDs {
		ids = append(ids, id)
	}

	const workspacesQuery = `
		query ($ids: [ID!]) {
		workspaces(ids: $ids) {
			id
			name
			kind
			description
			created_at
			is_default_workspace
			state
			account_product {
				id
			}
			owners_subscribers {
				id
			}
			team_owners_subscribers {
				id
			}
			teams_subscribers {
				id
			}
			users_subscribers {
				id
			}
		}
	}
	`

	// Fetch workspace details in parallel by chunking IDs.
	const workspaceChunkSize = 50
	chunks := chunkStrings(ids, workspaceChunkSize)
	if len(chunks) == 0 {
		return nil
	}

	workerCount := opts.Parallelism
	if workerCount <= 0 {
		workerCount = 3
	}
	workerCount = min(workerCount, 5)
	workerCount = min(workerCount, len(chunks))

	ctxFetch, cancel := context.WithCancel(ctx)
	defer cancel()

	chunksCh := make(chan []string)
	workspacesCh := make(chan map[string]interface{}, defaultBatchSize)
	workerErrCh := make(chan error, 1)

	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for idsChunk := range chunksCh {
			data, err := s.executeGraphQL(ctxFetch, workspacesQuery, map[string]interface{}{"ids": idsChunk})
			if err != nil {
				select {
				case workerErrCh <- fmt.Errorf("failed to execute workspaces query: %w", err):
				default:
				}
				cancel()
				return
			}

			var response map[string]interface{}
			if err := json.Unmarshal(data, &response); err != nil {
				select {
				case workerErrCh <- fmt.Errorf("failed to unmarshal workspaces response: %w", err):
				default:
				}
				cancel()
				return
			}

			workspaces, ok := response["workspaces"].([]interface{})
			if !ok || len(workspaces) == 0 {
				continue
			}

			for _, ws := range workspaces {
				wsMap, ok := ws.(map[string]interface{})
				if !ok {
					continue
				}
				select {
				case <-ctxFetch.Done():
					return
				case workspacesCh <- wsMap:
				}
			}
		}
	}

	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go worker()
	}

	go func() {
		defer close(chunksCh)
		for _, chunk := range chunks {
			select {
			case <-ctxFetch.Done():
				return
			case chunksCh <- chunk:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(workspacesCh)
	}()

	batch := make([]map[string]interface{}, 0, defaultBatchSize)
	totalProcessed := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, workspacesFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build workspaces record: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = batch[:0]
		return nil
	}

	for {
		select {
		case <-ctxFetch.Done():
			if err := ctxFetch.Err(); err != nil && err != context.Canceled {
				return err
			}
		case err := <-workerErrCh:
			if err != nil {
				return err
			}
		case wsMap, ok := <-workspacesCh:
			if !ok {
				if err := flush(); err != nil {
					return err
				}
				config.Debug("[Monday] Finished reading workspaces: %d total records", totalProcessed)
				return nil
			}

			batch = append(batch, s.transformWorkspaces(wsMap))
			totalProcessed++
			if len(batch) >= defaultBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
}

func (s *MondaySource) transformWorkspaces(node map[string]interface{}) map[string]interface{} {
	normalized := normalizeDictionaries(node)
	// Preserve settings as JSON instead of flattening to settings_icon_* keys.
	if v, ok := node["settings"]; ok {
		normalized["settings"] = v
	}
	return normalized
}

// readWebhooks reads webhooks for all boards by first collecting board IDs.
// First gets all boards to extract board IDs, then fetches webhooks for each board.
func (s *MondaySource) readWebhooks(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[Monday] Reading webhooks")

	const boardsQuery = `
		query ($limit: Int!, $page: Int!) {
		boards(limit: $limit, page: $page) {
			id
		}
	}
	`

	boardIDs := make([]string, 0, defaultBatchSize)
	itemsChan, errChan := s.paginateGraphQL(ctx, boardsQuery, "boards", maxPageSize, nil)

collectLoop:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
				continue
			}
			if err != nil {
				return err
			}
		case item, ok := <-itemsChan:
			if !ok {
				break collectLoop
			}
			if id, ok := item["id"]; ok && id != nil {
				boardIDs = append(boardIDs, fmt.Sprint(id))
			}
		}
	}

	if len(boardIDs) == 0 {
		config.Debug("[Monday] No board IDs found for webhooks")
		return nil
	}

	const webhooksQuery = `
		query ($board_id: ID!) {
		webhooks(board_id: $board_id) {
			id
			event
			board_id
			config
		}
	}
	`

	workerCount := opts.Parallelism
	if workerCount <= 0 {
		workerCount = 3
	}
	workerCount = min(workerCount, 5)
	workerCount = min(workerCount, len(boardIDs))

	ctxFetch, cancel := context.WithCancel(ctx)
	defer cancel()

	boardsCh := make(chan string)
	webhooksCh := make(chan map[string]interface{}, defaultBatchSize)
	workerErrCh := make(chan error, 1)

	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for boardID := range boardsCh {
			data, err := s.executeGraphQL(ctxFetch, webhooksQuery, map[string]interface{}{"board_id": boardID})
			if err != nil {
				select {
				case workerErrCh <- fmt.Errorf("failed to execute webhooks query for board %s: %w", boardID, err):
				default:
				}
				cancel()
				return
			}

			var response map[string]interface{}
			if err := json.Unmarshal(data, &response); err != nil {
				select {
				case workerErrCh <- fmt.Errorf("failed to unmarshal webhooks response: %w", err):
				default:
				}
				cancel()
				return
			}

			webhooks, ok := response["webhooks"].([]interface{})
			if !ok || len(webhooks) == 0 {
				continue
			}

			for _, webhook := range webhooks {
				webhookMap, ok := webhook.(map[string]interface{})
				if !ok {
					continue
				}
				select {
				case <-ctxFetch.Done():
					return
				case webhooksCh <- webhookMap:
				}
			}
		}
	}

	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go worker()
	}

	go func() {
		defer close(boardsCh)
		for _, id := range boardIDs {
			select {
			case <-ctxFetch.Done():
				return
			case boardsCh <- id:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(webhooksCh)
	}()

	batch := make([]map[string]interface{}, 0, defaultBatchSize)
	totalProcessed := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, webhooksFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build webhooks record: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = batch[:0]
		return nil
	}

	for {
		select {
		case <-ctxFetch.Done():
			if err := ctxFetch.Err(); err != nil && err != context.Canceled {
				return err
			}
		case err := <-workerErrCh:
			if err != nil {
				return err
			}
		case webhookMap, ok := <-webhooksCh:
			if !ok {
				if err := flush(); err != nil {
					return err
				}
				config.Debug("[Monday] Finished reading webhooks: %d total records", totalProcessed)
				return nil
			}

			batch = append(batch, normalizeDictionaries(webhookMap))
			totalProcessed++
			if len(batch) >= defaultBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
}

func normalizeDictionaries(item map[string]interface{}) map[string]interface{} {
	normalized := make(map[string]interface{})

	for key, value := range item {
		if value == nil {
			normalized[key] = nil
			continue
		}

		if valueMap, ok := value.(map[string]interface{}); ok {
			if id, hasID := valueMap["id"]; hasID && len(valueMap) == 1 {
				normalized[key+"_id"] = id
				continue
			}
			for subKey, subValue := range valueMap {
				normalized[key+"_"+subKey] = subValue
			}
			continue
		}

		if valueList, ok := value.([]interface{}); ok {
			if len(valueList) > 0 {
				if firstMap, ok := valueList[0].(map[string]interface{}); ok && len(firstMap) == 1 {
					if _, hasID := firstMap["id"]; hasID {
						ids := make([]interface{}, 0, len(valueList))
						allIDs := true
						for _, elem := range valueList {
							elemMap, ok := elem.(map[string]interface{})
							if !ok || len(elemMap) != 1 {
								allIDs = false
								break
							}
							id, hasID := elemMap["id"]
							if !hasID {
								allIDs = false
								break
							}
							ids = append(ids, id)
						}
						if allIDs {
							normalized[key] = ids
							continue
						}
					}
				}
			}

			encoded, err := json.Marshal(valueList)
			if err != nil {
				normalized[key] = nil
			} else {
				normalized[key] = string(encoded)
			}
			continue
		}

		normalized[key] = value
	}

	return normalized
}

func parseTimestampString(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return ts, nil
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts, nil
	}
	// Monday's Date scalar is commonly YYYY-MM-DD.
	if d, err := time.Parse("2006-01-02", s); err == nil {
		return d.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp: %s", s)
}
