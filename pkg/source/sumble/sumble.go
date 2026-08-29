package sumble

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
	"golang.org/x/sync/errgroup"
)

const (
	baseURL        = "https://api.sumble.com/v9"
	maxPageSize    = 100
	maxOffset      = 10000
	maxWorkers     = 5
	requestTimeout = 60 * time.Second

	// Sumble allows 10 requests per second across all endpoints.
	rateLimit      = 8.0
	rateLimitBurst = 5
)

var supportedTables = []string{
	"organization_lists",
	"organization_list_organizations",
	"contact_lists",
	"contact_list_people",
	"signals",
	"priority_signals",
	"signal_configs",
}

type SumbleSource struct {
	apiKey string
	client *httpclient.Client
}

func NewSumbleSource() *SumbleSource {
	return &SumbleSource{}
}

func (s *SumbleSource) HandlesIncrementality() bool {
	return true
}

func (s *SumbleSource) Schemes() []string {
	return []string{"sumble"}
}

func (s *SumbleSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(requestTimeout),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithRetry(5, 2*time.Second, 30*time.Second),
		// The signal endpoints are POST, which resty does not retry by default.
		httpclient.WithAllowNonIdempotentRetry(),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(s.apiKey)),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithHeader("Content-Type", "application/json"),
	)

	config.Debug("[SUMBLE] Connected successfully")
	return nil
}

func (s *SumbleSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(rawURI string) (string, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return "", fmt.Errorf("invalid sumble URI: %w", err)
	}
	if parsed.Scheme != "sumble" {
		return "", fmt.Errorf("invalid sumble URI: must start with sumble://")
	}

	apiKey := parsed.Query().Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key query parameter is required in sumble URI: sumble://?api_key=<key>")
	}
	return apiKey, nil
}

func (s *SumbleSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	spec, err := parseTableSpec(req.Name)
	if err != nil {
		return nil, err
	}

	primaryKeys := []string{"id"}
	incrementalKey := ""
	strategy := config.StrategyReplace

	switch spec.table {
	case "organization_list_organizations":
		primaryKeys = []string{"_ingestr_id"}
	case "contact_list_people":
		primaryKeys = []string{"_ingestr_id"}
	case "signals":
		primaryKeys = []string{"_ingestr_id"}
		incrementalKey = "date"
		strategy = config.StrategyMerge
	case "priority_signals":
		incrementalKey = "date"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           spec.table,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("sumble source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, spec, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	for _, supported := range supportedTables {
		if table == supported {
			return true
		}
	}
	return false
}

type sumbleParams struct {
	ListIDs         []string `mapstructure:"list_ids"`
	OrganizationIDs []string `mapstructure:"organization_ids"`
	PersonIDs       []string `mapstructure:"person_ids"`
	SignalIDs       []string `mapstructure:"signal_ids"`
	TechnologySlugs []string `mapstructure:"technology_slugs"`
	JobFunctions    []string `mapstructure:"job_functions"`
	Priorities      []string `mapstructure:"priorities"`
	AccountListIDs  []string `mapstructure:"account_list_ids"`
	SignalConfigIDs []string `mapstructure:"signal_config_ids"`
	JobPostIDs      []string `mapstructure:"job_post_ids"`
	Types           []string `mapstructure:"types"`
	IsRelevant      string   `mapstructure:"is_relevant"`
}

type sumbleReadSpec struct {
	table   string
	listIDs []int64
	filter  map[string]any
}

// allowedParams lists the table parameters each table accepts. Tables absent
// from the map take no parameters.
var allowedParams = map[string][]string{
	"organization_list_organizations": {"list_ids"},
	"contact_list_people":             {"list_ids"},
	"signals": {
		"organization_ids", "person_ids", "signal_ids", "technology_slugs",
		"job_functions", "priorities", "account_list_ids", "signal_config_ids",
	},
	"priority_signals": {"organization_ids", "person_ids", "signal_ids", "job_post_ids", "is_relevant"},
	"signal_configs":   {"signal_config_ids", "types", "priorities"},
}

func parseTableSpec(raw string) (sumbleReadSpec, error) {
	var params sumbleParams
	path, hasParams, err := tablespec.Parse(raw, &params, tablespec.WithListSeparator(","))
	if err != nil {
		return sumbleReadSpec{}, err
	}

	table := strings.TrimSpace(path)
	if !isValidTable(table) {
		return sumbleReadSpec{}, fmt.Errorf("unsupported table: %s (supported: %s)", table, strings.Join(supportedTables, ", "))
	}

	spec := sumbleReadSpec{table: table}
	if !hasParams {
		return spec, nil
	}
	if err := validateParams(table, raw); err != nil {
		return sumbleReadSpec{}, err
	}

	switch table {
	case "organization_list_organizations", "contact_list_people":
		spec.listIDs, err = parsePositiveIDs("list_ids", params.ListIDs)
	case "signals":
		spec.filter, err = buildSignalsFilter(params)
	case "priority_signals":
		spec.filter, err = buildPrioritySignalsFilter(params)
	case "signal_configs":
		spec.filter, err = buildSignalConfigsFilter(params)
	}
	if err != nil {
		return sumbleReadSpec{}, err
	}
	return spec, nil
}

// validateParams rejects parameters the table does not accept, and parameters
// supplied with an empty value, which decode to a zero value and would
// otherwise be dropped without any filtering taking effect.
func validateParams(table, raw string) error {
	_, params, _, err := tablespec.Split(raw)
	if err != nil {
		return err
	}

	allowed := make(map[string]struct{}, len(allowedParams[table]))
	for _, name := range allowedParams[table] {
		allowed[name] = struct{}{}
	}

	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s table does not accept the %s parameter", table, name)
		}
		for _, value := range params[name] {
			if !hasListValue(value) {
				return fmt.Errorf("%s parameter must not be empty", name)
			}
		}
	}
	return nil
}

