// Package abra implements an ingestr source for ABRA Flexi (formerly Flexibee),
// a Czech cloud accounting and ERP system.
//
// Docs:  https://www.flexibee.eu/api/   (per-account devdoc at /devdoc)
// Base:  https://<account>.flexibee.eu/c/<company>/<evidence>.json
// Auth:  HTTP Basic.
//
// ── Why this is a generic engine rather than a hand-written table list ───────
//
// Flexi exposes 249 "evidences" (registers) per company, and every one of them
// publishes a machine-readable schema at /<evidence>/properties.json carrying the
// field name, type, sortability and key flags. That is a far better starting point
// than most vendor APIs give us: the sibling `fakturoid` source in this tree has to
// carry hand-generated 84/49/13/8-column allow-lists because Fakturoid publishes
// nothing comparable. Here one reader covers every evidence, and adding a table to
// a CronJob is a string, not a code change.
//
// ── What Flexi gives us that makes incremental loading real ─────────────────
//
//	id          integer, sortable   -> primary key
//	lastUpdate  datetime, sortable  -> incremental cursor
//	filter      query language      -> `lastUpdate gte '<ts>'` server-side
//	limit/start                     -> paging
//	add-row-count                   -> total, for progress and the runaway guard
//
// ── The traps, all of them verified against the live API ────────────────────
//
// ⚠️ NOT EVERY EVIDENCE IS A TABLE. Some are derived views: `ucetni-denik` (the
// accounting journal) reports 47,189 rows, every one with id = -1 and an EMPTY
// lastUpdate. buildPlan REFUSES those rather than loading them, because under
// `merge` they would append their entire result set on every single run and
// `count() FINAL` could not see the duplication. See the fail-closed note there.
//
// ⚠️ `limit` MUST ALWAYS BE SENT. Flexi treats a MISSING limit as "give them the
// default page" (~20 rows) and limit=0 as "give them EVERYTHING". Both are wrong
// here and both fail silently — the first truncates, the second can try to
// materialise a million-row evidence in one response. pageSize is never defaulted
// away; see readPaged.
//
// ⚠️ PAGING IS ORDERED BY id, NOT BY THE CURSOR. Ordering by lastUpdate while
// walking limit/start is unstable: any row edited mid-run moves in the sort order
// and can be skipped or repeated across page boundaries. `id@A` is immutable, so
// the walk is stable even while the books are being edited under us.
//
// ⚠️ RELATION AND SELECT FIELDS EXPLODE INTO @-SUFFIXED COLUMNS — `mena`,
// `mena@ref`, `mena@showAs`. `@` is not usable as a bare column name on most
// destinations and breaks downstream references, so sanitizeColumn rewrites it.
// That is the only place this source departs from verbatim naming, and it is a
// mechanical character substitution, not a rename.
//
// ⚠️ MONEY IS CARRIED AS TEXT. `numeric` maps to a string column. The reasoning is
// long enough to live at dataTypeFor — read it before "fixing" this.
//
// ⚠️ ONE CREDENTIAL, TEN COMPANY DATABASES, SELECTED ONLY BY `company` IN THE DSN.
// Exactly the shape that bit the fakturoid port: pointing the wrong company at the
// wrong destination database produces no error at all, just the wrong company's
// books in the wrong table. The company is required and never defaulted.
package abra

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
	// lastUpdateField is Flexi's own row-modification timestamp. It is the only
	// server-side filterable cursor Flexi offers, which is what makes incremental
	// loading possible at all.
	lastUpdateField = "lastUpdate"

	// externalIDsField rides along on most evidences without being declared in
	// properties.json — it carries integration keys such as
	// `ext:DATIVERY:bankDocumentsToInvoicing-30bf79:txn_...`.
	externalIDsField = "external-ids"

	// defaultPageSize is a compromise: large enough that a 14k-row evidence is ~14
	// requests, small enough that one page of a 300-column document evidence stays
	// a sane HTTP response. Overridable via `page_size` in the DSN.
	defaultPageSize = 1000

	// maxPages bounds the paging loop. At the default page size this allows 5M
	// rows per evidence — a runaway guard, not a limit.
	maxPages = 5000

	// defaultRateLimit keeps us polite against a shared production accounting
	// system that the finance team uses interactively. Flexi publishes no rate
	// limit, so this is deliberately conservative rather than tuned.
	defaultRateLimit = 4.0

	// flexiTimeLayout is what Flexi both emits and accepts in filters:
	// 2026-02-11T15:09:06.376+01:00
	flexiTimeLayout = "2006-01-02T15:04:05.000Z07:00"
)

