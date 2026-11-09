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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/output"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
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

type cannedResponse struct {
	status  int
	headers map[string]string
	body    string
}

func epoch(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }

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
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	prevREST, prevGraphQL := restApiBaseUrl, graphqlBaseUrl
	restApiBaseUrl, graphqlBaseUrl = srv.URL, srv.URL+"/graphql"
	t.Cleanup(func() { restApiBaseUrl, graphqlBaseUrl = prevREST, prevGraphQL })

	prevRetryWait, prevSecondaryWait := retryWait, secondaryRateLimitWait
	retryWait, secondaryRateLimitWait = time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { retryWait, secondaryRateLimitWait = prevRetryWait, prevSecondaryWait })

	s := NewGitHubSource()
	if err := s.Connect(context.Background(), "github://?access_token=test-token&owner=o&repo=r"); err != nil {
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
	results, err := s.read(ctx, table, source.ReadOptions{})
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

func TestGithubGraphQLTransientAnswersAreRetried(t *testing.T) {
	for name, first := range map[string]cannedResponse{
		"empty body":           {200, nil, ``},
		"unparsable body":      {200, nil, `{"data":{"repository":`},
		"no data, no errors":   {200, nil, `{"data":null}`},
		"something went wrong": {200, nil, `{"data":null,"errors":[{"message":"Something went wrong while executing your query. This may be the result of a timeout, or it could be a GitHub bug."}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			captureOutput(t)
			f := &fakeGitHub{graphql: []cannedResponse{first, issuesPage("c1", 1), issuesPage("")}}
			s, _ := connectToFake(t, f)

			rows, err := readTable(t, s, context.Background(), "issues")
			if err != nil || rows != 1 {
				t.Fatalf("rows=%d err=%v, want the answer retried", rows, err)
			}
			if len(f.cursors) != 3 {
				t.Fatalf("graphql requests=%d, want 3 (one retry)", len(f.cursors))
			}
		})
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
	page2 := srv.URL + "/repos/o/r/events?page=2&per_page=100"
	f.rest = []cannedResponse{
		{200, map[string]string{"Link": "<" + page2 + `>; rel="next"`}, `[{"id":"a","created_at":"2026-01-02T00:00:00Z"}]`},
		restQuotaSpent(),
		{200, nil, `[{"id":"b","created_at":"2026-01-01T00:00:00Z"}]`},
	}

	rows, err := readTable(t, s, context.Background(), "repo_events")
	if err != nil || rows != 2 {
		t.Fatalf("rows=%d err=%v, want both events", rows, err)
	}
	if len(f.restPaths) != 3 || f.restPaths[1] != f.restPaths[2] {
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

	if _, err := readTable(t, s, context.Background(), "repo_events"); err == nil {
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
		{429, map[string]string{"Retry-After": "0"}, `{"message":"You have exceeded a secondary rate limit."}`},
		{200, nil, `[{"id":"a","created_at":"2026-01-01T00:00:00Z"}]`},
	}}
	s, _ := connectToFake(t, f)
	retryWait = 100 * time.Millisecond

	start := time.Now()
	rows, err := readTable(t, s, context.Background(), "repo_events")
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
		{503, map[string]string{"Retry-After": "0"}, `{"message":"Service unavailable"}`},
		{200, nil, `[{"id":"a","created_at":"2026-01-01T00:00:00Z"}]`},
	}}
	s, _ := connectToFake(t, f)

	rows, err := readTable(t, s, context.Background(), "repo_events")
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
		secondary, secondary, secondary,
		{200, nil, `[{"id":"a","created_at":"2026-01-01T00:00:00Z"}]`},
	}}
	s, _ := connectToFake(t, f)

	rows, err := readTable(t, s, context.Background(), "repo_events")
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
