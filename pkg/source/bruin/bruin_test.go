package bruin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantKey string
		wantErr bool
	}{
		{
			name:    "valid URI",
			uri:     "bruin://?api_key=test-key-123",
			wantKey: "test-key-123",
		},
		{
			name:    "missing api_key",
			uri:     "bruin://",
			wantErr: true,
		},
		{
			name:    "empty api_key",
			uri:     "bruin://?api_key=",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://?api_key=test",
			wantErr: true,
		},
		{
			name:    "api_key with special characters",
			uri:     "bruin://?api_key=key%2Bwith%3Dspecial",
			wantKey: "key+with=special",
		},
		{
			name:    "api_token alias",
			uri:     "bruin://?api_token=token-123",
			wantKey: "token-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.wantKey {
				t.Errorf("parseURI() = %v, want %v", got, tt.wantKey)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		if !isValidTable(table) {
			t.Errorf("isValidTable(%q) = false, want true", table)
		}
	}

	invalidTables := []string{"", "unknown", "Pipelines", "ASSETS", "pipeline"}
	for _, table := range invalidTables {
		if isValidTable(table) {
			t.Errorf("isValidTable(%q) = true, want false", table)
		}
	}
}

func TestPickFields(t *testing.T) {
	src := map[string]any{
		"name":        "test",
		"description": "desc",
		"extra":       "should be excluded",
		"nested":      map[string]any{"key": "val"},
	}

	result := pickFields(src, []string{"name", "description", "missing", "nested"})

	if result["name"] != "test" {
		t.Errorf("expected name=test, got %v", result["name"])
	}
	if result["description"] != "desc" {
		t.Errorf("expected description=desc, got %v", result["description"])
	}
	if _, ok := result["extra"]; ok {
		t.Error("extra field should not be present")
	}
	if _, ok := result["missing"]; ok {
		t.Error("missing field should not be present")
	}
	if result["nested"] == nil {
		t.Error("nested field should be present")
	}
}

func TestJsonUseNumber(t *testing.T) {
	input := `[{"id": 9007199254740993, "name": "test", "ratio": 3.14}]`
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()

	var items []map[string]any
	if err := decoder.Decode(&items); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	id, ok := items[0]["id"].(json.Number)
	if !ok {
		t.Fatalf("expected json.Number for id, got %T", items[0]["id"])
	}
	if id.String() != "9007199254740993" {
		t.Errorf("expected 9007199254740993, got %s", id.String())
	}

	ratio, ok := items[0]["ratio"].(json.Number)
	if !ok {
		t.Fatalf("expected json.Number for ratio, got %T", items[0]["ratio"])
	}
	f, err := ratio.Float64()
	if err != nil {
		t.Fatalf("failed to convert ratio to float64: %v", err)
	}
	if f != 3.14 {
		t.Errorf("expected 3.14, got %f", f)
	}
}
