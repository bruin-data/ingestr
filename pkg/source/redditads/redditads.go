package redditads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
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
	baseURL   = "https://ads-api.reddit.com/api/v3"
	userAgent = "ingestr/1.0"

	// Reddit does not publish numeric Ads API rate limits; it returns 429 with
	// X-RateLimit-Remaining/-Reset headers. We throttle conservatively client-side
	// (~0.8 req/s) and rely on the shared http client's retry-on-429 as a backstop
	// (enabled for POST via WithAllowNonIdempotentRetry in Connect).
	rateLimit      = 0.8
	rateLimitBurst = 5

	maxPageSize = 100
	maxPages    = 1000
	workerCount = 5
)

var monetaryFields = map[string]bool{
	"spend": true,
	"ecpm":  true,
	"cpc":   true,
}

var entityTables = []string{
	"accounts",
	"campaigns",
	"ad_groups",
	"ads",
	"custom_audiences",
	"saved_audiences",
	"pixels",
	"funding_instruments",
}

// entityIncrementalKey maps each entity table to the timestamp field its objects
// carry (verified against live API responses). These use client-side interval
// filtering + merge (Reddit exposes no server-side time filter on entity
// endpoints). saved_audiences uses updated_at; every other entity uses
// modified_at. Tables absent here (funding_instruments has only
// start_time/end_time) use a full replace.
var entityIncrementalKey = map[string]string{
	"accounts":         "modified_at",
	"campaigns":        "modified_at",
	"ad_groups":        "modified_at",
	"ads":              "modified_at",
	"custom_audiences": "modified_at",
	"saved_audiences":  "updated_at",
	"pixels":           "modified_at",
}

var levelIDFields = map[string]string{
	"ACCOUNT":  "account_id",
	"CAMPAIGN": "campaign_id",
	"AD_GROUP": "ad_group_id",
	"AD":       "ad_id",
}

var validLevels = map[string]bool{
	"ACCOUNT": true, "CAMPAIGN": true, "AD_GROUP": true, "AD": true,
}

var validBreakdowns = map[string]bool{
	"date": true, "country": true, "region": true, "community": true,
	"placement": true, "device_os": true, "gender": true, "interest": true,
	"keyword": true, "carousel_card": true,
}

var validMetrics = map[string]bool{
	"IMPRESSIONS": true, "REACH": true, "CLICKS": true, "SPEND": true,
	"ECPM": true, "CTR": true, "CPC": true, "CONVERSIONS": true,
	"CONVERSION_ROAS": true, "TOTAL_ITEMS": true, "TOTAL_VALUE": true,
	"AVG_VALUE": true, "REDDIT_LEADS": true, "COMMENTS_PAGE_VIEWS": true,
	"COMMENT_UPVOTES": true, "COMMENT_DOWNVOTES": true, "VIEWER_COMMENTS": true,
	"VIDEO_STARTED": true, "VIDEO_WATCHED_3_SECONDS": true,
	"VIDEO_WATCHED_5_SECONDS": true, "VIDEO_WATCHED_25_PERCENT": true,
	"VIDEO_WATCHED_50_PERCENT": true, "VIDEO_WATCHED_75_PERCENT": true,
	"VIDEO_WATCHED_100_PERCENT": true, "VIDEO_WATCHED_6_SECONDS_RATE": true,
	"VIDEO_WATCHED_15_SECONDS_RATE": true,
}

type RedditAdsSource struct {
	accessToken string
	accountIDs  []string
	client      *httpclient.Client
}

func NewRedditAdsSource() *RedditAdsSource {
	return &RedditAdsSource{}
}

func (s *RedditAdsSource) HandlesIncrementality() bool {
	return true
}

func (s *RedditAdsSource) Schemes() []string {
	return []string{"redditads"}
}

func (s *RedditAdsSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.accessToken = creds.accessToken
	s.accountIDs = creds.accountIDs

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(s.accessToken)),
		httpclient.WithUserAgent(userAgent),
		// The reports endpoint is a POST; every request this source makes is a
		// read with no side effects, so allow retrying POSTs (otherwise resty
		// skips the 429/5xx retry for non-idempotent methods).
		httpclient.WithAllowNonIdempotentRetry(),
	)

	config.Debug("[REDDITADS] Connected successfully (accounts: %s)", strings.Join(s.accountIDs, ", "))
	return nil
}

