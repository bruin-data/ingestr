package surveymonkey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://api.surveymonkey.com/v3"

	// SurveyMonkey allows 120 requests/min for draft/private apps
	rateLimit      = (120.0 * 0.8) / 60.0 // ~1.6 req/s
	rateLimitBurst = 5

	maxPageSize           = 100  // most sub-resource endpoints cap at 100
	maxSurveyListPageSize = 1000 // /surveys supports up to 1000
	maxPages              = 1000
	workerCount           = 5
)

var supportedTables = []string{
	"surveys",
	"survey_details",
	"survey_responses",
	"collectors",
	"contact_lists",
	"contacts",
}

type SurveyMonkeySource struct {
	accessToken string
	client      *gonghttp.Client
}

func NewSurveyMonkeySource() *SurveyMonkeySource {
	return &SurveyMonkeySource{}
}

func (s *SurveyMonkeySource) HandlesIncrementality() bool {
	return true
}

func (s *SurveyMonkeySource) Schemes() []string {
	return []string{"surveymonkey"}
}

func (s *SurveyMonkeySource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.accessToken = creds.accessToken

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(creds.apiURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(s.accessToken)),
	)
	config.Debug("[SURVEYMONKEY] Connected successfully")
	return nil
}

func (s *SurveyMonkeySource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

type surveyMonkeyCredentials struct {
	accessToken string
	apiURL      string
}

func parseURI(uri string) (*surveyMonkeyCredentials, error) {
	if !strings.HasPrefix(uri, "surveymonkey://") {
		return nil, fmt.Errorf("invalid surveymonkey URI: must start with surveymonkey://")
	}

	rest := strings.TrimPrefix(uri, "surveymonkey://")
	if rest == "" || rest == "?" {
		return nil, fmt.Errorf("access_token is required in surveymonkey URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse surveymonkey URI query: %w", err)
	}

	accessToken := values.Get("access_token")
	if accessToken == "" {
		return nil, fmt.Errorf("access_token is required in surveymonkey URI")
	}

	apiURL := baseURL
	if dc := values.Get("region"); dc != "" {
		switch dc {
		case "us":
			apiURL = "https://api.surveymonkey.com/v3"
		case "eu":
			apiURL = "https://api.eu.surveymonkey.com/v3"
		case "ca":
			apiURL = "https://api.surveymonkey.ca/v3"
		default:
			return nil, fmt.Errorf("invalid region %q: must be us, eu, or ca", dc)
		}
	}

	return &surveyMonkeyCredentials{
		accessToken: accessToken,
		apiURL:      apiURL,
	}, nil
}

func (s *SurveyMonkeySource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	incrementalKey := ""
	strategy := config.StrategyReplace
	primaryKey := "id"

	switch tableName {
	case "surveys":
		incrementalKey = "date_modified"
		strategy = config.StrategyMerge
	case "survey_details":
		incrementalKey = "date_modified"
		strategy = config.StrategyMerge
	case "survey_responses":
		incrementalKey = "date_modified"
		strategy = config.StrategyMerge
	case "collectors":
		incrementalKey = "date_modified"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{primaryKey},
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("surveymonkey source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func (s *SurveyMonkeySource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "surveys":
			err = s.readSurveys(ctx, opts, results)
		case "survey_details":
			err = s.readSurveyDetails(ctx, opts, results)
		case "survey_responses":
			err = s.readSurveyResponses(ctx, opts, results)
		case "collectors":
			err = s.readCollectors(ctx, opts, results)
		case "contact_lists":
			err = s.readContactLists(ctx, opts, results)
		case "contacts":
			err = s.readContacts(ctx, opts, results)
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

func extractItems(body map[string]interface{}) []map[string]interface{} {
	dataRaw, ok := body["data"].([]interface{})
	if !ok {
		return nil
	}

	items := make([]map[string]interface{}, 0, len(dataRaw))
	for _, d := range dataRaw {
		if item, ok := d.(map[string]interface{}); ok {
			items = append(items, item)
		}
	}
	return items
}

func hasNextPage(body map[string]interface{}) bool {
	links, ok := body["links"].(map[string]interface{})
	if !ok {
		return false
	}
	next, ok := links["next"].(string)
	return ok && next != ""
}

type paginateConfig struct {
	endpoint     string
	label        string
	pageSize     int
	queryParams  map[string]string
	injectFields map[string]string
	startParam   string // query param name for interval start filter
	endParam     string // query param name for interval end filter
}

// paginateAndSend is the shared pagination loop for all list endpoints.
func (s *SurveyMonkeySource) paginateAndSend(ctx context.Context, cfg paginateConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	page := 1
	totalSent := 0
	pageSize := cfg.pageSize
	if pageSize == 0 {
		pageSize = maxPageSize
	}

	for page <= maxPages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("page", strconv.Itoa(page)).
			SetQueryParam("per_page", strconv.Itoa(pageSize))

		for k, v := range cfg.queryParams {
			req.SetQueryParam(k, v)
		}

		if cfg.startParam != "" && opts.IntervalStart != nil {
			req.SetQueryParam(cfg.startParam, opts.IntervalStart.UTC().Format("2006-01-02T15:04:05"))
		}
		if cfg.endParam != "" && opts.IntervalEnd != nil {
			req.SetQueryParam(cfg.endParam, opts.IntervalEnd.UTC().Format("2006-01-02T15:04:05"))
		}

		resp, err := req.Get(cfg.endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", cfg.label, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("surveymonkey API %s returned status %d: %s", cfg.endpoint, resp.StatusCode(), resp.String())
		}

		var body map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", cfg.label, err)
		}

		items := extractItems(body)
		if len(items) == 0 {
			break
		}

		for _, item := range items {
			for k, v := range cfg.injectFields {
				if _, exists := item[k]; !exists {
					item[k] = v
				}
			}
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", cfg.label, err)
		}

		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(items)
		config.Debug("[SURVEYMONKEY] %s page %d: sent %d records (total: %d)", cfg.label, page, len(items), totalSent)

		if !hasNextPage(body) {
			break
		}
		page++
	}

	if totalSent == 0 {
		config.Debug("[SURVEYMONKEY] No %s found", cfg.label)
	}
	return nil
}

// forEachSurvey runs fn for each survey in parallel using a worker pool.
func (s *SurveyMonkeySource) forEachSurvey(ctx context.Context, fn func(ctx context.Context, surveyID string) error) error {
	surveyIDs, err := s.getAllSurveyIDs(ctx)
	if err != nil {
		return err
	}

	sem := make(chan struct{}, workerCount)
	var mu sync.Mutex
	var firstErr error

	var wg sync.WaitGroup
	for _, surveyID := range surveyIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := fn(ctx, id); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(surveyID)
	}

	wg.Wait()
	return firstErr
}

func (s *SurveyMonkeySource) getAllSurveyIDs(ctx context.Context) ([]string, error) {
	var surveyIDs []string
	page := 1

	for page <= maxPages {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("page", strconv.Itoa(page)).
			SetQueryParam("per_page", strconv.Itoa(maxSurveyListPageSize)).
			Get("/surveys")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch surveys for IDs: %w", err)
		}

		if !resp.IsSuccess() {
			return nil, fmt.Errorf("surveymonkey API /surveys returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var body map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return nil, fmt.Errorf("failed to parse surveys response: %w", err)
		}

		items := extractItems(body)
		for _, item := range items {
			if id, ok := item["id"].(string); ok {
				surveyIDs = append(surveyIDs, id)
			}
		}

		if !hasNextPage(body) {
			break
		}
		page++
	}

	config.Debug("[SURVEYMONKEY] Found %d surveys", len(surveyIDs))
	return surveyIDs, nil
}

// --- table readers ---

func (s *SurveyMonkeySource) readSurveys(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SURVEYMONKEY] reading surveys")
	return s.paginateAndSend(ctx, paginateConfig{
		endpoint: "/surveys",
		label:    "surveys",
		pageSize: maxSurveyListPageSize,
		queryParams: map[string]string{
			"include": "response_count,date_created,date_modified,language,question_count,analyze_url,preview,shared_with,shared_by,owned",
		},
		startParam: "start_modified_at",
		endParam:   "end_modified_at",
	}, opts, results)
}

