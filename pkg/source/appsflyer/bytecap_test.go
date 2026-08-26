package appsflyer

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

func TestAppsflyerByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	const nRows = 50

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := make([]map[string]interface{}, 0, nRows)
		for i := 0; i < nRows; i++ {
			rows = append(rows, map[string]interface{}{
				"install_time": "2024-01-01",
				"blob":         wide,
				"id":           i,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer srv.Close()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	run := func(maxBytes int64) (int64, int64) {
		s := &AppsflyerSource{
			apiKey: "dummy",
			client: httpclient.New(httpclient.WithBaseURL(srv.URL)),
		}
		meta := tableMeta{dimensions: []string{"install_time"}, metrics: []string{"blob"}}
		results, err := s.read(context.Background(), meta, nil, source.ReadOptions{
			IntervalStart: &start,
			IntervalEnd:   &end,
			MaxBatchBytes: maxBytes,
		})
		if err != nil {
			t.Fatal(err)
		}
		var batches, rows int64
		for res := range results {
			if res.Err != nil {
				t.Fatal(res.Err)
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
	if offR != onR || offR != nRows {
		t.Fatalf("row mismatch off=%d on=%d want %d", offR, onR, nRows)
	}
}
