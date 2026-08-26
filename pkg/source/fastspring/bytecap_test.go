package fastspring

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestFastspringByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/products" {
			// list endpoint: 50 ids, no pagination
			ids := []map[string]interface{}{}
			for i := 0; i < 50; i++ {
				ids = append(ids, map[string]interface{}{"id": strconv.Itoa(i)})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"products": ids})
			return
		}
		// detail endpoint: return the full objects with a wide preserved field
		rows := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]interface{}{"id": strconv.Itoa(i), "name": wide})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"products": rows})
	}))
	defer srv.Close()

	tc := supportedTables["products"]

	run := func(max int64) (int64, int64) {
		s := &FastspringSource{client: httpclient.New(httpclient.WithBaseURL(srv.URL))}
		results, err := s.read(context.Background(), "products", tc, source.ReadOptions{MaxBatchBytes: max})
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
