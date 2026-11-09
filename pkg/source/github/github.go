package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

// Variables rather than constants so tests can point the source at a local server.
var (
	restApiBaseUrl = "https://api.github.com"
	graphqlBaseUrl = "https://api.github.com/graphql"
)

const (
	maxPageSize      = 100 // GitHub GraphQL allows up to 100
	defaultBatchSize = 50
	rateLimit        = 10
	rateLimitBurst   = 5
)

var supportedTables = []string{
	"commits",
	"issues",
	"pull_requests",
	"repo_events",
	"stargazers",
}

var issueFields = []schema.Column{
	{Name: "number", DataType: schema.TypeInt64, Nullable: false},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "body", DataType: schema.TypeString, Nullable: true},
	{Name: "author", DataType: schema.TypeJSON, Nullable: true},
	{Name: "authorAssociation", DataType: schema.TypeString, Nullable: true},
	{Name: "closed", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "closedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "state", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "reactionsTotalCount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "reactions", DataType: schema.TypeJSON, Nullable: true},
	{Name: "commentsTotalCount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "comments", DataType: schema.TypeJSON, Nullable: true},
}

var pullRequestFields = []schema.Column{
	{Name: "number", DataType: schema.TypeInt64, Nullable: false},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "body", DataType: schema.TypeString, Nullable: true},
	{Name: "author", DataType: schema.TypeJSON, Nullable: true},
	{Name: "authorAssociation", DataType: schema.TypeString, Nullable: true},
	{Name: "closed", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "closedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "createdAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "state", DataType: schema.TypeString, Nullable: true},
	{Name: "updatedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "merged", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "mergedAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "reactionsTotalCount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "reactions", DataType: schema.TypeJSON, Nullable: true},
	{Name: "commentsTotalCount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "comments", DataType: schema.TypeJSON, Nullable: true},
}

var repoEventFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "type", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "actor", DataType: schema.TypeJSON, Nullable: true},
	{Name: "repo", DataType: schema.TypeJSON, Nullable: true},
	{Name: "payload", DataType: schema.TypeJSON, Nullable: true},
	{Name: "public", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "org", DataType: schema.TypeJSON, Nullable: true},
}

