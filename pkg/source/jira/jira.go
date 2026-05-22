package jira

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
	// Jira Cloud: 100 GET req/s burst for most endpoints.
	// Using ~80 req/s to stay safely under.
	rateLimit      = 80.0
	rateLimitBurst = 5
	maxPageSize    = 100
	// Safety cap to prevent infinite pagination loops.
	maxPages = 100
	// Jira search/jql uses nextPageToken and pages up to 5000 with fewer fields.
	searchMaxResults = 100
	// Bulk changelog endpoint accepts up to 1000 issue IDs/keys per request.
	changelogBatchSize = 1000
	// Number of parallel workers for per-project and changelog fetches.
	parallelism = 4
)

var supportedTables = []string{
	"projects",
	"issues",
	"users",
	"issue_types",
	"statuses",
	"priorities",
	"resolutions",
	"events",
	"project_versions",
	"project_components",
	"issue_changelogs",
	"issue_fields",
	"issue_custom_field_contexts",
	"issue_custom_field_options",
}

type JiraSource struct {
	client *gonghttp.Client
	domain string
}

func NewJiraSource() *JiraSource {
	return &JiraSource{}
}

func (s *JiraSource) HandlesIncrementality() bool {
	return true
}

func (s *JiraSource) Schemes() []string {
	return []string{"jira"}
}

func (s *JiraSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.domain = creds.domain
	baseURL := fmt.Sprintf("https://%s/rest/api/3", creds.domain)

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBasicAuth(creds.email, creds.apiToken)),
	)

	config.Debug("[JIRA] Connected to domain: %s", creds.domain)
	return nil
}

func (s *JiraSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

var skipArchivedTables = map[string]bool{
	"projects":           true,
	"project_versions":   true,
	"project_components": true,
}

func parseTableName(table string) (name string, skipArchived bool) {
	parts := strings.SplitN(table, ":", 2)
	name = parts[0]
	if len(parts) == 2 && parts[1] == "skip_archived" {
		skipArchived = true
	}
	return name, skipArchived
}

func (s *JiraSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName, skipArchived := parseTableName(req.Name)

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	if skipArchived && !skipArchivedTables[tableName] {
		return nil, fmt.Errorf("table %s does not support :skip_archived (supported: projects, project_versions, project_components)", tableName)
	}

	primaryKeys := []string{"id"}
	incrementalKey := ""
	strategy := config.StrategyReplace

	switch tableName {
	case "issues":
		incrementalKey = "fields_updated"
		strategy = config.StrategyMerge
	case "users":
		primaryKeys = []string{"accountId"}
	case "issue_changelogs":
		primaryKeys = nil
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("jira source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts, skipArchived)
		},
	}, nil
}

func (s *JiraSource) read(ctx context.Context, table string, opts source.ReadOptions, skipArchived bool) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "projects":
			if skipArchived {
				err = s.readProjectSearch(ctx, []string{"live"}, opts, results)
			} else {
				err = s.readPaginatedArray(ctx, "/project", "projects", opts, results)
			}
		case "issues":
			err = s.readIssues(ctx, opts, results)
		case "users":
			err = s.readPaginatedArray(ctx, "/users/search", "users", opts, results)
		case "issue_types":
			err = s.readSimpleList(ctx, "/issuetype", "issue_types", opts, results)
		case "statuses":
			err = s.readSimpleList(ctx, "/status", "statuses", opts, results)
		case "priorities":
			err = s.readSimpleList(ctx, "/priority", "priorities", opts, results)
		case "resolutions":
			err = s.readSimpleList(ctx, "/resolution", "resolutions", opts, results)
		case "events":
			err = s.readSimpleList(ctx, "/events", "events", opts, results)
		case "project_versions":
			err = s.readPerProject(ctx, "/project/%s/version", "project_versions", opts, results, skipArchived)
		case "project_components":
			err = s.readPerProject(ctx, "/project/%s/component", "project_components", opts, results, skipArchived)
		case "issue_changelogs":
			err = s.readIssueChangelogs(ctx, opts, results)
		case "issue_fields":
			err = s.readSimpleList(ctx, "/field", "issue_fields", opts, results)
		case "issue_custom_field_contexts":
			err = s.readCustomFieldContexts(ctx, opts, results)
		case "issue_custom_field_options":
			err = s.readCustomFieldOptions(ctx, opts, results)
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

type jiraCredentials struct {
	domain   string
	email    string
	apiToken string
}

// parseURI parses a Jira URI: jira://<domain>?email=<email>&api_token=<api_token>
// The domain can be just the subdomain (e.g. "mycompany") or the full domain (e.g. "mycompany.atlassian.net").
func parseURI(uri string) (jiraCredentials, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return jiraCredentials{}, fmt.Errorf("invalid jira URI: %w", err)
	}

	if parsed.Scheme != "jira" {
		return jiraCredentials{}, fmt.Errorf("invalid jira URI: must start with jira://")
	}

	domain := parsed.Host
	if domain == "" {
		return jiraCredentials{}, fmt.Errorf("domain is required in jira URI: jira://<domain>?email=<email>&api_token=<api_token>")
	}

	if !strings.Contains(domain, ".") {
		domain = domain + ".atlassian.net"
	}

	email := parsed.Query().Get("email")
	if email == "" {
		return jiraCredentials{}, fmt.Errorf("email query parameter is required in jira URI: jira://<domain>?email=<email>&api_token=<api_token>")
	}

	apiToken := parsed.Query().Get("api_token")
	if apiToken == "" {
		return jiraCredentials{}, fmt.Errorf("api_token query parameter is required in jira URI: jira://<domain>?email=<email>&api_token=<api_token>")
	}

	return jiraCredentials{
		domain:   domain,
		email:    email,
		apiToken: apiToken,
	}, nil
}

