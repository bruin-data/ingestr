// Package fakturoid implements an ingestr source for the Fakturoid API v3 —
// invoices (with their lines and VAT-rate summaries) and subjects.
//
// Docs: https://www.fakturoid.cz/api/v3
//
// Four tables:
//
//	invoices             84 API fields
//	invoices_lines       13 line fields + invoice_id
//	invoices_vat_rates    8 rate fields + invoice_id
//	subjects             49 API fields
//
// The field lists live in fields.go — see the header there for why they are
// allow-lists rather than "whatever the API returns".
//
// AUTH: OAuth2 **client_credentials**. POST /oauth/token with the client id and
// secret as HTTP Basic credentials returns a bearer token with a short lifetime
// (~2 h). Tokens are refreshed lazily and shared across requests, so a backfill
// that outlives one token does not die halfway.
//
// ⚠️ THE USER-AGENT IS MANDATORY AND MUST CARRY A CONTACT ADDRESS. Fakturoid
// rejects requests with a missing or generic User-Agent — it is in their docs as a
// hard requirement, not a courtesy. It is therefore a REQUIRED URI parameter with
// no default: a shared default would send one user's contact address on every
// other user's traffic. A 403 on every endpoint, including /oauth/token, is what a
// bad UA looks like; it does NOT present as an auth error.
//
// ⚠️ PAGINATION IS FIXED AT 40 ROWS AND THERE IS NO TOTAL COUNT. `per_page` is not
// a parameter — the page size is a server constant. The only end-of-data signal is
// a page shorter than 40.
//
// ⚠️ invoices_lines AND invoices_vat_rates COST A FULL RE-PAGE OF /invoices.json.
// Both are exploded out of the invoice payload, and ingestr reads each table
// independently, so loading all three walks the invoice list three times. That is
// accepted rather than worked around (caching across table reads would mean
// holding the whole invoice history in memory). It is why `updated_since` matters
// so much for the nightly: incremental runs page almost nothing.
//
// ⚠️ MERGE CANNOT SEE A DELETED LINE. Under `merge` on (invoice_id, id) a line
// removed from an existing invoice simply lingers in the destination. Invoice and
// subject deletions are equally invisible. If that matters, the fix is a
// periodic full reload of the child tables, not a cleverer incremental key.
package fakturoid

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
	"resty.dev/v3"
)

const (
	baseURL  = "https://app.fakturoid.cz/api/v3"
	tokenURL = "https://app.fakturoid.cz/api/v3/oauth/token"

	// perPage is a SERVER CONSTANT, not a request parameter. Fakturoid returns 40
	// records per page and offers no way to change it, so this exists only to
	// recognise the last page (len < perPage).
	perPage = 40

	// maxPages bounds the page loop. At 40 rows/page this allows 20 M records —
	// it is a runaway guard, not a limit.
	maxPages = 500000

	// defaultRateLimit is SELF-IMPOSED. Fakturoid throttles but does not publish a
	// precise number, so this is deliberately conservative (~90 req/min) rather
	// than tuned to a documented ceiling. Override with ?rate_limit= while
	// backfilling rather than rebuilding the image.
	defaultRateLimit = 1.5
	rateLimitBurst   = 3

	retryAttempts = 5
	retryBackoff  = 5 * time.Second
	retryMaxWait  = 90 * time.Second

	// tokenSkew renews a little before real expiry so a request in flight at the
	// boundary cannot land with a just-expired token.
	tokenSkew = 2 * time.Minute
)

// supportedTables lists the four tables this source produces.
//
// Nothing else from the API is exposed. Fakturoid also serves estimates,
// expenses, generators, recurring generators, todos and inventory — none of which
// downstream models reference, so adding them here would be new surface to
// maintain rather than parity.
var supportedTables = map[string]struct{}{
	"invoices":           {},
	"invoices_lines":     {},
	"invoices_vat_rates": {},
	"subjects":           {},
}

type FakturoidSource struct {
	client *httpclient.Client
	slug   string
}

func NewFakturoidSource() *FakturoidSource {
	return &FakturoidSource{}
}

