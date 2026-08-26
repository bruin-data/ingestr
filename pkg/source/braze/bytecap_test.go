package braze

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestBrazeByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	const nRows = 50

	// Segment export download is a zip of newline-delimited JSON user records.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, err := zw.Create("users.txt")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < nRows; i++ {
		line, _ := json.Marshal(map[string]interface{}{
			"external_id": i,
			"blob":        wide,
		})
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	archive := zipBuf.Bytes()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/users/export/segment":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"url": srv.URL + "/download"})
		case r.URL.Path == "/download":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	run := func(maxBytes int64) (int64, int64) {
		s := &BrazeSource{
			apiKey:   "dummy",
			endpoint: srv.URL,
			client:   httpclient.New(httpclient.WithBaseURL(srv.URL)),
		}
		results, err := s.read(context.Background(), "user_data", []string{"seg1"}, source.ReadOptions{
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
