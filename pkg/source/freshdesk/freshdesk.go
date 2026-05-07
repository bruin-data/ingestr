package freshdesk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	// Freshdesk API: 200 req/min on Growth, 400 on Pro, 700 on Enterprise.
	// Using ~2.7 req/s (~160/min) to stay safely under Growth tier.
	rateLimit      = 2.7
	rateLimitBurst = 5
	maxPageSize    = 100
	// Freshdesk enforces a hard limit of 300 pages for list endpoints.
	maxPages = 300
)

var supportedTables = []string{
	"tickets",
	"agents",
	"contacts",
	"companies",
	"groups",
	"roles",
}

type FreshdeskSource struct {
	client *gonghttp.Client
}

func NewFreshdeskSource() *FreshdeskSource {
	return &FreshdeskSource{}
}

func (s *FreshdeskSource) HandlesIncrementality() bool {
	return true
}

func (s *FreshdeskSource) Schemes() []string {
	return []string{"freshdesk"}
}

func (s *FreshdeskSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	baseURL := fmt.Sprintf("https://%s.freshdesk.com", creds.subdomain)

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBasicAuth(creds.apiKey, "X")),
	)

	config.Debug("[FRESHDESK] Connected to subdomain: %s", creds.subdomain)
	return nil
}

func (s *FreshdeskSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *FreshdeskSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	baseName, query := parseTableName(tableName)

	if !isValidTable(baseName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", baseName, strings.Join(supportedTables, ", "))
	}

	if query != "" && baseName != "tickets" {
		return nil, fmt.Errorf("query parameter is only supported for tickets table, got %s:%s", baseName, query)
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: "updated_at",
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("freshdesk source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, baseName, query, opts)
		},
	}, nil
}

func parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return table, ""
}

func (s *FreshdeskSource) read(ctx context.Context, table, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "tickets":
			if query != "" {
				err = s.readTicketsSearch(ctx, query, opts, results)
			} else {
				err = s.readTickets(ctx, opts, results)
			}
		case "agents":
			err = s.readAgents(ctx, opts, results)
		case "contacts":
			err = s.readContacts(ctx, opts, results)
		case "companies":
			err = s.readCompanies(ctx, opts, results)
		case "groups":
			err = s.readGroups(ctx, opts, results)
		case "roles":
			err = s.readRoles(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

type freshdeskCredentials struct {
	subdomain string
	apiKey    string
}

// parseURI parses a Freshdesk URI: freshdesk://<domain>?api_key=<api_key>
// The domain can be just the subdomain (e.g. "mycompany") or the full domain (e.g. "mycompany.freshdesk.com").
func parseURI(uri string) (freshdeskCredentials, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return freshdeskCredentials{}, fmt.Errorf("invalid freshdesk URI: %w", err)
	}

	if parsed.Scheme != "freshdesk" {
		return freshdeskCredentials{}, fmt.Errorf("invalid freshdesk URI: must start with freshdesk://")
	}

	domain := parsed.Host
	if domain == "" {
		return freshdeskCredentials{}, fmt.Errorf("domain is required in freshdesk URI: freshdesk://<domain>?api_key=<api_key>")
	}

	// Strip .freshdesk.com if full domain is provided
	if idx := strings.Index(domain, "."); idx > 0 {
		domain = domain[:idx]
	}

	apiKey := parsed.Query().Get("api_key")
	if apiKey == "" {
		return freshdeskCredentials{}, fmt.Errorf("api_key query parameter is required in freshdesk URI: freshdesk://<domain>?api_key=<api_key>")
	}

	return freshdeskCredentials{
		subdomain: domain,
		apiKey:    apiKey,
	}, nil
}

// jsonUseNumber decodes JSON with UseNumber to preserve large integer precision.
func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

// When serverSideFilter is true, updated_since is sent as a query param (tickets, contacts, companies).
// When false (agents, groups, roles), IntervalStart is applied client-side instead.
// IntervalEnd is always applied client-side.
func (s *FreshdeskSource) paginateAndSend(ctx context.Context, endpoint, label string, serverSideFilter bool, opts source.ReadOptions, results chan<- source.RecordBatchResult, extraParams ...map[string]string) error {
	page := 1
	totalProcessed := 0
	limit := opts.Limit

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("per_page", strconv.Itoa(maxPageSize)).
			SetQueryParam("page", strconv.Itoa(page))

		if serverSideFilter && opts.IntervalStart != nil {
			req.SetQueryParam("updated_since", opts.IntervalStart.Format(time.RFC3339))
		}

		for _, params := range extraParams {
			for k, v := range params {
				req.SetQueryParam(k, v)
			}
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("freshdesk %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		var items []map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &items); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		if len(items) == 0 {
			break
		}

		rows := make([]map[string]interface{}, 0, len(items))
		endOutOfRange := false
		for _, row := range items {
			if ts, ok := row["updated_at"].(string); ok {
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					if !serverSideFilter && opts.IntervalStart != nil && t.Before(*opts.IntervalStart) {
						continue
					}
					if opts.IntervalEnd != nil && t.After(*opts.IntervalEnd) {
						endOutOfRange = true
						continue
					}
				}
			}

			rows = append(rows, row)

			if limit > 0 && totalProcessed+len(rows) >= limit {
				rows = rows[:limit-totalProcessed]
				endOutOfRange = true
				break
			}
		}

		if len(rows) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for %s: %w", label, err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			totalProcessed += len(rows)
		}

		if endOutOfRange {
			break
		}

		if len(items) < maxPageSize {
			break
		}

		page++

		if page > maxPages {
			config.Debug("[FRESHDESK] reached max page limit (%d) for %s, stopping pagination", maxPages, label)
			break
		}
	}

	config.Debug("[FRESHDESK] finished reading %s: %d total records", label, totalProcessed)
	return nil
}

