package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ghPR is a pull request as the landing algorithm sees it.
type ghPR struct {
	Number       int
	MergedAt     *time.Time
	UpdatedAt    time.Time
	BaseRef      string
	HeadOid      string
	MergeCommit  string
	EventCommits []string
}

// ghCommit is one commit from a GraphQL history page.
type ghCommit struct {
	Oid            string
	CommittedDate  time.Time
	Parents        []string
	SignedByGitHub bool
	PRs            []ghPR
	PRsTotal       int
	page           int
}

type compareResult struct {
	Status           string
	AheadBy          int
	Commits          []map[string]interface{}
	NoCommonAncestor bool
}

// activityEntry is one entry of GET /repos/{owner}/{repo}/activity.
type activityEntry struct {
	Before    string
	After     string
	Timestamp time.Time
	Type      string
	Ref       string
}

type checkSuite struct {
	CreatedAt   time.Time
	Branch      string
	PreviousSha string
	HasPush     bool
}

// eventRequest asks for the MergedEvent commits of the PRs around one commit: the PR list of its
// second parent (a pre-2016 merge commit has an empty list of its own) and/or its own list.
type eventRequest struct {
	Commit  string
	Parent2 string
	Own     bool
}

type eventPR struct {
	ghPR
	FromParent2 bool
}

var errNoPermission = errors.New("token has no permission")

// landedAPI is everything the landing algorithm reads from GitHub. githubLandedAPI implements it on
// the live API; tests use an in-memory repository.
type landedAPI interface {
	defaultBranch(ctx context.Context) (string, error)
	branchHead(ctx context.Context, branch string) (sha string, at time.Time, empty bool, err error)
	commit(ctx context.Context, oid string) (*ghCommit, error)
	pageMates(oid string) []*ghCommit
	compare(ctx context.Context, base, head string, page int) (*compareResult, error)
	compareStatus(ctx context.Context, base, head string) (string, error)
	compareCount(ctx context.Context, base, head string) (string, int, error)
	requestCount() int
	mergedPRCount(ctx context.Context) (int, error)
	oldestActivity(ctx context.Context, ref string) (*activityEntry, error)
	listCommits(ctx context.Context, sha string, page int) ([]map[string]interface{}, error)
	restCommit(ctx context.Context, sha string) (map[string]interface{}, error)
	activity(ctx context.Context, query url.Values, stop time.Time) ([]activityEntry, error)
	checkSuites(ctx context.Context, oid string) ([]checkSuite, error)
	mergedEvents(ctx context.Context, reqs []eventRequest) (map[string][]eventPR, error)
	resolveBranch(ctx context.Context, name string) (string, error)
	mergedPullRequests(ctx context.Context, since time.Time) ([]ghPR, error)
}

const (
	comparePageSize   = 100
	compareMaxPages   = 100
	eventBatchSize    = 50
	activityPageSize  = 100
	landedCommitQuery = `oid committedDate parents(first: 100) { nodes { oid } } signature { wasSignedByGitHub } associatedPullRequests(first: 10) { totalCount nodes { number mergedAt baseRefName headRefOid mergeCommit { oid } } }`
	eventPRFields     = `associatedPullRequests(first: 5) { nodes { number mergedAt baseRefName headRefOid mergeCommit { oid } timelineItems(itemTypes: [MERGED_EVENT], first: 10) { nodes { ... on MergedEvent { commit { oid } } } } } }`
)

const historyQuery = `
query($owner: String!, $name: String!, $oid: GitObjectID!, $page_after: String) {
  repository(owner: $owner, name: $name) {
    object(oid: $oid) {
      ... on Commit {
        history(first: 100, after: $page_after) {
          pageInfo { hasNextPage endCursor }
          nodes { ` + landedCommitQuery + ` }
        }
      }
    }
  }
  rateLimit { limit cost remaining resetAt }
}`

const checkSuitesQuery = `
query($owner: String!, $name: String!, $oid: GitObjectID!) {
  repository(owner: $owner, name: $name) {
    object(oid: $oid) {
      ... on Commit {
        checkSuites(first: 50) { nodes { createdAt branch { name } push { previousSha } } }
      }
    }
  }
  rateLimit { limit cost remaining resetAt }
}`