var commitFields = []schema.Column{
	{Name: "sha", DataType: schema.TypeString, Nullable: false},
	{Name: "committed_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "node_id", DataType: schema.TypeString, Nullable: true},
	{Name: "commit", DataType: schema.TypeJSON, Nullable: true},
	{Name: "url", DataType: schema.TypeString, Nullable: true},
	{Name: "html_url", DataType: schema.TypeString, Nullable: true},
	{Name: "comments_url", DataType: schema.TypeString, Nullable: true},
	{Name: "author", DataType: schema.TypeJSON, Nullable: true},
	{Name: "committer", DataType: schema.TypeJSON, Nullable: true},
	{Name: "parents", DataType: schema.TypeJSON, Nullable: true},
}

var stargazerFields = []schema.Column{
	{Name: "starredAt", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "node", DataType: schema.TypeJSON, Nullable: true},
}

type GithubSource struct {
	owner         string
	repo          string
	apiKey        string
	restClient    *ingestrhttp.Client
	graphqlClient *ingestrhttp.Client
}

func NewGitHubSource() *GithubSource {
	return &GithubSource{}
}

func (s *GithubSource) HandlesIncrementality() bool {
	return true
}

func (s *GithubSource) Schemes() []string {
	return []string{"github"}
}

func (s *GithubSource) Connect(ctx context.Context, uri string) error {
	owner, repo, apiKey, err := parseGitHubURI(uri)
	if err != nil {
		return err
	}

	s.owner = owner
	s.repo = repo
	s.apiKey = apiKey

	// Build client options
	restOpts := []ingestrhttp.Option{
		ingestrhttp.WithBaseURL(restApiBaseUrl),
		ingestrhttp.WithTimeout(60 * time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
	}
	restOpts = append(restOpts, retryOptions(owner, repo)...)

	graphqlOpts := []ingestrhttp.Option{
		ingestrhttp.WithBaseURL(graphqlBaseUrl),
		ingestrhttp.WithTimeout(60 * time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
		// GraphQL queries are POSTs but read-only; resty skips retries for POST otherwise.
		ingestrhttp.WithAllowNonIdempotentRetry(),
	}
	graphqlOpts = append(graphqlOpts, retryOptions(owner, repo)...)

	// Add auth if access_token is provided (optional - unauthenticated has 60 req/hour limit)
	if s.apiKey != "" {
		restOpts = append(restOpts, ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth(s.apiKey)))
		graphqlOpts = append(graphqlOpts, ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth(s.apiKey)))
		config.Debug("[GITHUB] Connected to repo: %s/%s (authenticated)", s.owner, s.repo)
	} else {
		config.Debug("[GITHUB] Connected to repo: %s/%s (unauthenticated - 60 requests/hour limit)", s.owner, s.repo)
	}

	s.restClient = ingestrhttp.New(restOpts...)
	s.graphqlClient = ingestrhttp.New(graphqlOpts...)

	return nil
}

func parseGitHubURI(uri string) (owner, repo, apiKey string, err error) {
	if !strings.HasPrefix(uri, "github://") {
		return "", "", "", fmt.Errorf("invalid github URI: must start with github://")
	}

	rest := strings.TrimPrefix(uri, "github://")
	parts := strings.SplitN(rest, "?", 2)

	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("github URI must include query parameters (github://?access_token=...&owner=...&repo=...)")
	}

	values, err := url.ParseQuery(parts[1])
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse github URI query: %w", err)
	}

	// Get owner and repo from query parameters
	owner = values.Get("owner")
	repo = values.Get("repo")

	if owner == "" || repo == "" {
		return "", "", "", fmt.Errorf("owner and repo parameters are required in URI (github://?access_token=...&owner=...&repo=...)")
	}

	// Accept both api_key and access_token for flexibility (optional)
	apiKey = values.Get("api_key")
	if apiKey == "" {
		apiKey = values.Get("access_token")
	}
	// access_token is optional - unauthenticated requests have lower rate limits (60/hour vs 5000/hour)

	return owner, repo, apiKey, nil
}

