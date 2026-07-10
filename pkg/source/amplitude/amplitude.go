package amplitude

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
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
	usBaseURL            = "https://amplitude.com"
	euBaseURL            = "https://analytics.eu.amplitude.com"
	exportRateLimit      = 1.0
	exportRateLimitBurst = 2
	apiRateLimit         = 1.6
	apiRateLimitBurst    = 4

	batchSize = 500

	defaultParallelism = 4
)

var supportedTables = []string{
	"events",
	"cohorts",
	"annotations",
	"event_types",
	"event_categories",
	"event_properties",
	"user_properties",
}

var validRegions = map[string]bool{
	"us": true,
	"eu": true,
}

type AmplitudeSource struct {
	exportClient *httpclient.Client
	apiClient    *httpclient.Client
}

func NewAmplitudeSource() *AmplitudeSource {
	return &AmplitudeSource{}
}

func (s *AmplitudeSource) Schemes() []string {
	return []string{"amplitude"}
}

func (s *AmplitudeSource) HandlesIncrementality() bool {
	return true
}

func (s *AmplitudeSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	baseURL := usBaseURL
	if creds.region == "eu" {
		baseURL = euBaseURL
	}

	auth := httpclient.NewBasicAuth(creds.apiKey, creds.secretKey)

	s.exportClient = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(300*time.Second),
		httpclient.WithRateLimiter(exportRateLimit, exportRateLimitBurst),
		httpclient.WithAuth(auth),
		httpclient.WithDebug(config.DebugMode),
	)

	s.apiClient = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(120*time.Second),
		httpclient.WithRateLimiter(apiRateLimit, apiRateLimitBurst),
		httpclient.WithAuth(auth),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
	)

	config.Debug("[AMPLITUDE] Connected successfully (region: %s)", creds.region)
	return nil
}

