package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

// Recorded-shape answers of the GitHub REST and GraphQL endpoints the commits table uses.

func restCommitJSON(sha, date string, parents ...string) map[string]interface{} {
	var ps []interface{}
	for _, p := range parents {
		ps = append(ps, map[string]interface{}{"sha": p})
	}
	return map[string]interface{}{
		"sha":     sha,
		"node_id": "C_" + sha[:6],
		"url":     "https://api.github.com/repos/o/r/commits/" + sha,
		"commit": map[string]interface{}{
			"author":       map[string]interface{}{"date": date},
			"committer":    map[string]interface{}{"date": date, "email": "noreply@github.com"},
			"verification": map[string]interface{}{"verified": true, "reason": "valid"},
		},
		"parents": ps,
	}
}

func gqlCommit(sha, date string, parents []string, signed bool, prs ...map[string]interface{}) map[string]interface{} {
	var ps []interface{}
	for _, p := range parents {
		ps = append(ps, map[string]interface{}{"oid": p})
	}
	var nodes []interface{}
	for _, pr := range prs {
		nodes = append(nodes, pr)
	}
	return map[string]interface{}{
		"oid":                    sha,
		"committedDate":          date,
		"parents":                map[string]interface{}{"nodes": ps},
		"signature":              map[string]interface{}{"wasSignedByGitHub": signed},
		"associatedPullRequests": map[string]interface{}{"totalCount": len(nodes), "nodes": nodes},
	}
}

func gqlPR(number int, mergedAt, base, head, mergeCommit string) map[string]interface{} {
	return map[string]interface{}{"number": number, "mergedAt": mergedAt, "baseRefName": base, "headRefOid": head, "mergeCommit": map[string]interface{}{"oid": mergeCommit}}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func testSource(srvURL string) *GithubSource {
	noRetry := ingestrhttp.WithRetry(0, time.Millisecond, time.Millisecond)
	return &GithubSource{
		owner:         "o",
		repo:          "r",
		restClient:    ingestrhttp.New(ingestrhttp.WithBaseURL(srvURL), noRetry),
		graphqlClient: ingestrhttp.New(ingestrhttp.WithBaseURL(srvURL+"/graphql"), noRetry),
	}
}

func readAll(t *testing.T, s *GithubSource, opts source.ReadOptions) ([]map[string]interface{}, error) {
	t.Helper()
	results := make(chan source.RecordBatchResult, 16)
	err := s.readCommits(context.Background(), opts, results)
	close(results)
	var rows []map[string]interface{}
	for res := range results {
		if res.Batch == nil {
			continue
		}
		sch := res.Batch.Schema()
		for i := 0; i < int(res.Batch.NumRows()); i++ {
			row := map[string]interface{}{}
			for c, f := range sch.Fields() {
				col := res.Batch.Column(c)
				if ext, ok := col.(array.ExtensionArray); ok {
					col = ext.Storage()
				}
				if col.IsNull(i) {
					row[f.Name] = nil
					continue
				}
				row[f.Name] = col.(interface{ ValueStr(int) string }).ValueStr(i)
				if b, ok := col.(*array.Boolean); ok {
					row[f.Name] = b.Value(i)
				}
			}
			rows = append(rows, row)
		}
		res.Batch.Release()
	}
	return rows, err
}

func TestGithubReadCommitsEndToEnd(t *testing.T) {
	sha := func(n string) string { return oid(n) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r":
			writeJSON(w, http.StatusOK, map[string]string{"default_branch": "main"})
		case r.URL.Path == "/repos/o/r/commits/main":
			w.Header().Set("Date", "Sun, 08 Nov 2026 00:00:00 GMT")
			writeJSON(w, http.StatusOK, map[string]string{"sha": sha("M1")})
		case r.URL.Path == "/repos/o/r/activity":
			writeJSON(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by personal access token"})
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/compare/"):
			if r.URL.Path != "/repos/o/r/compare/"+sha("M0")+"..."+sha("M1") {
				t.Errorf("unexpected compare %s", r.URL.Path)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"status": "ahead", "ahead_by": 2,
				"commits": []interface{}{restCommitJSON(sha("X1"), "2026-10-20T10:00:00Z", sha("M0")), restCommitJSON(sha("M1"), "2026-11-01T02:00:00Z", sha("M0"), sha("X1"))},
			})
		case r.URL.Path == "/graphql":
			body, _ := io.ReadAll(r.Body)
			var req graphQLRequest
			_ = json.Unmarshal(body, &req)
			if strings.Contains(req.Query, "pullRequests(states: MERGED) { totalCount") {
				writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{"repository": map[string]interface{}{"pullRequests": map[string]interface{}{"totalCount": 2}}}})
				return
			}
			if strings.Contains(req.Query, "pullRequests(") {
				pr := gqlPR(2, "2026-11-01T02:00:00Z", "main", sha("X1"), sha("M1"))
				pr["updatedAt"] = "2026-11-01T02:00:00Z"
				pr["timelineItems"] = map[string]interface{}{"nodes": []interface{}{map[string]interface{}{"commit": map[string]interface{}{"oid": sha("M1")}}}}
				writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{"repository": map[string]interface{}{"pullRequests": map[string]interface{}{
					"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": "p1"}, "nodes": []interface{}{pr},
				}}}})
				return
			}
			if !strings.Contains(req.Query, "history(") {
				t.Errorf("unexpected GraphQL query %s", req.Query)
			}
			nodes := []interface{}{
				gqlCommit(sha("M1"), "2026-11-01T02:00:00Z", []string{sha("M0"), sha("X1")}, true, gqlPR(2, "2026-11-01T02:00:00Z", "main", sha("X1"), sha("M1"))),
				gqlCommit(sha("M0"), "2026-10-30T00:00:00+03:00", []string{sha("R")}, true, gqlPR(1, "2026-10-29T21:00:00Z", "main", sha("Xa"), sha("M0"))),
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
				"repository": map[string]interface{}{"object": map[string]interface{}{"history": map[string]interface{}{
					"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": "c1"}, "nodes": nodes,
				}}},
			}})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	rows, err := readAll(t, testSource(srv.URL), source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d want 2: %v", len(rows), rows)
	}
	for _, row := range rows {
		if row["landed_at_exact"] != true || row["landed_at_source"] != srcPR || row["landed_via_sha"] != sha("M1") || row["pr_number"] != "2" {
			t.Fatalf("row %v", row)
		}
		if !strings.HasPrefix(row["landed_at"].(string), "2026-11-01T02:00:00") || row["removed_from_branch_at"] != nil {
			t.Fatalf("row landed_at=%v removed=%v", row["landed_at"], row["removed_from_branch_at"])
		}
		if row["commit"] == nil || row["committed_at"] == nil || row["authored_at"] == nil {
			t.Fatalf("row is missing the commit columns: %v", row)
		}
	}
}