func (s *GithubSource) Close(ctx context.Context) error {
	var errs []error
	if s.restClient != nil {
		if err := s.restClient.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.graphqlClient != nil {
		if err := s.graphqlClient.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (s *GithubSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	tableSchema, err := s.getSchema(ctx, tableName)
	if err != nil {
		return nil, err
	}

	// Default strategy is replace; repo_events and commits use merge with incremental loading
	incrementalKey := ""
	strategy := config.StrategyReplace

	switch tableName {
	case "repo_events":
		incrementalKey = "created_at"
		strategy = config.StrategyMerge
	case "commits":
		incrementalKey = "committed_at"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    tableSchema.PrimaryKeys, // Use primary keys from schema
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *GithubSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	var fields []schema.Column
	var primaryKeys []string

	switch table {
	case "commits":
		fields = commitFields
		primaryKeys = []string{"sha"}
	case "issues":
		fields = issueFields
		primaryKeys = nil
	case "pull_requests":
		fields = pullRequestFields
		primaryKeys = nil
	case "repo_events":
		fields = repoEventFields
		primaryKeys = []string{"id"}
	case "stargazers":
		fields = stargazerFields
		primaryKeys = nil
	default:
		return nil, fmt.Errorf("unsupported table: %s", table)
	}

	return &schema.TableSchema{
		Name:        table,
		Columns:     fields,
		PrimaryKeys: primaryKeys,
	}, nil
}

func (s *GithubSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if !isValidTable(table) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", table, strings.Join(supportedTables, ", "))
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "commits":
			err = s.readCommits(ctx, opts, results)
		case "issues":
			err = s.readIssues(ctx, opts, results)
		case "pull_requests":
			err = s.readPullRequests(ctx, opts, results)
		case "repo_events":
			err = s.readRepoEvents(ctx, opts, results)
		case "stargazers":
			err = s.readStargazers(ctx, opts, results)
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

// GraphQL types
type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data      json.RawMessage `json:"data"`
	Errors    []graphQLError  `json:"errors,omitempty"`
	RateLimit *rateLimitInfo  `json:"rateLimit,omitempty"`
}

type graphQLError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type rateLimitInfo struct {
	Limit     int    `json:"limit"`
	Cost      int    `json:"cost"`
	Remaining int    `json:"remaining"`
	ResetAt   string `json:"resetAt"`
}

func (s *GithubSource) executeGraphQL(ctx context.Context, query string, variables map[string]interface{}) (json.RawMessage, *rateLimitInfo, error) {
	reqBody := graphQLRequest{
		Query:     query,
		Variables: variables,
	}

	config.Debug("[GITHUB] Executing GraphQL query")

	httpResp, err := s.graphqlClient.R(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		Post("")
	if err != nil {
		return nil, nil, fmt.Errorf("graphql request failed: %w", err)
	}

	if !httpResp.IsSuccess() {
		return nil, nil, fmt.Errorf("graphql request failed with status %d: %s", httpResp.StatusCode(), httpResp.String())
	}

	// Decoded here rather than via SetResult: the rate-limit retry condition reads the body,
	// and a result reused across retries would keep a rate-limited attempt's errors.
	var resp graphQLResponse
	if err := httpResp.JSON(&resp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode graphql response: %w", err)
	}

	if len(resp.Errors) > 0 {
		var errMsgs []string
		for _, e := range resp.Errors {
			errMsgs = append(errMsgs, e.Message)
		}
		return nil, nil, fmt.Errorf("graphql errors: %s", strings.Join(errMsgs, "; "))
	}

	// Extract rateLimit from data (GitHub returns it in the data field)
	var rateLimit *rateLimitInfo
	var dataMap map[string]interface{}
	if err := json.Unmarshal(resp.Data, &dataMap); err == nil {
		if rlData, ok := dataMap["rateLimit"].(map[string]interface{}); ok {
			rateLimit = &rateLimitInfo{}
			if limit, ok := rlData["limit"].(float64); ok {
				rateLimit.Limit = int(limit)
			}
			if cost, ok := rlData["cost"].(float64); ok {
				rateLimit.Cost = int(cost)
			}
			if remaining, ok := rlData["remaining"].(float64); ok {
				rateLimit.Remaining = int(remaining)
			}
			if resetAt, ok := rlData["resetAt"].(string); ok {
				rateLimit.ResetAt = resetAt
			}
		}
	}

	// Log rate limit info
	if rateLimit != nil {
		config.Debug("[GITHUB] Rate limit: %d/%d remaining (cost: %d, resets: %s)",
			rateLimit.Remaining, rateLimit.Limit, rateLimit.Cost, rateLimit.ResetAt)
	}

	return resp.Data, rateLimit, nil
}

// GraphQL Queries
const issuesQuery = `
query($owner: String!, $name: String!, $issues_per_page: Int!, $first_reactions: Int!, $first_comments: Int!, $page_after: String) {
  repository(owner: $owner, name: $name) {
    issues(first: $issues_per_page, orderBy: {field: CREATED_AT, direction: DESC}, after: $page_after) {
      totalCount
      pageInfo {
        endCursor
        startCursor
      }
      nodes {
        number
        url
        title
        body
        author {login avatarUrl url}
        authorAssociation
        closed
        closedAt
        createdAt
        state
        updatedAt
        reactions(first: $first_reactions) {
          totalCount
          nodes {
            user {login avatarUrl url}
            content
            createdAt
          }
        }
        comments(first: $first_comments) {
          totalCount
          nodes {
            id
            url
            body
            author {avatarUrl login url}
            authorAssociation
            createdAt
            reactionGroups {content createdAt}
			# reactions(first: 0) {
            #   totalCount
            #   nodes {
            #     # id
            #     user {login avatarUrl url}
            #     content
            #     createdAt
            #   }
            # }
          }
        }
      }
    }
  }
  rateLimit {
    limit
    cost
    remaining
    resetAt
  }
}`

const pullRequestsQuery = `
query($owner: String!, $name: String!, $issues_per_page: Int!, $first_reactions: Int!, $first_comments: Int!, $page_after: String) {
  repository(owner: $owner, name: $name) {
    pullRequests(first: $issues_per_page, orderBy: {field: CREATED_AT, direction: DESC}, after: $page_after) {
      totalCount
      pageInfo {
        endCursor
        startCursor
      }
      nodes {
        number
        url
        title
        body
        author {login avatarUrl url}
        authorAssociation
        closed
        closedAt
        createdAt
        state
        updatedAt
        merged
        mergedAt
        reactions(first: $first_reactions) {
          totalCount
          nodes {
            user {login avatarUrl url}
            content
            createdAt
          }
        }
        comments(first: $first_comments) {
          totalCount
          nodes {
            id
            url
            body
            author {avatarUrl login url}
            authorAssociation
            createdAt
            reactionGroups {content createdAt}
			# reactions(first: 0) {
            #   totalCount
            #   nodes {
            #     # id
            #     user {login avatarUrl url}
            #     content
            #     createdAt
            #   }
            # }
          }
        }
      }
    }
  }
  rateLimit {
    limit
    cost
    remaining
    resetAt
  }
}`

const stargazersQuery = `
query($owner: String!, $name: String!, $items_per_page: Int!, $page_after: String) {
  repository(owner: $owner, name: $name) {
    stargazers(first: $items_per_page, orderBy: {field: STARRED_AT, direction: DESC}, after: $page_after) {
      pageInfo {
        endCursor
        startCursor
      }
      edges {
        starredAt
        node {
          login
          avatarUrl
          url
        }
      }
    }
  }
  rateLimit {
    limit
    cost
    remaining
    resetAt
  }
}`

const commentReactionsQuery = `
node_%s: node(id:"%s") {
     ... on IssueComment {
      id
      reactions(first: 100) {
        totalCount
        nodes {
            user {login avatarUrl url}
            content
            createdAt
          }
      }
    }
  }
}`

func (s *GithubSource) readIssues(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[GITHUB] Reading issues for %s/%s", s.owner, s.repo)

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatchSize
	}

	variables := map[string]interface{}{
		"issues_per_page": pageSize,
		"first_reactions": 100,
		"first_comments":  100,
	}

	return s.paginateGraphQL(ctx, opts, results, issuesQuery, "issues", "nodes", issueFields, variables, s.transformIssue)
}

func normalizeDictionaries(item map[string]interface{}) map[string]interface{} {
	normalized := make(map[string]interface{})
	for key, value := range item {
		if valueMap, ok := value.(map[string]interface{}); ok {
			if totalCount, hasTotalCount := valueMap["totalCount"]; hasTotalCount {
				normalized[key+"TotalCount"] = totalCount
				if nodes, hasNodes := valueMap["nodes"]; hasNodes {
					normalized[key] = nodes
				}
			} else {
				normalized[key] = value
			}
		} else {
			normalized[key] = value
		}
	}
	return normalized
}

func (s *GithubSource) transformIssue(node map[string]interface{}) map[string]interface{} {
	return normalizeDictionaries(node)
}

func (s *GithubSource) readPullRequests(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[GITHUB] Reading pull requests for %s/%s", s.owner, s.repo)

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatchSize
	}

	variables := map[string]interface{}{
		"issues_per_page": pageSize,
		"first_reactions": 100,
		"first_comments":  100,
	}

	return s.paginateGraphQL(ctx, opts, results, pullRequestsQuery, "pullRequests", "nodes", pullRequestFields, variables, s.transformPullRequest)
}

func (s *GithubSource) transformPullRequest(node map[string]interface{}) map[string]interface{} {
	return normalizeDictionaries(node)
}

func (s *GithubSource) readStargazers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[GITHUB] Reading stargazers for %s/%s", s.owner, s.repo)

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatchSize
	}

	variables := map[string]interface{}{
		"items_per_page": pageSize,
	}

	return s.paginateGraphQL(ctx, opts, results, stargazersQuery, "stargazers", "edges", stargazerFields, variables, s.transformStargazer)
}

func (s *GithubSource) transformStargazer(edge map[string]interface{}) map[string]interface{} {
	return edge
}

func (s *GithubSource) readRepoEvents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[GITHUB] Reading repo events for %s/%s", s.owner, s.repo)

	// Get date filters for incremental loading
	startFilter := opts.IntervalStart
	endFilter := opts.IntervalEnd

	// GitHub Events API only returns max ~300 events (3 pages) and max 30 days
	eventsPath := fmt.Sprintf("/repos/%s/%s/events?per_page=100", url.PathEscape(s.owner), url.PathEscape(s.repo))
	nextURL := eventsPath
	totalSent := 0
	batchNum := 0
	var lastRemaining string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var events []map[string]interface{}
		httpResp, err := s.restClient.R(ctx).
			SetResult(&events).
			Get(nextURL)
		if err != nil {
			return fmt.Errorf("failed to fetch repo events: %w", err)
		}

		if !httpResp.IsSuccess() {
			return fmt.Errorf("failed to fetch repo events with status %d: %s", httpResp.StatusCode(), httpResp.String())
		}

		remaining := httpResp.Header().Get("x-ratelimit-remaining")
		if remaining != "" {
			lastRemaining = remaining
		}
		config.Debug("[GITHUB] Got %d events, requests remaining: %s", len(events), remaining)

		if len(events) == 0 {
			break
		}

		// Filter events by date range (incremental logic - client-side)
		var filteredEvents []map[string]interface{}
		var accBytes int64
		stopPagination := false

		flush := func() error {
			if len(filteredEvents) == 0 {
				return nil
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(filteredEvents, repoEventFields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert events to Arrow: %w", err)
			}

			batchNum++
			config.Debug("[GITHUB] Sending batch %d with %d events", batchNum, len(filteredEvents))
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(filteredEvents)

			// Update rate limit info below the spinner (on a separate line)
			if lastRemaining != "" {
				config.Debug("[GITHUB] repo_events: %d rows | requests: %d | remaining: %s",
					totalSent, batchNum, lastRemaining)
			}

			filteredEvents = nil
			accBytes = 0
			return nil
		}

		for _, event := range events {
			var createdAtStr string
			if v, ok := event["created_at"]; ok {
				if s, ok := v.(string); ok {
					createdAtStr = s
				}
			}
			if createdAtStr == "" {
				continue
			}

			eventDate, err := time.Parse(time.RFC3339, createdAtStr)
			if err != nil {
				continue
			}

			// Events are returned newest first
			// If we hit an event older than start filter, stop pagination
			if startFilter != nil && eventDate.Before(*startFilter) {
				stopPagination = true
				break
			}

			// Skip events newer than end filter
			if endFilter != nil && eventDate.After(*endFilter) {
				continue
			}

			transformed := s.transformRepoEvent(event)

			if opts.MaxBatchBytes > 0 {
				rowBytes := arrowconv.RowBytes(transformed)
				if len(filteredEvents) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
					if err := flush(); err != nil {
						return err
					}
				}
				accBytes += rowBytes
			}
			filteredEvents = append(filteredEvents, transformed)

			if opts.Limit > 0 && totalSent+len(filteredEvents) >= opts.Limit {
				stopPagination = true
				break
			}
		}

		if err := flush(); err != nil {
			return err
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			config.Debug("[GITHUB] Reached limit of %d records", opts.Limit)
			break
		}

		if stopPagination {
			config.Debug("[GITHUB] Reached events older than start filter, stopping pagination")
			break
		}

		// Check for next page using Link header
		linkHeader := httpResp.Header().Get("Link")
		nextURL = extractNextPageURL(linkHeader)
		if nextURL == "" {
			break
		}
	}

	config.Debug("[GITHUB] Finished reading repo events, total records: %d", totalSent)
	// No need to print at the end - last iteration already printed the final state
	return nil
}

