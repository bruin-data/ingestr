package trustpilot

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

func TestTrustpilotByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reviews := []map[string]interface{}{}
		if r.URL.Query().Get("page") == "1" {
			for i := 0; i < 50; i++ {
				reviews = append(reviews, map[string]interface{}{"id": i, "name": wide})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"reviews": reviews})
	}))
	defer srv.Close()

	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC)

	run := func(max int64) (int64, int64) {
		s := &TrustpilotSource{
			client:         httpclient.New(httpclient.WithBaseURL(srv.URL)),
			businessUnitID: "biz",
			apiKey:         "key",
		}
		results, err := s.read(context.Background(), "reviews", source.ReadOptions{
			IntervalStart: &start,
			IntervalEnd:   &end,
			MaxBatchBytes: max,
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
