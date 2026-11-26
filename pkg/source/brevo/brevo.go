package brevo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
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
	baseURL = "https://api.brevo.com/v3"

	// Brevo General tier: /contacts/* allows 10 req/s (GET /smtp/blockedContacts allows more and shares it).
	contactsRateLimit      = 8.0
	contactsRateLimitBurst = 5
	// Brevo General tier: /smtp/* (templates, statistics) allows 300 req/hour.
	smtpRateLimit      = 300 * 0.8 / 3600.0
	smtpRateLimitBurst = 5
	// Brevo General tier: everything else (campaigns, CRM, ecommerce, /events, SMS and WhatsApp
	// statistics) allows 100 req/hour.
	otherRateLimit      = 100 * 0.8 / 3600.0
	otherRateLimitBurst = 3

	retryCount = 3
	// Hourly groups can report a reset close to an hour away; the wait must be allowed to reach it.
	retryMaxWait = time.Hour + time.Minute

	modifiedSinceLayout = "2006-01-02T15:04:05.000Z"
	dayLayout           = "2006-01-02"
	timeWindowLayout    = "2006-01-02T15:04:05.000Z07:00"
	defaultWindowDays   = 90
	// GET /events rejects ranges over 6 months.
	customEventsWindowDays = 180
)

type rateGroup int

const (
	groupContacts rateGroup = iota
	groupSMTP
	groupOther
)

type tableConfig struct {
	endpoint       string
	responseKey    string
	pageSize       int
	group          rateGroup
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
	params         map[string]string
	// maxWindowDays > 0 marks a day-windowed statistics table read with startDate/endDate.
	maxWindowDays int
	// includeArchived adds a status=archive pass; Brevo leaves archived campaigns out by default.
	includeArchived bool
	// modifiedSince sends the interval start as the modifiedSince filter.
	modifiedSince bool
	// pageNumbers pages with page=1,2,... instead of offset (only /companies).
	pageNumbers bool
	// singlePage reads one response per request window: the endpoint ignores limit and offset.
	singlePage bool
	// liftModifiedAt copies attributes.<name> to a top-level modifiedAt for CRM objects.
	liftModifiedAt string
	// eventDate lifts the day of the event's date into a DATE-like event_date column.
	eventDate bool
	// timeWindows reads startDate/endDate as RFC3339 instants in windows of customEventsWindowDays.
	timeWindows bool
}

