package twilio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://api.twilio.com"

	// Twilio caps list responses at 1000 items per page (default 50).
	maxPageSize = 1000

	// Twilio enforces an account-level concurrency limit (no fixed rps) and
	// returns HTTP 429 / error 20429 when exceeded; 10 rps stays safely under.
	rateLimit      = 10.0
	rateLimitBurst = 5

	// Server-side date filters on the 2010-04-01 API accept day granularity.
	dateParamLayout = "2006-01-02"
)

// tableConfig describes how to read and incrementally filter a Twilio resource.
// Mutable resources filter client-side on date_updated; Twilio's list APIs have no server-side updated filter.
type tableConfig struct {
	resource       string // path segment under /Accounts/{sid}/, e.g. "Messages.json"
	responseKey    string // envelope key holding the array, e.g. "messages"
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
	dateField      string            // row timestamp field for exact client-side filtering ("" = none)
	startParam     string            // server-side "on or after" query param ("" = none)
	endParam       string            // server-side "on or before" query param ("" = none)
	extraParams    map[string]string // static query params always sent (e.g. IncludeSoftDeleted)
}

var supportedTables = map[string]tableConfig{
	// Redaction empties a message body without bumping date_updated, and messages can be
	// hard-deleted, so replace re-fetches everything and mirrors Twilio's current state.
	"messages": {
		resource:    "Messages.json",
		responseKey: "messages",
		primaryKeys: []string{"sid"},
		strategy:    config.StrategyReplace,
	},
	// Calls are mutable and their changes bump date_updated, so we merge filtering
	// client-side on it. Deletions aren't reflected, but call logs are rarely deleted.
	"calls": {
		resource:       "Calls.json",
		responseKey:    "calls",
		primaryKeys:    []string{"sid"},
		incrementalKey: "date_updated",
		strategy:       config.StrategyMerge,
		dateField:      "date_updated",
	},
	// Recordings are mutable and soft-delete: IncludeSoftDeleted surfaces deleted ones as
	// status="deleted" with a bumped date_updated, so merge picks the deletion up.
	"recordings": {
		resource:       "Recordings.json",
		responseKey:    "recordings",
		primaryKeys:    []string{"sid"},
		incrementalKey: "date_updated",
		strategy:       config.StrategyMerge,
		dateField:      "date_updated",
		extraParams:    map[string]string{"IncludeSoftDeleted": "true"},
	},
	"incoming_phone_numbers": {
		resource:    "IncomingPhoneNumbers.json",
		responseKey: "incoming_phone_numbers",
		primaryKeys: []string{"sid"},
		strategy:    config.StrategyReplace,
	},
	// Usage records are running per-category aggregates with no per-row updated_at,
	// so we always full-fetch the lifetime totals and replace (date scoping would
	// rebuild the table with only one window's aggregate).
	"usage_records": {
		resource:    "Usage/Records.json",
		responseKey: "usage_records",
		strategy:    config.StrategyReplace,
	},
}

type twilioCredentials struct {
	accountSID string
	authToken  string
	apiKey     string
	apiSecret  string
}

type TwilioSource struct {
	client     *httpclient.Client
	accountSID string
}

func NewTwilioSource() *TwilioSource {
	return &TwilioSource{}
}

func (s *TwilioSource) Schemes() []string {
	return []string{"twilio"}
}

func (s *TwilioSource) HandlesIncrementality() bool {
	return true
}

func (s *TwilioSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.accountSID = creds.accountSID

	// API Key + Secret is preferred; Account SID + Auth Token is the fallback.
	// Either way the Account SID stays in the request path.
	var auth httpclient.Authenticator
	if creds.apiKey != "" {
		auth = httpclient.NewBasicAuth(creds.apiKey, creds.apiSecret)
	} else {
		auth = httpclient.NewBasicAuth(creds.accountSID, creds.authToken)
	}

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(auth),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithHeader("Accept", "application/json"),
	)

	config.Debug("[TWILIO] connected for account %s", s.accountSID)
	return nil
}

