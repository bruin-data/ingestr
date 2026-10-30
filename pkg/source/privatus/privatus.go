package privatus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
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
	baseURL = "https://privatusanalytics.com"

	maxPageSize          = 100
	maxBreakdownPageSize = 1000
	maxPages             = 10000
	timeseriesWindowDays = 90
	defaultDays          = 30
	maxBatchRows         = 10000

	// The API allows 1,000 requests an hour on the Free plan (Pro 5,000, Business 20,000).
	defaultRequestsPerHour = 1000
	maxRequestsPerHour     = 100000
	rateLimitBurst         = 5

	dateLayout = "2006-01-02"
)

var supportedTables = []string{
	"workspaces",
	"sites",
	"goals",
	"daily_stats",
	"pages",
	"referrers",
	"events",
	"goal_conversions",
	"breakdown",
}

var namedBreakdowns = map[string]string{
	"pages":            "page",
	"referrers":        "referrer",
	"events":           "event",
	"goal_conversions": "goal",
}

var (
	siteIDPattern    = regexp.MustCompile(`^pa_[A-Za-z0-9]+$`)
	dimensionPattern = regexp.MustCompile(`^(?:[a-z_]+|prop:[A-Za-z0-9_.\-]{1,64})$`)
)

type PrivatusSource struct {
	client  *httpclient.Client
	siteIDs []string
}

type connection struct {
	apiKey          string
	siteIDs         []string
	requestsPerHour int
}

type tableSpec struct {
	table     string
	dimension string
}

func NewPrivatusSource() *PrivatusSource {
	return &PrivatusSource{}
}

func (s *PrivatusSource) Schemes() []string {
	return []string{"privatus"}
}

func (s *PrivatusSource) HandlesIncrementality() bool {
	return true
}

func (s *PrivatusSource) Connect(ctx context.Context, uri string) error {
	conn, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.siteIDs = conn.siteIDs
	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(conn.apiKey)),
		httpclient.WithRateLimiter(float64(conn.requestsPerHour)*0.8/3600.0, rateLimitBurst),
	)

	config.Debug("[PRIVATUS] Connected (sites filter: %d, requests per hour: %d)", len(conn.siteIDs), conn.requestsPerHour)
	return nil
}

func parseURI(uri string) (*connection, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to parse privatus URI: %w", err)
	}
	if parsed.Scheme != "privatus" {
		return nil, fmt.Errorf("invalid privatus URI: must start with privatus://")
	}

	query := parsed.Query()
	conn := &connection{
		apiKey:          strings.TrimSpace(query.Get("api_key")),
		requestsPerHour: defaultRequestsPerHour,
	}
	if conn.apiKey == "" {
		return nil, fmt.Errorf("api_key is required in the privatus URI, expected format: privatus://?api_key=<pat_token>")
	}

	if raw := query.Get("site_ids"); raw != "" {
		for _, id := range strings.Split(raw, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if !siteIDPattern.MatchString(id) {
				return nil, fmt.Errorf("invalid site id %q in site_ids, expected a value like pa_AB12CD34EF", id)
			}
			conn.siteIDs = append(conn.siteIDs, id)
		}
	}

	if raw := query.Get("requests_per_hour"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxRequestsPerHour {
			return nil, fmt.Errorf("requests_per_hour must be a whole number from 1 to %d", maxRequestsPerHour)
		}
		conn.requestsPerHour = n
	}

	return conn, nil
}

