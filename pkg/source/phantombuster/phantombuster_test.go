package phantombuster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestPhantombusterByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "fetch-all"):
			containers := make([]map[string]interface{}, 0, 50)
			for i := 0; i < 50; i++ {
				containers = append(containers, map[string]interface{}{
					"id":      fmt.Sprintf("c-%d", i),
					"endedAt": float64(1600000000000),
					"name":    wide,
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"containers":      containers,
				"maxLimitReached": false,
			})
		case strings.Contains(r.URL.Path, "fetch-result-object"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	start := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)

	run := func(max int64) (int64, int64) {
		s := &PhantombusterSource{client: httpclient.New(httpclient.WithBaseURL(srv.URL)), apiKey: "key"}
		results, err := s.read(context.Background(), "completed_phantoms:agent1", source.ReadOptions{
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
