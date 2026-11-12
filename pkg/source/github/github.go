package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"resty.dev/v3"
)

// Variables rather than constants so tests can point the source at a local server and shorten the waits.
var (
	restApiBaseUrl = "https://api.github.com"
	graphqlBaseUrl = "https://api.github.com/graphql"

	retryWait              = 1 * time.Second
	secondaryRateLimitWait = 1 * time.Minute
)

const (
	maxPageSize      = 100 // GitHub GraphQL allows up to 100
	defaultBatchSize = 50
	rateLimit        = 10
	rateLimitBurst   = 5

	retryCount   = 5
	retryMaxWait = 1 * time.Hour // GitHub rate-limit windows reset hourly
	backoffMax   = 30 * time.Second

	secondaryRateLimitText = "secondary rate limit"
	retryAfterHeader       = "X-Ingestr-Retry-After"
	// Matches both GraphQL primary-limit types: RATE_LIMITED (query costs more than the points left)
	// and RATE_LIMIT (no points left).
	graphQLRateLimitType = "RATE_LIMIT"
)

type githubRateLimit int

const (
	notRateLimited githubRateLimit = iota
	primaryRateLimit
	secondaryRateLimit
)

var supportedTables = []string{
	"commits",
	"issues",
	"pull_requests",
	"repo_events",
	"repositories",
	"stargazers",
}

var graphQLTables = map[string]bool{
	"issues":        true,
	"pull_requests": true,
	"stargazers":    true,
}

// The fields GitHub's OpenAPI marks as required on every repository list response; the rest are inferred.
var repositoryFields = []schema.Column{
	{Name: "id", DataType: schema.TypeInt64, Nullable: false},
	{Name: "node_id", DataType: schema.TypeString, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "full_name", DataType: schema.TypeString, Nullable: true},
	{Name: "owner", DataType: schema.TypeJSON, Nullable: true},
	{Name: "private", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "fork", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "html_url", DataType: schema.TypeString, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
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
	{Name: "repo", DataType: schema.TypeString, Nullable: false},
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
		ingestrhttp.WithRetry(retryCount, retryWait, retryMaxWait),
		ingestrhttp.WithRetryCondition(githubRetryCondition),
		ingestrhttp.WithRetryStrategy(s.githubRetryStrategy),
		ingestrhttp.WithDebug(config.DebugMode),
	}

	graphqlOpts := []ingestrhttp.Option{
		ingestrhttp.WithBaseURL(graphqlBaseUrl),
		ingestrhttp.WithTimeout(60 * time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithRetry(retryCount, retryWait, retryMaxWait),
		// GraphQL queries are POSTs but read-only; resty skips retries for POST otherwise.
		ingestrhttp.WithAllowNonIdempotentRetry(),
		ingestrhttp.WithRetryCondition(githubRetryCondition),
		ingestrhttp.WithRetryStrategy(s.githubRetryStrategy),
		ingestrhttp.WithDebug(config.DebugMode),
	}

	// Add auth if access_token is provided (optional - unauthenticated has 60 req/hour limit)
	if s.apiKey != "" {
		restOpts = append(restOpts, ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth(s.apiKey)))
		graphqlOpts = append(graphqlOpts, ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth(s.apiKey)))
		config.Debug("[GITHUB] Connected to %s (authenticated)", s.target())
	} else {
		config.Debug("[GITHUB] Connected to %s (unauthenticated - 60 requests/hour limit)", s.target())
	}

	s.restClient = ingestrhttp.New(restOpts...)
	s.graphqlClient = ingestrhttp.New(graphqlOpts...)
	s.restClient.Resty().AddResponseMiddleware(moveRetryAfter)
	s.graphqlClient.Resty().AddResponseMiddleware(moveRetryAfter)

	return nil
}

// moveRetryAfter moves Retry-After to retryAfterHeader. resty waits out a 429/503 with Retry-After
// itself, without calling the retry strategy, so that wait would skip clampWait and the warning.
func moveRetryAfter(_ *resty.Client, resp *resty.Response) error {
	h := resp.Header()
	if v := h.Get("Retry-After"); v != "" {
		h.Del("Retry-After")
		h.Set(retryAfterHeader, v)
	}
	return nil
}

