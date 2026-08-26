package elasticsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestElasticsearchByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/events/_search":
			hits := []map[string]interface{}{}
			for i := 0; i < 50; i++ {
				hits = append(hits, map[string]interface{}{
					"_id":     strconv.Itoa(i),
					"_source": map[string]interface{}{"name": wide},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"_scroll_id": "s1",
				"hits":       map[string]interface{}{"hits": hits},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/_search/scroll":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"_scroll_id": "s2",
				"hits":       map[string]interface{}{"hits": []interface{}{}},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/_search/scroll":
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	clientURI := strings.Replace(server.URL, "http://", "elasticsearch://", 1) + "?secure=false"

	run := func(max int64) (int64, int64) {
		s := NewElasticsearchSource()
		if err := s.Connect(context.Background(), clientURI); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close(context.Background()) }()
		results, err := s.read(context.Background(), "events", source.ReadOptions{MaxBatchBytes: max})
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