const searchMaxPages = 10

// prepareSearchQuery ensures the query is wrapped in double quotes as required by the Freshdesk search API.
func prepareSearchQuery(query string) string {
	query = strings.TrimSpace(query)
	if !strings.HasPrefix(query, `"`) || !strings.HasSuffix(query, `"`) {
		query = strings.ReplaceAll(query, `"`, "")
		query = fmt.Sprintf(`"%s"`, query)
	}
	return query
}

func (s *FreshdeskSource) readTicketsSearch(ctx context.Context, query string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FRESHDESK] searching tickets with query: %s", query)

	query = prepareSearchQuery(query)

	totalProcessed := 0
	limit := opts.Limit

	for page := 1; page <= searchMaxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("query", query).
			SetQueryParam("page", strconv.Itoa(page)).
			Get("/api/v2/search/tickets")
		if err != nil {
			return fmt.Errorf("failed to search tickets: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("freshdesk search tickets returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var result map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &result); err != nil {
			return fmt.Errorf("failed to parse search tickets response: %w", err)
		}

		rawResults, ok := result["results"].([]interface{})
		if !ok || len(rawResults) == 0 {
			break
		}

		rows := make([]map[string]interface{}, 0, len(rawResults))
		done := false
		for _, item := range rawResults {
			row, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			if ts, ok := row["updated_at"].(string); ok {
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					if opts.IntervalStart != nil && t.Before(*opts.IntervalStart) {
						continue
					}
					if opts.IntervalEnd != nil && t.After(*opts.IntervalEnd) {
						continue
					}
				}
			}

			rows = append(rows, row)

			if limit > 0 && totalProcessed+len(rows) >= limit {
				rows = rows[:limit-totalProcessed]
				done = true
				break
			}
		}

		if len(rows) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for search tickets: %w", err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			totalProcessed += len(rows)
		}

		if done || len(rawResults) < 30 {
			break
		}
	}

	config.Debug("[FRESHDESK] finished searching tickets: %d total records", totalProcessed)
	return nil
}

func (s *FreshdeskSource) readTickets(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FRESHDESK] reading tickets")
	return s.paginateAndSend(ctx, "/api/v2/tickets", "tickets", true, opts, results, map[string]string{"include": "description"})
}

func (s *FreshdeskSource) readAgents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FRESHDESK] reading agents")
	return s.paginateAndSend(ctx, "/api/v2/agents", "agents", false, opts, results)
}

func (s *FreshdeskSource) readContacts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FRESHDESK] reading contacts")
	return s.paginateAndSend(ctx, "/api/v2/contacts", "contacts", true, opts, results)
}

func (s *FreshdeskSource) readCompanies(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FRESHDESK] reading companies")
	return s.paginateAndSend(ctx, "/api/v2/companies", "companies", true, opts, results)
}

func (s *FreshdeskSource) readGroups(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FRESHDESK] reading groups")
	return s.paginateAndSend(ctx, "/api/v2/groups", "groups", false, opts, results)
}

func (s *FreshdeskSource) readRoles(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FRESHDESK] reading roles")
	return s.paginateAndSend(ctx, "/api/v2/roles", "roles", false, opts, results)
}
