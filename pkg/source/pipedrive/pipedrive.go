package pipedrive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://api.pipedrive.com/v1"
	// Pipedrive Essential plan: 20 requests per 2 seconds (10 req/s). Using 80% = 8 req/s.
	rateLimit      = 8.0
	rateLimitBurst = 5
	maxPageSize    = 500
)

var supportedTables = []string{
	"activities",
	"activity_types",
	"deals",
	"deals_participants",
	"deals_flow",
	"files",
	"filters",
	"leads",
	"notes",
	"organizations",
	"persons",
	"pipelines",
	"products",
	"stages",
	"users",
}

// recentsEntityMap maps table names to the entity name used in the /recents endpoint's `items` param.
// Tables NOT supported by /recents: leads, deals_participants, deals_flow (no mapping),
// activity_types, filters (return 0 results).
var recentsEntityMap = map[string]string{
	"activities":    "activity",
	"deals":         "deal",
	"files":         "file",
	"notes":         "note",
	"organizations": "organization",
	"persons":       "person",
	"pipelines":     "pipeline",
	"products":      "product",
	"stages":        "stage",
	"users":         "user",
}

// tableEntityMap maps table names to entity names for custom field mapping.
var tableEntityMap = map[string]string{
	"activities":    "activity",
	"deals":         "deal",
	"organizations": "organization",
	"persons":       "person",
	"products":      "product",
	"leads":         "deal",
}

// entityFieldsEndpoints maps entity names to their *Fields API endpoints.
var entityFieldsEndpoints = map[string]string{
	"activity":     "/activityFields",
	"deal":         "/dealFields",
	"organization": "/organizationFields",
	"person":       "/personFields",
	"product":      "/productFields",
}

type customFieldInfo struct {
	Name      string
	Options   map[string]string
	FieldType string
}

type PipedriveSource struct {
	apiToken         string
	client           *httpclient.Client
	customFields     map[string]map[string]customFieldInfo // entity -> hash_key -> info
	customFieldsOnce sync.Once
	customFieldsErr  error
}

func NewPipedriveSource() *PipedriveSource {
	return &PipedriveSource{}
}

func (s *PipedriveSource) HandlesIncrementality() bool {
	return true
}

func (s *PipedriveSource) Schemes() []string {
	return []string{"pipedrive"}
}

func (s *PipedriveSource) Connect(ctx context.Context, uri string) error {
	apiToken, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.apiToken = apiToken

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
	)

	config.Debug("[PIPEDRIVE] Connected successfully")
	return nil
}

func (s *PipedriveSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "pipedrive://") {
		return "", fmt.Errorf("invalid pipedrive URI: must start with pipedrive://")
	}

	rest := strings.TrimPrefix(uri, "pipedrive://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_token is required in pipedrive URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse pipedrive URI query: %w", err)
	}

	apiToken := values.Get("api_token")
	if apiToken == "" {
		return "", fmt.Errorf("api_token is required in pipedrive URI")
	}

	return apiToken, nil
}

