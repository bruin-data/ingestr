package freshdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

// TestFreshdeskByteCap proves the MaxBatchBytes flush loop in paginateAndSend: with
// the cap off every agent arrives in a single batch, and with a small cap the same
// agents are split across multiple batches without losing any rows.
func TestFreshdeskByteCap(t *testing.T) {
	const rowCount = 50
	wide := strings.Repeat("x", 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") != "1" {
			_, _ = w.Write([]byte("[]"))
			return
		}
		agents := make([]map[string]interface{}, 0, rowCount)
		for i := 0; i < rowCount; i++ {
			agents = append(agents, map[string]interface{}{
				"id":   i,
				"blob": wide,
			})
		}
		_ = json.NewEncoder(w).Encode(agents)
	}))
	defer srv.Close()

	run := func(maxBytes int64) (int64, int64) {
		s := &FreshdeskSource{
			client: httpclient.New(httpclient.WithBaseURL(srv.URL)),
		}
		results, err := s.read(context.Background(), "agents", "", source.ReadOptions{MaxBatchBytes: maxBytes})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var batches, rows int64
		for res := range results {
			if res.Err != nil {
				t.Fatalf("batch error: %v", res.Err)
			}
			batches++
			rows += res.Batch.NumRows()
			res.Batch.Release()
		}
		return batches, rows
	}

	offBatches, offRows := run(0)
	onBatches, onRows := run(4096)

	if offBatches != 1 {
		t.Fatalf("cap-off batches=%d want 1", offBatches)
	}
	if onBatches <= 1 {
		t.Fatalf("cap-on batches=%d want >1", onBatches)
	}
	if offRows != onRows || offRows != rowCount {
		t.Fatalf("row mismatch off=%d on=%d want %d", offRows, onRows, rowCount)
	}
}