func (s *FakturoidSource) Schemes() []string {
	return []string{"fakturoid"}
}

// HandlesIncrementality is false: this source applies `updated_since` server-side
// as a read filter, but the destination still does the merge. It does not manage
// its own state.
func (s *FakturoidSource) HandlesIncrementality() bool {
	return false
}

// tokenAuth is an Authenticator that lazily fetches and refreshes an OAuth2
// client_credentials bearer token.
//
// It exists because ingestr's stock authenticators are all static, and a Fakturoid
// token lives ~2 h — shorter than a full invoice backfill. Apply() is called on
// every request and is the only place that can notice expiry, so the refresh has
// to happen here rather than once in Connect.
type tokenAuth struct {
	mu      sync.Mutex
	token   string
	expiry  time.Time
	refresh func(ctx context.Context) (string, time.Duration, error)
}

func (a *tokenAuth) Apply(req *resty.Request) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token == "" || time.Now().After(a.expiry) {
		// A fresh background context on purpose: the token outlives any single
		// request, and inheriting a per-request deadline would make an unrelated
		// slow call poison the shared token.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tok, ttl, err := a.refresh(ctx)
		if err != nil {
			return fmt.Errorf("fakturoid: failed to obtain access token: %w", err)
		}
		if ttl <= tokenSkew {
			// Honour a short-lived token rather than computing an expiry in the past.
			a.expiry = time.Now().Add(ttl / 2)
		} else {
			a.expiry = time.Now().Add(ttl - tokenSkew)
		}
		a.token = tok
		config.Debug("[FAKTUROID] obtained access token, valid for %s", ttl)
	}
	req.SetAuthToken(a.token)
	return nil
}

func (a *tokenAuth) Name() string { return "fakturoid-oauth2" }

func (s *FakturoidSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.slug = cfg.slug

	// A SEPARATE client for the token endpoint, carrying Basic auth. It must not
	// use tokenAuth or obtaining a token would require a token.
	tokenClient := httpclient.New(
		httpclient.WithTimeout(30*time.Second),
		httpclient.WithUserAgent(cfg.userAgent),
		httpclient.WithAuth(httpclient.NewBasicAuth(cfg.clientID, cfg.clientSecret)),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
	)

	auth := &tokenAuth{
		refresh: func(ctx context.Context) (string, time.Duration, error) {
			var payload struct {
				AccessToken string `json:"access_token"`
				TokenType   string `json:"token_type"`
				ExpiresIn   int    `json:"expires_in"`
			}
			resp, err := tokenClient.R(ctx).
				SetHeader("Content-Type", "application/json").
				SetBody(map[string]string{"grant_type": "client_credentials"}).
				SetResult(&payload).
				Post(tokenURL)
			if err != nil {
				return "", 0, err
			}
			if !resp.IsSuccess() {
				// Deliberately does NOT echo the body: a token-endpoint error can
				// quote back the submitted credentials.
				return "", 0, fmt.Errorf("token endpoint returned status %d", resp.StatusCode())
			}
			if payload.AccessToken == "" {
				return "", 0, fmt.Errorf("token endpoint returned no access_token (status %d)", resp.StatusCode())
			}
			ttl := time.Duration(payload.ExpiresIn) * time.Second
			if ttl <= 0 {
				ttl = time.Hour
			}
			return payload.AccessToken, ttl, nil
		},
	}

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(120*time.Second),
		httpclient.WithUserAgent(cfg.userAgent),
		httpclient.WithRateLimiter(cfg.rateLimit, rateLimitBurst),
		httpclient.WithRetry(retryAttempts, retryBackoff, retryMaxWait),
		httpclient.WithRetryCondition(func(resp *httpclient.Response, err error) bool {
			if err != nil {
				return true
			}
			// 429 is the throttle; 5xx is transient. 4xx otherwise is our bug and
			// retrying just burns quota.
			return resp.StatusCode() == 429 || resp.StatusCode() >= 500
		}),
		httpclient.WithAuth(auth),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
	)
	config.Debug("[FAKTUROID] connected, account slug %s, rate limit %.2f req/s", cfg.slug, cfg.rateLimit)
	return nil
}