// readCommits lists default-branch commits. GitHub applies since/until to the committer date.
func (s *GithubSource) readCommits(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[GITHUB] Reading commits for %s/%s", s.owner, s.repo)

	params := url.Values{}
	params.Set("per_page", strconv.Itoa(maxPageSize))
	if opts.IntervalStart != nil {
		params.Set("since", opts.IntervalStart.UTC().Format(time.RFC3339))
	}
	if opts.IntervalEnd != nil {
		params.Set("until", opts.IntervalEnd.UTC().Format(time.RFC3339))
	}

	nextURL := fmt.Sprintf("/repos/%s/%s/commits?%s", url.PathEscape(s.owner), url.PathEscape(s.repo), params.Encode())
	totalSent := 0

	for nextURL != "" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var commits []map[string]interface{}
		httpResp, err := s.restClient.R(ctx).
			SetResult(&commits).
			Get(nextURL)
		if err != nil {
			return fmt.Errorf("failed to fetch commits: %w", err)
		}

		if isEmptyRepository(httpResp) {
			config.Debug("[GITHUB] %s/%s has no commits yet", s.owner, s.repo)
			break
		}

		if !httpResp.IsSuccess() {
			return fmt.Errorf("failed to fetch commits with status %d: %s", httpResp.StatusCode(), httpResp.String())
		}
		config.Debug("[GITHUB] Got %d commits, requests remaining: %s", len(commits), httpResp.Header().Get("x-ratelimit-remaining"))

		if opts.Limit > 0 && totalSent+len(commits) > opts.Limit {
			commits = commits[:opts.Limit-totalSent]
		}

		var batch []map[string]interface{}
		var batchBytes int64
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, commitFields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert commits to Arrow: %w", err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(batch)
			batch = nil
			batchBytes = 0
			return nil
		}

		for _, c := range commits {
			c["committed_at"] = committerDate(c)
			if opts.MaxBatchBytes > 0 {
				rowBytes := arrowconv.RowBytes(c)
				if len(batch) > 0 && batchBytes+rowBytes > opts.MaxBatchBytes {
					if err := flush(); err != nil {
						return err
					}
				}
				batchBytes += rowBytes
			}
			batch = append(batch, c)
		}
		if err := flush(); err != nil {
			return err
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}
		nextURL = extractNextPageURL(httpResp.Header().Get("Link"))
	}

	config.Debug("[GITHUB] Finished reading commits, total records: %d", totalSent)
	return nil
}

