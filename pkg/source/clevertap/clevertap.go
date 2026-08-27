package clevertap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
)

const (
	maxPageSize = 5000
	maxPages    = 10000
	// /v1/contentBlock/list paginates by page number and caps pageSize at 100.
	contentBlockPageSize = 100
	// Its date filters insist on milliseconds; plain RFC3339 is rejected with
	// "Enter date in ISO format".
	contentBlockTimeFormat = "2006-01-02T15:04:05.000Z"
	// CleverTap caps concurrent requests (3 for the export APIs) rather than
	// enforcing a per-minute quota, so this keeps in-flight requests under that.
	rateLimit                 = 3.0
	rateLimitBurst            = 3
	campaignReportParallelism = 3
	eventFanOutParallelism    = 3
	// CleverTap prepares each batch asynchronously and rejects a fetch that
	// arrives while the previous one is still running.
	transientRetries    = 6
	transientRetryDelay = 2 * time.Second
	// /1/targets/list.json is only optimized for ranges of 31 days or less.
	campaignWindowDays = 31
	// How far past today the campaign sweep runs, so campaigns scheduled for a
	// future date are picked up while they are still pending.
	campaignFutureMonths = 12
)

var supportedTables = []string{
	"events",
	"profiles",
	"campaigns",
	"campaign_reports",
	"content_blocks",
	"message_reports",
	"event_schema",
	"user_properties",
	"category_groups",
}

// errExportNotAllowed marks events CleverTap refuses to export (notification
// events, which are only available through its S3/GCP exports).
var errExportNotAllowed = errors.New("export not allowed for this event")

// validRegions maps a region code to its API host prefix. Europe uses the
// unprefixed host and the dashboard labels it "global", so both are accepted.
var validRegions = map[string]string{
	"eu1":    "",
	"global": "",
	"in1":    "in1",
	"us1":    "us1",
	"sg1":    "sg1",
	"aps3":   "aps3",
	"mec1":   "mec1",
}

// defaultStartDate bounds the export when no interval is supplied. CleverTap was
// founded in 2013, so nothing can predate it.
var defaultStartDate = time.Date(2013, 1, 1, 0, 0, 0, 0, time.UTC)

type CleverTapSource struct {
	client   *httpclient.Client
	timezone *time.Location
}

func NewCleverTapSource() *CleverTapSource {
	return &CleverTapSource{}
}

func (s *CleverTapSource) HandlesIncrementality() bool {
	return true
}

func (s *CleverTapSource) Schemes() []string {
	return []string{"clevertap"}
}

type clevertapCredentials struct {
	accountID string
	passcode  string
	region    string
	timezone  *time.Location
}

func parseURI(uri string) (clevertapCredentials, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return clevertapCredentials{}, fmt.Errorf("invalid clevertap URI: %w", err)
	}
	if parsed.Scheme != "clevertap" {
		return clevertapCredentials{}, fmt.Errorf("invalid clevertap URI: must start with clevertap://")
	}

	params := parsed.Query()

	accountID := params.Get("account_id")
	if accountID == "" {
		return clevertapCredentials{}, fmt.Errorf("account_id is required in clevertap URI")
	}

	passcode := params.Get("passcode")
	if passcode == "" {
		return clevertapCredentials{}, fmt.Errorf("passcode is required in clevertap URI")
	}

	region := params.Get("region")
	if region == "" {
		region = "eu1"
	}
	if _, ok := validRegions[region]; !ok {
		return clevertapCredentials{}, fmt.Errorf("invalid region %q: must be one of eu1 (or global), in1, us1, sg1, aps3, mec1", region)
	}

	// Event timestamps come back in the project's timezone with nothing in the
	// response naming it, so the caller has to declare it.
	timezone := params.Get("timezone")
	if timezone == "" {
		timezone = "UTC"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return clevertapCredentials{}, fmt.Errorf("invalid timezone %q: must be an IANA name such as UTC or Asia/Kolkata", timezone)
	}

	return clevertapCredentials{accountID: accountID, passcode: passcode, region: region, timezone: loc}, nil
}

