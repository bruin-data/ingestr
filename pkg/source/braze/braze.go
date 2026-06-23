package braze

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	maxListPageSize  = 100 // campaigns, canvases, segments, products: groups of 100
	maxEventPageSize = 250 // events/list: returns 250 event names per page
	maxKPILength     = 100 // kpi data_series: length must be between 1 and 100 days
	maxListPages     = 10000

	// Braze's default pool (list + data_series endpoints) is 250,000 requests/hour.
	rateLimit      = 250000 * 0.8 / 3600.0 // ~55.5 req/s
	rateLimitBurst = 5

	// /events/list and /purchases/product_list share a 1,000 requests/hour pool.
	lowRateLimit      = 1000 * 0.8 / 3600.0 // ~0.22 req/s
	lowRateLimitBurst = 5

	// Braze expects ISO-8601 datetimes; the trailing Z marks the values (always UTC) as UTC.
	brazeTimeLayout = "2006-01-02T15:04:05Z"

	// Segment exports are asynchronous: Braze returns a download URL that becomes
	// available once the export finishes, so we poll it.
	exportPollInterval = 10 * time.Second
	exportMaxPolls     = 90 // ~15 minutes
	subscriptionsBatch = 1000
)

var supportedTables = []string{
	"campaigns",
	"canvases",
	"segments",
	"events",
	"products",
	"kpi_dau",
	"kpi_mau",
	"kpi_new_users",
	"kpi_uninstalls",
	"user_data",
}

// userDataFields are the fields requested from the user export: identifiers,
// subscription state, and bounded profile fields (heavy unbounded history is omitted).
// Braze also auto-returns the subscription opt-in/unsubscribe timestamps alongside
// email_subscribe/push_subscribe; they are not separately requestable here.
var userDataFields = []string{
	"external_id",
	"braze_id",
	"user_aliases",
	"email",
	"phone",
	"first_name",
	"last_name",
	"country",
	"home_city",
	"language",
	"time_zone",
	"dob",
	"gender",
	"email_subscribe",
	"push_subscribe",
	"push_tokens",
	"devices",
	"apps",
	"attributed_campaign",
	"attributed_source",
	"attributed_adgroup",
	"attributed_ad",
	"total_revenue",
	"purchases",
	"random_bucket",
	"last_coordinates",
	"created_at",
	"created_from",
	"uninstalled_at",
}

type BrazeSource struct {
	apiKey    string
	endpoint  string
	client    *httpclient.Client // default pool (list + data_series)
	lowClient *httpclient.Client // low-limit pool (events/list, purchases/product_list)
}

func NewBrazeSource() *BrazeSource {
	return &BrazeSource{}
}

func (s *BrazeSource) Schemes() []string {
	return []string{"braze"}
}

func (s *BrazeSource) HandlesIncrementality() bool {
	return true
}

func (s *BrazeSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseBrazeURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = creds.apiKey
	s.endpoint = creds.endpoint

	s.client = httpclient.New(
		httpclient.WithBaseURL(s.endpoint),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithAuth(httpclient.NewBearerAuth(s.apiKey)),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
	)
	s.lowClient = httpclient.New(
		httpclient.WithBaseURL(s.endpoint),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(lowRateLimit, lowRateLimitBurst),
		httpclient.WithAuth(httpclient.NewBearerAuth(s.apiKey)),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
	)

	config.Debug("[BRAZE] Connected to %s", s.endpoint)
	return nil
}

func (s *BrazeSource) Close(ctx context.Context) error {
	var errs []error
	if s.client != nil {
		errs = append(errs, s.client.Close())
	}
	if s.lowClient != nil {
		errs = append(errs, s.lowClient.Close())
	}
	return errors.Join(errs...)
}

type brazeCredentials struct {
	apiKey   string
	endpoint string
}

