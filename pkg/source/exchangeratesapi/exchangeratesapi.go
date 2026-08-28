// Package exchangeratesapi is a source for api.exchangeratesapi.io (an APILayer product).
//
// ══ WHY THIS EXISTS WHEN `frankfurter` ALREADY SERVES EXCHANGE RATES ════════════════════
// frankfurter serves the ECB reference rates: free, keyless, ~30 currencies, and NO WEEKEND
// ROWS, because the ECB does not publish on weekends. That is the right default and remains
// the fleet's source of record.
//
// This source exists for the PAID exchangeratesapi.io plan, which does publish weekends and
// ~172 currencies with a switchable base. Measured against a live paid account:
//
//	                    frankfurter (ECB)   exchangeratesapi (paid)
//	currencies                    ~30                        172
//	weekend rows                    0                        yes
//	base                          EUR                switchable (CZK verified)
//
// The weekend rows are the point. A pipeline that joins FX on an EXACT DATE rather than
// ASOF turns a missing Saturday into a zero rather than a carried-forward rate.
//
// ⚠️ ON THE FREE TIER THIS SOURCE IS POINTLESS. exchangeratesapi.io's free tier IS ECB data,
// base EUR only — identical numbers to `frankfurter`, but with a key to rotate and leak.
// Use frankfurter unless you specifically need the paid feed.
//
// ⚠️ HISTORICAL RATES ARE NOT REPRODUCIBLE, so do not use this to "rebuild" history.
// The API answers with what it believes today; asking again later gives a different answer
// for the same past date. Measured across 16 years of stored rates, two collectors
// overlapping on 32,089 (date, currency) pairs DISAGREED on 31,878 of them (99.3%), by up to
// 10.1%. Stored rates are a record of what was quoted at the time and can only be preserved,
// never regenerated.
package exchangeratesapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://api.exchangeratesapi.io/v1/"
	// The paid plan we run is not rate-limit documented per-second; these mirror the
	// frankfurter source's politeness rather than a published ceiling.
	rateLimit      = 10
	rateLimitBurst = 5

	// maxBackfillDays caps a single run. ⚠️ THIS SOURCE COSTS ONE HTTP REQUEST PER DAY —
	// see readExchangeRates for why there is no bulk endpoint available to us. A nightly asks for
	// 1-2 days; a week's catch-up is 7 requests. An accidental --interval-start of 2010
	// would otherwise fire 6,000+ requests and burn the monthly quota in one run, so it is
	// refused loudly instead. Backfill history from the frozen table, not from here.
	maxBackfillDays = 400
)

var supportedTables = []string{
	"exchange_rates",
	"latest",
	"symbols",
}

// rateFields are the emitted column names. Renaming them (for example to frankfurter's
// currency_code/base_currency/rate) is a breaking change for any existing destination table:
// future rows land in a different column set and silently stop deduplicating against
// against history.
var rateFields = []schema.Column{
	{Name: "date", DataType: schema.TypeDate, Nullable: false},
	{Name: "base", DataType: schema.TypeString, Nullable: false},
	{Name: "currency", DataType: schema.TypeString, Nullable: false},
	{Name: "exchange_rate", DataType: schema.TypeFloat64, Nullable: true},
}

var symbolFields = []schema.Column{
	{Name: "currency", DataType: schema.TypeString, Nullable: false},
	{Name: "currency_name", DataType: schema.TypeString, Nullable: false},
}

type Source struct {
	client    *ingestrhttp.Client
	accessKey string
	base      string
}

func New() *Source { return &Source{} }

func (s *Source) HandlesIncrementality() bool { return true }

func (s *Source) Schemes() []string { return []string{"exchangeratesapi"} }

// parseURI extracts the access key and base currency.
//
// ⚠️ THE ACCESS KEY IS A SECRET AND MUST NEVER REACH A LOG. It is a query parameter on every
// request, so anything that logs a URL leaks it — which is why no Debug call in this file
// prints an endpoint, unlike the frankfurter source it was modelled on. Log dates, not URLs.
func parseURI(uri string) (accessKey, base string, err error) {
	if !strings.HasPrefix(uri, "exchangeratesapi://") {
		return "", "", fmt.Errorf("invalid exchangeratesapi URI: must start with exchangeratesapi://")
	}

	rest := strings.TrimPrefix(uri, "exchangeratesapi://")
	parts := strings.SplitN(rest, "?", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("exchangeratesapi URI requires an access_key query parameter")
	}

	values, err := url.ParseQuery(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("failed to parse exchangeratesapi URI query: %w", err)
	}

	accessKey = values.Get("access_key")
	if accessKey == "" {
		return "", "", fmt.Errorf("exchangeratesapi URI requires a non-empty access_key")
	}

	base = strings.ToUpper(values.Get("base"))
	if base == "" {
		// EUR is the API's own default. It is deliberately NOT defaulted to CZK: a silent
		// base change is the kind of thing that produces plausible, wrong money.
		base = "EUR"
	}

	return accessKey, base, nil
}

