// Package satismeter implements an ingestr source for the SatisMeter REST API
// v3 — NPS / CSAT / CES survey responses.
//
// Docs: https://app.satismeter.com/apidoc
//
// AUTH: a project-scoped API key (Settings > Integrations > API), sent as
// `Authorization: Bearer <key>`. Because the key is scoped to one project, the
// project id must be supplied alongside it — every endpoint is nested under
// /projects/{projectId}.
//
// ⚠️⚠️ THE ONE TRAP THAT MATTERS: `startDate` DEFAULTS TO 30 DAYS AGO.
//
// /responses applies a server-side default of "last 30 days" when startDate is
// omitted. It is not an error and there is no warning — you simply get a recent
// slice that looks like the whole dataset. This source therefore ALWAYS sends an
// explicit startDate, falling back to `historyFloor` when the pipeline gives no
// interval, so "no interval" means "everything" rather than "the last month".
//
// PII: response objects embed `user.email`, `user.name`, `user.userId` and the
// whole `user.traits` bag verbatim. They are passed through unmodified — treat
// the destination table accordingly.
package satismeter

import (
	"context"
	"encoding/json"
	"fmt"
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
	baseURL = "https://app.satismeter.com/api/v3"

	// pageSize is the documented maximum (1-100, default 20).
	maxPageSize = 100

	// maxPages bounds the cursor loop. /responses reports hasNextPage, so this
	// is a backstop against a server that never stops advertising a next page,
	// not the normal exit path. 100 pages x 100 rows = 1M responses.
	maxPages = 10000

	// SatisMeter documents NO rate limit, so this is a deliberately conservative
	// self-imposed ceiling rather than 80% of a published number: 5 req/s with a
	// small burst. Revisit if the vendor ever publishes real limits.
	rateLimit      = 5.0
	rateLimitBurst = 5

	// historyFloor is the explicit startDate used when the pipeline supplies no
	// interval. SatisMeter predates none of our accounts by much, but the point
	// is to defeat the 30-day server-side default rather than to be exact.
	historyFloor = "2010-01-01T00:00:00Z"
)

// supportedTables maps a table name to how it is read.
//
// campaign statistics is deliberately NOT exposed. It is a single aggregate row
// per (campaign, window) with no date dimension, so backfilling it over
// different windows silently rewrites the same key with different totals — the
// same failure the Apple Search Ads *_reports tables have. It is also fully
// derivable from `responses`, which we do ingest.
var supportedTables = map[string]struct{}{
	"responses": {},
	"campaigns": {},
	"project":   {},
}

type SatisMeterSource struct {
	client    *httpclient.Client
	projectID string
}

func NewSatisMeterSource() *SatisMeterSource {
	return &SatisMeterSource{}
}

func (s *SatisMeterSource) Schemes() []string {
	return []string{"satismeter"}
}

func (s *SatisMeterSource) HandlesIncrementality() bool {
	return false
}

func (s *SatisMeterSource) Connect(ctx context.Context, uri string) error {
	apiKey, projectID, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.projectID = projectID
	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithAuth(httpclient.NewBearerAuth(apiKey)),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
	)
	config.Debug("[SATISMETER] connected, project %s", projectID)
	return nil
}

func (s *SatisMeterSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (apiKey, projectID string, err error) {
	if !strings.HasPrefix(uri, "satismeter://") {
		return "", "", fmt.Errorf("invalid satismeter URI: must start with satismeter://")
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(uri, "satismeter://"), "?")
	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse satismeter URI query: %w", err)
	}
	apiKey = values.Get("api_key")
	if apiKey == "" {
		return "", "", fmt.Errorf("api_key is required in satismeter URI")
	}
	projectID = values.Get("project_id")
	if projectID == "" {
		return "", "", fmt.Errorf("project_id is required in satismeter URI")
	}
	return apiKey, projectID, nil
}

func isValidTable(name string) bool {
	_, ok := supportedTables[name]
	return ok
}