func (s *JiraSource) paginateValuesFunc(ctx context.Context, endpoint, label string, onPage func([]map[string]any) error, skipStatuses ...int) error {
	startAt := 0

	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("startAt", strconv.Itoa(startAt)).
			SetQueryParam("maxResults", strconv.Itoa(maxPageSize)).
			Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}

		for _, code := range skipStatuses {
			if resp.StatusCode() == code {
				return nil
			}
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("jira %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		var result map[string]any
		if err := jsonUseNumber(resp.Body(), &result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		rawValues, ok := result["values"].([]any)
		if !ok || len(rawValues) == 0 {
			break
		}

		rows := make([]map[string]any, 0, len(rawValues))
		for _, item := range rawValues {
			if row, ok := item.(map[string]any); ok {
				rows = append(rows, row)
			}
		}

		if err := onPage(rows); err != nil {
			return err
		}

		isLast, _ := result["isLast"].(bool)
		if isLast || len(rawValues) < maxPageSize {
			break
		}
		startAt += len(rawValues)
	}

	return nil
}

// runParallel executes fn for each item in parallel with bounded concurrency.
func runParallel[T any](ctx context.Context, items []T, fn func(context.Context, T) error) error {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	for _, item := range items {
		if workerCtx.Err() != nil {
			break
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(it T) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := fn(workerCtx, it); err != nil {
				errOnce.Do(func() { firstErr = err })
				cancel()
			}
		}(item)
	}

	wg.Wait()
	return firstErr
}

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

func sendBatch(ctx context.Context, rows []map[string]any, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(rows) == 0 {
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record for %s: %w", label, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case results <- source.RecordBatchResult{Batch: record}:
	}

	return nil
}

var jiraTimestampFormats = []string{
	"2006-01-02T15:04:05.000-0700",
	"2006-01-02T15:04:05.000Z",
	time.RFC3339,
}

func parseJiraTimestamp(ts string) (time.Time, error) {
	for _, layout := range jiraTimestampFormats {
		if t, err := time.Parse(layout, ts); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse Jira timestamp: %s", ts)
}

// flattenIssue promotes fields inside the "fields" key to top-level keys (e.g. fields.summary)
// so each field becomes its own column instead of one large JSON blob.
func flattenIssue(issue map[string]any) map[string]any {
	flat := make(map[string]any)

	for k, v := range issue {
		if k == "fields" {
			if fields, ok := v.(map[string]any); ok {
				for fk, fv := range fields {
					flat["fields_"+fk] = fv
				}
			}
			continue
		}
		flat[k] = v
	}

	return flat
}

// readSimpleList fetches a non-paginated endpoint that returns a flat JSON array.
// Used by: issue_types, statuses, priorities, resolutions, events.
func (s *JiraSource) readSimpleList(ctx context.Context, endpoint, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JIRA] reading %s", label)

	resp, err := s.client.R(ctx).Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", label, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("jira %s returned status %d: %s", label, resp.StatusCode(), resp.String())
	}

	var items []map[string]any
	if err := jsonUseNumber(resp.Body(), &items); err != nil {
		return fmt.Errorf("failed to parse %s response: %w", label, err)
	}

	if len(items) == 0 {
		return nil
	}

	if err := sendBatch(ctx, items, label, opts, results); err != nil {
		return err
	}

	config.Debug("[JIRA] finished reading %s: %d total records", label, len(items))
	return nil
}