func (s *Source) Connect(ctx context.Context, uri string) error {
	accessKey, base, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.accessKey = accessKey
	s.base = base
	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
	)
	config.Debug("[EXCHANGERATESAPI] Connected, base currency: %s", s.base)
	return nil
}

func (s *Source) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	base := s.base

	// Base may be given inline as "exchange_rates:CZK", matching the frankfurter convention.
	if strings.Contains(tableName, ":") {
		parts := strings.SplitN(tableName, ":", 2)
		tableName = parts[0]
		if b := strings.ToUpper(strings.TrimSpace(parts[1])); b != "" {
			base = b
		}
	}

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	tableSchema, primaryKeys := getSchema(tableName)

	incrementalKey := ""
	strategy := config.StrategyReplace
	switch tableName {
	case "exchange_rates":
		incrementalKey = "date"
		strategy = config.StrategyMerge
	case "latest":
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, base, opts)
		},
	}, nil
}

func getSchema(table string) (*schema.TableSchema, []string) {
	var columns []schema.Column
	var primaryKeys []string

	switch table {
	case "exchange_rates", "latest":
		columns = rateFields
		// Matches the destination's ReplicatedReplacingMergeTree sorting key.
		primaryKeys = []string{"date", "base", "currency"}
	case "symbols":
		columns = symbolFields
		primaryKeys = []string{"currency"}
	}

	return &schema.TableSchema{Columns: columns, PrimaryKeys: primaryKeys}, primaryKeys
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func (s *Source) read(ctx context.Context, table, base string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "symbols":
			err = s.readSymbols(ctx, opts, results)
		case "latest":
			err = s.readLatest(ctx, base, opts, results)
		case "exchange_rates":
			err = s.readExchangeRates(ctx, base, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// apiError is the error envelope. ⚠️ NOTE THE SHAPE: a FAILED response carries no `success`
// field at all — it is `{"error": {"code": ..., "message": ...}}` — while a successful one is
// `{"success": true, ...}`. So "success is false" is the wrong test; absence is the signal.
// Verified live 2026-08-13: a restricted endpoint returns HTTP 403 and a bad key HTTP 401,
// both with this body, so the HTTP status is checked too rather than trusted alone.
type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// checkResponse turns a non-2xx or an error-shaped body into a Go error.
// The raw body is deliberately NOT included verbatim: it echoes the request in some APILayer
// error classes, and the request contains the access key.
func checkResponse(status int, body []byte) error {
	var apiErr apiError
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Code != "" {
		switch apiErr.Error.Code {
		case "function_access_restricted":
			return fmt.Errorf("exchangeratesapi rejected the request as out of plan (%s): this source needs a PAID plan; the free tier is ECB data and the `frankfurter` source serves that without a key", apiErr.Error.Code)
		case "invalid_access_key":
			return fmt.Errorf("exchangeratesapi rejected the access key (%s)", apiErr.Error.Code)
		default:
			return fmt.Errorf("exchangeratesapi error %s: %s", apiErr.Error.Code, apiErr.Error.Message)
		}
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("exchangeratesapi request failed with HTTP %d", status)
	}
	return nil
}

type ratesResponse struct {
	Success bool               `json:"success"`
	Base    string             `json:"base"`
	Date    string             `json:"date"`
	Rates   map[string]float64 `json:"rates"`
}

// get issues a request, appending the access key. Callers pass a path plus already-encoded
// query params WITHOUT the key.
func (s *Source) get(ctx context.Context, path, query string) ([]byte, error) {
	endpoint := path + "?access_key=" + url.QueryEscape(s.accessKey)
	if query != "" {
		endpoint += "&" + query
	}
	resp, err := s.client.R(ctx).Get(endpoint)
	if err != nil {
		// ⚠️ %w on the transport error would embed the URL, and the URL carries the key.
		return nil, fmt.Errorf("exchangeratesapi request failed (transport error contacting %s)", path)
	}
	if err := checkResponse(resp.StatusCode(), resp.Body()); err != nil {
		return nil, err
	}
	return resp.Body(), nil
}

func (s *Source) readSymbols(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	body, err := s.get(ctx, "symbols", "")
	if err != nil {
		return err
	}

	var result struct {
		Success bool              `json:"success"`
		Symbols map[string]string `json:"symbols"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse symbols response: %w", err)
	}

	items := make([]map[string]interface{}, 0, len(result.Symbols))
	codes := sortedKeys(result.Symbols)
	for _, code := range codes {
		items = append(items, map[string]interface{}{
			"currency":      code,
			"currency_name": result.Symbols[code],
		})
	}

	if len(items) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, symbolFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert symbols to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
	}
	config.Debug("[EXCHANGERATESAPI] Fetched %d symbols", len(items))
	return nil
}

func (s *Source) readLatest(ctx context.Context, base string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	body, err := s.get(ctx, "latest", "base="+url.QueryEscape(base))
	if err != nil {
		return err
	}

	var result ratesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse latest response: %w", err)
	}

	items := flattenRates(result.Date, result.Base, result.Rates)
	if len(items) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, rateFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert latest rates to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
	}
	config.Debug("[EXCHANGERATESAPI] Fetched %d rates for %s", len(items), result.Date)
	return nil
}

// readExchangeRates walks the requested interval ONE DAY AT A TIME.
//
// ⚠️ THERE IS NO BULK ENDPOINT AVAILABLE TO US. The API does have /timeseries, and it is the
// obvious way to do this — but it is plan-gated: it returns HTTP 403
// `function_access_restricted` on plans where the single-date /v1/{date} endpoint works fine.
// So this is deliberately N requests for N days, not an oversight. On a plan that includes
// /timeseries, switching is a contained change to this one function.
func (s *Source) readExchangeRates(ctx context.Context, base string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	now := time.Now().UTC()

	start := toDate(opts.IntervalStart)
	if start.IsZero() {
		start = now.AddDate(0, 0, -1)
	}
	end := toDate(opts.IntervalEnd)
	if end.IsZero() {
		end = now
	}
	if end.Before(start) {
		return fmt.Errorf("interval end %s is before interval start %s",
			end.Format("2006-01-02"), start.Format("2006-01-02"))
	}

	days := int(end.Sub(start).Hours()/24) + 1
	if days > maxBackfillDays {
		return fmt.Errorf(
			"refusing to fetch %d days: this source costs one request per day (no bulk endpoint on this plan) and the cap is %d. "+
				"Historical rates are not reproducible from this API anyway — backfill from the frozen table and use this source going forward",
			days, maxBackfillDays)
	}

	config.Debug("[EXCHANGERATESAPI] Fetching %d day(s) from %s to %s, base %s",
		days, start.Format("2006-01-02"), end.Format("2006-01-02"), base)

	var allItems []map[string]interface{}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		day := d.Format("2006-01-02")

		body, err := s.get(ctx, day, "base="+url.QueryEscape(base))
		if err != nil {
			return fmt.Errorf("failed to fetch rates for %s: %w", day, err)
		}

		var result ratesResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse rates response for %s: %w", day, err)
		}

		// Trust the date WE asked for, not the one echoed back. The API answers a
		// not-yet-published date with the most recent one it has, which would silently
		// write today's rate under yesterday's key.
		allItems = append(allItems, flattenRates(day, result.Base, result.Rates)...)
	}

	if len(allItems) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, rateFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert exchange rates to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
	}

	config.Debug("[EXCHANGERATESAPI] Fetched %d rate rows across %d day(s)", len(allItems), days)
	return nil
}

// flattenRates turns one day's map into rows, including the base->base identity row.
//
// The base row (rate 1.0) is deliberate and matches the frankfurter source. Without it,
// converting an amount already in the base currency finds no row and yields NULL — which on
// destinations that coalesce nulls in joins becomes a silent 0 rather than an error.
func flattenRates(date, base string, rates map[string]float64) []map[string]interface{} {
	base = strings.ToUpper(base)
	items := make([]map[string]interface{}, 0, len(rates)+1)
	items = append(items, map[string]interface{}{
		"date":          date,
		"base":          base,
		"currency":      base,
		"exchange_rate": 1.0,
	})

	for _, code := range sortedKeys(rates) {
		if strings.EqualFold(code, base) {
			continue // never emit the base twice
		}
		items = append(items, map[string]interface{}{
			"date":          date,
			"base":          base,
			"currency":      strings.ToUpper(code),
			"exchange_rate": rates[code],
		})
	}
	return items
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func toDate(v interface{}) time.Time {
	switch t := v.(type) {
	case time.Time:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	case *time.Time:
		if t != nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		}
	case string:
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, time.UTC)
		}
		if parsed, err := time.Parse("2006-01-02", t); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

var _ source.Source = (*Source)(nil)
