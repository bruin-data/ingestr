package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

// TestGithubByteCap proves the MaxBatchBytes cap on the repo_events path: with the
// cap off a single events page lands in one batch; with a small cap the same
// events split across many batches with no event lost.
func TestGithubByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve one events page; no Link header => pagination stops after this page.
		events := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			events = append(events, map[string]interface{}{
				"id":         "evt-" + strconv.Itoa(i),
				"type":       "PushEvent",
				"created_at": "2023-01-01T00:00:00Z",
				"blob":       wide,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(events)
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &GithubSource{
			owner:      "o",
			repo:       "r",
			restClient: ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL)),
		}
		results := make(chan source.RecordBatchResult, 64)
		go func() {
			defer close(results)
			if err := s.readRepoEvents(context.Background(), source.ReadOptions{MaxBatchBytes: max}, results); err != nil {
				results <- source.RecordBatchResult{Err: err}
			}
		}()
		var batches, rows int64
		for res := range results {
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			if res.Batch == nil {
				continue
			}
			batches++
			rows += res.Batch.NumRows()
			res.Batch.Release()
		}
		return batches, rows
	}

	offB, offR := run(0)
	onB, onR := run(4096)

	if offB != 1 {
		t.Fatalf("cap-off batches=%d want 1", offB)
	}
	if onB <= 1 {
		t.Fatalf("cap-on batches=%d want >1", onB)
	}
	if offR != onR || offR != 50 {
		t.Fatalf("row mismatch off=%d on=%d want 50", offR, onR)
	}
}

func TestGithubReadCommits(t *testing.T) {
	commit := func(sha, date string) map[string]interface{} {
		return map[string]interface{}{
			"sha": sha,
			"commit": map[string]interface{}{
				"committer":    map[string]interface{}{"date": date},
				"verification": map[string]interface{}{"verified": true, "reason": "valid"},
			},
		}
	}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveRepoLookup(w, r) {
			return
		}
		if r.URL.Path != "/repos/o/r/commits" {
			t.Errorf("path=%s want /repos/o/r/commits", r.URL.Path)
		}
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		if q.Get("page") == "" {
			if got := q.Get("since"); got != "2026-10-30T00:00:00Z" {
				t.Errorf("since=%q", got)
			}
			if got := q.Get("until"); got != "2026-10-30T23:59:59Z" {
				t.Errorf("until=%q", got)
			}
			if got := q.Get("per_page"); got != "100" {
				t.Errorf("per_page=%q", got)
			}
			w.Header().Set("Link", "<"+srv.URL+"/repos/o/r/commits?page=2>; rel=\"next\"")
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				commit("a1", "2026-10-30T13:05:11Z"),
				commit("b2", "2026-10-30T12:32:47Z"),
			})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			commit("c3", "2026-10-30T07:46:55Z"),
		})
	}))
	defer srv.Close()

	s := &GithubSource{
		owner:      "o",
		repo:       "r",
		restClient: ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL)),
	}
	start := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 30, 23, 59, 59, 0, time.UTC)

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		if err := s.readCommits(context.Background(), source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}, results, nil); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	var rows int64
	for res := range results {
		if res.Err != nil {
			t.Fatal(res.Err)
		}
		if res.Batch == nil {
			continue
		}
		if len(res.Batch.Schema().FieldIndices("sha")) == 0 || len(res.Batch.Schema().FieldIndices("commit")) == 0 {
			t.Fatalf("missing sha/commit columns: %s", res.Batch.Schema())
		}
		idx := res.Batch.Schema().FieldIndices("committed_at")
		if len(idx) == 0 {
			t.Fatalf("missing committed_at column: %s", res.Batch.Schema())
		}
		if typ := res.Batch.Schema().Field(idx[0]).Type.Name(); typ != "timestamp" {
			t.Fatalf("committed_at type=%s want timestamp", typ)
		}
		if nulls := res.Batch.Column(idx[0]).NullN(); nulls != 0 {
			t.Fatalf("committed_at has %d nulls, want 0", nulls)
		}
		rows += res.Batch.NumRows()
		res.Batch.Release()
	}
	if rows != 3 {
		t.Fatalf("rows=%d want 3", rows)
	}
}

func TestGithubReadCommitsMissingCommitterDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveRepoLookup(w, r) {
			return
		}
		if q := r.URL.Query(); q.Has("since") || q.Has("until") {
			t.Errorf("unexpected interval params without an interval: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"sha": "a1", "commit": map[string]interface{}{"committer": map[string]interface{}{"date": "2026-10-30T13:05:11Z"}}},
			{"sha": "b2", "commit": map[string]interface{}{"committer": nil}},
			{"sha": "c3", "commit": map[string]interface{}{"committer": map[string]interface{}{"name": "x", "email": "x@example.com"}}},
		})
	}))
	defer srv.Close()

	s := &GithubSource{
		owner:      "o",
		repo:       "r",
		restClient: ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL)),
	}
	results := make(chan source.RecordBatchResult, 1)
	if err := s.readCommits(context.Background(), source.ReadOptions{}, results, nil); err != nil {
		t.Fatal(err)
	}
	close(results)

	res := <-results
	if res.Batch == nil {
		t.Fatal("expected one batch")
	}
	defer res.Batch.Release()
	if rows := res.Batch.NumRows(); rows != 3 {
		t.Fatalf("rows=%d want 3", rows)
	}
	idx := res.Batch.Schema().FieldIndices("committed_at")
	if len(idx) == 0 {
		t.Fatalf("missing committed_at column: %s", res.Batch.Schema())
	}
	if nulls := res.Batch.Column(idx[0]).NullN(); nulls != 2 {
		t.Fatalf("committed_at nulls=%d want 2", nulls)
	}
}

func TestGithubReadCommitsEmptyRepository(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveRepoLookup(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"Git Repository is empty.","documentation_url":"https://docs.github.com/rest/commits/commits#list-commits","status":"409"}`))
	}))
	defer srv.Close()

	s := &GithubSource{
		owner:      "o",
		repo:       "r",
		restClient: ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL)),
	}
	results := make(chan source.RecordBatchResult, 1)
	if err := s.readCommits(context.Background(), source.ReadOptions{}, results, nil); err != nil {
		t.Fatalf("empty repository should read as zero rows, got: %v", err)
	}
	close(results)

	if res, ok := <-results; ok {
		t.Fatalf("expected no batches, got one with %d rows", res.Batch.NumRows())
	}
}

type cannedResponse struct {
	status  int
	headers map[string]string
	body    string
}

func epoch(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }

var repoOR = cannedResponse{200, nil, `{"full_name":"o/r"}`}

// serveRepoLookup answers the repository lookup single mode makes before reading commits.
func serveRepoLookup(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/repos/o/r" {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"full_name":"o/r"}`))
	return true
}

// The exact bodies GitHub returned live for the two GraphQL primary-limit cases: no points left
// (x-ratelimit-remaining 0) and a query costing more than the points left (remaining stayed at 98).
const (
	liveGraphQLRateLimitBody          = `{"errors":[{"type":"RATE_LIMIT","code":"graphql_rate_limit","message":"API rate limit already exceeded for user ID 55869347."}]}`
	liveGraphQLCostAboveRemainingBody = `{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded for user ID 55869347."}]}`
)

func graphQLQuotaSpent() cannedResponse {
	return cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(time.Now().Add(-time.Minute))}, liveGraphQLRateLimitBody}
}

func restQuotaSpent() cannedResponse {
	return cannedResponse{403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(time.Now().Add(-time.Minute))}, `{"message":"API rate limit exceeded for 1.2.3.4."}`}
}

func TestGithubRateLimitWait(t *testing.T) {
	now := time.Now()
	in10m := epoch(now.Add(10 * time.Minute))

	cases := []struct {
		name      string
		resp      cannedResponse
		throttled bool
		minWait   time.Duration
		maxWait   time.Duration
	}{
		{"rest primary 403", cannedResponse{403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": in10m}, `{"message":"API rate limit exceeded for 1.2.3.4."}`}, true, 9 * time.Minute, 11 * time.Minute},
		{"rest primary 429", cannedResponse{429, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": in10m}, ""}, true, 9 * time.Minute, 11 * time.Minute},
		{"reset beyond an hour is capped", cannedResponse{403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(now.Add(3 * time.Hour))}, ""}, true, retryMaxWait, retryMaxWait},
		{"reset that already passed while still limited waits a minute", cannedResponse{403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(now.Add(-time.Minute))}, ""}, true, time.Minute, time.Minute},
		{"reset in the current second waits a second, not zero", cannedResponse{403, map[string]string{"Date": now.UTC().Format(http.TimeFormat), "X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(now)}, ""}, true, time.Second, time.Second},
		{"429 without any details waits a minute", cannedResponse{429, nil, "<html>Too Many Requests</html>"}, true, time.Minute, time.Minute},
		{"huge retry-after doesn't overflow", cannedResponse{429, map[string]string{"Retry-After": "9999999999"}, ""}, true, retryMaxWait, retryMaxWait},
		{"graphql secondary message wins over a RATE_LIMITED type", cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "4000", "X-RateLimit-Reset": in10m}, `{"errors":[{"type":"RATE_LIMITED","message":"You have exceeded a secondary rate limit."}]}`}, true, time.Minute, time.Minute},
		{"graphql secondary message in another case", cannedResponse{200, nil, `{"errors":[{"message":"Secondary Rate Limit exceeded"}]}`}, true, time.Minute, time.Minute},
		{"graphql abuse detection message", cannedResponse{200, nil, `{"errors":[{"message":"You have triggered an abuse detection mechanism."}]}`}, true, time.Minute, time.Minute},
		{"secondary with retry-after", cannedResponse{403, map[string]string{"Retry-After": "30", "X-RateLimit-Remaining": "4000"}, ""}, true, 30 * time.Second, 30 * time.Second},
		{"secondary without headers", cannedResponse{403, nil, `{"message":"You have exceeded a secondary rate limit."}`}, true, time.Minute, time.Minute},
		{"graphql primary 200", cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": in10m}, liveGraphQLRateLimitBody}, true, 9 * time.Minute, 11 * time.Minute},
		{"graphql secondary message with a spent quota waits for the reset", cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": in10m}, `{"errors":[{"message":"You have exceeded a secondary rate limit."}]}`}, true, 9 * time.Minute, 11 * time.Minute},
		{"graphql query costing more than the points left waits for the reset", cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "98", "X-RateLimit-Reset": in10m}, liveGraphQLCostAboveRemainingBody}, true, 9 * time.Minute, 11 * time.Minute},
		{"secondary with spent quota waits for the reset", cannedResponse{403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": in10m}, `{"message":"You have exceeded a secondary rate limit."}`}, true, 9 * time.Minute, 11 * time.Minute},
		{"graphql secondary 200", cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "4000"}, `{"errors":[{"message":"You have exceeded a secondary rate limit. Please wait a few minutes."}]}`}, true, time.Minute, time.Minute},
		{"retry-after above an hour is capped", cannedResponse{429, map[string]string{"Retry-After": "7200"}, ""}, true, retryMaxWait, retryMaxWait},
		{"retry-after as an HTTP date", cannedResponse{403, map[string]string{"Date": now.UTC().Format(http.TimeFormat), "Retry-After": now.Add(2 * time.Minute).UTC().Format(http.TimeFormat)}, ""}, true, 119 * time.Second, 121 * time.Second},
		{"503 with retry-after", cannedResponse{503, map[string]string{"Retry-After": "30"}, ""}, true, 30 * time.Second, 30 * time.Second},
		{"reset measured on GitHub's clock, not ours", cannedResponse{403, map[string]string{"Date": now.Add(-10 * time.Second).UTC().Format(http.TimeFormat), "X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(now.Add(-5 * time.Second))}, ""}, true, 4 * time.Second, 7 * time.Second},
		{"403 rate-limit message without headers", cannedResponse{403, nil, `{"message":"API rate limit exceeded for user ID 1."}`}, true, time.Minute, time.Minute},
		{"abuse detection message", cannedResponse{403, nil, `{"message":"You have triggered an abuse detection mechanism. Please wait a few minutes before you try again."}`}, true, time.Minute, time.Minute},
		{"permission denied", cannedResponse{403, map[string]string{"X-RateLimit-Remaining": "4999"}, `{"message":"Resource not accessible by personal access token"}`}, false, 0, 0},
		{"not found", cannedResponse{404, map[string]string{"X-RateLimit-Remaining": "0"}, ""}, false, 0, 0},
		{"data mentioning rate limits", cannedResponse{200, nil, `{"data":{"issue":{"title":"We hit a secondary rate limit","label":"RATE_LIMIT"}}}`}, false, 0, 0},
		{"rest page mentioning rate limits", cannedResponse{200, nil, `[{"sha":"a","commit":{"message":"handle secondary rate limit and RATE_LIMIT errors"}}]`}, false, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wait, _, throttled := githubRetryWait(cannedHTTPResponse(t, tc.resp), now)
			if throttled != tc.throttled || wait < tc.minWait || wait > tc.maxWait {
				t.Fatalf("got wait=%v throttled=%v, want throttled=%v wait in [%v, %v]", wait, throttled, tc.throttled, tc.minWait, tc.maxWait)
			}
		})
	}
}

// githubRetryWait accepts any Retry-After, so the condition is checked on its own: it decides
// which answers are retried at all.
func TestGithubRetryCondition(t *testing.T) {
	cases := []struct {
		name  string
		resp  cannedResponse
		retry bool
	}{
		{"403 with only Retry-After", cannedResponse{403, map[string]string{"Retry-After": "30"}, ""}, true},
		{"429 without details", cannedResponse{429, nil, ""}, true},
		{"graphql query costing more than the points left", cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "98"}, liveGraphQLCostAboveRemainingBody}, true},
		{"graphql secondary message", cannedResponse{200, nil, `{"errors":[{"message":"You have exceeded a secondary rate limit."}]}`}, true},
		{"permission 403", cannedResponse{403, map[string]string{"X-RateLimit-Remaining": "4999"}, `{"message":"Resource not accessible by personal access token"}`}, false},
		{"graphql not found", cannedResponse{200, nil, `{"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository with the name 'o/r'."}]}`}, false},
		{"page mentioning rate limits", cannedResponse{200, nil, `{"data":{"issue":{"title":"We hit a secondary rate limit","label":"RATE_LIMIT"}}}`}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := githubRetryCondition(cannedHTTPResponse(t, tc.resp), nil); got != tc.retry {
				t.Fatalf("retry=%v, want %v", got, tc.retry)
			}
		})
	}
}

// GitHub answered the live GraphQL test from two counters with different resets, alternating
// between requests; each wait must come from the response it follows.
func TestGithubRateLimitWaitUsesEachResponse(t *testing.T) {
	now := time.Now()
	counterA := cannedHTTPResponse(t, cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(now.Add(5 * time.Minute))}, liveGraphQLRateLimitBody})
	counterB := cannedHTTPResponse(t, cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "98", "X-RateLimit-Reset": epoch(now.Add(40 * time.Minute))}, liveGraphQLCostAboveRemainingBody})

	for i, step := range []struct {
		resp     *ingestrhttp.Response
		min, max time.Duration
	}{
		{counterA, 4 * time.Minute, 6 * time.Minute},
		{counterB, 39 * time.Minute, 41 * time.Minute},
		{counterA, 4 * time.Minute, 6 * time.Minute},
	} {
		wait, _, ok := githubRetryWait(step.resp, now)
		if !ok || wait < step.min || wait > step.max {
			t.Fatalf("step %d: wait=%v ok=%v, want [%v, %v]", i, wait, ok, step.min, step.max)
		}
	}
}

func cannedHTTPResponse(t *testing.T, r cannedResponse) *ingestrhttp.Response {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeCanned(w, r)
	}))
	t.Cleanup(srv.Close)

	client := ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL), ingestrhttp.WithDisableRetry())
	client.Resty().AddResponseMiddleware(moveRetryAfter)
	t.Cleanup(func() { _ = client.Close() })

	resp, err := client.R(context.Background()).Get("/")
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func writeCanned(w http.ResponseWriter, r cannedResponse) {
	w.Header().Set("Content-Type", "application/json")
	for k, v := range r.headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(r.status)
	_, _ = w.Write([]byte(r.body))
}

