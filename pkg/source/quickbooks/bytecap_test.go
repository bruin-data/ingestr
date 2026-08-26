package quickbooks

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

func TestQuickBooksByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		rows := []map[string]interface{}{}
		if calls == 1 {
			for i := 0; i < 50; i++ {
				rows = append(rows, map[string]interface{}{"Id": i, "name": wide})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"QueryResponse": map[string]interface{}{"Customer": rows},
		})
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		calls = 0
		s := &QuickBooksSource{
			companyID: "1",
			client:    httpclient.New(httpclient.WithBaseURL(srv.URL)),
		}
		results, err := s.read(context.Background(), "customers", source.ReadOptions{MaxBatchBytes: max})
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