const mergedPRCountQuery = `
query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) { pullRequests(states: MERGED) { totalCount } }
  rateLimit { limit cost remaining resetAt }
}`

const mergedPullRequestsQuery = `
query($owner: String!, $name: String!, $page_after: String) {
  repository(owner: $owner, name: $name) {
    pullRequests(first: 100, after: $page_after, states: MERGED, orderBy: {field: UPDATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes { number updatedAt mergedAt baseRefName headRefOid mergeCommit { oid } timelineItems(itemTypes: [MERGED_EVENT], first: 10) { nodes { ... on MergedEvent { commit { oid } } } } }
    }
  }
  rateLimit { limit cost remaining resetAt }
}`

var oidPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type gqlPRNode struct {
	Number      int     `json:"number"`
	MergedAt    *string `json:"mergedAt"`
	UpdatedAt   string  `json:"updatedAt"`
	BaseRefName string  `json:"baseRefName"`
	HeadRefOid  string  `json:"headRefOid"`
	MergeCommit *struct {
		Oid string `json:"oid"`
	} `json:"mergeCommit"`
	TimelineItems *struct {
		Nodes []struct {
			Commit *struct {
				Oid string `json:"oid"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"timelineItems"`
}

type gqlCommitNode struct {
	Oid           string `json:"oid"`
	CommittedDate string `json:"committedDate"`
	Parents       struct {
		Nodes []struct {
			Oid string `json:"oid"`
		} `json:"nodes"`
	} `json:"parents"`
	Signature *struct {
		WasSignedByGitHub bool `json:"wasSignedByGitHub"`
	} `json:"signature"`
	AssociatedPullRequests struct {
		TotalCount int         `json:"totalCount"`
		Nodes      []gqlPRNode `json:"nodes"`
	} `json:"associatedPullRequests"`
}

func parseGitHubTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad GitHub timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

func (n gqlPRNode) toPR() (ghPR, error) {
	pr := ghPR{Number: n.Number, BaseRef: n.BaseRefName, HeadOid: n.HeadRefOid}
	if n.MergedAt != nil && *n.MergedAt != "" {
		t, err := parseGitHubTime(*n.MergedAt)
		if err != nil {
			return pr, err
		}
		pr.MergedAt = &t
	}
	if n.UpdatedAt != "" {
		t, err := parseGitHubTime(n.UpdatedAt)
		if err != nil {
			return pr, err
		}
		pr.UpdatedAt = t
	}
	if n.MergeCommit != nil {
		pr.MergeCommit = n.MergeCommit.Oid
	}
	if n.TimelineItems != nil {
		for _, ev := range n.TimelineItems.Nodes {
			if ev.Commit != nil && ev.Commit.Oid != "" {
				pr.EventCommits = append(pr.EventCommits, ev.Commit.Oid)
			}
		}
	}
	return pr, nil
}

func (n gqlCommitNode) toCommit() (*ghCommit, error) {
	cd, err := parseGitHubTime(n.CommittedDate)
	if err != nil {
		return nil, err
	}
	c := &ghCommit{Oid: n.Oid, CommittedDate: cd, PRsTotal: n.AssociatedPullRequests.TotalCount}
	for _, p := range n.Parents.Nodes {
		c.Parents = append(c.Parents, p.Oid)
	}
	if n.Signature != nil {
		c.SignedByGitHub = n.Signature.WasSignedByGitHub
	}
	for _, p := range n.AssociatedPullRequests.Nodes {
		pr, err := p.toPR()
		if err != nil {
			return nil, err
		}
		c.PRs = append(c.PRs, pr)
	}
	return c, nil
}

func remarshal(in interface{}, out interface{}) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// githubLandedAPI reads the live GitHub API. Commits are cached for the whole run; a cache miss
// fetches one history page starting at the missing commit (a "restart" of the history walk).
type githubLandedAPI struct {
	s        *GithubSource
	commits  map[string]*ghCommit
	pages    map[int][]string
	nextPage int
}

func newGithubLandedAPI(s *GithubSource) *githubLandedAPI {
	return &githubLandedAPI{s: s, commits: map[string]*ghCommit{}, pages: map[int][]string{}}
}

func (a *githubLandedAPI) repoPath(suffix string) string {
	return fmt.Sprintf("/repos/%s/%s%s", url.PathEscape(a.s.owner), url.PathEscape(a.s.repo), suffix)
}

func (a *githubLandedAPI) defaultBranch(ctx context.Context) (string, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	resp, err := a.s.restGet(ctx, a.repoPath(""), &repo)
	if err != nil {
		return "", fmt.Errorf("failed to read repository: %w", err)
	}
	if !resp.IsSuccess() || repo.DefaultBranch == "" {
		return "", fmt.Errorf("failed to read the default branch with status %d: %s", resp.StatusCode(), resp.String())
	}
	return repo.DefaultBranch, nil
}

func (a *githubLandedAPI) branchHead(ctx context.Context, branch string) (string, time.Time, bool, error) {
	var c struct {
		SHA string `json:"sha"`
	}
	resp, err := a.s.restGet(ctx, a.repoPath("/commits/"+escapeRef(branch)), &c)
	if err != nil {
		return "", time.Time{}, false, fmt.Errorf("failed to read the head of %s: %w", branch, err)
	}
	if isEmptyRepository(resp) {
		return "", time.Time{}, true, nil
	}
	if !resp.IsSuccess() || c.SHA == "" {
		return "", time.Time{}, false, fmt.Errorf("failed to read the head of %s with status %d: %s", branch, resp.StatusCode(), resp.String())
	}
	at := time.Now().UTC()
	if d, err := http.ParseTime(resp.Header().Get("Date")); err == nil {
		at = d.UTC()
	}
	return c.SHA, at, false, nil
}

func escapeRef(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func (a *githubLandedAPI) commit(ctx context.Context, oid string) (*ghCommit, error) {
	if c, ok := a.commits[oid]; ok {
		return c, nil
	}
	if !oidPattern.MatchString(oid) {
		return nil, fmt.Errorf("not a commit id: %q", oid)
	}
	page := a.nextPage
	a.nextPage++
	err := a.s.walkGraphQLConnection(ctx, historyQuery, map[string]interface{}{"oid": oid}, []string{"repository", "object", "history"}, "nodes",
		func(items []interface{}, _ *rateLimitInfo) (bool, error) {
			for _, item := range items {
				var n gqlCommitNode
				if err := remarshal(item, &n); err != nil {
					return false, fmt.Errorf("failed to parse history node: %w", err)
				}
				c, err := n.toCommit()
				if err != nil {
					return false, err
				}
				if _, seen := a.commits[c.Oid]; seen {
					continue
				}
				c.page = page
				a.commits[c.Oid] = c
				a.pages[page] = append(a.pages[page], c.Oid)
			}
			return false, nil
		})
	if err != nil {
		return nil, fmt.Errorf("failed to read the history at %s: %w", oid, err)
	}
	c, ok := a.commits[oid]
	if !ok {
		return nil, fmt.Errorf("commit %s not found", oid)
	}
	return c, nil
}

func (a *githubLandedAPI) pageMates(oid string) []*ghCommit {
	c, ok := a.commits[oid]
	if !ok {
		return nil
	}
	var out []*ghCommit
	for _, m := range a.pages[c.page] {
		out = append(out, a.commits[m])
	}
	return out
}

func (a *githubLandedAPI) compare(ctx context.Context, base, head string, page int) (*compareResult, error) {
	var body struct {
		Status  string                   `json:"status"`
		AheadBy int                      `json:"ahead_by"`
		Commits []map[string]interface{} `json:"commits"`
	}
	path := a.repoPath(fmt.Sprintf("/compare/%s...%s?per_page=%d&page=%d", base, head, comparePageSize, page))
	resp, err := a.s.restGet(ctx, path, &body)
	if err != nil {
		return nil, fmt.Errorf("compare %s...%s: %w", short(base), short(head), err)
	}
	if resp.StatusCode() == http.StatusNotFound && strings.Contains(resp.String(), "No common ancestor") {
		return &compareResult{NoCommonAncestor: true}, nil
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("compare %s...%s failed with status %d: %s", short(base), short(head), resp.StatusCode(), resp.String())
	}
	return &compareResult{Status: body.Status, AheadBy: body.AheadBy, Commits: body.Commits}, nil
}

// compareStatus is compare's status alone ("ahead", "behind", "identical", "diverged"); "not_found"
// when either commit is unknown or the histories are unrelated.
func (a *githubLandedAPI) compareStatus(ctx context.Context, base, head string) (string, error) {
	st, _, err := a.compareCount(ctx, base, head)
	return st, err
}

// compareCount is compare's status and ahead_by, without the commit list.
func (a *githubLandedAPI) compareCount(ctx context.Context, base, head string) (string, int, error) {
	var body struct {
		Status  string `json:"status"`
		AheadBy int    `json:"ahead_by"`
	}
	resp, err := a.s.restGet(ctx, a.repoPath(fmt.Sprintf("/compare/%s...%s?per_page=1", base, head)), &body)
	if err != nil {
		return "", 0, fmt.Errorf("compare %s...%s: %w", short(base), short(head), err)
	}
	if resp.StatusCode() == http.StatusNotFound {
		return "not_found", 0, nil
	}
	if !resp.IsSuccess() {
		return "", 0, fmt.Errorf("compare %s...%s failed with status %d: %s", short(base), short(head), resp.StatusCode(), resp.String())
	}
	return body.Status, body.AheadBy, nil
}

func (a *githubLandedAPI) requestCount() int {
	return a.s.requests
}

func (a *githubLandedAPI) mergedPRCount(ctx context.Context) (int, error) {
	data, _, err := a.s.executeGraphQL(ctx, mergedPRCountQuery, map[string]interface{}{"owner": a.s.owner, "name": a.s.repo})
	if err != nil {
		return 0, fmt.Errorf("failed to count merged pull requests: %w", err)
	}
	var body struct {
		Repository *struct {
			PullRequests struct {
				TotalCount int `json:"totalCount"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &body); err != nil || body.Repository == nil {
		return 0, fmt.Errorf("failed to parse the merged pull request count: %w", err)
	}
	return body.Repository.PullRequests.TotalCount, nil
}

// oldestActivity is the first entry of the ref's activity log (direction=asc), or nil when it is empty.
func (a *githubLandedAPI) oldestActivity(ctx context.Context, ref string) (*activityEntry, error) {
	q := url.Values{"ref": {ref}, "direction": {"asc"}, "per_page": {"1"}}
	var page []struct {
		Before       string `json:"before"`
		After        string `json:"after"`
		Ref          string `json:"ref"`
		Timestamp    string `json:"timestamp"`
		ActivityType string `json:"activity_type"`
	}
	resp, err := a.s.restGet(ctx, a.repoPath("/activity?"+q.Encode()), &page)
	if err != nil {
		return nil, err
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode(), resp.String())
	}
	if len(page) == 0 {
		return nil, nil
	}
	ts, err := parseGitHubTime(page[0].Timestamp)
	if err != nil {
		return nil, err
	}
	return &activityEntry{Before: page[0].Before, After: page[0].After, Timestamp: ts, Type: page[0].ActivityType, Ref: page[0].Ref}, nil
}

func (a *githubLandedAPI) restCommit(ctx context.Context, sha string) (map[string]interface{}, error) {
	var commits []map[string]interface{}
	resp, err := a.s.restGet(ctx, a.repoPath("/commits?per_page=1&sha="+url.QueryEscape(sha)), &commits)
	if err != nil {
		return nil, fmt.Errorf("read commit %s: %w", short(sha), err)
	}
	if !resp.IsSuccess() || len(commits) == 0 {
		return nil, fmt.Errorf("read commit %s failed with status %d: %s", short(sha), resp.StatusCode(), resp.String())
	}
	return commits[0], nil
}

func (a *githubLandedAPI) listCommits(ctx context.Context, sha string, page int) ([]map[string]interface{}, error) {
	var commits []map[string]interface{}
	path := a.repoPath(fmt.Sprintf("/commits?sha=%s&per_page=%d&page=%d", url.QueryEscape(sha), comparePageSize, page))
	resp, err := a.s.restGet(ctx, path, &commits)
	if err != nil {
		return nil, fmt.Errorf("list commits of %s: %w", short(sha), err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("list commits of %s failed with status %d: %s", short(sha), resp.StatusCode(), resp.String())
	}
	return commits, nil
}

// activity reads the activity log newest first, page by page, and stops after the first page whose
// oldest entry is older than stop. There is no page limit (no walk budget, PLAN_REVIEW A7).
func (a *githubLandedAPI) activity(ctx context.Context, query url.Values, stop time.Time) ([]activityEntry, error) {
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	q.Set("per_page", strconv.Itoa(activityPageSize))
	next := a.repoPath("/activity?" + q.Encode())
	var out []activityEntry
	for next != "" {
		var page []struct {
			Before       string `json:"before"`
			After        string `json:"after"`
			Ref          string `json:"ref"`
			Timestamp    string `json:"timestamp"`
			ActivityType string `json:"activity_type"`
		}
		resp, err := a.s.restGet(ctx, next, &page)
		if err != nil {
			return nil, err
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode(), resp.String())
		}
		if len(page) == 0 {
			break
		}
		for _, e := range page {
			ts, err := parseGitHubTime(e.Timestamp)
			if err != nil {
				return nil, err
			}
			out = append(out, activityEntry{Before: e.Before, After: e.After, Timestamp: ts, Type: e.ActivityType, Ref: e.Ref})
		}
		if out[len(out)-1].Timestamp.Before(stop) {
			break
		}
		next = extractNextPageURL(resp.Header().Get("Link"))
	}
	return out, nil
}

func (a *githubLandedAPI) checkSuites(ctx context.Context, oid string) ([]checkSuite, error) {
	data, _, err := a.s.executeGraphQL(ctx, checkSuitesQuery, map[string]interface{}{"owner": a.s.owner, "name": a.s.repo, "oid": oid})
	if err != nil {
		var gqlErrs graphQLErrors
		if errors.As(err, &gqlErrs) && (gqlErrs.hasType("FORBIDDEN") || strings.Contains(strings.ToLower(err.Error()), "not accessible")) {
			return nil, fmt.Errorf("%w: %v", errNoPermission, err)
		}
		if strings.Contains(err.Error(), "status 403") {
			return nil, fmt.Errorf("%w: %v", errNoPermission, err)
		}
		return nil, err
	}
	var body struct {
		Repository struct {
			Object *struct {
				CheckSuites struct {
					Nodes []struct {
						CreatedAt string `json:"createdAt"`
						Branch    *struct {
							Name string `json:"name"`
						} `json:"branch"`
						Push *struct {
							PreviousSha string `json:"previousSha"`
						} `json:"push"`
					} `json:"nodes"`
				} `json:"checkSuites"`
			} `json:"object"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("failed to parse check suites of %s: %w", short(oid), err)
	}
	if body.Repository.Object == nil {
		return nil, fmt.Errorf("commit %s not found while reading its check suites", oid)
	}
	var out []checkSuite
	for _, n := range body.Repository.Object.CheckSuites.Nodes {
		created, err := parseGitHubTime(n.CreatedAt)
		if err != nil {
			return nil, err
		}
		cs := checkSuite{CreatedAt: created}
		if n.Branch != nil {
			cs.Branch = n.Branch.Name
		}
		if n.Push != nil {
			cs.HasPush = true
			cs.PreviousSha = n.Push.PreviousSha
		}
		out = append(out, cs)
	}
	return out, nil
}

func (a *githubLandedAPI) mergedEvents(ctx context.Context, reqs []eventRequest) (map[string][]eventPR, error) {
	out := map[string][]eventPR{}
	for startIdx := 0; startIdx < len(reqs); startIdx += eventBatchSize {
		batch := reqs[startIdx:min(startIdx+eventBatchSize, len(reqs))]
		var parts []string
		owners := map[string]eventRequest{}
		for i, r := range batch {
			if _, ok := out[r.Commit]; !ok {
				out[r.Commit] = nil
			}
			if r.Parent2 != "" {
				if !oidPattern.MatchString(r.Parent2) {
					return nil, fmt.Errorf("not a commit id: %q", r.Parent2)
				}
				alias := fmt.Sprintf("a%d", i)
				parts = append(parts, fmt.Sprintf(`%s: object(oid: "%s") { ... on Commit { %s } }`, alias, r.Parent2, eventPRFields))
				owners[alias] = r
			}
			if r.Parent2 == "" || r.Own {
				if !oidPattern.MatchString(r.Commit) {
					return nil, fmt.Errorf("not a commit id: %q", r.Commit)
				}
				alias := fmt.Sprintf("b%d", i)
				parts = append(parts, fmt.Sprintf(`%s: object(oid: "%s") { ... on Commit { %s } }`, alias, r.Commit, eventPRFields))
				owners[alias] = r
			}
		}
		query := "query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) { " + strings.Join(parts, " ") + " } rateLimit { limit cost remaining resetAt } }"
		data, _, err := a.s.executeGraphQL(ctx, query, map[string]interface{}{"owner": a.s.owner, "name": a.s.repo})
		if err != nil {
			return nil, fmt.Errorf("MergedEvent lookup failed: %w", err)
		}
		var body struct {
			Repository map[string]*struct {
				AssociatedPullRequests struct {
					Nodes []gqlPRNode `json:"nodes"`
				} `json:"associatedPullRequests"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			return nil, fmt.Errorf("failed to parse MergedEvent lookup: %w", err)
		}
		for alias, obj := range body.Repository {
			r, ok := owners[alias]
			if !ok || obj == nil {
				continue
			}
			for _, n := range obj.AssociatedPullRequests.Nodes {
				pr, err := n.toPR()
				if err != nil {
					return nil, err
				}
				if pr.MergedAt == nil {
					continue
				}
				out[r.Commit] = append(out[r.Commit], eventPR{ghPR: pr, FromParent2: strings.HasPrefix(alias, "a")})
			}
		}
	}
	return out, nil
}

func (a *githubLandedAPI) resolveBranch(ctx context.Context, name string) (string, error) {
	var b struct {
		Name string `json:"name"`
	}
	resp, err := a.s.restGet(ctx, a.repoPath("/branches/"+escapeRef(name)), &b)
	if err != nil {
		return "", fmt.Errorf("failed to resolve branch %s: %w", name, err)
	}
	if resp.StatusCode() == http.StatusNotFound {
		return "", nil
	}
	if !resp.IsSuccess() {
		return "", fmt.Errorf("failed to resolve branch %s with status %d: %s", name, resp.StatusCode(), resp.String())
	}
	return b.Name, nil
}

// mergedPullRequests lists merged PRs newest-updated first and stops at the first page whose oldest
// PR was updated before since (a PR merged at or after since was updated at or after it). No
// baseRefName filter: PRs keep the base name they were merged into (EXP-63).
func (a *githubLandedAPI) mergedPullRequests(ctx context.Context, since time.Time) ([]ghPR, error) {
	var out []ghPR
	err := a.s.walkGraphQLConnection(ctx, mergedPullRequestsQuery, nil, []string{"repository", "pullRequests"}, "nodes",
		func(items []interface{}, _ *rateLimitInfo) (bool, error) {
			var oldest time.Time
			for _, item := range items {
				var n gqlPRNode
				if err := remarshal(item, &n); err != nil {
					return false, fmt.Errorf("failed to parse pull request: %w", err)
				}
				pr, err := n.toPR()
				if err != nil {
					return false, err
				}
				out = append(out, pr)
				oldest = pr.UpdatedAt
			}
			return !oldest.Before(since), nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