// readPaginatedArray fetches an endpoint that returns a flat JSON array with startAt/maxResults offset pagination.
// Used by: projects, users.
func (s *JiraSource) readPaginatedArray(ctx context.Context, endpoint, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JIRA] reading %s", label)

	startAt := 0
	totalProcessed := 0

	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("startAt", strconv.Itoa(startAt)).
			SetQueryParam("maxResults", strconv.Itoa(maxPageSize)).
			Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("jira %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		var items []map[string]any
		if err := jsonUseNumber(resp.Body(), &items); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		if len(items) == 0 {
			break
		}

		if err := sendBatch(ctx, items, label, opts, results); err != nil {
			return err
		}

		totalProcessed += len(items)

		if len(items) < maxPageSize {
			break
		}

		startAt += len(items)
	}

	config.Debug("[JIRA] finished reading %s: %d total records", label, totalProcessed)
	return nil
}

// readProjectSearch fetches projects via /project/search with status filtering.
// Used by projects:skip_archived (statuses=["live"]).
func (s *JiraSource) readProjectSearch(ctx context.Context, statuses []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JIRA] reading projects via search (statuses=%v)", statuses)

	projects, err := s.paginateProjectSearch(ctx, statuses)
	if err != nil {
		return err
	}

	if err := sendBatch(ctx, projects, "projects", opts, results); err != nil {
		return err
	}

	config.Debug("[JIRA] finished reading projects: %d total records", len(projects))
	return nil
}

// paginateProjectSearch paginates /project/search with the given status filters.
// Shared by readProjectSearch (for the projects table) and getProjectKeys (for per-project tables).
func (s *JiraSource) paginateProjectSearch(ctx context.Context, statuses []string) ([]map[string]any, error) {
	var all []map[string]any
	startAt := 0

	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		params := url.Values{
			"startAt":    {strconv.Itoa(startAt)},
			"maxResults": {strconv.Itoa(maxPageSize)},
			"status":     statuses,
		}

		resp, err := s.client.R(ctx).
			SetQueryParamValues(params).
			Get("/project/search")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch projects: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("jira projects search returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var result map[string]any
		if err := jsonUseNumber(resp.Body(), &result); err != nil {
			return nil, fmt.Errorf("failed to parse projects search response: %w", err)
		}

		rawValues, ok := result["values"].([]any)
		if !ok || len(rawValues) == 0 {
			break
		}

		for _, item := range rawValues {
			if row, ok := item.(map[string]any); ok {
				all = append(all, row)
			}
		}

		isLast, _ := result["isLast"].(bool)
		if isLast || len(rawValues) < maxPageSize {
			break
		}
		startAt += len(rawValues)
	}

	return all, nil
}

// readIssues fetches issues via the search/jql endpoint with nextPageToken pagination.
// Server-side incremental filtering via JQL: updated >= 'date'.
func (s *JiraSource) readIssues(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JIRA] reading issues")

	// Jira search/jql requires bounded queries. Default to 2010-01-01 like ingestr.
	dateStr := "2010-01-01 00:00"
	if opts.IntervalStart != nil {
		dateStr = opts.IntervalStart.Format("2006-01-02 15:04")
	}
	jql := fmt.Sprintf("updated >= '%s' ORDER BY updated ASC", dateStr)

	nextPageToken := ""
	totalProcessed := 0

	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("jql", jql).
			SetQueryParam("maxResults", strconv.Itoa(searchMaxResults)).
			SetQueryParam("fields", "*all")

		if nextPageToken != "" {
			req.SetQueryParam("nextPageToken", nextPageToken)
		}

		resp, err := req.Get("/search/jql")
		if err != nil {
			return fmt.Errorf("failed to search issues: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("jira search issues returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var result map[string]any
		if err := jsonUseNumber(resp.Body(), &result); err != nil {
			return fmt.Errorf("failed to parse search issues response: %w", err)
		}

		rawIssues, ok := result["issues"].([]any)
		if !ok || len(rawIssues) == 0 {
			break
		}

		rows := make([]map[string]any, 0, len(rawIssues))
		done := false
		for _, item := range rawIssues {
			issue, ok := item.(map[string]any)
			if !ok {
				continue
			}

			flat := flattenIssue(issue)

			if opts.IntervalEnd != nil {
				if ts, ok := flat["fields_updated"].(string); ok {
					if t, err := parseJiraTimestamp(ts); err == nil {
						if t.After(*opts.IntervalEnd) {
							done = true
							continue
						}
					}
				}
			}

			rows = append(rows, flat)
		}

		if err := sendBatch(ctx, rows, "issues", opts, results); err != nil {
			return err
		}
		totalProcessed += len(rows)

		if done {
			break
		}

		npt, _ := result["nextPageToken"].(string)
		if npt == "" {
			break
		}
		nextPageToken = npt
	}

	config.Debug("[JIRA] finished reading issues: %d total records", totalProcessed)
	return nil
}

