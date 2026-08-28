// Package twocheckout implements an ingestr source for the 2Checkout (Verifone)
// REST API 6.0. There is no upstream ingestr connector for it.
//
// Scheme is `twocheckout`, NOT `2checkout`: RFC 3986 requires a URI scheme to
// begin with a letter, and a leading digit breaks url.Parse.
//
// AUTH — custom HMAC-SHA256 in the X-Avangate-Authentication header:
//   - hash payload is len(code)+code + len(date)+date
//   - date must be formatted EXACTLY "2006-01-02 15:04:05" in UTC. Sending
//     RFC3339 (with the T and Z) returns 401.
//   - server clock-skew tolerance is only a few minutes, so every request is
//     re-stamped rather than reusing a cached header.
//
// PAGINATION is Page/Limit query params — capitalised, like every other 2Checkout
// parameter. No cursor and no Link header. The envelope is
// {"Pagination":{"Page":1,"Limit":100,"Count":5616},"Items":[...]}.
//
// ⚠️ THE ORDERS ENDPOINT RETURNS BROKEN GROSS FIGURES — see correctOrdersGross.
package twocheckout

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	defaultBaseURL = "https://api.2checkout.com"
	apiPathPrefix  = "/rest/6.0"
	pageLimit      = 100
	// 2Checkout refuses page 201: "Only first 200 pages can be returned!". With
	// pageLimit=100 that is a hard 20,000-record ceiling per query — the only way
	// past it is to narrow the query, not to page harder.
	maxPages    = 200
	httpTimeout = 120 * time.Second
	// authDateLayout is not negotiable — RFC3339 gets a 401.
	authDateLayout = "2006-01-02 15:04:05"
)

type tableConfig struct {
	path        string
	primaryKeys []string
	// dateFiltered: the endpoint accepts StartDate/EndDate, so the interval is
	// pushed server-side. Endpoints without it are full snapshots every run.
	dateFiltered bool
	// fixGross: apply the cumulative-gross correction (orders only).
	fixGross bool
	// modifiedAfter: endpoint filters on ModifiedAfter (Y-m-d) instead of
	// StartDate/EndDate. Also the correct incremental key — it catches CHANGED
	// records, not just newly created ones.
	modifiedAfter bool
	// minColumns are columns that MUST exist in the emitted batch even when every
	// row in it is null.
	//
	// This exists because an untyped column can vanish before it reaches the
	// destination. arrowconv keeps a key that is present-but-null in every row
	// (twocheckout_test.go logs it), so the loss is not there — what it cannot do is
	// give such a column a type. It lands as UnknownArrowType and is dropped further
	// along the schema-inference path used whenever KnownSchema is false. Declaring
	// the column supplies the type, which is what makes it survive.
	//
	// Whether 2Checkout omits these keys entirely or sends them empty is not
	// distinguishable from the destination shape alone, which is what logObservedKeys
	// is for. Either way the column now exists.
	//
	// Declared columns are ADDITIVE — arrowconv seeds the field list from them and
	// still appends every other key it finds, so this pins a floor without freezing
	// the vendor's shape.
	minColumns []schema.Column
}