func parseBrazeURI(uri string) (*brazeCredentials, error) {
	if !strings.HasPrefix(uri, "braze://") {
		return nil, fmt.Errorf("invalid braze URI: must start with braze://")
	}

	rest := strings.TrimPrefix(uri, "braze://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse braze URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return nil, fmt.Errorf("api_key is required in braze URI")
	}

	endpoint := values.Get("endpoint")
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required in braze URI (e.g. endpoint=rest.iad-01.braze.com)")
	}

	return &brazeCredentials{apiKey: apiKey, endpoint: normalizeEndpoint(endpoint)}, nil
}

// normalizeEndpoint ensures the REST endpoint has an https scheme and no trailing slash.
func normalizeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "https://" + endpoint
	}
	return strings.TrimRight(endpoint, "/")
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func (s *BrazeSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	base, params := parseTableParam(req.Name)

	if !isValidTable(base) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}
	switch {
	case base == "user_data":
		if len(params) == 0 {
			return nil, fmt.Errorf("user_data requires at least one segment id, e.g. user_data:<segment_id>[,<segment_id>]")
		}
	case isKPITable(base):
		// app_id list is optional
	case len(params) > 0:
		return nil, fmt.Errorf("the :param suffix is only supported for kpi_* and user_data tables, not %q", base)
	}

	primaryKeys, incrementalKey, strategy := tableMetadata(base)
	// Per-app KPI rows share a date, so app_id must be part of the merge key.
	if isKPITable(base) && len(params) > 0 {
		primaryKeys = []string{"time", "app_id"}
	}
	if len(req.PrimaryKeys) > 0 {
		primaryKeys = req.PrimaryKeys
	}

	return &source.DynamicSourceTable{
		TableName:           base,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("braze source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, base, params, opts)
		},
	}, nil
}

// tableMetadata returns the primary key(s), incremental key, and write strategy for a table.
func tableMetadata(table string) ([]string, string, config.IncrementalStrategy) {
	switch table {
	case "campaigns", "canvases":
		// list endpoints accept last_edit.time[gt]; rows carry last_edited
		return []string{"id"}, "last_edited", config.StrategyMerge
	case "segments":
		return []string{"id"}, "", config.StrategyReplace
	case "events":
		return []string{"event_name"}, "", config.StrategyReplace
	case "products":
		return []string{"product_id"}, "", config.StrategyReplace
	case "kpi_dau", "kpi_mau", "kpi_new_users", "kpi_uninstalls":
		// daily series keyed on date; merge updates the still-changing latest day
		return []string{"time"}, "time", config.StrategyMerge
	case "user_data":
		// point-in-time snapshot of users (and their subscription state) per segment;
		// segment_id is part of the key so a user can appear in multiple segments
		return []string{"braze_id", "segment_id"}, "", config.StrategyReplace
	default:
		return nil, "", config.StrategyReplace
	}
}