func (s *PrivatusSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func parseTableName(name string) (tableSpec, error) {
	table, dimension, hasDimension := strings.Cut(name, ":")
	if !isValidTable(table) {
		return tableSpec{}, fmt.Errorf("unsupported privatus table %q, supported tables are: %s", name, strings.Join(supportedTables, ", "))
	}

	if table == "breakdown" {
		if !hasDimension || dimension == "" {
			return tableSpec{}, fmt.Errorf("the breakdown table needs a dimension, e.g. --source-table 'breakdown:country'")
		}
		if !dimensionPattern.MatchString(dimension) {
			return tableSpec{}, fmt.Errorf("invalid breakdown dimension %q", dimension)
		}
		return tableSpec{table: table, dimension: dimension}, nil
	}

	if hasDimension {
		return tableSpec{}, fmt.Errorf("table %q does not take a ':' suffix", table)
	}
	return tableSpec{table: table, dimension: namedBreakdowns[table]}, nil
}

func (spec tableSpec) primaryKeys() []string {
	switch spec.table {
	case "workspaces", "sites", "goals":
		return []string{"id"}
	case "daily_stats":
		return []string{"site_id", "date"}
	}
	if spec.dimension == "goal" {
		return []string{"site_id", "date", "goal_id"}
	}
	return []string{"site_id", "date", "value"}
}

func (spec tableSpec) isDaily() bool {
	return spec.table == "daily_stats" || spec.dimension != ""
}

func (s *PrivatusSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	spec, err := parseTableName(req.Name)
	if err != nil {
		return nil, err
	}

	strategy := config.StrategyReplace
	incrementalKey := ""
	if spec.isDaily() {
		strategy = config.StrategyMerge
		incrementalKey = "date"
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    spec.primaryKeys(),
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("privatus source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, spec, opts)
		},
	}, nil
}

func (s *PrivatusSource) read(ctx context.Context, spec tableSpec, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 4)

	go func() {
		defer close(results)

		var err error
		switch spec.table {
		case "workspaces":
			err = s.readWorkspaces(ctx, opts, results)
		case "sites":
			err = s.readSites(ctx, opts, results)
		case "goals":
			err = s.readGoals(ctx, opts, results)
		case "daily_stats":
			err = s.readDailyStats(ctx, opts, results)
		default:
			err = s.readBreakdown(ctx, spec, opts, results)
		}
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

type batcher struct {
	opts     source.ReadOptions
	results  chan<- source.RecordBatchResult
	items    []map[string]interface{}
	accBytes int64
}

func newBatcher(opts source.ReadOptions, results chan<- source.RecordBatchResult) *batcher {
	return &batcher{opts: opts, results: results}
}

func (b *batcher) add(item map[string]interface{}) error {
	if b.opts.MaxBatchBytes > 0 {
		rowBytes := arrowconv.RowBytes(item)
		if len(b.items) > 0 && b.accBytes+rowBytes > b.opts.MaxBatchBytes {
			if err := b.flush(); err != nil {
				return err
			}
		}
		b.accBytes += rowBytes
	}
	b.items = append(b.items, item)
	if len(b.items) >= maxBatchRows {
		return b.flush()
	}
	return nil
}

func (b *batcher) flush() error {
	if len(b.items) == 0 {
		return nil
	}
	record, err := arrowconv.ItemsToArrowRecordWithSchema(b.items, nil, b.opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert rows to Arrow: %w", err)
	}
	b.results <- source.RecordBatchResult{Batch: record}
	b.items = nil
	b.accBytes = 0
	return nil
}

func (s *PrivatusSource) get(ctx context.Context, path string, params url.Values, out interface{}) error {
	resp, err := s.client.R(ctx).SetQueryParamValues(params).Get(path)
	if err != nil {
		return fmt.Errorf("privatus request to %s failed: %w", path, err)
	}
	if !resp.IsSuccess() {
		body := resp.String()
		if len(body) > 500 {
			body = body[:500]
		}
		return fmt.Errorf("privatus request to %s failed with status %d: %s", path, resp.StatusCode(), body)
	}

	decoder := json.NewDecoder(bytes.NewReader(resp.Body()))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("failed to parse privatus response from %s: %w", path, err)
	}
	return nil
}

type listResponse struct {
	Data []map[string]interface{} `json:"data"`
	Meta struct {
		TotalPages int `json:"total_pages"`
	} `json:"meta"`
}

func (s *PrivatusSource) paginateList(ctx context.Context, path string, params url.Values, handle func(item map[string]interface{}) error) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("per_page", strconv.Itoa(maxPageSize))

	for page := 1; page <= maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params.Set("page", strconv.Itoa(page))
		var resp listResponse
		if err := s.get(ctx, path, params, &resp); err != nil {
			return err
		}
		for _, item := range resp.Data {
			if err := handle(item); err != nil {
				return err
			}
		}
		if len(resp.Data) < maxPageSize || page >= resp.Meta.TotalPages {
			return nil
		}
	}
	config.Debug("[PRIVATUS] stopped %s after %d pages", path, maxPages)
	return nil
}

