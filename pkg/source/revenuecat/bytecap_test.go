package revenuecat

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

// TestRevenueCatByteCap proves the MaxBatchBytes flush in readCustomers: with the
// cap off the padded (enriched) customers land in a single batch, and with a small
// cap they split across multiple batches with no row loss.
func TestRevenueCatByteCap(t *testing.T) {
	const mockRows = 50
	wide := strings.Repeat("x", 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions"), strings.HasSuffix(r.URL.Path, "/purchases"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": []interface{}{}, "next_page": ""})
		case strings.HasSuffix(r.URL.Path, "/customers"):
			items := make([]map[string]interface{}, 0, mockRows)
			for i := 0; i < mockRows; i++ {
				items = append(items, map[string]interface{}{"id": "cust_" + strconv.Itoa(i), "blob": wide})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": items, "next_page": ""})
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		client := ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL))
		s := &RevenueCatSource{
			projectID:      "proj",
			customerClient: client,
			projectClient:  client,
		}
		opts := source.ReadOptions{MaxBatchBytes: max}
		results, err := s.read(context.Background(), "customers", opts)
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