func committerDate(c map[string]interface{}) interface{} {
	commit, _ := c["commit"].(map[string]interface{})
	committer, _ := commit["committer"].(map[string]interface{})
	return committer["date"]
}

// isEmptyRepository reports GitHub's answer for a repository with no commits yet:
// 409 "Git Repository is empty.". The other tables read such a repo as zero rows.
func isEmptyRepository(resp *ingestrhttp.Response) bool {
	return resp.StatusCode() == http.StatusConflict && strings.Contains(resp.String(), "Git Repository is empty")
}

// extractNextPageURL extracts the next page URL from GitHub's Link header
func extractNextPageURL(linkHeader string) string {
	if linkHeader == "" {
		return ""
	}

	links := strings.Split(linkHeader, ",")
	for _, link := range links {
		parts := strings.Split(strings.TrimSpace(link), ";")
		if len(parts) < 2 {
			continue
		}

		urlPart := strings.TrimSpace(parts[0])
		relPart := strings.TrimSpace(parts[1])

		if strings.Contains(relPart, `rel="next"`) {
			// Extract URL from <url>
			urlPart = strings.TrimPrefix(urlPart, "<")
			urlPart = strings.TrimSuffix(urlPart, ">")
			// Return relative path
			if strings.HasPrefix(urlPart, restApiBaseUrl) {
				return strings.TrimPrefix(urlPart, restApiBaseUrl)
			}
			return urlPart
		}
	}

	return ""
}