func (s *PrivatusSource) listWorkspaceIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := s.paginateList(ctx, "/workspaces.json", nil, func(item map[string]interface{}) error {
		if id, ok := item["id"].(string); ok && id != "" {
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch workspaces: %w", err)
	}
	return ids, nil
}

func (s *PrivatusSource) wantSite(id string) bool {
	if len(s.siteIDs) == 0 {
		return true
	}
	for _, want := range s.siteIDs {
		if want == id {
			return true
		}
	}
	return false
}

func (s *PrivatusSource) eachSite(ctx context.Context, handle func(site map[string]interface{}) error) error {
	workspaceIDs, err := s.listWorkspaceIDs(ctx)
	if err != nil {
		return err
	}
	for _, workspaceID := range workspaceIDs {
		path := "/workspaces/" + url.PathEscape(workspaceID) + "/sites.json"
		err := s.paginateList(ctx, path, nil, func(site map[string]interface{}) error {
			id, _ := site["id"].(string)
			if !s.wantSite(id) {
				return nil
			}
			return handle(site)
		})
		if err != nil {
			return fmt.Errorf("failed to fetch sites: %w", err)
		}
	}
	return nil
}

func (s *PrivatusSource) siteIDList(ctx context.Context) ([]string, error) {
	if len(s.siteIDs) > 0 {
		return s.siteIDs, nil
	}
	var ids []string
	err := s.eachSite(ctx, func(site map[string]interface{}) error {
		if id, ok := site["id"].(string); ok && siteIDPattern.MatchString(id) {
			ids = append(ids, id)
		}
		return nil
	})
	return ids, err
}

func (s *PrivatusSource) readWorkspaces(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PRIVATUS] reading workspaces")
	b := newBatcher(opts, results)
	err := s.paginateList(ctx, "/workspaces.json", nil, b.add)
	if err != nil {
		return fmt.Errorf("failed to fetch workspaces: %w", err)
	}
	return b.flush()
}

func (s *PrivatusSource) readSites(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PRIVATUS] reading sites")
	b := newBatcher(opts, results)
	err := s.eachSite(ctx, func(site map[string]interface{}) error {
		// The tracking snippet and ingest key prefix are setup details, not data.
		delete(site, "snippet")
		delete(site, "ingest_key_prefix")
		return b.add(site)
	})
	if err != nil {
		return err
	}
	return b.flush()
}

func (s *PrivatusSource) readGoals(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PRIVATUS] reading goals")
	siteIDs, err := s.siteIDList(ctx)
	if err != nil {
		return err
	}

	b := newBatcher(opts, results)
	for _, siteID := range siteIDs {
		path := "/sites/" + url.PathEscape(siteID) + "/goals.json"
		params := url.Values{"status": {"all"}}
		err := s.paginateList(ctx, path, params, func(goal map[string]interface{}) error {
			// stats covers only the request's default period; daily numbers live in goal_conversions.
			delete(goal, "stats")
			goal["site_id"] = siteID
			return b.add(goal)
		})
		if err != nil {
			return fmt.Errorf("failed to fetch goals for %s: %w", siteID, err)
		}
	}
	return b.flush()
}