var tableConfigs = map[string]tableConfig{
	"contacts": {
		endpoint:       "/contacts",
		responseKey:    "contacts",
		pageSize:       1000,
		group:          groupContacts,
		primaryKeys:    []string{"id"},
		incrementalKey: "modifiedAt",
		strategy:       config.StrategyMerge,
		params:         map[string]string{"sort": "asc"},
		modifiedSince:  true,
	},
	"lists": {
		endpoint:    "/contacts/lists",
		responseKey: "lists",
		pageSize:    50,
		group:       groupContacts,
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		params:      map[string]string{"sort": "asc"},
	},
	"segments": {
		endpoint:    "/contacts/segments",
		responseKey: "segments",
		pageSize:    50,
		group:       groupContacts,
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		params:      map[string]string{"sort": "asc"},
	},
	"folders": {
		endpoint:    "/contacts/folders",
		responseKey: "folders",
		pageSize:    50,
		group:       groupContacts,
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		params:      map[string]string{"sort": "asc"},
	},
	"contact_attributes": {
		endpoint:    "/contacts/attributes",
		responseKey: "attributes",
		group:       groupContacts,
		primaryKeys: []string{"category", "name"},
		strategy:    config.StrategyReplace,
	},
	"email_campaigns": {
		endpoint:        "/emailCampaigns",
		responseKey:     "campaigns",
		pageSize:        100,
		group:           groupOther,
		primaryKeys:     []string{"id"},
		strategy:        config.StrategyReplace,
		params:          map[string]string{"sort": "asc", "statistics": "globalStats", "excludeHtmlContent": "true"},
		includeArchived: true,
	},
	"sms_campaigns": {
		endpoint:        "/smsCampaigns",
		responseKey:     "campaigns",
		pageSize:        1000,
		group:           groupOther,
		primaryKeys:     []string{"id"},
		strategy:        config.StrategyReplace,
		params:          map[string]string{"sort": "asc"},
		includeArchived: true,
	},
	"email_templates": {
		endpoint:    "/smtp/templates",
		responseKey: "templates",
		pageSize:    1000,
		group:       groupSMTP,
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		params:      map[string]string{"sort": "asc"},
	},
	"transactional_email_events": {
		endpoint:       "/smtp/statistics/events",
		responseKey:    "events",
		pageSize:       5000,
		group:          groupSMTP,
		incrementalKey: "event_date",
		strategy:       config.StrategyDeleteInsert,
		params:         map[string]string{"sort": "asc"},
		maxWindowDays:  90,
		eventDate:      true,
	},
	"transactional_email_reports": {
		endpoint:       "/smtp/statistics/reports",
		responseKey:    "reports",
		pageSize:       30,
		group:          groupSMTP,
		incrementalKey: "date",
		strategy:       config.StrategyDeleteInsert,
		params:         map[string]string{"sort": "asc"},
		maxWindowDays:  30,
		singlePage:     true,
	},
	"blocked_contacts": {
		endpoint:    "/smtp/blockedContacts",
		responseKey: "contacts",
		pageSize:    100,
		group:       groupContacts,
		primaryKeys: []string{"email"},
		strategy:    config.StrategyReplace,
		params:      map[string]string{"sort": "asc"},
	},
	"companies": {
		endpoint:       "/companies",
		responseKey:    "items",
		pageSize:       100,
		group:          groupOther,
		primaryKeys:    []string{"id"},
		incrementalKey: "modifiedAt",
		strategy:       config.StrategyMerge,
		params:         map[string]string{"sort": "asc"},
		modifiedSince:  true,
		pageNumbers:    true,
		liftModifiedAt: "last_updated_at",
	},
	"deals": {
		endpoint:       "/crm/deals",
		responseKey:    "items",
		pageSize:       100,
		group:          groupOther,
		primaryKeys:    []string{"id"},
		incrementalKey: "modifiedAt",
		strategy:       config.StrategyMerge,
		params:         map[string]string{"sort": "asc"},
		modifiedSince:  true,
		liftModifiedAt: "last_updated_date",
	},
	"crm_tasks": {
		endpoint:    "/crm/tasks",
		responseKey: "items",
		pageSize:    100,
		group:       groupOther,
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		params:      map[string]string{"sort": "asc"},
	},
	"crm_notes": {
		endpoint:    "/crm/notes",
		pageSize:    100,
		group:       groupOther,
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		params:      map[string]string{"sort": "asc"},
	},
	"orders": {
		endpoint:       "/orders",
		responseKey:    "orders",
		pageSize:       100,
		group:          groupOther,
		primaryKeys:    []string{"id"},
		incrementalKey: "updatedAt",
		strategy:       config.StrategyMerge,
		params:         map[string]string{"sort": "asc", "secondarySortByModifiedAt": "true"},
		modifiedSince:  true,
	},
	"products": {
		endpoint:       "/products",
		responseKey:    "products",
		pageSize:       1000,
		group:          groupOther,
		primaryKeys:    []string{"id"},
		incrementalKey: "modifiedAt",
		strategy:       config.StrategyMerge,
		params:         map[string]string{"sort": "asc"},
		modifiedSince:  true,
	},
	"categories": {
		endpoint:       "/categories",
		responseKey:    "categories",
		pageSize:       100,
		group:          groupOther,
		primaryKeys:    []string{"id"},
		incrementalKey: "modifiedAt",
		strategy:       config.StrategyMerge,
		params:         map[string]string{"sort": "asc"},
		modifiedSince:  true,
	},
	"sms_events": {
		endpoint:       "/transactionalSMS/statistics/events",
		responseKey:    "events",
		pageSize:       100,
		group:          groupOther,
		incrementalKey: "event_date",
		strategy:       config.StrategyDeleteInsert,
		params:         map[string]string{"sort": "asc"},
		maxWindowDays:  90,
		eventDate:      true,
	},
	"whatsapp_events": {
		endpoint:       "/whatsapp/statistics/events",
		responseKey:    "events",
		pageSize:       2500,
		group:          groupOther,
		incrementalKey: "event_date",
		strategy:       config.StrategyDeleteInsert,
		params:         map[string]string{"sort": "asc"},
		maxWindowDays:  90,
		eventDate:      true,
	},
	"whatsapp_campaigns": {
		endpoint:    "/whatsappCampaigns",
		responseKey: "campaigns",
		pageSize:    100,
		group:       groupOther,
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		params:      map[string]string{"sort": "asc"},
	},
	"custom_events": {
		endpoint:       "/events",
		responseKey:    "events",
		pageSize:       5000,
		group:          groupOther,
		incrementalKey: "event_date",
		strategy:       config.StrategyDeleteInsert,
		timeWindows:    true,
	},
}