func supportedTableNames() string {
	names := make([]string, 0, len(supportedTables))
	for n := range supportedTables {
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

func (s *SatisMeterSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if !isValidTable(req.Name) {
		return nil, fmt.Errorf("unsupported satismeter table %q, supported tables are: %s", req.Name, supportedTableNames())
	}

	// `created` is server-side filterable on /responses, which makes it a real
	// incremental key. campaigns/project are small snapshots with no time filter
	// and no update timestamp — merge on their stable id keeps re-runs
	// idempotent. Neither tracks deletions.
	incrementalKey := ""
	if req.Name == "responses" {
		incrementalKey = "created"
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: incrementalKey,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("satismeter source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
		},
	}, nil
}

func (s *SatisMeterSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		var err error
		switch table {
		case "responses":
			err = s.readResponses(ctx, opts, results)
		case "campaigns":
			err = s.readCampaigns(ctx, opts, results)
		case "project":
			err = s.readProject(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported satismeter table: %s", table)
		}
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

// readResponses pages /responses with a cursor, filtering server-side on the
// requested interval.
func (s *SatisMeterSource) readResponses(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SATISMETER] reading responses")

	// Always explicit — see the ⚠️ in the package doc. An omitted startDate is
	// silently rewritten by SatisMeter to "30 days ago".
	startDate := historyFloor
	if opts.IntervalStart != nil {
		startDate = opts.IntervalStart.UTC().Format(time.RFC3339)
	}
	endDate := ""
	if opts.IntervalEnd != nil {
		endDate = opts.IntervalEnd.UTC().Format(time.RFC3339)
	}

	endpoint := "/projects/" + url.PathEscape(s.projectID) + "/responses"
	cursor := ""
	for page := 0; ; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if page >= maxPages {
			config.Debug("[SATISMETER] hit maxPages=%d on responses, stopping", maxPages)
			return fmt.Errorf("responses exceeded the %d page guard; narrow the interval", maxPages)
		}

		req := s.client.R(ctx).
			SetQueryParam("pageSize", strconv.Itoa(maxPageSize)).
			SetQueryParam("startDate", startDate)
		if endDate != "" {
			req = req.SetQueryParam("endDate", endDate)
		}
		if cursor != "" {
			req = req.SetQueryParam("pageCursor", cursor)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch responses: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("responses returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var payload struct {
			Data []map[string]interface{} `json:"data"`
			Page struct {
				HasNextPage    bool   `json:"hasNextPage"`
				NextPageCursor string `json:"nextPageCursor"`
			} `json:"page"`
		}
		// UseNumber: response `answers[].value` and the numeric user traits can
		// exceed float64 precision once ids leak into them.
		dec := json.NewDecoder(strings.NewReader(string(resp.Body())))
		dec.UseNumber()
		if err := dec.Decode(&payload); err != nil {
			return fmt.Errorf("failed to parse responses payload: %w", err)
		}

		if len(payload.Data) > 0 {
			if err := emit(payload.Data, opts, results); err != nil {
				return fmt.Errorf("failed to convert responses to Arrow: %w", err)
			}
			config.Debug("[SATISMETER] responses page %d: %d rows", page, len(payload.Data))
		}

		if !payload.Page.HasNextPage || payload.Page.NextPageCursor == "" {
			return nil
		}
		cursor = payload.Page.NextPageCursor
	}
}

// readCampaigns fetches the survey list. The endpoint returns everything in one
// call — it advertises no cursor and takes no date filter.
func (s *SatisMeterSource) readCampaigns(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SATISMETER] reading campaigns")
	endpoint := "/projects/" + url.PathEscape(s.projectID) + "/campaigns"
	resp, err := s.client.R(ctx).Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch campaigns: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("campaigns returned status %d: %s", resp.StatusCode(), resp.String())
	}
	var payload struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &payload); err != nil {
		return fmt.Errorf("failed to parse campaigns payload: %w", err)
	}
	if len(payload.Data) == 0 {
		return nil
	}
	return emit(payload.Data, opts, results)
}

// readProject fetches the single project object. It is returned bare, not
// wrapped in a `data` envelope like the collection endpoints.
func (s *SatisMeterSource) readProject(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SATISMETER] reading project")
	endpoint := "/projects/" + url.PathEscape(s.projectID)
	resp, err := s.client.R(ctx).Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch project: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("project returned status %d: %s", resp.StatusCode(), resp.String())
	}
	var item map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &item); err != nil {
		return fmt.Errorf("failed to parse project payload: %w", err)
	}
	if len(item) == 0 {
		return nil
	}
	// Some deployments nest the object under `data`; accept both rather than
	// landing a single row whose only column is a JSON blob.
	if inner, ok := item["data"].(map[string]interface{}); ok && len(item) == 1 {
		item = inner
	}
	return emit([]map[string]interface{}{item}, opts, results)
}

func emit(items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return err
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}
