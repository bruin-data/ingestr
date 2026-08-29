// Package twenty implements a source for Twenty CRM.
package twenty

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
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	updatedAtField        = "updatedAt"
	deletedAtField        = "deletedAt"
	defaultPageSize       = 200
	maxPageSize           = 200
	maxPages              = 200000
	metadataPageSize      = 200
	maxMetadataPages      = 50
	defaultRateLimit      = 1.3333
	defaultRateLimitBurst = 5
	twentyTimeLayout      = "2006-01-02T15:04:05.000Z"
	defaultBasePath       = "/rest"
)

var standardTables = map[string]struct{}{
	"companies":        {},
	"notes":            {},
	"opportunities":    {},
	"people":           {},
	"tasks":            {},
	"workspaceMembers": {},
}

type pageInfo struct {
	StartCursor string `json:"startCursor"`
	EndCursor   string `json:"endCursor"`
	HasNextPage bool   `json:"hasNextPage"`
}

type Source struct {
	client         *httpclient.Client
	pageSize       int
	includeDeleted bool
	host           string
	meta           metadataCache
}

func NewTwentySource() *Source { return &Source{} }

func (s *Source) Schemes() []string { return []string{"twenty"} }

func (s *Source) HandlesIncrementality() bool { return false }

type uriConfig struct {
	baseURL        string
	host           string
	apiKey         string
	pageSize       int
	rateLimit      float64
	includeDeleted bool
}

func parseURI(uri string) (uriConfig, error) {
	var cfg uriConfig

	parsed, err := url.Parse(uri)
	if err != nil {
		return cfg, fmt.Errorf("twenty: could not parse source URI: %w", err)
	}
	if parsed.Scheme != "twenty" {
		return cfg, fmt.Errorf("twenty: source URI must start with twenty://")
	}
	if parsed.Host == "" {
		return cfg, fmt.Errorf("twenty: the workspace host is required, e.g. twenty://api.twenty.com?api_key=…")
	}
	cfg.host = parsed.Host
	q := parsed.Query()

	transport := q.Get("scheme")
	if transport == "" {
		transport = "https"
	}
	if transport != "https" && transport != "http" {
		return cfg, fmt.Errorf("twenty: scheme must be https or http, got %q", transport)
	}

	basePath := q.Get("base_path")
	if basePath == "" {
		basePath = defaultBasePath
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	cfg.baseURL = transport + "://" + parsed.Host + strings.TrimSuffix(basePath, "/")

	cfg.apiKey = q.Get("api_key")
	if cfg.apiKey == "" {
		return cfg, fmt.Errorf("twenty: api_key is required (Settings → API & Webhooks in the workspace)")
	}

	cfg.pageSize = defaultPageSize
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, fmt.Errorf("twenty: page_size must be a positive integer, got %q", v)
		}
		if n > maxPageSize {
			return cfg, fmt.Errorf("twenty: page_size may not exceed %d (the API's cap), got %d", maxPageSize, n)
		}
		cfg.pageSize = n
	}

	cfg.rateLimit = defaultRateLimit
	if v := q.Get("rate_limit"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			return cfg, fmt.Errorf("twenty: rate_limit must be a positive number, got %q", v)
		}
		cfg.rateLimit = f
	}

	cfg.includeDeleted = true
	if v := q.Get("include_deleted"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return cfg, fmt.Errorf("twenty: include_deleted must be a boolean, got %q", v)
		}
		cfg.includeDeleted = b
	}

	return cfg, nil
}

func (s *Source) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.pageSize = cfg.pageSize
	s.includeDeleted = cfg.includeDeleted
	s.host = cfg.host

	s.client = httpclient.New(
		httpclient.WithBaseURL(cfg.baseURL),
		httpclient.WithTimeout(120*time.Second),
		httpclient.WithAuth(httpclient.NewBearerAuth(cfg.apiKey)),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithRateLimiter(cfg.rateLimit, defaultRateLimitBurst),
		httpclient.WithRetry(4, 2*time.Second, 30*time.Second),
		httpclient.WithDebug(config.DebugMode),
	)
	return nil
}

func (s *Source) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("twenty: table name is required")
	}

	objectName := name
	if strings.HasPrefix(name, "custom:") {
		objectName = strings.TrimSpace(strings.TrimPrefix(name, "custom:"))
		if objectName == "" {
			return nil, fmt.Errorf("twenty: custom object name is required after custom:")
		}
	} else if _, ok := standardTables[name]; !ok {
		supported := make([]string, 0, len(standardTables))
		for table := range standardTables {
			supported = append(supported, table)
		}
		sortStrings(supported)
		return nil, fmt.Errorf("twenty: unsupported table %q (supported: %s, or use 'custom:<object_name>' for custom objects)",
			name, strings.Join(supported, ", "))
	}

	objects, err := s.fetchObjects(ctx)
	if err != nil {
		return nil, err
	}

	obj, err := findObject(objects, objectName)
	if err != nil {
		return nil, err
	}
	plan, err := buildPlan(*obj)
	if err != nil {
		return nil, err
	}

	incremental := ""
	if plan.hasUpdatedAt {
		incremental = updatedAtField
	} else {
		config.Debug("[TWENTY] object %s has no %s field: every run will re-read it in full",
			plan.object, updatedAtField)
	}
	if !plan.hasDeletedAt && s.includeDeleted {
		config.Debug("[TWENTY] object %s has no %s field: the soft-delete pass is skipped",
			plan.object, deletedAtField)
	}

	tableSchema := &schema.TableSchema{Name: plan.object, Columns: plan.columns}

	return &source.DynamicSourceTable{
		TableName:           plan.object,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: incremental,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, plan, opts)
		},
	}, nil
}

