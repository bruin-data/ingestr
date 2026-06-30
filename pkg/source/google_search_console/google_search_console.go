package google_search_console

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"google.golang.org/api/option"
	searchconsole "google.golang.org/api/searchconsole/v1"
)

const (
	maxRowsPerRequest   = 25000
	defaultParallelism  = 5
	defaultLookbackDays = 30
	timeColumn          = "date"
)

const (
	granularityHourly = "hourly"
	granularityDaily  = "daily"
)

var validDimensions = map[string]struct{}{
	"country":          {},
	"device":           {},
	"page":             {},
	"query":            {},
	"searchAppearance": {},
}

type tableConfig struct {
	name           string
	granularity    string
	dimensions     []string
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
}

func (cfg *tableConfig) timeDimension() string {
	switch cfg.granularity {
	case granularityHourly:
		return "hour"
	case granularityDaily:
		return "date"
	default:
		return ""
	}
}

func (cfg *tableConfig) apiDimensions() []string {
	dims := make([]string, 0, len(cfg.dimensions)+1)
	if td := cfg.timeDimension(); td != "" {
		dims = append(dims, td)
	}
	return append(dims, cfg.dimensions...)
}

func (cfg *tableConfig) dataState() string {
	if cfg.granularity == granularityHourly {
		return "HOURLY_ALL"
	}
	return ""
}

type GoogleSearchConsoleSource struct {
	client *searchconsole.Service
	sites  []string
}

func NewGoogleSearchConsoleSource() *GoogleSearchConsoleSource {
	return &GoogleSearchConsoleSource{}
}

func (s *GoogleSearchConsoleSource) Schemes() []string {
	return []string{"gsc", "googlesearchconsole"}
}

func (s *GoogleSearchConsoleSource) Connect(ctx context.Context, uri string) error {
	credJSON, sites, err := parseConnectionURI(uri)
	if err != nil {
		return err
	}

	var opts []option.ClientOption
	if len(credJSON) > 0 {
		opts = append(opts, option.WithAuthCredentialsJSON(option.ServiceAccount, credJSON))
	}

	client, err := searchconsole.NewService(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create search console service: %w", err)
	}

	s.client = client
	s.sites = sites

	config.Debug("[GOOGLE SEARCH CONSOLE] Connected to %d sites", len(sites))
	return nil
}

func (s *GoogleSearchConsoleSource) Close(ctx context.Context) error {
	return nil
}

func (s *GoogleSearchConsoleSource) HandlesIncrementality() bool {
	return true
}

func (s *GoogleSearchConsoleSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("table name is required for google search console source")
	}

	cfg, err := buildTableConfig(req.Name)
	if err != nil {
		return nil, err
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    cfg.primaryKeys,
		TableIncrementalKey: cfg.incrementalKey,
		TableStrategy:       cfg.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("google search console source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, cfg, opts)
		},
	}, nil
}

func buildTableConfig(table string) (*tableConfig, error) {
	switch table {
	case "sites":
		return &tableConfig{
			name:        table,
			primaryKeys: []string{"site_url"},
			strategy:    config.StrategyReplace,
		}, nil
	case "sitemaps":
		return &tableConfig{
			name:        table,
			primaryKeys: []string{"site_url", "path"},
			strategy:    config.StrategyReplace,
		}, nil
	case "searchAppearance":
		// Search appearance cannot be combined with any other dimension
		return &tableConfig{
			name:        table,
			dimensions:  []string{"searchAppearance"},
			primaryKeys: []string{"site_url", "searchAppearance"},
			strategy:    config.StrategyReplace,
		}, nil
	}

	return buildSearchAnalyticsConfig(table)
}

// buildSearchAnalyticsConfig parses a "<granularity>:<dimensions>" table name,
// e.g. "daily:query,country". The dimensions part is optional
func buildSearchAnalyticsConfig(table string) (*tableConfig, error) {
	gran, rest, _ := strings.Cut(table, ":")
	gran = strings.ToLower(strings.TrimSpace(gran))
	if !isValidGranularity(gran) {
		return nil, fmt.Errorf("invalid table %q; expected one of: sites, sitemaps, searchAppearance, or <granularity>:<dimensions> where granularity is hourly or daily (e.g. daily:query,country)", table)
	}

	dims, err := parseDimensions(rest)
	if err != nil {
		return nil, err
	}
	for _, dim := range dims {
		if dim == "searchAppearance" {
			return nil, fmt.Errorf("searchAppearance cannot be combined with a time granularity or other dimensions; request it on its own with --source-table searchAppearance")
		}
	}

	pks := make([]string, 0, len(dims)+2)
	pks = append(pks, "site_url", timeColumn)
	pks = append(pks, dims...)

	return &tableConfig{
		name:           table,
		granularity:    gran,
		dimensions:     dims,
		primaryKeys:    pks,
		incrementalKey: timeColumn,
		strategy:       config.StrategyMerge,
	}, nil
}