func (s *SurveyMonkeySource) readSurveyDetails(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SURVEYMONKEY] reading survey_details")

	// Pre-filter survey IDs using server-side date filtering on /surveys
	surveyIDs, err := s.getSurveyIDsWithInterval(ctx, opts.IntervalStart, opts.IntervalEnd)
	if err != nil {
		return err
	}

	sem := make(chan struct{}, workerCount)
	var mu sync.Mutex
	var firstErr error

	var wg sync.WaitGroup
	for _, surveyID := range surveyIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()

			resp, err := s.client.R(ctx).Get(fmt.Sprintf("/surveys/%s/details", id))
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to fetch survey details for %s: %w", id, err)
				}
				mu.Unlock()
				return
			}

			if !resp.IsSuccess() {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("surveymonkey API /surveys/%s/details returned status %d: %s", id, resp.StatusCode(), resp.String())
				}
				mu.Unlock()
				return
			}

			var item map[string]interface{}
			if err := jsonUseNumber(resp.Body(), &item); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to parse survey details for %s: %w", id, err)
				}
				mu.Unlock()
				return
			}

			record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{item}, nil, opts.ExcludeColumns)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to convert survey details to Arrow: %w", err)
				}
				mu.Unlock()
				return
			}

			results <- source.RecordBatchResult{Batch: record}
			config.Debug("[SURVEYMONKEY] sent details for survey %s", id)
		}(surveyID)
	}

	wg.Wait()
	return firstErr
}

