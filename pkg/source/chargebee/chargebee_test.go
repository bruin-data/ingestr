package chargebee

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseChargebeeURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantSite  string
		wantKey   string
		wantError bool
	}{
		{
			name:     "valid site and key",
			uri:      "chargebee://acme-test?api_key=test_123",
			wantSite: "acme-test",
			wantKey:  "test_123",
		},
		{
			name:     "full host is trimmed to site",
			uri:      "chargebee://acme-test.chargebee.com?api_key=live_abc",
			wantSite: "acme-test",
			wantKey:  "live_abc",
		},
		{
			name:      "missing api_key",
			uri:       "chargebee://acme-test",
			wantError: true,
		},
		{
			name:      "missing site",
			uri:       "chargebee://?api_key=abc",
			wantError: true,
		},
		{
			name:      "wrong scheme",
			uri:       "paddle://acme?api_key=abc",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site, key, err := parseChargebeeURI(tt.uri)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if site != tt.wantSite {
				t.Errorf("site = %q, want %q", site, tt.wantSite)
			}
			if key != tt.wantKey {
				t.Errorf("api_key = %q, want %q", key, tt.wantKey)
			}
		})
	}
}

func TestApplyTimeFilter(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		start *time.Time
		end   *time.Time
		want  map[string]string
	}{
		{
			name:  "both bounds use between",
			start: &start,
			end:   &end,
			want:  map[string]string{"updated_at[between]": "[1704067200,1706745600]"},
		},
		{
			name:  "start only uses after minus one",
			start: &start,
			want:  map[string]string{"updated_at[after]": "1704067199"},
		},
		{
			name: "end only uses before plus one",
			end:  &end,
			want: map[string]string{"updated_at[before]": "1706745601"},
		},
		{
			name: "no bounds adds nothing",
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := url.Values{}
			applyTimeFilter(params, "updated_at", tt.start, tt.end)
			for k, v := range tt.want {
				if got := params.Get(k); got != v {
					t.Errorf("param %q = %q, want %q", k, got, v)
				}
			}
			if len(params) != len(tt.want) {
				t.Errorf("got %d params, want %d: %v", len(params), len(tt.want), params)
			}
		})
	}
}

func TestSupportedTables(t *testing.T) {
	got := supportedTables()
	want := "customers, events, invoices, orders, subscriptions, transactions"
	if got != want {
		t.Errorf("supportedTables() = %q, want %q", got, want)
	}
}

func TestChargebeeByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		list := []map[string]interface{}{}
		if calls == 1 {
			for i := 0; i < 50; i++ {
				list = append(list, map[string]interface{}{
					"customer": map[string]interface{}{"id": i, "name": wide},
				})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"list": list, "next_offset": ""})
	}))
	defer srv.Close()

	ep := endpoint{path: "/customers", resourceKey: "customer", incrementalKey: "updated_at"}

	run := func(max int64) (int64, int64) {
		calls = 0
		s := &ChargebeeSource{client: httpclient.New(httpclient.WithBaseURL(srv.URL))}
		results, err := s.read(context.Background(), "customers", ep, source.ReadOptions{MaxBatchBytes: max})
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
