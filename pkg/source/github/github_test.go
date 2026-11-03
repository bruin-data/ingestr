package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

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
		if err := s.readCommits(context.Background(), source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}, results); err != nil {
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
	if err := s.readCommits(context.Background(), source.ReadOptions{}, results); err != nil {
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
	if err := s.readCommits(context.Background(), source.ReadOptions{}, results); err != nil {
		t.Fatalf("empty repository should read as zero rows, got: %v", err)
	}
	close(results)

	if res, ok := <-results; ok {
		t.Fatalf("expected no batches, got one with %d rows", res.Batch.NumRows())
	}
}