// regionBaseURL maps a region code to its API host; Europe is the unprefixed default.
func regionBaseURL(region string) string {
	prefix, ok := validRegions[region]
	if !ok || prefix == "" {
		return "https://api.clevertap.com"
	}
	return fmt.Sprintf("https://%s.api.clevertap.com", prefix)
}

func (s *CleverTapSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.timezone = creds.timezone

	s.client = httpclient.New(
		httpclient.WithBaseURL(regionBaseURL(creds.region)),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("X-CleverTap-Account-Id", creds.accountID),
		httpclient.WithHeader("X-CleverTap-Passcode", creds.passcode),
		httpclient.WithHeader("Content-Type", "application/json"),
	)

	config.Debug("[CLEVERTAP] Connected to region %s", creds.region)
	return nil
}

func (s *CleverTapSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

type clevertapParams struct {
	// A comma-separated list, so several events can be loaded into one table
	// without falling back to every event in the project.
	EventName []string `mapstructure:"event_name"`
}

func parseTableName(raw string) (string, clevertapParams, error) {
	var p clevertapParams
	path, hasParams, err := tablespec.Parse(raw, &p, tablespec.WithListSeparator(","))
	if err != nil {
		return "", p, err
	}
	if !hasParams {
		return raw, p, nil
	}
	return path, p, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func (s *CleverTapSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	table, params, err := parseTableName(req.Name)
	if err != nil {
		return nil, err
	}
	if !isValidTable(table) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", table, strings.Join(supportedTables, ", "))
	}

	var primaryKeys []string
	incrementalKey := ""
	strategy := config.StrategyMerge

	switch table {
	case "events":
		// Event records carry no unique id, so merge is impossible; delete+insert
		// rewrites the loaded window instead.
		incrementalKey = "ts"
		strategy = config.StrategyDeleteInsert
	case "profiles":
		// No way to detect profile edits or deletions, so each run rebuilds rather
		// than accumulating stale rows.
		primaryKeys = []string{"object_id"}
		strategy = config.StrategyReplace
	case "campaigns", "campaign_reports":
		// scheduled_on says nothing about when a campaign last changed, so a
		// windowed load would freeze its status; each run rebuilds in full.
		primaryKeys = []string{"id"}
		strategy = config.StrategyReplace
	case "content_blocks":
		primaryKeys = []string{"id"}
		incrementalKey = "updatedAt"
	case "message_reports":
		// start_date is when a message went out, not when its counts last moved,
		// so there is nothing to load incrementally on.
		primaryKeys = []string{"message_id"}
		strategy = config.StrategyReplace
	case "event_schema", "user_properties":
		// A catalog of what exists in the project, with no date dimension to
		// load incrementally, so each run replaces the snapshot.
		primaryKeys = []string{"name"}
		strategy = config.StrategyReplace
	case "category_groups":
		// Also a catalog; keyed on the numeric group id rather than its name.
		primaryKeys = []string{"key"}
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           table,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("clevertap source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, table, params, opts)
		},
	}, nil
}

func (s *CleverTapSource) read(ctx context.Context, table string, params clevertapParams, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "events":
			err = s.fanOutByEvent(ctx, params.EventName, func(ctx context.Context, name string) error {
				return s.readEvents(ctx, name, opts, results)
			})
		case "profiles":
			err = s.fanOutByEvent(ctx, params.EventName, func(ctx context.Context, name string) error {
				return s.readProfiles(ctx, name, opts, results)
			})
		case "campaigns":
			err = s.readCampaigns(ctx, opts, results)
		case "campaign_reports":
			err = s.readCampaignReports(ctx, opts, results)
		case "content_blocks":
			err = s.readContentBlocks(ctx, opts, results)
		case "message_reports":
			err = s.readMessageReports(ctx, opts, results)
		case "event_schema":
			err = s.readSchema(ctx, "events", "events", opts, results)
		case "user_properties":
			err = s.readSchema(ctx, "userProperties", "userProperties", opts, results)
		case "category_groups":
			err = s.readCategoryGroups(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

// intervalBounds resolves the ingestion interval in loc, since CleverTap's days
// are project-local and UTC dates would skip the day a boundary falls on.
func intervalBounds(opts source.ReadOptions, loc *time.Location) (time.Time, time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	from := defaultStartDate
	to := time.Now()
	if opts.IntervalStart != nil {
		from = *opts.IntervalStart
	}
	if opts.IntervalEnd != nil {
		to = *opts.IntervalEnd
	}
	return from.In(loc), to.In(loc)
}

// nowIn is today in the project's timezone, which is the day CleverTap's date
// filters are measured against.
func nowIn(loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	return time.Now().In(loc)
}

// forEachDateWindow calls fn with the inclusive YYYYMMDD bounds of each window, so
// one export never has to walk the whole history in a single cursor.
func forEachDateWindow(ctx context.Context, start, end time.Time, days int, fn func(from, to int) error) error {
	for windowStart := start; !windowStart.After(end); windowStart = windowStart.AddDate(0, 0, days) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		windowEnd := windowStart.AddDate(0, 0, days-1)
		if windowEnd.After(end) {
			windowEnd = end
		}
		if err := fn(yyyymmdd(windowStart), yyyymmdd(windowEnd)); err != nil {
			return err
		}
	}
	return nil
}

func yyyymmdd(t time.Time) int {
	n, _ := strconv.Atoi(t.Format("20060102"))
	return n
}

// isRequestInProgress reports that CleverTap is still preparing a batch. Its
// exports run asynchronously, so an overlapping fetch is refused and retryable.
func isRequestInProgress(status, apiError string) bool {
	return status == "fail" && strings.Contains(strings.ToLower(apiError), "still in progress")
}

// hasNoResultYet reports that a campaign has produced no report. CleverTap sends
// this as a 500 rather than the documented 409, so the body has to be inspected.
func hasNoResultYet(body []byte) bool {
	var parsed struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	return parsed.Status == "fail" && strings.Contains(strings.ToLower(parsed.Error), "no result as of yet")
}

// isExportNotAllowed reports CleverTap's refusal to export an event. Notification
// events answer this way and are only reachable through its S3/GCP exports.
func isExportNotAllowed(body []byte) bool {
	var parsed struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	return parsed.Status == "fail" && strings.Contains(strings.ToLower(parsed.Error), "export not allowed")
}

// cursorExport runs CleverTap's two-step export: a POST returns a cursor, then each
// GET returns one batch plus the cursor for the next, until next_cursor is absent.
func (s *CleverTapSource) cursorExport(
	ctx context.Context,
	endpoint string,
	body map[string]interface{},
	query map[string]string,
	transform func(map[string]interface{}) map[string]interface{},
	keep func(map[string]interface{}) bool,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
) error {
	req := s.client.R(ctx).SetBody(body)
	for k, v := range query {
		req.SetQueryParam(k, v)
	}

	resp, err := req.Post(endpoint)
	if err != nil {
		return fmt.Errorf("failed to open %s cursor: %w", endpoint, err)
	}
	// The refusal arrives as a 400, so it has to be recognised before the generic
	// non-success check turns it into a plain error.
	if isExportNotAllowed(resp.Body()) {
		return errExportNotAllowed
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("%s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	var start struct {
		Status string `json:"status"`
		Cursor string `json:"cursor"`
		Error  string `json:"error"`
	}
	if err := jsonUseNumber(resp.Body(), &start); err != nil {
		return fmt.Errorf("failed to parse %s cursor response: %w", endpoint, err)
	}
	if start.Status == "fail" {
		return fmt.Errorf("%s failed to open cursor: %s", endpoint, start.Error)
	}

	cursor := start.Cursor
	previousCursor := ""
	totalSent := 0

	// No page cap: capping a cursor walk truncates the export, and delete+insert
	// would commit the short result over the full window. A cursor that repeats
	// is the only way the walk fails to terminate, so guard on that instead.
	for cursor != "" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if cursor == previousCursor {
			return fmt.Errorf("%s cursor stopped advancing after %d records", endpoint, totalSent)
		}
		previousCursor = cursor

		var page struct {
			Status     string                   `json:"status"`
			NextCursor string                   `json:"next_cursor"`
			Records    []map[string]interface{} `json:"records"`
			Error      string                   `json:"error"`
		}

		// The same cursor is re-requested while CleverTap is still preparing the
		// batch; the attempt is not a page, so it does not advance the cursor.
		var lastBody string
		for attempt := 0; ; attempt++ {
			resp, err := s.client.R(ctx).SetQueryParam("cursor", decodeCursor(cursor)).Get(endpoint)
			if err != nil {
				return fmt.Errorf("failed to fetch %s batch: %w", endpoint, err)
			}
			if !resp.IsSuccess() {
				return fmt.Errorf("%s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
			}

			page.Status, page.NextCursor, page.Records, page.Error = "", "", nil, ""
			if err := jsonUseNumber(resp.Body(), &page); err != nil {
				return fmt.Errorf("failed to parse %s batch: %w", endpoint, err)
			}
			lastBody = resp.String()

			if !isRequestInProgress(page.Status, page.Error) {
				break
			}
			if attempt >= transientRetries {
				return fmt.Errorf("%s batch still not ready after %d retries: %s", endpoint, transientRetries, lastBody)
			}
			config.Debug("[CLEVERTAP] %s batch not ready, retrying (%d/%d)", endpoint, attempt+1, transientRetries)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(transientRetryDelay):
			}
		}

		if page.Status == "fail" {
			return fmt.Errorf("%s batch failed: %s", endpoint, page.Error)
		}

		if len(page.Records) > 0 {
			items := make([]map[string]interface{}, 0, len(page.Records))
			var accBytes int64

			flush := func() error {
				if len(items) == 0 {
					return nil
				}
				record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
				if err != nil {
					return fmt.Errorf("failed to convert %s records to Arrow: %w", endpoint, err)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case results <- source.RecordBatchResult{Batch: record}:
				}
				totalSent += len(items)
				config.Debug("[CLEVERTAP] %s sent %d records (total: %d)", endpoint, len(items), totalSent)
				items = nil
				accBytes = 0
				return nil
			}

			for _, raw := range page.Records {
				if transform != nil {
					raw = transform(raw)
				}
				if keep != nil && !keep(raw) {
					continue
				}
				if opts.MaxBatchBytes > 0 {
					rowBytes := arrowconv.RowBytes(raw)
					if len(items) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
						if err := flush(); err != nil {
							return err
						}
					}
					accBytes += rowBytes
				}
				items = append(items, raw)
			}

			if err := flush(); err != nil {
				return err
			}
		}

		cursor = page.NextCursor
	}

	return nil
}

// decodeCursor undoes the percent-encoding CleverTap applies to cursors, since the
// request builder encodes query values again.
func decodeCursor(cursor string) string {
	if decoded, err := url.QueryUnescape(cursor); err == nil {
		return decoded
	}
	return cursor
}

func (s *CleverTapSource) readEvents(ctx context.Context, eventName string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[CLEVERTAP] reading events for %q", eventName)

	// Enrichments off: they would copy onto every row what profiles already holds.
	// objectId and identity are returned either way.
	query := map[string]string{
		"batch_size": strconv.Itoa(maxPageSize),
		"app":        "false",
		"events":     "false",
		"profile":    "false",
	}

	transform := func(item map[string]interface{}) map[string]interface{} {
		// The export does not echo the event name back, so records from different
		// events would be indistinguishable in one table.
		item["event_name"] = eventName
		if profile, ok := item["profile"].(map[string]interface{}); ok {
			// object_id is per-device so it under-joins; identity is per-person and
			// is the reliable key, though empty for users who never logged in.
			if id, ok := profile["objectId"].(string); ok {
				item["object_id"] = id
			}
			if identity, ok := profile["identity"].(string); ok {
				item["identity"] = identity
			}
		}
		if ts, ok := parseEventTimestamp(item["ts"], s.timezone); ok {
			item["ts"] = ts
		} else {
			// Keep the record with a null ts rather than dropping it; a raw value
			// would clash with the timestamp column.
			item["ts"] = nil
		}
		return item
	}

	// The API filters by whole days but delete+insert removes an exact instant
	// range, so trim the day padding or the edges duplicate on every run.
	keep := func(item map[string]interface{}) bool {
		return withinInterval(item["ts"], opts.IntervalStart, opts.IntervalEnd)
	}

	start, end := intervalBounds(opts, s.timezone)
	body := map[string]interface{}{
		"event_name": eventName,
		"from":       yyyymmdd(start),
		"to":         yyyymmdd(end),
	}
	return s.cursorExport(ctx, "/1/events.json", body, query, transform, keep, opts, results)
}

// withinInterval reports whether a converted timestamp is inside the bounds. Both
// ends are inclusive to match the delete predicate; a null ts is kept.
func withinInterval(v interface{}, start, end *time.Time) bool {
	ts, ok := v.(time.Time)
	if !ok {
		return true
	}
	if start != nil && ts.Before(*start) {
		return false
	}
	if end != nil && ts.After(*end) {
		return false
	}
	return true
}

// parseEventTimestamp turns the 14-digit yyyyMMddHHmmss timestamp into an instant.
// It is project-local, so loc supplies the offset.
func parseEventTimestamp(v interface{}, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}

	var raw string
	switch t := v.(type) {
	case json.Number:
		raw = t.String()
	case string:
		raw = t
	default:
		return time.Time{}, false
	}

	parsed, err := time.ParseInLocation("20060102150405", raw, loc)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// readSchema loads one of CleverTap's project catalogs. responseKey is the array
// field the endpoint wraps its records in.
func (s *CleverTapSource) readSchema(ctx context.Context, schemaType, responseKey string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[CLEVERTAP] reading schema %s", schemaType)

	resp, err := s.client.R(ctx).SetQueryParam("type", schemaType).Get("/getSchema")
	if err != nil {
		return fmt.Errorf("failed to fetch %s schema: %w", schemaType, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("%s schema returned status %d: %s", schemaType, resp.StatusCode(), resp.String())
	}

	var body map[string]interface{}
	if err := jsonUseNumber(resp.Body(), &body); err != nil {
		return fmt.Errorf("failed to parse %s schema response: %w", schemaType, err)
	}
	if status, _ := body["status"].(string); status == "fail" {
		return fmt.Errorf("%s schema request failed: %v", schemaType, body["error"])
	}

	raw, _ := body[responseKey].([]interface{})
	items := make([]map[string]interface{}, 0, len(raw))
	for _, r := range raw {
		if item, ok := r.(map[string]interface{}); ok {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil
	}

	var batch []map[string]interface{}
	var accBytes int64
	sent := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s schema to Arrow: %w", schemaType, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case results <- source.RecordBatchResult{Batch: record}:
		}
		sent += len(batch)
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
	if err := flush(); err != nil {
		return err
	}

	config.Debug("[CLEVERTAP] %s schema sent %d records", schemaType, sent)
	return nil
}

// readCategoryGroups loads the project's messaging subscription groups. It takes no
// parameters and wraps its records in a "list" field rather than a named one.
func (s *CleverTapSource) readCategoryGroups(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[CLEVERTAP] reading category_groups")

	resp, err := s.client.R(ctx).Get("/category-groups")
	if err != nil {
		return fmt.Errorf("failed to fetch category groups: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("category groups returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var body struct {
		Status string                   `json:"status"`
		List   []map[string]interface{} `json:"list"`
		Error  string                   `json:"error"`
	}
	if err := jsonUseNumber(resp.Body(), &body); err != nil {
		return fmt.Errorf("failed to parse category groups response: %w", err)
	}
	if body.Status == "fail" {
		return fmt.Errorf("category groups request failed: %s", body.Error)
	}
	if len(body.List) == 0 {
		return nil
	}

	var batch []map[string]interface{}
	var accBytes int64
	sent := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert category groups to Arrow: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case results <- source.RecordBatchResult{Batch: record}:
		}
		sent += len(batch)
		batch = nil
		accBytes = 0
		return nil
	}

	for _, row := range body.List {
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
	if err := flush(); err != nil {
		return err
	}

	config.Debug("[CLEVERTAP] category_groups sent %d records", sent)
	return nil
}

// fetchEventNames lists every event name in the project. Nothing is filtered out:
// eventCount is unreliable and Discarded events keep their history.
func (s *CleverTapSource) fetchEventNames(ctx context.Context) ([]string, error) {
	resp, err := s.client.R(ctx).SetQueryParam("type", "events").Get("/getSchema")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch event schema: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("event schema returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var body struct {
		Status string `json:"status"`
		Events []struct {
			Name string `json:"name"`
		} `json:"events"`
		Error string `json:"error"`
	}
	if err := jsonUseNumber(resp.Body(), &body); err != nil {
		return nil, fmt.Errorf("failed to parse event schema response: %w", err)
	}
	if body.Status == "fail" {
		return nil, fmt.Errorf("event schema request failed: %s", body.Error)
	}

	names := make([]string, 0, len(body.Events))
	for _, e := range body.Events {
		if e.Name != "" {
			names = append(names, e.Name)
		}
	}
	return names, nil
}

// fanOutByEvent runs fn per event name, a few at a time; an empty list means every
// event in the project. Events CleverTap refuses to export are skipped.
func (s *CleverTapSource) fanOutByEvent(ctx context.Context, names []string, fn func(context.Context, string) error) error {
	if len(names) == 0 {
		discovered, err := s.fetchEventNames(ctx)
		if err != nil {
			return err
		}
		names = discovered
	}
	if len(names) == 0 {
		return nil
	}
	config.Debug("[CLEVERTAP] fanning out over %d events", len(names))

	nameCh := make(chan string)
	errs := make(chan error, 1)
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i := 0; i < eventFanOutParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range nameCh {
				err := fn(ctx, name)
				if errors.Is(err, errExportNotAllowed) {
					config.Debug("[CLEVERTAP] %q cannot be exported, skipping", name)
					continue
				}
				if err != nil {
					select {
					case errs <- err:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}

	for _, name := range names {
		select {
		case nameCh <- name:
		case <-ctx.Done():
			// A worker failed; stop queueing instead of spinning through the rest.
			close(nameCh)
			wg.Wait()
			close(errs)
			return <-errs
		}
	}
	close(nameCh)

	wg.Wait()
	close(errs)

	return <-errs
}

// readProfiles fetches the users who raised one event. Sweeping several returns a
// person once per event they fired; replace de-duplicates on object_id.
func (s *CleverTapSource) readProfiles(ctx context.Context, eventName string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[CLEVERTAP] reading profiles for %q", eventName)

	// Interval ignored: profiles are selected by event activity, not by when the
	// profile changed, so only a full sweep is complete.
	query := map[string]string{
		"batch_size": strconv.Itoa(maxPageSize),
		"app":        "true",
		"events":     "true",
		"profile":    "true",
	}

	body := map[string]interface{}{
		"event_name": eventName,
		"from":       yyyymmdd(defaultStartDate),
		"to":         yyyymmdd(nowIn(s.timezone)),
	}
	return s.cursorExport(ctx, "/1/profiles.json", body, query, liftProfileObjectID, nil, opts, results)
}

// liftProfileObjectID promotes the CleverTap user id to a top-level column for merge.
// Profiles matched only through a device carry it inside platformInfo instead.
func liftProfileObjectID(item map[string]interface{}) map[string]interface{} {
	if id, ok := item["objectId"].(string); ok && id != "" {
		item["object_id"] = id
		return item
	}
	platforms, ok := item["platformInfo"].([]interface{})
	if !ok {
		return item
	}
	for _, p := range platforms {
		entry, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		if id, ok := entry["objectId"].(string); ok && id != "" {
			item["object_id"] = id
			return item
		}
	}
	return item
}

func (s *CleverTapSource) readContentBlocks(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[CLEVERTAP] reading content_blocks")

	totalSent := 0
	for page := 1; page <= maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("pageNumber", strconv.Itoa(page)).
			SetQueryParam("pageSize", strconv.Itoa(contentBlockPageSize))
		if opts.IntervalStart != nil {
			req.SetQueryParam("updatedFrom", opts.IntervalStart.UTC().Format(contentBlockTimeFormat))
		}
		if opts.IntervalEnd != nil {
			req.SetQueryParam("updatedTo", opts.IntervalEnd.UTC().Format(contentBlockTimeFormat))
		}

		resp, err := req.Get("/v1/contentBlock/list")
		if err != nil {
			return fmt.Errorf("failed to fetch content blocks: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("content blocks returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var body struct {
			Status        string                   `json:"status"`
			ContentBlocks []map[string]interface{} `json:"contentBlocks"`
			Total         json.Number              `json:"total"`
			Error         string                   `json:"error"`
		}
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse content blocks response: %w", err)
		}
		if body.Status == "fail" {
			return fmt.Errorf("content blocks request failed: %s", body.Error)
		}
		if len(body.ContentBlocks) == 0 {
			break
		}

		var batch []map[string]interface{}
		var accBytes int64

		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert content blocks to Arrow: %w", err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}
			batch = nil
			accBytes = 0
			return nil
		}

		for _, row := range body.ContentBlocks {
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
		if err := flush(); err != nil {
			return err
		}

		totalSent += len(body.ContentBlocks)
		config.Debug("[CLEVERTAP] content_blocks sent %d records (total: %d)", len(body.ContentBlocks), totalSent)

		if len(body.ContentBlocks) < contentBlockPageSize {
			break
		}
		if page == maxPages {
			return fmt.Errorf("content blocks did not terminate within %d pages", maxPages)
		}
	}

	return nil
}

func (s *CleverTapSource) readMessageReports(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[CLEVERTAP] reading message_reports")

	// Interval ignored: from/to select on send date while counts keep rising after
	// it. The optional filters are omitted so every channel and status is covered.
	from, to := yyyymmdd(defaultStartDate), yyyymmdd(nowIn(s.timezone))
	resp, err := s.client.R(ctx).
		SetBody(map[string]interface{}{"from": from, "to": to}).
		Post("/1/message/report.json")
	if err != nil {
		return fmt.Errorf("failed to fetch message reports: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("message reports returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var body struct {
		Status   string                   `json:"status"`
		Messages []map[string]interface{} `json:"messages"`
		Error    string                   `json:"error"`
	}
	if err := jsonUseNumber(resp.Body(), &body); err != nil {
		return fmt.Errorf("failed to parse message reports response: %w", err)
	}
	if body.Status == "fail" {
		return fmt.Errorf("message reports request failed: %s", body.Error)
	}
	if len(body.Messages) == 0 {
		return nil
	}

	for _, m := range body.Messages {
		liftMessageID(m)
	}

	var batch []map[string]interface{}
	var accBytes int64
	sent := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert message reports to Arrow: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case results <- source.RecordBatchResult{Batch: record}:
		}
		sent += len(batch)
		batch = nil
		accBytes = 0
		return nil
	}

	for _, row := range body.Messages {
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
	if err := flush(); err != nil {
		return err
	}

	config.Debug("[CLEVERTAP] message_reports sent %d records", sent)
	return nil
}

// liftMessageID copies the "message id" field, whose space makes it awkward as a
// merge key, to a conventional message_id column.
func liftMessageID(item map[string]interface{}) map[string]interface{} {
	if id, ok := item["message id"]; ok {
		item["message_id"] = id
	}
	return item
}

// fetchCampaigns walks the full history in 31-day windows, invoking fn per window.
// The interval is ignored; see the strategy note in GetTable.
func (s *CleverTapSource) fetchCampaigns(ctx context.Context, opts source.ReadOptions, fn func([]map[string]interface{}) error) error {
	start := defaultStartDate
	// Campaigns are selected by their scheduled date, which for a pending campaign
	// is in the future, so the sweep has to run past today to see them at all.
	end := nowIn(s.timezone).AddDate(0, campaignFutureMonths, 0)

	return forEachDateWindow(ctx, start, end, campaignWindowDays, func(from, to int) error {
		body := map[string]interface{}{"from": from, "to": to}
		resp, err := s.client.R(ctx).SetBody(body).Post("/1/targets/list.json")
		if err != nil {
			return fmt.Errorf("failed to fetch campaigns: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("campaigns returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var page struct {
			Status  string                   `json:"status"`
			Targets []map[string]interface{} `json:"targets"`
			Error   string                   `json:"error"`
		}
		if err := jsonUseNumber(resp.Body(), &page); err != nil {
			return fmt.Errorf("failed to parse campaigns response: %w", err)
		}
		if page.Status == "fail" {
			return fmt.Errorf("campaigns request failed: %s", page.Error)
		}
		if len(page.Targets) == 0 {
			return nil
		}

		return fn(page.Targets)
	})
}

func (s *CleverTapSource) readCampaigns(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[CLEVERTAP] reading campaigns")

	return s.fetchCampaigns(ctx, opts, func(targets []map[string]interface{}) error {
		var batch []map[string]interface{}
		var accBytes int64

		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert campaigns to Arrow: %w", err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}
			batch = nil
			accBytes = 0
			return nil
		}

		for _, row := range targets {
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
		if err := flush(); err != nil {
			return err
		}
		config.Debug("[CLEVERTAP] campaigns sent %d records", len(targets))
		return nil
	})
}

func (s *CleverTapSource) readCampaignReports(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[CLEVERTAP] reading campaign_reports")

	ids := make([]json.Number, 0)
	err := s.fetchCampaigns(ctx, opts, func(targets []map[string]interface{}) error {
		for _, t := range targets {
			if id, ok := t["id"].(json.Number); ok {
				ids = append(ids, id)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	idCh := make(chan json.Number, campaignReportParallelism)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	var skipped atomic.Int64

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i := 0; i < campaignReportParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range idCh {
				if err := s.sendCampaignReport(ctx, id, opts, results, &skipped); err != nil {
					select {
					case errs <- err:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}

	for _, id := range ids {
		select {
		case idCh <- id:
		case <-ctx.Done():
			// A worker failed; stop queueing instead of spinning through the rest.
			close(idCh)
			wg.Wait()
			close(errs)
			return <-errs
		}
	}
	close(idCh)

	wg.Wait()
	close(errs)

	if err := <-errs; err != nil {
		return err
	}

	// Reported once rather than per campaign: on a busy account most campaigns are
	// pending or running at any moment, and each one would otherwise log a line.
	if n := skipped.Load(); n > 0 {
		output.Warnf("Warning: clevertap skipped %d of %d campaign(s) that have no report yet\n", n, len(ids))
	}
	return nil
}

func (s *CleverTapSource) sendCampaignReport(ctx context.Context, id json.Number, opts source.ReadOptions, results chan<- source.RecordBatchResult, skipped *atomic.Int64) error {
	resp, err := s.client.R(ctx).SetBody(map[string]interface{}{"id": id}).Post("/1/targets/result.json")
	if err != nil {
		return fmt.Errorf("failed to fetch campaign report for %s: %w", id, err)
	}
	// No report yet is expected, not an error. The docs promise 409 but a 500 with
	// "no result as of yet" also occurs, so match on the message too.
	if resp.StatusCode() == 409 || hasNoResultYet(resp.Body()) {
		config.Debug("[CLEVERTAP] campaign %s has no report yet, skipping", id)
		skipped.Add(1)
		return nil
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("campaign report for %s returned status %d: %s", id, resp.StatusCode(), resp.String())
	}

	var page struct {
		Status string                 `json:"status"`
		Result map[string]interface{} `json:"result"`
		Error  string                 `json:"error"`
	}
	if err := jsonUseNumber(resp.Body(), &page); err != nil {
		return fmt.Errorf("failed to parse campaign report for %s: %w", id, err)
	}
	if page.Status == "fail" {
		return fmt.Errorf("campaign report for %s failed: %s", id, page.Error)
	}
	// A campaign that never delivered answers 200 with an explanatory message and
	// no result, so this is a normal skip rather than a failure.
	if page.Result == nil {
		config.Debug("[CLEVERTAP] campaign %s has no report: %s", id, page.Error)
		skipped.Add(1)
		return nil
	}

	page.Result["id"] = id

	record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{page.Result}, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert campaign report for %s to Arrow: %w", id, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case results <- source.RecordBatchResult{Batch: record}:
	}
	return nil
}