func (s *BrazeSource) read(ctx context.Context, table string, params []string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "campaigns":
			err = s.readCampaigns(ctx, opts, results)
		case "canvases":
			err = s.readCanvases(ctx, opts, results)
		case "segments":
			err = s.readSegments(ctx, opts, results)
		case "events":
			err = s.readEvents(ctx, opts, results)
		case "products":
			err = s.readProducts(ctx, opts, results)
		case "kpi_dau":
			err = s.readKPISeries(ctx, "/kpi/dau/data_series", params, opts, results)
		case "kpi_mau":
			err = s.readKPISeries(ctx, "/kpi/mau/data_series", params, opts, results)
		case "kpi_new_users":
			err = s.readKPISeries(ctx, "/kpi/new_users/data_series", params, opts, results)
		case "kpi_uninstalls":
			err = s.readKPISeries(ctx, "/kpi/uninstalls/data_series", params, opts, results)
		case "user_data":
			err = s.readUserData(ctx, params, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// parseTableParam splits "kpi_dau:id1,id2" into the base table and its app_id list.
func parseTableParam(name string) (string, []string) {
	base, param, found := strings.Cut(name, ":")
	if !found {
		return base, nil
	}
	var ids []string
	for _, p := range strings.Split(param, ",") {
		if p = strings.TrimSpace(p); p != "" {
			ids = append(ids, p)
		}
	}
	return base, ids
}

func isKPITable(t string) bool {
	switch t {
	case "kpi_dau", "kpi_mau", "kpi_new_users", "kpi_uninstalls":
		return true
	default:
		return false
	}
}

func decodeBody(b []byte, v interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return dec.Decode(v)
}

// asMap is the item converter for list endpoints that return objects.
func asMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return nil
}

// filterItemsByInterval keeps items whose timestamp falls within [start, end).
// Rows with no parseable timestamp are kept; empty fields or an open interval is a no-op.
func filterItemsByInterval(items []map[string]interface{}, fields []string, start, end *time.Time) []map[string]interface{} {
	if len(fields) == 0 || (start == nil && end == nil) {
		return items
	}

	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		ts, ok := firstTimestamp(item, fields)
		if !ok {
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

func firstTimestamp(item map[string]interface{}, fields []string) (time.Time, bool) {
	for _, field := range fields {
		raw := item[field]
		if raw == nil {
			continue
		}
		switch v := raw.(type) {
		case string:
			if v == "" {
				continue
			}
			if ts, err := dateparse.ParseAny(v); err == nil {
				return ts.UTC(), true
			}
		case json.Number:
			// decodeBody uses UseNumber(); treat a numeric timestamp as Unix epoch seconds.
			if sec, err := v.Int64(); err == nil {
				return time.Unix(sec, 0).UTC(), true
			}
		case time.Time:
			return v.UTC(), true
		}
	}
	return time.Time{}, false
}

// paginateList walks a page-numbered list endpoint, one Arrow batch per page.
// pageSize 0 stops only on an empty page; filterFields applies a client-side interval filter.
func (s *BrazeSource) paginateList(
	ctx context.Context,
	client *httpclient.Client,
	endpoint, dataKey string,
	pageSize int,
	extraParams map[string]string,
	itemFn func(interface{}) map[string]interface{},
	filterFields []string,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
) error {
	totalSent := 0
	for page := 0; ; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if page >= maxListPages {
			config.Debug("[BRAZE] %s hit maxListPages guard (%d)", endpoint, maxListPages)
			break
		}

		req := client.R(ctx).SetQueryParam("page", strconv.Itoa(page))
		for k, v := range extraParams {
			req.SetQueryParam(k, v)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("%s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var body map[string]interface{}
		if err := decodeBody(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		raw, ok := body[dataKey].([]interface{})
		if !ok || len(raw) == 0 {
			break
		}

		items := make([]map[string]interface{}, 0, len(raw))
		for _, r := range raw {
			if item := itemFn(r); item != nil {
				items = append(items, item)
			}
		}

		items = filterItemsByInterval(items, filterFields, opts.IntervalStart, opts.IntervalEnd)

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", endpoint, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
			config.Debug("[BRAZE] %s page %d: sent %d (total: %d)", endpoint, page, len(items), totalSent)
		}

		if pageSize > 0 && len(raw) < pageSize {
			break
		}
	}

	return nil
}

func (s *BrazeSource) readCampaigns(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BRAZE] reading campaigns")
	// include_archived defaults to false, so request archived campaigns too for a full export.
	params := map[string]string{"include_archived": "true"}
	// /campaigns/list supports only a greater-than filter on last edit time; the
	// client-side filter on last_edited adds the interval-end bound the API lacks.
	if opts.IntervalStart != nil {
		params["last_edit.time[gt]"] = opts.IntervalStart.UTC().Format(brazeTimeLayout)
	}
	return s.paginateList(ctx, s.client, "/campaigns/list", "campaigns", maxListPageSize, params, asMap, []string{"last_edited"}, opts, results)
}

func (s *BrazeSource) readCanvases(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BRAZE] reading canvases")
	// include_archived defaults to false, so request archived canvases too for a full export.
	params := map[string]string{"include_archived": "true"}
	if opts.IntervalStart != nil {
		params["last_edit.time[gt]"] = opts.IntervalStart.UTC().Format(brazeTimeLayout)
	}
	return s.paginateList(ctx, s.client, "/canvas/list", "canvases", maxListPageSize, params, asMap, []string{"last_edited"}, opts, results)
}

func (s *BrazeSource) readSegments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BRAZE] reading segments")
	return s.paginateList(ctx, s.client, "/segments/list", "segments", maxListPageSize, nil, asMap, nil, opts, results)
}

