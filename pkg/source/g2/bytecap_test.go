package g2

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

func TestG2ByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]map[string]interface{}, 0, 50)
		for i := 0; i < 50; i++ {
			data = append(data, map[string]interface{}{
				"id":         i,
				"type":       "product",
				"attributes": map[string]interface{}{"name": wide},
			})
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data, "links": map[string]interface{}{"next": ""}})
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &G2Source{client: httpclient.New(httpclient.WithBaseURL(srv.URL))}
		results, err := s.read(context.Background(), "products", source.ReadOptions{MaxBatchBytes: max})
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
	if offR != onR || offR != 50 {
		t.Fatalf("row mismatch off=%d on=%d", offR, onR)
	}
}
