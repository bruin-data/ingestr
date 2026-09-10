// Package fakturoid implements an ingestr source for the Fakturoid API v3 —
// invoices (with their lines and VAT-rate summaries) and subjects.
//
// Docs: https://www.fakturoid.cz/api/v3
//
// Tables: invoices, invoices_lines, invoices_vat_rates, subjects. Every field the
// API returns is passed through and typed by schema inference (nested values become
// JSON); the two child tables are exploded from the invoice payload and carry their
// parent's id as invoice_id.
//
// Auth is OAuth2 client_credentials: POST /oauth/token with the client id and
// secret as HTTP Basic returns a ~2h bearer token, refreshed lazily.
//
// The User-Agent is mandatory and must carry a contact address — Fakturoid rejects
// a missing or generic one with a 403 on every endpoint (including /oauth/token),
// which reads like an auth error but is not. It has no default and is required.
//
// Pagination is fixed at 40 rows with no total count, so a short page is the only
// end-of-data signal. merge cannot observe deletions: a removed line, invoice or
// subject lingers in the destination — use a periodic full reload if that matters.
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

	// perPage is a server constant Fakturoid does not let us change; a page shorter
	// than this marks the end of the collection.
	perPage = 40

	// maxPages is a runaway guard (~20 M records), not a limit.
	maxPages = 500000

	// defaultRateLimit is self-imposed (~90 req/min); Fakturoid throttles but
	// publishes no number. Override with ?rate_limit=.
	defaultRateLimit = 1.5
	rateLimitBurst   = 3

	retryAttempts = 5
	retryBackoff  = 5 * time.Second
	retryMaxWait  = 90 * time.Second

	// tokenSkew renews slightly before expiry so an in-flight request cannot land
	// with a just-expired token.
	tokenSkew = 2 * time.Minute
)

// supportedTables lists the tables this source produces.
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

// tokenAuth lazily fetches and refreshes an OAuth2 client_credentials bearer token.
// ingestr's stock authenticators are static, but a Fakturoid token lives only ~2h,
// so Apply() refreshes it on expiry.
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
		// Fresh background context: the token outlives any single request, so it must
		// not inherit a per-request deadline.
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
		httpclient.WithRetryStrategy(fakturoidRetryStrategy),
		httpclient.WithRetryCondition(func(resp *httpclient.Response, err error) bool {
			if err != nil {
				return true
			}
			// 429 is the throttle, 5xx is transient; other 4xx are our bug.
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
		if err := s.readPaged(ctx, table, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

// fakturoidRetryStrategy honors the Retry-After header on a throttle and otherwise
// backs off exponentially, capped at retryMaxWait.
func fakturoidRetryStrategy(resp *httpclient.Response, _ error) (time.Duration, error) {
	if resp != nil {
		if v := resp.Header().Get("Retry-After"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
				return time.Duration(secs) * time.Second, nil
			}
		}
	}
	attempt := 1
	if resp != nil && resp.Attempt() > 0 {
		attempt = resp.Attempt()
	}
	delay := retryBackoff << (attempt - 1)
	if delay <= 0 || delay > retryMaxWait {
		delay = retryMaxWait
	}
	return delay, nil
}

// readPaged walks a Fakturoid collection endpoint, accumulating rows into batches
// bounded by opts.MaxBatchBytes. All four tables share this loop; only the endpoint
// and projection differ.
func (s *FakturoidSource) readPaged(ctx context.Context, table string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := "/accounts/" + url.PathEscape(s.slug) + "/invoices.json"
	if table == "subjects" {
		endpoint = "/accounts/" + url.PathEscape(s.slug) + "/subjects.json"
	}

	// Fakturoid's updated_since is inclusive and takes an ISO-8601 instant.
	updatedSince := ""
	if opts.IntervalStart != nil {
		updatedSince = opts.IntervalStart.UTC().Format(time.RFC3339)
	}

	var (
		batch    []map[string]interface{}
		accBytes int64
		total    int
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := emit(batch, opts, results); err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", table, err)
		}
		total += len(batch)
		batch = nil
		accBytes = 0
		return nil
	}

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

		// UseNumber keeps large ids exact rather than rounding through float64.
		var items []map[string]interface{}
		dec := json.NewDecoder(strings.NewReader(resp.String()))
		dec.UseNumber()
		if err := dec.Decode(&items); err != nil {
			return fmt.Errorf("failed to parse %s page %d: %w", table, page, err)
		}

		rows := projectPage(table, items)
		for _, row := range rows {
			if opts.MaxBatchBytes > 0 {
				rowBytes := arrowconv.RowBytes(row)
				if len(batch) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
					if err := flush(); err != nil {
						return err
					}
				}
				accBytes += rowBytes
			}
			batch = append(batch, row)
		}
		config.Debug("[FAKTUROID] %s page %d: %d source records -> %d rows", table, page, len(items), len(rows))

		if len(items) < perPage {
			break
		}
	}

	if err := flush(); err != nil {
		return err
	}
	config.Debug("[FAKTUROID] %s complete: %d rows", table, total)
	return nil
}

// projectPage turns one page of API objects into destination rows. Every field is
// passed through as-is so the pipeline can infer types (nested objects/arrays land
// as JSON); callers drop what they don't want with --exclude-columns. The invoices
// parent omits `lines` and `vat_rates_summary`, which are exploded into their own
// tables, and each child row gets its parent's id as invoice_id.
func projectPage(table string, items []map[string]interface{}) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(items))
	switch table {
	case "subjects":
		rows = append(rows, items...)
	case "invoices":
		for _, it := range items {
			delete(it, "lines")
			delete(it, "vat_rates_summary")
			rows = append(rows, it)
		}
	case "invoices_lines":
		for _, it := range items {
			parent := it["id"]
			for _, child := range childArray(it, "lines") {
				child["invoice_id"] = parent
				rows = append(rows, child)
			}
		}
	case "invoices_vat_rates":
		for _, it := range items {
			parent := it["id"]
			for _, child := range childArray(it, "vat_rates_summary") {
				child["invoice_id"] = parent
				rows = append(rows, child)
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func emit(items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	// nil columns: let the pipeline infer types from the raw values.
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return err
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}
