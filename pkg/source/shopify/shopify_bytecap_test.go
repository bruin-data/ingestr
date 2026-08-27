package shopify

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

// TestShopifyByteCap proves the MaxBatchBytes flush in readEvents: a single page
// of padded events lands in one batch with the cap off, and splits across multiple
// batches with a small cap, with no row loss. The padding rides in the "message"
// field, which transformEvent preserves.
func TestShopifyByteCap(t *testing.T) {
	const mockRows = 50
	wide := strings.Repeat("x", 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/events") {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		events := make([]map[string]interface{}, 0, mockRows)
		for i := 0; i < mockRows; i++ {
			events = append(events, map[string]interface{}{
				"id":      i + 1,
				"verb":    "create",
				"message": wide,
			})
		}
		// No Link header -> the paginator stops after this single page.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"events": events})
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &ShopifySource{restClient: httpclient.New(httpclient.WithBaseURL(srv.URL))}
		opts := source.ReadOptions{MaxBatchBytes: max}
		results, err := s.read(context.Background(), "events", opts)
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
	if offR != onR || offR != mockRows {
		t.Fatalf("row mismatch off=%d on=%d want %d", offR, onR, mockRows)
	}
}