func TestGithubReadCommitsEmptyRepository(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r":
			writeJSON(w, http.StatusOK, map[string]string{"default_branch": "main"})
		case "/repos/o/r/commits/main":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"Git Repository is empty.","status":"409"}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer srv.Close()

	rows, err := readAll(t, testSource(srv.URL), source.ReadOptions{})
	if err != nil {
		t.Fatalf("empty repository should read as zero rows, got: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows=%d want 0", len(rows))
	}
}

func TestLandedAPICompareWithoutCommonAncestor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "No common ancestor between a and b."})
	}))
	defer srv.Close()
	api := newGithubLandedAPI(testSource(srv.URL))
	res, err := api.compare(context.Background(), oid("A"), oid("B"), 1)
	if err != nil || !res.NoCommonAncestor {
		t.Fatalf("res=%+v err=%v, want NoCommonAncestor", res, err)
	}
	st, err := api.compareStatus(context.Background(), oid("A"), oid("B"))
	if err != nil || st != "not_found" {
		t.Fatalf("status=%q err=%v", st, err)
	}
}

func TestLandedAPIActivityStopsAtTheFirstPageOlderThanStop(t *testing.T) {
	var srv *httptest.Server
	pages := 0
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		if got := r.URL.Query().Get("ref"); got != "refs/heads/main" {
			t.Errorf("ref=%q", got)
		}
		if r.URL.Query().Get("after") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/activity?ref=refs%%2Fheads%%2Fmain&per_page=100&after=c2>; rel="next"`, srv.URL))
			writeJSON(w, http.StatusOK, []map[string]string{
				{"before": "b2", "after": "a2", "ref": "refs/heads/main", "timestamp": "2026-11-05T10:00:00Z", "activity_type": "push"},
			})
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/activity?after=c3>; rel="next"`, srv.URL))
		writeJSON(w, http.StatusOK, []map[string]string{
			{"before": "b1", "after": "a1", "ref": "refs/heads/main", "timestamp": "2026-11-01T10:00:00Z", "activity_type": "force_push"},
		})
	}))
	defer srv.Close()
	api := newGithubLandedAPI(testSource(srv.URL))
	got, err := api.activity(context.Background(), url.Values{"ref": {"refs/heads/main"}}, time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 || len(got) != 2 || got[1].Type != "force_push" || got[1].Before != "b1" {
		t.Fatalf("pages=%d entries=%+v", pages, got)
	}
}

func TestLandedAPIHistoryPageFillsTheCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
			"repository": map[string]interface{}{"object": map[string]interface{}{"history": map[string]interface{}{
				"pageInfo": map[string]interface{}{"hasNextPage": true, "endCursor": "c1"},
				"nodes": []interface{}{
					gqlCommit(oid("B"), "2026-11-01T00:00:00Z", []string{oid("A")}, false),
					gqlCommit(oid("A"), "2026-10-31T23:00:00-02:00", nil, false),
				},
			}}},
		}})
	}))
	defer srv.Close()
	api := newGithubLandedAPI(testSource(srv.URL))
	b, err := api.commit(context.Background(), oid("B"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := api.commit(context.Background(), oid("A"))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("GraphQL calls=%d want 1 (one page, the parent comes from the cache)", calls)
	}
	if len(b.Parents) != 1 || b.Parents[0] != oid("A") || !a.CommittedDate.Equal(time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("b=%+v a=%+v (committedDate must be normalised to UTC)", b, a)
	}
	if len(api.pageMates(oid("A"))) != 2 {
		t.Fatalf("page mates=%d want 2", len(api.pageMates(oid("A"))))
	}
	if _, err := api.commit(context.Background(), "not-an-oid"); err == nil {
		t.Fatal("expected an error for an invalid commit id")
	}
}