var supportedTables = map[string]tableConfig{
	// minColumns names the API's CamelCase keys, not the snake_case destination
	// names — the snake_casing happens downstream (Source -> source,
	// ExternalReference -> external_reference), consistent with RefNo -> ref_no.
	// The key is (RefNo, Status), not RefNo alone, and that is load-bearing. An
	// order MOVES between statuses over its life (COMPLETE -> REFUND -> REVERSED).
	// Keyed on RefNo alone, `merge` overwrites the earlier snapshot and only the
	// FINAL status survives — which destroys the original amounts, because 2Checkout
	// NEGATES money on a refund row. Keeping both rows lets a consumer read the
	// charge from the COMPLETE row and the refund from the REFUND row.
	//
	// Note this does not recover history: /orders/ returns each order's CURRENT
	// state only, so a backfill can never re-observe a status an order has already
	// left. It preserves transitions from the first load onwards.
	//
	// Changing this key changes the destination's sort order on destinations that
	// derive it from the primary key, which is a breaking change for existing
	// tables rather than a transparent one.
	"orders": {
		path: "/orders/", primaryKeys: []string{"RefNo", "Status"}, dateFiltered: true, fixGross: true,
		minColumns: []schema.Column{
			{Name: "Source", DataType: schema.TypeString, Nullable: true},
			{Name: "ExternalReference", DataType: schema.TypeString, Nullable: true},
			// ApproveStatus is 2Checkout's own approval flag (`FRAUD` marks a rejected
			// order) and was absent from the destination without this declaration.
			// VendorApproveStatus is a DIFFERENT field, observed as 'OK' on every row,
			// so it is not a substitute.
			{Name: "ApproveStatus", DataType: schema.TypeString, Nullable: true},
			// Declared for shape parity so the column exists rather than vanishing
			// untyped.
			{Name: "AffiliateCommission", DataType: schema.TypeFloat64, Nullable: true},
		},
	},
	// Uses ModifiedAfter, NOT StartDate/EndDate. The endpoint refuses to page past
	// 20k results, so an unfiltered pull of a large account is impossible rather
	// than merely slow — and StartDate/EndDate and ProductCode are silently IGNORED
	// here (the Count comes back identical). ModifiedAfter is the filter this
	// endpoint actually honours.
	//
	// It is also the better incremental key: it catches subscriptions that CHANGED,
	// not merely ones created in the window, which is what `merge` wants. The
	// trade-off is that a narrow window returns recently-TOUCHED records, not
	// recently-created ones, so it cannot be read as "everything since date X".
	"subscriptions": {path: "/subscriptions/", primaryKeys: []string{"SubscriptionReference"}, modifiedAfter: true},
	"products":      {path: "/products/", primaryKeys: []string{"ProductCode"}},
	// `code`, NOT `PromotionCode`. The LIST payload is snake_case and carries no
	// PromotionCode field — that name only exists on the /promotions/{code}/ detail
	// endpoint. ingestr snake_cases declared primary keys (RefNo -> ref_no), so the
	// wrong name surfaces late as a missing-column error at the destination.
	"promotions": {path: "/promotions/", primaryKeys: []string{"code"}},
}

// customers is deliberately absent: REST 6.0 exposes only /customers/search/,
// which REQUIRES an Email parameter. There is no way to enumerate the customer
// base, so there is nothing to ingest. Customer identity arrives on orders and
// subscriptions instead.