func isValidGranularity(gran string) bool {
	switch gran {
	case granularityHourly, granularityDaily:
		return true
	default:
		return false
	}
}

// parseDimensions splits and validates the comma-separated group-by dimensions.
// An empty list is allowed (a time-only report).
func parseDimensions(raw string) ([]string, error) {
	dims := make([]string, 0)
	for _, dim := range strings.Split(raw, ",") {
		dim = strings.TrimSpace(dim)
		if dim == "" {
			continue
		}
		if _, ok := validDimensions[dim]; !ok {
			return nil, fmt.Errorf("invalid dimension %q; valid dimensions are country, device, page, query, searchAppearance", dim)
		}
		dims = append(dims, dim)
	}
	return dims, nil
}

func (s *GoogleSearchConsoleSource) read(ctx context.Context, cfg *tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	// The sites table is a single global call rather than a per-site fan-out.
	if cfg.name == "sites" {
		results := make(chan source.RecordBatchResult, 1)
		go func() {
			defer close(results)
			if err := s.fetchSites(ctx, opts, results); err != nil {
				select {
				case results <- source.RecordBatchResult{Err: err}:
				case <-ctx.Done():
				}
			}
		}()
		return results, nil
	}

	return s.readPerSite(ctx, opts, func(ctx context.Context, siteURL string, out chan<- source.RecordBatchResult) error {
		if cfg.name == "sitemaps" {
			return s.fetchSitemaps(ctx, opts, siteURL, out)
		}
		return s.fetchSearchAnalytics(ctx, cfg, opts, siteURL, out)
	}), nil
}

func (s *GoogleSearchConsoleSource) readPerSite(ctx context.Context, opts source.ReadOptions, fn func(ctx context.Context, siteURL string, out chan<- source.RecordBatchResult) error) <-chan source.RecordBatchResult {
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}
	if parallelism > len(s.sites) {
		parallelism = len(s.sites)
	}

	ctx, cancel := context.WithCancel(ctx)
	taskChan := make(chan string, len(s.sites))
	results := make(chan source.RecordBatchResult, parallelism*2)

	var wg sync.WaitGroup
	for range parallelism {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for siteURL := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if err := fn(ctx, siteURL, results); err != nil {
					select {
					case results <- source.RecordBatchResult{Err: err}:
					case <-ctx.Done():
					}
					cancel()
					return
				}
			}
		}()
	}

	go func() {
		defer close(taskChan)
		for _, siteURL := range s.sites {
			select {
			case taskChan <- siteURL:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
		cancel()
	}()

	return results
}

func (s *GoogleSearchConsoleSource) fetchSearchAnalytics(ctx context.Context, cfg *tableConfig, opts source.ReadOptions, siteURL string, out chan<- source.RecordBatchResult) error {
	now := time.Now().UTC()
	startDate := now.AddDate(0, 0, -defaultLookbackDays)
	endDate := now
	if opts.IntervalStart != nil {
		startDate = *opts.IntervalStart
	}
	if opts.IntervalEnd != nil {
		endDate = *opts.IntervalEnd
	}

	apiDims := cfg.apiDimensions()
	hasTime := cfg.timeDimension() != ""
	dataState := cfg.dataState()

	for startRow := int64(0); ; startRow += maxRowsPerRequest {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := &searchconsole.SearchAnalyticsQueryRequest{
			StartDate:  startDate.Format("2006-01-02"),
			EndDate:    endDate.Format("2006-01-02"),
			Dimensions: apiDims,
			RowLimit:   maxRowsPerRequest,
			StartRow:   startRow,
			// AggregationType is left as the default (AUTO): the API aggregates by page
			// for page/searchAppearance dimensions and by property otherwise.
		}
		if dataState != "" {
			req.DataState = dataState
		}

		resp, err := s.client.Searchanalytics.Query(siteURL, req).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("failed to query search analytics for site %q: %w", siteURL, err)
		}
		if len(resp.Rows) == 0 {
			break
		}

		items := make([]map[string]any, 0, len(resp.Rows))
		for _, row := range resp.Rows {
			item := make(map[string]any, len(apiDims)+5)
			item["site_url"] = siteURL
			for i, dim := range apiDims {
				if i >= len(row.Keys) {
					continue
				}
				if i == 0 && hasTime {
					raw := row.Keys[i]
					if cfg.granularity == granularityHourly {
						if t, err := time.Parse(time.RFC3339, raw); err == nil {
							item[timeColumn] = t.UTC()
						} else {
							item[timeColumn] = raw
						}
					} else if t, err := time.ParseInLocation("2006-01-02", raw, time.UTC); err == nil {
						item[timeColumn] = t
					} else {
						item[timeColumn] = raw
					}
				} else {
					item[dim] = row.Keys[i]
				}
			}
			item["clicks"] = int64(row.Clicks)
			item["impressions"] = int64(row.Impressions)
			item["ctr"] = row.Ctr
			item["position"] = row.Position
			items = append(items, item)
		}

		rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert to arrow: %w", err)
		}
		select {
		case out <- source.RecordBatchResult{Batch: rec}:
		case <-ctx.Done():
			return ctx.Err()
		}

		config.Debug("[GOOGLE SEARCH CONSOLE] Site %q: fetched %d rows (startRow: %d)", siteURL, len(resp.Rows), startRow)

		if int64(len(resp.Rows)) < maxRowsPerRequest {
			break
		}
	}

	return nil
}