// Source is one authenticated view into ONE Flexi company database.
type Source struct {
	client           *httpclient.Client
	company          string
	pageSize         int
	includeExpensive bool
}

func NewAbraSource() *Source { return &Source{} }

func (s *Source) Schemes() []string { return []string{"abra", "flexibee"} }

// HandlesIncrementality is false: this source pushes `lastUpdate gte ...` down to
// Flexi as a read filter, but the destination still performs the merge. It keeps
// no state of its own.
func (s *Source) HandlesIncrementality() bool { return false }

type uriConfig struct {
	baseURL          string
	username         string
	password         string
	company          string
	pageSize         int
	rateLimit        float64
	includeExpensive bool
}

// parseURI accepts:
//
//	abra://example.flexibee.eu?username=API&password=…&company=acme_s_r_o_
//
// Optional: page_size, rate_limit, include_expensive, scheme (http for tests).
func parseURI(uri string) (uriConfig, error) {
	var cfg uriConfig

	parsed, err := url.Parse(uri)
	if err != nil {
		return cfg, fmt.Errorf("abra: could not parse source URI: %w", err)
	}
	if parsed.Scheme != "abra" && parsed.Scheme != "flexibee" {
		return cfg, fmt.Errorf("abra: source URI must start with abra:// or flexibee://")
	}
	host := parsed.Host
	if host == "" {
		return cfg, fmt.Errorf("abra: the Flexi account host is required, e.g. abra://example.flexibee.eu?…")
	}
	q := parsed.Query()

	// `scheme` exists so the test server can be reached over plain http. Production
	// is always https — Basic auth over http would put the shared credential on the
	// wire in clear text.
	transport := q.Get("scheme")
	if transport == "" {
		transport = "https"
	}
	if transport != "https" && transport != "http" {
		return cfg, fmt.Errorf("abra: scheme must be https or http, got %q", transport)
	}
	cfg.baseURL = transport + "://" + host + strings.TrimSuffix(parsed.Path, "/")

	cfg.username = q.Get("username")
	if cfg.username == "" {
		return cfg, fmt.Errorf("abra: username is required (the shared Flexi API user)")
	}
	cfg.password = q.Get("password")
	if cfg.password == "" {
		return cfg, fmt.Errorf("abra: password is required")
	}

	// ⚠️ Required and never defaulted — see the DSN warning in the package doc.
	cfg.company = q.Get("company")
	if cfg.company == "" {
		return cfg, fmt.Errorf(
			"abra: company is required — it selects WHICH set of books to read " +
				"(e.g. acme_s_r_o_, widgets__s_r_o_). Omitting it cannot be defaulted safely")
	}

	cfg.pageSize = defaultPageSize
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, fmt.Errorf("abra: page_size must be a positive integer, got %q", v)
		}
		cfg.pageSize = n
	}

	cfg.rateLimit = defaultRateLimit
	if v := q.Get("rate_limit"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			return cfg, fmt.Errorf("abra: rate_limit must be a positive number, got %q", v)
		}
		cfg.rateLimit = f
	}

	// Expensive properties are INCLUDED by default: this is the raw layer, and a
	// silently narrower table is worse than a slower read. Set include_expensive=false
	// if a specific evidence turns out to be pathologically slow.
	cfg.includeExpensive = true
	if v := q.Get("include_expensive"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return cfg, fmt.Errorf("abra: include_expensive must be a boolean, got %q", v)
		}
		cfg.includeExpensive = b
	}

	return cfg, nil
}

