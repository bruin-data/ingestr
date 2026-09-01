package google_sheets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

func TestParseURIAllowsMissingCredentials(t *testing.T) {
	creds, err := parseURI("gsheets://")
	if err != nil {
		t.Fatalf("parseURI returned error: %v", err)
	}
	if creds != nil {
		t.Fatalf("creds = %v, want nil when no credentials provided", creds)
	}
}

func TestParseURIDecodesBase64Credentials(t *testing.T) {
	payload := []byte(`{"type":"service_account"}`)
	uri := "gsheets://?credentials_base64=" + base64.StdEncoding.EncodeToString(payload)
	creds, err := parseURI(uri)
	if err != nil {
		t.Fatalf("parseURI returned error: %v", err)
	}
	if string(creds) != string(payload) {
		t.Fatalf("creds = %q, want %q", creds, payload)
	}
}

func TestParseURIRejectsBadScheme(t *testing.T) {
	if _, err := parseURI("postgres://"); err == nil {
		t.Fatal("expected error for invalid scheme, got nil")
	}
}

func TestGoogleSheetsByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := [][]interface{}{{"id", "name"}}
		for i := 0; i < 50; i++ {
			values = append(values, []interface{}{i, wide})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"range":          "Sheet1",
			"majorDimension": "ROWS",
			"values":         values,
		})
	}))
	defer srv.Close()

	svc, err := sheets.NewService(context.Background(),
		option.WithEndpoint(srv.URL),
		option.WithoutAuthentication(),
	)
	if err != nil {
		t.Fatal(err)
	}

	run := func(max int64) (int64, int64) {
		s := &GoogleSheetsSource{client: svc}
		results, err := s.read(context.Background(), "spreadsheet", "Sheet1", source.ReadOptions{MaxBatchBytes: max})
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
