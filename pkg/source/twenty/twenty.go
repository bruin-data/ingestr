// Package twenty implements an ingestr source for Twenty CRM — the open-source
// an open-source CRM, for sales and lifecycle data.
//
// Docs:  https://docs.twenty.com/developers/extend/api
// Base:  https://<host>/rest/<objectPlural>          (core records)
//
//	https://<host>/rest/metadata/objects         (schema)
//
// Auth:  Authorization: Bearer <API key>, one key per workspace.
//
// Works against both deployment shapes we run: Twenty Cloud
// (api.twenty.com) and self-hosted (any host running the REST API).
//
// ── Why this is a generic engine rather than a hand-written table list ───────
//
// Twenty is metadata-driven: every object, standard or custom, publishes its
// fields with types at /rest/metadata/objects. One reader therefore covers every
// object, and adding a table to a CronJob is a string, not a code change. That
// is not just convenience — two workspaces of the same product genuinely diverge.
// Measured across two live workspaces: one had a `lead` object the other lacked,
// and their `person` objects carried 79 fields against 32.
// A static table/column list would have been wrong for one of them immediately.
//
// ── The traps, all verified against the live API ─────────────────────────────
//
// ⚠️ depth=0 IS ALWAYS SENT. Twenty's default depth embeds related records, so a
// person arrives carrying its whole company object. That inflates every page,
// and worse, it makes the row shape depend on a server-side default we do not
// control. depth=0 returns the flat record plus foreign keys, which is exactly
// the raw layer's job. See readPass.
//
// ⚠️ THE CURSOR IS OPAQUE BASE64, NOT AN ID. pageInfo.endCursor decodes to
// {"id":"…"} today, but it is a cursor and is passed back verbatim. Never
// synthesise one from a record id.
//
// ⚠️ PAGING IS ORDERED BY id, NOT BY THE CURSOR FIELD, and order_by is
// deliberately NOT sent. The default order is what endCursor is built against;
// ordering by updatedAt while walking a cursor would be unstable, because any
// row edited mid-run moves in the sort order and can be skipped or repeated
// across a page boundary. id is immutable, so the walk is stable even while
// sales is editing records underneath it.
//
// ⚠️ SOFT DELETES ARE INVISIBLE BY DEFAULT. Twenty excludes deletedAt IS NOT NULL
// from every list response. Under merge that means a deleted record keeps its
// last-known state in the warehouse FOREVER, looking live. So a second pass runs
// with deletedAt[is]:NOT_NULL and re-reads exactly those rows, whose deletedAt
// then lands populated for downstream to filter on. Disable with
// include_deleted=false; do not disable it casually.
//
// ⚠️ MONEY IS {amountMicros, currencyCode} AND STAYS TEXT. See dataTypeFor —
// this also keeps the source clear of destination decimal handling on any
// table with a decimal column on its SECOND sync.
//
// ⚠️ ONE KEY PER WORKSPACE, AND THE WORKSPACE IS ONLY IN THE HOST. Pointing the
// one workspace's key at another workspace's host (or either at the wrong destination)
// produces no error, just the wrong company's CRM in the wrong table. Same shape
// that bit the fakturoid and abra ports.
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
	// updatedAtField is Twenty's row-modification timestamp, present on every
	// object. It is server-side filterable, which is what makes incremental
	// loading real rather than a full re-read every night.
	updatedAtField = "updatedAt"

	// deletedAtField drives the soft-delete pass — see the package doc.
	deletedAtField = "deletedAt"

	// defaultPageSize is Twenty's documented maximum. At 100 requests/minute the
	// page size is the only lever on wall-clock, so it is pinned to the ceiling:
	// a 68k-person workspace is ~342 requests here, and 6,828 at the API default of
	// 20 — over an hour of pure rate-limit wait.
	defaultPageSize = 200

	// maxPageSize is the API's hard cap; a larger value is rejected upstream.
	maxPageSize = 200

	// maxPages bounds the cursor walk. At the default page size this allows 40M
	// rows per object — a runaway guard against a cursor that never advances,
	// not a limit on real data.
	maxPages = 200000

	// metadataPageSize / maxMetadataPages bound the schema read. Workspaces have
	// tens of objects, not thousands.
	metadataPageSize = 200
	maxMetadataPages = 50

	// defaultRateLimit is 80% of Twenty's documented 100 requests/minute,
	// expressed per second: (100 * 0.8) / 60. With burst 5 the first minute
	// issues at most 5 + ~80 = 85 requests, inside the cap.
	defaultRateLimit = 1.3333

	// defaultRateLimitBurst per the add-source guidance.
	defaultRateLimitBurst = 5

	// twentyTimeLayout is what Twenty both emits and accepts in filters:
	// 2026-08-15T01:44:03.057Z — always UTC, always milliseconds.
	twentyTimeLayout = "2006-01-02T15:04:05.000Z"

	// defaultBasePath is where both Cloud and self-hosted serve the REST API.
	defaultBasePath = "/rest"
)

// pageInfo is Twenty's cursor envelope, shared by core and metadata responses.
type pageInfo struct {
	StartCursor string `json:"startCursor"`
	EndCursor   string `json:"endCursor"`
	HasNextPage bool   `json:"hasNextPage"`
}

// Source is one authenticated view into ONE Twenty workspace.
type Source struct {
	client         *httpclient.Client
	pageSize       int
	includeDeleted bool
	host           string
	meta           metadataCache
}

func NewTwentySource() *Source { return &Source{} }

func (s *Source) Schemes() []string { return []string{"twenty"} }

// HandlesIncrementality is false: this source pushes `updatedAt[gte]:…` down to
// Twenty as a read filter, but the destination still performs the merge. It
// keeps no state of its own.
func (s *Source) HandlesIncrementality() bool { return false }