func githubRetryCondition(resp *ingestrhttp.Response, _ error) bool {
	return githubRateLimitKind(resp) != notRateLimited
}

// githubRetryStrategy waits as long as GitHub asks, with a warning, and backs off exponentially
// after other errors.
func (s *GithubSource) githubRetryStrategy(resp *ingestrhttp.Response, _ error) (time.Duration, error) {
	wait, kind, ok := githubRetryWait(resp, time.Now())
	if !ok {
		return githubBackoff(resp), nil
	}
	if kind == notRateLimited {
		output.Warnf("\nWarning: GitHub answered %d for %s and asked to retry later, waiting %v before retrying\n", resp.StatusCode(), s.target(), roundWait(wait))
	} else {
		output.Warnf("\nWarning: GitHub rate limit exceeded for %s, waiting %v before retrying\n", s.target(), roundWait(wait))
	}
	return wait, nil
}

// githubRateLimitKind tells GitHub's rate-limit answers apart. REST answers 403/429. GraphQL answers
// 200 with a RATE_LIMIT* error type for its primary limit, and 200 or 403 with a message for its
// secondary limit. A 429 is always a rate limit.
func githubRateLimitKind(resp *ingestrhttp.Response) githubRateLimit {
	if resp == nil {
		return notRateLimited
	}
	h := resp.Header()
	switch resp.StatusCode() {
	case http.StatusTooManyRequests:
		if h.Get("X-RateLimit-Remaining") == "0" {
			return primaryRateLimit
		}
		return secondaryRateLimit
	case http.StatusForbidden:
		if h.Get("X-RateLimit-Remaining") == "0" {
			return primaryRateLimit
		}
		if h.Get(retryAfterHeader) != "" || mentionsRateLimit(resp.String()) {
			return secondaryRateLimit
		}
	case http.StatusOK:
		return graphQLRateLimitKind(resp.Body())
	}
	return notRateLimited
}

// graphQLRateLimitKind reads only the errors of a GraphQL response. A secondary-limit message wins
// over a RATE_LIMIT* type, so the shorter wait is used when both appear.
func graphQLRateLimitKind(body []byte) githubRateLimit {
	if !bytes.Contains(body, []byte(`"errors"`)) {
		return notRateLimited
	}
	var parsed struct {
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return notRateLimited
	}
	kind := notRateLimited
	for _, e := range parsed.Errors {
		if mentionsSecondaryLimit(e.Message) {
			return secondaryRateLimit
		}
		if strings.Contains(e.Type, graphQLRateLimitType) {
			kind = primaryRateLimit
		}
	}
	return kind
}

func mentionsRateLimit(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, "rate limit") || strings.Contains(t, "abuse detection")
}

func mentionsSecondaryLimit(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, secondaryRateLimitText) || strings.Contains(t, "abuse detection")
}

// githubRetryWait returns how long GitHub asks us to wait: Retry-After, else until
// x-ratelimit-reset for a primary limit or a spent quota, else a wait that doubles per attempt
// from secondaryRateLimitWait. A GraphQL query that costs more than the points left keeps
// x-ratelimit-remaining above zero, so the reset is used for every primary limit; a reset that has
// already passed while the limit still holds gets the doubling wait instead. Times come from the
// response's Date header, so a local clock ahead of GitHub's doesn't end a wait early. Never below
// retryWait, since resty treats a zero delay as "wait the maximum".
func githubRetryWait(resp *ingestrhttp.Response, now time.Time) (time.Duration, githubRateLimit, bool) {
	if resp == nil {
		return 0, notRateLimited, false
	}
	kind := githubRateLimitKind(resp)
	h := resp.Header()
	now = serverTime(h, now)
	if wait, ok := retryAfter(h, now); ok {
		return clampWait(wait), kind, true
	}
	if kind == notRateLimited {
		return 0, kind, false
	}
	if kind == primaryRateLimit || h.Get("X-RateLimit-Remaining") == "0" {
		if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			if wait := time.Unix(reset, 0).Sub(now) + time.Second; wait > 0 {
				return clampWait(wait), kind, true
			}
		}
	}
	return clampWait(secondaryRateLimitWait << (retryAttempt(resp) - 1)), kind, true
}

