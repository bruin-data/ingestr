package primer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

// TestPrimerByteCap proves the MaxBatchBytes flush in readPayments: with the cap
// off the padded rows land in a single batch, and with a small cap the same rows
// split across multiple batches with no row loss.
func TestPrimerByteCap(t *testing.T) {
	const mockRows = 50
	wide := strings.Repeat("x", 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/payments" {
			// Payment ID listing: one page, no next cursor.
			data := make([]map[string]interface{}, 0, mockRows)
			for i := 0; i < mockRows; i++ {
				data = append(data, map[string]interface{}{"id": "pay_" + strconv.Itoa(i)})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data, "nextCursor": ""})
			return
		}
		// Payment detail: a padded object accumulated into the byte-capped batch.
		id := strings.TrimPrefix(r.URL.Path, "/payments/")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "blob": wide})
	}))
	defer srv.Close()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start

	run := func(max int64) (int64, int64) {
		s := &PrimerSource{client: httpclient.New(httpclient.WithBaseURL(srv.URL))}
		opts := source.ReadOptions{
			MaxBatchBytes: max,
			IntervalStart: &start,
			IntervalEnd:   &end,
			Parallelism:   1,
		}
		results, err := s.read(context.Background(), "payments", []string{"SETTLED"}, opts)
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