func (s *PipedriveSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	incrementalKey := "update_time"
	switch tableName {
	case "users":
		incrementalKey = "modified"
	case "leads":
		incrementalKey = "update_time"
	case "deals_participants", "deals_flow":
		incrementalKey = ""
	}

	strategy := config.StrategyMerge
	primaryKeys := []string{"id"}
	if tableName == "deals_flow" {
		strategy = config.StrategyReplace
		primaryKeys = nil
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("pipedrive source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	return slices.Contains(supportedTables, table)
}

func (s *PipedriveSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		// Use /recents endpoint for tables that support it (returns all items including inactive).
		if recentsEntityMap[table] != "" {
			err = s.readRecents(ctx, table, opts, results)
		} else {
			switch table {
			case "activity_types":
				err = s.readActivityTypes(ctx, opts, results)
			case "filters":
				err = s.readFilters(ctx, opts, results)
			case "leads":
				err = s.readLeads(ctx, opts, results)
			case "deals_participants":
				err = s.readDealsParticipants(ctx, opts, results)
			case "deals_flow":
				err = s.readDealsFlow(ctx, opts, results)
			default:
				err = fmt.Errorf("unsupported table: %s", table)
			}
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// ensureCustomFields fetches custom field mappings once and caches them.
func (s *PipedriveSource) ensureCustomFields(ctx context.Context) error {
	s.customFieldsOnce.Do(func() {
		s.customFields, s.customFieldsErr = s.fetchCustomFieldsMapping(ctx)
	})
	return s.customFieldsErr
}

func (s *PipedriveSource) fetchCustomFieldsMapping(ctx context.Context) (map[string]map[string]customFieldInfo, error) {
	mapping := make(map[string]map[string]customFieldInfo)

	for entity, endpoint := range entityFieldsEndpoints {
		resp, err := s.client.R(ctx).
			SetQueryParam("api_token", s.apiToken).
			Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}
		if !resp.IsSuccess() {
			config.Debug("[PIPEDRIVE] %s returned status %d, skipping custom fields for %s", endpoint, resp.StatusCode(), entity)
			continue
		}

		var body pipedriveResponse
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return nil, fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		if !body.Success || len(body.Data) == 0 {
			continue
		}

		entityMapping := make(map[string]customFieldInfo)
		for _, field := range body.Data {
			key, _ := field["key"].(string)
			name, _ := field["name"].(string)
			fieldType, _ := field["field_type"].(string)

			if key == "" || name == "" {
				continue
			}

			editFlag := false
			if ef, ok := field["edit_flag"].(bool); ok {
				editFlag = ef
			}

			isCustom := editFlag
			if !isCustom && (fieldType == "set" || fieldType == "enum") {
				if options, ok := field["options"].([]any); ok && len(options) > 0 {
					if opt, ok := options[0].(map[string]any); ok {
						if _, ok := opt["id"].(json.Number); ok {
							isCustom = true
						}
					}
				}
			}

			if !isCustom {
				continue
			}

			fieldName := name
			if !editFlag {
				// Built-in enum/set fields: resolve option IDs to labels but keep original key name
				fieldName = key
			}

			info := customFieldInfo{
				Name:      fieldName,
				FieldType: fieldType,
				Options:   make(map[string]string),
			}

			if options, ok := field["options"].([]any); ok {
				for _, opt := range options {
					if optMap, ok := opt.(map[string]any); ok {
						var optID string
						if id, ok := optMap["id"].(json.Number); ok {
							optID = id.String()
						} else if id, ok := optMap["id"].(string); ok {
							optID = id
						}
						label, _ := optMap["label"].(string)
						if optID != "" && label != "" {
							info.Options[optID] = label
						}
					}
				}
			}

			entityMapping[key] = info
		}

		if len(entityMapping) > 0 {
			mapping[entity] = entityMapping
			config.Debug("[PIPEDRIVE] loaded %d custom field mappings for %s", len(entityMapping), entity)
		}
	}

	return mapping, nil
}

// applyCustomFields renames hash-key custom fields to human-readable names and resolves enum/set option IDs to labels.
func (s *PipedriveSource) applyCustomFields(items []map[string]any, entity string) {
	if s.customFields == nil {
		return
	}
	entityMapping, ok := s.customFields[entity]
	if !ok {
		return
	}

	for _, item := range items {
		for hashKey, info := range entityMapping {
			val, exists := item[hashKey]
			if !exists {
				continue
			}
			delete(item, hashKey)

			switch info.FieldType {
			case "enum":
				if strVal, ok := val.(string); ok {
					if label, found := info.Options[strVal]; found {
						val = label
					}
				} else if numVal, ok := val.(json.Number); ok {
					if label, found := info.Options[numVal.String()]; found {
						val = label
					}
				}
			case "set":
				if strVal, ok := val.(string); ok {
					ids := strings.Split(strVal, ",")
					labels := make([]string, 0, len(ids))
					for _, id := range ids {
						id = strings.TrimSpace(id)
						if label, found := info.Options[id]; found {
							labels = append(labels, label)
						} else {
							labels = append(labels, id)
						}
					}
					val = strings.Join(labels, ",")
				}
			}

			item[info.Name] = val
		}
	}
}

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

type pipedriveResponse struct {
	Success        bool             `json:"success"`
	Data           []map[string]any `json:"data"`
	AdditionalData *additionalData  `json:"additional_data"`
}

type additionalData struct {
	Pagination *pagination `json:"pagination"`
}

type pagination struct {
	Start                 int  `json:"start"`
	Limit                 int  `json:"limit"`
	MoreItemsInCollection bool `json:"more_items_in_collection"`
	NextStart             int  `json:"next_start"`
}

// recentsResponse represents the /recents endpoint response.
type recentsResponse struct {
	Success        bool            `json:"success"`
	Data           []recentsItem   `json:"data"`
	AdditionalData *additionalData `json:"additional_data"`
}

type recentsItem struct {
	Item string          `json:"item"`
	ID   any             `json:"id"`
	Data json.RawMessage `json:"data"`
}

// paginateAndSend uses v1 API with offset-based pagination, client-side filtering, and custom field mapping.
func (s *PipedriveSource) paginateAndSend(ctx context.Context, endpoint, label, entity string, opts source.ReadOptions, results chan<- source.RecordBatchResult, extraParams map[string]string) error {
	if entity != "" {
		if err := s.ensureCustomFields(ctx); err != nil {
			return fmt.Errorf("failed to load custom fields for %s: %w", label, err)
		}
	}

	start := 0
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("api_token", s.apiToken).
			SetQueryParam("start", strconv.Itoa(start)).
			SetQueryParam("limit", strconv.Itoa(maxPageSize))

		for k, v := range extraParams {
			req.SetQueryParam(k, v)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("pipedrive %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		var body pipedriveResponse
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		if !body.Success {
			return fmt.Errorf("pipedrive %s returned success=false", label)
		}

		if len(body.Data) == 0 {
			break
		}

		items := filterByInterval(body.Data, opts)

		if entity != "" {
			s.applyCustomFields(items, entity)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for %s: %w", label, err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			totalSent += len(items)
			config.Debug("[PIPEDRIVE] %s: sent %d records (total: %d)", label, len(items), totalSent)
		}

		if body.AdditionalData == nil || body.AdditionalData.Pagination == nil || !body.AdditionalData.Pagination.MoreItemsInCollection {
			break
		}

		start = body.AdditionalData.Pagination.NextStart
	}

	config.Debug("[PIPEDRIVE] finished reading %s: %d total records", label, totalSent)
	return nil
}

// readRecents uses the /recents endpoint with since_timestamp for server-side filtering.
func (s *PipedriveSource) readRecents(ctx context.Context, table string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	entity, ok := recentsEntityMap[table]
	if !ok {
		return fmt.Errorf("no recents entity mapping for table: %s", table)
	}

	cfEntity := tableEntityMap[table]
	if cfEntity != "" {
		if err := s.ensureCustomFields(ctx); err != nil {
			return fmt.Errorf("failed to load custom fields for %s: %w", table, err)
		}
	}

	label := fmt.Sprintf("recents/%s", table)
	sinceTimestamp := "2000-01-01 00:00:00"
	if opts.IntervalStart != nil {
		sinceTimestamp = opts.IntervalStart.UTC().Format("2006-01-02 15:04:05")
	}
	start := 0
	totalSent := 0

	config.Debug("[PIPEDRIVE] reading %s via /recents (since_timestamp=%s)", table, sinceTimestamp)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("api_token", s.apiToken).
			SetQueryParam("since_timestamp", sinceTimestamp).
			SetQueryParam("items", entity).
			SetQueryParam("start", strconv.Itoa(start)).
			SetQueryParam("limit", strconv.Itoa(maxPageSize)).
			Get("/recents")
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("pipedrive %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		var body recentsResponse
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		if !body.Success {
			return fmt.Errorf("pipedrive %s returned success=false", label)
		}

		if len(body.Data) == 0 {
			break
		}

		var items []map[string]any
		for _, recent := range body.Data {
			if len(recent.Data) == 0 || string(recent.Data) == "null" {
				continue
			}
			// /recents returns data as a map for most entities, but as an array for users
			var single map[string]any
			if err := json.Unmarshal(recent.Data, &single); err == nil {
				if single != nil {
					items = append(items, single)
				}
				continue
			}
			var multi []map[string]any
			if err := json.Unmarshal(recent.Data, &multi); err == nil {
				items = append(items, multi...)
			}
		}

		if opts.IntervalEnd != nil {
			items = filterByInterval(items, source.ReadOptions{IntervalEnd: opts.IntervalEnd})
		}

		if cfEntity != "" {
			s.applyCustomFields(items, cfEntity)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for %s: %w", label, err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			totalSent += len(items)
			config.Debug("[PIPEDRIVE] %s: sent %d records (total: %d)", label, len(items), totalSent)
		}

		if body.AdditionalData == nil || body.AdditionalData.Pagination == nil || !body.AdditionalData.Pagination.MoreItemsInCollection {
			break
		}

		start = body.AdditionalData.Pagination.NextStart
	}

	config.Debug("[PIPEDRIVE] finished reading %s: %d total records", label, totalSent)
	return nil
}

// incrementalDateFields are the fields checked for incremental filtering, matching ingestr's "update_time|modified" behavior.
var incrementalDateFields = []string{"update_time", "modified"}

func filterByInterval(items []map[string]any, opts source.ReadOptions) []map[string]any {
	if opts.IntervalStart == nil && opts.IntervalEnd == nil {
		return items
	}

	var filtered []map[string]any
	for _, item := range items {
		t, found := extractTime(item)
		if !found {
			filtered = append(filtered, item)
			continue
		}

		if opts.IntervalStart != nil && !t.After(*opts.IntervalStart) {
			continue
		}
		if opts.IntervalEnd != nil && t.After(*opts.IntervalEnd) {
			continue
		}
		filtered = append(filtered, item)
	}

	return filtered
}

// extractTime tries each field in incrementalDateFields and returns the first valid time found.
func extractTime(item map[string]any) (time.Time, bool) {
	for _, field := range incrementalDateFields {
		ts, ok := item[field].(string)
		if !ok {
			continue
		}
		t, err := parsePipedriveTime(ts)
		if err != nil {
			continue
		}
		return t, true
	}
	return time.Time{}, false
}

func parsePipedriveTime(s string) (time.Time, error) {
	// Pipedrive uses "YYYY-MM-DD HH:MM:SS" format
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return time.Parse(time.RFC3339, s)
	}
	return t, nil
}

func (s *PipedriveSource) readActivityTypes(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PIPEDRIVE] reading activity_types")
	return s.paginateAndSend(ctx, "/activityTypes", "activity_types", "", opts, results, nil)
}

func (s *PipedriveSource) readFilters(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PIPEDRIVE] reading filters")
	return s.paginateAndSend(ctx, "/filters", "filters", "", opts, results, nil)
}

// readLeads fetches leads with sort=update_time DESC for early-stop optimization.
// Leads do not support the /recents endpoint, so incremental filtering is done client-side.
// Leads use the "deal" custom fields mapping.
// Since results are sorted newest-first, once a page contains items older than IntervalStart, we stop.
func (s *PipedriveSource) readLeads(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PIPEDRIVE] reading leads")

	if err := s.ensureCustomFields(ctx); err != nil {
		return fmt.Errorf("failed to load custom fields for leads: %w", err)
	}

	start := 0
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("api_token", s.apiToken).
			SetQueryParam("start", strconv.Itoa(start)).
			SetQueryParam("limit", strconv.Itoa(maxPageSize)).
			SetQueryParam("sort", "update_time DESC").
			Get("/leads")
		if err != nil {
			return fmt.Errorf("failed to fetch leads: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("pipedrive leads returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var body pipedriveResponse
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse leads response: %w", err)
		}

		if !body.Success {
			return fmt.Errorf("pipedrive leads returned success=false")
		}

		if len(body.Data) == 0 {
			break
		}

		pageSize := len(body.Data)
		// Filter by IntervalStart first to check for early stop (sorted DESC, older items come later)
		itemsAfterStart := filterByInterval(body.Data, source.ReadOptions{IntervalStart: opts.IntervalStart})
		// Then filter by IntervalEnd
		items := filterByInterval(itemsAfterStart, source.ReadOptions{IntervalEnd: opts.IntervalEnd})
		s.applyCustomFields(items, "deal")

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for leads: %w", err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			totalSent += len(items)
			config.Debug("[PIPEDRIVE] leads: sent %d records (total: %d)", len(items), totalSent)
		}

		// Early stop: sorted DESC, so if items were dropped by IntervalStart, remaining pages are older
		if opts.IntervalStart != nil && len(itemsAfterStart) < pageSize {
			config.Debug("[PIPEDRIVE] leads: early stop — reached items older than interval start")
			break
		}

		if body.AdditionalData == nil || body.AdditionalData.Pagination == nil || !body.AdditionalData.Pagination.MoreItemsInCollection {
			break
		}

		start = body.AdditionalData.Pagination.NextStart
	}

	config.Debug("[PIPEDRIVE] finished reading leads: %d total records", totalSent)
	return nil
}

