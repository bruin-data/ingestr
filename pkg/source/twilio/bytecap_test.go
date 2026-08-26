package twilio

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

func TestTwilioByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		messages := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			messages = append(messages, map[string]interface{}{"sid": i, "body": wide})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"messages":      messages,
			"next_page_uri": "",
		})
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &TwilioSource{
			client:     httpclient.New(httpclient.WithBaseURL(srv.URL)),
			accountSID: "AC123",
		}
		results, err := s.read(context.Background(), "messages", source.ReadOptions{MaxBatchBytes: max})
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