// fakeGitHub serves scripted REST and GraphQL responses and records what the source sent.
type fakeGitHub struct {
	mu          sync.Mutex
	graphql     []cannedResponse
	rest        []cannedResponse
	cursors     []string
	restPaths   []string
	authHeaders []string
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authHeaders = append(f.authHeaders, r.Header.Get("Authorization"))

	if r.URL.Path == "/graphql" {
		var req struct {
			Variables map[string]interface{} `json:"variables"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		cursor, _ := req.Variables["page_after"].(string)
		f.cursors = append(f.cursors, r.Method+" "+cursor)
		writeCanned(w, f.graphql[min(len(f.cursors)-1, len(f.graphql)-1)])
		return
	}

	f.restPaths = append(f.restPaths, r.URL.RequestURI())
	writeCanned(w, f.rest[min(len(f.restPaths)-1, len(f.rest)-1)])
}

// connectToFake runs the real Connect against a local fake GitHub, so the retry
// options, auth and URI parsing under test are the production wiring.
func connectToFake(t *testing.T, f *fakeGitHub) (*GithubSource, *httptest.Server) {
	t.Helper()
	return connectURIToFake(t, f, "github://?access_token=test-token&owner=o&repo=r")
}

func connectURIToFake(t *testing.T, h http.Handler, uri string) (*GithubSource, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	prevREST, prevGraphQL := restApiBaseUrl, graphqlBaseUrl
	restApiBaseUrl, graphqlBaseUrl = srv.URL, srv.URL+"/graphql"
	t.Cleanup(func() { restApiBaseUrl, graphqlBaseUrl = prevREST, prevGraphQL })

	prevRetryWait, prevSecondaryWait := retryWait, secondaryRateLimitWait
	retryWait, secondaryRateLimitWait = time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { retryWait, secondaryRateLimitWait = prevRetryWait, prevSecondaryWait })

	s := NewGitHubSource()
	if err := s.Connect(context.Background(), uri); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s, srv
}

func captureOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevErr, prevMode := output.Current()
	output.Init(&buf, &buf, output.ModeText)
	t.Cleanup(func() { output.Init(prevOut, prevErr, prevMode) })
	return &buf
}

func readTable(t *testing.T, s *GithubSource, ctx context.Context, table string) (int64, error) {
	t.Helper()
	results, err := s.read(ctx, table, nil, source.ReadOptions{})
	if err != nil {
		return 0, err
	}
	var rows int64
	var readErr error
	for res := range results {
		if res.Err != nil {
			readErr = res.Err
			continue
		}
		rows += res.Batch.NumRows()
		res.Batch.Release()
	}
	return rows, readErr
}

func issuesPage(endCursor string, numbers ...int) cannedResponse {
	nodes := make([]string, 0, len(numbers))
	for _, n := range numbers {
		nodes = append(nodes, fmt.Sprintf(`{"number":%d,"title":"issue %d","state":"OPEN","createdAt":"2026-01-01T00:00:00Z","comments":{"totalCount":0,"nodes":[]},"reactions":{"totalCount":0,"nodes":[]}}`, n, n))
	}
	cursor := "null"
	if endCursor != "" {
		cursor = strconv.Quote(endCursor)
	}
	body := fmt.Sprintf(`{"data":{"repository":{"issues":{"totalCount":3,"pageInfo":{"endCursor":%s},"nodes":[%s]}},"rateLimit":{"limit":5000,"cost":1,"remaining":4000,"resetAt":"2026-01-01T00:00:00Z"}}}`, cursor, strings.Join(nodes, ","))
	return cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "4000"}, body}
}

func TestGithubGraphQLQuotaRunsOutMidRun(t *testing.T) {
	logs := captureOutput(t)
	f := &fakeGitHub{graphql: []cannedResponse{
		issuesPage("c1", 1, 2),
		graphQLQuotaSpent(),
		issuesPage("c2", 3),
		issuesPage(""),
	}}
	s, _ := connectToFake(t, f)

	rows, err := readTable(t, s, context.Background(), "issues")
	if err != nil {
		t.Fatalf("issues read should survive the rate limit, got: %v", err)
	}
	if rows != 3 {
		t.Fatalf("rows=%d, want all 3 issues", rows)
	}
	wantCursors := []string{"POST ", "POST c1", "POST c1", "POST c2"}
	if strings.Join(f.cursors, "|") != strings.Join(wantCursors, "|") {
		t.Fatalf("graphql requests=%q, want %q (the rate-limited page retried with the same cursor)", f.cursors, wantCursors)
	}
	for _, h := range f.authHeaders {
		if h != "Bearer test-token" {
			t.Fatalf("retried request lost auth: %q", h)
		}
	}
	if !strings.Contains(logs.String(), "Warning: GitHub rate limit exceeded for o/r, waiting 10ms before retrying") {
		t.Fatalf("missing rate-limit warning, output: %q", logs.String())
	}
}

// A query costing more than the points left gets RATE_LIMITED while x-ratelimit-remaining stays
// above zero; it must still be retried. TestGithubRateLimitWait checks that it waits for the reset.
func TestGithubGraphQLCostAboveRemainingIsRetried(t *testing.T) {
	logs := captureOutput(t)
	f := &fakeGitHub{graphql: []cannedResponse{
		issuesPage("c1", 1, 2),
		{200, map[string]string{"X-RateLimit-Remaining": "98", "X-RateLimit-Reset": epoch(time.Now().Add(-time.Minute))}, liveGraphQLCostAboveRemainingBody},
		issuesPage("c2", 3),
		issuesPage(""),
	}}
	s, _ := connectToFake(t, f)

	rows, err := readTable(t, s, context.Background(), "issues")
	if err != nil || rows != 3 {
		t.Fatalf("rows=%d err=%v, want all 3 issues after the wait", rows, err)
	}
	if !strings.Contains(logs.String(), "Warning: GitHub rate limit exceeded for o/r") {
		t.Fatalf("missing rate-limit warning, output: %q", logs.String())
	}
}

func TestGithubGraphQLSecondaryLimit403(t *testing.T) {
	captureOutput(t)
	f := &fakeGitHub{graphql: []cannedResponse{
		{403, map[string]string{"Retry-After": "0", "X-RateLimit-Remaining": "4000"}, `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`},
		issuesPage("c1", 1),
		issuesPage(""),
	}}
	s, _ := connectToFake(t, f)

	rows, err := readTable(t, s, context.Background(), "issues")
	if err != nil || rows != 1 {
		t.Fatalf("rows=%d err=%v, want 1 row after the secondary-limit wait", rows, err)
	}
	if len(f.cursors) != 3 {
		t.Fatalf("graphql calls=%d, want 3", len(f.cursors))
	}
}

func TestGithubGraphQLServerErrorIsRetried(t *testing.T) {
	captureOutput(t)
	f := &fakeGitHub{graphql: []cannedResponse{
		{502, nil, `{"message":"Server Error"}`},
		issuesPage("c1", 1),
		issuesPage(""),
	}}
	s, _ := connectToFake(t, f)

	rows, err := readTable(t, s, context.Background(), "issues")
	if err != nil || rows != 1 {
		t.Fatalf("rows=%d err=%v, want the 502 retried like REST requests are", rows, err)
	}
}

func TestGithubGraphQLOtherErrorsAreNotRetried(t *testing.T) {
	captureOutput(t)
	f := &fakeGitHub{graphql: []cannedResponse{
		{200, nil, `{"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository with the name 'o/r'."}]}`},
	}}
	s, _ := connectToFake(t, f)

	_, err := readTable(t, s, context.Background(), "issues")
	if err == nil || !strings.Contains(err.Error(), "Could not resolve to a Repository") {
		t.Fatalf("expected the NOT_FOUND error, got: %v", err)
	}
	if len(f.cursors) != 1 {
		t.Fatalf("graphql calls=%d, want 1", len(f.cursors))
	}
}

func TestGithubRESTQuotaRunsOutMidRun(t *testing.T) {
	logs := captureOutput(t)
	f := &fakeGitHub{}
	s, srv := connectToFake(t, f)
	page2 := srv.URL + "/repos/o/r/commits?page=2&per_page=100"
	f.rest = []cannedResponse{
		repoOR,
		{200, map[string]string{"Link": "<" + page2 + `>; rel="next"`}, `[{"sha":"a","commit":{"committer":{"date":"2026-01-02T00:00:00Z"}}}]`},
		restQuotaSpent(),
		{200, nil, `[{"sha":"b","commit":{"committer":{"date":"2026-01-01T00:00:00Z"}}}]`},
	}

	rows, err := readTable(t, s, context.Background(), "commits")
	if err != nil || rows != 2 {
		t.Fatalf("rows=%d err=%v, want both commits", rows, err)
	}
	if len(f.restPaths) != 4 || f.restPaths[2] != f.restPaths[3] {
		t.Fatalf("rest requests=%q, want page 2 retried", f.restPaths)
	}
	if !strings.Contains(logs.String(), "Warning: GitHub rate limit exceeded for o/r") {
		t.Fatalf("missing rate-limit warning, output: %q", logs.String())
	}
}

func TestGithubPermissionDeniedIsNotRetried(t *testing.T) {
	captureOutput(t)
	f := &fakeGitHub{rest: []cannedResponse{
		{403, map[string]string{"X-RateLimit-Remaining": "4999"}, `{"message":"Resource not accessible by personal access token"}`},
	}}
	s, _ := connectToFake(t, f)

	if _, err := readTable(t, s, context.Background(), "commits"); err == nil {
		t.Fatal("expected permission error")
	}
	if len(f.restPaths) != 1 {
		t.Fatalf("permission 403 should not be retried, got %d calls", len(f.restPaths))
	}
}

func TestGithubRateLimitWaitCanBeCancelled(t *testing.T) {
	captureOutput(t)
	f := &fakeGitHub{graphql: []cannedResponse{
		{200, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(time.Now().Add(30 * time.Minute))}, liveGraphQLRateLimitBody},
	}}
	s, _ := connectToFake(t, f)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := readTable(t, s, ctx, "issues"); err == nil {
		t.Fatal("expected cancellation error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancel should interrupt the 30m rate-limit wait, took %v", elapsed)
	}
}

func TestGithubRateLimitRetriesRunOut(t *testing.T) {
	logs := captureOutput(t)
	f := &fakeGitHub{graphql: []cannedResponse{graphQLQuotaSpent()}}
	s, _ := connectToFake(t, f)

	_, err := readTable(t, s, context.Background(), "issues")
	if err == nil || !strings.Contains(err.Error(), "API rate limit already exceeded") {
		t.Fatalf("expected GitHub's rate-limit error once retries run out, got: %v", err)
	}
	if len(f.cursors) != retryCount+1 {
		t.Fatalf("graphql calls=%d, want the first attempt plus %d retries", len(f.cursors), retryCount)
	}
	if got := strings.Count(logs.String(), "GitHub rate limit exceeded"); got != retryCount {
		t.Fatalf("warnings=%d, want one per wait (%d)", got, retryCount)
	}
}

// resty would wait out a 429 with Retry-After itself, skipping the strategy's minimum; the wait
// must still go through clampWait (raised to 100ms here) and be logged.
func TestGithubRetryAfter429KeepsTheMinimumWait(t *testing.T) {
	logs := captureOutput(t)
	f := &fakeGitHub{rest: []cannedResponse{
		repoOR,
		{429, map[string]string{"Retry-After": "0"}, `{"message":"You have exceeded a secondary rate limit."}`},
		{200, nil, `[{"sha":"a","commit":{"committer":{"date":"2026-01-01T00:00:00Z"}}}]`},
	}}
	s, _ := connectToFake(t, f)
	retryWait = 100 * time.Millisecond

	start := time.Now()
	rows, err := readTable(t, s, context.Background(), "commits")
	if err != nil || rows != 1 {
		t.Fatalf("rows=%d err=%v, want 1 row after the wait", rows, err)
	}
	if elapsed := time.Since(start); elapsed < retryWait {
		t.Fatalf("retried after %v, want at least %v", elapsed, retryWait)
	}
	if !strings.Contains(logs.String(), "Warning: GitHub rate limit exceeded for o/r, waiting 100ms before retrying") {
		t.Fatalf("missing rate-limit warning for the 429, output: %q", logs.String())
	}
}

func TestGithubRetryAfter503IsLogged(t *testing.T) {
	logs := captureOutput(t)
	f := &fakeGitHub{rest: []cannedResponse{
		repoOR,
		{503, map[string]string{"Retry-After": "0"}, `{"message":"Service unavailable"}`},
		{200, nil, `[{"sha":"a","commit":{"committer":{"date":"2026-01-01T00:00:00Z"}}}]`},
	}}
	s, _ := connectToFake(t, f)

	rows, err := readTable(t, s, context.Background(), "commits")
	if err != nil || rows != 1 {
		t.Fatalf("rows=%d err=%v, want 1 row after the wait", rows, err)
	}
	if !strings.Contains(logs.String(), "Warning: GitHub answered 503 for o/r and asked to retry later, waiting 1ms before retrying") {
		t.Fatalf("missing warning for the 503, output: %q", logs.String())
	}
}

func TestGithubGraphQLTablesNeedAToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected, got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	prevREST, prevGraphQL := restApiBaseUrl, graphqlBaseUrl
	restApiBaseUrl, graphqlBaseUrl = srv.URL, srv.URL+"/graphql"
	t.Cleanup(func() { restApiBaseUrl, graphqlBaseUrl = prevREST, prevGraphQL })

	s := NewGitHubSource()
	if err := s.Connect(context.Background(), "github://?owner=o&repo=r"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	for _, table := range []string{"issues", "pull_requests", "stargazers"} {
		if _, err := readTable(t, s, context.Background(), table); err == nil || !strings.Contains(err.Error(), "needs an access_token") {
			t.Errorf("%s without a token: got %v, want an access_token error", table, err)
		}
	}
}

// Reactions are best effort, but a failure to fetch them must be visible.
func TestGithubReactionFailureIsWarned(t *testing.T) {
	logs := captureOutput(t)
	pageWithReactedComment := cannedResponse{
		200,
		map[string]string{"X-RateLimit-Remaining": "4000"},
		`{"data":{"repository":{"issues":{"totalCount":1,"pageInfo":{"endCursor":"c1"},"nodes":[` +
			`{"number":1,"title":"issue 1","state":"OPEN","createdAt":"2026-01-01T00:00:00Z",` +
			`"comments":{"totalCount":1,"nodes":[{"id":"IC_1","body":"hi","reactionGroups":[{"content":"THUMBS_UP","users":{"totalCount":1}}]}]},` +
			`"reactions":{"totalCount":0,"nodes":[]}}]}}}}`,
	}
	responses := []cannedResponse{pageWithReactedComment}
	for range retryCount + 1 {
		responses = append(responses, graphQLQuotaSpent())
	}
	f := &fakeGitHub{graphql: append(responses, issuesPage(""))}
	s, _ := connectToFake(t, f)

	rows, err := readTable(t, s, context.Background(), "issues")
	if err != nil || rows != 1 {
		t.Fatalf("rows=%d err=%v, want the issue written without reactions", rows, err)
	}
	if !strings.Contains(logs.String(), "Warning: could not fetch comment reactions for o/r") {
		t.Fatalf("missing reactions warning, output: %q", logs.String())
	}
}

// Without Retry-After or a spent quota, GitHub asks to wait at least a minute and longer each
// time the secondary limit persists (10ms in tests).
func TestGithubSecondaryLimitWaitDoubles(t *testing.T) {
	logs := captureOutput(t)
	secondary := cannedResponse{403, nil, `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`}
	f := &fakeGitHub{rest: []cannedResponse{
		repoOR,
		secondary, secondary, secondary,
		{200, nil, `[{"sha":"a","commit":{"committer":{"date":"2026-01-01T00:00:00Z"}}}]`},
	}}
	s, _ := connectToFake(t, f)

	rows, err := readTable(t, s, context.Background(), "commits")
	if err != nil || rows != 1 {
		t.Fatalf("rows=%d err=%v, want 1 row after the waits", rows, err)
	}
	var got []string
	for _, m := range regexp.MustCompile(`waiting (\S+) before retrying`).FindAllStringSubmatch(logs.String(), -1) {
		got = append(got, m[1])
	}
	if strings.Join(got, ",") != "10ms,20ms,40ms" {
		t.Fatalf("waits=%v, want 10ms,20ms,40ms", got)
	}
}

// resty clamps every retry delay to the client's max wait; without retryMaxWait a
// 59-minute rate-limit wait would silently shrink to the 30s default.
func TestGithubClientsAllowHourLongRateLimitWaits(t *testing.T) {
	s, _ := connectToFake(t, &fakeGitHub{})
	for name, c := range map[string]*ingestrhttp.Client{"rest": s.restClient, "graphql": s.graphqlClient} {
		if got := c.Resty().RetryMaxWaitTime(); got != retryMaxWait {
			t.Errorf("%s client max retry wait=%v, want %v", name, got, retryMaxWait)
		}
		if got := c.Resty().RetryCount(); got != retryCount {
			t.Errorf("%s client retry count=%d, want %d", name, got, retryCount)
		}
	}
}

func TestGithubReadRepositories(t *testing.T) {
	cases := []struct {
		name     string
		ownerTyp string
		apiKey   string
		viewer   string
		wantPath string
		wantQ    map[string]string
	}{
		{name: "organization", ownerTyp: "Organization", apiKey: "tok", wantPath: "/orgs/o/repos", wantQ: map[string]string{"type": "all"}},
		{name: "token user", ownerTyp: "User", apiKey: "tok", viewer: "O", wantPath: "/user/repos", wantQ: map[string]string{"affiliation": "owner", "visibility": "all"}},
		{name: "other user", ownerTyp: "User", apiKey: "tok", viewer: "someone", wantPath: "/users/o/repos", wantQ: map[string]string{"type": "owner"}},
		{name: "unauthenticated user", ownerTyp: "User", wantPath: "/users/o/repos", wantQ: map[string]string{"type": "owner"}},
		{name: "bot", ownerTyp: "Bot", wantPath: "/users/o/repos", wantQ: map[string]string{"type": "owner"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/users/o":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"login": "o", "type": tc.ownerTyp})
					return
				case "/user":
					if tc.apiKey == "" {
						t.Errorf("unexpected /user call without a token")
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"login": tc.viewer})
					return
				case tc.wantPath:
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				q := r.URL.Query()
				if q.Get("page") == "" {
					for k, v := range tc.wantQ {
						if got := q.Get(k); got != v {
							t.Errorf("%s=%q want %q", k, got, v)
						}
					}
					w.Header().Set("Link", "<"+srv.URL+tc.wantPath+"?page=2>; rel=\"next\"")
					_ = json.NewEncoder(w).Encode([]map[string]interface{}{
						{"id": 1, "name": "a", "private": false, "owner": map[string]interface{}{"login": "o"}},
						{"id": 2, "name": "b", "private": true, "owner": map[string]interface{}{"login": "o"}},
					})
					return
				}
				_ = json.NewEncoder(w).Encode([]map[string]interface{}{
					{"id": 3, "name": "c", "private": false, "owner": map[string]interface{}{"login": "o"}},
				})
			}))
			defer srv.Close()

			s := &GithubSource{
				owner:      "o",
				apiKey:     tc.apiKey,
				restClient: ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL)),
			}
			results := make(chan source.RecordBatchResult, 8)
			go func() {
				defer close(results)
				if err := s.readRepositories(context.Background(), source.ReadOptions{}, results); err != nil {
					results <- source.RecordBatchResult{Err: err}
				}
			}()

			var rows int64
			for res := range results {
				if res.Err != nil {
					t.Fatal(res.Err)
				}
				for _, col := range []string{"id", "name", "private", "owner"} {
					if len(res.Batch.Schema().FieldIndices(col)) == 0 {
						t.Fatalf("missing %s column: %s", col, res.Batch.Schema())
					}
				}
				if typ := res.Batch.Schema().Field(res.Batch.Schema().FieldIndices("id")[0]).Type.Name(); typ != "int64" {
					t.Fatalf("id type=%s want int64", typ)
				}
				rows += res.Batch.NumRows()
				res.Batch.Release()
			}
			if rows != 3 {
				t.Fatalf("rows=%d want 3", rows)
			}
		})
	}
}

func TestGithubOwnerOnlyURI(t *testing.T) {
	s := NewGitHubSource()
	if err := s.Connect(context.Background(), "github://?owner=o"); err != nil {
		t.Fatalf("owner-only URI should connect: %v", err)
	}
	defer func() { _ = s.Close(context.Background()) }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "repositories"})
	if err != nil {
		t.Fatalf("repositories should not need repo: %v", err)
	}
	if pks := table.PrimaryKeys(); len(pks) != 1 || pks[0] != "id" {
		t.Fatalf("primary keys=%v want [id]", pks)
	}
	if table.Strategy() != config.StrategyReplace || table.IncrementalKey() != "" {
		t.Fatalf("strategy=%q incremental key=%q, want replace without an incremental key", table.Strategy(), table.IncrementalKey())
	}

	if _, err := s.GetTable(context.Background(), source.TableRequest{Name: "issues"}); err == nil || !strings.Contains(err.Error(), "table issues needs owner and repo in the URI") {
		t.Fatalf("issues without repo should fail with a repo error, got %v", err)
	}
}

func TestGithubReadRepositoriesInstallationToken(t *testing.T) {
	repo := func(id int, name, owner string, private bool) map[string]interface{} {
		return map[string]interface{}{"id": id, "name": name, "private": private, "owner": map[string]interface{}{"login": owner}}
	}

	cases := []struct {
		name         string
		installation bool
		wantIDs      []int64
	}{
		{name: "installation token adds private repos once", installation: true, wantIDs: []int64{1, 2, 3}},
		{name: "token without user or installation stays public", installation: false, wantIDs: []int64{1, 2}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				q := r.URL.Query()
				switch r.URL.Path {
				case "/users/o":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"login": "o", "type": "User"})
				case "/user":
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
				case "/users/o/repos":
					if got := q.Get("type"); got != "owner" {
						t.Errorf("type=%q want owner", got)
					}
					_ = json.NewEncoder(w).Encode([]map[string]interface{}{repo(1, "a", "o", false), repo(2, "shared", "o", false)})
				case "/installation/repositories":
					if !tc.installation {
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte(`{"message":"You must authenticate with an installation access token"}`))
						return
					}
					if q.Get("page") == "" {
						if q.Get("per_page") != "1" {
							w.Header().Set("Link", "<"+srv.URL+"/installation/repositories?page=2>; rel=\"next\"")
						}
						_ = json.NewEncoder(w).Encode(map[string]interface{}{
							"total_count":  3,
							"repositories": []map[string]interface{}{repo(2, "shared", "o", false), repo(3, "secret", "O", true)},
						})
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"total_count":  3,
						"repositories": []map[string]interface{}{repo(4, "foreign", "someone-else", true)},
					})
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			s := &GithubSource{
				owner:      "o",
				apiKey:     "ghs_x",
				restClient: ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL)),
			}
			results := make(chan source.RecordBatchResult, 8)
			go func() {
				defer close(results)
				if err := s.readRepositories(context.Background(), source.ReadOptions{}, results); err != nil {
					results <- source.RecordBatchResult{Err: err}
				}
			}()

			var ids []int64
			for res := range results {
				if res.Err != nil {
					t.Fatal(res.Err)
				}
				col := res.Batch.Column(res.Batch.Schema().FieldIndices("id")[0]).(*array.Int64)
				for i := 0; i < col.Len(); i++ {
					ids = append(ids, col.Value(i))
				}
				res.Batch.Release()
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			if fmt.Sprint(ids) != fmt.Sprint(tc.wantIDs) {
				t.Fatalf("ids=%v want %v", ids, tc.wantIDs)
			}
		})
	}
}

func repoJSON(id int, owner string, private bool) string {
	return fmt.Sprintf(`{"id":%d,"node_id":"R%d","name":"r%d","full_name":"%s/r%d","owner":{"login":%q},"private":%t,"fork":false,"html_url":"https://github.com/%s/r%d","description":null}`, id, id, id, owner, id, owner, private, owner, id)
}

func repoList(repos ...string) string { return "[" + strings.Join(repos, ",") + "]" }

func connectOwnerToFake(t *testing.T, f *fakeGitHub) (*GithubSource, *httptest.Server) {
	t.Helper()
	s, srv := connectToFake(t, f)
	s.repo = ""
	return s, srv
}

func readRepositoryIDs(t *testing.T, s *GithubSource, opts source.ReadOptions) ([]int64, []string, error) {
	t.Helper()
	results, err := s.read(context.Background(), "repositories", nil, opts)
	if err != nil {
		return nil, nil, err
	}
	var ids []int64
	var columns []string
	var readErr error
	for res := range results {
		if res.Err != nil {
			readErr = res.Err
			continue
		}
		if columns == nil {
			for _, f := range res.Batch.Schema().Fields() {
				columns = append(columns, f.Name)
			}
		}
		col := res.Batch.Column(res.Batch.Schema().FieldIndices("id")[0]).(*array.Int64)
		for i := 0; i < col.Len(); i++ {
			ids = append(ids, col.Value(i))
		}
		res.Batch.Release()
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, columns, readErr
}

var (
	ownerIsUser    = cannedResponse{200, nil, `{"login":"o","type":"User"}`}
	tokenUserIsO   = cannedResponse{200, nil, `{"login":"o"}`}
	noUserForToken = cannedResponse{403, nil, `{"message":"Resource not accessible by integration"}`}
)

func TestGithubRepositoriesQuotaRunsOutMidRun(t *testing.T) {
	logs := captureOutput(t)
	f := &fakeGitHub{}
	s, srv := connectOwnerToFake(t, f)
	f.rest = []cannedResponse{
		ownerIsUser,
		tokenUserIsO,
		{200, map[string]string{"Link": "<" + srv.URL + `/user/repos?page=2>; rel="next"`}, repoList(repoJSON(1, "o", false))},
		restQuotaSpent(),
		{200, nil, repoList(repoJSON(2, "o", true))},
	}

	ids, _, err := readRepositoryIDs(t, s, source.ReadOptions{})
	if err != nil || fmt.Sprint(ids) != "[1 2]" {
		t.Fatalf("ids=%v err=%v, want both repositories", ids, err)
	}
	if len(f.restPaths) != 5 || f.restPaths[3] != f.restPaths[4] {
		t.Fatalf("rest requests=%q, want page 2 retried", f.restPaths)
	}
	if !strings.Contains(logs.String(), "Warning: GitHub rate limit exceeded for o, waiting") {
		t.Fatalf("warning should name the owner alone, output: %q", logs.String())
	}
}

func TestGithubRepositoriesUserLookupFailureStopsTheRun(t *testing.T) {
	cases := []struct {
		name string
		user cannedResponse
	}{
		{"server keeps failing", cannedResponse{502, nil, `{"message":"Bad Gateway"}`}},
		{"secondary limit never lifts", cannedResponse{403, nil, `{"message":"You have exceeded a secondary rate limit"}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureOutput(t)
			f := &fakeGitHub{}
			s, _ := connectOwnerToFake(t, f)
			f.rest = []cannedResponse{ownerIsUser, tc.user}

			ids, _, err := readRepositoryIDs(t, s, source.ReadOptions{})
			if err == nil || !strings.Contains(err.Error(), "failed to resolve the authenticated github user") {
				t.Fatalf("ids=%v err=%v, want the run to fail instead of listing public repositories", ids, err)
			}
			for _, p := range f.restPaths {
				if strings.Contains(p, "/repos") {
					t.Fatalf("listed repositories after /user failed: %q", f.restPaths)
				}
			}
		})
	}
}

