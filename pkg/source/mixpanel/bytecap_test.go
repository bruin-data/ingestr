package mixpanel

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

// TestMixpanelByteCap proves the MaxBatchBytes cap on the events export path:
// with the cap off the JSONL export streams into one batch; with a small cap the
// same events split across many batches with no event lost.
func TestMixpanelByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The export API returns newline-delimited JSON (one event per line) in a
		// single unpaginated response.
		w.Header().Set("Content-Type", "text/plain")
		enc := json.NewEncoder(w)
		for i := 0; i < 50; i++ {
			_ = enc.Encode(map[string]interface{}{"event": "e", "insert_id": i, "blob": wide})
		}
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &MixpanelSource{
			exportClient: httpclient.New(httpclient.WithBaseURL(srv.URL)),
			projectID:    "test",
		}
		results, err := s.read(context.Background(), "events", source.ReadOptions{MaxBatchBytes: max})
		if err != nil {
			t.Fatal(err)
		}
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