func dateRange(opts source.ReadOptions, now time.Time) (time.Time, time.Time, error) {
	today := truncateDay(now.UTC())
	end := today
	if opts.IntervalEnd != nil {
		end = truncateDay(opts.IntervalEnd.UTC())
		if end.After(today) {
			end = today
		}
	}
	start := end.AddDate(0, 0, -(defaultDays - 1))
	if opts.IntervalStart != nil {
		start = truncateDay(opts.IntervalStart.UTC())
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("interval start %s is after interval end %s", start.Format(dateLayout), end.Format(dateLayout))
	}
	return start, end, nil
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

type timeseriesResponse struct {
	Data struct {
		Series []map[string]interface{} `json:"series"`
	} `json:"data"`
}

func (s *PrivatusSource) readDailyStats(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PRIVATUS] reading daily_stats")
	start, end, err := dateRange(opts, time.Now())
	if err != nil {
		return err
	}
	siteIDs, err := s.siteIDList(ctx)
	if err != nil {
		return err
	}

	b := newBatcher(opts, results)
	for _, siteID := range siteIDs {
		path := "/sites/" + url.PathEscape(siteID) + "/stats/timeseries.json"
		for windowStart := start; !windowStart.After(end); windowStart = windowStart.AddDate(0, 0, timeseriesWindowDays) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			windowEnd := windowStart.AddDate(0, 0, timeseriesWindowDays-1)
			if windowEnd.After(end) {
				windowEnd = end
			}
			params := url.Values{
				"period":   {"custom"},
				"interval": {"day"},
				"from":     {windowStart.Format(dateLayout)},
				"to":       {windowEnd.Format(dateLayout)},
			}
			var resp timeseriesResponse
			if err := s.get(ctx, path, params, &resp); err != nil {
				return fmt.Errorf("failed to fetch daily_stats for %s: %w", siteID, err)
			}
			for _, row := range resp.Data.Series {
				date, _ := row["date"].(string)
				if len(date) < len(dateLayout) {
					continue
				}
				row["date"] = date[:len(dateLayout)]
				row["site_id"] = siteID
				if err := b.add(row); err != nil {
					return err
				}
			}
		}
		if err := b.flush(); err != nil {
			return err
		}
	}
	return b.flush()
}

type breakdownResponse struct {
	Data struct {
		HasMore bool                     `json:"has_more"`
		Rows    []map[string]interface{} `json:"rows"`
	} `json:"data"`
}

func (s *PrivatusSource) readBreakdown(ctx context.Context, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PRIVATUS] reading breakdown %s", spec.dimension)
	start, end, err := dateRange(opts, time.Now())
	if err != nil {
		return err
	}
	siteIDs, err := s.siteIDList(ctx)
	if err != nil {
		return err
	}

	b := newBatcher(opts, results)
	for _, siteID := range siteIDs {
		path := "/sites/" + url.PathEscape(siteID) + "/breakdown/" + url.PathEscape(spec.dimension) + ".json"
		for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
			date := day.Format(dateLayout)
			params := url.Values{
				"period": {"custom"},
				"from":   {date},
				"to":     {date},
				"limit":  {strconv.Itoa(maxBreakdownPageSize)},
			}
			for page := 1; page <= maxPages; page++ {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				params.Set("page", strconv.Itoa(page))
				var resp breakdownResponse
				if err := s.get(ctx, path, params, &resp); err != nil {
					return fmt.Errorf("failed to fetch %s breakdown for %s on %s: %w", spec.dimension, siteID, date, err)
				}
				for _, row := range resp.Data.Rows {
					row["site_id"] = siteID
					row["date"] = date
					if err := b.add(row); err != nil {
						return err
					}
				}
				if !resp.Data.HasMore || len(resp.Data.Rows) == 0 {
					break
				}
				if page == maxPages {
					config.Debug("[PRIVATUS] stopped %s on %s after %d pages", path, date, maxPages)
				}
			}
		}
		if err := b.flush(); err != nil {
			return err
		}
	}
	return b.flush()
}

var _ source.Source = (*PrivatusSource)(nil)