func supportedTableNames() string {
	names := make([]string, 0, len(supportedTables))
	for n := range supportedTables {
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

type Source struct {
	merchantCode string
	secretKey    string
	baseURL      string
	client       *http.Client
}

func NewTwoCheckoutSource() *Source {
	return &Source{baseURL: defaultBaseURL, client: &http.Client{Timeout: httpTimeout}}
}

func (s *Source) Schemes() []string { return []string{"twocheckout"} }

// The orders endpoint filters server-side on StartDate/EndDate and the rest are
// snapshots, so the source owns incrementality either way.
func (s *Source) HandlesIncrementality() bool { return true }

func (s *Source) Connect(ctx context.Context, uri string) error {
	parsed, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid twocheckout URI: %w", err)
	}
	q := parsed.Query()
	s.merchantCode = q.Get("merchant_code")
	s.secretKey = q.Get("secret_key")
	if s.merchantCode == "" || s.secretKey == "" {
		return fmt.Errorf("merchant_code and secret_key are required: twocheckout://?merchant_code=<code>&secret_key=<key>")
	}
	if b := q.Get("base_url"); b != "" {
		s.baseURL = strings.TrimRight(b, "/")
	}
	return nil
}

func (s *Source) Close(ctx context.Context) error { return nil }

// authHeader builds X-Avangate-Authentication for this instant. The hash covers
// len(code)+code+len(date)+date, and the date format is exact — see the package
// doc for why RFC3339 fails.
func (s *Source) authHeader(now time.Time) string {
	date := now.UTC().Format(authDateLayout)
	payload := fmt.Sprintf("%d%s%d%s", len(s.merchantCode), s.merchantCode, len(date), date)
	mac := hmac.New(sha256.New, []byte(s.secretKey))
	mac.Write([]byte(payload))
	return fmt.Sprintf(`code="%s" date="%s" hash="%s" algo="sha256"`,
		s.merchantCode, date, hex.EncodeToString(mac.Sum(nil)))
}

func (s *Source) get(ctx context.Context, path string, q url.Values) (json.RawMessage, error) {
	u := s.baseURL + apiPathPrefix + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// Re-stamped per request: the server tolerates only a few minutes of skew.
	req.Header.Set("X-Avangate-Authentication", s.authHeader(time.Now()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("2checkout %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", path, err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		// Do not echo the body — it is an auth failure, not user-actionable data.
		return nil, fmt.Errorf("2checkout %s: 401 unauthorized (check merchant_code/secret_key, and that the host clock is accurate — the signature embeds a timestamp)", path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("2checkout %s: status %d: %s", path, resp.StatusCode, truncate(string(body), 300))
	}
	return body, nil
}

func (s *Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tc, ok := supportedTables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported twocheckout table %q, supported tables are: %s", req.Name, supportedTableNames())
	}
	return &source.DynamicSourceTable{
		TableName:        req.Name,
		TablePrimaryKeys: tc.primaryKeys,
		TableStrategy:    config.StrategyMerge,
		KnownSchema:      false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("twocheckout source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tc, opts)
		},
	}, nil
}

func (s *Source) read(ctx context.Context, tc tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 4)
	go func() {
		defer close(results)
		if err := s.paginate(ctx, tc, nil, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

func (s *Source) paginate(ctx context.Context, tc tableConfig, extra url.Values, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	// Union of raw API keys seen this run. Cheap, and it is the only way a field
	// the vendor stopped sending (or that we never noticed) becomes visible: the
	// destination shape alone cannot distinguish "absent" from "null everywhere".
	seenKeys := map[string]struct{}{}
	for page := 1; ; page++ {
		q := url.Values{}
		for k, vs := range extra {
			for _, v := range vs {
				q.Set(k, v)
			}
		}
		// Capitalised, like every 2Checkout parameter.
		q.Set("Page", fmt.Sprintf("%d", page))
		q.Set("Limit", fmt.Sprintf("%d", pageLimit))
		if tc.modifiedAfter && opts.IntervalStart != nil {
			q.Set("ModifiedAfter", opts.IntervalStart.UTC().Format("2006-01-02"))
		}
		if tc.dateFiltered {
			if opts.IntervalStart != nil {
				q.Set("StartDate", opts.IntervalStart.UTC().Format("2006-01-02"))
			}
			if opts.IntervalEnd != nil {
				q.Set("EndDate", opts.IntervalEnd.UTC().Format("2006-01-02"))
			}
		}

		body, err := s.get(ctx, tc.path, q)
		if err != nil {
			return err
		}
		if tc.fixGross {
			body = correctOrdersGross(body)
		}

		var env struct {
			Items      []map[string]any `json:"Items"`
			Pagination struct {
				Page  int `json:"Page"`
				Limit int `json:"Limit"`
				Count int `json:"Count"`
			} `json:"Pagination"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			return fmt.Errorf("decode %s: %w", tc.path, err)
		}
		if len(env.Items) == 0 {
			return nil
		}

		// ⚠️ DO NOT GATE ON Pagination.Count. Verifone returns the ACCOUNT-WIDE
		// total there, not the count for the filtered query — it comes back as an
		// identical 48417 whether you filter by ProductCode, by date, or not at
		// all. Gating on it made every subscriptions pull fail even though the
		// filter was working. Truncation is instead detected where it actually
		// happens: hitting the page ceiling below.

		logObservedKeys(tc.path, env.Items, seenKeys)
		if err := emit(ctx, env.Items, tc.minColumns, opts, results); err != nil {
			return err
		}
		if len(env.Items) < pageLimit {
			return nil
		}
		// Real truncation guard: the API rejects page 201 outright, so reaching
		// the ceiling with a full page means there IS more data we cannot reach.
		// Fail rather than return a partial table that looks complete.
		if page >= maxPages {
			return fmt.Errorf("2checkout %s hit the hard %d-page ceiling (%d records) with a full "+
				"page — there is more data the API will not return. Narrow the query further",
				tc.path, maxPages, maxPages*pageLimit)
		}
	}
}

// correctOrdersGross repairs GrossPrice / GrossDiscountedPrice on an /orders/
// list response.
//
// ⚠️ THIS IS THE ONE PLACE THIS CONNECTOR DEVIATES FROM "RAW = VERBATIM VENDOR
// SHAPE", AND IT IS DELIBERATE.
//
// Verifone's list endpoint serialises those two fields as a RUNNING CUMULATIVE
// TOTAL across the page: each order's gross is its own gross plus every gross
// before it. It is an upstream bug. NetPrice / NetDiscountedPrice / VAT are
// correct, and gross == net + vat holds for every order (including refunds,
// where all three are negative), so the correct value is recomputable exactly.
//
// Ingesting the raw field would put silently, plausibly WRONG revenue in the
// warehouse — the worst kind of wrong, because nothing looks broken. It would
// The rewrite is surgical and self-disabling: an epsilon guard makes it a no-op
// once upstream is fixed, so a correct payload passes through untouched.
func correctOrdersGross(raw json.RawMessage) json.RawMessage {
	const epsilon = 0.005

	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return raw
	}
	itemsRaw, ok := env["Items"]
	if !ok {
		return raw
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		return raw
	}

	changed := false
	for _, it := range items {
		net, okNet := numField(it, "NetPrice")
		vat, okVat := numField(it, "VAT")
		if !okNet || !okVat {
			continue
		}
		if gross, ok := numField(it, "GrossPrice"); ok {
			if want := round2(net + vat); math.Abs(gross-want) > epsilon {
				it["GrossPrice"] = jsonNum(want)
				changed = true
			}
		}
		if netDisc, ok := numField(it, "NetDiscountedPrice"); ok {
			if grossDisc, ok := numField(it, "GrossDiscountedPrice"); ok {
				if want := round2(netDisc + vat); math.Abs(grossDisc-want) > epsilon {
					it["GrossDiscountedPrice"] = jsonNum(want)
					changed = true
				}
			}
		}
	}
	if !changed {
		return raw
	}
	fixed, err := json.Marshal(items)
	if err != nil {
		return raw
	}
	env["Items"] = fixed
	out, err := json.Marshal(env)
	if err != nil {
		return raw
	}
	return out
}

func numField(m map[string]json.RawMessage, key string) (float64, bool) {
	raw, ok := m[key]
	if !ok {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, false
	}
	return f, true
}

func jsonNum(v float64) json.RawMessage {
	return json.RawMessage(fmt.Sprintf("%.2f", v))
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// logObservedKeys records every raw key the API returned and logs the union the
// first time it grows. Debug-level: it is diagnostic, not per-row noise.
func logObservedKeys(path string, items []map[string]any, seen map[string]struct{}) {
	grew := false
	for _, it := range items {
		for k := range it {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				grew = true
			}
		}
	}
	if !grew {
		return
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	config.Debug("[2CHECKOUT] %s raw keys now %d: %s", path, len(keys), strings.Join(keys, ", "))
}

func emit(ctx context.Context, items []map[string]any, cols []schema.Column, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}
	rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, cols, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("convert twocheckout rows to arrow: %w", err)
	}
	select {
	case results <- source.RecordBatchResult{Batch: rec}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