func (s *Source) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.company = cfg.company
	s.pageSize = cfg.pageSize
	s.includeExpensive = cfg.includeExpensive

	s.client = httpclient.New(
		httpclient.WithBaseURL(cfg.baseURL),
		httpclient.WithTimeout(180*time.Second),
		httpclient.WithAuth(httpclient.NewBasicAuth(cfg.username, cfg.password)),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithRateLimiter(cfg.rateLimit, 1),
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

// GetTable resolves one evidence into a readable table.
//
// The schema round-trip happens HERE rather than lazily inside Read so that a
// misspelled evidence, a derived view, or a permissions problem fails before any
// destination table is created. A half-created table is materially worse than a
// clean failure: ingestr recreates a MISSING destination as a plain,
// NON-replicated ReplacingMergeTree, so a failure after creation can quietly
// un-replicate a promoted table.
func (s *Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	evidence := strings.TrimSpace(req.Name)
	if evidence == "" {
		return nil, fmt.Errorf("abra: table name is required (the evidence path, e.g. faktura-vydana)")
	}

	doc, err := s.fetchProperties(ctx, evidence)
	if err != nil {
		return nil, err
	}
	plan, err := buildPlan(evidence, doc, s.includeExpensive)
	if err != nil {
		return nil, err
	}

	incremental := ""
	if plan.hasLastUpdate {
		incremental = lastUpdateField
	} else {
		// Not fatal — plenty of small codebooks (currencies, VAT rates) have a
		// stable id and simply never change. They just cannot be windowed, so every
		// run re-reads them in full and merge collapses the result.
		config.Debug("[ABRA] evidence %s has no %s column: every run will re-read it in full",
			evidence, lastUpdateField)
	}
	if plan.expensiveCount > 0 {
		config.Debug("[ABRA] evidence %s declares %d expensive propert(ies) (included=%v)",
			evidence, plan.expensiveCount, s.includeExpensive)
	}

	tableSchema := &schema.TableSchema{Name: evidence, Columns: plan.columns}

	return &source.DynamicSourceTable{
		TableName:           evidence,
		TablePrimaryKeys:    []string{sanitizeColumn(plan.primaryKey)},
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

func (s *Source) read(ctx context.Context, plan *tablePlan, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 4)
	go func() {
		defer close(results)
		if err := s.readPaged(ctx, plan, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

// buildFilter renders the Flexi query-language predicate for the requested window.
//
// Only the START bound is applied. Flexi would accept an upper bound too, but
// applying one would make a re-run of an old window silently DROP rows edited
// since — and merge is idempotent, so a wider window costs requests, never
// correctness. Same reasoning as the fakturoid port's `updated_since`.
func buildFilter(plan *tablePlan, opts source.ReadOptions) string {
	if !plan.hasLastUpdate || opts.IntervalStart == nil {
		return ""
	}
	return fmt.Sprintf("%s gte '%s'", lastUpdateField, opts.IntervalStart.Format(flexiTimeLayout))
}

func (s *Source) readPaged(ctx context.Context, plan *tablePlan, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := s.pageSize
	if opts.PageSize > 0 {
		pageSize = opts.PageSize
	}

	// The evidence path doubles as the JSON key Flexi answers with, so it is needed
	// both in the URL and when unwrapping the envelope.
	path := "/c/" + url.PathEscape(s.company) + "/" + url.PathEscape(plan.evidence) + ".json"
	filter := buildFilter(plan, opts)

	drift := map[string]struct{}{}
	total := 0
	rowCount := -1

	for page := 0; ; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if page >= maxPages {
			return fmt.Errorf("abra: evidence %s exceeded the %d-page guard", plan.evidence, maxPages)
		}

		start := page * pageSize

		req := s.client.R(ctx).
			// ⚠️ limit is ALWAYS explicit. See the package doc.
			SetQueryParam("limit", strconv.Itoa(pageSize)).
			SetQueryParam("start", strconv.Itoa(start)).
			SetQueryParam("detail", "full").
			// ⚠️ Stable paging key. NOT the cursor — see the package doc.
			SetQueryParam("order", "id@A")
		if page == 0 {
			req = req.SetQueryParam("add-row-count", "true")
		}
		if filter != "" {
			req = req.SetQueryParam("filter", filter)
		}

		resp, err := req.Get(path)
		if err != nil {
			return fmt.Errorf("abra: failed to read %s at offset %d: %w", plan.evidence, start, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("abra: read of %s at offset %d returned HTTP %d: %s",
				plan.evidence, start, resp.StatusCode(), truncate(resp.String(), 400))
		}

		items, count, err := extractRecords([]byte(resp.String()), plan.evidence)
		if err != nil {
			return err
		}
		if page == 0 && count >= 0 {
			rowCount = count
			config.Debug("[ABRA] %s: %d row(s) match%s", plan.evidence, rowCount,
				map[bool]string{true: " the window", false: ""}[filter != ""])
		}

		if len(items) > 0 {
			rows := make([]map[string]interface{}, 0, len(items))
			for _, it := range items {
				rows = append(rows, projectRow(it, plan, drift))
			}
			if err := emit(rows, plan.columns, opts, results); err != nil {
				return fmt.Errorf("abra: failed to convert %s to Arrow: %w", plan.evidence, err)
			}
			total += len(rows)
		}

		config.Debug("[ABRA] %s offset %d: %d row(s) (running total %d)",
			plan.evidence, start, len(items), total)

		// A short page is the end of data. Flexi also honours @rowCount, but the
		// short-page test is the one that stays correct when rows are being added
		// underneath us mid-walk.
		if len(items) < pageSize {
			break
		}
	}

	if len(drift) > 0 {
		// Loud, once per run. A key Flexi returned that properties.json never
		// declared is data we are dropping, and we would rather know.
		config.Debug("[ABRA] ⚠️ %s: %d undeclared field(s) dropped: %s",
			plan.evidence, len(drift), strings.Join(sortedKeys(drift), ", "))
	}
	// ⚠️ SILENT-ZERO GUARD. Flexi told us how many rows match; if it said "some" and we
	// read none, something is wrong with the read, NOT with the data — and every other
	// signal would say success (exit 0, "Ingestion completed successfully", no table
	// created). That is exactly how the stav-ceniku envelope-key bug hid: it was caught
	// by reconciling counts against a probe afterwards, which is not a thing anyone will
	// remember to do every night. Fail loudly instead.
	if rowCount > 0 && total == 0 {
		return fmt.Errorf(
			"abra: %s reported %d matching row(s) but the read returned none — "+
				"this is a read bug, not an empty table", plan.evidence, rowCount)
	}
	if rowCount >= 0 && total != rowCount {
		// Not an error: rows can legitimately appear or vanish during a long walk.
		// Worth surfacing though, because a large gap usually means a paging bug.
		config.Debug("[ABRA] %s: read %d row(s), server reported %d at start of walk",
			plan.evidence, total, rowCount)
	}
	config.Debug("[ABRA] %s complete: %d row(s)", plan.evidence, total)
	return nil
}

// extractRecords unwraps Flexi's `winstrom` envelope:
//
//	{"winstrom":{"@version":"1.0","@rowCount":"14035","faktura-vydana":[ {...} ]}}
//
// @rowCount is a STRING, and it is absent unless add-row-count was requested.
func extractRecords(body []byte, evidence string) ([]map[string]interface{}, int, error) {
	var outer struct {
		Winstrom map[string]json.RawMessage `json:"winstrom"`
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	if err := dec.Decode(&outer); err != nil {
		return nil, -1, fmt.Errorf("abra: failed to parse response for %s: %w", evidence, err)
	}
	if outer.Winstrom == nil {
		return nil, -1, fmt.Errorf("abra: response for %s was not a winstrom envelope", evidence)
	}

	rowCount := -1
	if rc, ok := outer.Winstrom["@rowCount"]; ok {
		var s string
		if json.Unmarshal(rc, &s) == nil {
			if n, err := strconv.Atoi(s); err == nil {
				rowCount = n
			}
		}
	}

	// ⚠️ THE ARRAY KEY IS USUALLY THE EVIDENCE PATH — BUT NOT ALWAYS, AND THE
	// MISMATCH IS SILENT. `stav-ceniku` returns its rows under a different key, so
	// looking the array up by evidence name alone yielded ZERO ROWS AND NO ERROR: the
	// run reported "Ingestion completed successfully", created no table, and only a
	// row-count reconciliation against the probe caught it (5 rows expected, table
	// absent). Never treat "my key is missing" as "the page is empty".
	//
	// So: prefer the evidence-named key, then fall back to the first non-`@` key whose
	// value is a JSON array. Only when there is NO array anywhere is the page really
	// empty — which is how Flexi answers a read past the last row.
	decode := func(raw json.RawMessage) ([]map[string]interface{}, bool) {
		var items []map[string]interface{}
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if err := d.Decode(&items); err != nil {
			return nil, false
		}
		return items, true
	}

	if raw, ok := outer.Winstrom[evidence]; ok {
		items, ok := decode(raw)
		if !ok {
			return nil, rowCount, fmt.Errorf("abra: failed to parse %s records", evidence)
		}
		return items, rowCount, nil
	}

	for key, raw := range outer.Winstrom {
		if strings.HasPrefix(key, "@") {
			continue // @version / @rowCount metadata, never records
		}
		if items, ok := decode(raw); ok {
			config.Debug("[ABRA] %s: records arrived under key %q, not the evidence path",
				evidence, key)
			return items, rowCount, nil
		}
	}
	return nil, rowCount, nil
}

// projectRow maps one Flexi record onto the planned columns, coercing each value
// to its declared type and recording anything undeclared as drift.
func projectRow(item map[string]interface{}, plan *tablePlan, drift map[string]struct{}) map[string]interface{} {
	row := make(map[string]interface{}, len(plan.columns))
	// Start every declared column at NULL so a row missing a field produces a NULL
	// rather than a ragged batch.
	for _, c := range plan.columns {
		row[c.Name] = nil
	}
	for key, val := range item {
		dest, ok := plan.sourceToColumn[key]
		if !ok {
			drift[key] = struct{}{}
			continue
		}
		row[dest] = coerce(val, plan.typeOf[dest])
	}
	return row
}

// coerce converts one Flexi JSON value to the declared column type.
//
// ⚠️ DATES AND DATETIMES ARE PARSED HERE, IN GO, ON PURPOSE. The Arrow builders
// accept a string and fall back to AppendNull() when they cannot parse it — a
// silent per-row data loss with no error anywhere. Parsing here means an
// unexpected format shows up as a NULL we chose, and the layouts below are the
// exact ones the live API emits:
//
//	date      2025-12-12+01:00        (a date carrying a UTC offset!)
//	datetime  2026-02-11T15:09:06.376+01:00
//
// The date form is why time.Parse("2006-01-02") alone is not enough — Flexi
// appends an offset to plain dates, which dateparse also mishandles.
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
		return parseFlexiDate(s)

	case schema.TypeTimestamp:
		s, ok := v.(string)
		if !ok {
			return nil
		}
		return parseFlexiDateTime(s)

	default: // schema.TypeString — including every `numeric` money column.
		switch t := v.(type) {
		case string:
			return t
		case json.Number:
			// The exact decimal TEXT Flexi sent. No float round-trip.
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

// parseFlexiDate handles "2025-12-12+01:00" and plain "2025-12-12".
//
// The offset is deliberately DISCARDED rather than applied. Flexi's date fields
// are calendar dates (issue date, due date) that happen to be serialised with the
// server's offset; converting them to UTC would move an invoice issued on the 1st
// at midnight CET back to the 30th of the previous month — a real, and in an
// accounting period a material, off-by-one.
func parseFlexiDate(s string) interface{} {
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

// parseFlexiDateTime handles "2026-02-11T15:09:06.376+01:00" and neighbouring
// shapes, normalising to UTC — these are true instants, so the offset matters and
// is applied.
func parseFlexiDateTime(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{
		flexiTimeLayout,
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

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
