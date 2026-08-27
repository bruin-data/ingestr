package revenuecat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseRevenueCatURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantKey   string
		wantProj  string
		wantError bool
	}{
		{
			name:     "valid URI with api_key and project_id",
			uri:      "revenuecat://?api_key=sk_test_123&project_id=proj_abc",
			wantKey:  "sk_test_123",
			wantProj: "proj_abc",
		},
		{
			name:     "valid URI with api_key only",
			uri:      "revenuecat://?api_key=sk_test_123",
			wantKey:  "sk_test_123",
			wantProj: "",
		},
		{
			name:      "missing api_key",
			uri:       "revenuecat://?project_id=proj_abc",
			wantError: true,
		},
		{
			name:      "wrong scheme",
			uri:       "stripe://?api_key=sk_test_123",
			wantError: true,
		},
		{
			name:      "empty URI",
			uri:       "revenuecat://",
			wantError: true,
		},
		{
			name:      "empty query",
			uri:       "revenuecat://?",
			wantError: true,
		},
		{
			name:     "api_key with special characters",
			uri:      "revenuecat://?api_key=sk_WbIlYjISGXrTXQuUmTkSyABGsHyph&project_id=c09fd2a0",
			wantKey:  "sk_WbIlYjISGXrTXQuUmTkSyABGsHyph",
			wantProj: "c09fd2a0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parseRevenueCatURI(tt.uri)
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if creds.apiKey != tt.wantKey {
				t.Errorf("apiKey = %q, want %q", creds.apiKey, tt.wantKey)
			}
			if creds.projectID != tt.wantProj {
				t.Errorf("projectID = %q, want %q", creds.projectID, tt.wantProj)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	validTables := []string{"projects", "customers", "products", "entitlements", "offerings"}
	for _, table := range validTables {
		if !isValidTable(table) {
			t.Errorf("isValidTable(%q) = false, want true", table)
		}
	}

	invalidTables := []string{"", "unknown", "Projects", "CUSTOMERS", "subscriptions", "purchases"}
	for _, table := range invalidTables {
		if isValidTable(table) {
			t.Errorf("isValidTable(%q) = true, want false", table)
		}
	}
}

func TestExtractStartingAfter(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "standard next_page URL",
			url:  "/v2/projects/abc/customers?starting_after=cust_123&limit=1000",
			want: "cust_123",
		},
		{
			name: "starting_after at end",
			url:  "/v2/projects/abc/customers?limit=1000&starting_after=cust_456",
			want: "cust_456",
		},
		{
			name: "no starting_after",
			url:  "/v2/projects/abc/customers?limit=1000",
			want: "",
		},
		{
			name: "empty string",
			url:  "",
			want: "",
		},
		{
			name: "full URL with starting_after",
			url:  "https://api.revenuecat.com/v2/projects/abc/products?starting_after=prod_789&limit=1000",
			want: "prod_789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStartingAfter(tt.url)
			if got != tt.want {
				t.Errorf("extractStartingAfter(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestConvertTimestampsToISO(t *testing.T) {
	tests := []struct {
		name   string
		item   map[string]interface{}
		fields []string
		want   string
	}{
		{
			name:   "json.Number milliseconds",
			item:   map[string]interface{}{"created_at": json.Number("1711200000000")},
			fields: []string{"created_at"},
			want:   "2024-03-23T13:20:00.000Z",
		},
		{
			name:   "float64 milliseconds",
			item:   map[string]interface{}{"created_at": float64(1711200000000)},
			fields: []string{"created_at"},
			want:   "2024-03-23T13:20:00.000Z",
		},
		{
			name:   "nil value is skipped",
			item:   map[string]interface{}{"created_at": nil},
			fields: []string{"created_at"},
			want:   "",
		},
		{
			name:   "missing field is skipped",
			item:   map[string]interface{}{"other": "value"},
			fields: []string{"created_at"},
			want:   "",
		},
		{
			name:   "string value is not converted",
			item:   map[string]interface{}{"created_at": "already_string"},
			fields: []string{"created_at"},
			want:   "already_string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			convertTimestampsToISO(tt.item, tt.fields)
			got, ok := tt.item[tt.fields[0]]
			if tt.want == "" {
				if ok && got != nil && got != "already_string" {
					t.Errorf("expected nil or missing, got %v", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJsonUseNumber(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "large integer preserved",
			input: `{"id": 9007199254740993}`,
		},
		{
			name:  "float preserved",
			input: `{"price": 9.99}`,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]interface{}
			decoder := json.NewDecoder(bytes.NewReader([]byte(tt.input)))
			decoder.UseNumber()
			err := decoder.Decode(&result)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			for _, v := range result {
				if _, ok := v.(json.Number); !ok {
					t.Errorf("expected json.Number, got %T", v)
				}
			}
		})
	}
}

// TestRevenueCatByteCap proves the MaxBatchBytes flush in readCustomers: with the
// cap off the padded (enriched) customers land in a single batch, and with a small
// cap they split across multiple batches with no row loss.
func TestRevenueCatByteCap(t *testing.T) {
	const mockRows = 50
	wide := strings.Repeat("x", 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions"), strings.HasSuffix(r.URL.Path, "/purchases"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": []interface{}{}, "next_page": ""})
		case strings.HasSuffix(r.URL.Path, "/customers"):
			items := make([]map[string]interface{}, 0, mockRows)
			for i := 0; i < mockRows; i++ {
				items = append(items, map[string]interface{}{"id": "cust_" + strconv.Itoa(i), "blob": wide})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": items, "next_page": ""})
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		client := ingestrhttp.New(ingestrhttp.WithBaseURL(srv.URL))
		s := &RevenueCatSource{
			projectID:      "proj",
			customerClient: client,
			projectClient:  client,
		}
		opts := source.ReadOptions{MaxBatchBytes: max}
		results, err := s.read(context.Background(), "customers", opts)
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
	if offR != onR || offR != mockRows {
		t.Fatalf("row mismatch off=%d on=%d want %d", offR, onR, mockRows)
	}
}