func (s *TwilioSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (twilioCredentials, error) {
	if !strings.HasPrefix(uri, "twilio://") {
		return twilioCredentials{}, fmt.Errorf("invalid twilio URI: must start with twilio://")
	}

	rest := strings.TrimPrefix(uri, "twilio://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return twilioCredentials{}, fmt.Errorf("failed to parse twilio URI query: %w", err)
	}

	creds := twilioCredentials{
		accountSID: values.Get("account_sid"),
		authToken:  values.Get("auth_token"),
		apiKey:     values.Get("api_key"),
		apiSecret:  values.Get("api_secret"),
	}

	if creds.accountSID == "" {
		return twilioCredentials{}, fmt.Errorf("account_sid is required in twilio URI")
	}

	// Two supported auth modes: API Key SID + Secret, or Account SID + Auth Token.
	switch {
	case creds.apiKey != "":
		if creds.apiSecret == "" {
			return twilioCredentials{}, fmt.Errorf("api_secret is required when api_key is provided in twilio URI")
		}
	case creds.authToken != "":
		// ok
	default:
		return twilioCredentials{}, fmt.Errorf("twilio URI requires either auth_token, or api_key and api_secret")
	}

	return creds, nil
}

// usageGranularities maps a usage_records granularity modifier to its Twilio
// sub-resource path segment.
var usageGranularities = map[string]string{
	"daily":   "Daily",
	"monthly": "Monthly",
	"yearly":  "Yearly",
}

// resolveTableConfig resolves a table name (optionally with a ":granularity"
// modifier, e.g. "usage_records:daily") to its tableConfig. Granular usage rows
// are tied to a fixed period, so unlike the lifetime aggregate they load
// incrementally via merge on (account_sid, category, start_date).
func resolveTableConfig(name string) (tableConfig, error) {
	base, modifier, hasModifier := strings.Cut(name, ":")

	cfg, ok := supportedTables[base]
	if !ok {
		return tableConfig{}, fmt.Errorf("unsupported table: %s (supported: %s)", name, supportedTableNames())
	}
	if !hasModifier {
		return cfg, nil
	}

	if base != "usage_records" {
		return tableConfig{}, fmt.Errorf("table %s does not support a ':' modifier", base)
	}
	segment, ok := usageGranularities[strings.ToLower(modifier)]
	if !ok {
		return tableConfig{}, fmt.Errorf("invalid usage_records granularity %q (supported: daily, monthly, yearly)", modifier)
	}

	cfg.resource = "Usage/Records/" + segment + ".json"
	cfg.strategy = config.StrategyMerge
	cfg.primaryKeys = []string{"account_sid", "category", "start_date"}
	cfg.incrementalKey = "start_date"
	cfg.startParam = "StartDate"
	cfg.endParam = "EndDate"
	return cfg, nil
}

func (s *TwilioSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	cfg, err := resolveTableConfig(tableName)
	if err != nil {
		return nil, err
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    cfg.primaryKeys,
		TableIncrementalKey: cfg.incrementalKey,
		TableStrategy:       cfg.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("twilio source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	_, ok := supportedTables[table]
	return ok
}

func supportedTableNames() string {
	names := make([]string, 0, len(supportedTables))
	for name := range supportedTables {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (s *TwilioSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		cfg, err := resolveTableConfig(table)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}

		if err := s.paginateAndSend(ctx, cfg, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// paginateAndSend walks Twilio's next_page_uri pagination, streaming one Arrow batch per page.
// Query params are set only on the first request; Twilio embeds them in next_page_uri thereafter.
func (s *TwilioSource) paginateAndSend(ctx context.Context, cfg tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	path := fmt.Sprintf("/2010-04-01/Accounts/%s/%s", s.accountSID, cfg.resource)

	query := map[string]string{"PageSize": strconv.Itoa(maxPageSize)}
	if cfg.startParam != "" && opts.IntervalStart != nil {
		query[cfg.startParam] = opts.IntervalStart.UTC().Format(dateParamLayout)
	}
	if cfg.endParam != "" && opts.IntervalEnd != nil {
		query[cfg.endParam] = opts.IntervalEnd.UTC().Format(dateParamLayout)
	}
	for k, v := range cfg.extraParams {
		query[k] = v
	}

	first := true
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx)
		if first {
			req = req.SetQueryParams(query)
		}

		resp, err := req.Get(path)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", cfg.resource, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("twilio API %s returned status %d: %s", cfg.resource, resp.StatusCode(), resp.String())
		}

		items, nextPageURI, err := decodeListResponse(resp.Body(), cfg.responseKey)
		if err != nil {
			return fmt.Errorf("failed to parse %s response: %w", cfg.resource, err)
		}

		if cfg.dateField != "" {
			items = filterItemsByInterval(items, cfg.dateField, opts.IntervalStart, opts.IntervalEnd)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", cfg.resource, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
			config.Debug("[TWILIO] %s: sent %d records (total: %d)", cfg.resource, len(items), totalSent)
		}

		if nextPageURI == "" {
			break
		}
		path = nextPageURI
		first = false
	}

	if totalSent == 0 {
		config.Debug("[TWILIO] no records found for %s", cfg.resource)
	}
	return nil
}

func decodeListResponse(body []byte, key string) ([]map[string]interface{}, string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var raw map[string]interface{}
	if err := decoder.Decode(&raw); err != nil {
		return nil, "", err
	}

	nextPageURI := ""
	if n, ok := raw["next_page_uri"].(string); ok {
		nextPageURI = n
	}

	arr, ok := raw[key].([]interface{})
	if !ok {
		return nil, nextPageURI, nil
	}

	items := make([]map[string]interface{}, 0, len(arr))
	for _, v := range arr {
		if m, ok := v.(map[string]interface{}); ok {
			items = append(items, m)
		}
	}

	return items, nextPageURI, nil
}

func filterItemsByInterval(items []map[string]interface{}, field string, start, end *time.Time) []map[string]interface{} {
	if field == "" || (start == nil && end == nil) {
		return items
	}

	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		ts, ok := parseTimestamp(item[field])
		if !ok {
			// Keep rows whose timestamp is missing/unparseable rather than dropping them.
			filtered = append(filtered, item)
			continue
		}
		if start != nil && ts.Before(start.UTC()) {
			continue
		}
		if end != nil && !ts.Before(end.UTC()) {
			continue
		}
		filtered = append(filtered, item)
	}

	return filtered
}

func parseTimestamp(raw interface{}) (time.Time, bool) {
	switch v := raw.(type) {
	case string:
		if v == "" {
			return time.Time{}, false
		}
		ts, err := dateparse.ParseAny(v)
		if err != nil {
			return time.Time{}, false
		}
		return ts.UTC(), true
	case time.Time:
		return v.UTC(), true
	default:
		return time.Time{}, false
	}
}