func (s *GoogleSearchConsoleSource) fetchSites(ctx context.Context, opts source.ReadOptions, out chan<- source.RecordBatchResult) error {
	resp, err := s.client.Sites.List().Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to list sites: %w", err)
	}

	if len(resp.SiteEntry) == 0 {
		return nil
	}

	items := make([]map[string]any, 0, len(resp.SiteEntry))
	for _, site := range resp.SiteEntry {
		items = append(items, map[string]any{
			"site_url":         site.SiteUrl,
			"permission_level": site.PermissionLevel,
		})
	}

	rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert to arrow: %w", err)
	}
	select {
	case out <- source.RecordBatchResult{Batch: rec}:
	case <-ctx.Done():
		return ctx.Err()
	}

	config.Debug("[GOOGLE SEARCH CONSOLE] fetched %d sites", len(resp.SiteEntry))
	return nil
}

func (s *GoogleSearchConsoleSource) fetchSitemaps(ctx context.Context, opts source.ReadOptions, siteURL string, out chan<- source.RecordBatchResult) error {
	resp, err := s.client.Sitemaps.List(siteURL).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to list sitemaps for site %q: %w", siteURL, err)
	}

	if len(resp.Sitemap) == 0 {
		return nil
	}

	items := make([]map[string]any, 0, len(resp.Sitemap))
	for _, sm := range resp.Sitemap {
		items = append(items, map[string]any{
			"site_url":          siteURL,
			"path":              sm.Path,
			"type":              sm.Type,
			"is_pending":        sm.IsPending,
			"is_sitemaps_index": sm.IsSitemapsIndex,
			"last_downloaded":   parseTimestamp(sm.LastDownloaded),
			"last_submitted":    parseTimestamp(sm.LastSubmitted),
			"errors":            sm.Errors,
			"warnings":          sm.Warnings,
		})
	}

	rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert to arrow: %w", err)
	}
	select {
	case out <- source.RecordBatchResult{Batch: rec}:
	case <-ctx.Done():
		return ctx.Err()
	}

	config.Debug("[GOOGLE SEARCH CONSOLE] Site %q: fetched %d sitemaps", siteURL, len(resp.Sitemap))
	return nil
}

func parseTimestamp(raw string) any {
	if raw == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	return raw
}

func parseConnectionURI(uri string) (credJSON []byte, sites []string, err error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse google search console URI: %w", err)
	}

	if parsed.Scheme != "gsc" && parsed.Scheme != "googlesearchconsole" {
		return nil, nil, fmt.Errorf("invalid google search console URI: must start with gsc:// or googlesearchconsole://")
	}

	params := parsed.Query()

	credPath := params.Get("credentials_path")
	credBase64 := params.Get("credentials_base64")

	// Credentials are optional: when neither is provided, the client falls back
	// to Application Default Credentials (e.g. the gcloud ADC file on the machine).
	switch {
	case credPath != "":
		credJSON, err = os.ReadFile(credPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read credentials file: %w", err)
		}
	case credBase64 != "":
		credJSON, err = base64.StdEncoding.DecodeString(credBase64)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decode credentials_base64: %w", err)
		}
	}

	rawSite := params.Get("site_url")
	if rawSite == "" {
		return nil, nil, fmt.Errorf("site_url is required to connect to Google Search Console")
	}

	seen := make(map[string]struct{})
	for _, site := range strings.Split(rawSite, ",") {
		site = strings.TrimSpace(site)
		if site == "" {
			continue
		}
		if _, ok := seen[site]; ok {
			continue
		}
		seen[site] = struct{}{}
		sites = append(sites, site)
	}
	if len(sites) == 0 {
		return nil, nil, fmt.Errorf("site_url is required to connect to Google Search Console")
	}

	return credJSON, sites, nil
}

var _ source.Source = (*GoogleSearchConsoleSource)(nil)
