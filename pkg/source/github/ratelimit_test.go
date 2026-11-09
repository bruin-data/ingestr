package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/output"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

type cannedResponse struct {
	status  int
	headers map[string]string
	body    string
}

func epoch(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }

// The exact body GitHub returned when we exhausted a token's GraphQL quota live.
const liveGraphQLRateLimitBody = `{"errors":[{"type":"RATE_LIMIT","code":"graphql_rate_limit","message":"API rate limit already exceeded for user ID 55869347."}]}`

func graphQLQuotaSpent() cannedResponse {
	return cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(time.Now().Add(-time.Minute))}, liveGraphQLRateLimitBody}
}

func restQuotaSpent() cannedResponse {
	return cannedResponse{403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(time.Now().Add(-time.Minute))}, `{"message":"API rate limit exceeded for 1.2.3.4."}`}
}

func TestRateLimitWait(t *testing.T) {
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
		{"reset beyond an hour is capped", cannedResponse{403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(now.Add(3 * time.Hour))}, ""}, true, maxRateLimitWait, maxRateLimitWait},
		{"reset already passed waits a second, not zero", cannedResponse{403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": epoch(now.Add(-time.Minute))}, ""}, true, time.Second, time.Second},
		{"secondary with retry-after", cannedResponse{403, map[string]string{"Retry-After": "30", "X-RateLimit-Remaining": "4000"}, ""}, true, 30 * time.Second, 30 * time.Second},
		{"secondary without headers", cannedResponse{403, nil, `{"message":"You have exceeded a secondary rate limit."}`}, true, time.Minute, time.Minute},
		{"graphql primary 200", cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": in10m}, liveGraphQLRateLimitBody}, true, 9 * time.Minute, 11 * time.Minute},
		{"graphql secondary 200", cannedResponse{200, map[string]string{"X-RateLimit-Remaining": "4000"}, `{"errors":[{"message":"You have exceeded a secondary rate limit. Please wait a few minutes."}]}`}, true, time.Minute, time.Minute},
		{"permission denied", cannedResponse{403, map[string]string{"X-RateLimit-Remaining": "4999"}, `{"message":"Resource not accessible by personal access token"}`}, false, 0, 0},
		{"not found", cannedResponse{404, map[string]string{"X-RateLimit-Remaining": "0"}, ""}, false, 0, 0},
		{"data mentioning rate limits", cannedResponse{200, nil, `{"data":{"issue":{"title":"We hit a secondary rate limit","label":"RATE_LIMIT"}}}`}, false, 0, 0},
		{"rest page mentioning rate limits", cannedResponse{200, nil, `[{"sha":"a","commit":{"message":"handle secondary rate limit and RATE_LIMIT errors"}}]`}, false, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeCanned(w, tc.resp)
			}))
			defer srv.Close()

			resp, err := ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL), ingestrhttp.WithDisableRetry()).R(context.Background()).Get("/")
			if err != nil {
				t.Fatal(err)
			}
			wait, throttled := rateLimitWait(resp, now)
			if throttled != tc.throttled || wait < tc.minWait || wait > tc.maxWait {
				t.Fatalf("got wait=%v throttled=%v, want throttled=%v wait in [%v, %v]", wait, throttled, tc.throttled, tc.minWait, tc.maxWait)
			}
		})
	}
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

func TestSimulatedGraphQLQuotaRunsOutMidRun(t *testing.T) {
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
	if !strings.Contains(logs.String(), "Warning: GitHub rate limit exceeded for o/r, waiting 1s before retrying") {
		t.Fatalf("missing rate-limit warning, output: %q", logs.String())
	}
}

func TestSimulatedGraphQLSecondaryLimit403(t *testing.T) {
	captureOutput(t)
	f := &fakeGitHub{graphql: []cannedResponse{
		{403, map[string]string{"Retry-After": "1", "X-RateLimit-Remaining": "4000"}, `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`},
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

func TestSimulatedGraphQLServerErrorIsRetried(t *testing.T) {
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

func TestSimulatedGraphQLOtherErrorsAreNotRetried(t *testing.T) {
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

func TestSimulatedRESTQuotaRunsOutMidRun(t *testing.T) {
	logs := captureOutput(t)
	f := &fakeGitHub{}
	s, srv := connectToFake(t, f)
	page2 := srv.URL + "/repos/o/r/commits?page=2&per_page=100"
	f.rest = []cannedResponse{
		{200, map[string]string{"Link": "<" + page2 + `>; rel="next"`}, `[{"sha":"a","commit":{"committer":{"date":"2026-01-02T00:00:00Z"}}}]`},
		restQuotaSpent(),
		{200, nil, `[{"sha":"b","commit":{"committer":{"date":"2026-01-01T00:00:00Z"}}}]`},
	}

	rows, err := readTable(t, s, context.Background(), "commits")
	if err != nil || rows != 2 {
		t.Fatalf("rows=%d err=%v, want both commits", rows, err)
	}
	if len(f.restPaths) != 3 || f.restPaths[1] != f.restPaths[2] {
		t.Fatalf("rest requests=%q, want page 2 retried", f.restPaths)
	}
	if !strings.Contains(logs.String(), "Warning: GitHub rate limit exceeded for o/r") {
		t.Fatalf("missing rate-limit warning, output: %q", logs.String())
	}
}

func TestSimulatedPermissionDeniedIsNotRetried(t *testing.T) {
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

func TestSimulatedRateLimitWaitCanBeCancelled(t *testing.T) {
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