func TestGithubRepositoriesInstallationProbeFailureStopsTheRun(t *testing.T) {
	captureOutput(t)
	f := &fakeGitHub{}
	s, _ := connectOwnerToFake(t, f)
	f.rest = []cannedResponse{ownerIsUser, noUserForToken, {502, nil, `{"message":"Bad Gateway"}`}}

	_, _, err := readRepositoryIDs(t, s, source.ReadOptions{})
	if err == nil || !strings.Contains(err.Error(), "failed to probe github installation repositories") {
		t.Fatalf("err=%v, want the probe failure to stop the run", err)
	}
}

func TestGithubRepositoriesAppTokenIsNotRetried(t *testing.T) {
	logs := captureOutput(t)
	f := &fakeGitHub{}
	s, _ := connectOwnerToFake(t, f)
	f.rest = []cannedResponse{
		ownerIsUser,
		noUserForToken,
		{200, nil, `{"total_count":2,"repositories":[` + repoJSON(2, "o", false) + `]}`},
		{200, nil, repoList(repoJSON(1, "o", false), repoJSON(2, "o", false))},
		{200, nil, `{"total_count":2,"repositories":[` + repoJSON(2, "o", false) + "," + repoJSON(3, "o", true) + `]}`},
	}

	ids, _, err := readRepositoryIDs(t, s, source.ReadOptions{})
	if err != nil || fmt.Sprint(ids) != "[1 2 3]" {
		t.Fatalf("ids=%v err=%v, want public and installation repositories once each", ids, err)
	}
	want := []string{"/users/o", "/user", "/installation/repositories?per_page=1", "/users/o/repos?per_page=100&type=owner", "/installation/repositories?per_page=100"}
	if fmt.Sprint(f.restPaths) != fmt.Sprint(want) {
		t.Fatalf("rest requests=%q, want %q", f.restPaths, want)
	}
	if strings.Contains(logs.String(), "Warning") {
		t.Fatalf("app token path should not warn, output: %q", logs.String())
	}
}

func TestGithubRepositoriesTokenWithoutUserOrInstallationWarns(t *testing.T) {
	cases := []struct {
		name  string
		user  cannedResponse
		probe cannedResponse
	}{
		{"403", noUserForToken, cannedResponse{403, nil, `{"message":"You must authenticate with an installation access token in order to list repositories for an installation."}`}},
		{"401", cannedResponse{401, nil, `{"message":"Bad credentials"}`}, cannedResponse{401, nil, `{"message":"Bad credentials"}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureOutput(t)
			f := &fakeGitHub{}
			s, _ := connectOwnerToFake(t, f)
			f.rest = []cannedResponse{ownerIsUser, tc.user, tc.probe, {200, nil, repoList(repoJSON(1, "o", false))}}

			ids, _, err := readRepositoryIDs(t, s, source.ReadOptions{})
			if err != nil || fmt.Sprint(ids) != "[1]" {
				t.Fatalf("ids=%v err=%v, want the public repository", ids, err)
			}
			if !strings.Contains(logs.String(), "listing only the public repositories of o") {
				t.Fatalf("missing public-only warning, output: %q", logs.String())
			}
		})
	}
}

func TestGithubRepositoriesUnknownOwnerFails(t *testing.T) {
	captureOutput(t)
	f := &fakeGitHub{}
	s, _ := connectOwnerToFake(t, f)
	f.rest = []cannedResponse{{404, nil, `{"message":"Not Found"}`}}

	_, _, err := readRepositoryIDs(t, s, source.ReadOptions{})
	if err == nil || !strings.Contains(err.Error(), "failed to look up github owner o with status 404") {
		t.Fatalf("err=%v, want the owner lookup to fail", err)
	}
	if len(f.restPaths) != 1 {
		t.Fatalf("rest requests=%q, want only the owner lookup", f.restPaths)
	}
}

func TestGithubRepositoriesLimitSpansListings(t *testing.T) {
	captureOutput(t)
	f := &fakeGitHub{}
	s, _ := connectOwnerToFake(t, f)
	f.rest = []cannedResponse{
		ownerIsUser,
		noUserForToken,
		{200, nil, `{"total_count":1,"repositories":[` + repoJSON(2, "o", true) + `]}`},
		{200, nil, repoList(repoJSON(1, "o", false))},
		{200, nil, `{"total_count":1,"repositories":[` + repoJSON(2, "o", true) + `]}`},
	}

	ids, _, err := readRepositoryIDs(t, s, source.ReadOptions{Limit: 1})
	if err != nil || fmt.Sprint(ids) != "[1]" {
		t.Fatalf("ids=%v err=%v, want exactly one row", ids, err)
	}
}

func TestGithubRepositoriesKeepsDeclaredColumnsWhenNull(t *testing.T) {
	captureOutput(t)
	f := &fakeGitHub{}
	s, _ := connectOwnerToFake(t, f)
	f.rest = []cannedResponse{ownerIsUser, tokenUserIsO, {200, nil, `[{"id":1}]`}}

	_, columns, err := readRepositoryIDs(t, s, source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"id", "node_id", "name", "full_name", "owner", "private", "fork", "html_url", "description"}
	got := "," + strings.Join(columns, ",") + ","
	for _, c := range want {
		if !strings.Contains(got, ","+c+",") {
			t.Fatalf("column %q missing from %q", c, columns)
		}
	}

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "repositories"})
	if err != nil {
		t.Fatal(err)
	}
	sch, err := table.GetSchema(context.Background())
	if err != nil || len(sch.Columns) != len(want) {
		t.Fatalf("schema=%v err=%v, want the %d declared columns for an owner without repositories", sch, err, len(want))
	}
}

// commitsFake serves organizations and users whose repository listings span two pages, per-repo
// lookups (case-insensitive, with renames answered by a redirect to /repositories/{id}), and
// per-repository commit lists keyed by GitHub's owner/name. It records every request path.
type commitsFake struct {
	t       *testing.T
	mu      sync.Mutex
	paths   []string
	orgs    map[string][]map[string]interface{}
	users   map[string][]map[string]interface{}
	hidden  []map[string]interface{}
	renamed map[string]int
	lookup  map[string]int
	blocked map[string]int
	commits map[string][]string
	empty   map[string]bool
}

func listedRepo(id int, owner, name string, fork, archived bool) map[string]interface{} {
	return map[string]interface{}{"id": id, "name": name, "full_name": owner + "/" + name, "fork": fork, "archived": archived, "owner": map[string]interface{}{"login": owner}}
}

func (f *commitsFake) findRepo(match func(map[string]interface{}) bool) map[string]interface{} {
	for _, group := range []map[string][]map[string]interface{}{f.orgs, f.users, {"": f.hidden}} {
		for _, repos := range group {
			for _, r := range repos {
				if match(r) {
					return r
				}
			}
		}
	}
	return nil
}

func writeStatus(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"message":"status %d","status":"%d"}`, status, status)
}

func (f *commitsFake) serveListing(w http.ResponseWriter, r *http.Request, repos []map[string]interface{}) {
	half := len(repos) / 2
	if r.URL.Query().Get("page") == "" {
		w.Header().Set("Link", "<"+restApiBaseUrl+r.URL.Path+"?page=2>; rel=\"next\"")
		_ = json.NewEncoder(w).Encode(repos[:half])
		return
	}
	_ = json.NewEncoder(w).Encode(repos[half:])
}

func (f *commitsFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.paths = append(f.paths, r.URL.Path)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")

	switch {
	case r.URL.Path == "/user":
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"login": "me"})
	case len(parts) == 2 && parts[0] == "users":
		owner := strings.ToLower(parts[1])
		switch {
		case f.orgs[owner] != nil:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"login": owner, "type": "Organization"})
		case f.users[owner] != nil:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"login": owner, "type": "User"})
		default:
			writeStatus(w, http.StatusNotFound)
		}
	case len(parts) == 3 && parts[0] == "orgs" && parts[2] == "repos":
		f.serveListing(w, r, f.orgs[strings.ToLower(parts[1])])
	case len(parts) == 3 && parts[0] == "users" && parts[2] == "repos":
		if got := r.URL.Query().Get("type"); got != "owner" && r.URL.Query().Get("page") == "" {
			f.t.Errorf("user listing type=%q want owner", got)
		}
		f.serveListing(w, r, f.users[strings.ToLower(parts[1])])
	case len(parts) == 2 && parts[0] == "repositories":
		id, _ := strconv.Atoi(parts[1])
		if repo := f.findRepo(func(r map[string]interface{}) bool { return r["id"] == id }); repo != nil {
			_ = json.NewEncoder(w).Encode(repo)
			return
		}
		writeStatus(w, http.StatusNotFound)
	case len(parts) == 3 && parts[0] == "repos":
		key := strings.ToLower(parts[1] + "/" + parts[2])
		if status, ok := f.lookup[key]; ok {
			writeStatus(w, status)
			return
		}
		if id, ok := f.renamed[key]; ok {
			w.Header().Set("Location", restApiBaseUrl+"/repositories/"+strconv.Itoa(id))
			writeStatus(w, http.StatusMovedPermanently)
			return
		}
		if repo := f.findRepo(func(r map[string]interface{}) bool { return strings.EqualFold(r["full_name"].(string), key) }); repo != nil {
			_ = json.NewEncoder(w).Encode(repo)
			return
		}
		writeStatus(w, http.StatusNotFound)
	case len(parts) == 4 && parts[0] == "repos" && parts[3] == "commits":
		fullName := parts[1] + "/" + parts[2]
		if status, ok := f.blocked[fullName]; ok {
			writeStatus(w, status)
			return
		}
		if f.empty[fullName] {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"Git Repository is empty.","status":"409"}`))
			return
		}
		shas, ok := f.commits[fullName]
		if !ok {
			f.t.Errorf("unexpected commits request for %s", fullName)
			writeStatus(w, http.StatusNotFound)
			return
		}
		page := make([]map[string]interface{}, 0, len(shas))
		for _, sha := range shas {
			page = append(page, map[string]interface{}{
				"sha":    sha,
				"commit": map[string]interface{}{"committer": map[string]interface{}{"date": "2026-11-02T10:00:00Z"}},
			})
		}
		_ = json.NewEncoder(w).Encode(page)
	default:
		f.t.Errorf("unexpected path %s", r.URL.Path)
		writeStatus(w, http.StatusNotFound)
	}
}

func (f *commitsFake) requests(match func(string) bool) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, p := range f.paths {
		if match(p) {
			out = append(out, p)
		}
	}
	return out
}

func (f *commitsFake) commitRequests() []string {
	return f.requests(func(p string) bool { return strings.HasSuffix(p, "/commits") })
}

func (f *commitsFake) listingRequests() []string {
	return f.requests(func(p string) bool {
		return strings.HasPrefix(p, "/users") || strings.HasPrefix(p, "/user") || strings.HasPrefix(p, "/orgs")
	})
}

func newCommitsFake(t *testing.T) *commitsFake {
	return &commitsFake{
		t: t,
		orgs: map[string][]map[string]interface{}{
			"o": {
				listedRepo(1, "o", "r", false, false),
				listedRepo(2, "o", "ingestr", false, false),
				listedRepo(3, "o", "old", false, true),
				listedRepo(4, "o", "forked", true, false),
				listedRepo(5, "o", "Bruin", false, false),
				listedRepo(2, "o", "ingestr", false, false),
			},
			"other": {
				listedRepo(10, "other", "tool", false, false),
				listedRepo(11, "other", "Lib", true, false),
			},
		},
		users: map[string][]map[string]interface{}{
			"u": {
				listedRepo(20, "u", "app", false, false),
				listedRepo(21, "u", "fork", true, false),
			},
		},
		hidden:  []map[string]interface{}{listedRepo(30, "u", "secret", false, false)},
		renamed: map[string]int{},
		lookup:  map[string]int{},
		blocked: map[string]int{},
		commits: map[string][]string{
			"o/r":        {"r1"},
			"o/ingestr":  {"i1", "i2"},
			"o/old":      {"o1"},
			"o/forked":   {"f1"},
			"o/Bruin":    {"b1", "b2"},
			"other/tool": {"t1"},
			"other/Lib":  {"l1"},
			"u/app":      {"a1"},
			"u/secret":   {"s1"},
		},
		empty: map[string]bool{},
	}
}

type commitRow struct{ repo, sha string }

func readCommitRows(t *testing.T, s *GithubSource, tableName string, opts source.ReadOptions) (source.SourceTable, []commitRow, int, error) {
	t.Helper()
	table, err := s.GetTable(context.Background(), source.TableRequest{Name: tableName})
	if err != nil {
		return nil, nil, 0, err
	}
	results, err := table.Read(context.Background(), opts)
	if err != nil {
		return table, nil, 0, err
	}
	var rows []commitRow
	batches := 0
	var readErr error
	for res := range results {
		if res.Err != nil {
			readErr = res.Err
			continue
		}
		batches++
		repoCol := res.Batch.Column(res.Batch.Schema().FieldIndices("repo")[0]).(*array.String)
		shaCol := res.Batch.Column(res.Batch.Schema().FieldIndices("sha")[0]).(*array.String)
		for i := 0; i < int(res.Batch.NumRows()); i++ {
			rows = append(rows, commitRow{repoCol.Value(i), shaCol.Value(i)})
		}
		res.Batch.Release()
	}
	return table, rows, batches, readErr
}

func TestGithubCommitsTableName(t *testing.T) {
	cases := []struct {
		name    string
		owner   string
		base    string
		repos   []string
		owners  []string
		wantErr string
	}{
		{name: "commits", owner: "o", base: "commits"},
		{name: "commits", base: "commits"},
		{name: "commits:a", owner: "o", base: "commits", repos: []string{"o/a"}},
		{name: "commits:agent-runner, bruin,  ingestr ", owner: "o", base: "commits", repos: []string{"o/agent-runner", "o/bruin", "o/ingestr"}},
		{name: "commits:a,,b, ,", owner: "o", base: "commits", repos: []string{"o/a", "o/b"}},
		{name: "commits:Bruin, bruin,BRUIN, a", owner: "o", base: "commits", repos: []string{"o/Bruin", "o/a"}},
		{name: "commits:bruin, o/Bruin, O/bruin", owner: "o", base: "commits", repos: []string{"o/bruin"}},
		{name: "commits:other/tool, x , other / lib,Other/Tool", owner: "o", base: "commits", repos: []string{"other/tool", "o/x", "other/lib"}},
		{name: "commits:o/*", owner: "o", base: "commits", owners: []string{"o"}},
		{name: "commits:O/*, o/*, o / * ", base: "commits", owners: []string{"O"}},
		{name: "commits:u/*, o/x, other/*, bruin, U/*", owner: "o", base: "commits", repos: []string{"o/x", "o/bruin"}, owners: []string{"u", "other"}},
		{name: "commits:o/x, u/*", base: "commits", repos: []string{"o/x"}, owners: []string{"u"}},
		{name: "commits:", owner: "o", wantErr: "lists no repositories"},
		{name: "commits: , ", wantErr: "lists no repositories"},
		{name: "commits:a, x/b/c", owner: "o", wantErr: `invalid repository "x/b/c" in table commits:a, x/b/c: use <repo> for a repository of o, <owner>/<repo>, or <owner>/* for every repository of an owner`},
		{name: "commits:a/b/c", wantErr: `invalid repository "a/b/c" in table commits:a/b/c: use <owner>/<repo>, or <owner>/* for every repository of an owner`},
		{name: "commits:/b", owner: "o", wantErr: `invalid repository "/b"`},
		{name: "commits:/*", owner: "o", wantErr: `invalid repository "/*"`},
		{name: "commits:o/*/x", owner: "o", wantErr: `invalid repository "o/*/x"`},
		{name: "commits:x/ ", owner: "o", wantErr: `invalid repository "x/" in table commits:x/ : use x/* for every repository of x, or x/<repo>`},
		{name: "commits:x/", wantErr: `invalid repository "x/" in table commits:x/: use x/* for every repository of x, or x/<repo>`},
		{name: "commits:*", owner: "o", wantErr: `invalid repository "*" in table commits:*: a wildcard can only be <owner>/*, which reads every repository of the owner`},
		{name: "commits:*", wantErr: `invalid repository "*" in table commits:*: a wildcard can only be <owner>/*`},
		{name: "commits:bruin*", owner: "o", wantErr: `invalid repository "bruin*" in table commits:bruin*: a wildcard can only be <owner>/*`},
		{name: "commits:o/bruin-*", owner: "o", wantErr: `invalid repository "o/bruin-*" in table commits:o/bruin-*: a wildcard can only be <owner>/*`},
		{name: "commits:o/**", owner: "o", wantErr: `a wildcard can only be <owner>/*`},
		{name: "commits:*/x", owner: "o", wantErr: `invalid repository "*/x" in table commits:*/x: a wildcard can only be <owner>/*`},
		{name: "commits:o*/x", owner: "o", wantErr: `a wildcard can only be <owner>/*`},
		{name: "commits:*/*", owner: "o", wantErr: `a wildcard can only be <owner>/*`},
		{name: "commits:x", wantErr: `repository "x" in table commits:x has no owner and the URI has no owner: write it as <owner>/x, or use <owner>/* for every repository of an owner`},
		{name: "commits:o/x, y", wantErr: `repository "y" in table commits:o/x, y has no owner`},
		{name: "issues:x", owner: "o", base: "issues:x"},
		{name: "issues:x", base: "issues:x"},
	}
	for _, tc := range cases {
		t.Run(tc.name+" owner="+tc.owner, func(t *testing.T) {
			base, sel, err := parseTableName(tc.name, tc.owner)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if wantList := tc.repos != nil || tc.owners != nil; (sel != nil) != wantList {
				t.Fatalf("selection=%+v, want a selection: %v", sel, wantList)
			}
			var repos, owners []string
			if sel != nil {
				repos, owners = sel.repos, sel.owners
			}
			if base != tc.base || fmt.Sprint(repos) != fmt.Sprint(tc.repos) || fmt.Sprint(owners) != fmt.Sprint(tc.owners) {
				t.Fatalf("base=%q repos=%q owners=%q, want %q %q %q", base, repos, owners, tc.base, tc.repos, tc.owners)
			}
		})
	}

	s := NewGitHubSource()
	if err := s.Connect(context.Background(), "github://?owner=o&repo=r"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close(context.Background()) }()
	for _, name := range []string{"issues:x", "repositories:x", "commits:", "commits:a/b/c"} {
		if _, err := s.GetTable(context.Background(), source.TableRequest{Name: name}); err == nil {
			t.Errorf("GetTable(%q) should fail", name)
		}
	}
}

