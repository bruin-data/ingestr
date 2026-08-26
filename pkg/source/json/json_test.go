package json

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestJSONByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	rows := make([]map[string]interface{}, 0, 50)
	for i := 0; i < 50; i++ {
		rows = append(rows, map[string]interface{}{"id": i, "name": wide})
	}
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	fp := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(fp, data, 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(max int64) (int64, int64) {
		s := &JSONSource{filePath: fp}
		results, err := s.read(context.Background(), source.ReadOptions{MaxBatchBytes: max})
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