func findObject(objects []objectMeta, name string) (*objectMeta, error) {
	for i := range objects {
		if objects[i].NamePlural == name {
			return &objects[i], nil
		}
	}
	for i := range objects {
		if objects[i].NameSingular == name {
			return nil, fmt.Errorf("twenty: %q is the singular name; use the plural api name %q as the table",
				name, objects[i].NamePlural)
		}
	}
	available := make([]string, 0, len(objects))
	for _, o := range objects {
		available = append(available, o.NamePlural)
	}
	sortStrings(available)
	return nil, fmt.Errorf("twenty: this workspace has no object %q. Available: %s",
		name, strings.Join(available, ", "))
}

func sortStrings(in []string) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}

func (s *Source) read(ctx context.Context, plan *tablePlan, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 4)
	go func() {
		defer close(results)
		if err := s.readAll(ctx, plan, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

func buildFilter(plan *tablePlan, opts source.ReadOptions, deletedOnly bool) string {
	parts := make([]string, 0, 2)
	if plan.hasUpdatedAt && opts.IntervalStart != nil {
		parts = append(parts, fmt.Sprintf("%s[gte]:%s",
			updatedAtField, opts.IntervalStart.UTC().Format(twentyTimeLayout)))
	}
	if deletedOnly {
		parts = append(parts, deletedAtField+"[is]:NOT_NULL")
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return "and(" + strings.Join(parts, ",") + ")"
	}
}

func (s *Source) readAll(ctx context.Context, plan *tablePlan, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	drift := map[string]struct{}{}

	live, err := s.readPass(ctx, plan, opts, buildFilter(plan, opts, false), "live", drift, results)
	if err != nil {
		return err
	}

	deleted := passResult{}
	if s.includeDeleted && plan.hasDeletedAt {
		deleted, err = s.readPass(ctx, plan, opts, buildFilter(plan, opts, true), "deleted", drift, results)
		if err != nil {
			return err
		}
	}

	if len(drift) > 0 {
		config.Debug("[TWENTY] %s: %d undeclared field(s) dropped: %s",
			plan.object, len(drift), strings.Join(sortedKeys(drift), ", "))
	}
	config.Debug("[TWENTY] %s complete: %d live + %d soft-deleted row(s)",
		plan.object, live.rows, deleted.rows)
	return nil
}

type passResult struct {
	rows        int
	serverTotal int
}

func (s *Source) readPass(
	ctx context.Context,
	plan *tablePlan,
	opts source.ReadOptions,
	filter, label string,
	drift map[string]struct{},
	results chan<- source.RecordBatchResult,
) (passResult, error) {
	out := passResult{serverTotal: -1}

	pageSize := s.pageSize
	if opts.PageSize > 0 && opts.PageSize <= maxPageSize {
		pageSize = opts.PageSize
	}

	cursor := ""
	for page := 0; ; page++ {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}
		if page >= maxPages {
			return out, fmt.Errorf("twenty: %s (%s pass) exceeded the %d-page guard", plan.object, label, maxPages)
		}

		req := s.client.R(ctx).
			SetQueryParam("limit", strconv.Itoa(pageSize)).
			SetQueryParam("depth", "0")
		if filter != "" {
			req = req.SetQueryParam("filter", filter)
		}
		if cursor != "" {
			req = req.SetQueryParam("starting_after", cursor)
		}

		resp, err := req.Get("/" + url.PathEscape(plan.object))
		if err != nil {
			return out, fmt.Errorf("twenty: failed to read %s (%s pass, page %d): %w", plan.object, label, page, err)
		}
		if !resp.IsSuccess() {
			return out, fmt.Errorf("twenty: read of %s (%s pass, page %d) returned HTTP %d: %s",
				plan.object, label, page, resp.StatusCode(), truncate(resp.String(), 400))
		}

		items, info, total, err := extractRecords([]byte(resp.String()), plan.object)
		if err != nil {
			return out, err
		}
		if page == 0 {
			out.serverTotal = total
			config.Debug("[TWENTY] %s (%s): %d row(s) match%s", plan.object, label, total,
				map[bool]string{true: " the window", false: ""}[filter != ""])
		}

		if len(items) > 0 {
			rows := make([]map[string]interface{}, 0, len(items))
			for _, it := range items {
				rows = append(rows, projectRow(it, plan, drift))
			}
			if err := emit(rows, plan.columns, opts, results); err != nil {
				return out, fmt.Errorf("twenty: failed to convert %s to Arrow: %w", plan.object, err)
			}
			out.rows += len(rows)
		}

		config.Debug("[TWENTY] %s (%s) page %d: %d row(s) (running total %d)",
			plan.object, label, page, len(items), out.rows)

		if !info.HasNextPage || info.EndCursor == "" || len(items) == 0 {
			break
		}
		if info.EndCursor == cursor {
			return out, fmt.Errorf("twenty: %s (%s pass) returned the same cursor twice at page %d — refusing to loop",
				plan.object, label, page)
		}
		cursor = info.EndCursor
	}

	if out.serverTotal > 0 && out.rows == 0 {
		return out, fmt.Errorf(
			"twenty: %s (%s pass) reported %d matching row(s) but the read returned none — "+
				"this is a read bug, not an empty table", plan.object, label, out.serverTotal)
	}
	if out.serverTotal >= 0 && out.rows != out.serverTotal {
		config.Debug("[TWENTY] %s (%s): read %d row(s), server reported %d at start of walk",
			plan.object, label, out.rows, out.serverTotal)
	}
	return out, nil
}

func extractRecords(body []byte, object string) ([]map[string]interface{}, pageInfo, int, error) {
	var env struct {
		Data       map[string]json.RawMessage `json:"data"`
		PageInfo   pageInfo                   `json:"pageInfo"`
		TotalCount *int                       `json:"totalCount"`
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	if err := dec.Decode(&env); err != nil {
		return nil, pageInfo{}, -1, fmt.Errorf("twenty: failed to parse response for %s: %w", object, err)
	}
	if env.Data == nil {
		return nil, pageInfo{}, -1, fmt.Errorf("twenty: response for %s had no data envelope", object)
	}

	total := -1
	if env.TotalCount != nil {
		total = *env.TotalCount
	}

	decode := func(raw json.RawMessage) ([]map[string]interface{}, bool) {
		var items []map[string]interface{}
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if err := d.Decode(&items); err != nil {
			return nil, false
		}
		return items, true
	}

	if raw, ok := env.Data[object]; ok {
		items, ok := decode(raw)
		if !ok {
			return nil, env.PageInfo, total, fmt.Errorf("twenty: failed to parse %s records", object)
		}
		return items, env.PageInfo, total, nil
	}
	for key, raw := range env.Data {
		if items, ok := decode(raw); ok {
			config.Debug("[TWENTY] %s: records arrived under key %q, not the object name", object, key)
			return items, env.PageInfo, total, nil
		}
	}
	return nil, env.PageInfo, total, nil
}

func projectRow(item map[string]interface{}, plan *tablePlan, drift map[string]struct{}) map[string]interface{} {
	row := make(map[string]interface{}, len(plan.columns))
	for _, c := range plan.columns {
		row[c.Name] = nil
	}
	for key, val := range item {
		dest := sanitizeColumn(key)
		dt, ok := plan.typeOf[dest]
		if !ok {
			if _, deliberate := plan.dropped[key]; !deliberate {
				drift[key] = struct{}{}
			}
			continue
		}
		row[dest] = coerce(val, dt)
	}
	return row
}

func coerce(v interface{}, dt schema.DataType) interface{} {
	if v == nil {
		return nil
	}
	switch dt {
	case schema.TypeInt64:
		switch t := v.(type) {
		case json.Number:
			n, err := t.Int64()
			if err != nil {
				return nil
			}
			return n
		case string:
			n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
			if err != nil {
				return nil
			}
			return n
		case float64:
			return int64(t)
		default:
			return nil
		}

	case schema.TypeFloat64:
		switch t := v.(type) {
		case json.Number:
			f, err := t.Float64()
			if err != nil {
				return nil
			}
			return f
		case float64:
			return t
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
			if err != nil {
				return nil
			}
			return f
		default:
			return nil
		}

	case schema.TypeBoolean:
		switch t := v.(type) {
		case bool:
			return t
		case string:
			b, err := strconv.ParseBool(strings.TrimSpace(t))
			if err != nil {
				return nil
			}
			return b
		default:
			return nil
		}

	case schema.TypeDate:
		s, ok := v.(string)
		if !ok {
			return nil
		}
		return parseTwentyDate(s)

	case schema.TypeTimestamp:
		s, ok := v.(string)
		if !ok {
			return nil
		}
		return parseTwentyTimestamp(s)

	case schema.TypeJSON:
		return v

	default:
		switch t := v.(type) {
		case string:
			return t
		case json.Number:
			return t.String()
		case bool:
			return strconv.FormatBool(t)
		case map[string]interface{}, []interface{}:
			b, err := json.Marshal(t)
			if err != nil {
				return fmt.Sprintf("%v", t)
			}
			return string(b)
		default:
			return fmt.Sprintf("%v", t)
		}
	}
}

func parseTwentyDate(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t
		}
	}
	return nil
}

func parseTwentyTimestamp(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{
		twentyTimeLayout,
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return nil
}

func emit(items []map[string]interface{}, cols []schema.Column, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, cols, opts.ExcludeColumns)
	if err != nil {
		return err
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}
