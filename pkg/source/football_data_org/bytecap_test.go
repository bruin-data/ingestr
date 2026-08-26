package football_data_org

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestFootballDataOrgByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		teams := make([]map[string]interface{}, 0, 50)
		for i := 0; i < 50; i++ {
			teams = append(teams, map[string]interface{}{"id": i, "name": wide})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"teams": teams})
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := NewFootballDataOrgSource()
		if err := s.Connect(context.Background(), "footballdata://?api_key=test-token&base_url="+url.QueryEscape(srv.URL)); err != nil {
			t.Fatal(err)
		}
		table, err := s.GetTable(context.Background(), source.TableRequest{Name: "teams"})
		if err != nil {
			t.Fatal(err)
		}
		results, err := table.Read(context.Background(), source.ReadOptions{MaxBatchBytes: max})
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