type uriConfig struct {
	baseURL        string
	host           string
	apiKey         string
	pageSize       int
	rateLimit      float64
	includeDeleted bool
}

// parseURI accepts:
//
//	twenty://api.twenty.com?api_key=…
//	twenty://crm.example.com?api_key=…
//
// Optional: page_size, rate_limit, base_path, include_deleted, scheme (http for
// tests).
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

	// `scheme` exists so the test server can be reached over plain http.
	// Production is always https — a bearer token over http would put the key on
	// the wire in clear text.
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

	// Soft-deleted rows are INCLUDED by default. See the package doc: without the
	// second pass a deleted record silently persists in the warehouse as live.
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

// GetTable resolves one object (by its PLURAL api name) into a readable table.
//
// The metadata round-trip happens HERE rather than lazily inside Read so that a
// misspelled object or a permissions problem fails before any destination table
// is created. A half-created table is materially worse than a clean failure:
// ingestr recreates a MISSING destination as a plain, NON-replicated
// ReplacingMergeTree, so a failure after creation can quietly un-replicate a
// promoted table.
func (s *Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("twenty: table name is required (the object's plural api name, e.g. people)")
	}

	objects, err := s.fetchObjects(ctx)
	if err != nil {
		return nil, err
	}

	obj, err := findObject(objects, name)
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

// findObject resolves a table name to an object definition.
//
// Matching is on namePlural — the REST path segment — but a singular name is
// accepted too, because "person" vs "people" is the single easiest mistake to
// make when writing a CronJob and the API's 404 for it says nothing useful.
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

// buildFilter renders Twenty's filter expression for one pass.
//
// Only the START bound of the window is applied. Twenty would accept an upper
// bound too, but applying one would make a re-run of an old window silently DROP
// rows edited since — and merge is idempotent, so a wider window costs requests,
// never correctness. Same reasoning as the abra and fakturoid ports.
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

// readAll runs the live pass and, when enabled, the soft-delete pass.
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
		// Loud, once per run. A key Twenty returned that the metadata never
		// declared is data we are dropping, and we would rather know.
		config.Debug("[TWENTY] ⚠️ %s: %d undeclared field(s) dropped: %s",
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

// readPass walks one filtered cursor sequence to exhaustion.
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
			// ⚠️ Always flat. See the package doc.
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

		// hasNextPage is authoritative; the empty-cursor and short-page checks are
		// belt-and-braces against a server that sets it without advancing, which
		// would otherwise spin until the page guard.
		if !info.HasNextPage || info.EndCursor == "" || len(items) == 0 {
			break
		}
		if info.EndCursor == cursor {
			return out, fmt.Errorf("twenty: %s (%s pass) returned the same cursor twice at page %d — refusing to loop",
				plan.object, label, page)
		}
		cursor = info.EndCursor
	}

	// ⚠️ SILENT-ZERO GUARD. Twenty told us how many rows match; if it said "some"
	// and we read none, that is a read bug, NOT an empty table — and every other
	// signal would say success (exit 0, "Ingestion completed successfully", no
	// rows written). Fail loudly instead.
	if out.serverTotal > 0 && out.rows == 0 {
		return out, fmt.Errorf(
			"twenty: %s (%s pass) reported %d matching row(s) but the read returned none — "+
				"this is a read bug, not an empty table", plan.object, label, out.serverTotal)
	}
	if out.serverTotal >= 0 && out.rows != out.serverTotal {
		// Not an error: rows can legitimately appear or vanish during a long walk.
		// Worth surfacing though, because a large gap usually means a paging bug.
		config.Debug("[TWENTY] %s (%s): read %d row(s), server reported %d at start of walk",
			plan.object, label, out.rows, out.serverTotal)
	}
	return out, nil
}

// extractRecords unwraps Twenty's core list envelope:
//
//	{"data":{"people":[…]},"pageInfo":{…},"totalCount":68279}
//
// The array key is the object's plural name. It is looked up by name first and
// then by "the only array under data", because treating a key mismatch as an
// empty page is precisely how a read bug disguises itself as an empty table.
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
		// ⚠️ UseNumber: Twenty carries money as integer amountMicros, which passes
		// 2^53 for large amounts and would lose its last digits through float64.
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

// projectRow maps one Twenty record onto the planned columns, coercing each
// value to its declared type and recording anything undeclared as drift.
func projectRow(item map[string]interface{}, plan *tablePlan, drift map[string]struct{}) map[string]interface{} {
	row := make(map[string]interface{}, len(plan.columns))
	// Start every declared column at NULL so a row missing a field produces a
	// NULL rather than a ragged batch.
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

// coerce converts one Twenty JSON value to the declared column type.
//
// ⚠️ TIMESTAMPS AND DATES ARE PARSED HERE, IN GO, ON PURPOSE. The Arrow builders
// accept a string and fall back to AppendNull() when they cannot parse it — a
// silent per-row data loss with no error anywhere. Parsing here means an
// unexpected format shows up as a NULL we chose.
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

	default: // schema.TypeString — including every composite, carried as JSON text.
		switch t := v.(type) {
		case string:
			return t
		case json.Number:
			// The exact digits Twenty sent. No float round-trip.
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

// parseTwentyDate handles Twenty's plain calendar dates ("2026-07-30").
//
// Twenty's DATE fields are calendar dates (lastLogin, renewalDate) and carry no
// offset. An empty string is Twenty's "unset" for some custom fields.
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

// parseTwentyTimestamp handles "2026-08-15T01:44:03.057Z" and neighbouring
// shapes, normalising to UTC — these are true instants.
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
