package hostaway

import (
	"encoding/json"
	"testing"
	"time"
)

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    string
		wantErr bool
	}{
		{
			name: "valid URI",
			uri:  "hostaway://?api_key=test-token-123",
			want: "test-token-123",
		},
		{
			name:    "missing api_key",
			uri:     "hostaway://?other=value",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "http://example.com?api_key=test",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "hostaway://",
			wantErr: true,
		},
		{
			name:    "just question mark",
			uri:     "hostaway://?",
			wantErr: true,
		},
		{
			name: "api_key with special characters",
			uri:  "hostaway://?api_key=eyJhbGciOiJSUzI1NiJ9.test",
			want: "eyJhbGciOiJSUzI1NiJ9.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseURI(%q) expected error, got nil", tt.uri)
				}
				return
			}
			if err != nil {
				t.Errorf("parseURI(%q) unexpected error: %v", tt.uri, err)
				return
			}
			if got != tt.want {
				t.Errorf("parseURI(%q) = %q, want %q", tt.uri, got, tt.want)
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

	invalid := []string{"", "unknown", "LISTINGS", "Listings", "listing"}
	for _, table := range invalid {
		if isValidTable(table) {
			t.Errorf("isValidTable(%q) = true, want false", table)
		}
	}
}

func TestExtractResult(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
		wantErr bool
		wantNil bool
	}{
		{
			name:    "array result",
			input:   `{"status":"success","result":[{"id":1,"name":"test"},{"id":2,"name":"test2"}]}`,
			wantLen: 2,
		},
		{
			name:    "single object result",
			input:   `{"status":"success","result":{"id":1,"name":"test"}}`,
			wantLen: 1,
		},
		{
			name:    "empty array result",
			input:   `{"status":"success","result":[]}`,
			wantLen: 0,
		},
		{
			name:    "null result",
			input:   `{"status":"success","result":null}`,
			wantNil: true,
		},
		{
			name:    "missing result field",
			input:   `{"status":"success"}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractResult([]byte(tt.input))
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
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("got %d items, want %d", len(got), tt.wantLen)
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
			input: `{"price": 99.99}`,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]any
			err := jsonUseNumber([]byte(tt.input), &result)
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

func TestFilterByInterval(t *testing.T) {
	t.Run("no interval returns all", func(t *testing.T) {
		item := map[string]any{"updatedOn": "2025-01-15T10:00:00Z"}
		if !filterByInterval(item, "updatedOn", nil, nil) {
			t.Error("expected true with no interval")
		}
	})

	t.Run("missing field with no interval", func(t *testing.T) {
		item := map[string]any{"other": "value"}
		if !filterByInterval(item, "updatedOn", nil, nil) {
			t.Error("expected true when field missing and no interval")
		}
	})

	t.Run("missing field with interval", func(t *testing.T) {
		item := map[string]any{"other": "value"}
		start := mustParseTime("2025-01-01T00:00:00Z")
		if filterByInterval(item, "updatedOn", &start, nil) {
			t.Error("expected false when field missing with interval")
		}
	})

	t.Run("within range", func(t *testing.T) {
		item := map[string]any{"updatedOn": "2025-01-15T10:00:00Z"}
		start := mustParseTime("2025-01-01T00:00:00Z")
		end := mustParseTime("2025-02-01T00:00:00Z")
		if !filterByInterval(item, "updatedOn", &start, &end) {
			t.Error("expected true for item within range")
		}
	})

	t.Run("before range", func(t *testing.T) {
		item := map[string]any{"updatedOn": "2024-12-01T10:00:00Z"}
		start := mustParseTime("2025-01-01T00:00:00Z")
		if filterByInterval(item, "updatedOn", &start, nil) {
			t.Error("expected false for item before range")
		}
	})

	t.Run("after range", func(t *testing.T) {
		item := map[string]any{"updatedOn": "2025-03-01T10:00:00Z"}
		end := mustParseTime("2025-02-01T00:00:00Z")
		if filterByInterval(item, "updatedOn", nil, &end) {
			t.Error("expected false for item after range")
		}
	})

	t.Run("non-RFC3339 format", func(t *testing.T) {
		item := map[string]any{"updatedOn": "2025-01-15 10:00:00"}
		start := mustParseTime("2025-01-01T00:00:00Z")
		end := mustParseTime("2025-02-01T00:00:00Z")
		if !filterByInterval(item, "updatedOn", &start, &end) {
			t.Error("expected true for non-RFC3339 format within range")
		}
	})

	t.Run("null field with end-only interval excluded", func(t *testing.T) {
		item := map[string]any{"id": "123", "updatedOn": nil}
		end := mustParseTime("2025-02-01T00:00:00Z")
		if filterByInterval(item, "updatedOn", nil, &end) {
			t.Error("expected false when field is null with end interval")
		}
	})

	t.Run("empty string field with interval excluded", func(t *testing.T) {
		item := map[string]any{"id": "456", "updatedOn": ""}
		start := mustParseTime("2025-01-01T00:00:00Z")
		if filterByInterval(item, "updatedOn", &start, nil) {
			t.Error("expected false when field is empty string with interval")
		}
	})
}