func (s *GithubSource) transformRepoEvent(event map[string]interface{}) map[string]interface{} {
	return event
}

// fetchCommentReactions fetches reactions for comments that have any reactions (using reactionGroups)
func (s *GithubSource) fetchCommentReactions(ctx context.Context, rawItems []interface{}) error {
	// Extract comment IDs that have reactions
	var commentIDsWithReactions []string
	commentNodeMap := make(map[string]map[string]interface{}) // commentID -> comment node

	for _, item := range rawItems {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// Get comments from the item
		commentsData, ok := itemMap["comments"].(map[string]interface{})
		if !ok {
			continue
		}

		commentsNodes, ok := commentsData["nodes"].([]interface{})
		if !ok {
			continue
		}

		for _, comment := range commentsNodes {
			commentNode, ok := comment.(map[string]interface{})
			if !ok {
				continue
			}

			commentID, ok := commentNode["id"].(string)
			if !ok || commentID == "" {
				continue
			}

			// Check reactionGroups to see if this comment has any reactions
			reactionGroups, ok := commentNode["reactionGroups"].([]interface{})
			if !ok {
				continue
			}

			hasReactions := false
			for _, rg := range reactionGroups {
				rgMap, ok := rg.(map[string]interface{})
				if !ok {
					continue
				}
				users, ok := rgMap["users"].(map[string]interface{})
				if !ok {
					continue
				}
				totalCount, ok := users["totalCount"].(float64)
				if ok && totalCount > 0 {
					hasReactions = true
					break
				}
			}

			if hasReactions {
				commentIDsWithReactions = append(commentIDsWithReactions, commentID)
				commentNodeMap[commentID] = commentNode
			}
		}
	}

	if len(commentIDsWithReactions) == 0 {
		return nil // No comments with reactions
	}

	config.Debug("[GITHUB] Fetching reactions for %d comments with reactions", len(commentIDsWithReactions))

	batchSize := 50
	numBatches := (len(commentIDsWithReactions) + batchSize - 1) / batchSize

	sem := make(chan struct{}, 3)
	errChan := make(chan error, numBatches)
	var wg sync.WaitGroup

	for i := 0; i < len(commentIDsWithReactions); i += batchSize {
		end := i + batchSize
		if end > len(commentIDsWithReactions) {
			end = len(commentIDsWithReactions)
		}
		batch := commentIDsWithReactions[i:end]

		wg.Add(1)
		go func(b []string) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			if err := s.fetchReactionsForComments(ctx, b, commentNodeMap); err != nil {
				errChan <- fmt.Errorf("failed to fetch reactions for comment batch: %w", err)
			}
		}(batch)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Check for errors
	if len(errChan) > 0 {
		var errs []string
		for err := range errChan {
			errs = append(errs, err.Error())
		}
		return fmt.Errorf("errors fetching comment reactions: %s", strings.Join(errs, "; "))
	}

	return nil
}