func (s *FakturoidSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

type uriConfig struct {
	clientID     string
	clientSecret string
	slug         string
	userAgent    string
	rateLimit    float64
}

func parseURI(uri string) (uriConfig, error) {
	var cfg uriConfig
	if !strings.HasPrefix(uri, "fakturoid://") {
		return cfg, fmt.Errorf("invalid fakturoid URI: must start with fakturoid://")
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(uri, "fakturoid://"), "?")
	values, err := url.ParseQuery(rest)
	if err != nil {
		return cfg, fmt.Errorf("failed to parse fakturoid URI query: %w", err)
	}
	cfg.clientID = values.Get("client_id")
	if cfg.clientID == "" {
		return cfg, fmt.Errorf("client_id is required in fakturoid URI")
	}
	cfg.clientSecret = values.Get("client_secret")
	if cfg.clientSecret == "" {
		return cfg, fmt.Errorf("client_secret is required in fakturoid URI")
	}
	// The slug identifies WHICH Fakturoid account. One credential pair can reach
	// several, so omitting it would silently load the wrong company's books.
	// Required, never defaulted.
	cfg.slug = values.Get("slug")
	if cfg.slug == "" {
		return cfg, fmt.Errorf("slug is required in fakturoid URI (the account slug from the Fakturoid URL)")
	}
	cfg.userAgent = values.Get("user_agent")
	if cfg.userAgent == "" {
		return cfg, fmt.Errorf(
			"user_agent is required in fakturoid URI and must carry a contact address, " +
				"e.g. user_agent=MyCompany%%20(billing@mycompany.com) — Fakturoid rejects " +
				"requests with a missing or generic User-Agent with a 403 on every endpoint")
	}
	cfg.rateLimit = defaultRateLimit
	if v := values.Get("rate_limit"); v != "" {
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil || parsed <= 0 {
			return cfg, fmt.Errorf("rate_limit must be a positive number, got %q", v)
		}
		cfg.rateLimit = parsed
	}
	return cfg, nil
}

func isValidTable(name string) bool {
	_, ok := supportedTables[name]
	return ok
}

func supportedTableNames() string {
	names := make([]string, 0, len(supportedTables))
	for n := range supportedTables {
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

func (s *FakturoidSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if !isValidTable(req.Name) {
		return nil, fmt.Errorf("unsupported fakturoid table %q, supported tables are: %s", req.Name, supportedTableNames())
	}

	var pks []string
	incrementalKey := ""
	switch req.Name {
	case "invoices", "subjects":
		pks = []string{"id"}
		// `updated_at` is both present on the row and server-side filterable via
		// updated_since, which is what makes it a real incremental key.
		incrementalKey = "updated_at"
	case "invoices_lines":
		// Line ids are unique within an invoice; qualifying with invoice_id makes
		// the key safe even if Fakturoid ever restarts line numbering per document.
		pks = []string{"invoice_id", "id"}
	case "invoices_vat_rates":
		// vat_rates_summary entries carry NO id of their own — the column
		// exists but the API does not populate it. There is exactly one summary row
		// per rate per invoice, so (invoice_id, vat_rate) is the natural key.
		pks = []string{"invoice_id", "vat_rate"}
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    pks,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("fakturoid source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
		},
	}, nil
}

func (s *FakturoidSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		var err error
		switch table {
		case "subjects":
			err = s.readPaged(ctx, "subjects", opts, results)
		case "invoices", "invoices_lines", "invoices_vat_rates":
			err = s.readPaged(ctx, table, opts, results)
		default:
			err = fmt.Errorf("unsupported fakturoid table: %s", table)
		}
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

// readPaged walks a Fakturoid collection endpoint and emits one batch per page.
//
// All three invoice-derived tables share this loop and differ only in how each
// page is projected, so the pagination and drift accounting cannot diverge
// between them.
func (s *FakturoidSource) readPaged(ctx context.Context, table string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := "/accounts/" + url.PathEscape(s.slug) + "/invoices.json"
	if table == "subjects" {
		endpoint = "/accounts/" + url.PathEscape(s.slug) + "/subjects.json"
	}

	// One timestamp for the whole run rather than stamping each page differently.
	loadedAt := time.Now().UTC()

	// Fakturoid's updated_since is inclusive and takes an ISO-8601 instant.
	updatedSince := ""
	if opts.IntervalStart != nil {
		updatedSince = opts.IntervalStart.UTC().Format(time.RFC3339)
	}

	// Explicit schema, computed once — see the ⚠️ on columnsFor.
	cols := columnsFor(table)
	if len(cols) == 0 {
		return fmt.Errorf("no column schema defined for table %s", table)
	}

	drift := map[string]struct{}{}
	total := 0

	for page := 1; ; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if page > maxPages {
			return fmt.Errorf("%s exceeded the %d page guard", table, maxPages)
		}

		req := s.client.R(ctx).SetQueryParam("page", strconv.Itoa(page))
		if updatedSince != "" {
			req = req.SetQueryParam("updated_since", updatedSince)
		}
		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s page %d: %w", table, page, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("%s page %d returned status %d: %s", table, page, resp.StatusCode(), truncate(resp.String(), 400))
		}

		// UseNumber keeps ids and money exact. Fakturoid sends amounts as JSON
		// strings today, but ids are numeric and large enough to matter, and
		// float64 would quietly round them.
		var items []map[string]interface{}
		dec := json.NewDecoder(strings.NewReader(resp.String()))
		dec.UseNumber()
		if err := dec.Decode(&items); err != nil {
			return fmt.Errorf("failed to parse %s page %d: %w", table, page, err)
		}

		rows := projectPage(table, items, loadedAt, drift)
		if len(rows) > 0 {
			if err := emit(rows, cols, opts, results); err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", table, err)
			}
			total += len(rows)
		}
		config.Debug("[FAKTUROID] %s page %d: %d source records -> %d rows (running total %d)",
			table, page, len(items), len(rows), total)

		// The ONLY end-of-data signal: a short page. There is no total and no
		// next-page link. An empty page also ends the walk.
		if len(items) < perPage {
			break
		}
	}

	if len(drift) > 0 {
		// Loud, once per run, rather than a silent drop. See fields.go.
		config.Debug("[FAKTUROID] ⚠️ %s: %d API field(s) not in the allow-list were skipped: %s",
			table, len(drift), strings.Join(sortedKeys(drift), ", "))
	}
	config.Debug("[FAKTUROID] %s complete: %d rows", table, total)
	return nil
}

// projectPage turns one page of API objects into destination rows, applying the
// allow-list for the requested table and recording any unexpected field in drift.
func projectPage(table string, items []map[string]interface{}, loadedAt time.Time, drift map[string]struct{}) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(items))
	switch table {
	case "subjects":
		for _, it := range items {
			rows = append(rows, project(it, subjectFields, loadedAt, drift, nil))
		}
	case "invoices":
		for _, it := range items {
			// `lines` and `vat_rates_summary` are the exploded children and are
			// expected to be absent from the parent projection — they must not be
			// reported as drift.
			rows = append(rows, project(it, invoiceFields, loadedAt, drift,
				map[string]struct{}{"lines": {}, "vat_rates_summary": {}}))
		}
	case "invoices_lines":
		for _, it := range items {
			parent := it["id"]
			for _, child := range childArray(it, "lines") {
				row := project(child, lineFields, loadedAt, drift, nil)
				row["invoice_id"] = coerce(parent, true)
				rows = append(rows, row)
			}
		}
	case "invoices_vat_rates":
		for _, it := range items {
			parent := it["id"]
			for _, child := range childArray(it, "vat_rates_summary") {
				row := project(child, vatRateFields, loadedAt, drift, nil)
				row["invoice_id"] = coerce(parent, true)
				rows = append(rows, row)
			}
		}
	}
	return rows
}