func TestGithubCommitsSingleRepo(t *testing.T) {
	for _, uriRepo := range []string{"r", "R"} {
		t.Run(uriRepo, func(t *testing.T) {
			captureOutput(t)
			f := newCommitsFake(t)
			s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=O&repo="+uriRepo)

			table, rows, _, err := readCommitRows(t, s, "commits", source.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if pks := table.PrimaryKeys(); fmt.Sprint(pks) != "[sha]" {
				t.Fatalf("primary keys=%v want [sha]", pks)
			}
			if table.Strategy() != config.StrategyMerge || table.IncrementalKey() != "committed_at" {
				t.Fatalf("strategy=%q incremental key=%q", table.Strategy(), table.IncrementalKey())
			}
			if fmt.Sprint(rows) != "[{o/r r1}]" {
				t.Fatalf("rows=%v, want GitHub's name in repo", rows)
			}
			if got := fmt.Sprint(f.paths); got != "[/repos/O/"+uriRepo+" /repos/o/r/commits]" {
				t.Fatalf("requests=%s, want one lookup and the commits", got)
			}
		})
	}
}

func TestGithubCommitsSingleRepoNotFound(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o&repo=nope")

	_, _, _, err := readCommitRows(t, s, "commits", source.ReadOptions{})
	if err == nil || err.Error() != "github repositories not found or not accessible with this token: o/nope" {
		t.Fatalf("err=%v", err)
	}
	if reqs := f.commitRequests(); len(reqs) != 0 {
		t.Fatalf("commits were requested: %v", reqs)
	}
}

func TestGithubCommitsListedRepos(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o&repo=r")

	table, rows, _, err := readCommitRows(t, s, "commits: bruin, forked,INGESTR", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pks := table.PrimaryKeys(); fmt.Sprint(pks) != "[repo sha]" {
		t.Fatalf("primary keys=%v want [repo sha]", pks)
	}
	sch, err := table.GetSchema(context.Background())
	if err != nil || fmt.Sprint(sch.PrimaryKeys) != "[repo sha]" {
		t.Fatalf("schema primary keys=%v err=%v", sch, err)
	}
	if got := fmt.Sprint(f.commitRequests()); got != "[/repos/o/Bruin/commits /repos/o/forked/commits /repos/o/ingestr/commits]" {
		t.Fatalf("commit requests=%s", got)
	}
	if reqs := f.listingRequests(); len(reqs) != 0 {
		t.Fatalf("list mode listed owners: %v", reqs)
	}
	if got := fmt.Sprint(rows); got != "[{o/Bruin b1} {o/Bruin b2} {o/forked f1} {o/ingestr i1} {o/ingestr i2}]" {
		t.Fatalf("rows=%s", got)
	}
}

func TestGithubCommitsListedReposOfSeveralOwners(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o&repo=r")

	table, rows, _, err := readCommitRows(t, s, "commits: bruin, other/tool, O/ingestr, Other/lib, o/Bruin", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pks := table.PrimaryKeys(); fmt.Sprint(pks) != "[repo sha]" {
		t.Fatalf("primary keys=%v want [repo sha]", pks)
	}
	if reqs := f.listingRequests(); len(reqs) != 0 {
		t.Fatalf("list mode listed owners: %v", reqs)
	}
	if got := fmt.Sprint(f.commitRequests()); got != "[/repos/o/Bruin/commits /repos/other/tool/commits /repos/o/ingestr/commits /repos/other/Lib/commits]" {
		t.Fatalf("commit requests=%s", got)
	}
	if got := fmt.Sprint(rows); got != "[{o/Bruin b1} {o/Bruin b2} {other/tool t1} {o/ingestr i1} {o/ingestr i2} {other/Lib l1}]" {
		t.Fatalf("rows=%s", got)
	}
}

func TestGithubCommitsUnknownOwnerFailsBeforeReading(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o")

	_, rows, batches, err := readCommitRows(t, s, "commits:bruin, ghost/x, other/nope, dacc, other/tool", source.ReadOptions{})
	want := "github repositories not found or not accessible with this token: ghost/x, other/nope, o/dacc"
	if err == nil || err.Error() != want {
		t.Fatalf("err=%v want %q", err, want)
	}
	if batches != 0 || len(rows) != 0 {
		t.Fatalf("got %d batches with %d rows before failing", batches, len(rows))
	}
	if reqs := f.commitRequests(); len(reqs) != 0 {
		t.Fatalf("commits were requested before failing: %v", reqs)
	}
}

func TestGithubCommitsAllRepos(t *testing.T) {
	cases := []struct {
		uri, wantRequests, wantRows, wantLog string
	}{
		{
			uri:          "github://?access_token=test-token&owner=o",
			wantRequests: "[/repos/o/r/commits /repos/o/ingestr/commits /repos/o/old/commits /repos/o/Bruin/commits]",
			wantRows:     "[{o/r r1} {o/ingestr i1} {o/ingestr i2} {o/old o1} {o/Bruin b1} {o/Bruin b2}]",
			wantLog:      "Reading the commits of 4 repositories of o (forks skipped: 1)",
		},
		{
			uri:          "github://?access_token=test-token&owner=o&repo=",
			wantRequests: "[/repos/o/r/commits /repos/o/ingestr/commits /repos/o/old/commits /repos/o/Bruin/commits]",
			wantRows:     "[{o/r r1} {o/ingestr i1} {o/ingestr i2} {o/old o1} {o/Bruin b1} {o/Bruin b2}]",
			wantLog:      "Reading the commits of 4 repositories of o (forks skipped: 1)",
		},
		{
			uri:          "github://?access_token=test-token&owner=u",
			wantRequests: "[/repos/u/app/commits]",
			wantRows:     "[{u/app a1}]",
			wantLog:      "Reading the commits of 1 repositories of u (forks skipped: 1)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			logs := captureOutput(t)
			f := newCommitsFake(t)
			s, _ := connectURIToFake(t, f, tc.uri)

			table, rows, _, err := readCommitRows(t, s, "commits", source.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if pks := table.PrimaryKeys(); fmt.Sprint(pks) != "[repo sha]" {
				t.Fatalf("primary keys=%v want [repo sha]", pks)
			}
			if got := fmt.Sprint(f.commitRequests()); got != tc.wantRequests {
				t.Fatalf("commit requests=%s, want every repository but the forks, once: %s", got, tc.wantRequests)
			}
			if got := fmt.Sprint(rows); got != tc.wantRows {
				t.Fatalf("rows=%s want %s", got, tc.wantRows)
			}
			if !strings.Contains(logs.String(), tc.wantLog) {
				t.Fatalf("missing %q in output: %q", tc.wantLog, logs.String())
			}
		})
	}
}

func TestGithubCommitsAllReposSkipsUnreadableRepos(t *testing.T) {
	logs := captureOutput(t)
	f := newCommitsFake(t)
	f.blocked["o/ingestr"] = http.StatusForbidden
	f.blocked["o/old"] = http.StatusUnavailableForLegalReasons
	f.blocked["o/Bruin"] = http.StatusNotFound
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o")

	_, rows, _, err := readCommitRows(t, s, "commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(rows); got != "[{o/r r1}]" {
		t.Fatalf("rows=%s", got)
	}
	for _, want := range []string{
		"Warning: skipping the commits of o/ingestr, GitHub answered 403",
		"Warning: skipping the commits of o/old, GitHub answered 451",
		"Warning: skipping the commits of o/Bruin, GitHub answered 404",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing %q in output: %q", want, logs.String())
		}
	}
}

func TestGithubCommitsUnreadableRepoFailsOtherModes(t *testing.T) {
	cases := []struct{ name, uri, table string }{
		{name: "all mode on a server error", uri: "github://?access_token=test-token&owner=o", table: "commits"},
		{name: "list mode", uri: "github://?access_token=test-token&owner=o", table: "commits:r,old"},
		{name: "single mode", uri: "github://?access_token=test-token&owner=o&repo=old", table: "commits"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureOutput(t)
			f := newCommitsFake(t)
			if tc.name == "all mode on a server error" {
				f.blocked["o/old"] = http.StatusBadGateway
			} else {
				f.blocked["o/old"] = http.StatusUnavailableForLegalReasons
			}
			s, _ := connectURIToFake(t, f, tc.uri)

			_, _, _, err := readCommitRows(t, s, tc.table, source.ReadOptions{})
			if err == nil || !strings.Contains(err.Error(), "failed to fetch commits with status") {
				t.Fatalf("err=%v, want the commits request to fail the run", err)
			}
		})
	}
}

func TestGithubCommitsListedReposTheTokenCanRead(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	f.renamed["o/bruin-old"] = 5
	f.lookup["other/blocked"] = http.StatusUnavailableForLegalReasons
	f.lookup["other/sso"] = http.StatusForbidden
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o")

	_, _, _, err := readCommitRows(t, s, "commits:bruin-old, u/secret, other/blocked, other/sso", source.ReadOptions{})
	want := "github repositories not found or not accessible with this token: other/blocked, other/sso"
	if err == nil || err.Error() != want {
		t.Fatalf("err=%v want %q", err, want)
	}

	f = newCommitsFake(t)
	f.renamed["o/bruin-old"] = 5
	s, _ = connectURIToFake(t, f, "github://?access_token=test-token&owner=o")
	_, rows, _, err := readCommitRows(t, s, "commits:bruin-old, u/secret, Bruin", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(rows); got != "[{o/Bruin b1} {o/Bruin b2} {u/secret s1}]" {
		t.Fatalf("rows=%s, want the renamed repo under its new name once and the collaborator repo", got)
	}
}

func TestGithubCommitsUnknownRepoFailsBeforeReading(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o")

	_, rows, batches, err := readCommitRows(t, s, "commits:bruin, dacc, ingestr, foo", source.ReadOptions{})
	want := "github repositories not found or not accessible with this token: o/dacc, o/foo"
	if err == nil || err.Error() != want {
		t.Fatalf("err=%v want %q", err, want)
	}
	if batches != 0 || len(rows) != 0 {
		t.Fatalf("got %d batches with %d rows before failing", batches, len(rows))
	}
	if reqs := f.commitRequests(); len(reqs) != 0 {
		t.Fatalf("commits were requested before failing: %v", reqs)
	}
}

func TestGithubCommitsEmptyRepoInList(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	f.empty["o/old"] = true
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o")

	_, rows, _, err := readCommitRows(t, s, "commits:old,ingestr", source.ReadOptions{})
	if err != nil {
		t.Fatalf("an empty repository should read as zero rows, got: %v", err)
	}
	if got := fmt.Sprint(rows); got != "[{o/ingestr i1} {o/ingestr i2}]" {
		t.Fatalf("rows=%s", got)
	}
}

func TestGithubCommitsLimitSpansRepos(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o")

	_, rows, _, err := readCommitRows(t, s, "commits:ingestr,bruin,r", source.ReadOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(rows); got != "[{o/ingestr i1} {o/ingestr i2} {o/Bruin b1}]" {
		t.Fatalf("rows=%s", got)
	}
	if got := fmt.Sprint(f.commitRequests()); got != "[/repos/o/ingestr/commits /repos/o/Bruin/commits]" {
		t.Fatalf("commit requests=%s, want none after the limit", got)
	}

	f = newCommitsFake(t)
	s, _ = connectURIToFake(t, f, "github://?access_token=test-token&owner=o")
	_, rows, _, err = readCommitRows(t, s, "commits:u/*, ingestr, o/*", source.ReadOptions{Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(rows); got != "[{o/ingestr i1} {o/ingestr i2} {u/app a1} {o/r r1}]" {
		t.Fatalf("rows=%s, want the named repository first, then the owners in the order written", got)
	}
	if got := fmt.Sprint(f.commitRequests()); got != "[/repos/o/ingestr/commits /repos/u/app/commits /repos/o/r/commits]" {
		t.Fatalf("commit requests=%s, want none after the limit", got)
	}
}

func TestGithubURIOwnerAndRepoRules(t *testing.T) {
	cases := []struct{ uri, wantErr, wantTarget string }{
		{uri: "github://?access_token=x", wantTarget: "the listed repositories"},
		{uri: "github://?access_token=x&owner=&repo=", wantTarget: "the listed repositories"},
		{uri: "github://?access_token=x&owner=o", wantTarget: "o"},
		{uri: "github://?access_token=x&owner=o&repo=r", wantTarget: "o/r"},
		{uri: "github://?access_token=x&repo=r", wantErr: `repo "r" needs owner in the URI`},
		{uri: "github://?access_token=x&owner=&repo=r", wantErr: `repo "r" needs owner in the URI`},
		{uri: "github://?access_token=x&owner=bruin-data&repo=bruin-data/ingestr", wantErr: `repo must be a bare repository name, got "bruin-data/ingestr": set owner=<owner> and repo=<name>`},
		{uri: "github://?access_token=x&owner=bruin-data&repo=bruin-data%2Fingestr", wantErr: `repo must be a bare repository name, got "bruin-data/ingestr"`},
		{uri: "github://?access_token=x&repo=bruin-data/ingestr", wantErr: `repo must be a bare repository name`},
		{uri: "github://?access_token=x&owner=bruin-data/ingestr", wantErr: `owner must be a user or organization login, got "bruin-data/ingestr"`},
	}
	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			s := NewGitHubSource()
			err := s.Connect(context.Background(), tc.uri)
			defer func() { _ = s.Close(context.Background()) }()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := s.target(); got != tc.wantTarget {
				t.Fatalf("target=%q want %q", got, tc.wantTarget)
			}
		})
	}
}

func TestGithubNoOwnerTables(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token")

	for name, want := range map[string]string{
		"commits":       "table commits needs owner in the URI, or a list of repositories: commits:<owner>/<repo>, or commits:<owner>/* for every repository of an owner",
		"commits:r":     `repository "r" in table commits:r has no owner and the URI has no owner`,
		"repositories":  "table repositories needs owner in the URI",
		"issues":        "table issues needs owner and repo in the URI (only commits and pull_request_commits can list repositories)",
		"pull_requests": "table pull_requests needs owner and repo in the URI",
		"stargazers":    "table stargazers needs owner and repo in the URI",
		"repo_events":   "table repo_events needs owner and repo in the URI",
	} {
		if _, err := s.GetTable(context.Background(), source.TableRequest{Name: name}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("GetTable(%q) err=%v want %q", name, err, want)
		}
	}
	if reqs := f.requests(func(string) bool { return true }); len(reqs) != 0 {
		t.Fatalf("GitHub was called before the table errors: %v", reqs)
	}

	table, rows, _, err := readCommitRows(t, s, "commits:o/r, u/app", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pks := table.PrimaryKeys(); fmt.Sprint(pks) != "[repo sha]" {
		t.Fatalf("primary keys=%v want [repo sha]", pks)
	}
	if got := fmt.Sprint(rows); got != "[{o/r r1} {u/app a1}]" {
		t.Fatalf("rows=%s", got)
	}
}

func TestGithubOwnerWithoutRepoTables(t *testing.T) {
	s := NewGitHubSource()
	if err := s.Connect(context.Background(), "github://?owner=o"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close(context.Background()) }()
	for _, name := range []string{"issues", "pull_requests", "stargazers", "repo_events"} {
		want := "table " + name + " needs owner and repo in the URI"
		if _, err := s.GetTable(context.Background(), source.TableRequest{Name: name}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("GetTable(%q) err=%v want %q", name, err, want)
		}
	}
}

func TestGithubCommitsWildcard(t *testing.T) {
	for _, uri := range []string{"github://?access_token=test-token", "github://?access_token=test-token&owner=o"} {
		t.Run(uri, func(t *testing.T) {
			logs := captureOutput(t)
			f := newCommitsFake(t)
			s, _ := connectURIToFake(t, f, uri)

			table, rows, _, err := readCommitRows(t, s, "commits:o/*", source.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if pks := table.PrimaryKeys(); fmt.Sprint(pks) != "[repo sha]" {
				t.Fatalf("primary keys=%v want [repo sha]", pks)
			}
			if got := fmt.Sprint(f.commitRequests()); got != "[/repos/o/r/commits /repos/o/ingestr/commits /repos/o/old/commits /repos/o/Bruin/commits]" {
				t.Fatalf("commit requests=%s, want every repository of o but the fork, once", got)
			}
			if got := fmt.Sprint(rows); got != "[{o/r r1} {o/ingestr i1} {o/ingestr i2} {o/old o1} {o/Bruin b1} {o/Bruin b2}]" {
				t.Fatalf("rows=%s", got)
			}
			if want := "Reading the commits of 4 repositories of o (forks skipped: 1)"; !strings.Contains(logs.String(), want) {
				t.Fatalf("missing %q in output: %q", want, logs.String())
			}
		})
	}
}

func TestGithubCommitsWildcardSeveralOwners(t *testing.T) {
	logs := captureOutput(t)
	f := newCommitsFake(t)
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token")

	_, rows, _, err := readCommitRows(t, s, "commits:o/*, u/*, Other/*", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(rows); got != "[{o/r r1} {o/ingestr i1} {o/ingestr i2} {o/old o1} {o/Bruin b1} {o/Bruin b2} {u/app a1} {other/tool t1}]" {
		t.Fatalf("rows=%s", got)
	}
	for _, want := range []string{
		"Reading the commits of 4 repositories of o (forks skipped: 1)",
		"Reading the commits of 1 repositories of u (forks skipped: 1)",
		"Reading the commits of 1 repositories of Other (forks skipped: 1)",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing %q in output: %q", want, logs.String())
		}
	}
}

func TestGithubCommitsWildcardAndNamedRepos(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token")

	_, rows, _, err := readCommitRows(t, s, "commits:o/*, O/INGESTR, o/forked, u/app", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(f.commitRequests()); got != "[/repos/o/ingestr/commits /repos/o/forked/commits /repos/u/app/commits /repos/o/r/commits /repos/o/old/commits /repos/o/Bruin/commits]" {
		t.Fatalf("commit requests=%s, want the named repositories first, a named fork read, and ingestr once", got)
	}
	if got := fmt.Sprint(rows); got != "[{o/ingestr i1} {o/ingestr i2} {o/forked f1} {u/app a1} {o/r r1} {o/old o1} {o/Bruin b1} {o/Bruin b2}]" {
		t.Fatalf("rows=%s", got)
	}
}

func TestGithubCommitsWildcardSkipsUnreadableRepos(t *testing.T) {
	logs := captureOutput(t)
	f := newCommitsFake(t)
	f.blocked["o/ingestr"] = http.StatusForbidden
	f.blocked["o/old"] = http.StatusUnavailableForLegalReasons
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token")

	_, rows, _, err := readCommitRows(t, s, "commits:o/*", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(rows); got != "[{o/r r1} {o/Bruin b1} {o/Bruin b2}]" {
		t.Fatalf("rows=%s", got)
	}
	for _, want := range []string{
		"Warning: skipping the commits of o/ingestr, GitHub answered 403",
		"Warning: skipping the commits of o/old, GitHub answered 451",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing %q in output: %q", want, logs.String())
		}
	}

	f = newCommitsFake(t)
	f.blocked["o/old"] = http.StatusUnavailableForLegalReasons
	s, _ = connectURIToFake(t, f, "github://?access_token=test-token")
	_, _, _, err = readCommitRows(t, s, "commits:o/*, o/old", source.ReadOptions{})
	if err == nil || !strings.Contains(err.Error(), "failed to fetch commits with status") {
		t.Fatalf("err=%v, want a named repository that can't be read to fail the run", err)
	}
}

func TestGithubCommitsWildcardUnknownOwnerFailsBeforeReading(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token")

	_, rows, batches, err := readCommitRows(t, s, "commits:o/r, o/*, ghost/*", source.ReadOptions{})
	if err == nil || !strings.Contains(err.Error(), "failed to look up github owner ghost with status 404") {
		t.Fatalf("err=%v, want the unknown owner to fail the run", err)
	}
	if batches != 0 || len(rows) != 0 {
		t.Fatalf("got %d batches with %d rows before failing", batches, len(rows))
	}
	if reqs := f.commitRequests(); len(reqs) != 0 {
		t.Fatalf("commits were requested before failing: %v", reqs)
	}
}

func TestGithubCommitsWildcardOverridesURIRepo(t *testing.T) {
	captureOutput(t)
	f := newCommitsFake(t)
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o&repo=r")

	table, rows, _, err := readCommitRows(t, s, "commits:u/*", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pks := table.PrimaryKeys(); fmt.Sprint(pks) != "[repo sha]" {
		t.Fatalf("primary keys=%v want [repo sha]", pks)
	}
	if got := fmt.Sprint(rows); got != "[{u/app a1}]" {
		t.Fatalf("rows=%s, want only the repositories of u", got)
	}
	if reqs := f.requests(func(p string) bool {
		return strings.HasPrefix(p, "/repos/o/") || strings.HasPrefix(p, "/orgs/o/") || p == "/users/o"
	}); len(reqs) != 0 {
		t.Fatalf("the URI owner or repo was read: %v", reqs)
	}
}

// fakeGraph is an in-memory commit graph. SHAs are numbered in creation order, which stands in
// for commit dates.
type fakeGraph struct {
	parents map[string][]string
	order   map[string]int
}

func newFakeGraph() *fakeGraph {
	return &fakeGraph{parents: map[string][]string{}, order: map[string]int{}}
}

func (g *fakeGraph) add(parents ...string) string {
	idx := len(g.order) + 1
	sha := fmt.Sprintf("c%05d", idx)
	g.order[sha] = idx
	g.parents[sha] = parents
	return sha
}

func (g *fakeGraph) chain(from string, n int) string {
	tip := from
	for i := 0; i < n; i++ {
		if tip == "" {
			tip = g.add()
		} else {
			tip = g.add(tip)
		}
	}
	return tip
}

func (g *fakeGraph) reach(sha string) map[string]bool {
	seen := map[string]bool{}
	stack := []string{sha}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[s] {
			continue
		}
		if _, ok := g.order[s]; !ok {
			continue
		}
		seen[s] = true
		stack = append(stack, g.parents[s]...)
	}
	return seen
}

// between is git rev-list base..head, oldest first.
func (g *fakeGraph) between(base, head string) []string {
	exclude := g.reach(base)
	var out []string
	for s := range g.reach(head) {
		if !exclude[s] {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return g.order[out[i]] < g.order[out[j]] })
	return out
}

func (g *fakeGraph) date(sha string) string {
	return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(g.order[sha]) * time.Minute).Format(time.RFC3339)
}

type fakeForcePush struct{ at, before string }

type fakePR struct {
	number                  int
	state                   string
	created, updated        string
	closed, merged          string
	base, head, mergeCommit string
	baseRef, headRef        string
	headRepo, author        string
	pushes                  []fakeForcePush
	reads                   int
}

// fakeCommitter overrides who committed a commit and its signature.
type fakeCommitter struct {
	name, email, gqlLogin, restLogin string
	state, reason                    string
	valid, byGitHub                  bool
}

// prFake serves pull requests over a fakeGraph: the GraphQL listing and its per-pull-request
// commit and force-push pages, with GitHub's 250-commit cap, REST compare (cut to the newest
// compareCap commits, like GitHub's ~10,000 cut that drops the oldest) and the commit history
// listing. Repository lookups and owner listings go to commitsFake.
type prFake struct {
	*commitsFake
	g                          *fakeGraph
	mu                         sync.Mutex
	prs                        map[string][]*fakePR
	gqlErrors                  map[string]string
	compareStatus              map[string]int
	committers                 map[string]fakeCommitter
	aheadExtra                 int
	compareCap                 int
	listPageSize               int
	listStep                   int
	gqlNodeLimit               int
	pushDuringReads            int
	rewriteDuringReads         int
	updatedOnChange            string
	noCursor                   bool
	unknownSignatures          map[string]bool
	forcePushesDuringPushPages int
	pushOnReads                map[int]bool
	pushPagesServed            int
	timelinePageSize           int
	gqlCalls                   []string
	compares                   []string
	histories                  []string
}

func newPRFake(t *testing.T) *prFake {
	return &prFake{
		commitsFake:      newCommitsFake(t),
		g:                newFakeGraph(),
		prs:              map[string][]*fakePR{},
		gqlErrors:        map[string]string{},
		compareStatus:    map[string]int{},
		committers:       map[string]fakeCommitter{},
		compareCap:       10000,
		listPageSize:     50,
		timelinePageSize: 100,
	}
}

func (f *prFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	switch {
	case r.URL.Path == "/graphql":
		f.serveGraphQL(w, r)
	case len(parts) == 5 && parts[0] == "repos" && parts[3] == "compare":
		f.serveCompare(w, r, parts[4])
	case len(parts) == 4 && parts[0] == "repos" && parts[3] == "commits" && r.URL.Query().Get("sha") != "":
		f.serveHistory(w, r)
	default:
		f.commitsFake.ServeHTTP(w, r)
	}
}

func (f *prFake) record(list *[]string, entry string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	*list = append(*list, entry)
}

func (f *prFake) calls(list *[]string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), *list...)
}

func (f *prFake) committer(sha string) fakeCommitter {
	if c, ok := f.committers[sha]; ok {
		return c
	}
	return fakeCommitter{name: "Dev", email: "dev@example.com", gqlLogin: "dev", restLogin: "dev", reason: "unsigned"}
}

func (f *prFake) gqlCommit(sha string) map[string]interface{} {
	c := f.committer(sha)
	var committerUser interface{}
	if c.gqlLogin != "" {
		committerUser = map[string]interface{}{"login": c.gqlLogin}
	}
	var signature interface{}
	if c.state != "" {
		signature = map[string]interface{}{"isValid": c.valid, "state": c.state, "wasSignedByGitHub": c.byGitHub}
	}
	return map[string]interface{}{
		"oid":       sha,
		"url":       "https://github.com/o/r/commit/" + sha,
		"message":   "commit " + sha,
		"author":    map[string]interface{}{"name": "Dev", "email": "dev@example.com", "date": f.g.date(sha), "user": map[string]interface{}{"login": "dev"}},
		"committer": map[string]interface{}{"name": c.name, "email": c.email, "date": f.g.date(sha), "user": committerUser},
		"parents":   map[string]interface{}{"totalCount": len(f.g.parents[sha])},
		"signature": signature,
	}
}

func (f *prFake) restCommit(sha string) map[string]interface{} {
	c := f.committer(sha)
	parents := make([]map[string]interface{}, 0, len(f.g.parents[sha]))
	for _, p := range f.g.parents[sha] {
		parents = append(parents, map[string]interface{}{"sha": p})
	}
	var committer interface{}
	if c.restLogin != "" {
		committer = map[string]interface{}{"login": c.restLogin}
	}
	return map[string]interface{}{
		"sha":      sha,
		"html_url": "https://github.com/o/r/commit/" + sha,
		"commit": map[string]interface{}{
			"message":      "commit " + sha,
			"author":       map[string]interface{}{"name": "Dev", "email": "dev@example.com", "date": f.g.date(sha)},
			"committer":    map[string]interface{}{"name": c.name, "email": c.email, "date": f.g.date(sha)},
			"verification": map[string]interface{}{"verified": c.valid, "reason": c.reason},
		},
		"author":    map[string]interface{}{"login": "dev"},
		"committer": committer,
		"parents":   parents,
	}
}

func (f *prFake) prCommitList(pr *fakePR) ([]string, int) {
	all := f.g.between(pr.base, pr.head)
	total := min(len(all), 10000)
	if len(all) > prCommitListCap {
		all = all[len(all)-prCommitListCap:]
	}
	return all, total
}

func pageOf[T any](items []T, after string, size int) ([]T, map[string]interface{}) {
	offset, _ := strconv.Atoi(after)
	offset = min(offset, len(items))
	end := min(offset+size, len(items))
	return items[offset:end], map[string]interface{}{"hasNextPage": end < len(items), "endCursor": strconv.Itoa(end)}
}

func (f *prFake) commitConnection(pr *fakePR, after string) map[string]interface{} {
	list, total := f.prCommitList(pr)
	if f.gqlNodeLimit > 0 {
		list = list[:min(f.gqlNodeLimit, len(list))]
	}
	page, info := pageOf(list, after, 100)
	if f.noCursor {
		info["endCursor"] = nil
	}
	nodes := make([]interface{}, 0, len(page))
	for _, sha := range page {
		nodes = append(nodes, map[string]interface{}{"commit": f.gqlCommit(sha)})
	}
	return map[string]interface{}{"totalCount": total, "pageInfo": info, "nodes": nodes}
}

func (f *prFake) pushConnection(pr *fakePR, after string) map[string]interface{} {
	page, info := pageOf(pr.pushes, after, f.timelinePageSize)
	if f.noCursor {
		info["endCursor"] = nil
	}
	nodes := make([]interface{}, 0, len(page))
	for _, e := range page {
		var before interface{}
		if e.before != "" {
			before = map[string]interface{}{"oid": e.before}
		}
		nodes = append(nodes, map[string]interface{}{"createdAt": e.at, "beforeCommit": before})
	}
	return map[string]interface{}{"pageInfo": info, "nodes": nodes}
}

func nullable(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func (f *prFake) prNode(pr *fakePR) map[string]interface{} {
	node := map[string]interface{}{
		"number": pr.number, "url": fmt.Sprintf("https://github.com/o/r/pull/%d", pr.number), "title": fmt.Sprintf("PR %d", pr.number),
		"state": pr.state, "author": map[string]interface{}{"login": pr.author},
		"createdAt": pr.created, "updatedAt": pr.updated, "closedAt": nullable(pr.closed), "mergedAt": nullable(pr.merged),
		"baseRefName": pr.baseRef, "baseRefOid": pr.base, "headRefName": pr.headRef, "headRefOid": pr.head,
		"headRepository": nil, "mergeCommit": nil,
		"commits":     f.commitConnection(pr, ""),
		"forcePushes": f.pushConnection(pr, ""),
	}
	if pr.headRepo != "" {
		node["headRepository"] = map[string]interface{}{"nameWithOwner": pr.headRepo}
	}
	if pr.mergeCommit != "" {
		node["mergeCommit"] = map[string]interface{}{"oid": pr.mergeCommit}
	}
	return node
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *prFake) findPR(key string, number int) *fakePR {
	for _, p := range f.prs[key] {
		if p.number == number {
			return p
		}
	}
	f.t.Errorf("unknown pull request %s#%d", key, number)
	return nil
}

func (f *prFake) serveGraphQL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Errorf("bad graphql request: %v", err)
		return
	}
	owner, _ := req.Variables["owner"].(string)
	name, _ := req.Variables["name"].(string)
	after, _ := req.Variables["after"].(string)
	key := strings.ToLower(owner + "/" + name)

	if strings.Contains(req.Query, "object(oid:") {
		f.serveSignatures(w, key, req.Query)
		return
	}
	if strings.Contains(req.Query, "pullRequests(") {
		f.record(&f.gqlCalls, "list "+key+" "+after)
		if typ, ok := f.gqlErrors[key]; ok {
			writeJSON(w, map[string]interface{}{"data": map[string]interface{}{"repository": nil}, "errors": []interface{}{map[string]interface{}{"type": typ, "message": typ + " for " + key}}})
			return
		}
		prs := append([]*fakePR(nil), f.prs[key]...)
		sort.SliceStable(prs, func(i, j int) bool { return prs[i].updated > prs[j].updated })
		offset, _ := strconv.Atoi(after)
		offset = min(offset, len(prs))
		end := min(offset+f.listPageSize, len(prs))
		next := end
		if f.listStep > 0 {
			next = min(offset+f.listStep, len(prs))
		}
		nodes := make([]interface{}, 0)
		for _, pr := range prs[offset:end] {
			nodes = append(nodes, f.prNode(pr))
		}
		for _, pr := range prs[offset:end] {
			f.afterFirstCommitPage(pr)
		}
		writeJSON(w, map[string]interface{}{"data": map[string]interface{}{"repository": map[string]interface{}{"pullRequests": map[string]interface{}{
			"pageInfo": map[string]interface{}{"hasNextPage": end < len(prs), "endCursor": strconv.Itoa(next)},
			"nodes":    nodes,
		}}}})
		return
	}

	number := int(req.Variables["number"].(float64))
	pr := f.findPR(key, number)
	if pr == nil {
		return
	}
	if strings.Contains(req.Query, "...prNode") {
		f.record(&f.gqlCalls, fmt.Sprintf("pr %s#%d", key, number))
		node := f.prNode(pr)
		f.afterFirstCommitPage(pr)
		writeJSON(w, map[string]interface{}{"data": map[string]interface{}{"repository": map[string]interface{}{"pullRequest": node}}})
		return
	}
	pull := map[string]interface{}{}
	if strings.Contains(req.Query, "forcePushes") {
		f.record(&f.gqlCalls, fmt.Sprintf("pushes %s#%d %s", key, number, after))
		f.pushPagesServed++
		if f.pushPagesServed <= f.forcePushesDuringPushPages {
			f.pushThenForcePush(pr)
		}
		pull["headRefOid"] = pr.head
		pull["forcePushes"] = f.pushConnection(pr, after)
	} else {
		f.record(&f.gqlCalls, fmt.Sprintf("commits %s#%d %s", key, number, after))
		pull["headRefOid"] = pr.head
		pull["commits"] = f.commitConnection(pr, after)
	}
	writeJSON(w, map[string]interface{}{"data": map[string]interface{}{"repository": map[string]interface{}{"pullRequest": pull}}})
}

// afterFirstCommitPage simulates a push (pushDuringReads) or a rewrite of the whole pull request
// with as many commits (rewriteDuringReads) right after the first page of each of its first reads.
func (f *prFake) afterFirstCommitPage(pr *fakePR) {
	pr.reads++
	changed := false
	if pr.reads <= f.pushDuringReads || f.pushOnReads[pr.reads] {
		pr.head = f.g.add(pr.head)
		changed = true
	}
	if pr.reads <= f.rewriteDuringReads {
		pr.head = f.g.chain(pr.base, len(f.g.between(pr.base, pr.head)))
		changed = true
	}
	if changed && f.updatedOnChange != "" {
		pr.updated = f.updatedOnChange
	}
}

// pushThenForcePush simulates, while force-push pages are read, a push of commit C and a force push
// that replaces C, recorded as a new force-push event.
func (f *prFake) pushThenForcePush(pr *fakePR) {
	c := f.g.add(pr.head)
	at := time.Date(2026, 11, 7, 10, 0, len(pr.pushes), 0, time.UTC).Format(time.RFC3339)
	pr.pushes = append(pr.pushes, fakeForcePush{at: at, before: c})
	pr.head = f.g.add(pr.head)
	if f.updatedOnChange != "" {
		pr.updated = f.updatedOnChange
	}
}

var signatureAlias = regexp.MustCompile(`(c\d+): object\(oid: "([^"]+)"\)`)

// serveSignatures answers the batched commit lookups: a commit unknown to the graph, or listed in
// unknownSignatures, is null.
func (f *prFake) serveSignatures(w http.ResponseWriter, key, query string) {
	matches := signatureAlias.FindAllStringSubmatch(query, -1)
	f.record(&f.gqlCalls, fmt.Sprintf("signatures %s %d", key, len(matches)))
	repo := map[string]interface{}{}
	for _, m := range matches {
		if _, ok := f.g.order[m[2]]; !ok || f.unknownSignatures[m[2]] {
			repo[m[1]] = nil
			continue
		}
		var signature interface{}
		if c := f.committer(m[2]); c.state != "" {
			signature = map[string]interface{}{"wasSignedByGitHub": c.byGitHub}
		}
		repo[m[1]] = map[string]interface{}{"signature": signature}
	}
	writeJSON(w, map[string]interface{}{"data": map[string]interface{}{"repository": repo}})
}

func (f *prFake) serveCompare(w http.ResponseWriter, r *http.Request, spec string) {
	base, head, _ := strings.Cut(spec, "...")
	f.record(&f.compares, spec)
	if status, ok := f.compareStatus[head]; ok {
		writeStatus(w, status)
		return
	}
	if _, ok := f.g.order[head]; !ok {
		writeStatus(w, http.StatusNotFound)
		return
	}
	headReach, baseReach := f.g.reach(head), f.g.reach(base)
	common := false
	for s := range headReach {
		if baseReach[s] {
			common = true
			break
		}
	}
	if !common {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, `{"message":"No common ancestor between %s and %s."}`, base, head)
		return
	}
	ahead := f.g.between(base, head)
	behind := len(f.g.between(head, base))
	visible := ahead
	if len(visible) > f.compareCap {
		visible = visible[len(visible)-f.compareCap:]
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	start := min((page-1)*perPage, len(visible))
	commits := make([]interface{}, 0)
	for _, sha := range visible[start:min(start+perPage, len(visible))] {
		commits = append(commits, f.restCommit(sha))
	}
	writeJSON(w, map[string]interface{}{"ahead_by": len(ahead) + f.aheadExtra, "behind_by": behind, "commits": commits})
}

func (f *prFake) serveHistory(w http.ResponseWriter, r *http.Request) {
	head := r.URL.Query().Get("sha")
	f.record(&f.histories, head)
	all := f.g.between("", head)
	sort.Slice(all, func(i, j int) bool { return f.g.order[all[i]] > f.g.order[all[j]] })
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(page, 1)
	start := min((page-1)*100, len(all))
	end := min(start+100, len(all))
	if end < len(all) {
		w.Header().Set("Link", fmt.Sprintf("<%s%s?sha=%s&per_page=100&page=%d>; rel=\"next\"", restApiBaseUrl, r.URL.Path, head, page+1))
	}
	commits := make([]interface{}, 0)
	for _, sha := range all[start:end] {
		commits = append(commits, f.restCommit(sha))
	}
	writeJSON(w, commits)
}

func (f *prFake) addPR(repo string, pr *fakePR) *fakePR {
	if pr.state == "" {
		pr.state = "OPEN"
	}
	if pr.created == "" {
		pr.created = "2026-11-01T00:00:00Z"
	}
	if pr.updated == "" {
		pr.updated = "2026-11-10T00:00:00Z"
	}
	if pr.baseRef == "" {
		pr.baseRef = "main"
	}
	if pr.headRef == "" {
		pr.headRef = fmt.Sprintf("feature-%d", pr.number)
	}
	if pr.author == "" {
		pr.author = "dev"
	}
	f.prs[strings.ToLower(repo)] = append(f.prs[strings.ToLower(repo)], pr)
	return pr
}

type prRow map[string]interface{}

func (r prRow) key() string { return fmt.Sprintf("%v#%v %v", r["repo"], r["pr_number"], r["sha"]) }

func readPRCommitTable(t *testing.T, s *GithubSource, name string, opts source.ReadOptions) (source.SourceTable, []prRow, error) {
	t.Helper()
	table, err := s.GetTable(context.Background(), source.TableRequest{Name: name})
	if err != nil {
		return nil, nil, err
	}
	results, err := table.Read(context.Background(), opts)
	if err != nil {
		return table, nil, err
	}
	var rows []prRow
	var readErr error
	for res := range results {
		if res.Err != nil {
			readErr = res.Err
			continue
		}
		rec := res.Batch
		for i := 0; i < int(rec.NumRows()); i++ {
			row := prRow{}
			for c, field := range rec.Schema().Fields() {
				col := rec.Column(c)
				if col.IsNull(i) {
					row[field.Name] = nil
					continue
				}
				switch a := col.(type) {
				case *array.String:
					row[field.Name] = a.Value(i)
				case *array.Int64:
					row[field.Name] = a.Value(i)
				case *array.Boolean:
					row[field.Name] = a.Value(i)
				case *array.Timestamp:
					row[field.Name] = time.UnixMicro(int64(a.Value(i))).UTC().Format(time.RFC3339)
				default:
					t.Fatalf("column %s has unexpected type %s", field.Name, field.Type)
				}
			}
			rows = append(rows, row)
		}
		rec.Release()
	}
	return table, rows, readErr
}

func connectPRFake(t *testing.T, f *prFake) *GithubSource {
	t.Helper()
	s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o&repo=r")
	return s
}

func shasOf(rows []prRow, pr int64) []string {
	var out []string
	for _, r := range rows {
		if r["pr_number"] == pr && r["replaced_by_force_push"] != true {
			out = append(out, r["sha"].(string))
		}
	}
	sort.Strings(out)
	return out
}

func mustEqualSet(t *testing.T, what string, got, want []string) {
	t.Helper()
	got, want = append([]string(nil), got...), append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s: got %d %v\nwant %d %v", what, len(got), got, len(want), want)
	}
}