// retryAfter parses the moved Retry-After header: seconds, capped at retryMaxWait before they are
// converted so a huge value can't overflow, or an HTTP date.
func retryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get(retryAfterHeader))
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Duration(min(max(secs, 0), int64(retryMaxWait/time.Second))) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return t.Sub(now), true
	}
	return 0, false
}

func serverTime(h http.Header, fallback time.Time) time.Time {
	if t, err := http.ParseTime(h.Get("Date")); err == nil {
		return t
	}
	return fallback
}

func clampWait(d time.Duration) time.Duration {
	return min(max(d, retryWait), retryMaxWait)
}

func roundWait(d time.Duration) time.Duration {
	if d < time.Second {
		return d
	}
	return d.Round(time.Second)
}

func retryAttempt(resp *ingestrhttp.Response) int {
	if resp != nil && resp.Attempt() > 0 {
		return min(resp.Attempt(), 16)
	}
	return 1
}

func githubBackoff(resp *ingestrhttp.Response) time.Duration {
	return min(retryWait<<(retryAttempt(resp)-1), backoffMax)
}

// target names the run in messages: owner/repo, just the owner when no repo is set, or the
// listed repositories when the URI has no owner.
func (s *GithubSource) target() string {
	switch {
	case s.owner == "":
		return "the listed repositories"
	case s.repo == "":
		return s.owner
	}
	return s.owner + "/" + s.repo
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

	if strings.Contains(owner, "/") {
		return "", "", "", fmt.Errorf("owner must be a user or organization login, got %q", owner)
	}
	if strings.Contains(repo, "/") {
		return "", "", "", fmt.Errorf("repo must be a bare repository name, got %q: set owner=<owner> and repo=<name>", repo)
	}
	if repo != "" && owner == "" {
		return "", "", "", fmt.Errorf("repo %q needs owner in the URI (github://?access_token=...&owner=<owner>&repo=%s)", repo, repo)
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
	tableName, sel, err := parseTableName(req.Name, s.owner)
	if err != nil {
		return nil, err
	}

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	switch tableName {
	case "commits":
		if sel == nil && s.owner == "" {
			return nil, fmt.Errorf("table commits needs owner in the URI, or a list of repositories: commits:<owner>/<repo>, or commits:<owner>/* for every repository of an owner")
		}
	case "repositories":
		if s.owner == "" {
			return nil, fmt.Errorf("table repositories needs owner in the URI")
		}
	default:
		if s.repo == "" {
			return nil, fmt.Errorf("table %s needs owner and repo in the URI (only commits can list repositories)", tableName)
		}
	}

	tableSchema, err := s.getSchema(ctx, tableName)
	if err != nil {
		return nil, err
	}
	if tableName == "commits" && s.readsManyRepos(sel) {
		tableSchema.PrimaryKeys = []string{"repo", "sha"}
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
			return s.read(ctx, tableName, sel, opts)
		},
	}, nil
}

// commitSelection is the parsed list of a "commits:..." table: repositories named one by one, as
// owner/name in the order written, and owners whose every repository is read through <owner>/*.
type commitSelection struct {
	repos  []string
	owners []string
}

