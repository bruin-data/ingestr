package klaviyo

import (
	"encoding/json"
	"testing"
	"time"
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
			uri:     "klaviyo://?api_key=pk_abc123",
			wantKey: "pk_abc123",
		},
		{
			name:    "valid URI with extra params",
			uri:     "klaviyo://?api_key=pk_abc123&extra=val",
			wantKey: "pk_abc123",
		},
		{
			name:    "missing api_key",
			uri:     "klaviyo://?other=val",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "klaviyo://",
			wantErr: true,
		},
		{
			name:    "empty with question mark",
			uri:     "klaviyo://?",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://?api_key=pk_abc123",
			wantErr: true,
		},
		{
			name:    "empty api_key value",
			uri:     "klaviyo://?api_key=",
			wantErr: true,
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
			if got != tt.wantKey {
				t.Errorf("parseURI(%q) = %q, want %q", tt.uri, got, tt.wantKey)
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

	invalidTables := []string{"", "unknown", "EVENTS", "Events", "users", "orders"}
	for _, table := range invalidTables {
		if isValidTable(table) {
			t.Errorf("isValidTable(%q) = true, want false", table)
		}
	}
}

func TestFlattenAttributes(t *testing.T) {
	items := []map[string]interface{}{
		{
			"id":   "123",
			"type": "event",
			"attributes": map[string]interface{}{
				"name":  "Test Event",
				"email": "test@example.com",
			},
		},
		{
			"id":   "456",
			"type": "profile",
		},
	}

	result := flattenAttributes(items)

	if result[0]["name"] != "Test Event" {
		t.Errorf("expected name=Test Event, got %v", result[0]["name"])
	}
	if result[0]["email"] != "test@example.com" {
		t.Errorf("expected email=test@example.com, got %v", result[0]["email"])
	}
	if _, ok := result[0]["attributes"]; ok {
		t.Error("expected attributes key to be removed after flattening")
	}
	if result[0]["id"] != "123" {
		t.Errorf("expected id=123, got %v", result[0]["id"])
	}

	if result[1]["id"] != "456" {
		t.Errorf("expected id=456, got %v", result[1]["id"])
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
			input: `{"price": 29.99}`,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]interface{}
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

func TestSplitTimeRange(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)

	t.Run("splits into N windows", func(t *testing.T) {
		windows := splitTimeRange(start, end, 4)
		if len(windows) != 4 {
			t.Fatalf("expected 4 windows, got %d", len(windows))
		}
		if !windows[0].start.Equal(start) {
			t.Errorf("first window start = %v, want %v", windows[0].start, start)
		}
		if !windows[len(windows)-1].end.Equal(end) {
			t.Errorf("last window end = %v, want %v", windows[len(windows)-1].end, end)
		}
		for i := 1; i < len(windows); i++ {
			if !windows[i].start.Equal(windows[i-1].end) {
				t.Errorf("gap between window %d and %d", i-1, i)
			}
		}
	})

	t.Run("n=1 returns single window", func(t *testing.T) {
		windows := splitTimeRange(start, end, 1)
		if len(windows) != 1 {
			t.Fatalf("expected 1 window, got %d", len(windows))
		}
	})

	t.Run("tiny range returns single window", func(t *testing.T) {
		tinyEnd := start.Add(30 * time.Minute)
		windows := splitTimeRange(start, tinyEnd, 4)
		if len(windows) != 1 {
			t.Fatalf("expected 1 window for tiny range, got %d", len(windows))
		}
	})

	t.Run("zero duration returns single window", func(t *testing.T) {
		windows := splitTimeRange(start, start, 4)
		if len(windows) != 1 {
			t.Fatalf("expected 1 window for zero duration, got %d", len(windows))
		}
	})
}

func TestBuildFilterURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		params   map[string]string
		wantBase string
	}{
		{
			name:     "no params",
			endpoint: "/events/",
			params:   map[string]string{},
			wantBase: "/events/",
		},
		{
			name:     "with sort param",
			endpoint: "/events/",
			params:   map[string]string{"sort": "-datetime"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildFilterURL(tt.endpoint, tt.params)
			if len(tt.params) == 0 {
				if result != tt.wantBase {
					t.Errorf("buildFilterURL() = %q, want %q", result, tt.wantBase)
				}
			} else {
				if len(result) <= len(tt.endpoint) {
					t.Errorf("buildFilterURL() result %q too short, expected params appended", result)
				}
			}
		})
	}
}