// getSurveyIDsWithInterval fetches survey IDs, optionally filtered by date_modified.
func (s *SurveyMonkeySource) getSurveyIDsWithInterval(ctx context.Context, start, end *time.Time) ([]string, error) {
	var surveyIDs []string
	page := 1

	for page <= maxPages {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("page", strconv.Itoa(page)).
			SetQueryParam("per_page", strconv.Itoa(maxSurveyListPageSize))

		if start != nil {
			req.SetQueryParam("start_modified_at", start.UTC().Format("2006-01-02T15:04:05"))
		}
		if end != nil {
			req.SetQueryParam("end_modified_at", end.UTC().Format("2006-01-02T15:04:05"))
		}

		resp, err := req.Get("/surveys")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch surveys: %w", err)
		}

		if !resp.IsSuccess() {
			return nil, fmt.Errorf("surveymonkey API /surveys returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var body map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return nil, fmt.Errorf("failed to parse surveys response: %w", err)
		}

		items := extractItems(body)
		for _, item := range items {
			if id, ok := item["id"].(string); ok {
				surveyIDs = append(surveyIDs, id)
			}
		}

		if !hasNextPage(body) {
			break
		}
		page++
	}

	config.Debug("[SURVEYMONKEY] Found %d surveys (filtered)", len(surveyIDs))
	return surveyIDs, nil
}

func (s *SurveyMonkeySource) readSurveyResponses(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SURVEYMONKEY] reading survey_responses")
	return s.forEachSurvey(ctx, func(ctx context.Context, id string) error {
		return s.paginateAndSend(ctx, paginateConfig{
			endpoint:     fmt.Sprintf("/surveys/%s/responses/bulk", id),
			label:        fmt.Sprintf("survey %s responses", id),
			pageSize:     maxPageSize,
			injectFields: map[string]string{"survey_id": id},
			startParam:   "start_modified_at",
			endParam:     "end_modified_at",
		}, opts, results)
	})
}

func (s *SurveyMonkeySource) readCollectors(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SURVEYMONKEY] reading collectors")
	return s.forEachSurvey(ctx, func(ctx context.Context, id string) error {
		return s.paginateAndSend(ctx, paginateConfig{
			endpoint: fmt.Sprintf("/surveys/%s/collectors", id),
			label:    fmt.Sprintf("survey %s collectors", id),
			queryParams: map[string]string{
				"include": "type,status,response_count,date_created,date_modified,url",
			},
			injectFields: map[string]string{"survey_id": id},
			startParam:   "start_date",
			endParam:     "end_date",
		}, opts, results)
	})
}

func (s *SurveyMonkeySource) readContactLists(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SURVEYMONKEY] reading contact_lists")
	return s.paginateAndSend(ctx, paginateConfig{
		endpoint: "/contact_lists",
		label:    "contact_lists",
	}, opts, results)
}

func (s *SurveyMonkeySource) readContacts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SURVEYMONKEY] reading contacts")
	// Fetch all statuses: the default is "active" which excludes bounced/optout contacts
	for _, status := range []string{"active", "optout", "bounced"} {
		if err := s.paginateAndSend(ctx, paginateConfig{
			endpoint:    "/contacts",
			label:       "contacts (" + status + ")",
			queryParams: map[string]string{"status": status},
		}, opts, results); err != nil {
			return err
		}
	}
	return nil
}
