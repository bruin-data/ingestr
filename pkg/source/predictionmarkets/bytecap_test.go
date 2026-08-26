package predictionmarkets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestJSONAPIByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		rows := []map[string]interface{}{}
		if calls == 1 {
			for i := 0; i < 50; i++ {
				rows = append(rows, map[string]interface{}{"id": i, "name": wide})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": rows})
	}))
	defer srv.Close()

	spec := TableSpec{
		Name:       "items",
		Path:       "/items",
		ResultPath: []string{"data"},
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: true},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		Pagination: PaginationNone,
	}

	run := func(max int64) (int64, int64) {
		s := &JSONAPISource{
			Scheme: "test",
			Params: map[string][]string{},
			Client: NewClient(srv.URL, 0, 0),
		}
		defer func() { _ = s.Close(context.Background()) }()
		calls = 0
		results, err := s.ReadSpec(context.Background(), spec, source.ReadOptions{MaxBatchBytes: max})
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