func TestLandedAPICheckSuitesWithoutPermission(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data":   map[string]interface{}{"repository": map[string]interface{}{"object": nil}},
			"errors": []map[string]string{{"type": "FORBIDDEN", "message": "Resource not accessible by personal access token"}},
		})
	}))
	defer srv.Close()
	_, err := newGithubLandedAPI(testSource(srv.URL)).checkSuites(context.Background(), oid("A"))
	if !errors.Is(err, errNoPermission) {
		t.Fatalf("err=%v want errNoPermission", err)
	}
}

func TestLandedAPIMergedEventsReadsBothSides(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := string(body)
		if !strings.Contains(q, `a0: object(oid: \"`+oid("P2")+`\")`) || !strings.Contains(q, `b0: object(oid: \"`+oid("M")+`\")`) {
			t.Errorf("query does not alias parent 2 and the commit: %s", q)
		}
		pr := gqlPR(9, "2014-06-01T10:00:00Z", "master", oid("P2"), "")
		pr["mergeCommit"] = nil
		pr["timelineItems"] = map[string]interface{}{"nodes": []interface{}{
			map[string]interface{}{"commit": map[string]interface{}{"oid": oid("M")}},
			map[string]interface{}{"commit": map[string]interface{}{"oid": oid("P2")}},
		}}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{"repository": map[string]interface{}{
			"a0": map[string]interface{}{"associatedPullRequests": map[string]interface{}{"totalCount": 7, "nodes": []interface{}{pr}}},
			"b0": map[string]interface{}{"associatedPullRequests": map[string]interface{}{"nodes": []interface{}{}}},
		}}})
	}))
	defer srv.Close()
	got, overflow, err := newGithubLandedAPI(testSource(srv.URL)).mergedEvents(context.Background(), []eventRequest{{Commit: oid("M"), Parent2: oid("P2"), Own: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !overflow[oid("M")] {
		t.Fatal("a PR list longer than the page (totalCount 7, 1 node) must be reported")
	}
	evs := got[oid("M")]
	if len(evs) != 1 || !evs[0].FromParent2 || evs[0].MergeCommit != "" || len(evs[0].EventCommits) != 2 {
		t.Fatalf("events=%+v", evs)
	}
}

func TestLandedAPIMergedPullRequestsStopsAtSince(t *testing.T) {
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "baseRefName:") {
			t.Errorf("the bulk list must not filter by baseRefName: %s", body)
		}
		updated := "2026-11-05T00:00:00Z"
		if pages == 2 {
			updated = "2026-10-01T00:00:00Z"
		}
		pr := gqlPR(pages, updated, "main", oid("H"), oid("M"))
		pr["updatedAt"] = updated
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{"repository": map[string]interface{}{"pullRequests": map[string]interface{}{
			"pageInfo": map[string]interface{}{"hasNextPage": true, "endCursor": fmt.Sprintf("c%d", pages)},
			"nodes":    []interface{}{pr},
		}}}})
	}))
	defer srv.Close()
	prs, err := newGithubLandedAPI(testSource(srv.URL)).mergedPullRequests(context.Background(), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 || len(prs) != 2 || prs[0].MergeCommit != oid("M") {
		t.Fatalf("pages=%d prs=%+v", pages, prs)
	}
}

func TestLandedAPIMergedPRCountAndOldestActivity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{"repository": map[string]interface{}{"pullRequests": map[string]interface{}{"totalCount": 0}}}})
			return
		}
		q := r.URL.Query()
		if q.Get("direction") != "asc" || q.Get("per_page") != "1" || q.Get("ref") != "refs/heads/master" {
			t.Errorf("unexpected activity query %s", r.URL.RawQuery)
		}
		writeJSON(w, http.StatusOK, []map[string]string{{"before": oid("B"), "after": oid("A"), "ref": "refs/heads/master", "timestamp": "2023-03-07T21:25:25Z", "activity_type": "push"}})
	}))
	defer srv.Close()
	api := newGithubLandedAPI(testSource(srv.URL))
	n, err := api.mergedPRCount(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	e, err := api.oldestActivity(context.Background(), "refs/heads/master")
	if err != nil || e == nil || e.Before != oid("B") || !e.Timestamp.Equal(time.Date(2023, 3, 7, 21, 25, 25, 0, time.UTC)) {
		t.Fatalf("entry=%+v err=%v", e, err)
	}
}
