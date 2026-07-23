package typeform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantToken   string
		wantAPIURL  string
		wantErr     bool
		errContains string
	}{
		{
			name:       "valid URI",
			uri:        "typeform://?token=my-token-123",
			wantToken:  "my-token-123",
			wantAPIURL: baseURL,
		},
		{
			name:       "valid URI with eu region",
			uri:        "typeform://?token=tok&region=eu",
			wantToken:  "tok",
			wantAPIURL: baseURLEU,
		},
		{
			name:       "valid URI with us region",
			uri:        "typeform://?token=tok&region=us",
			wantToken:  "tok",
			wantAPIURL: baseURL,
		},
		{
			name:      "token with special characters",
			uri:       "typeform://?token=aaa.bbb-ccc_ddd",
			wantToken: "aaa.bbb-ccc_ddd",
		},
		{
			name:        "wrong scheme",
			uri:         "surveymonkey://?token=my-token",
			wantErr:     true,
			errContains: "must start with typeform://",
		},
		{
			name:        "missing token",
			uri:         "typeform://?other_param=value",
			wantErr:     true,
			errContains: "token is required",
		},
		{
			name:        "empty URI after scheme",
			uri:         "typeform://",
			wantErr:     true,
			errContains: "token is required",
		},
		{
			name:        "only question mark",
			uri:         "typeform://?",
			wantErr:     true,
			errContains: "token is required",
		},
		{
			name:        "invalid region",
			uri:         "typeform://?token=tok&region=au",
			wantErr:     true,
			errContains: "invalid region",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if creds.token != tt.wantToken {
				t.Fatalf("expected token %q, got %q", tt.wantToken, creds.token)
			}
			if tt.wantAPIURL != "" && creds.apiURL != tt.wantAPIURL {
				t.Fatalf("expected apiURL %q, got %q", tt.wantAPIURL, creds.apiURL)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	validTables := []string{"forms", "responses", "workspaces", "themes"}
	for _, table := range validTables {
		if !isValidTable(table) {
			t.Errorf("expected %q to be valid", table)
		}
	}

	invalidTables := []string{"", "unknown", "FORMS", "Form", "users"}
	for _, table := range invalidTables {
		if isValidTable(table) {
			t.Errorf("expected %q to be invalid", table)
		}
	}
}

func TestExtractItems(t *testing.T) {
	t.Run("valid items array", func(t *testing.T) {
		body := map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{"id": "1"},
				map[string]interface{}{"id": "2"},
			},
		}
		items := extractItems(body)
		if len(items) != 2 {
			t.Fatalf("expected 2, got %d", len(items))
		}
	})

	t.Run("missing items field", func(t *testing.T) {
		body := map[string]interface{}{"other": "value"}
		if items := extractItems(body); items != nil {
			t.Fatalf("expected nil, got %v", items)
		}
	})

	t.Run("empty items array", func(t *testing.T) {
		body := map[string]interface{}{"items": []interface{}{}}
		if items := extractItems(body); len(items) != 0 {
			t.Fatalf("expected 0, got %d", len(items))
		}
	})
}

func TestToInt(t *testing.T) {
	if got := toInt(json.Number("42")); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
	if got := toInt(float64(7)); got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
	if got := toInt("not a number"); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
	if got := toInt(nil); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestJsonUseNumber(t *testing.T) {
	var result map[string]interface{}
	if err := jsonUseNumber([]byte(`{"id": 9007199254740993}`), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id, ok := result["id"].(json.Number)
	if !ok {
		t.Fatalf("expected json.Number, got %T", result["id"])
	}
	if id.String() != "9007199254740993" {
		t.Fatalf("expected 9007199254740993, got %s", id.String())
	}

	if err := jsonUseNumber([]byte(`{"broken":`), &result); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