// parseTableName splits "commits:<repo>,<owner>/<repo>,<owner>/*" into the table and its
// selection: trimmed, a bare name under defaultOwner, without empty entries or case-insensitive
// duplicates. The selection is nil without a list. Other tables keep their full name, so a ":"
// suffix fails the table check.
func parseTableName(name, defaultOwner string) (string, *commitSelection, error) {
	base, args, ok := strings.Cut(name, ":")
	if !ok || base != "commits" {
		return name, nil, nil
	}

	sel := &commitSelection{}
	seenRepos := make(map[string]bool)
	seenOwners := make(map[string]bool)
	for _, entry := range strings.Split(args, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		owner, repo, qualified := strings.Cut(entry, "/")
		owner, repo = strings.TrimSpace(owner), strings.TrimSpace(repo)
		switch {
		case strings.Contains(owner, "*"):
			return "", nil, wildcardError(entry, name)
		case !qualified && defaultOwner == "":
			return "", nil, fmt.Errorf("repository %q in table %s has no owner and the URI has no owner: write it as <owner>/%s, or use <owner>/* for every repository of an owner", entry, name, entry)
		case !qualified:
			owner, repo = defaultOwner, owner
		case owner == "" || strings.Contains(repo, "/"):
			return "", nil, invalidRepositoryError(entry, name, defaultOwner)
		case repo == "*":
			if !seenOwners[strings.ToLower(owner)] {
				seenOwners[strings.ToLower(owner)] = true
				sel.owners = append(sel.owners, owner)
			}
			continue
		case strings.Contains(repo, "*"):
			return "", nil, wildcardError(entry, name)
		case repo == "":
			return "", nil, fmt.Errorf("invalid repository %q in table %s: use %s/* for every repository of %s, or %s/<repo>", entry, name, owner, owner, owner)
		}
		fullName := owner + "/" + repo
		if seenRepos[strings.ToLower(fullName)] {
			continue
		}
		seenRepos[strings.ToLower(fullName)] = true
		sel.repos = append(sel.repos, fullName)
	}
	if len(sel.repos) == 0 && len(sel.owners) == 0 {
		return "", nil, fmt.Errorf("table %s lists no repositories, use commits:<repo>,<owner>/<repo>,<owner>/*", name)
	}
	return base, sel, nil
}

func wildcardError(entry, table string) error {
	return fmt.Errorf("invalid repository %q in table %s: a wildcard can only be <owner>/*, which reads every repository of the owner", entry, table)
}

func invalidRepositoryError(entry, table, defaultOwner string) error {
	if defaultOwner == "" {
		return fmt.Errorf("invalid repository %q in table %s: use <owner>/<repo>, or <owner>/* for every repository of an owner", entry, table)
	}
	return fmt.Errorf("invalid repository %q in table %s: use <repo> for a repository of %s, <owner>/<repo>, or <owner>/* for every repository of an owner", entry, table, defaultOwner)
}