var supportedTables []string

func init() {
	for name := range tableConfigs {
		supportedTables = append(supportedTables, name)
	}
}

type BrevoSource struct {
	clients map[rateGroup]*httpclient.Client
}

func NewBrevoSource() *BrevoSource {
	return &BrevoSource{}
}

func (s *BrevoSource) Schemes() []string {
	return []string{"brevo"}
}

func (s *BrevoSource) HandlesIncrementality() bool {
	return true
}

func (s *BrevoSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}

	newClient := func(rps float64, burst int) *httpclient.Client {
		return httpclient.New(
			httpclient.WithBaseURL(baseURL),
			httpclient.WithTimeout(120*time.Second),
			httpclient.WithDebug(config.DebugMode),
			httpclient.WithAuth(httpclient.NewAPIKeyAuth("api-key", apiKey, true)),
			httpclient.WithHeader("Accept", "application/json"),
			httpclient.WithRateLimiter(rps, burst),
			httpclient.WithRetry(retryCount, time.Second, retryMaxWait),
			httpclient.WithRetryStrategy(brevoRetryStrategy),
		)
	}

	s.clients = map[rateGroup]*httpclient.Client{
		groupContacts: newClient(contactsRateLimit, contactsRateLimitBurst),
		groupSMTP:     newClient(smtpRateLimit, smtpRateLimitBurst),
		groupOther:    newClient(otherRateLimit, otherRateLimitBurst),
	}
	return nil
}

// brevoRetryStrategy waits for x-sib-ratelimit-reset on 429; Brevo sends no Retry-After.
func brevoRetryStrategy(resp *httpclient.Response, _ error) (time.Duration, error) {
	attempt := 1
	if resp != nil {
		if resp.StatusCode() == http.StatusTooManyRequests {
			if secs, err := strconv.ParseFloat(resp.Header().Get("x-sib-ratelimit-reset"), 64); err == nil && secs >= 0 {
				return time.Duration(secs*float64(time.Second)) + time.Second, nil
			}
		}
		if a := resp.Attempt(); a > 0 {
			attempt = a
		}
	}
	return time.Duration(math.Pow(2, float64(attempt))) * time.Second, nil
}

func (s *BrevoSource) Close(ctx context.Context) error {
	return nil
}

func parseURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "brevo://") {
		return "", fmt.Errorf("invalid brevo URI: must start with brevo://")
	}

	rest := strings.TrimPrefix(strings.TrimPrefix(uri, "brevo://"), "?")
	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse brevo URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in brevo URI")
	}
	return apiKey, nil
}

