package applovinmax

import (
	"context"
	"encoding/csv"
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

func TestAppLovinMaxByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)

	var csvBody strings.Builder
	cw := csv.NewWriter(&csvBody)
	_ = cw.Write([]string{"id", "name"})
	for i := 0; i < 50; i++ {
		_ = cw.Write([]string{strconv.Itoa(i), wide})
	}
	cw.Flush()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "csv") {
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte(csvBody.String()))
			return
		}
		// userAdRevenueReport: only ios has data so exactly one task emits.
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("platform") == "ios" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ad_revenue_report_url": "http://" + r.Host + "/csv",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer srv.Close()

	day := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	run := func(max int64) (int64, int64) {
		s := &AppLovinMaxSource{
			apiKey:       "k",
			applications: []string{"app1"},
			client:       httpclient.New(httpclient.WithBaseURL(srv.URL)),
		}
		results, err := s.read(context.Background(), source.ReadOptions{
			MaxBatchBytes: max,
			IntervalStart: &day,
			IntervalEnd:   &day,
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
