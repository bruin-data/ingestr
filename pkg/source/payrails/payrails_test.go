package payrails

import (
	"testing"
	"time"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantID      string
		wantSecret  string
		wantBase    string
		wantCert    string
		wantKey     string
		wantCertB64 string
		wantKeyB64  string
		wantErr     bool
	}{
		{
			name:       "valid sandbox with mTLS",
			uri:        "payrails://?client_id=cid&client_secret=sec&environment=sandbox&cert_path=/tmp/c.pem&key_path=/tmp/c.key",
			wantID:     "cid",
			wantSecret: "sec",
			wantBase:   stagingBaseURL,
			wantCert:   "/tmp/c.pem",
			wantKey:    "/tmp/c.key",
		},
		{
			name:     "staging alias",
			uri:      "payrails://?client_id=cid&client_secret=sec&environment=staging",
			wantBase: stagingBaseURL,
		},
		{
			name:     "defaults to production",
			uri:      "payrails://?client_id=cid&client_secret=sec",
			wantBase: productionBaseURL,
		},
		{
			name:     "explicit production",
			uri:      "payrails://?client_id=cid&client_secret=sec&environment=production",
			wantBase: productionBaseURL,
		},
		{
			name:     "base_url override",
			uri:      "payrails://?client_id=cid&client_secret=sec&base_url=https%3A%2F%2Fapi.eu.payrails.io%2F",
			wantBase: "https://api.eu.payrails.io",
		},
		{
			name:    "invalid base_url",
			uri:     "payrails://?client_id=cid&client_secret=sec&base_url=not-a-url",
			wantErr: true,
		},
		{
			name:        "base64 cert and key",
			uri:         "payrails://?client_id=cid&client_secret=sec&cert_base64=Y2VydA%3D%3D&key_base64=a2V5",
			wantBase:    productionBaseURL,
			wantCertB64: "Y2VydA==",
			wantKeyB64:  "a2V5",
		},
		{
			name:    "cert without key",
			uri:     "payrails://?client_id=cid&client_secret=sec&cert_path=/tmp/c.pem",
			wantErr: true,
		},
		{
			name:    "cert_base64 without key_base64",
			uri:     "payrails://?client_id=cid&client_secret=sec&cert_base64=Y2VydA%3D%3D",
			wantErr: true,
		},
		{
			name:    "key without cert",
			uri:     "payrails://?client_id=cid&client_secret=sec&key_path=/tmp/c.key",
			wantErr: true,
		},
		{
			name:    "missing client_id",
			uri:     "payrails://?client_secret=sec",
			wantErr: true,
		},
		{
			name:    "missing client_secret",
			uri:     "payrails://?client_id=cid",
			wantErr: true,
		},
		{
			name:    "invalid environment",
			uri:     "payrails://?client_id=cid&client_secret=sec&environment=dev",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "primer://?client_id=cid&client_secret=sec",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantID != "" && cfg.clientID != tt.wantID {
				t.Errorf("client_id = %q, want %q", cfg.clientID, tt.wantID)
			}
			if tt.wantSecret != "" && cfg.clientSecret != tt.wantSecret {
				t.Errorf("client_secret = %q, want %q", cfg.clientSecret, tt.wantSecret)
			}
			if cfg.baseURL != tt.wantBase {
				t.Errorf("baseURL = %q, want %q", cfg.baseURL, tt.wantBase)
			}
			if cfg.certPath != tt.wantCert {
				t.Errorf("certPath = %q, want %q", cfg.certPath, tt.wantCert)
			}
			if cfg.keyPath != tt.wantKey {
				t.Errorf("keyPath = %q, want %q", cfg.keyPath, tt.wantKey)
			}
			if cfg.certBase64 != tt.wantCertB64 {
				t.Errorf("certBase64 = %q, want %q", cfg.certBase64, tt.wantCertB64)
			}
			if cfg.keyBase64 != tt.wantKeyB64 {
				t.Errorf("keyBase64 = %q, want %q", cfg.keyBase64, tt.wantKeyB64)
			}
		})
	}
}

