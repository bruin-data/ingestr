package duckdb

import (
	"testing"
)

func TestParseDBPath_MotherDuck(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		expected    string
		expectError bool
	}{
		{
			name:     "motherduck_with_database",
			uri:      "motherduck://mydb?token=test123",
			expected: "md:mydb?motherduck_token=test123",
		},
		{
			name:     "motherduck_without_database_uses_default",
			uri:      "motherduck://?token=test123",
			expected: "md:?motherduck_token=test123",
		},
		{
			name:     "md_scheme_with_database",
			uri:      "md://mydb?token=test123",
			expected: "md:mydb?motherduck_token=test123",
		},
		{
			name:     "md_scheme_without_database_uses_default",
			uri:      "md://?token=test123",
			expected: "md:?motherduck_token=test123",
		},
		{
			name:        "motherduck_without_token",
			uri:         "motherduck://mydb",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDBPath(tt.uri)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for URI %s, got nil", tt.uri)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for URI %s: %v", tt.uri, err)
				return
			}
			if result != tt.expected {
				t.Errorf("parseDBPath(%s) = %s, want %s", tt.uri, result, tt.expected)
			}
		})
	}
}

func TestBuildConnectionString_MotherDuck(t *testing.T) {
	d := NewDialect()

	tests := []struct {
		name        string
		uri         string
		expected    string
		expectError bool
	}{
		{
			name:     "motherduck_uri",
			uri:      "motherduck://mydb?token=test123",
			expected: "driver=duckdb;path=md:mydb?motherduck_token=test123",
		},
		{
			name:     "md_uri",
			uri:      "md://mydb?token=test123",
			expected: "driver=duckdb;path=md:mydb?motherduck_token=test123",
		},
		{
			name:     "duckdb_uri_unchanged",
			uri:      "duckdb:///:memory:",
			expected: "driver=duckdb;path=:memory:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := d.BuildConnectionString(tt.uri)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for URI %s, got nil", tt.uri)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for URI %s: %v", tt.uri, err)
				return
			}
			if result != tt.expected {
				t.Errorf("BuildConnectionString(%s) = %s, want %s", tt.uri, result, tt.expected)
			}
		})
	}
}

func TestSchemes_IncludesMotherDuck(t *testing.T) {
	d := NewDialect()
	schemes := d.Schemes()

	expected := map[string]bool{"duckdb": true, "motherduck": true, "md": true}
	if len(schemes) != len(expected) {
		t.Errorf("expected %d schemes, got %d: %v", len(expected), len(schemes), schemes)
	}
	for _, s := range schemes {
		if !expected[s] {
			t.Errorf("unexpected scheme: %s", s)
		}
	}
}