const dealSubResourceWorkers = 4

func (s *PipedriveSource) readDealsParticipants(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PIPEDRIVE] reading deals_participants")
	return s.readDealSubResource(ctx, "participants", "deals_participants", opts, results)
}

// readDealsFlow fetches deal flow for each deal. Each flow item has {object, timestamp, data}
func (s *PipedriveSource) readDealsFlow(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PIPEDRIVE] reading deals_flow")

	dealIDs, err := s.fetchAllDealIDs(ctx)
	if err != nil {
		return err
	}

	config.Debug("[PIPEDRIVE] fetching deals_flow for %d deals with %d workers", len(dealIDs), dealSubResourceWorkers)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int64, len(dealIDs))
	for _, id := range dealIDs {
		jobs <- id
	}
	close(jobs)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		firstErr  error
		totalSent int
	)

	for range dealSubResourceWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for dealID := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}

				endpoint := fmt.Sprintf("/deals/%d/flow", dealID)
				start := 0
				for {
					resp, err := s.client.R(ctx).
						SetQueryParam("api_token", s.apiToken).
						SetQueryParam("start", strconv.Itoa(start)).
						SetQueryParam("limit", strconv.Itoa(maxPageSize)).
						Get(endpoint)
					if err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = fmt.Errorf("failed to fetch deals_flow for deal %d: %w", dealID, err)
							cancel()
						}
						mu.Unlock()
						return
					}
					if !resp.IsSuccess() {
						mu.Lock()
						if firstErr == nil {
							firstErr = fmt.Errorf("pipedrive deals_flow for deal %d returned status %d: %s", dealID, resp.StatusCode(), resp.String())
							cancel()
						}
						mu.Unlock()
						return
					}

					var body pipedriveResponse
					if err := jsonUseNumber(resp.Body(), &body); err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = fmt.Errorf("failed to parse deals_flow response for deal %d: %w", dealID, err)
							cancel()
						}
						mu.Unlock()
						return
					}

					if !body.Success || len(body.Data) == 0 {
						break
					}

					// Keep flow items as {object, timestamp, data} where data is serialized as JSON string
					var items []map[string]any
					for _, flowItem := range body.Data {
						item := make(map[string]any)

						if obj, ok := flowItem["object"]; ok {
							item["object"] = obj
						}
						if ts, ok := flowItem["timestamp"]; ok {
							item["timestamp"] = ts
						}

						if data, ok := flowItem["data"]; ok {
							dataBytes, err := json.Marshal(data)
							if err == nil {
								item["data"] = string(dataBytes)
							}
						}

						items = append(items, item)
					}

					if len(items) > 0 {
						record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
						if err != nil {
							mu.Lock()
							if firstErr == nil {
								firstErr = fmt.Errorf("failed to build arrow record for deals_flow (deal %d): %w", dealID, err)
								cancel()
							}
							mu.Unlock()
							return
						}

						select {
						case <-ctx.Done():
							return
						case results <- source.RecordBatchResult{Batch: record}:
						}

						mu.Lock()
						totalSent += len(items)
						count := totalSent
						mu.Unlock()
						config.Debug("[PIPEDRIVE] deals_flow: sent %d records for deal %d (total: %d)", len(items), dealID, count)
					}

					if body.AdditionalData == nil || body.AdditionalData.Pagination == nil || !body.AdditionalData.Pagination.MoreItemsInCollection {
						break
					}
					start = body.AdditionalData.Pagination.NextStart
				}
			}
		}()
	}

	wg.Wait()

	config.Debug("[PIPEDRIVE] finished reading deals_flow: %d total records", totalSent)
	return firstErr
}