func (s *RedditAdsSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

type redditAdsCredentials struct {
	accessToken string
	accountIDs  []string
}

func parseURI(uri string) (*redditAdsCredentials, error) {
	if !strings.HasPrefix(uri, "redditads://") {
		return nil, fmt.Errorf("invalid redditads URI: must start with redditads://")
	}

	rest := strings.TrimPrefix(uri, "redditads://")
	if rest == "" || rest == "?" {
		return nil, fmt.Errorf("access_token is required in redditads URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redditads URI query: %w", err)
	}

	accessToken := values.Get("access_token")
	if accessToken == "" {
		return nil, fmt.Errorf("access_token is required in redditads URI")
	}

	accountIDsRaw := values.Get("account_ids")
	if accountIDsRaw == "" {
		return nil, fmt.Errorf("account_ids is required in redditads URI")
	}

	accountIDs := splitAndTrim(accountIDsRaw, ",")
	if len(accountIDs) == 0 {
		return nil, fmt.Errorf("account_ids must contain at least one account ID")
	}

	return &redditAdsCredentials{
		accessToken: accessToken,
		accountIDs:  accountIDs,
	}, nil
}

func splitAndTrim(s string, sep string) []string {
	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func (s *RedditAdsSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if strings.HasPrefix(tableName, "custom:") {
		return s.getCustomReportTable(tableName)
	}

	if !isValidEntityTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s, or custom:<level>,<breakdowns>:<metrics>)", tableName, strings.Join(entityTables, ", "))
	}

	// Reddit entity endpoints expose no server-side time filter, but most objects
	// carry a modified_at timestamp, so those tables filter client-side and merge.
	// Tables without it (e.g. funding_instruments) fall back to a full replace.
	strategy := config.StrategyReplace
	incrementalKey := entityIncrementalKey[tableName]
	if incrementalKey != "" {
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableStrategy:       strategy,
		TableIncrementalKey: incrementalKey,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("redditads source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *RedditAdsSource) getCustomReportTable(tableName string) (source.SourceTable, error) {
	level, breakdowns, metrics, err := parseCustomTable(tableName)
	if err != nil {
		return nil, err
	}

	levelIDField := levelIDFields[level]
	primaryKeys := append([]string{levelIDField}, breakdowns...)

	hasDateBreakdown := slices.Contains(breakdowns, "date")

	incrementalKey := ""
	strategy := config.StrategyMerge
	if hasDateBreakdown {
		incrementalKey = "date"
	}

	return &source.DynamicSourceTable{
		TableName:           "custom_reports",
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("redditads source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readCustomReport(ctx, level, breakdowns, metrics, opts)
		},
	}, nil
}

func isValidEntityTable(table string) bool {
	return slices.Contains(entityTables, table)
}

func (s *RedditAdsSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "accounts":
			err = s.readAccounts(ctx, opts, results)
		case "campaigns":
			err = s.readPerAccount(ctx, "/campaigns", "campaigns", opts, results)
		case "ad_groups":
			err = s.readPerAccount(ctx, "/ad_groups", "ad_groups", opts, results)
		case "ads":
			err = s.readPerAccount(ctx, "/ads", "ads", opts, results)
		case "custom_audiences":
			err = s.readPerAccount(ctx, "/custom_audiences", "custom_audiences", opts, results)
		case "saved_audiences":
			err = s.readPerAccount(ctx, "/saved_audiences", "saved_audiences", opts, results)
		case "pixels":
			err = s.readPerAccount(ctx, "/pixels", "pixels", opts, results)
		case "funding_instruments":
			err = s.readPerAccount(ctx, "/funding_instruments", "funding_instruments", opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// --- helpers ---

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

func extractItems(body map[string]any) []map[string]any {
	dataRaw, ok := body["data"].([]any)
	if !ok {
		return nil
	}

	items := make([]map[string]any, 0, len(dataRaw))
	for _, d := range dataRaw {
		if item, ok := d.(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items
}

// filterItemsByInterval keeps items whose incrementalKey timestamp falls within
// [start, end]. Items missing the key are kept (so tables without the field are
// unaffected). A no-op when no key or no interval is set.
func filterItemsByInterval(items []map[string]any, incrementalKey string, start, end *time.Time) []map[string]any {
	if incrementalKey == "" || (start == nil && end == nil) {
		return items
	}

	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		raw, ok := item[incrementalKey]
		if !ok || raw == nil {
			filtered = append(filtered, item)
			continue
		}

		t, ok := parseTimestamp(raw)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if start != nil && t.Before(*start) {
			continue
		}
		if end != nil && t.After(*end) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func parseTimestamp(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case string:
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return parsed, true
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}

// extractReportItems pulls the rows from a v3 report response, where the
// records live under data.metrics (unlike entity endpoints, where data is the
// array itself).
func extractReportItems(body map[string]any) []map[string]any {
	data, ok := body["data"].(map[string]any)
	if !ok {
		return nil
	}
	metricsRaw, ok := data["metrics"].([]any)
	if !ok {
		return nil
	}

	items := make([]map[string]any, 0, len(metricsRaw))
	for _, m := range metricsRaw {
		if item, ok := m.(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items
}

func getNextURL(body map[string]any) string {
	pagination, ok := body["pagination"].(map[string]any)
	if !ok {
		return ""
	}
	nextURL, ok := pagination["next_url"].(string)
	if !ok {
		return ""
	}
	return nextURL
}

func convertMicrocurrency(items []map[string]any, metrics []string) {
	requested := make(map[string]bool)
	for _, m := range metrics {
		requested[strings.ToLower(m)] = true
	}

	for _, item := range items {
		for field := range monetaryFields {
			if !requested[field] {
				continue
			}
			val, ok := item[field]
			if !ok || val == nil {
				continue
			}
			switch v := val.(type) {
			case json.Number:
				f, err := v.Float64()
				if err == nil {
					item[field] = f / 1_000_000
				}
			case float64:
				item[field] = v / 1_000_000
			}
		}
	}
}

func parseCustomTable(table string) (level string, breakdowns []string, metrics []string, err error) {
	parts := strings.Split(table, ":")
	if len(parts) != 3 {
		return "", nil, nil, fmt.Errorf("invalid custom table format: expected custom:<level>,<breakdowns>:<metrics>")
	}

	dimensions := splitAndTrim(parts[1], ",")
	if len(dimensions) == 0 {
		return "", nil, nil, fmt.Errorf("at least a level is required in the dimensions segment")
	}

	level = strings.ToUpper(dimensions[0])
	if !validLevels[level] {
		return "", nil, nil, fmt.Errorf("invalid level %q: must be one of ACCOUNT, CAMPAIGN, AD_GROUP, AD", level)
	}

	breakdowns = make([]string, 0, len(dimensions)-1)
	for _, b := range dimensions[1:] {
		b = strings.ToLower(b)
		if !validBreakdowns[b] {
			return "", nil, nil, fmt.Errorf("invalid breakdown %q: must be one of %s", b, joinMapKeys(validBreakdowns))
		}
		breakdowns = append(breakdowns, b)
	}

	if len(breakdowns) > 2 {
		return "", nil, nil, fmt.Errorf("reddit ads supports at most 2 breakdowns per report")
	}

	metrics = make([]string, 0)
	for _, m := range splitAndTrim(parts[2], ",") {
		m = strings.ToUpper(m)
		if !validMetrics[m] {
			return "", nil, nil, fmt.Errorf("invalid metric %q: must be one of %s", m, joinMapKeys(validMetrics))
		}
		metrics = append(metrics, m)
	}

	if len(metrics) == 0 {
		return "", nil, nil, fmt.Errorf("at least one metric is required")
	}

	return level, breakdowns, metrics, nil
}

func joinMapKeys(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// --- entity table readers ---

func (s *RedditAdsSource) readAccounts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[REDDITADS] reading accounts")

	items := make([]map[string]any, 0, len(s.accountIDs))
	for _, accountID := range s.accountIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).Get("/ad_accounts/" + accountID)
		if err != nil {
			return fmt.Errorf("failed to fetch ad account %s: %w", accountID, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("redditads API /ad_accounts/%s returned status %d: %s", accountID, resp.StatusCode(), resp.String())
		}

		var body map[string]any
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse ad account response: %w", err)
		}

		if item, ok := body["data"].(map[string]any); ok {
			items = append(items, item)
		}
	}

	items = filterItemsByInterval(items, entityIncrementalKey["accounts"], opts.IntervalStart, opts.IntervalEnd)
	if len(items) == 0 {
		config.Debug("[REDDITADS] No accounts found matching account_ids")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert accounts to Arrow: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[REDDITADS] accounts: sent %d records", len(items))
	return nil
}

// readPerAccount fetches a paginated sub-resource for each account in parallel.
func (s *RedditAdsSource) readPerAccount(ctx context.Context, endpoint, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[REDDITADS] reading %s", label)

	sem := make(chan struct{}, workerCount)
	var mu sync.Mutex
	var firstErr error

	var wg sync.WaitGroup
	for _, accountID := range s.accountIDs {
		select {
		case <-ctx.Done():
			mu.Lock()
			if firstErr == nil {
				firstErr = ctx.Err()
			}
			mu.Unlock()
			// Stop spawning, but wait for already-running goroutines below so
			// none send on results after the caller closes the channel.
			goto done
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(acctID string) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := s.paginateEntity(ctx, acctID, endpoint, label, opts, results); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(accountID)
	}

done:
	wg.Wait()
	return firstErr
}

func (s *RedditAdsSource) paginateEntity(ctx context.Context, accountID, endpoint, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	fullEndpoint := fmt.Sprintf("/ad_accounts/%s%s", accountID, endpoint)
	currentURL := ""
	page := 1
	totalSent := 0

	for page <= maxPages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp *httpclient.Response
		var err error

		if currentURL != "" {
			resp, err = s.client.R(ctx).Get(currentURL)
		} else {
			resp, err = s.client.R(ctx).
				SetQueryParam("page_size", strconv.Itoa(maxPageSize)).
				Get(fullEndpoint)
		}
		if err != nil {
			return fmt.Errorf("failed to fetch %s for account %s: %w", label, accountID, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("redditads API %s returned status %d: %s", fullEndpoint, resp.StatusCode(), resp.String())
		}

		var body map[string]any
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		items := extractItems(body)
		if len(items) == 0 {
			break
		}

		for _, item := range items {
			item["account_id"] = accountID
		}

		// Client-side interval filtering (no server-side time filter on these
		// endpoints). Pagination still follows the unfiltered next_url so a page
		// with no in-range rows doesn't stop the scan early.
		filtered := filterItemsByInterval(items, entityIncrementalKey[label], opts.IntervalStart, opts.IntervalEnd)
		if len(filtered) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(filtered, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", label, err)
			}

			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(filtered)
			config.Debug("[REDDITADS] %s (account %s) page %d: sent %d records (total: %d)", label, accountID, page, len(filtered), totalSent)
		}

		nextURL := getNextURL(body)
		if nextURL == "" {
			break
		}
		if page >= maxPages {
			config.Debug("[REDDITADS] %s (account %s) hit maxPages cap (%d); more pages may exist", label, accountID, maxPages)
			break
		}
		currentURL = nextURL
		page++
	}

	return nil
}

// --- custom report reader ---

func (s *RedditAdsSource) readCustomReport(ctx context.Context, level string, breakdowns, metrics []string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		startDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate := time.Now().UTC()

		if opts.IntervalStart != nil {
			startDate = opts.IntervalStart.UTC()
		}
		if opts.IntervalEnd != nil {
			endDate = opts.IntervalEnd.UTC()
		}

		sem := make(chan struct{}, workerCount)
		var mu sync.Mutex
		var firstErr error

		var wg sync.WaitGroup
		for _, accountID := range s.accountIDs {
			select {
			case <-ctx.Done():
				mu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				mu.Unlock()
				// Stop spawning, but wait for in-flight goroutines below so none
				// send on results after the deferred close.
				goto done
			default:
			}

			wg.Add(1)
			sem <- struct{}{}
			go func(acctID string) {
				defer wg.Done()
				defer func() { <-sem }()

				if err := s.fetchReport(ctx, acctID, level, breakdowns, metrics, startDate, endDate, opts, results); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
				}
			}(accountID)
		}

	done:
		wg.Wait()
		if firstErr != nil {
			results <- source.RecordBatchResult{Err: firstErr}
		}
	}()

	return results, nil
}

func (s *RedditAdsSource) fetchReport(ctx context.Context, accountID, level string, breakdowns, metrics []string, startDate, endDate time.Time, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/ad_accounts/%s/reports", accountID)

	// Reddit Ads API v3 reports group by "breakdowns"; the level (account/campaign/
	// ad_group/ad) is the first breakdown dimension. Metrics go into "fields".
	// Breakdown/field enums are uppercase (the API is case-insensitive but we
	// normalize).
	reqBreakdowns := make([]string, 0, len(breakdowns)+1)
	reqBreakdowns = append(reqBreakdowns, strings.ToUpper(levelIDFields[level]))
	for _, b := range breakdowns {
		reqBreakdowns = append(reqBreakdowns, strings.ToUpper(b))
	}

	// starts_at/ends_at must be hourly-aligned RFC3339 (YYYY-MM-DDTHH:00:00Z) — the
	// API rejects any non-zero minute/second with 400. Truncate to the hour.
	body := map[string]any{
		"data": map[string]any{
			"breakdowns": reqBreakdowns,
			"fields":     metrics,
			"starts_at":  startDate.UTC().Truncate(time.Hour).Format(time.RFC3339),
			"ends_at":    endDate.UTC().Truncate(time.Hour).Format(time.RFC3339),
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal report request: %w", err)
	}

	resp, err := s.client.R(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(jsonBody).
		Post(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch report for account %s: %w", accountID, err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("redditads API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	var respBody map[string]any
	if err := jsonUseNumber(resp.Body(), &respBody); err != nil {
		return fmt.Errorf("failed to parse report response: %w", err)
	}

	items := extractReportItems(respBody)
	if len(items) == 0 {
		config.Debug("[REDDITADS] No report data for account %s", accountID)
		return nil
	}

	for _, item := range items {
		item["account_id"] = accountID
	}

	convertMicrocurrency(items, metrics)

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert report data to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[REDDITADS] report (account %s): sent %d records", accountID, len(items))
	return nil
}