// fetchReactionsForComments fetches reactions for a batch of comments
func (s *GithubSource) fetchReactionsForComments(ctx context.Context, commentIDs []string, commentNodeMap map[string]map[string]interface{}) error {
	// Build a batch query for multiple comments
	var queryParts []string
	for i, commentID := range commentIDs {
		alias := fmt.Sprintf("node_%d", i)
		queryPart := fmt.Sprintf(commentReactionsQuery, alias, commentID)
		queryParts = append(queryParts, queryPart)
	}

	query := "query {\n" + strings.Join(queryParts, "\n") + "\n  rateLimit { limit cost remaining resetAt }\n}"

	data, rateLimit, err := s.executeGraphQL(ctx, query, nil)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("failed to parse reactions response: %w", err)
	}

	// Log query cost and remaining credits for comment reactions fetch
	if rateLimit != nil {
		config.Debug("[GITHUB] Fetched reactions for %d comments, query cost %d, remaining credits: %d",
			len(commentIDs), rateLimit.Cost, rateLimit.Remaining)
	}

	// Merge reactions back into comments
	for i, commentID := range commentIDs {
		alias := fmt.Sprintf("node_%d", i)
		nodeData, ok := result[alias].(map[string]interface{})
		if !ok {
			continue
		}

		commentNode, ok := commentNodeMap[commentID]
		if !ok {
			continue
		}

		// Extract reactions from the response
		reactionsData, ok := nodeData["reactions"].(map[string]interface{})
		if ok {
			commentNode["reactions"] = reactionsData
		}
	}

	return nil
}