// getProjectKeys fetches all project keys for transformer-style tables.
func (s *JiraSource) getProjectKeys(ctx context.Context, skipArchived bool) ([]string, error) {
	if skipArchived {
		projects, err := s.paginateProjectSearch(ctx, []string{"live"})
		if err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(projects))
		for _, p := range projects {
			if key, ok := p["key"].(string); ok {
				keys = append(keys, key)
			}
		}
		return keys, nil
	}
	return s.getProjectKeysFromList(ctx)
}

func (s *JiraSource) getProjectKeysFromList(ctx context.Context) ([]string, error) {
	var keys []string
	startAt := 0

	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("startAt", strconv.Itoa(startAt)).
			SetQueryParam("maxResults", strconv.Itoa(maxPageSize)).
			Get("/project")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch projects for keys: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("jira projects returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var items []map[string]any
		if err := jsonUseNumber(resp.Body(), &items); err != nil {
			return nil, fmt.Errorf("failed to parse projects response: %w", err)
		}

		if len(items) == 0 {
			break
		}

		for _, item := range items {
			if key, ok := item["key"].(string); ok {
				keys = append(keys, key)
			}
		}

		if len(items) < maxPageSize {
			break
		}
		startAt += len(items)
	}

	return keys, nil
}

// readPerProject fetches a per-project paginated endpoint that returns { values: [...], isLast: bool }.
// Used by: project_versions, project_components. Projects are fetched in parallel.
func (s *JiraSource) readPerProject(ctx context.Context, endpointPattern, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult, skipArchived bool) error {
	config.Debug("[JIRA] reading %s", label)

	projectKeys, err := s.getProjectKeys(ctx, skipArchived)
	if err != nil {
		return err
	}

	err = runParallel(ctx, projectKeys, func(ctx context.Context, projectKey string) error {
		endpoint := fmt.Sprintf(endpointPattern, projectKey)
		return s.paginateValuesFunc(ctx, endpoint, label, func(rows []map[string]any) error {
			return sendBatch(ctx, rows, label, opts, results)
		})
	})
	if err != nil {
		return err
	}

	config.Debug("[JIRA] finished reading %s", label)
	return nil
}

// readIssueChangelogs fetches changelogs for all issues using the bulk endpoint.
// Steps: 1) Get all projects, 2) Fetch issue keys per project in parallel,
// 3) POST to /changelog/bulkfetch in parallel batches of 1000 issue keys.
func (s *JiraSource) readIssueChangelogs(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JIRA] reading issue_changelogs")

	projectKeys, err := s.getProjectKeys(ctx, false)
	if err != nil {
		return err
	}

	// Step 1: fetch issue keys per project in parallel.
	type keyResult struct {
		keys []string
		err  error
	}
	keyCh := make(chan keyResult, len(projectKeys))
	var keyWg sync.WaitGroup
	keySem := make(chan struct{}, parallelism)

	for _, pk := range projectKeys {
		keySem <- struct{}{}
		keyWg.Add(1)
		go func(projectKey string) {
			defer keyWg.Done()
			defer func() { <-keySem }()

			keys, err := s.getIssueKeysForProject(ctx, projectKey)
			keyCh <- keyResult{keys: keys, err: err}
		}(pk)
	}
	keyWg.Wait()
	close(keyCh)

	var allIssueKeys []string
	for kr := range keyCh {
		if kr.err != nil {
			return kr.err
		}
		allIssueKeys = append(allIssueKeys, kr.keys...)
	}

	if len(allIssueKeys) == 0 {
		config.Debug("[JIRA] no issues found for changelogs")
		return nil
	}

	config.Debug("[JIRA] fetching changelogs for %d issues", len(allIssueKeys))

	// Step 2: bulk-fetch changelogs in parallel batches.
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	sem := make(chan struct{}, parallelism)

	for i := 0; i < len(allIssueKeys); i += changelogBatchSize {
		if workerCtx.Err() != nil {
			break
		}

		end := i + changelogBatchSize
		if end > len(allIssueKeys) {
			end = len(allIssueKeys)
		}
		batch := allIssueKeys[i:end]

		sem <- struct{}{}
		wg.Add(1)
		go func(keys []string) {
			defer wg.Done()
			defer func() { <-sem }()

			if _, err := s.fetchChangelogBulk(workerCtx, keys, opts, results); err != nil {
				errOnce.Do(func() { firstErr = err })
				cancel()
			}
		}(batch)
	}

	wg.Wait()

	if firstErr != nil {
		return firstErr
	}

	config.Debug("[JIRA] finished reading issue_changelogs")
	return nil
}

