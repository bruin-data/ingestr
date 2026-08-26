package hubspot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestHubspotByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		results := []map[string]interface{}{}
		if calls == 1 {
			for i := 0; i < 50; i++ {
				results = append(results, map[string]interface{}{
					"id": fmt.Sprintf("%d", i),
					"properties": map[string]interface{}{
						"hs_object_id": fmt.Sprintf("%d", i),
						"note":         wide,
					},
				})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
	}))
	defer srv.Close()

	cfg := tableConfig{ObjectType: "contacts", IncrementalKey: "lastmodifieddate"}
	props := []string{"hs_object_id", "note"}

	run := func(max int64) (int64, int64) {
		calls = 0
		s := &Hubspotsource{searchClient: httpclient.New(httpclient.WithBaseURL(srv.URL))}
		results := make(chan source.RecordBatchResult, 64)
		err := s.searchCRMObjects(context.Background(), cfg, props, "0", source.ReadOptions{MaxBatchBytes: max}, results)
		if err != nil {
			t.Fatal(err)
		}
		close(results)
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