// paginateGraphQL handles cursor-based pagination for GraphQL queries
func (s *GithubSource) paginateGraphQL(
	ctx context.Context,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	query, rootField, extractKey string,
	fields []schema.Column,
	variables map[string]interface{},
	transform func(map[string]interface{}) map[string]interface{},
) error {
	var cursor *string
	totalSent := 0
	batchNum := 0
	totalCost := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Copy base variables and add pagination
		vars := map[string]interface{}{
			"owner": s.owner,
			"name":  s.repo,
		}
		for k, v := range variables {
			vars[k] = v
		}
		if cursor != nil {
			vars["page_after"] = *cursor
		}

		data, rateLimit, err := s.executeGraphQL(ctx, query, vars)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", rootField, err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", rootField, err)
		}

		repoData, ok := result["repository"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("unexpected response format: missing repository")
		}

		connectionData, ok := repoData[rootField].(map[string]interface{})
		if !ok {
			return fmt.Errorf("unexpected response format for %s", rootField)
		}

		// Extract items from either "nodes" or "edges"
		rawItems, ok := connectionData[extractKey].([]interface{})
		if !ok || len(rawItems) == 0 {
			config.Debug("[GITHUB] No more %s to fetch", rootField)
			break
		}

		// For issues and pull_requests, fetch reactions for comments that have them
		if rootField == "issues" || rootField == "pullRequests" {
			if err := s.fetchCommentReactions(ctx, rawItems); err != nil {
				config.Debug("[GITHUB] Warning: failed to fetch comment reactions: %v", err)
				// Continue even if reaction fetching fails
			}
		}

		var items []map[string]interface{}
		var accBytes int64

		flush := func() error {
			if len(items) == 0 {
				return nil
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, fields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", rootField, err)
			}

			batchNum++
			config.Debug("[GITHUB] Sending batch %d with %d %s", batchNum, len(items), rootField)
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)

			// Update rate limit info below the spinner (on a separate line)
			if rateLimit != nil {
				totalCost += rateLimit.Cost
				config.Debug("[GITHUB] %s: %d rows | total cost: %d | remaining: %d",
					rootField, totalSent, totalCost, rateLimit.Remaining)
			}

			items = nil
			accBytes = 0
			return nil
		}

		for _, item := range rawItems {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			transformed := transform(itemMap)

			if opts.MaxBatchBytes > 0 {
				rowBytes := arrowconv.RowBytes(transformed)
				if len(items) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
					if err := flush(); err != nil {
						return err
					}
				}
				accBytes += rowBytes
			}
			items = append(items, transformed)

			if opts.Limit > 0 && totalSent+len(items) >= opts.Limit {
				break
			}
		}

		if err := flush(); err != nil {
			return err
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			config.Debug("[GITHUB] Reached limit of %d records", opts.Limit)
			break
		}

		pageInfoData, ok := connectionData["pageInfo"].(map[string]interface{})
		if !ok {
			break
		}

		endCursor, ok := pageInfoData["endCursor"].(string)
		if !ok || endCursor == "" {
			break
		}
		cursor = &endCursor
	}

	config.Debug("[GITHUB] Finished reading %s, total records: %d", rootField, totalSent)
	// No need to print at the end - last iteration already printed the final state
	return nil
}
