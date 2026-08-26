package monday

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

// TestMondayByteCap proves the MaxBatchBytes cap on the paginated GraphQL list
// path (users): with the cap off a single page of users lands in one batch; with
// a small cap the same users split across many batches with no user lost.
func TestMondayByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// One page of 50 users; 50 < page limit (100) => pagination stops after it.
		users := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			users = append(users, map[string]interface{}{
				"id":   strconv.Itoa(i),
				"name": "u" + strconv.Itoa(i),
				"blob": wide,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"users": users},
		})
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := newTestSource(t, srv.URL)
		defer func() { _ = s.client.Close() }()
		results := make(chan source.RecordBatchResult, 64)
		go func() {
			defer close(results)
			if err := s.readUsers(context.Background(), source.ReadOptions{MaxBatchBytes: max}, results); err != nil {
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
