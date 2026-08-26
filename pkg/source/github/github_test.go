package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

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
