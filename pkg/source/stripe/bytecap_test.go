package stripe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	stripego "github.com/stripe/stripe-go/v81"
)

// TestStripeByteCap proves the MaxBatchBytes flush in readTableFromEvents (the
// events-based path used by tables with an eventTypeFilter, e.g. "charge"). A
// single page of change events fans out to padded object fetches; with the cap
// off they land in one batch, with a small cap they split, with no row loss.
func TestStripeByteCap(t *testing.T) {
	const mockRows = 50
	wide := strings.Repeat("x", 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/events":
			// One page of charge.* events, each pointing at a unique charge id.
			data := make([]map[string]interface{}, 0, mockRows)
			for i := 0; i < mockRows; i++ {
				data = append(data, map[string]interface{}{
					"id":      "evt_" + strconv.Itoa(i),
					"object":  "event",
					"type":    "charge.updated",
					"created": time.Now().Unix(),
					"data": map[string]interface{}{
						"object": map[string]interface{}{
							"id":     "ch_" + strconv.Itoa(i),
							"object": "charge",
						},
					},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"object":   "list",
				"url":      "/v1/events",
				"has_more": false,
				"data":     data,
			})
		case strings.HasPrefix(r.URL.Path, "/v1/charges/"):
			// Re-fetched charge object carries the padding that drives the byte cap.
			id := strings.TrimPrefix(r.URL.Path, "/v1/charges/")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":          id,
				"object":      "charge",
				"description": wide,
			})
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	originalKey := stripego.Key
	originalBackend := stripego.GetBackend(stripego.APIBackend)
	stripego.Key = "sk_test_bytecap"
	stripego.SetBackend(stripego.APIBackend, stripego.GetBackendWithConfig(stripego.APIBackend, &stripego.BackendConfig{
		URL:        stripego.String(srv.URL),
		HTTPClient: srv.Client(),
	}))
	t.Cleanup(func() {
		stripego.SetBackend(stripego.APIBackend, originalBackend)
		stripego.Key = originalKey
	})

	start := time.Now().Add(-24 * time.Hour)

	run := func(max int64) (int64, int64) {
		s := &StripeSource{}
		results := make(chan source.RecordBatchResult, 8)
		errCh := make(chan error, 1)
		go func() {
			err := s.readTableFromEvents(context.Background(), "charge", "charge.*", source.ReadOptions{MaxBatchBytes: max}, &start, nil, results)
			close(results)
			errCh <- err
		}()
		var batches, rows int64
		for res := range results {
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			batches++
			rows += res.Batch.NumRows()
			res.Batch.Release()
		}
		if err := <-errCh; err != nil {
			t.Fatal(err)
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