func childArray(item map[string]interface{}, key string) []map[string]interface{} {
	raw, ok := item[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, e := range raw {
		if m, ok := e.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

// project copies exactly the allow-listed fields, stringifies anything nested and
// appends _etl_loaded_at.
//
// Every allow-listed key is set even when the API omits it, so the column set is
// identical on every page. Without that, schema inference would see a different
// shape per batch and the destination table would gain columns over time.
func project(src map[string]interface{}, allow []string, loadedAt time.Time, drift map[string]struct{}, expected map[string]struct{}) map[string]interface{} {
	row := make(map[string]interface{}, len(allow)+2)
	allowed := make(map[string]struct{}, len(allow))
	for _, k := range allow {
		allowed[k] = struct{}{}
		row[k] = coerce(src[k], isIntColumn(k))
	}
	for k := range src {
		if _, ok := allowed[k]; ok {
			continue
		}
		if expected != nil {
			if _, ok := expected[k]; ok {
				continue
			}
		}
		drift[k] = struct{}{}
	}
	row["_etl_loaded_at"] = loadedAt
	return row
}

// intColumns are the only non-text columns. Everything else is String, mirroring
// a wide-text projection where every column but the ids is a string.
//
// They are also the primary keys, which is why they must be non-nullable: a
// ReplacingMergeTree ORDER BY over a Nullable column needs allow_nullable_key,
// and the promote step declares `id Int64` exactly as the payments tables do.
var intColumns = map[string]struct{}{
	"id":         {},
	"invoice_id": {},
}

func isIntColumn(name string) bool {
	_, ok := intColumns[name]
	return ok
}

// columnsFor returns the EXPLICIT destination schema for a table.
//
// ⚠️ THIS IS NOT OPTIONAL, AND OMITTING IT IS A SILENT DATA-SHAPE BUG. Arrow
// schema INFERENCE drops any column that is null across every row of a batch, so
// a first run of `subjects` produced 29 columns instead of 50: the 21 fields that
// happen to be empty for every current subject simply vanished, and the table
// would then gain columns later as data appeared. Passing an explicit column list
// to arrowconv pins the shape regardless of the values in any given page.
func columnsFor(table string) []schema.Column {
	var (
		fields []string
		pks    map[string]struct{}
	)
	switch table {
	case "invoices":
		fields = invoiceFields
		pks = map[string]struct{}{"id": {}}
	case "subjects":
		fields = subjectFields
		pks = map[string]struct{}{"id": {}}
	case "invoices_lines":
		fields = append(append([]string{}, lineFields...), "invoice_id")
		pks = map[string]struct{}{"id": {}, "invoice_id": {}}
	case "invoices_vat_rates":
		fields = append(append([]string{}, vatRateFields...), "invoice_id")
		// `id` is deliberately NOT a key here — vat_rates_summary carries none.
		pks = map[string]struct{}{"invoice_id": {}, "vat_rate": {}}
	default:
		return nil
	}

	cols := make([]schema.Column, 0, len(fields)+1)
	for _, f := range fields {
		col := schema.Column{Name: f, DataType: schema.TypeString, Nullable: true}
		if isIntColumn(f) {
			col.DataType = schema.TypeInt64
		}
		if _, ok := pks[f]; ok {
			col.IsPrimaryKey = true
			col.Nullable = false
		}
		cols = append(cols, col)
	}
	cols = append(cols, schema.Column{Name: "_etl_loaded_at", DataType: schema.TypeTimestamp, Nullable: false})
	return cols
}

// coerce converts an API value to the declared column type.
//
// Text columns are stringified rather than passed through, so a value that is a
// JSON number on one invoice and a string on another cannot flip the column type
// between batches. Nested objects (invoice lines carry an `inventory` object)
// become JSON text rather than a structured column.
func coerce(v interface{}, wantInt bool) interface{} {
	if v == nil {
		return nil
	}
	if wantInt {
		switch t := v.(type) {
		case json.Number:
			n, err := t.Int64()
			if err != nil {
				return nil
			}
			return n
		case float64:
			return int64(t)
		case int64:
			return t
		case string:
			n, err := strconv.ParseInt(t, 10, 64)
			if err != nil {
				return nil
			}
			return n
		default:
			return nil
		}
	}
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
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

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Small sets; insertion sort keeps this dependency-free and deterministic.
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

func emit(items []map[string]interface{}, cols []schema.Column, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, cols, opts.ExcludeColumns)
	if err != nil {
		return err
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}
