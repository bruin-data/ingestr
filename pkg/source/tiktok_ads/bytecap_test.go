package tiktokads

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

func TestTiktokAdsByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		list := make([]map[string]interface{}, 0, 50)
		for i := 0; i < 50; i++ {
			list = append(list, map[string]interface{}{
				"dimensions": map[string]interface{}{"campaign_id": wide},
				"metrics":    map[string]interface{}{"spend": "1.5"},
			})
		}
		resp := map[string]interface{}{
			"code":    0,
			"message": "OK",
			"data": map[string]interface{}{
				"list": list,
				"page_info": map[string]interface{}{
					"total_number": 50,
					"page":         1,
					"page_size":    1000,
					"total_page":   1,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dimensions := []string{"campaign_id"}
	metrics := []string{"spend"}
	schemaCols := buildSchemaColumns(dimensions, metrics)
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()

	run := func(max int64) (int64, int64) {
		s := &TiktokAdsSource{
			client:        httpclient.New(httpclient.WithBaseURL(srv.URL)),
			accessToken:   "tok",
			advertiserIDs: []string{"123"},
			timezone:      "UTC",
		}
		results, err := s.read(context.Background(), dimensions, metrics, schemaCols, "", nil, source.ReadOptions{
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