func (s *AmplitudeSource) Close(ctx context.Context) error {
	var firstErr error
	for _, c := range []*httpclient.Client{s.exportClient, s.apiClient} {
		if c != nil {
			if err := c.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

type amplitudeCredentials struct {
	apiKey    string
	secretKey string
	region    string
}

func parseURI(uri string) (amplitudeCredentials, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return amplitudeCredentials{}, fmt.Errorf("invalid amplitude URI: %w", err)
	}
	if parsed.Scheme != "amplitude" {
		return amplitudeCredentials{}, fmt.Errorf("invalid amplitude URI: must start with amplitude://")
	}

	params := parsed.Query()

	apiKey := params.Get("api_key")
	if apiKey == "" {
		return amplitudeCredentials{}, fmt.Errorf("api_key is required in amplitude URI")
	}
	secretKey := params.Get("secret_key")
	if secretKey == "" {
		return amplitudeCredentials{}, fmt.Errorf("secret_key is required in amplitude URI")
	}

	region := params.Get("region")
	if region == "" {
		region = "us"
	}
	region = strings.ToLower(region)
	if !validRegions[region] {
		return amplitudeCredentials{}, fmt.Errorf("invalid region %q: must be one of us, eu", region)
	}

	return amplitudeCredentials{apiKey: apiKey, secretKey: secretKey, region: region}, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func (s *AmplitudeSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	primaryKeys := []string{"id"}
	incrementalKey := ""
	strategy := config.StrategyReplace

	switch tableName {
	case "events":
		primaryKeys = []string{"uuid"}
		incrementalKey = "server_upload_time"
		strategy = config.StrategyMerge
	case "event_types":
		primaryKeys = []string{"event_type"}
	case "event_properties":
		primaryKeys = []string{"event_property"}
	case "user_properties":
		primaryKeys = []string{"user_property"}
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("amplitude source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *AmplitudeSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "events":
			err = s.readEvents(ctx, opts, results)
		case "cohorts":
			err = s.readCohorts(ctx, opts, results)
		case "annotations":
			err = s.readAnnotations(ctx, opts, results)
		case "event_types":
			err = s.readTaxonomy(ctx, "/api/2/taxonomy/event", "event_types", opts, results)
		case "event_categories":
			err = s.readTaxonomy(ctx, "/api/2/taxonomy/category", "event_categories", opts, results)
		case "event_properties":
			err = s.readTaxonomy(ctx, "/api/2/taxonomy/event-property", "event_properties", opts, results)
		case "user_properties":
			err = s.readTaxonomy(ctx, "/api/2/taxonomy/user-property", "user_properties", opts, results)
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

func emitBatch(ctx context.Context, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record: %w", err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case results <- source.RecordBatchResult{Batch: record}:
	}
	return nil
}

func (s *AmplitudeSource) readTaxonomy(ctx context.Context, endpoint, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[AMPLITUDE] reading %s", label)

	resp, err := s.apiClient.R(ctx).Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", label, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("amplitude %s returned status %d: %s", label, resp.StatusCode(), resp.String())
	}

	var body struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := jsonUseNumber(resp.Body(), &body); err != nil {
		return fmt.Errorf("failed to parse %s response: %w", label, err)
	}

	config.Debug("[AMPLITUDE] %s: fetched %d records", label, len(body.Data))
	return emitBatch(ctx, body.Data, opts, results)
}

func (s *AmplitudeSource) readCohorts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[AMPLITUDE] reading cohorts")

	resp, err := s.apiClient.R(ctx).
		SetQueryParam("includeSyncInfo", "true").
		Get("/api/3/cohorts")
	if err != nil {
		return fmt.Errorf("failed to fetch cohorts: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("amplitude cohorts returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var body struct {
		Cohorts []map[string]interface{} `json:"cohorts"`
	}
	if err := jsonUseNumber(resp.Body(), &body); err != nil {
		return fmt.Errorf("failed to parse cohorts response: %w", err)
	}

	config.Debug("[AMPLITUDE] cohorts: fetched %d records", len(body.Cohorts))
	return emitBatch(ctx, body.Cohorts, opts, results)
}

func (s *AmplitudeSource) readAnnotations(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[AMPLITUDE] reading annotations")

	resp, err := s.apiClient.R(ctx).Get("/api/3/annotations")
	if err != nil {
		return fmt.Errorf("failed to fetch annotations: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("amplitude annotations returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var body struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := jsonUseNumber(resp.Body(), &body); err != nil {
		return fmt.Errorf("failed to parse annotations response: %w", err)
	}

	config.Debug("[AMPLITUDE] annotations: fetched %d records", len(body.Data))
	return emitBatch(ctx, body.Data, opts, results)
}

func (s *AmplitudeSource) readEvents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[AMPLITUDE] reading events")

	end := time.Now().UTC()
	if opts.IntervalEnd != nil {
		end = opts.IntervalEnd.UTC()
	}
	start := end.AddDate(0, 0, -30)
	if opts.IntervalStart != nil {
		start = opts.IntervalStart.UTC()
	}

	// The export end hour is inclusive of the whole hour, but interval-end is
	// exclusive, so the last hour we request is the one ending at interval-end
	// (e.g. interval-end 15:00 -> last export hour T14).
	start = start.Truncate(time.Hour)
	end = end.Add(-time.Nanosecond).Truncate(time.Hour)
	if start.After(end) {
		return nil
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}

	windows := [][2]time.Time{{start, end}}
	if parallelism > 1 && opts.IntervalStart != nil && opts.IntervalEnd != nil {
		windows = splitEventWindows(start, end, parallelism)
	}

	if len(windows) == 1 {
		if err := s.readEventsRange(ctx, windows[0][0], windows[0][1], opts, results); err != nil {
			return err
		}
		config.Debug("[AMPLITUDE] finished reading events")
		return nil
	}

	config.Debug("[AMPLITUDE] parallel events: %d windows over [%s, %s]", len(windows), start.Format("20060102T15"), end.Format("20060102T15"))

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(windows))
	for _, w := range windows {
		wg.Add(1)
		go func(winStart, winEnd time.Time) {
			defer wg.Done()
			if err := s.readEventsRange(workerCtx, winStart, winEnd, opts, results); err != nil {
				errCh <- err
				cancel()
			}
		}(w[0], w[1])
	}
	wg.Wait()
	close(errCh)

	if err := <-errCh; err != nil {
		return err
	}

	config.Debug("[AMPLITUDE] finished reading events")
	return nil
}

func splitEventWindows(start, end time.Time, n int) [][2]time.Time {
	totalHours := int(end.Sub(start).Hours()) + 1
	if n <= 1 || totalHours <= 1 {
		return [][2]time.Time{{start, end}}
	}
	if n > totalHours {
		n = totalHours
	}

	base := totalHours / n
	rem := totalHours % n
	windows := make([][2]time.Time, 0, n)
	cur := start
	for i := 0; i < n; i++ {
		hours := base
		if i < rem {
			hours++
		}
		winEnd := cur.Add(time.Duration(hours-1) * time.Hour)
		windows = append(windows, [2]time.Time{cur, winEnd})
		cur = winEnd.Add(time.Hour)
	}
	return windows
}

func (s *AmplitudeSource) readEventsRange(ctx context.Context, start, end time.Time, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	for winStart := start; !winStart.After(end); {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		winEnd := time.Date(winStart.Year(), winStart.Month(), winStart.Day(), 23, 0, 0, 0, time.UTC)
		if winEnd.After(end) {
			winEnd = end
		}

		startParam := winStart.Format("20060102T15")
		endParam := winEnd.Format("20060102T15")

		resp, err := s.exportClient.R(ctx).
			SetQueryParam("start", startParam).
			SetQueryParam("end", endParam).
			Get("/api/2/export")
		if err != nil {
			return fmt.Errorf("failed to fetch events for %s: %w", startParam, err)
		}
		// 404 means no data was recorded in this window; skip it.
		if resp.StatusCode() == 404 {
			config.Debug("[AMPLITUDE] no events for %s-%s", startParam, endParam)
			winStart = winEnd.Add(time.Hour)
			continue
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("amplitude events for %s returned status %d: %s", startParam, resp.StatusCode(), resp.String())
		}

		body := resp.Body()
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return fmt.Errorf("failed to read export archive for %s: %w", startParam, err)
		}

		batch := make([]map[string]interface{}, 0, batchSize)
		flush := func() error {
			if err := emitBatch(ctx, batch, opts, results); err != nil {
				return err
			}
			batch = batch[:0]
			return nil
		}

		for _, f := range zr.File {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if err := s.readEventFile(ctx, f, opts, &batch, flush, results); err != nil {
				return fmt.Errorf("failed to read %s from export for %s: %w", f.Name, startParam, err)
			}
		}

		if err := flush(); err != nil {
			return err
		}

		winStart = winEnd.Add(time.Hour)
	}
	return nil
}

func (s *AmplitudeSource) readEventFile(ctx context.Context, f *zip.File, opts source.ReadOptions, batch *[]map[string]interface{}, flush func() error, results chan<- source.RecordBatchResult) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	var reader io.Reader = rc
	if strings.HasSuffix(f.Name, ".gz") {
		gz, err := gzip.NewReader(rc)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()
		reader = gz
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 20*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var event map[string]interface{}
		if err := jsonUseNumber(line, &event); err != nil {
			config.Debug("[AMPLITUDE] skipping malformed event line: %v", err)
			continue
		}

		*batch = append(*batch, event)
		if len(*batch) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