func (s *BrevoSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	cfg, ok := tableConfigs[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	tableName := req.Name
	return &source.DynamicSourceTable{
		TableName:            tableName,
		TablePrimaryKeys:     cfg.primaryKeys,
		TableIncrementalKey:  cfg.incrementalKey,
		TableStrategy:        cfg.strategy,
		TableIgnoresInterval: cfg.strategy == config.StrategyReplace,
		KnownSchema:          false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("brevo source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	_, ok := tableConfigs[table]
	return ok
}

func (s *BrevoSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		cfg := tableConfigs[table]
		config.Debug("[Brevo] reading %s", table)

		var err error
		switch {
		case cfg.maxWindowDays > 0:
			err = s.readDayWindows(ctx, table, cfg, opts, results)
		case cfg.timeWindows:
			err = s.readTimeWindows(ctx, table, cfg, opts, results)
		case cfg.modifiedSince:
			params := map[string]string{}
			if opts.IntervalStart != nil {
				params["modifiedSince"] = opts.IntervalStart.UTC().Format(modifiedSinceLayout)
			}
			err = s.paginateAndSend(ctx, table, cfg, params, nil, opts, results)
		case cfg.includeArchived:
			seen := map[string]bool{}
			err = s.paginateAndSend(ctx, table, cfg, nil, seen, opts, results)
			if err == nil {
				err = s.paginateAndSend(ctx, table, cfg, map[string]string{"status": "archive"}, seen, opts, results)
			}
		default:
			err = s.paginateAndSend(ctx, table, cfg, nil, nil, opts, results)
		}

		if err != nil {
			select {
			case results <- source.RecordBatchResult{Err: err}:
			case <-ctx.Done():
			}
		}
	}()

	return results, nil
}

// readDayWindows splits [start, end] (whole days, both inclusive) into windows of at most
// cfg.maxWindowDays, matching the delete+insert bounds on the DATE incremental key.
func (s *BrevoSource) readDayWindows(ctx context.Context, table string, cfg tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	loc, err := s.accountLocation(ctx)
	if err != nil {
		return err
	}
	start, end, err := resolveDayRange(opts.IntervalStart, opts.IntervalEnd, time.Now().In(loc))
	if err != nil {
		return fmt.Errorf("%s: %w", table, err)
	}

	for _, w := range splitDays(start, end, cfg.maxWindowDays) {
		params := map[string]string{
			"startDate": w[0].Format(dayLayout),
			"endDate":   w[1].Format(dayLayout),
		}
		if err := s.paginateAndSend(ctx, table, cfg, params, nil, opts, results); err != nil {
			return err
		}
	}
	return nil
}

// accountLocation returns the account's timezone, which Brevo uses for event dates and for
// rejecting an endDate after "today".
func (s *BrevoSource) accountLocation(ctx context.Context) (*time.Location, error) {
	resp, err := s.clients[groupOther].R(ctx).Get("/account")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch account: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("brevo API /account returned status %d: %s", resp.StatusCode(), resp.String())
	}
	var account struct {
		DateTimePreferences struct {
			Timezone string `json:"timezone"`
		} `json:"dateTimePreferences"`
	}
	if err := json.Unmarshal(resp.Body(), &account); err != nil {
		return nil, fmt.Errorf("failed to parse account response: %w", err)
	}
	if account.DateTimePreferences.Timezone == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(account.DateTimePreferences.Timezone)
	if err != nil {
		config.Debug("[Brevo] unknown account timezone %q, using UTC: %v", account.DateTimePreferences.Timezone, err)
		return time.UTC, nil
	}
	return loc, nil
}

// resolveDayRange takes interval bounds as calendar dates in their own location, as the
// delete+insert DATE bounds do; now should be in the account's timezone.
// readTimeWindows reads [start, end] in windows Brevo accepts. A window ends 1ms before the next
// starts, because Brevo includes events at endDate and the next window would return them again.
func (s *BrevoSource) readTimeWindows(ctx context.Context, table string, cfg tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if opts.IntervalStart == nil && opts.IntervalEnd == nil {
		return s.paginateAndSend(ctx, table, cfg, nil, nil, opts, results)
	}
	for _, w := range splitTimeRange(opts.IntervalStart, opts.IntervalEnd, time.Now(), customEventsWindowDays) {
		params := map[string]string{
			"startDate": w[0].UTC().Format(timeWindowLayout),
			"endDate":   w[1].UTC().Format(timeWindowLayout),
		}
		if err := s.paginateAndSend(ctx, table, cfg, params, nil, opts, results); err != nil {
			return err
		}
	}
	return nil
}

func splitTimeRange(intervalStart, intervalEnd *time.Time, now time.Time, maxDays int) [][2]time.Time {
	end := now
	if intervalEnd != nil {
		end = *intervalEnd
	}
	start := end.AddDate(0, 0, -maxDays)
	if intervalStart != nil {
		start = *intervalStart
	}
	var windows [][2]time.Time
	for cur := start; cur.Before(end); {
		next := cur.AddDate(0, 0, maxDays)
		if !next.Before(end) {
			return append(windows, [2]time.Time{cur, end})
		}
		windows = append(windows, [2]time.Time{cur, next.Add(-time.Millisecond)})
		cur = next
	}
	return windows
}

func resolveDayRange(intervalStart, intervalEnd *time.Time, now time.Time) (time.Time, time.Time, error) {
	today := calendarDay(now)
	end := today
	if intervalEnd != nil {
		end = calendarDay(*intervalEnd)
	}
	if end.After(today) {
		end = today
	}
	start := end.AddDate(0, 0, -(defaultWindowDays - 1))
	if intervalStart != nil {
		start = calendarDay(*intervalStart)
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("interval start %s is after interval end %s (end is capped at today)", start.Format(dayLayout), end.Format(dayLayout))
	}
	return start, end, nil
}

func splitDays(start, end time.Time, maxDays int) [][2]time.Time {
	var windows [][2]time.Time
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, maxDays) {
		wEnd := cur.AddDate(0, 0, maxDays-1)
		if wEnd.After(end) {
			wEnd = end
		}
		windows = append(windows, [2]time.Time{cur, wEnd})
	}
	return windows
}

func calendarDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// paginateAndSend skips rows whose id is already in seen, when seen is non-nil, and records the
// ids it sends, so a row returned by two passes or a shifted offset page is emitted once.
func (s *BrevoSource) paginateAndSend(ctx context.Context, table string, cfg tableConfig, extra map[string]string, seen map[string]bool, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	client := s.clients[cfg.group]
	offset := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := client.R(ctx).SetQueryParams(cfg.params).SetQueryParams(extra)
		if cfg.pageSize > 0 {
			req = req.SetQueryParam("limit", strconv.Itoa(cfg.pageSize))
			if cfg.pageNumbers {
				req = req.SetQueryParam("page", strconv.Itoa(offset/cfg.pageSize+1))
			} else {
				req = req.SetQueryParam("offset", strconv.Itoa(offset))
			}
		}

		resp, err := req.Get(cfg.endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", table, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("brevo API %s returned status %d: %s", cfg.endpoint, resp.StatusCode(), resp.String())
		}

		items, err := decodeItems(resp.Body(), cfg.responseKey)
		if err != nil {
			return fmt.Errorf("failed to parse %s response: %w", table, err)
		}

		// Keep the day as Brevo writes it (account timezone) rather than the UTC day, so event_date
		// follows the same day boundaries as the startDate/endDate filter and the delete window.
		for _, item := range items {
			if cfg.eventDate {
				if d, ok := item["date"].(string); ok && len(d) >= len(dayLayout) {
					item["event_date"] = d[:len(dayLayout)]
				}
			}
			if cfg.liftModifiedAt != "" {
				if attrs, ok := item["attributes"].(map[string]interface{}); ok {
					item["modifiedAt"] = attrs[cfg.liftModifiedAt]
				}
			}
		}

		pageLen := len(items)
		if cfg.modifiedSince && opts.IntervalEnd != nil {
			items = dropChangedAfter(items, cfg.incrementalKey, *opts.IntervalEnd)
		}
		if seen != nil {
			unique := items[:0]
			for _, item := range items {
				id := fmt.Sprint(item["id"])
				if !seen[id] {
					seen[id] = true
					unique = append(unique, item)
				}
			}
			items = unique
		}

		if err := sendItems(ctx, items, opts, results); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("failed to convert %s to Arrow: %w", table, err)
		}
		config.Debug("[Brevo] %s offset %d: sent %d records", table, offset, len(items))

		if cfg.pageSize == 0 || cfg.singlePage || pageLen < cfg.pageSize {
			return nil
		}
		offset += cfg.pageSize
	}
}

// dropChangedAfter removes rows whose key is after end, since modifiedSince has no upper bound.
// Rows without a parseable key are kept.
func dropChangedAfter(items []map[string]interface{}, key string, end time.Time) []map[string]interface{} {
	kept := items[:0]
	for _, item := range items {
		if v, ok := item[key].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil && t.After(end) {
				continue
			}
		}
		kept = append(kept, item)
	}
	return kept
}

func sendItems(ctx context.Context, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	var batch []map[string]interface{}
	var accBytes int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
		if err != nil {
			return err
		}
		select {
		case results <- source.RecordBatchResult{Batch: record}:
		case <-ctx.Done():
			record.Release()
			return ctx.Err()
		}
		batch = nil
		accBytes = 0
		return nil
	}

	for _, row := range items {
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
	return flush()
}

func decodeItems(body []byte, key string) ([]map[string]interface{}, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var itemsRaw interface{}
	ok := true
	if key == "" {
		if err := decoder.Decode(&itemsRaw); err != nil {
			return nil, err
		}
	} else {
		var raw map[string]interface{}
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		itemsRaw, ok = raw[key]
	}
	if !ok || itemsRaw == nil {
		return nil, nil
	}

	arr, ok := itemsRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected array for key %q, got %T", key, itemsRaw)
	}

	items := make([]map[string]interface{}, 0, len(arr))
	for _, v := range arr {
		if m, ok := v.(map[string]interface{}); ok {
			items = append(items, m)
		}
	}
	return items, nil
}
