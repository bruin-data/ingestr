package pipedrive

import (
	"encoding/json"
	"testing"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    string
		wantErr bool
	}{
		{
			name: "valid URI",
			uri:  "pipedrive://?api_token=abc123",
			want: "abc123",
		},
		{
			name:    "wrong scheme",
			uri:     "hubspot://?api_token=abc123",
			wantErr: true,
		},
		{
			name:    "missing api_token",
			uri:     "pipedrive://?foo=bar",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "pipedrive://",
			wantErr: true,
		},
		{
			name:    "empty query",
			uri:     "pipedrive://?",
			wantErr: true,
		},
		{
			name: "api_token with special chars",
			uri:  "pipedrive://?api_token=abc-123_def",
			want: "abc-123_def",
		},
		{
			name:    "empty api_token value",
			uri:     "pipedrive://?api_token=",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseURI() = %v, want %v", got, tt.want)
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

	invalidTables := []string{"", "unknown", "DEALS", "Persons", "PIPELINES", "Stages", "deal_participants", "deal_flow"}
	for _, table := range invalidTables {
		if isValidTable(table) {
			t.Errorf("isValidTable(%q) = true, want false", table)
		}
	}
}

func TestParsePipedriveTime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "standard pipedrive format",
			input: "2026-03-19 10:30:00",
		},
		{
			name:  "RFC3339 format",
			input: "2026-03-19T10:30:00Z",
		},
		{
			name:    "invalid format",
			input:   "not-a-date",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePipedriveTime(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePipedriveTime(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestJsonUseNumber(t *testing.T) {
	t.Run("large integer preserved", func(t *testing.T) {
		data := []byte(`{"id": 9007199254740993}`)
		var result map[string]any
		if err := jsonUseNumber(data, &result); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		id, ok := result["id"].(json.Number)
		if !ok {
			t.Fatal("id is not json.Number")
		}
		if id.String() != "9007199254740993" {
			t.Errorf("id = %v, want 9007199254740993", id)
		}
	})

	t.Run("float preserved", func(t *testing.T) {
		data := []byte(`{"value": 99.99}`)
		var result map[string]any
		if err := jsonUseNumber(data, &result); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		val, ok := result["value"].(json.Number)
		if !ok {
			t.Fatal("value is not json.Number")
		}
		if val.String() != "99.99" {
			t.Errorf("value = %v, want 99.99", val)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		data := []byte(`{invalid}`)
		var result map[string]any
		if err := jsonUseNumber(data, &result); err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}