func prCommitColumnNames(t *testing.T, s *GithubSource) string {
	t.Helper()
	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "pull_request_commits"})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(table.PrimaryKeys()) != "[repo pr_number sha]" || table.IncrementalKey() != "pr_updated_at" || table.Strategy() != config.StrategyMerge {
		t.Fatalf("pk=%v key=%q strategy=%q", table.PrimaryKeys(), table.IncrementalKey(), table.Strategy())
	}
	sch, err := table.GetSchema(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range sch.Columns {
		names = append(names, c.Name)
	}
	return strings.Join(names, " ")
}

func TestGithubPRCommitsTable(t *testing.T) {
	s := connectPRFake(t, newPRFake(t))
	want := "repo pr_number sha pr_state pr_url pr_title pr_author pr_created_at pr_updated_at pr_closed_at pr_merged_at base_ref head_ref head_repo merge_commit_sha commit_url message author_name author_email author_login authored_at committer_name committer_email committer_login committed_at parent_count verified verification_reason signed_by_github replaced_by_force_push replaced_at"
	if got := prCommitColumnNames(t, s); got != want {
		t.Fatalf("columns=%s", got)
	}
	for _, c := range prCommitFields {
		if strings.HasSuffix(c.Name, "_at") {
			if typ, ok := schema.DataTypeToArrowType(c).(*arrow.TimestampType); !ok || typ.Unit != arrow.Microsecond {
				t.Fatalf("%s maps to %v", c.Name, schema.DataTypeToArrowType(c))
			}
		}
	}
}

