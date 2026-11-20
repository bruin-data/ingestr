package breezyhr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL     = "https://api.breezy.hr/v3"
	maxPageSize = 50
	// Breezy allows 100 requests per minute per access token; 80% of that.
	rateLimit      = 100 * 0.8 / 60.0
	rateLimitBurst = 5
)

var supportedTables = []string{
	"departments",
	"pipelines",
	"questionnaires",
	"custom_fields",
	"positions",
	"position_team_members",
	"position_stream",
	"position_custom_fields",
	"candidates",
	"candidate_stream",
	"candidate_scorecards",
	"candidate_questionnaires",
	"candidate_custom_fields",
}

// The positions endpoint hides archived positions when state is omitted, so
// every state is requested explicitly.
var positionStates = []string{"published", "draft", "closed", "archived", "pending"}

var customDefinitionTypes = []string{"candidate", "position"}

// candidateExtra describes a per-candidate list read through the bulk endpoint.
// A list the bulk endpoint truncated is re-read from its own endpoint: lists
// with a limit are truncated at that size, and a paged stream is complete only
// once it reaches the event flagged first_activity.
type candidateExtra struct {
	include  string
	limit    int
	endpoint string
	paged    bool
}

var candidateExtras = map[string]candidateExtra{
	"candidate_stream":         {include: "stream", endpoint: "stream", paged: true},
	"candidate_scorecards":     {include: "scorecards"},
	"candidate_questionnaires": {include: "questionnaires", limit: 10, endpoint: "questionnaires"},
	"candidate_custom_fields":  {include: "custom_fields"},
}

func (e candidateExtra) truncated(items []map[string]interface{}) bool {
	if e.paged {
		return len(items) > 0 && !hasFirstActivity(items)
	}
	return e.limit > 0 && len(items) >= e.limit
}

func hasFirstActivity(items []map[string]interface{}) bool {
	for _, item := range items {
		if item["first_activity"] == true {
			return true
		}
	}
	return false
}

const (
	extraPageSize      = 50
	positionStreamPage = 30
	// Caps the fallback candidate paging at 50,000 candidates per position.
	maxCandidatePages = 1000
	// The API clamps skip at 10000, so paging stops there.
	maxSkip = 10000
)

type BreezyHRSource struct {
	companyID string
	client    *httpclient.Client
}

func NewBreezyHRSource() *BreezyHRSource {
	return &BreezyHRSource{}
}

func (s *BreezyHRSource) Schemes() []string {
	return []string{"breezyhr"}
}

func (s *BreezyHRSource) HandlesIncrementality() bool {
	return true
}

func (s *BreezyHRSource) Connect(ctx context.Context, uri string) error {
	apiKey, companyID, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.companyID = companyID

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		// The only POST is the read-only candidates bulk endpoint.
		httpclient.WithAllowNonIdempotentRetry(),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithHeader("Authorization", apiKey),
	)

	config.Debug("[BREEZYHR] Connected successfully")
	return nil
}