// readUserData exports the users of one or more segments via the async
// /users/export/segment endpoint, tagging each row with its segment_id.
func (s *BrazeSource) readUserData(ctx context.Context, segmentIDs []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	for _, segmentID := range segmentIDs {
		data, err := s.exportSegment(ctx, segmentID)
		if err != nil {
			return fmt.Errorf("segment %s: %w", segmentID, err)
		}
		if err := emitSegmentExport(ctx, data, segmentID, opts, results); err != nil {
			return fmt.Errorf("segment %s: %w", segmentID, err)
		}
	}
	return nil
}

// exportSegment starts the async segment export and returns the zipped result,
// polling the download URL (which 404/403s until the export materializes).
func (s *BrazeSource) exportSegment(ctx context.Context, segmentID string) ([]byte, error) {
	config.Debug("[BRAZE] exporting user_data for segment %s", segmentID)
	body := map[string]interface{}{"segment_id": segmentID, "fields_to_export": userDataFields}
	resp, err := s.client.R(ctx).SetBody(body).Post("/users/export/segment")
	if err != nil {
		return nil, fmt.Errorf("failed to start segment export: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("/users/export/segment returned status %d: %s", resp.StatusCode(), resp.String())
	}
	var export struct{ URL, Message string } // json matches url/message case-insensitively
	if err := decodeBody(resp.Body(), &export); err != nil {
		return nil, fmt.Errorf("failed to parse segment export response: %w", err)
	}
	if export.URL == "" {
		return nil, fmt.Errorf("segment export returned no download URL (cloud-storage exports are unsupported); message: %s", export.Message)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	for i := 0; i < exportMaxPolls; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(exportPollInterval):
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, export.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build segment export download request: %w", err)
		}
		dl, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to download segment export: %w", err)
		}
		if dl.StatusCode == http.StatusOK {
			data, readErr := io.ReadAll(dl.Body)
			_ = dl.Body.Close()
			return data, readErr
		}
		_ = dl.Body.Close()
		// 403/404 mean the export is not ready yet; anything else is unexpected.
		if dl.StatusCode != http.StatusNotFound && dl.StatusCode != http.StatusForbidden {
			return nil, fmt.Errorf("unexpected status %d while downloading segment export", dl.StatusCode)
		}
	}
	return nil, fmt.Errorf("segment export did not complete after %s", time.Duration(exportMaxPolls)*exportPollInterval)
}

// emitSegmentExport parses the zipped export (newline-delimited JSON users),
// tags each record with its segment_id, and streams them in batches.
func emitSegmentExport(ctx context.Context, data []byte, segmentID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("failed to open segment export archive: %w", err)
	}

	batch := make([]map[string]interface{}, 0, subscriptionsBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert user_data to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = batch[:0]
		return nil
	}

	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to read export entry %s: %w", f.Name, err)
		}
		scanner := bufio.NewScanner(rc)
		scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			var user map[string]interface{}
			if err := decodeBody(line, &user); err != nil {
				_ = rc.Close()
				return fmt.Errorf("failed to parse user record: %w", err)
			}
			user["segment_id"] = segmentID
			batch = append(batch, user)
			if len(batch) >= subscriptionsBatch {
				if err := flush(); err != nil {
					_ = rc.Close()
					return err
				}
			}
		}
		scanErr := scanner.Err()
		_ = rc.Close()
		if scanErr != nil {
			return fmt.Errorf("failed to scan export entry %s: %w", f.Name, scanErr)
		}
	}

	return flush()
}

