package payrails

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

func TestPayrailsByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/auth/token/") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "tok", "expires_in": 3600})
			return
		}
		results := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			results = append(results, map[string]interface{}{"id": i, "name": wide})
		}
		// empty links.next/paging.next terminates pagination after this single page.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": results,
			"links":   map[string]interface{}{"next": ""},
			"paging":  map[string]interface{}{"next": ""},
		})
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &PayrailsSource{
			client: httpclient.New(httpclient.WithBaseURL(srv.URL)),
			cfg:    &payrailsConfig{clientID: "cid", clientSecret: "secret", baseURL: srv.URL},
		}
		results, err := s.read(context.Background(), "payments", nil, source.ReadOptions{MaxBatchBytes: max})
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