// readsManyRepos reports whether commits come from a repository list or from every repository of
// the owner, rather than from the URI repo alone.
func (s *GithubSource) readsManyRepos(sel *commitSelection) bool {
	return sel != nil || s.repo == ""
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
	case "repositories":
		fields = repositoryFields
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

func (s *GithubSource) read(ctx context.Context, table string, sel *commitSelection, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if !isValidTable(table) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", table, strings.Join(supportedTables, ", "))
	}
	if s.apiKey == "" && graphQLTables[table] {
		return nil, fmt.Errorf("the %s table reads GitHub's GraphQL API, which needs an access_token", table)
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "commits":
			err = s.readCommits(ctx, opts, results, sel)
		case "issues":
			err = s.readIssues(ctx, opts, results)
		case "pull_requests":
			err = s.readPullRequests(ctx, opts, results)
		case "repo_events":
			err = s.readRepoEvents(ctx, opts, results)
		case "repositories":
			err = s.readRepositories(ctx, opts, results)
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

var errEmptyRepository = errors.New("repository is empty")

// unavailableRepoError is a REST answer saying the repository can't be read with this token:
// missing, blocked, or forbidden for a reason other than a rate limit.
type unavailableRepoError struct {
	error
	status int
}

func repoUnavailable(resp *ingestrhttp.Response) bool {
	switch resp.StatusCode() {
	case http.StatusNotFound, http.StatusUnavailableForLegalReasons:
		return true
	case http.StatusForbidden:
		return githubRateLimitKind(resp) == notRateLimited
	}
	return false
}

// readCommits reads the URI repo, or the selected repositories, or every repository of the owner
// except forks when neither is given. A repository found through an owner's listing that can't be
// read is skipped with a warning; a repository named in the list or the URI fails the run.
func (s *GithubSource) readCommits(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, sel *commitSelection) error {
	repos, err := s.resolveCommitRepos(ctx, sel)
	if err != nil {
		return err
	}

	sent := 0
	for _, r := range repos {
		repoOpts := opts
		if opts.Limit > 0 {
			repoOpts.Limit = opts.Limit - sent
			if repoOpts.Limit <= 0 {
				break
			}
		}
		n, err := s.readRepoCommits(ctx, repoOpts, results, r.fullName)
		sent += n
		var unavailable unavailableRepoError
		if !r.explicit && errors.As(err, &unavailable) {
			output.Warnf("\nWarning: skipping the commits of %s, GitHub answered %d\n", r.fullName, unavailable.status)
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// readRepoCommits lists one repository's default-branch commits. GitHub applies since/until to
// the committer date.
func (s *GithubSource) readRepoCommits(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, fullName string) (int, error) {
	config.Debug("[GITHUB] Reading commits for %s", fullName)

	params := url.Values{}
	params.Set("per_page", strconv.Itoa(maxPageSize))
	if opts.IntervalStart != nil {
		params.Set("since", opts.IntervalStart.UTC().Format(time.RFC3339))
	}
	if opts.IntervalEnd != nil {
		params.Set("until", opts.IntervalEnd.UTC().Format(time.RFC3339))
	}

	owner, name, _ := strings.Cut(fullName, "/")
	path := fmt.Sprintf("/repos/%s/%s/commits?%s", url.PathEscape(owner), url.PathEscape(name), params.Encode())
	n, err := s.paginateREST(ctx, opts, results, path, "", "commits", commitFields, func(c map[string]interface{}) bool {
		c["committed_at"] = committerDate(c)
		c["repo"] = fullName
		return true
	})
	if errors.Is(err, errEmptyRepository) {
		config.Debug("[GITHUB] %s has no commits yet", fullName)
		return n, nil
	}
	return n, err
}

// commitRepo is a repository to read commits from. explicit is false when it was found through an
// owner's listing rather than named in the list or the URI.
type commitRepo struct {
	fullName string
	explicit bool
}

// resolveCommitRepos returns the repositories to read under GitHub's full names: the selected
// repositories in their order, then the repositories of each selected owner except forks and
// those already selected; or the URI repo; or every repository of the URI owner except forks.
func (s *GithubSource) resolveCommitRepos(ctx context.Context, sel *commitSelection) ([]commitRepo, error) {
	switch {
	case sel != nil:
		named, err := s.lookupRepos(ctx, sel.repos)
		if err != nil {
			return nil, err
		}
		repos := make([]commitRepo, 0, len(named))
		seen := make(map[string]bool)
		for _, fullName := range named {
			seen[strings.ToLower(fullName)] = true
			repos = append(repos, commitRepo{fullName: fullName, explicit: true})
		}
		for _, owner := range sel.owners {
			listed, err := s.ownerCommitRepos(ctx, owner)
			if err != nil {
				return nil, err
			}
			for _, r := range listed {
				if !seen[strings.ToLower(r.fullName)] {
					seen[strings.ToLower(r.fullName)] = true
					repos = append(repos, r)
				}
			}
		}
		return repos, nil
	case s.repo != "":
		named, err := s.lookupRepos(ctx, []string{s.owner + "/" + s.repo})
		if err != nil {
			return nil, err
		}
		repos := make([]commitRepo, 0, len(named))
		for _, fullName := range named {
			repos = append(repos, commitRepo{fullName: fullName, explicit: true})
		}
		return repos, nil
	}
	return s.ownerCommitRepos(ctx, s.owner)
}

func (s *GithubSource) ownerCommitRepos(ctx context.Context, owner string) ([]commitRepo, error) {
	names, forks, err := s.listOwnerRepoNames(ctx, owner)
	if err != nil {
		return nil, err
	}
	output.Infof("[GITHUB] Reading the commits of %d repositories of %s (forks skipped: %d)\n", len(names), owner, forks)
	repos := make([]commitRepo, 0, len(names))
	for _, fullName := range names {
		repos = append(repos, commitRepo{fullName: fullName})
	}
	return repos, nil
}

// lookupRepos looks up each repository and returns GitHub's full names, following renames and
// dropping repositories named twice. Repositories that are missing or not readable with this
// token fail the run before any commit is read, all named at once.
func (s *GithubSource) lookupRepos(ctx context.Context, fullNames []string) ([]string, error) {
	var resolved, missing []string
	seen := make(map[string]bool)
	for _, fullName := range fullNames {
		canonical, found, err := s.lookupRepo(ctx, fullName)
		if err != nil {
			return nil, err
		}
		if !found {
			missing = append(missing, fullName)
			continue
		}
		if seen[strings.ToLower(canonical)] {
			continue
		}
		seen[strings.ToLower(canonical)] = true
		resolved = append(resolved, canonical)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("github repositories not found or not accessible with this token: %s", strings.Join(missing, ", "))
	}
	return resolved, nil
}

func (s *GithubSource) lookupRepo(ctx context.Context, fullName string) (string, bool, error) {
	owner, name, _ := strings.Cut(fullName, "/")
	var repo struct {
		FullName string `json:"full_name"`
	}
	resp, err := s.restClient.R(ctx).SetResult(&repo).Get(fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(name)))
	if err != nil {
		return "", false, fmt.Errorf("failed to look up github repository %s: %w", fullName, err)
	}
	if repoUnavailable(resp) {
		return "", false, nil
	}
	if !resp.IsSuccess() || repo.FullName == "" {
		return "", false, fmt.Errorf("failed to look up github repository %s with status %d: %s", fullName, resp.StatusCode(), resp.String())
	}
	return repo.FullName, true, nil
}

// listOwnerRepoNames lists the repositories the repositories table would read, once each, and
// returns the full names of those that aren't forks with the number of forks left out.
func (s *GithubSource) listOwnerRepoNames(ctx context.Context, owner string) ([]string, int, error) {
	listings, err := s.repositoryListings(ctx, owner)
	if err != nil {
		return nil, 0, err
	}

	seen := make(map[interface{}]bool)
	var names []string
	forks := 0
	for _, l := range listings {
		for nextURL := l.path; nextURL != ""; {
			resp, err := s.restClient.R(ctx).Get(nextURL)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to list github repositories of %s: %w", owner, err)
			}
			if !resp.IsSuccess() {
				return nil, 0, fmt.Errorf("failed to list github repositories of %s with status %d: %s", owner, resp.StatusCode(), resp.String())
			}
			items, err := decodeRESTItems(resp.Body(), l.itemsKey)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to decode github repositories of %s: %w", owner, err)
			}
			for _, item := range items {
				if (l.keep != nil && !l.keep(item)) || seen[item["id"]] {
					continue
				}
				seen[item["id"]] = true
				if fork, _ := item["fork"].(bool); fork {
					forks++
					continue
				}
				names = append(names, repoFullName(owner, item))
			}
			nextURL = extractNextPageURL(resp.Header().Get("Link"))
		}
	}
	return names, forks, nil
}

func repoFullName(owner string, repo map[string]interface{}) string {
	if fullName, _ := repo["full_name"].(string); fullName != "" {
		return fullName
	}
	name, _ := repo["name"].(string)
	return owner + "/" + name
}

type restListing struct {
	path     string
	itemsKey string
	keep     func(map[string]interface{}) bool
}

// readRepositories lists the repositories owned by the URI owner. A repository reachable through
// more than one listing is emitted once.
func (s *GithubSource) readRepositories(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[GITHUB] Reading repositories for %s", s.owner)

	listings, err := s.repositoryListings(ctx, s.owner)
	if err != nil {
		return err
	}

	seen := make(map[interface{}]bool)
	sent := 0
	for _, l := range listings {
		filter := l.keep
		keep := func(item map[string]interface{}) bool {
			if filter != nil && !filter(item) {
				return false
			}
			id := item["id"]
			if seen[id] {
				return false
			}
			seen[id] = true
			return true
		}
		listingOpts := opts
		if opts.Limit > 0 {
			listingOpts.Limit = opts.Limit - sent
			if listingOpts.Limit <= 0 {
				break
			}
		}
		n, err := s.paginateREST(ctx, listingOpts, results, l.path, l.itemsKey, "repositories", repositoryFields, keep)
		if err != nil {
			return err
		}
		sent += n
	}
	return nil
}

// repositoryListings picks the endpoints that cover ownerName's repositories for this token:
// org repos via /orgs, the token user's own via /user/repos, otherwise the public list, plus the
// installation's repositories when the token is a GitHub App installation token.
func (s *GithubSource) repositoryListings(ctx context.Context, ownerName string) ([]restListing, error) {
	params := url.Values{}
	params.Set("per_page", strconv.Itoa(maxPageSize))

	var owner struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	}
	resp, err := s.restClient.R(ctx).SetResult(&owner).Get("/users/" + url.PathEscape(ownerName))
	if err != nil {
		return nil, fmt.Errorf("failed to look up github owner %s: %w", ownerName, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("failed to look up github owner %s with status %d: %s", ownerName, resp.StatusCode(), resp.String())
	}

	if owner.Type == "Organization" {
		params.Set("type", "all")
		return []restListing{{path: fmt.Sprintf("/orgs/%s/repos?%s", url.PathEscape(ownerName), params.Encode())}}, nil
	}

	login := ""
	if s.apiKey != "" {
		login, err = s.authenticatedLogin(ctx)
		if err != nil {
			return nil, err
		}
		if login != "" && strings.EqualFold(login, owner.Login) {
			params.Set("affiliation", "owner")
			params.Set("visibility", "all")
			return []restListing{{path: "/user/repos?" + params.Encode()}}, nil
		}
	}

	params.Set("type", "owner")
	listings := []restListing{{path: fmt.Sprintf("/users/%s/repos?%s", url.PathEscape(ownerName), params.Encode())}}

	if s.apiKey != "" && login == "" {
		installation, err := s.installationListing(ctx, owner.Login)
		if err != nil {
			return nil, err
		}
		if installation != nil {
			listings = append(listings, *installation)
		} else {
			output.Warnf("\nWarning: the GitHub token has no user and no app installation, listing only the public repositories of %s\n", owner.Login)
		}
	}
	return listings, nil
}

// installationListing returns the installation's repositories owned by owner when the token is a
// GitHub App installation token (only those can read /installation/repositories), else nil.
func (s *GithubSource) installationListing(ctx context.Context, owner string) (*restListing, error) {
	resp, err := s.restClient.R(ctx).Get("/installation/repositories?per_page=1")
	if err != nil {
		return nil, fmt.Errorf("failed to probe github installation repositories: %w", err)
	}
	if !resp.IsSuccess() {
		if tokenLacksAccess(resp) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to probe github installation repositories with status %d: %s", resp.StatusCode(), resp.String())
	}
	config.Debug("[GITHUB] Installation token detected, adding the installation's repositories owned by %s", owner)
	return &restListing{
		path:     "/installation/repositories?per_page=" + strconv.Itoa(maxPageSize),
		itemsKey: "repositories",
		keep: func(item map[string]interface{}) bool {
			o, _ := item["owner"].(map[string]interface{})
			login, _ := o["login"].(string)
			return strings.EqualFold(login, owner)
		},
	}, nil
}

// authenticatedLogin returns the token's user login, or "" when the token has no user
// (e.g. a GitHub App installation token). Any other failure is an error, so an outage can't
// shrink the table to public repositories.
func (s *GithubSource) authenticatedLogin(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	resp, err := s.restClient.R(ctx).SetResult(&user).Get("/user")
	if err != nil {
		return "", fmt.Errorf("failed to resolve the authenticated github user: %w", err)
	}
	if resp.IsSuccess() {
		return user.Login, nil
	}
	if tokenLacksAccess(resp) {
		return "", nil
	}
	return "", fmt.Errorf("failed to resolve the authenticated github user with status %d: %s", resp.StatusCode(), resp.String())
}

// tokenLacksAccess reports a 401, or a 403 that is not a rate limit: the token can't use the
// endpoint at all, as opposed to GitHub failing or throttling.
func tokenLacksAccess(resp *ingestrhttp.Response) bool {
	switch resp.StatusCode() {
	case http.StatusUnauthorized:
		return true
	case http.StatusForbidden:
		return githubRateLimitKind(resp) == notRateLimited
	}
	return false
}

func (s *GithubSource) paginateREST(
	ctx context.Context,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	path, itemsKey, table string,
	fields []schema.Column,
	keep func(map[string]interface{}) bool,
) (int, error) {
	nextURL := path
	totalSent := 0

	for nextURL != "" {
		select {
		case <-ctx.Done():
			return totalSent, ctx.Err()
		default:
		}

		httpResp, err := s.restClient.R(ctx).Get(nextURL)
		if err != nil {
			return totalSent, fmt.Errorf("failed to fetch %s: %w", table, err)
		}

		if table == "commits" && isEmptyRepository(httpResp) {
			return totalSent, errEmptyRepository
		}

		if !httpResp.IsSuccess() {
			err := fmt.Errorf("failed to fetch %s with status %d: %s", table, httpResp.StatusCode(), httpResp.String())
			if repoUnavailable(httpResp) {
				return totalSent, unavailableRepoError{err, httpResp.StatusCode()}
			}
			return totalSent, err
		}

		items, err := decodeRESTItems(httpResp.Body(), itemsKey)
		if err != nil {
			return totalSent, fmt.Errorf("failed to decode %s: %w", table, err)
		}
		config.Debug("[GITHUB] Got %d %s, requests remaining: %s", len(items), table, httpResp.Header().Get("x-ratelimit-remaining"))

		if keep != nil {
			kept := items[:0]
			for _, item := range items {
				if keep(item) {
					kept = append(kept, item)
				}
			}
			items = kept
		}

		if opts.Limit > 0 && totalSent+len(items) > opts.Limit {
			items = items[:opts.Limit-totalSent]
		}

		var batch []map[string]interface{}
		var batchBytes int64
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, fields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", table, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(batch)
			batch = nil
			batchBytes = 0
			return nil
		}

		for _, item := range items {
			if opts.MaxBatchBytes > 0 {
				rowBytes := arrowconv.RowBytes(item)
				if len(batch) > 0 && batchBytes+rowBytes > opts.MaxBatchBytes {
					if err := flush(); err != nil {
						return totalSent, err
					}
				}
				batchBytes += rowBytes
			}
			batch = append(batch, item)
		}
		if err := flush(); err != nil {
			return totalSent, err
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}
		nextURL = extractNextPageURL(httpResp.Header().Get("Link"))
	}

	config.Debug("[GITHUB] Finished reading %s, total records: %d", table, totalSent)
	return totalSent, nil
}

// decodeRESTItems reads a REST list page: a JSON array, or the array under itemsKey for
// endpoints that wrap it (e.g. /installation/repositories).
func decodeRESTItems(body []byte, itemsKey string) ([]map[string]interface{}, error) {
	var items []map[string]interface{}
	if itemsKey == "" {
		err := json.Unmarshal(body, &items)
		return items, err
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, err
	}
	raw, ok := wrapper[itemsKey]
	if !ok {
		return nil, fmt.Errorf("response has no %q field", itemsKey)
	}
	err := json.Unmarshal(raw, &items)
	return items, err
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
				output.Warnf("\nWarning: could not fetch comment reactions for %s/%s, writing these %s without them: %v\n", s.owner, s.repo, rootField, err)
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