func (s *BreezyHRSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (string, string, error) {
	if !strings.HasPrefix(uri, "breezyhr://") {
		return "", "", fmt.Errorf("invalid breezyhr URI: must start with breezyhr://")
	}

	rest := strings.TrimPrefix(uri, "breezyhr://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse breezyhr URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", "", fmt.Errorf("api_key is required in breezyhr URI")
	}
	companyID := values.Get("company_id")
	if companyID == "" {
		return "", "", fmt.Errorf("company_id is required in breezyhr URI")
	}

	return apiKey, companyID, nil
}

func (s *BreezyHRSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	name := req.Name
	if !isValidTable(name) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", name, strings.Join(supportedTables, ", "))
	}

	primaryKeys := []string{"_id"}
	incrementalKey := ""
	strategy := config.StrategyReplace
	switch name {
	case "departments":
		primaryKeys = []string{"company_id", "id"}
	case "pipelines", "questionnaires":
		primaryKeys = []string{"company_id", "_id"}
	case "custom_fields":
		primaryKeys = []string{"company_id", "type", "_id"}
	case "position_team_members", "position_custom_fields":
		primaryKeys = []string{"position_id", "_id"}
	case "position_stream":
		incrementalKey = "timestamp"
		strategy = config.StrategyMerge
	}
	if _, ok := candidateExtras[name]; ok {
		primaryKeys = []string{"candidate_id", "_id"}
	}

	return &source.DynamicSourceTable{
		TableName:            name,
		TablePrimaryKeys:     primaryKeys,
		TableIncrementalKey:  incrementalKey,
		TableStrategy:        strategy,
		TableIgnoresInterval: strategy == config.StrategyReplace,
		KnownSchema:          false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("breezyhr source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			// The pipeline's schema-inference read passes the interval even to
			// interval-ignoring tables, and a replace load must never be partial.
			if strategy == config.StrategyReplace {
				opts.IntervalStart, opts.IntervalEnd = nil, nil
			}
			return s.read(ctx, name, opts)
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

func (s *BreezyHRSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "departments":
			err = s.readCompanyList(ctx, "departments", opts, results)
		case "questionnaires":
			err = s.readCompanyList(ctx, "questionnaires", opts, results)
		case "pipelines":
			err = s.readPipelines(ctx, opts, results)
		case "custom_fields":
			err = s.readCustomFields(ctx, opts, results)
		case "positions":
			err = s.readPositions(ctx, opts, results)
		case "position_team_members":
			err = s.readPositionList(ctx, "team", "team", opts, results)
		case "position_custom_fields":
			err = s.readPositionList(ctx, "custom fields", "custom-fields", opts, results)
		case "position_stream":
			err = s.readPositionStreams(ctx, opts, results)
		case "candidates":
			err = s.readCandidates(ctx, opts, results)
		default:
			extra, ok := candidateExtras[table]
			if !ok {
				err = fmt.Errorf("unsupported table: %s", table)
				break
			}
			err = s.readCandidateExtra(ctx, table, extra, opts, results)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *BreezyHRSource) get(ctx context.Context, endpoint string, params map[string]string, out interface{}) error {
	return s.getWith(s.client.R(ctx), endpoint, params, out)
}

func (s *BreezyHRSource) getWith(req *httpclient.Request, endpoint string, params map[string]string, out interface{}) error {
	if params != nil {
		req.SetQueryParams(params)
	}
	resp, err := req.Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
	}
	if !resp.IsSuccess() {
		return &statusError{endpoint: endpoint, status: resp.StatusCode(), body: resp.String()}
	}

	decoder := json.NewDecoder(bytes.NewReader(resp.Body()))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
	}
	return nil
}

type statusError struct {
	endpoint string
	status   int
	body     string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%s returned status %d: %s", e.endpoint, e.status, e.body)
}

func hasStatus(err error, codes ...int) bool {
	var se *statusError
	if !errors.As(err, &se) {
		return false
	}
	for _, c := range codes {
		if se.status == c {
			return true
		}
	}
	return false
}

func (s *BreezyHRSource) getArray(ctx context.Context, endpoint string, params map[string]string) ([]map[string]interface{}, error) {
	var arr []map[string]interface{}
	if err := s.get(ctx, endpoint, params, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

func (s *BreezyHRSource) sendItems(ctx context.Context, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	var batch []map[string]interface{}
	var accBytes int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert to Arrow: %w", err)
		}
		select {
		case results <- source.RecordBatchResult{Batch: record}:
		case <-ctx.Done():
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

// readCompanyList reads an unpaged list under the company, such as departments.
func (s *BreezyHRSource) readCompanyList(ctx context.Context, path string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BREEZYHR] reading %s", path)
	items, err := s.getArray(ctx, fmt.Sprintf("/company/%s/%s", url.PathEscape(s.companyID), path), nil)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", path, err)
	}
	setField(items, "company_id", s.companyID)
	return s.sendItems(ctx, items, opts, results)
}

// readPipelines flattens the pipelines object, which is keyed by pipeline id.
func (s *BreezyHRSource) readPipelines(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BREEZYHR] reading pipelines")
	var byID map[string]map[string]interface{}
	if err := s.get(ctx, fmt.Sprintf("/company/%s/pipelines", url.PathEscape(s.companyID)), nil, &byID); err != nil {
		return fmt.Errorf("failed to fetch pipelines: %w", err)
	}
	items := make([]map[string]interface{}, 0, len(byID))
	for key, p := range byID {
		if _, ok := p["_id"]; !ok {
			p["_id"] = key
		}
		items = append(items, p)
	}
	setField(items, "company_id", s.companyID)
	return s.sendItems(ctx, items, opts, results)
}

func (s *BreezyHRSource) listPositions(ctx context.Context) ([]map[string]interface{}, error) {
	var all []map[string]interface{}
	seen := make(map[string]bool)
	for _, state := range positionStates {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		items, err := s.getArray(ctx, fmt.Sprintf("/company/%s/positions", url.PathEscape(s.companyID)), map[string]string{"state": state, "include": "candidate_access"})
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s positions: %w", state, err)
		}
		// A position whose state changes between these requests can show up twice.
		for _, item := range items {
			id, _ := item["_id"].(string)
			if id != "" && seen[id] {
				continue
			}
			seen[id] = true
			all = append(all, item)
		}
	}
	setField(all, "company_id", s.companyID)
	return all, nil
}

