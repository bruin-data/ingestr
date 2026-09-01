package paddle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParsePaddleURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantKey   string
		wantError bool
	}{
		{
			name:    "valid key",
			uri:     "paddle://?api_key=test123",
			wantKey: "test123",
		},
		{
			name:    "api_key with special characters",
			uri:     "paddle://?api_key=test_KEY_123",
			wantKey: "test_KEY_123",
		},
		{
			name:      "missing api_key",
			uri:       "paddle://?foo=bar",
			wantError: true,
		},
		{
			name:      "empty URI",
			uri:       "paddle://",
			wantError: true,
		},
		{
			name:      "wrong scheme",
			uri:       "stripe://?api_key=abc",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parsePaddleURI(tt.uri)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tt.wantKey {
				t.Errorf("api_key = %q, want %q", key, tt.wantKey)
			}
		})
	}
}

func TestBaseURLForKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"pdl_live_xxx", prodBaseURL},
		{"pdl_sdbx_xxx", sandboxBaseURL},
		{"unprefixed", prodBaseURL},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := baseURLForKey(tt.key); got != tt.want {
				t.Errorf("baseURLForKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestInRange(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		item  map[string]interface{}
		start *time.Time
		end   *time.Time
		want  bool
	}{
		{"within range", map[string]interface{}{"updated_at": "2024-01-15T12:00:00Z"}, &start, &end, true},
		{"before start", map[string]interface{}{"updated_at": "2023-12-31T23:59:59Z"}, &start, &end, false},
		{"after end", map[string]interface{}{"updated_at": "2024-02-02T00:00:00Z"}, &start, &end, false},
		{"rfc3339 micros", map[string]interface{}{"updated_at": "2024-01-15T12:00:00.123456Z"}, &start, &end, true},
		{"missing key kept", map[string]interface{}{"id": "ctm_1"}, &start, &end, true},
		{"unparseable kept", map[string]interface{}{"updated_at": "not-a-date"}, &start, &end, true},
		{"only start bound", map[string]interface{}{"updated_at": "2024-06-01T00:00:00Z"}, &start, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inRange(tt.item, tt.start, tt.end); got != tt.want {
				t.Errorf("inRange() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPaddleByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]interface{}{"id": i, "name": wide})
		}
		payload := map[string]interface{}{
			"data": rows,
			"meta": map[string]interface{}{
				"pagination": map[string]interface{}{"has_more": false},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &PaddleSource{client: httpclient.New(httpclient.WithBaseURL(srv.URL))}
		results, err := s.read(context.Background(), "customers", endpoints["customers"], source.ReadOptions{MaxBatchBytes: max})
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