func TestGithubPRCommitsStatesAndColumns(t *testing.T) {
	f := newPRFake(t)
	main := f.g.chain("", 3)
	openHead := f.g.chain(main, 2)
	closedHead := f.g.chain(main, 1)
	mergedHead := f.g.chain(main, 3)
	mergeCommit := f.g.add(main, mergedHead)
	f.addPR("o/r", &fakePR{number: 1, base: main, head: openHead, headRepo: "fork/r", updated: "2026-11-12T00:00:00Z"})
	f.addPR("o/r", &fakePR{number: 2, state: "CLOSED", base: main, head: closedHead, closed: "2026-11-11T00:00:00Z", updated: "2026-11-11T00:00:00Z"})
	f.addPR("o/r", &fakePR{number: 3, state: "MERGED", base: main, head: mergedHead, mergeCommit: mergeCommit, merged: "2026-11-10T00:00:00Z", closed: "2026-11-10T00:00:00Z", headRepo: "o/r"})
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustEqualSet(t, "open", shasOf(rows, 1), f.g.between(main, openHead))
	mustEqualSet(t, "closed", shasOf(rows, 2), f.g.between(main, closedHead))
	mustEqualSet(t, "merged", shasOf(rows, 3), f.g.between(main, mergedHead))
	if len(rows) != 6 {
		t.Fatalf("rows=%d want 6", len(rows))
	}
	byPR := map[int64]prRow{}
	for _, r := range rows {
		byPR[r["pr_number"].(int64)] = r
		if r["repo"] != "o/r" || r["verified"] != false || r["verification_reason"] != "unsigned" || r["signed_by_github"] != false || r["parent_count"] != int64(1) {
			t.Fatalf("row %v", r)
		}
		if r["replaced_by_force_push"] != false || r["replaced_at"] != nil {
			t.Fatalf("row %v", r)
		}
		sha := r["sha"].(string)
		if r["committed_at"] != f.g.date(sha) || r["authored_at"] != f.g.date(sha) || r["commit_url"] != "https://github.com/o/r/commit/"+sha || r["author_login"] != "dev" {
			t.Fatalf("commit columns %v", r)
		}
	}
	if r := byPR[1]; r["pr_state"] != "open" || r["head_repo"] != "fork/r" || r["merge_commit_sha"] != nil || r["pr_closed_at"] != nil || r["pr_url"] != "https://github.com/o/r/pull/1" {
		t.Fatalf("open PR row %v", r)
	}
	if r := byPR[2]; r["pr_state"] != "closed" || r["head_repo"] != nil || r["pr_closed_at"] != "2026-11-11T00:00:00Z" || r["pr_merged_at"] != nil {
		t.Fatalf("closed PR (fork gone) row %v", r)
	}
	if r := byPR[3]; r["pr_state"] != "merged" || r["merge_commit_sha"] != mergeCommit || r["pr_merged_at"] != "2026-11-10T00:00:00Z" || r["base_ref"] != "main" || r["head_ref"] != "feature-3" || r["pr_updated_at"] != "2026-11-10T00:00:00Z" {
		t.Fatalf("merged PR row %v", r)
	}
	if len(f.calls(&f.compares)) != 0 {
		t.Fatalf("compare called for small pull requests: %v", f.calls(&f.compares))
	}
}

func TestGithubPRCommitsFollowUpPages(t *testing.T) {
	f := newPRFake(t)
	main := f.g.chain("", 1)
	head := f.g.chain(main, 230)
	f.addPR("o/r", &fakePR{number: 7, base: main, head: head})
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustEqualSet(t, "230 commits", shasOf(rows, 7), f.g.between(main, head))
	if got := fmt.Sprint(f.calls(&f.gqlCalls)); got != "[list o/r  commits o/r#7 100 commits o/r#7 200]" {
		t.Fatalf("graphql calls %s", got)
	}
}

func TestGithubPRCommitsGraphQLCountCheckFails(t *testing.T) {
	f := newPRFake(t)
	f.gqlNodeLimit = 5
	main := f.g.chain("", 1)
	f.addPR("o/r", &fakePR{number: 3, base: main, head: f.g.chain(main, 6)})
	logs := captureOutput(t)
	s := connectPRFake(t, f)

	_, _, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err == nil || !strings.Contains(err.Error(), "pull request o/r#3: GitHub listed 5 commits, expected 6, also when read again") {
		t.Fatalf("err=%v", err)
	}
	if got := strings.Count(logs.String(), "pull request o/r#3: GitHub listed 5 commits, expected 6, reading its commits again"); got != 1 {
		t.Fatalf("%d retry warnings, want 1:\n%s", got, logs.String())
	}
}

func TestGithubPRCommitsChangeDuringReadIsRetried(t *testing.T) {
	for _, tc := range []struct {
		name            string
		pushes, rewrite int
	}{{"push between pages", 1, 0}, {"rewrite with as many commits", 0, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPRFake(t)
			f.pushDuringReads, f.rewriteDuringReads = tc.pushes, tc.rewrite
			main := f.g.chain("", 1)
			pr := f.addPR("o/r", &fakePR{number: 7, base: main, head: f.g.chain(main, 130)})
			logs := captureOutput(t)
			s := connectPRFake(t, f)

			_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			mustEqualSet(t, "the version read last", shasOf(rows, 7), f.g.between(main, pr.head))
			if got := strings.Count(logs.String(), "reading its commits again"); got != tc.pushes+tc.rewrite {
				t.Fatalf("%d retry warnings, want %d:\n%s", got, tc.pushes+tc.rewrite, logs.String())
			}
		})
	}
}