func (s *BreezyHRSource) readPositions(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BREEZYHR] reading positions")
	positions, err := s.listPositions(ctx)
	if err != nil {
		return err
	}
	return s.sendItems(ctx, positions, opts, results)
}

// forEachPosition calls fn for every position of the company. With
// candidatesOnly, it skips positions whose candidates the token can't read,
// since their candidate endpoints answer 401.
func (s *BreezyHRSource) forEachPosition(ctx context.Context, candidatesOnly bool, fn func(companyID, positionID string) error) error {
	positions, err := s.listPositions(ctx)
	if err != nil {
		return err
	}
	for _, p := range positions {
		positionID, _ := p["_id"].(string)
		if positionID == "" {
			continue
		}
		if candidatesOnly && p["candidate_access"] == false {
			config.Debug("[BREEZYHR] skipping candidates of position %s: no candidate access", positionID)
			continue
		}
		if err := fn(s.companyID, positionID); err != nil {
			return err
		}
	}
	return nil
}

func (s *BreezyHRSource) readCustomFields(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BREEZYHR] reading custom fields")
	for _, kind := range customDefinitionTypes {
		items, err := s.getArray(ctx, fmt.Sprintf("/company/%s/custom-fields/%s", url.PathEscape(s.companyID), kind), nil)
		if err != nil {
			return fmt.Errorf("failed to fetch %s custom fields: %w", kind, err)
		}
		setField(items, "company_id", s.companyID)
		setField(items, "type", kind)
		if err := s.sendItems(ctx, items, opts, results); err != nil {
			return err
		}
	}
	return nil
}

// readPositionList reads an unpaged list under each position.
func (s *BreezyHRSource) readPositionList(ctx context.Context, label, path string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BREEZYHR] reading position %s", label)
	return s.forEachPosition(ctx, false, func(companyID, positionID string) error {
		endpoint := fmt.Sprintf("/company/%s/position/%s/%s", url.PathEscape(companyID), url.PathEscape(positionID), path)
		items, err := s.getArray(ctx, endpoint, nil)
		if err != nil {
			return fmt.Errorf("failed to fetch %s for position %s: %w", label, positionID, err)
		}
		setField(items, "company_id", companyID)
		setField(items, "position_id", positionID)
		return s.sendItems(ctx, items, opts, results)
	})
}

// readPositionStreams pages each position's stream newest first with the
// before-cursor, stopping once a page reaches IntervalStart.
func (s *BreezyHRSource) readPositionStreams(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BREEZYHR] reading position streams")
	return s.forEachPosition(ctx, false, func(companyID, positionID string) error {
		endpoint := fmt.Sprintf("/company/%s/position/%s/stream", url.PathEscape(companyID), url.PathEscape(positionID))
		before := ""
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			var params map[string]string
			if before != "" {
				params = map[string]string{"before": before}
			}
			items, err := s.getArray(ctx, endpoint, params)
			if err != nil {
				return fmt.Errorf("failed to fetch stream for position %s: %w", positionID, err)
			}
			if len(items) == 0 {
				return nil
			}
			setField(items, "company_id", companyID)
			setField(items, "position_id", positionID)
			if err := s.sendItems(ctx, filterItemsByInterval(items, "timestamp", opts.IntervalStart, opts.IntervalEnd), opts, results); err != nil {
				return err
			}

			last := items[len(items)-1]
			if len(items) < positionStreamPage {
				return nil
			}
			if opts.IntervalStart != nil {
				if ts, ok := parseTimestamp(last["timestamp"]); ok && ts.Before(opts.IntervalStart.UTC()) {
					return nil
				}
			}
			before, _ = last["_id"].(string)
			if before == "" {
				return nil
			}
		}
	})
}