// hasListValue reports whether a raw parameter value carries at least one
// element once the list separator is applied; "," alone decodes to an empty
// slice, which would silently drop the filter.
func hasListValue(value string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.TrimSpace(part) != "" {
			return true
		}
	}
	return false
}

func parsePositiveIDs(name string, values []string) ([]int64, error) {
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("%s must contain positive integer IDs, got %q", name, value)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func addIDFilter(filter map[string]any, name string, values []string) error {
	if len(values) == 0 {
		return nil
	}
	ids, err := parsePositiveIDs(name, values)
	if err != nil {
		return err
	}
	filter[name] = ids
	return nil
}

func validatePriorities(priorities []string) error {
	for _, priority := range priorities {
		switch priority {
		case "high", "medium", "low":
		default:
			return fmt.Errorf("priorities must contain high, medium, or low, got %q", priority)
		}
	}
	return nil
}

func buildSignalsFilter(params sumbleParams) (map[string]any, error) {
	filter := make(map[string]any)
	for _, field := range []struct {
		name   string
		values []string
	}{
		{"organization_ids", params.OrganizationIDs},
		{"person_ids", params.PersonIDs},
		{"signal_ids", params.SignalIDs},
		{"account_list_ids", params.AccountListIDs},
		{"signal_config_ids", params.SignalConfigIDs},
	} {
		if err := addIDFilter(filter, field.name, field.values); err != nil {
			return nil, err
		}
	}
	if len(params.TechnologySlugs) > 0 {
		filter["technology_slugs"] = params.TechnologySlugs
	}
	if len(params.JobFunctions) > 0 {
		filter["job_functions"] = params.JobFunctions
	}
	if len(params.Priorities) > 0 {
		if err := validatePriorities(params.Priorities); err != nil {
			return nil, err
		}
		filter["priorities"] = params.Priorities
	}
	return filter, nil
}

func buildPrioritySignalsFilter(params sumbleParams) (map[string]any, error) {
	filter := make(map[string]any)
	for _, field := range []struct {
		name   string
		values []string
	}{
		{"organization_ids", params.OrganizationIDs},
		{"person_ids", params.PersonIDs},
		{"signal_ids", params.SignalIDs},
		{"job_post_ids", params.JobPostIDs},
	} {
		if err := addIDFilter(filter, field.name, field.values); err != nil {
			return nil, err
		}
	}
	if params.IsRelevant != "" {
		isRelevant, err := strconv.ParseBool(params.IsRelevant)
		if err != nil {
			return nil, fmt.Errorf("is_relevant must be true or false, got %q", params.IsRelevant)
		}
		filter["is_relevant"] = isRelevant
	}
	return filter, nil
}

func buildSignalConfigsFilter(params sumbleParams) (map[string]any, error) {
	filter := make(map[string]any)
	if err := addIDFilter(filter, "signal_config_ids", params.SignalConfigIDs); err != nil {
		return nil, err
	}
	if len(params.Types) > 0 {
		filter["types"] = params.Types
	}
	if len(params.Priorities) > 0 {
		if err := validatePriorities(params.Priorities); err != nil {
			return nil, err
		}
		filter["priorities"] = params.Priorities
	}
	if len(params.SignalConfigIDs) > 0 && (len(params.Types) > 0 || len(params.Priorities) > 0) {
		return nil, fmt.Errorf("signal_config_ids cannot be combined with types or priorities")
	}
	return filter, nil
}

// readContext carries the per-read state every reader needs: the caller's
// options, the destination channel, and the row budget shared across the
// parallel list workers.
type readContext struct {
	opts    source.ReadOptions
	limiter *rowLimiter
	results chan<- source.RecordBatchResult
}

func (s *SumbleSource) read(ctx context.Context, spec sumbleReadSpec, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)
	rc := readContext{opts: opts, limiter: newRowLimiter(opts.Limit), results: results}

	go func() {
		defer close(results)

		var err error
		switch spec.table {
		case "organization_lists":
			err = s.readOrganizationLists(ctx, rc)
		case "organization_list_organizations":
			err = s.readOrganizationListOrganizations(ctx, spec, rc)
		case "contact_lists":
			err = s.readContactLists(ctx, rc)
		case "contact_list_people":
			err = s.readContactListPeople(ctx, spec, rc)
		case "signals":
			err = s.readSignals(ctx, spec, rc)
		case "priority_signals":
			err = s.readPrioritySignals(ctx, spec, rc)
		case "signal_configs":
			err = s.readSignalConfigs(ctx, spec, rc)
		default:
			err = fmt.Errorf("unsupported table: %s", spec.table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *SumbleSource) readOrganizationLists(ctx context.Context, rc readContext) error {
	config.Debug("[SUMBLE] reading organization_lists")
	items, err := s.fetchGETItems(ctx, "/organization-lists", "organization_lists", "organization lists", map[string]string{"include_deleted": "true"})
	if err != nil {
		return err
	}
	return sendItems(ctx, items, "organization lists", rc)
}

func (s *SumbleSource) readContactLists(ctx context.Context, rc readContext) error {
	config.Debug("[SUMBLE] reading contact_lists")
	items, err := s.fetchGETItems(ctx, "/contact-lists", "contact_lists", "contact lists", nil)
	if err != nil {
		return err
	}
	return sendItems(ctx, items, "contact lists", rc)
}

func (s *SumbleSource) readOrganizationListOrganizations(ctx context.Context, spec sumbleReadSpec, rc readContext) error {
	config.Debug("[SUMBLE] reading organization_list_organizations")
	listIDs := spec.listIDs
	if len(listIDs) == 0 {
		items, err := s.fetchGETItems(ctx, "/organization-lists", "organization_lists", "organization lists", map[string]string{"include_deleted": "true"})
		if err != nil {
			return err
		}
		listIDs, err = extractIDs(items)
		if err != nil {
			return fmt.Errorf("failed to parse organization list IDs: %w", err)
		}
	}

	return s.readListMembers(ctx, listIDs, listMemberConfig{
		path:          "/organization-lists/%d",
		responseKey:   "organizations",
		label:         "organization list organizations",
		parentIDField: "organization_list_id",
		parentObject:  "organization_list",
		query:         map[string]string{"include_deleted": "true"},
	}, rc)
}

func (s *SumbleSource) readContactListPeople(ctx context.Context, spec sumbleReadSpec, rc readContext) error {
	config.Debug("[SUMBLE] reading contact_list_people")
	listIDs := spec.listIDs
	if len(listIDs) == 0 {
		items, err := s.fetchGETItems(ctx, "/contact-lists", "contact_lists", "contact lists", nil)
		if err != nil {
			return err
		}
		listIDs, err = extractIDs(items)
		if err != nil {
			return fmt.Errorf("failed to parse contact list IDs: %w", err)
		}
	}

	return s.readListMembers(ctx, listIDs, listMemberConfig{
		path:          "/contact-lists/%d",
		responseKey:   "people",
		label:         "contact list people",
		parentIDField: "contact_list_id",
		parentObject:  "contact_list",
	}, rc)
}

type listMemberConfig struct {
	path          string
	responseKey   string
	label         string
	parentIDField string
	parentObject  string
	query         map[string]string
}

func (s *SumbleSource) readListMembers(ctx context.Context, listIDs []int64, cfg listMemberConfig, rc readContext) error {
	workers := rc.opts.Parallelism
	if workers <= 0 || workers > maxWorkers {
		workers = maxWorkers
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(workers)
	for _, listID := range listIDs {
		if groupCtx.Err() != nil || rc.limiter.exhausted() {
			break
		}
		listID := listID
		group.Go(func() error {
			if rc.limiter.exhausted() {
				return nil
			}
			endpoint := fmt.Sprintf(cfg.path, listID)
			body, err := s.fetchGET(groupCtx, endpoint, cfg.label, cfg.query)
			if err != nil {
				return err
			}
			items, err := envelopeItems(body, cfg.responseKey)
			if err != nil {
				return fmt.Errorf("failed to parse %s for list %d: %w", cfg.label, listID, err)
			}

			listInfo, _ := body["list_info"].(map[string]any)
			for _, item := range items {
				item[cfg.parentIDField] = listID
				if listInfo != nil {
					item[cfg.parentObject] = listInfo
				}
			}
			if err := addScopedPrimaryKeys(items, listID); err != nil {
				return fmt.Errorf("failed to key %s for list %d: %w", cfg.label, listID, err)
			}
			return sendItems(groupCtx, items, cfg.label, rc)
		})
	}
	return group.Wait()
}

func (s *SumbleSource) readSignals(ctx context.Context, spec sumbleReadSpec, rc readContext) error {
	config.Debug("[SUMBLE] reading signals")
	return s.paginateAndSend(ctx, paginatedRead{
		endpoint:      "/signals",
		responseKey:   "signals",
		label:         "signals",
		filter:        spec.filter,
		intervalField: "date",
		newestFirst:   true,
		transform:     addSignalPrimaryKeys,
	}, rc)
}

func (s *SumbleSource) readPrioritySignals(ctx context.Context, spec sumbleReadSpec, rc readContext) error {
	config.Debug("[SUMBLE] reading priority_signals")
	// Priority signals are ordered by completion time rather than by date, so
	// pagination cannot stop early on the interval start.
	return s.paginateAndSend(ctx, paginatedRead{
		endpoint:      "/signals/priority",
		responseKey:   "priority_signals",
		label:         "priority signals",
		filter:        spec.filter,
		intervalField: "date",
	}, rc)
}

func (s *SumbleSource) readSignalConfigs(ctx context.Context, spec sumbleReadSpec, rc readContext) error {
	config.Debug("[SUMBLE] reading signal_configs")
	body := requestBody(spec.filter)
	response, err := s.post(ctx, "/signals/configs", "signal configs", body)
	if err != nil {
		return err
	}
	items, err := envelopeItems(response, "signal_configs")
	if err != nil {
		return fmt.Errorf("failed to parse signal configs response: %w", err)
	}
	return sendItems(ctx, items, "signal configs", rc)
}

type itemTransform func([]map[string]any) error

type paginatedRead struct {
	endpoint      string
	responseKey   string
	label         string
	filter        map[string]any
	intervalField string
	// newestFirst marks endpoints Sumble documents as ordering rows by
	// intervalField descending, which lets pagination stop at the interval start
	// instead of paging the endpoint's whole retention window.
	newestFirst bool
	transform   itemTransform
}

func (s *SumbleSource) paginateAndSend(ctx context.Context, read paginatedRead, rc readContext) error {
	pageSize := rc.opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	fetched := 0
	for offset := 0; offset <= maxOffset; offset += pageSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		body := requestBody(read.filter)
		body["limit"] = pageSize
		body["offset"] = offset

		response, err := s.post(ctx, read.endpoint, read.label, body)
		if err != nil {
			return err
		}
		page, err := envelopeItems(response, read.responseKey)
		if err != nil {
			return fmt.Errorf("failed to parse %s response: %w", read.label, err)
		}
		fetched += len(page)

		items := filterItemsByInterval(page, read.intervalField, rc.opts.IntervalStart, rc.opts.IntervalEnd)
		if read.transform != nil {
			if err := read.transform(items); err != nil {
				return fmt.Errorf("failed to transform %s: %w", read.label, err)
			}
		}
		if err := sendItems(ctx, items, read.label, rc); err != nil {
			return err
		}

		if len(page) < pageSize || rc.limiter.exhausted() {
			return nil
		}
		if read.newestFirst && rc.opts.IntervalStart != nil && pageEndsBeforeStart(page, read.intervalField, *rc.opts.IntervalStart) {
			config.Debug("[SUMBLE] reached the interval start after %d %s", fetched, read.label)
			return nil
		}
	}

	output.Warnf("[SUMBLE] WARNING: %s reached the API offset limit after fetching %d records; any older records were not ingested, narrow the extract with table parameters\n", read.label, fetched)
	return nil
}

// pageEndsBeforeStart reports whether every remaining row of a newest-first
// endpoint is older than the interval start. It gives up unless every row on the
// page carries a parseable value, so an unexpected payload never truncates.
func pageEndsBeforeStart(items []map[string]any, field string, start time.Time) bool {
	if field == "" || len(items) == 0 {
		return false
	}
	for _, item := range items {
		timestamp, dateOnly, ok := parseTimestamp(item[field])
		if !ok || !beforeIntervalStart(timestamp, dateOnly, start) {
			return false
		}
	}
	return true
}

func requestBody(filter map[string]any) map[string]any {
	body := make(map[string]any)
	if len(filter) > 0 {
		body["filter"] = filter
	}
	return body
}

func (s *SumbleSource) fetchGETItems(ctx context.Context, endpoint, responseKey, label string, query map[string]string) ([]map[string]any, error) {
	body, err := s.fetchGET(ctx, endpoint, label, query)
	if err != nil {
		return nil, err
	}
	items, err := envelopeItems(body, responseKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", label, err)
	}
	return items, nil
}

func (s *SumbleSource) fetchGET(ctx context.Context, endpoint, label string, query map[string]string) (map[string]any, error) {
	req := s.client.R(ctx)
	if len(query) > 0 {
		req.SetQueryParams(query)
	}
	resp, err := req.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s from %s: %w", label, endpoint, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("sumble API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	var body map[string]any
	if err := jsonUseNumber(resp.Body(), &body); err != nil {
		return nil, fmt.Errorf("failed to decode %s response from %s: %w", label, endpoint, err)
	}
	return body, nil
}

func (s *SumbleSource) post(ctx context.Context, endpoint, label string, body map[string]any) (map[string]any, error) {
	resp, err := s.client.R(ctx).SetBody(body).Post(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s from %s: %w", label, endpoint, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("sumble API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	var response map[string]any
	if err := jsonUseNumber(resp.Body(), &response); err != nil {
		return nil, fmt.Errorf("failed to decode %s response from %s: %w", label, endpoint, err)
	}
	return response, nil
}

func jsonUseNumber(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func envelopeItems(body map[string]any, key string) ([]map[string]any, error) {
	raw, ok := body[key]
	if !ok {
		return nil, fmt.Errorf("response missing %q field", key)
	}
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("response field %q has type %T, expected array", key, raw)
	}

	items := make([]map[string]any, 0, len(values))
	for index, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("response field %q item %d has type %T, expected object", key, index, value)
		}
		items = append(items, item)
	}
	return items, nil
}

func extractIDs(items []map[string]any) ([]int64, error) {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		id, err := int64Value(item["id"])
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func int64Value(value any) (int64, error) {
	switch typed := value.(type) {
	case json.Number:
		id, err := typed.Int64()
		if err != nil || id <= 0 {
			return 0, fmt.Errorf("invalid ID %q", typed)
		}
		return id, nil
	case int64:
		if typed > 0 {
			return typed, nil
		}
	case int:
		if typed > 0 {
			return int64(typed), nil
		}
	case float64:
		if typed > 0 && typed == float64(int64(typed)) {
			return int64(typed), nil
		}
	}
	return 0, fmt.Errorf("invalid ID value %v", value)
}

func sendItems(ctx context.Context, items []map[string]any, label string, rc readContext) error {
	items = items[:rc.limiter.take(len(items))]
	if len(items) == 0 {
		return nil
	}
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, rc.opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert %s to Arrow: %w", label, err)
	}

	select {
	case <-ctx.Done():
		record.Release()
		return ctx.Err()
	case rc.results <- source.RecordBatchResult{Batch: record}:
		return nil
	}
}

// rowLimiter enforces --sql-limit across the parallel list workers. A limit of
// zero or less means unlimited.
type rowLimiter struct {
	limit int64
	used  atomic.Int64
}

func newRowLimiter(limit int) *rowLimiter {
	return &rowLimiter{limit: int64(limit)}
}

// take claims up to n rows and reports how many of them fit in the budget.
func (l *rowLimiter) take(n int) int {
	if l.limit <= 0 {
		return n
	}
	for {
		used := l.used.Load()
		allowed := min(int64(n), l.limit-used)
		if allowed <= 0 {
			return 0
		}
		if l.used.CompareAndSwap(used, used+allowed) {
			return int(allowed)
		}
	}
}

func (l *rowLimiter) exhausted() bool {
	return l.limit > 0 && l.used.Load() >= l.limit
}

func filterItemsByInterval(items []map[string]any, field string, start, end *time.Time) []map[string]any {
	if field == "" || (start == nil && end == nil) {
		return items
	}

	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		timestamp, dateOnly, ok := parseTimestamp(item[field])
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if start != nil && beforeIntervalStart(timestamp, dateOnly, start.UTC()) {
			continue
		}
		if end != nil && !timestamp.Before(end.UTC()) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// beforeIntervalStart reports whether a value falls entirely before the interval
// start. A date-only value covers a whole day, so it stays in range as long as
// any part of that day falls at or after the start.
func beforeIntervalStart(timestamp time.Time, dateOnly bool, start time.Time) bool {
	if dateOnly {
		return !timestamp.AddDate(0, 0, 1).After(start)
	}
	return timestamp.Before(start)
}

// parseTimestamp reports the parsed value and whether it carried date-only
// granularity, which callers need to place it against a sub-day interval.
func parseTimestamp(value any) (timestamp time.Time, dateOnly, ok bool) {
	text, isString := value.(string)
	if !isString || text == "" {
		return time.Time{}, false, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return parsed.UTC(), false, true
	}
	if parsed, err := time.Parse(time.DateOnly, text); err == nil {
		return parsed.UTC(), true, true
	}
	return time.Time{}, false, false
}

func addSignalPrimaryKeys(items []map[string]any) error {
	for _, item := range items {
		if signalID := scalarString(item["signal_id"]); signalID != "" {
			item["_ingestr_id"] = "signal:" + signalID
			continue
		}

		encoded, err := json.Marshal(item)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(encoded)
		item["_ingestr_id"] = "hash:" + hex.EncodeToString(digest[:])
	}
	return nil
}

func addScopedPrimaryKeys(items []map[string]any, parentID int64) error {
	for _, item := range items {
		if itemID := scalarString(item["id"]); itemID != "" {
			item["_ingestr_id"] = fmt.Sprintf("%d:id:%s", parentID, itemID)
			continue
		}

		encoded, err := json.Marshal(item)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(encoded)
		item["_ingestr_id"] = fmt.Sprintf("%d:hash:%s", parentID, hex.EncodeToString(digest[:]))
	}
	return nil
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case json.Number:
		return typed.String()
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}