func (s *JiraSource) getIssueKeysForProject(ctx context.Context, projectKey string) ([]string, error) {
	var keys []string
	nextPageToken := ""

	for range maxPages {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		jql := fmt.Sprintf("project = %s ORDER BY updated DESC", projectKey)
		req := s.client.R(ctx).
			SetQueryParam("jql", jql).
			SetQueryParam("fields", "key").
			SetQueryParam("maxResults", strconv.Itoa(searchMaxResults))

		if nextPageToken != "" {
			req.SetQueryParam("nextPageToken", nextPageToken)
		}

		resp, err := req.Get("/search/jql")
		if err != nil {
			return nil, fmt.Errorf("failed to search issues for project %s: %w", projectKey, err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("jira search for project %s returned status %d: %s", projectKey, resp.StatusCode(), resp.String())
		}

		var result map[string]any
		if err := jsonUseNumber(resp.Body(), &result); err != nil {
			return nil, fmt.Errorf("failed to parse search response for project %s: %w", projectKey, err)
		}

		rawIssues, ok := result["issues"].([]any)
		if !ok || len(rawIssues) == 0 {
			break
		}

		for _, item := range rawIssues {
			if issue, ok := item.(map[string]any); ok {
				if key, ok := issue["key"].(string); ok {
					keys = append(keys, key)
				}
			}
		}

		npt, _ := result["nextPageToken"].(string)
		if npt == "" {
			break
		}
		nextPageToken = npt
	}

	return keys, nil
}

// fetchChangelogBulk uses POST /changelog/bulkfetch to fetch changelogs for a batch of issue keys.
// This matches ingestr's approach and is much more efficient than per-issue GET requests.
func (s *JiraSource) fetchChangelogBulk(ctx context.Context, issueKeys []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) (int, error) {
	totalProcessed := 0
	nextPageToken := ""

	for range maxPages {
		select {
		case <-ctx.Done():
			return totalProcessed, ctx.Err()
		default:
		}

		body := map[string]any{
			"issueIdsOrKeys": issueKeys,
			"maxResults":     changelogBatchSize,
		}
		if nextPageToken != "" {
			body["nextPageToken"] = nextPageToken
		}

		resp, err := s.client.R(ctx).
			SetHeader("Content-Type", "application/json").
			SetBody(body).
			Post("/changelog/bulkfetch")
		if err != nil {
			return totalProcessed, fmt.Errorf("failed to bulk fetch changelogs: %w", err)
		}
		if !resp.IsSuccess() {
			return totalProcessed, fmt.Errorf("jira changelog bulkfetch returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var result map[string]any
		if err := jsonUseNumber(resp.Body(), &result); err != nil {
			return totalProcessed, fmt.Errorf("failed to parse changelog bulkfetch response: %w", err)
		}

		issueChangeLogs, ok := result["issueChangeLogs"].([]any)
		if !ok || len(issueChangeLogs) == 0 {
			break
		}

		var rows []map[string]any
		for _, icl := range issueChangeLogs {
			issueChangeLog, ok := icl.(map[string]any)
			if !ok {
				continue
			}

			issueID, _ := issueChangeLog["issueId"].(string)

			changeHistories, ok := issueChangeLog["changeHistories"].([]any)
			if !ok {
				continue
			}

			for _, ch := range changeHistories {
				history, ok := ch.(map[string]any)
				if !ok {
					continue
				}

				history["issue_id"] = issueID

				// Convert created from epoch milliseconds to time.Time for proper timestamp handling.
				if created, ok := history["created"].(json.Number); ok {
					if ms, err := created.Int64(); err == nil {
						history["created"] = time.Unix(0, ms*int64(time.Millisecond)).UTC()
					}
				}

				rows = append(rows, history)
			}
		}

		if err := sendBatch(ctx, rows, "issue_changelogs", opts, results); err != nil {
			return totalProcessed, err
		}
		totalProcessed += len(rows)

		npt, _ := result["nextPageToken"].(string)
		if npt == "" {
			break
		}
		nextPageToken = npt
	}

	return totalProcessed, nil
}

// getCustomFieldIDs fetches all custom field IDs from /field.
func (s *JiraSource) getCustomFieldIDs(ctx context.Context) ([]string, error) {
	resp, err := s.client.R(ctx).Get("/field")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch fields: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("jira fields returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var fields []map[string]any
	if err := jsonUseNumber(resp.Body(), &fields); err != nil {
		return nil, fmt.Errorf("failed to parse fields response: %w", err)
	}

	var ids []string
	for _, f := range fields {
		if custom, ok := f["custom"].(bool); ok && custom {
			if id, ok := f["id"].(string); ok {
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}

// readCustomFieldContexts fetches contexts for all custom fields in parallel.
// GET /field/{fieldId}/context — paginated with values/isLast.
func (s *JiraSource) readCustomFieldContexts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JIRA] reading issue_custom_field_contexts")

	fieldIDs, err := s.getCustomFieldIDs(ctx)
	if err != nil {
		return err
	}

	err = runParallel(ctx, fieldIDs, func(ctx context.Context, fieldID string) error {
		endpoint := fmt.Sprintf("/field/%s/context", fieldID)
		return s.paginateValuesFunc(ctx, endpoint, fmt.Sprintf("contexts for %s", fieldID), func(rows []map[string]any) error {
			for _, row := range rows {
				row["fieldId"] = fieldID
			}
			return sendBatch(ctx, rows, "issue_custom_field_contexts", opts, results)
		}, 404)
	})
	if err != nil {
		return err
	}

	config.Debug("[JIRA] finished reading issue_custom_field_contexts")
	return nil
}

// readCustomFieldOptions fetches options for all custom field contexts in parallel.
// First fetches custom fields, then contexts, then options per context.
// GET /field/{fieldId}/context/{contextId}/option — paginated with values/isLast.
func (s *JiraSource) readCustomFieldOptions(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JIRA] reading issue_custom_field_options")

	fieldIDs, err := s.getCustomFieldIDs(ctx)
	if err != nil {
		return err
	}

	type fieldContext struct {
		fieldID   string
		contextID string
	}

	var mu sync.Mutex
	var pairs []fieldContext

	err = runParallel(ctx, fieldIDs, func(ctx context.Context, fid string) error {
		endpoint := fmt.Sprintf("/field/%s/context", fid)
		return s.paginateValuesFunc(ctx, endpoint, fmt.Sprintf("contexts for %s", fid), func(rows []map[string]any) error {
			local := make([]fieldContext, 0, len(rows))
			for _, c := range rows {
				if id, ok := c["id"].(string); ok {
					local = append(local, fieldContext{fieldID: fid, contextID: id})
				}
			}
			mu.Lock()
			pairs = append(pairs, local...)
			mu.Unlock()
			return nil
		}, 404)
	})
	if err != nil {
		return err
	}

	config.Debug("[JIRA] fetching options for %d field/context pairs", len(pairs))

	err = runParallel(ctx, pairs, func(ctx context.Context, fc fieldContext) error {
		endpoint := fmt.Sprintf("/field/%s/context/%s/option", fc.fieldID, fc.contextID)
		return s.paginateValuesFunc(ctx, endpoint, fmt.Sprintf("options for %s/%s", fc.fieldID, fc.contextID), func(rows []map[string]any) error {
			for _, row := range rows {
				row["fieldId"] = fc.fieldID
				row["contextId"] = fc.contextID
			}
			return sendBatch(ctx, rows, "issue_custom_field_options", opts, results)
		}, 400, 404)
	})
	if err != nil {
		return err
	}

	config.Debug("[JIRA] finished reading issue_custom_field_options")
	return nil
}