// readDealSubResource fetches a sub-resource for each deal in parallel using a worker pool.
func (s *PipedriveSource) readDealSubResource(ctx context.Context, subResource, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	dealIDs, err := s.fetchAllDealIDs(ctx)
	if err != nil {
		return err
	}

	config.Debug("[PIPEDRIVE] fetching %s for %d deals with %d workers", label, len(dealIDs), dealSubResourceWorkers)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int64, len(dealIDs))
	for _, id := range dealIDs {
		jobs <- id
	}
	close(jobs)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		firstErr  error
		totalSent int
	)

	for range dealSubResourceWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for dealID := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}

				endpoint := fmt.Sprintf("/deals/%d/%s", dealID, subResource)
				start := 0
				for {
					resp, err := s.client.R(ctx).
						SetQueryParam("api_token", s.apiToken).
						SetQueryParam("start", strconv.Itoa(start)).
						SetQueryParam("limit", strconv.Itoa(maxPageSize)).
						Get(endpoint)
					if err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = fmt.Errorf("failed to fetch %s for deal %d: %w", label, dealID, err)
							cancel()
						}
						mu.Unlock()
						return
					}
					if !resp.IsSuccess() {
						mu.Lock()
						if firstErr == nil {
							firstErr = fmt.Errorf("pipedrive %s for deal %d returned status %d: %s", label, dealID, resp.StatusCode(), resp.String())
							cancel()
						}
						mu.Unlock()
						return
					}

					var body pipedriveResponse
					if err := jsonUseNumber(resp.Body(), &body); err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = fmt.Errorf("failed to parse %s response for deal %d: %w", label, dealID, err)
							cancel()
						}
						mu.Unlock()
						return
					}

					if !body.Success || len(body.Data) == 0 {
						break
					}

					record, err := arrowconv.ItemsToArrowRecordWithSchema(body.Data, nil, opts.ExcludeColumns)
					if err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = fmt.Errorf("failed to build arrow record for %s (deal %d): %w", label, dealID, err)
							cancel()
						}
						mu.Unlock()
						return
					}

					select {
					case <-ctx.Done():
						return
					case results <- source.RecordBatchResult{Batch: record}:
					}

					mu.Lock()
					totalSent += len(body.Data)
					count := totalSent
					mu.Unlock()
					config.Debug("[PIPEDRIVE] %s: sent %d records for deal %d (total: %d)", label, len(body.Data), dealID, count)

					if body.AdditionalData == nil || body.AdditionalData.Pagination == nil || !body.AdditionalData.Pagination.MoreItemsInCollection {
						break
					}
					start = body.AdditionalData.Pagination.NextStart
				}
			}
		}()
	}

	wg.Wait()

	config.Debug("[PIPEDRIVE] finished reading %s: %d total records", label, totalSent)
	return firstErr
}

// fetchAllDealIDs retrieves all deal IDs using offset-based pagination.
func (s *PipedriveSource) fetchAllDealIDs(ctx context.Context) ([]int64, error) {
	var dealIDs []int64
	start := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("api_token", s.apiToken).
			SetQueryParam("start", strconv.Itoa(start)).
			SetQueryParam("limit", strconv.Itoa(maxPageSize)).
			SetQueryParam("status", "all_not_deleted").
			Get("/deals")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch deals for ID listing: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("pipedrive deals returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var body pipedriveResponse
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return nil, fmt.Errorf("failed to parse deals response: %w", err)
		}

		if !body.Success || len(body.Data) == 0 {
			break
		}

		for _, deal := range body.Data {
			if id, ok := deal["id"].(json.Number); ok {
				n, err := id.Int64()
				if err == nil {
					dealIDs = append(dealIDs, n)
				}
			}
		}

		if body.AdditionalData == nil || body.AdditionalData.Pagination == nil || !body.AdditionalData.Pagination.MoreItemsInCollection {
			break
		}

		start = body.AdditionalData.Pagination.NextStart
	}

	return dealIDs, nil
}