func TestSupportedTables(t *testing.T) {
	valid := []string{"payments", "instruments", "executions"}
	for _, tbl := range valid {
		if !supportedTables[tbl] {
			t.Errorf("supportedTables[%q] = false, want true", tbl)
		}
	}
	invalid := []string{"", "Payments", "payment", "workflows", "unknown"}
	for _, tbl := range invalid {
		if supportedTables[tbl] {
			t.Errorf("supportedTables[%q] = true, want false", tbl)
		}
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name      string
		wantTable string
		wantCodes []string
	}{
		{"payments", "payments", nil},
		{"executions", "executions", nil},
		{"executions:payment-acceptance", "executions", []string{"payment-acceptance"}},
		{"executions:a, b ,c", "executions", []string{"a", "b", "c"}},
		{"executions:", "executions", nil},
	}
	for _, tt := range tests {
		table, codes := parseTableName(tt.name)
		if table != tt.wantTable {
			t.Errorf("parseTableName(%q) table = %q, want %q", tt.name, table, tt.wantTable)
		}
		if len(codes) != len(tt.wantCodes) {
			t.Fatalf("parseTableName(%q) codes = %v, want %v", tt.name, codes, tt.wantCodes)
		}
		for i := range codes {
			if codes[i] != tt.wantCodes[i] {
				t.Errorf("parseTableName(%q) codes[%d] = %q, want %q", tt.name, i, codes[i], tt.wantCodes[i])
			}
		}
	}
}

func TestResolveNextTarget(t *testing.T) {
	base := "/payment/instruments"
	tests := []struct{ next, want string }{
		{"", ""},
		{"https://api.payrails.io/payment/instruments?page[after]=abc", "https://api.payrails.io/payment/instruments?page[after]=abc"},
		{"/payment/instruments?page[after]=abc", "/payment/instruments?page[after]=abc"},
		{"?createdAtOrAfter=2022-02-11&limit=50&pagingIdBefore=x", base + "?createdAtOrAfter=2022-02-11&limit=50&pagingIdBefore=x"},
		{"abc123", base + "?abc123"},
	}
	for _, tt := range tests {
		if got := resolveNextTarget(base, tt.next); got != tt.want {
			t.Errorf("resolveNextTarget(%q, %q) = %q, want %q", base, tt.next, got, tt.want)
		}
	}
}

func TestCreatedAtFilter(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	if got := createdAtFilter(nil, nil); got != "" {
		t.Errorf("no bounds = %q, want empty", got)
	}
	if got, want := createdAtFilter(&start, &end), "[2026-04-01T00:00:00Z,2026-05-01T00:00:00Z)"; got != want {
		t.Errorf("range = %q, want %q", got, want)
	}
	if got, want := createdAtFilter(&start, nil), "[2026-04-01T00:00:00Z"; got != want {
		t.Errorf("start only = %q, want %q", got, want)
	}
	if got, want := createdAtFilter(nil, &end), "2026-05-01T00:00:00Z)"; got != want {
		t.Errorf("end only = %q, want %q", got, want)
	}
}

func TestWorkflowCodePattern(t *testing.T) {
	valid := []string{"payment-acceptance", "payout", "a", "a1-B2"}
	for _, c := range valid {
		if !workflowCodePattern.MatchString(c) {
			t.Errorf("workflowCodePattern rejected valid code %q", c)
		}
	}
	invalid := []string{"", "1payment", "-payment", "Payment", "with space", "with/slash"}
	for _, c := range invalid {
		if workflowCodePattern.MatchString(c) {
			t.Errorf("workflowCodePattern accepted invalid code %q", c)
		}
	}
}

func TestFilterItemsByInterval(t *testing.T) {
	items := []map[string]interface{}{
		{"id": "a", "updatedAt": "2026-04-10T00:00:00Z"},
		{"id": "b", "updatedAt": "2026-05-10T00:00:00Z"},
		{"id": "c"}, // no timestamp: kept
	}
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	got := filterItemsByInterval(items, "updatedAt", &start, &end)
	ids := map[string]bool{}
	for _, it := range got {
		ids[it["id"].(string)] = true
	}
	if !ids["a"] || ids["b"] || !ids["c"] {
		t.Errorf("unexpected filter result: %v", ids)
	}

	if len(filterItemsByInterval(items, "updatedAt", nil, nil)) != len(items) {
		t.Errorf("no bounds should return all items")
	}
}
