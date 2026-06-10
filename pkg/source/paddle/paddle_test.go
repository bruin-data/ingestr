package paddle

import (
	"testing"
	"time"
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
			uri:     "paddle://?api_key=pdl_live_apikey_123",
			wantKey: "pdl_live_apikey_123",
		},
		{
			name:    "api_key with special characters",
			uri:     "paddle://?api_key=pdl_live_apikey_abc_DEF_123",
			wantKey: "pdl_live_apikey_abc_DEF_123",
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
		{"pdl_live_apikey_123", prodBaseURL},
		{"pdl_sdbx_apikey_123", sandboxBaseURL},
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
