package typeform

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
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL   = "https://api.typeform.com"
	baseURLEU = "https://api.eu.typeform.com"

	// Typeform allows 2 requests/second per account for the Create and Responses APIs.
	rateLimit      = 2.0 * 0.8 // 1.6 req/s
	rateLimitBurst = 5

	maxPageSize         = 200  // forms/workspaces/themes cap at 200 results per page
	maxResponsePageSize = 1000 // the responses endpoint caps at 1000 per page
	maxPages            = 1000
	workerCount         = 5
)

var supportedTables = []string{
	"forms",
	"responses",
	"workspaces",
	"themes",
}

type TypeformSource struct {
	token  string
	client *httpclient.Client
}

func NewTypeformSource() *TypeformSource {
	return &TypeformSource{}
}

func (s *TypeformSource) HandlesIncrementality() bool {
	return true
}

func (s *TypeformSource) Schemes() []string {
	return []string{"typeform"}
}

func (s *TypeformSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.token = creds.token

	s.client = httpclient.New(
		httpclient.WithBaseURL(creds.apiURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(s.token)),
	)
	config.Debug("[TYPEFORM] Connected successfully")
	return nil
}

func (s *TypeformSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

type typeformCredentials struct {
	token  string
	apiURL string
}

func parseURI(uri string) (*typeformCredentials, error) {
	if !strings.HasPrefix(uri, "typeform://") {
		return nil, fmt.Errorf("invalid typeform URI: must start with typeform://")
	}

	rest := strings.TrimPrefix(uri, "typeform://")
	if rest == "" || rest == "?" {
		return nil, fmt.Errorf("token is required in typeform URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse typeform URI query: %w", err)
	}

	token := values.Get("token")
	if token == "" {
		return nil, fmt.Errorf("token is required in typeform URI")
	}

	apiURL := baseURL
	if region := values.Get("region"); region != "" {
		switch region {
		case "us":
			apiURL = baseURL
		case "eu":
			apiURL = baseURLEU
		default:
			return nil, fmt.Errorf("invalid region %q: must be us or eu", region)
		}
	}

	return &typeformCredentials{
		token:  token,
		apiURL: apiURL,
	}, nil
}

func (s *TypeformSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	incrementalKey := ""
	strategy := config.StrategyReplace
	primaryKey := "id"

	switch tableName {
	case "forms":
		incrementalKey = "last_updated_at"
		strategy = config.StrategyMerge
	case "responses":
		incrementalKey = "submitted_at"
		strategy = config.StrategyMerge
		primaryKey = "response_id"
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{primaryKey},
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("typeform source does not have a predefined schema; schema inference is required")
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

func (s *TypeformSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "forms":
			err = s.readForms(ctx, opts, results)
		case "responses":
			err = s.readResponses(ctx, opts, results)
		case "workspaces":
			err = s.readWorkspaces(ctx, opts, results)
		case "themes":
			err = s.readThemes(ctx, opts, results)
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
	raw, ok := body["items"].([]interface{})
	if !ok {
		return nil
	}

	items := make([]map[string]interface{}, 0, len(raw))
	for _, d := range raw {
		if item, ok := d.(map[string]interface{}); ok {
			items = append(items, item)
		}
	}
	return items
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case float64:
		return int(n)
	}
	return 0
}

func filterItemsByInterval(items []map[string]interface{}, incrementalKey string, start, end *time.Time) []map[string]interface{} {
	if incrementalKey == "" || (start == nil && end == nil) {
		return items
	}

	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		raw, ok := item[incrementalKey]
		if !ok || raw == nil {
			filtered = append(filtered, item)
			continue
		}

		str, ok := raw.(string)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		t, err := time.Parse(time.RFC3339, str)
		if err != nil {
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

type pageConfig struct {
	endpoint     string
	label        string
	pageSize     int
	queryParams  map[string]string
	intervalKey  string            // if set, client-side filter items by this field
	injectFields map[string]string // fields injected into every item (e.g. parent id)
}

// paginatePages is the shared page-number pagination loop for the forms,
// workspaces, and themes endpoints, which share the {items, page_count} envelope.
func (s *TypeformSource) paginatePages(ctx context.Context, cfg pageConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
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
			SetQueryParam("page_size", strconv.Itoa(pageSize))

		for k, v := range cfg.queryParams {
			req.SetQueryParam(k, v)
		}

		resp, err := req.Get(cfg.endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", cfg.label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("typeform API %s returned status %d: %s", cfg.endpoint, resp.StatusCode(), resp.String())
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

		if cfg.intervalKey != "" {
			items = filterItemsByInterval(items, cfg.intervalKey, opts.IntervalStart, opts.IntervalEnd)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", cfg.label, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
			config.Debug("[TYPEFORM] %s page %d: sent %d records (total: %d)", cfg.label, page, len(items), totalSent)
		}

		pageCount := toInt(body["page_count"])
		if pageCount > 0 && page >= pageCount {
			break
		}
		if pageCount == 0 && len(extractItems(body)) < pageSize {
			break
		}
		page++
	}

	if totalSent == 0 {
		config.Debug("[TYPEFORM] No %s found", cfg.label)
	}
	return nil
}

// --- table readers ---

func (s *TypeformSource) readForms(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[TYPEFORM] reading forms")
	// The /forms endpoint has no server-side time filter, so filter client-side
	// on last_updated_at after fetching each page.
	return s.paginatePages(ctx, pageConfig{
		endpoint:    "/forms",
		label:       "forms",
		pageSize:    maxPageSize,
		intervalKey: "last_updated_at",
	}, opts, results)
}

func (s *TypeformSource) readWorkspaces(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[TYPEFORM] reading workspaces")
	return s.paginatePages(ctx, pageConfig{
		endpoint: "/workspaces",
		label:    "workspaces",
		pageSize: maxPageSize,
	}, opts, results)
}

func (s *TypeformSource) readThemes(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[TYPEFORM] reading themes")
	return s.paginatePages(ctx, pageConfig{
		endpoint: "/themes",
		label:    "themes",
		pageSize: maxPageSize,
	}, opts, results)
}

func (s *TypeformSource) readResponses(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[TYPEFORM] reading responses")

	formIDs, err := s.getAllFormIDs(ctx)
	if err != nil {
		return err
	}

	sem := make(chan struct{}, workerCount)
	var mu sync.Mutex
	var firstErr error

	var wg sync.WaitGroup
	for _, formID := range formIDs {
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

			if err := s.readFormResponses(ctx, id, opts, results); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(formID)
	}

	wg.Wait()
	return firstErr
}

// readFormResponses walks a single form's responses using cursor pagination.
// since/until filter server-side on submitted_at; before is the token cursor.
func (s *TypeformSource) readFormResponses(ctx context.Context, formID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/forms/%s/responses", formID)
	before := ""
	totalSent := 0

	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("page_size", strconv.Itoa(maxResponsePageSize))

		if opts.IntervalStart != nil {
			req.SetQueryParam("since", opts.IntervalStart.UTC().Format(time.RFC3339))
		}
		if opts.IntervalEnd != nil {
			req.SetQueryParam("until", opts.IntervalEnd.UTC().Format(time.RFC3339))
		}
		if before != "" {
			req.SetQueryParam("before", before)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch responses for form %s: %w", formID, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("typeform API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var body map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse responses for form %s: %w", formID, err)
		}

		items := extractItems(body)
		if len(items) == 0 {
			break
		}

		for _, item := range items {
			if _, exists := item["form_id"]; !exists {
				item["form_id"] = formID
			}
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert responses for form %s to Arrow: %w", formID, err)
		}
		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(items)
		config.Debug("[TYPEFORM] form %s: sent %d responses (total: %d)", formID, len(items), totalSent)

		if len(items) < maxResponsePageSize {
			break
		}

		token, _ := items[len(items)-1]["token"].(string)
		if token == "" || token == before {
			break
		}
		before = token
	}

	return nil
}

func (s *TypeformSource) getAllFormIDs(ctx context.Context) ([]string, error) {
	var formIDs []string
	page := 1

	for page <= maxPages {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("page", strconv.Itoa(page)).
			SetQueryParam("page_size", strconv.Itoa(maxPageSize)).
			Get("/forms")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch forms for IDs: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("typeform API /forms returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var body map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return nil, fmt.Errorf("failed to parse forms response: %w", err)
		}

		items := extractItems(body)
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			if id, ok := item["id"].(string); ok {
				formIDs = append(formIDs, id)
			}
		}

		pageCount := toInt(body["page_count"])
		if pageCount > 0 && page >= pageCount {
			break
		}
		if pageCount == 0 && len(items) < maxPageSize {
			break
		}
		page++
	}

	config.Debug("[TYPEFORM] Found %d forms", len(formIDs))
	return formIDs, nil
}
