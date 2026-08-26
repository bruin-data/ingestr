package allium

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestAlliumByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/run-async"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"run_id": "run1"})
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode("success")
		case strings.HasSuffix(r.URL.Path, "/results"):
			rows := []map[string]interface{}{}
			for i := 0; i < 50; i++ {
				rows = append(rows, map[string]interface{}{"id": i, "name": wide})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": rows})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &AlliumSource{client: ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL))}
		results, err := s.read(context.Background(), "query:q1", source.ReadOptions{MaxBatchBytes: max})
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
