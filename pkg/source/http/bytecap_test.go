package http

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

// TestHTTPByteCap proves the MaxBatchBytes cap: with the cap off the whole JSON
// array lands in one batch; with a small cap the same rows split across many
// batches with no row lost.
func TestHTTPByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The HTTP JSON source does a single unpaginated fetch per read, so it
		// always returns the full 50-row array.
		rows := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]interface{}{"id": i, "blob": wide})
		}
		w.Header().Set("Content-Type", "application/json")
		// The generic HTTP JSON reader with the byte cap parses a top-level array.
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &HTTPSource{url: srv.URL, client: httpclient.New()}
		results, err := s.read(context.Background(), "data#json", source.ReadOptions{MaxBatchBytes: max})
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