func TestGithubPRCommitsUpdatedPastTheEndDuringReadIsSkipped(t *testing.T) {
	f := newPRFake(t)
	f.pushDuringReads = 1
	f.updatedOnChange = "2026-11-12T00:00:00Z"
	main := f.g.chain("", 1)
	f.addPR("o/r", &fakePR{number: 7, base: main, head: f.g.chain(main, 130), updated: "2026-11-10T00:00:00Z"})
	logs := captureOutput(t)
	s := connectPRFake(t, f)

	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("%d rows written for a pull request updated after the interval end while it was read", len(rows))
	}
	if !strings.Contains(logs.String(), "pull request o/r#7 was updated after the interval while it was read, skipping it") {
		t.Fatalf("logs:\n%s", logs.String())
	}
}

func TestGithubPRCommitsChangingOnEveryReadFails(t *testing.T) {
	f := newPRFake(t)
	f.pushDuringReads = 4
	main := f.g.chain("", 1)
	f.addPR("o/r", &fakePR{number: 7, base: main, head: f.g.chain(main, 130)})
	logs := captureOutput(t)
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err == nil || !strings.Contains(err.Error(), "pull request o/r#7: its head changed while its commits were read, also when read again") {
		t.Fatalf("err=%v", err)
	}
	if len(rows) != 0 || strings.Count(logs.String(), "reading its commits again (retry 1 of 1)") != 1 {
		t.Fatalf("rows=%d logs:\n%s", len(rows), logs.String())
	}
}

func TestGithubPRCommitsOver250ReadThroughCompare(t *testing.T) {
	for _, shape := range []string{"open", "fast-forward merge"} {
		t.Run(shape, func(t *testing.T) {
			f := newPRFake(t)
			main := f.g.chain("", 2)
			head := f.g.chain(main, 300)
			pr := &fakePR{number: 9, base: main, head: head}
			if shape != "open" {
				pr.state, pr.mergeCommit, pr.merged = "MERGED", head, "2026-11-09T00:00:00Z"
			}
			f.addPR("o/r", pr)
			s := connectPRFake(t, f)

			_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			mustEqualSet(t, "300 commits", shasOf(rows, 9), f.g.between(main, head))
			for _, c := range f.calls(&f.compares) {
				if !strings.HasPrefix(c, main+"...") {
					t.Fatalf("compare %s does not start from baseRefOid %s", c, main)
				}
			}
			if got := fmt.Sprint(f.calls(&f.gqlCalls)); got != "[list o/r  signatures o/r 100 signatures o/r 100 signatures o/r 100]" {
				t.Fatalf("GraphQL calls for a pull request over 250 commits, want no commit pages and signatures 100 per query: %s", got)
			}
		})
	}
}

// A pull request that grows past 250 commits while it is read is read through compare instead.
func TestGithubPRCommitsGrowingPast250DuringReadUsesCompare(t *testing.T) {
	f := newPRFake(t)
	f.pushDuringReads = 1
	f.updatedOnChange = "2026-11-11T00:00:00Z"
	main := f.g.chain("", 1)
	pr := f.addPR("o/r", &fakePR{number: 8, base: main, head: f.g.chain(main, 250)})
	captureOutput(t)
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustEqualSet(t, "the head read last, through compare", shasOf(rows, 8), f.g.between(main, pr.head))
	if len(rows) != 251 {
		t.Fatalf("rows=%d want 251", len(rows))
	}
	for _, r := range rows {
		if r["pr_updated_at"] != "2026-11-11T00:00:00Z" {
			t.Fatalf("row %v", r)
		}
	}
	if len(f.calls(&f.compares)) == 0 {
		t.Fatal("not read through compare")
	}
}

func TestGithubPRCommitsCompareCountCheckFails(t *testing.T) {
	f := newPRFake(t)
	main := f.g.chain("", 1)
	head := f.g.chain(main, 260)
	f.addPR("o/r", &fakePR{number: 3, base: main, head: head})
	f.aheadExtra = 1
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err == nil || !strings.Contains(err.Error(), "found 260 commits, GitHub counts 261") {
		t.Fatalf("err=%v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows written for an incomplete pull request: %d", len(rows))
	}
}

// splitShape builds the sandbox PR #45 shape small: two interleaved lines merged, main moves and
// is merged in, then a few more commits. compareCap below the PR size forces a split.
func splitShape(f *prFake) (base, head string) {
	base0 := f.g.chain("", 2)
	x, y := base0, base0
	for i := 0; i < 40; i++ {
		x = f.g.add(x)
		y = f.g.add(y)
	}
	m1 := f.g.add(x, y)
	mainNew := f.g.add(base0)
	m2 := f.g.add(m1, mainNew)
	return mainNew, f.g.chain(m2, 15)
}

func compareAllSHAs(t *testing.T, s *GithubSource, base, head string) ([]string, int) {
	t.Helper()
	commits, behind, err := s.compareAll(context.Background(), "o/r", base, head)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range commits {
		got = append(got, c.SHA)
	}
	return got, behind
}

func TestGithubPRCommitsSplitBeyondCompareCut(t *testing.T) {
	f := newPRFake(t)
	f.compareCap = 30
	base, head := splitShape(f)
	s := connectPRFake(t, f)

	got, behind := compareAllSHAs(t, s, base, head)
	mustEqualSet(t, "split", got, f.g.between(base, head))
	if behind != 0 {
		t.Fatalf("behind_by=%d", behind)
	}
	for _, c := range f.calls(&f.compares) {
		if c == base+"..."+base {
			t.Fatalf("the base was compared with itself: %v", f.calls(&f.compares))
		}
	}
	if len(f.calls(&f.compares)) < 3 {
		t.Fatalf("no split happened: %v", f.calls(&f.compares))
	}
}

// The compare cuts a long line merged into the pull request; only the merge's second parent leads to it.
func TestGithubPRCommitsSplitThroughSecondParent(t *testing.T) {
	f := newPRFake(t)
	f.compareCap = 26
	base := f.g.chain("", 1)
	old := f.g.chain(base, 40)
	recent := f.g.chain(base, 20)
	head := f.g.chain(f.g.add(recent, old), 5)
	s := connectPRFake(t, f)

	got, _ := compareAllSHAs(t, s, base, head)
	mustEqualSet(t, "split through the second parent", got, f.g.between(base, head))
}

func TestGithubPRCommitsSplitUnrelatedHistory(t *testing.T) {
	f := newPRFake(t)
	f.compareCap = 30
	main := f.g.chain("", 3)
	legacy := f.g.chain("", 120)
	merged := f.g.add(main, legacy)
	head := f.g.chain(merged, 20)
	s := connectPRFake(t, f)

	got, _ := compareAllSHAs(t, s, main, head)
	mustEqualSet(t, "unrelated history", got, f.g.between(main, head))
	if len(f.calls(&f.histories)) == 0 {
		t.Fatalf("the unrelated history was not listed")
	}
}

// forcePushShape: V0 = 10 commits on base; F1 amends the last 3 (V1); F2 drops the last 5 of V1 and
// adds 2 (final). Git truth: F1 replaced c8-c10 of V0, F2 replaced the 3 amended and c6-c7.
func forcePushShape(f *prFake, base string) (v0, v1, final string) {
	v0 = f.g.chain(base, 10)
	keep := v0
	for i := 0; i < 3; i++ {
		keep = f.g.parents[keep][0]
	}
	v1 = f.g.chain(keep, 3)
	keep = v1
	for i := 0; i < 5; i++ {
		keep = f.g.parents[keep][0]
	}
	return v0, v1, f.g.chain(keep, 2)
}

func replacedAtOf(rows []prRow, pr int64) map[string]string {
	out := map[string]string{}
	for _, r := range rows {
		if r["pr_number"] == pr && r["replaced_by_force_push"] == true {
			out[r["sha"].(string)] = fmt.Sprint(r["replaced_at"])
		}
	}
	return out
}

func TestGithubPRCommitsForcePushes(t *testing.T) {
	cases := []struct {
		name  string
		build func(f *prFake) (prBase, v0, v1, final string)
		want  int
		cheap bool
	}{
		{name: "base inside final head", want: 8, cheap: true, build: func(f *prFake) (string, string, string, string) {
			base := f.g.chain("", 2)
			v0, v1, final := forcePushShape(f, base)
			return base, v0, v1, final
		}},
		{name: "base moved after the branch", want: 8, build: func(f *prFake) (string, string, string, string) {
			base := f.g.chain("", 2)
			v0, v1, final := forcePushShape(f, base)
			return f.g.add(base), v0, v1, final
		}},
		{name: "final rebased onto an older base", want: 13, build: func(f *prFake) (string, string, string, string) {
			base := f.g.chain("", 2)
			prBase := f.g.add(base)
			v0, v1, _ := forcePushShape(f, prBase)
			return prBase, v0, v1, f.g.chain(base, 4)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPRFake(t)
			prBase, v0, v1, final := tc.build(f)
			f.addPR("o/r", &fakePR{number: 4, base: prBase, head: final, pushes: []fakeForcePush{
				{at: "2026-11-05T10:00:00Z", before: v0},
				{at: "2026-11-04T10:00:00Z", before: ""},
				{at: "2026-11-06T10:00:00Z", before: v1},
			}})
			logs := captureOutput(t)
			s := connectPRFake(t, f)

			_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			finalSet := f.g.between(prBase, final)
			mustEqualSet(t, "final", shasOf(rows, 4), finalSet)
			inFinal := map[string]bool{}
			for _, sha := range finalSet {
				inFinal[sha] = true
			}
			wantAt := map[string]string{}
			for _, sha := range f.g.between(prBase, v0) {
				if !inFinal[sha] {
					wantAt[sha] = "2026-11-05T10:00:00Z"
				}
			}
			for _, sha := range f.g.between(prBase, v1) {
				if !inFinal[sha] {
					wantAt[sha] = "2026-11-06T10:00:00Z"
				}
			}
			if gotAt := replacedAtOf(rows, 4); fmt.Sprint(gotAt) != fmt.Sprint(wantAt) || len(wantAt) != tc.want {
				t.Fatalf("replaced:\n got %v\nwant %v", gotAt, wantAt)
			}
			if !strings.Contains(logs.String(), "does not show the head replaced by the force push at 2026-11-04T10:00:00Z in pull request o/r#4") {
				t.Fatalf("no warning for the force push without a replaced head:\n%s", logs.String())
			}
			cheap := 0
			for _, c := range f.calls(&f.compares) {
				if strings.HasPrefix(c, final+"...") {
					cheap++
				}
			}
			if tc.cheap != (cheap == 2) || (!tc.cheap && cheap != 0) {
				t.Fatalf("compares %v", f.calls(&f.compares))
			}
		})
	}
}

// A pull request force-pushed away from its head and later back to it: one replaced head is the final head itself.
func TestGithubPRCommitsForcePushBackToFinalHead(t *testing.T) {
	f := newPRFake(t)
	base := f.g.chain("", 1)
	v0, _, final := forcePushShape(f, base)
	f.addPR("o/r", &fakePR{number: 6, base: base, head: final, pushes: []fakeForcePush{
		{at: "2026-11-05T10:00:00Z", before: final},
		{at: "2026-11-06T10:00:00Z", before: v0},
	}})
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(replacedAtOf(rows, 6)); got != 5 {
		t.Fatalf("replaced rows=%d want 5", got)
	}
}

func TestGithubPRCommitsPageWithoutCursorFails(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		build      func(f *prFake, main string)
	}{
		{"commits", "pull request o/r#7: GitHub reported more commits without a cursor to read them", func(f *prFake, main string) {
			f.addPR("o/r", &fakePR{number: 7, base: main, head: f.g.chain(main, 130)})
		}},
		{"force pushes", "pull request o/r#7: GitHub reported more force pushes without a cursor to read them", func(f *prFake, main string) {
			f.timelinePageSize = 1
			v0, v1, final := forcePushShape(f, main)
			f.addPR("o/r", &fakePR{number: 7, base: main, head: final, pushes: []fakeForcePush{
				{at: "2026-11-05T10:00:00Z", before: v0},
				{at: "2026-11-06T10:00:00Z", before: v1},
			}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPRFake(t)
			f.noCursor = true
			tc.build(f, f.g.chain("", 1))
			captureOutput(t)
			s := connectPRFake(t, f)

			done := make(chan error, 1)
			go func() {
				_, _, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("err=%v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("paging without a cursor did not stop")
			}
		})
	}
}

// Compare rows take signed_by_github from GraphQL: a commit GitHub signed with someone else as committer
// (e.g. made in a Codespace) is true, and a web-flow commit GitHub says it didn't sign is false.
func TestGithubPRCommitsSignedByGitHubOnCompareRowsComesFromGraphQL(t *testing.T) {
	f := newPRFake(t)
	base := f.g.chain("", 1)
	v0, v1, final := forcePushShape(f, base)
	f.committers[v0] = fakeCommitter{name: "Dev", email: "dev@example.com", gqlLogin: "dev", restLogin: "dev", state: "VALID", reason: "valid", valid: true, byGitHub: true}
	f.committers[v1] = fakeCommitter{name: "GitHub", email: "noreply@github.com", restLogin: "web-flow", state: "VALID", reason: "valid", valid: true}
	f.addPR("o/r", &fakePR{number: 4, base: base, head: final, pushes: []fakeForcePush{
		{at: "2026-11-05T10:00:00Z", before: v0},
		{at: "2026-11-06T10:00:00Z", before: v1},
	}})
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]interface{}{}
	for _, r := range rows {
		got[r["sha"].(string)] = r["signed_by_github"]
	}
	if got[v0] != true || got[v1] != false {
		t.Fatalf("signed_by_github: GitHub-signed commit with a user committer=%v (want true), web-flow commit GitHub didn't sign=%v (want false)", got[v0], got[v1])
	}
}

func TestGithubPRCommitsSignatureNotFoundKeepsTheGuess(t *testing.T) {
	f := newPRFake(t)
	base := f.g.chain("", 1)
	v0, v1, final := forcePushShape(f, base)
	f.committers[v0] = fakeCommitter{name: "GitHub", email: "noreply@github.com", restLogin: "web-flow", state: "VALID", reason: "valid", valid: true, byGitHub: true}
	f.unknownSignatures = map[string]bool{v0: true}
	f.addPR("o/r", &fakePR{number: 4, base: base, head: final, pushes: []fakeForcePush{
		{at: "2026-11-05T10:00:00Z", before: v0},
		{at: "2026-11-06T10:00:00Z", before: v1},
	}})
	logs := captureOutput(t)
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r["sha"] == v0 && r["signed_by_github"] != true {
			t.Fatalf("a commit GitHub can't find loses the inferred value: %v", r)
		}
	}
	if !strings.Contains(logs.String(), "GitHub did not find 1 commits of pull request o/r#4 when reading their signatures") {
		t.Fatalf("logs:\n%s", logs.String())
	}
}

// Force pushes are read page by page; a push and a force push in between belong to a newer version,
// so the whole pull request is read again and the rows describe that version only.
func TestGithubPRCommitsForcePushDuringPagingIsRetried(t *testing.T) {
	f := newPRFake(t)
	f.timelinePageSize = 1
	f.forcePushesDuringPushPages = 1
	f.updatedOnChange = "2026-11-11T00:00:00Z"
	base := f.g.chain("", 1)
	v0, v1, final := forcePushShape(f, base)
	pr := f.addPR("o/r", &fakePR{number: 6, base: base, head: final, updated: "2026-11-10T00:00:00Z", pushes: []fakeForcePush{
		{at: "2026-11-05T10:00:00Z", before: v0},
		{at: "2026-11-06T10:00:00Z", before: v1},
	}})
	logs := captureOutput(t)
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustEqualSet(t, "final version read last", shasOf(rows, 6), f.g.between(base, pr.head))
	pushedThenReplaced := pr.pushes[2].before
	at := replacedAtOf(rows, 6)
	if at[pushedThenReplaced] != pr.pushes[2].at || len(at) != 9 {
		t.Fatalf("replaced %v, want the 8 earlier ones and %s at %s", at, pushedThenReplaced, pr.pushes[2].at)
	}
	for _, r := range rows {
		if r["pr_updated_at"] != "2026-11-11T00:00:00Z" {
			t.Fatalf("row keeps the details from before the force push: %v", r)
		}
	}
	if !strings.Contains(logs.String(), "pull request o/r#6: its head changed while its force pushes were read, reading it again (retry 1 of 1)") {
		t.Fatalf("logs:\n%s", logs.String())
	}
}

func TestGithubPRCommitsForcePushDuringPagingPastTheEndIsSkipped(t *testing.T) {
	f := newPRFake(t)
	f.timelinePageSize = 1
	f.forcePushesDuringPushPages = 1
	f.updatedOnChange = "2026-11-12T00:00:00Z"
	base := f.g.chain("", 1)
	v0, v1, final := forcePushShape(f, base)
	f.addPR("o/r", &fakePR{number: 6, base: base, head: final, updated: "2026-11-10T00:00:00Z", pushes: []fakeForcePush{
		{at: "2026-11-05T10:00:00Z", before: v0},
		{at: "2026-11-06T10:00:00Z", before: v1},
	}})
	logs := captureOutput(t)
	s := connectPRFake(t, f)

	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows=%d err=%v, want the pull request skipped", len(rows), err)
	}
	if !strings.Contains(logs.String(), "pull request o/r#6 was updated after the interval while it was read, skipping it") {
		t.Fatalf("logs:\n%s", logs.String())
	}
}

func TestGithubPRCommitsForcePushesChangingOnEveryReadFails(t *testing.T) {
	f := newPRFake(t)
	f.timelinePageSize = 1
	f.forcePushesDuringPushPages = 100
	base := f.g.chain("", 1)
	v0, v1, final := forcePushShape(f, base)
	f.addPR("o/r", &fakePR{number: 6, base: base, head: final, pushes: []fakeForcePush{
		{at: "2026-11-05T10:00:00Z", before: v0},
		{at: "2026-11-06T10:00:00Z", before: v1},
	}})
	logs := captureOutput(t)
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err == nil || !strings.Contains(err.Error(), "pull request o/r#6: its head changed while its force pushes were read, also when read again") {
		t.Fatalf("err=%v", err)
	}
	if len(rows) != 0 || strings.Count(logs.String(), "while its force pushes were read, reading it again") != 1 {
		t.Fatalf("rows=%d logs:\n%s", len(rows), logs.String())
	}
}

