package revenuecat

import (
	"bytes"
	"encoding/json"
	"testing"
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