func (s *BrazeSource) readEvents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BRAZE] reading events")
	// events/list returns an array of event-name strings; lift each to a record.
	itemFn := func(v interface{}) map[string]interface{} {
		name, ok := v.(string)
		if !ok {
			return nil
		}
		return map[string]interface{}{"event_name": name}
	}
	return s.paginateList(ctx, s.lowClient, "/events/list", "events", maxEventPageSize, nil, itemFn, nil, opts, results)
}

func (s *BrazeSource) readProducts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BRAZE] reading products")
	itemFn := func(v interface{}) map[string]interface{} {
		id, ok := v.(string)
		if !ok {
			return nil
		}
		return map[string]interface{}{"product_id": id}
	}
	// product_list page size is undocumented, so stop only on an empty page (pageSize 0).
	return s.paginateList(ctx, s.lowClient, "/purchases/product_list", "products", 0, nil, itemFn, nil, opts, results)
}

// readKPISeries fetches a daily KPI series: the all-apps aggregate with no app
// IDs, or one per-app series each (tagged with an app_id column) when given.
func (s *BrazeSource) readKPISeries(ctx context.Context, endpoint string, appIDs []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BRAZE] reading %s (apps: %d)", endpoint, len(appIDs))
	if len(appIDs) == 0 {
		return s.fetchKPIWindows(ctx, endpoint, "", opts, results)
	}
	for _, appID := range appIDs {
		if err := s.fetchKPIWindows(ctx, endpoint, appID, opts, results); err != nil {
			return fmt.Errorf("%s for app_id %s: %w", endpoint, appID, err)
		}
	}
	return nil
}

// fetchKPIWindows walks the series in 100-day windows back to interval-start (last
// 100 days with no interval); a non-empty appID scopes it and adds an app_id column.
func (s *BrazeSource) fetchKPIWindows(ctx context.Context, endpoint, appID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	end := time.Now().UTC()
	if opts.IntervalEnd != nil {
		end = opts.IntervalEnd.UTC()
	}
	var start *time.Time
	if opts.IntervalStart != nil {
		st := opts.IntervalStart.UTC()
		start = &st
	}

	for _, w := range planKPIWindows(start, end) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("length", strconv.Itoa(w.length)).
			SetQueryParam("ending_at", w.endingAt.Format(brazeTimeLayout))
		if appID != "" {
			req.SetQueryParam("app_id", appID)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("%s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var body struct {
			Data []map[string]interface{} `json:"data"`
		}
		if err := decodeBody(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		if len(body.Data) > 0 {
			if appID != "" {
				for _, row := range body.Data {
					row["app_id"] = appID
				}
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(body.Data, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", endpoint, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			config.Debug("[BRAZE] %s window ending %s: sent %d", endpoint, w.endingAt.Format(brazeTimeLayout), len(body.Data))
		}
	}

	return nil
}

type kpiWindow struct {
	length   int
	endingAt time.Time
}

// planKPIWindows splits [start, end] into <=100-day windows ending at successively
// earlier dates. With start nil it returns a single 100-day window ending at end.
func planKPIWindows(start *time.Time, end time.Time) []kpiWindow {
	var windows []kpiWindow
	windowEnd := end
	for {
		length := maxKPILength
		if start != nil {
			days := int(windowEnd.Sub(*start).Hours()/24) + 1
			if days <= 0 {
				break
			}
			if days < length {
				length = days
			}
		}

		windows = append(windows, kpiWindow{length: length, endingAt: windowEnd})

		if start == nil {
			break
		}
		windowEnd = windowEnd.AddDate(0, 0, -length)
		// Stop only once we've stepped past start; landing exactly on start still
		// needs one final 1-day window to cover the start day itself.
		if windowEnd.Before(*start) {
			break
		}
	}
	return windows
}