// readCandidates reads full candidate profiles through the bulk endpoint, one
// call per page of candidate summaries.
func (s *BreezyHRSource) readCandidates(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BREEZYHR] reading candidates")
	return s.forEachCandidatePage(ctx, func(companyID, positionID string, candidates []map[string]interface{}) error {
		setField(candidates, "company_id", companyID)
		setField(candidates, "position_id", positionID)
		return s.sendItems(ctx, candidates, opts, results)
	}, nil)
}

// readCandidateExtra emits one row per item of a candidate's extra list, such
// as stream events or scorecards, tagged with the candidate it belongs to.
func (s *BreezyHRSource) readCandidateExtra(ctx context.Context, table string, extra candidateExtra, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BREEZYHR] reading %s", table)
	return s.forEachCandidatePage(ctx, func(companyID, positionID string, candidates []map[string]interface{}) error {
		var rows []map[string]interface{}
		for _, c := range candidates {
			candidateID, _ := c["_id"].(string)
			items := toItems(c[extra.include])
			if extra.truncated(items) {
				full, err := s.fetchCandidateExtra(ctx, companyID, positionID, candidateID, extra)
				switch {
				case err == nil:
					items = full
				// The candidate was moved, made private, or removed after the bulk read.
				case hasStatus(err, 401, 403, 412):
					output.Warnf("Warning: could not re-read %s for candidate %s on position %s, keeping the %d items the bulk endpoint returned: %v\n", extra.endpoint, candidateID, positionID, len(items), err)
				default:
					return err
				}
			}
			setField(items, "company_id", companyID)
			setField(items, "position_id", positionID)
			setField(items, "candidate_id", candidateID)
			rows = append(rows, items...)
		}
		return s.sendItems(ctx, rows, opts, results)
	}, []string{extra.include})
}

// fetchCandidateExtra reads a candidate's full extra list from its own endpoint,
// for candidates whose list the bulk endpoint truncated.
func (s *BreezyHRSource) fetchCandidateExtra(ctx context.Context, companyID, positionID, candidateID string, extra candidateExtra) ([]map[string]interface{}, error) {
	endpoint := fmt.Sprintf("/company/%s/position/%s/candidate/%s/%s", url.PathEscape(companyID), url.PathEscape(positionID), url.PathEscape(candidateID), extra.endpoint)
	if !extra.paged {
		items, err := s.getArray(ctx, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s for candidate %s: %w", extra.endpoint, candidateID, err)
		}
		return items, nil
	}

	// Pages can come back short when the token can't see every event type, so
	// paging ends at the first_activity event or an empty page. A new event shifts
	// later pages, so repeats are skipped.
	var all []map[string]interface{}
	seen := make(map[string]bool)
	for skip := 0; ; skip += extraPageSize {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if skip > maxSkip {
			output.Warnf("Warning: %s for candidate %s has more than %d records; only the newest %d are loaded\n", extra.endpoint, candidateID, maxSkip, len(all))
			return all, nil
		}
		items, err := s.getArray(ctx, endpoint, map[string]string{"skip": strconv.Itoa(skip)})
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s for candidate %s: %w", extra.endpoint, candidateID, err)
		}
		if len(items) == 0 {
			return all, nil
		}
		for _, item := range items {
			id, _ := item["_id"].(string)
			if id != "" && seen[id] {
				continue
			}
			seen[id] = true
			all = append(all, item)
		}
		if hasFirstActivity(items) {
			return all, nil
		}
	}
}

type bulkResponse struct {
	Candidates []map[string]interface{} `json:"candidates"`
	Denied     []interface{}            `json:"denied"`
	NotFound   []interface{}            `json:"not_found"`
}

// bulkCandidates reads up to 50 candidates of a position, with the requested extras.
func (s *BreezyHRSource) bulkCandidates(ctx context.Context, companyID, positionID string, ids, include []string) ([]map[string]interface{}, error) {
	endpoint := fmt.Sprintf("/company/%s/position/%s/candidates/bulk", url.PathEscape(companyID), url.PathEscape(positionID))
	body := map[string]interface{}{"candidate_ids": ids}
	if len(include) > 0 {
		body["include"] = include
	}
	resp, err := s.client.R(ctx).SetHeader("Content-Type", "application/json").SetBody(body).Post(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch candidates for position %s: %w", positionID, err)
	}
	if !resp.IsSuccess() {
		return nil, &statusError{endpoint: endpoint, status: resp.StatusCode(), body: resp.String()}
	}

	decoder := json.NewDecoder(bytes.NewReader(resp.Body()))
	decoder.UseNumber()
	var out bulkResponse
	if err := decoder.Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", endpoint, err)
	}
	if len(out.Denied) > 0 || len(out.NotFound) > 0 {
		output.Warnf("Warning: position %s: the bulk endpoint denied %d and could not find %d candidates; they are not loaded\n", positionID, len(out.Denied), len(out.NotFound))
	}
	return out.Candidates, nil
}

