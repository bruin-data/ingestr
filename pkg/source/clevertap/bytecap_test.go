package clevertap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestCleverTapByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			// Step 1: open the cursor.
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"cursor": "c1",
			})
			return
		}
		// Step 2: one page of records within the interval, then terminate
		// (empty next_cursor).
		rows := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]interface{}{
				"ts":   "20260102120000",
				"name": wide,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "success",
			"next_cursor": "",
			"records":     rows,
		})
	}))
	defer srv.Close()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	run := func(max int64) (int64, int64) {
		s := &CleverTapSource{
			client:   httpclient.New(httpclient.WithBaseURL(srv.URL)),
			timezone: time.UTC,
		}
		params := clevertapParams{EventName: []string{"evt1"}}
		results, err := s.read(context.Background(), "events", params, source.ReadOptions{
			MaxBatchBytes: max,
			IntervalStart: &start,
			IntervalEnd:   &end,
		})
		if err != nil {
			t.Fatal(err)
		}
		var b, rw int64
		for res := range results {
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			b++
			rw += res.Batch.NumRows()
			res.Batch.Release()
		}
		return b, rw
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
		t.Fatalf("row mismatch off=%d on=%d", offR, onR)
	}
}
