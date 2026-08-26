package amplitude

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestAmplitudeByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	const nRows = 50

	// Build the export archive once: a single NDJSON entry with nRows wide events.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, err := zw.Create("export/events.json")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < nRows; i++ {
		line, _ := json.Marshal(map[string]interface{}{
			"event_type": "test",
			"blob":       wide,
			"id":         i,
		})
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	archive := zipBuf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(archive)))
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	start := time.Date(2024, 1, 1, 0, 30, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)

	run := func(maxBytes int64) (int64, int64) {
		s := &AmplitudeSource{
			exportClient: httpclient.New(httpclient.WithBaseURL(srv.URL)),
		}
		results, err := s.read(context.Background(), "events", source.ReadOptions{
			IntervalStart: &start,
			IntervalEnd:   &end,
			Parallelism:   1,
			MaxBatchBytes: maxBytes,
		})
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
	if offR != onR || offR != nRows {
		t.Fatalf("row mismatch off=%d on=%d want %d", offR, onR, nRows)
	}
}