// forEachCandidatePage lists every position's candidates and passes them
// through the bulk endpoint to fn, 50 at a time.
func (s *BreezyHRSource) forEachCandidatePage(ctx context.Context, fn func(companyID, positionID string, candidates []map[string]interface{}) error, include []string) error {
	return s.forEachPosition(ctx, true, func(companyID, positionID string) error {
		ids, err := s.positionCandidateIDs(ctx, companyID, positionID)
		if err != nil {
			return err
		}
		for start := 0; start < len(ids); start += maxPageSize {
			candidates, err := s.bulkCandidates(ctx, companyID, positionID, ids[start:min(start+maxPageSize, len(ids))], include)
			if err != nil {
				return err
			}
			if err := fn(companyID, positionID, candidates); err != nil {
				return err
			}
		}
		return nil
	})
}

type candidateSummary struct {
	ID string `json:"_id"`
}

// positionCandidateIDs reads a position's candidate IDs from the unpaged list,
// which has no pages for updates to shift. If that request fails, it pages by
// creation date instead, an order updates can't change.
func (s *BreezyHRSource) positionCandidateIDs(ctx context.Context, companyID, positionID string) ([]string, error) {
	endpoint := fmt.Sprintf("/company/%s/position/%s/candidates", url.PathEscape(companyID), url.PathEscape(positionID))
	var summaries []candidateSummary
	// A failure falls back to paging at once rather than retrying a request that may be too large.
	err := s.getWith(s.client.R(ctx).SetRetryOnRateLimitOnly(), endpoint, nil, &summaries)
	if err == nil {
		return appendCandidateIDs(nil, make(map[string]bool), summaries), nil
	}
	var se *statusError
	if ctx.Err() != nil || (errors.As(err, &se) && se.status < 500) {
		return nil, fmt.Errorf("failed to fetch candidates for position %s: %w", positionID, err)
	}
	output.Warnf("Warning: listing the candidates of position %s in one request failed, paging instead: %v\n", positionID, err)

	var ids []string
	seen := make(map[string]bool)
	// Paging is 1-based; page=0 repeats the first page.
	for page := 1; page <= maxCandidatePages; page++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		var items []candidateSummary
		params := map[string]string{"page_size": strconv.Itoa(maxPageSize), "page": strconv.Itoa(page), "sort": "created"}
		if err := s.get(ctx, endpoint, params, &items); err != nil {
			return nil, fmt.Errorf("failed to fetch candidates for position %s: %w", positionID, err)
		}
		before := len(ids)
		ids = appendCandidateIDs(ids, seen, items)
		if len(items) < maxPageSize || len(ids) == before {
			return ids, nil
		}
	}
	output.Warnf("Warning: position %s has more than %d pages of candidates; only the first %d are loaded\n", positionID, maxCandidatePages, len(ids))
	return ids, nil
}

func appendCandidateIDs(ids []string, seen map[string]bool, items []candidateSummary) []string {
	for _, item := range items {
		if item.ID != "" && !seen[item.ID] {
			seen[item.ID] = true
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func toItems(raw interface{}) []map[string]interface{} {
	arr, _ := raw.([]interface{})
	items := make([]map[string]interface{}, 0, len(arr))
	for _, v := range arr {
		if m, ok := v.(map[string]interface{}); ok {
			items = append(items, m)
		}
	}
	return items
}

func setField(items []map[string]interface{}, key, value string) {
	for _, item := range items {
		item[key] = value
	}
}

func filterItemsByInterval(items []map[string]interface{}, field string, start, end *time.Time) []map[string]interface{} {
	if start == nil && end == nil {
		return items
	}
	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		ts, ok := parseTimestamp(item[field])
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if start != nil && ts.Before(start.UTC()) {
			continue
		}
		if end != nil && ts.After(end.UTC()) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func parseTimestamp(raw interface{}) (time.Time, bool) {
	s, ok := raw.(string)
	if !ok || s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