// The worst case that still succeeds: the commit pages change, then the force pushes, then the commit
// pages of that re-read. The pull request is read 4 times in all (the listing and 3 re-reads), the
// most it can be, and its rows describe the last read.
func TestGithubPRCommitsChangesInBothPhasesAreReadAtMostFourTimes(t *testing.T) {
	f := newPRFake(t)
	f.pushOnReads = map[int]bool{1: true, 3: true}
	f.timelinePageSize = 1
	f.forcePushesDuringPushPages = 1
	base := f.g.chain("", 1)
	v0, v1, _ := forcePushShape(f, base)
	pr := f.addPR("o/r", &fakePR{number: 6, base: base, head: f.g.chain(base, 130), pushes: []fakeForcePush{
		{at: "2026-11-05T10:00:00Z", before: v0},
		{at: "2026-11-06T10:00:00Z", before: v1},
	}})
	logs := captureOutput(t)
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustEqualSet(t, "final version read last", shasOf(rows, 6), f.g.between(base, pr.head))
	rereads := 0
	for _, c := range f.calls(&f.gqlCalls) {
		if strings.HasPrefix(c, "pr o/r#6") {
			rereads++
		}
	}
	if rereads != 3 {
		t.Fatalf("pull request read again %d times, want 3: %v\n%s", rereads, f.calls(&f.gqlCalls), logs.String())
	}
}

func TestGithubPRCommitsReplacedHeadUnavailable(t *testing.T) {
	for _, tc := range []struct {
		status  int
		wantErr bool
	}{{http.StatusNotFound, false}, {http.StatusUnprocessableEntity, false}, {http.StatusForbidden, true}} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			f := newPRFake(t)
			base := f.g.chain("", 1)
			v0, v1, final := forcePushShape(f, base)
			f.compareStatus[v0] = tc.status
			f.addPR("o/r", &fakePR{number: 5, base: base, head: final, pushes: []fakeForcePush{
				{at: "2026-11-05T10:00:00Z", before: v0},
				{at: "2026-11-06T10:00:00Z", before: v1},
			}})
			logs := captureOutput(t)
			s := connectPRFake(t, f)

			_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "replaced by the force push at 2026-11-05T10:00:00Z") {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(logs.String(), "skipping the version of pull request o/r#5 replaced by the force push at 2026-11-05T10:00:00Z") {
				t.Fatalf("no warning:\n%s", logs.String())
			}
			if got := len(replacedAtOf(rows, 5)); got != 5 {
				t.Fatalf("replaced rows from the readable version=%d want 5", got)
			}
		})
	}
}

func TestGithubPRCommitsForcePushPages(t *testing.T) {
	f := newPRFake(t)
	f.timelinePageSize = 1
	base := f.g.chain("", 1)
	v0, v1, final := forcePushShape(f, base)
	f.addPR("o/r", &fakePR{number: 6, base: base, head: final, pushes: []fakeForcePush{
		{at: "2026-11-05T10:00:00Z", before: v0},
		{at: "2026-11-06T10:00:00Z", before: v1},
	}})
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(replacedAtOf(rows, 6)); got != 8 {
		t.Fatalf("replaced rows=%d want 8 (calls %v)", got, f.calls(&f.gqlCalls))
	}
	if got := fmt.Sprint(f.calls(&f.gqlCalls)); got != "[list o/r  pushes o/r#6 1 signatures o/r 8]" {
		t.Fatalf("graphql calls %s", got)
	}
}

func TestGithubPRCommitsInterval(t *testing.T) {
	f := newPRFake(t)
	f.listPageSize = 2
	main := f.g.chain("", 1)
	for i, pr := range []*fakePR{
		{number: 1, updated: "2026-11-10T00:00:00Z", created: "2026-11-02T00:00:00Z"},
		{number: 6, updated: "2026-11-04T00:00:00Z", created: "2026-11-01T00:00:00Z"},
		{number: 2, updated: "2026-11-03T00:00:00Z", created: "2026-10-25T00:00:00Z"},
		{number: 3, updated: "2026-11-01T00:00:00Z", created: "2026-10-20T00:00:00Z"},
		{number: 4, updated: "2026-10-31T23:59:59Z", created: "2026-10-01T00:00:00Z"},
		{number: 5, updated: "2026-10-10T00:00:00Z", created: "2026-10-01T00:00:00Z"},
		{number: 7, updated: "2026-10-05T00:00:00Z", created: "2026-10-01T00:00:00Z"},
	} {
		pr.base, pr.head = main, f.g.chain(main, i+1)
		f.addPR("o/r", pr)
	}
	s := connectPRFake(t, f)

	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
	if err != nil {
		t.Fatal(err)
	}
	prs := map[int64]bool{}
	for _, r := range rows {
		prs[r["pr_number"].(int64)] = true
		updated, _ := time.Parse(time.RFC3339, r["pr_updated_at"].(string))
		if updated.Before(start) || !updated.Before(end) {
			t.Fatalf("row with pr_updated_at %s outside [%s, %s): %v", updated, start, end, r)
		}
	}
	if fmt.Sprint(prs) != "map[2:true 3:true]" {
		t.Fatalf("pull requests read %v, want 2 (updated in the interval) and 3 (updated at the start); 1 (created before the end, updated after it) and 6 (updated at the end) are skipped", prs)
	}
	if got := fmt.Sprint(f.calls(&f.gqlCalls)); got != "[list o/r  list o/r 2 list o/r 4]" {
		t.Fatalf("listing did not page past the pull requests updated after the end, or did not stop at the first one updated before the start: %s", got)
	}

	f2 := newPRFake(t)
	main2 := f2.g.chain("", 1)
	f2.addPR("o/r", &fakePR{number: 8, base: main2, head: f2.g.chain(main2, 1), updated: "2020-01-01T00:00:00Z"})
	s2 := connectPRFake(t, f2)
	_, rows, err = readPRCommitTable(t, s2, "pull_request_commits", source.ReadOptions{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("without an interval every pull request is read: rows=%d err=%v", len(rows), err)
	}
}

func TestGithubPRCommitsEmitsEachPullRequestOnce(t *testing.T) {
	f := newPRFake(t)
	f.listPageSize, f.listStep = 2, 1
	main := f.g.chain("", 1)
	shared := f.g.chain(main, 1)
	f.addPR("o/r", &fakePR{number: 1, base: main, head: shared, updated: "2026-11-10T00:00:00Z"})
	f.addPR("o/r", &fakePR{number: 2, base: main, head: f.g.chain(shared, 1), updated: "2026-11-09T00:00:00Z"})
	f.addPR("o/r", &fakePR{number: 3, base: main, head: f.g.chain(main, 2), updated: "2026-11-08T00:00:00Z"})
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.key()] {
			t.Fatalf("row emitted twice: %s", r.key())
		}
		seen[r.key()] = true
	}
	if len(rows) != 5 || !seen["o/r#1 "+shared] || !seen["o/r#2 "+shared] {
		t.Fatalf("rows %v", seen)
	}
}

func TestGithubPRCommitsLimit(t *testing.T) {
	f := newPRFake(t)
	main := f.g.chain("", 1)
	f.addPR("o/r", &fakePR{number: 1, base: main, head: f.g.chain(main, 4), updated: "2026-11-10T00:00:00Z"})
	f.addPR("o/r", &fakePR{number: 2, base: main, head: f.g.chain(main, 4), updated: "2026-11-09T00:00:00Z"})
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{Limit: 3})
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
}

func TestGithubPRCommitsRepositoryModes(t *testing.T) {
	newFake := func(t *testing.T) *prFake {
		f := newPRFake(t)
		main := f.g.chain("", 1)
		for i, repo := range []string{"o/r", "o/ingestr", "o/Bruin", "other/tool", "u/app"} {
			f.addPR(repo, &fakePR{number: i + 1, base: main, head: f.g.chain(main, 1)})
		}
		return f
	}
	repos := func(rows []prRow) string {
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%v#%v", r["repo"], r["pr_number"]))
		}
		return strings.Join(out, " ")
	}

	t.Run("single", func(t *testing.T) {
		f := newFake(t)
		s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=O&repo=R")
		table, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
		if err != nil || repos(rows) != "o/r#1" || fmt.Sprint(table.PrimaryKeys()) != "[repo pr_number sha]" {
			t.Fatalf("rows=%s err=%v", repos(rows), err)
		}
	})

	t.Run("list", func(t *testing.T) {
		f := newFake(t)
		s, _ := connectURIToFake(t, f, "github://?access_token=test-token")
		_, rows, err := readPRCommitTable(t, s, "pull_request_commits:other/tool, u/app", source.ReadOptions{})
		if err != nil || repos(rows) != "other/tool#4 u/app#5" {
			t.Fatalf("rows=%s err=%v", repos(rows), err)
		}
	})

	t.Run("all skips repositories GraphQL can't find", func(t *testing.T) {
		f := newFake(t)
		f.gqlErrors["o/old"] = "NOT_FOUND"
		logs := captureOutput(t)
		s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o")
		_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
		if err != nil || repos(rows) != "o/r#1 o/ingestr#2 o/Bruin#3" {
			t.Fatalf("rows=%s err=%v", repos(rows), err)
		}
		if !strings.Contains(logs.String(), "Warning: skipping the pull request commits of o/old, GitHub answered NOT_FOUND") {
			t.Fatalf("logs:\n%s", logs.String())
		}
	})

	t.Run("owner wildcard fails when GraphQL forbids a repository", func(t *testing.T) {
		f := newFake(t)
		f.gqlErrors["o/ingestr"] = "FORBIDDEN"
		captureOutput(t)
		s, _ := connectURIToFake(t, f, "github://?access_token=test-token")
		_, _, err := readPRCommitTable(t, s, "pull_request_commits:o/*", source.ReadOptions{})
		if err == nil || !strings.Contains(err.Error(), "FORBIDDEN for o/ingestr") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("named repository GraphQL can't find fails", func(t *testing.T) {
		f := newFake(t)
		f.gqlErrors["o/r"] = "NOT_FOUND"
		s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o")
		_, _, err := readPRCommitTable(t, s, "pull_request_commits:r", source.ReadOptions{})
		if err == nil || !strings.Contains(err.Error(), "failed to list the pull requests of o/r") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("missing named repository fails before reading", func(t *testing.T) {
		f := newFake(t)
		s, _ := connectURIToFake(t, f, "github://?access_token=test-token&owner=o")
		_, _, err := readPRCommitTable(t, s, "pull_request_commits:r, nope", source.ReadOptions{})
		if err == nil || !strings.Contains(err.Error(), "not found or not accessible with this token: o/nope") || len(f.calls(&f.gqlCalls)) != 0 {
			t.Fatalf("err=%v graphql=%v", err, f.calls(&f.gqlCalls))
		}
	})

	t.Run("table errors", func(t *testing.T) {
		f := newFake(t)
		s, _ := connectURIToFake(t, f, "github://?access_token=test-token")
		for name, want := range map[string]string{
			"pull_request_commits":         "table pull_request_commits needs owner in the URI, or a list of repositories: pull_request_commits:<owner>/<repo>, or pull_request_commits:<owner>/* for every repository of an owner",
			"pull_request_commits:":        "table pull_request_commits: lists no repositories, use pull_request_commits:<repo>,<owner>/<repo>,<owner>/*",
			"pull_request_commits:r":       `repository "r" in table pull_request_commits:r has no owner`,
			"pull_request_commits:o/r/x":   `invalid repository "o/r/x" in table pull_request_commits:o/r/x`,
			"pull_request_commits:o/bru-*": `a wildcard can only be <owner>/*`,
			"pull_requests:o/r":            "unsupported table: pull_requests:o/r",
		} {
			if _, err := s.GetTable(context.Background(), source.TableRequest{Name: name}); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("GetTable(%q) err=%v want %q", name, err, want)
			}
		}
	})
}

func TestGithubPRCommitsNeedAToken(t *testing.T) {
	f := newPRFake(t)
	s, _ := connectURIToFake(t, f, "github://?owner=o&repo=r")
	_, _, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err == nil || !strings.Contains(err.Error(), "the pull_request_commits table reads GitHub's GraphQL API, which needs an access_token") {
		t.Fatalf("err=%v", err)
	}
}

// GitHub's schema allows a commit without an author or committer, and a Git actor without a name,
// email or date: such fields are NULL, never an empty string.
func TestGithubPRCommitsMissingActorFieldsAreNull(t *testing.T) {
	var c gqlCommit
	raw := `{"oid":"c1","url":"u","message":"m","author":null,"committer":{"name":null,"email":"","date":null,"user":null},"parents":{"totalCount":1},"signature":null}`
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	row := graphQLCommitRow(c)
	for _, k := range []string{"author_name", "author_email", "author_login", "authored_at", "committer_name", "committer_email", "committer_login", "committed_at"} {
		if row[k] != nil {
			t.Errorf("%s=%#v want nil", k, row[k])
		}
	}
}

func TestGithubPRCommitsMissingActorFieldsMatchInCompareRows(t *testing.T) {
	var g gqlCommit
	var r restCommit
	gqlRaw := `{"oid":"c1","url":"u","message":"m","author":null,"committer":{"name":null,"email":"","date":null,"user":null},"parents":{"totalCount":1},"signature":null}`
	restRaw := `{"sha":"c1","html_url":"u","commit":{"message":"m","author":null,"committer":{"name":"","email":""},"verification":{"verified":false,"reason":"unsigned"}},"author":null,"committer":{},"parents":[{"sha":"c0"}]}`
	if err := json.Unmarshal([]byte(gqlRaw), &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(restRaw), &r); err != nil {
		t.Fatal(err)
	}
	fromGraphQL, fromCompare := graphQLCommitRow(g), restCommitRow(r)
	if fmt.Sprint(fromGraphQL) != fmt.Sprint(fromCompare) {
		t.Fatalf("\ngraphql %v\ncompare %v", fromGraphQL, fromCompare)
	}
	for _, k := range []string{"author_name", "author_email", "author_login", "authored_at", "committer_name", "committer_email", "committer_login", "committed_at"} {
		if fromCompare[k] != nil {
			t.Errorf("%s=%#v want nil", k, fromCompare[k])
		}
	}
}

func TestGithubPRCommitsSignatureReason(t *testing.T) {
	for state, want := range map[string]string{
		"VALID": "valid", "INVALID": "invalid", "MALFORMED_SIG": "malformed_signature", "UNKNOWN_KEY": "unknown_key",
		"BAD_EMAIL": "bad_email", "UNVERIFIED_EMAIL": "unverified_email", "NO_USER": "no_user",
		"UNKNOWN_SIG_TYPE": "unknown_signature_type", "UNSIGNED": "unsigned", "GPGVERIFY_UNAVAILABLE": "gpgverify_unavailable",
		"GPGVERIFY_ERROR": "gpgverify_error", "NOT_SIGNING_KEY": "not_signing_key", "EXPIRED_KEY": "expired_key",
		"OCSP_PENDING": "ocsp_pending", "OCSP_ERROR": "ocsp_error", "BAD_CERT": "bad_cert", "OCSP_REVOKED": "ocsp_revoked",
	} {
		if got := signatureReason(state); got != want {
			t.Errorf("signatureReason(%s)=%s want %s", state, got, want)
		}
	}
}

// GraphQL shows commits GitHub made itself without an account; the row says web-flow, as REST does.
func TestGithubPRCommitsGitHubMadeCommits(t *testing.T) {
	f := newPRFake(t)
	main := f.g.chain("", 1)
	signed := f.g.add(main)
	byGitHub := f.g.add(signed, main)
	f.committers[signed] = fakeCommitter{name: "Dev", email: "dev@example.com", gqlLogin: "dev", state: "UNKNOWN_KEY"}
	f.committers[byGitHub] = fakeCommitter{name: "GitHub", email: "noreply@github.com", state: "VALID", valid: true, byGitHub: true}
	f.addPR("o/r", &fakePR{number: 1, base: main, head: byGitHub})
	s := connectPRFake(t, f)

	_, rows, err := readPRCommitTable(t, s, "pull_request_commits", source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r["sha"].(string)] = fmt.Sprintf("%v %v %v %v %v", r["committer_login"], r["verified"], r["verification_reason"], r["signed_by_github"], r["parent_count"])
	}
	if got[signed] != "dev false unknown_key false 1" || got[byGitHub] != "web-flow true valid true 2" {
		t.Fatalf("rows %v", got)
	}
}

// Rows read through GraphQL and through compare must match column for column, GitHub's own
// commits included: GraphQL shows them without an account, REST as web-flow.
func TestGithubPRCommitsGraphQLAndCompareRowsMatch(t *testing.T) {
	f := newPRFake(t)
	main := f.g.chain("", 1)
	plain := f.g.add(main)
	signed := f.g.add(plain)
	byGitHub := f.g.add(signed, main)
	userSigned := f.g.add(byGitHub)
	f.committers[userSigned] = fakeCommitter{name: "Dev", email: "dev@example.com", gqlLogin: "dev", restLogin: "dev", state: "VALID", reason: "valid", valid: true}
	f.committers[signed] = fakeCommitter{name: "Dev", email: "dev@example.com", gqlLogin: "dev", restLogin: "dev", state: "UNKNOWN_KEY", reason: "unknown_key"}
	f.committers[byGitHub] = fakeCommitter{name: "GitHub", email: "noreply@github.com", restLogin: "web-flow", state: "VALID", reason: "valid", valid: true, byGitHub: true}

	for _, sha := range []string{plain, signed, byGitHub, userSigned} {
		var g gqlCommit
		var r restCommit
		gb, _ := json.Marshal(f.gqlCommit(sha))
		rb, _ := json.Marshal(f.restCommit(sha))
		if err := json.Unmarshal(gb, &g); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(rb, &r); err != nil {
			t.Fatal(err)
		}
		fromGraphQL, fromCompare := graphQLCommitRow(g), restCommitRow(r)
		if fmt.Sprint(fromGraphQL) != fmt.Sprint(fromCompare) {
			t.Fatalf("%s:\ngraphql %v\ncompare %v", sha, fromGraphQL, fromCompare)
		}
	}
}
