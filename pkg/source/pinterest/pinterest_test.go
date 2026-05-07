package pinterest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
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
			uri:  "pinterest://?access_token=abc123",
			want: "abc123",
		},
		{
			name: "valid URI with long token",
			uri:  "pinterest://?access_token=pina_ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890",
			want: "pina_ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890",
		},
		{
			name:    "wrong scheme",
			uri:     "http://example.com?access_token=abc",
			wantErr: true,
		},
		{
			name:    "missing access_token",
			uri:     "pinterest://?other_param=abc",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "pinterest://",
			wantErr: true,
		},
		{
			name:    "empty query",
			uri:     "pinterest://?",
			wantErr: true,
		},
		{
			name:    "empty access_token value",
			uri:     "pinterest://?access_token=",
			wantErr: true,
		},
		{
			name: "access_token with extra params",
			uri:  "pinterest://?access_token=token123&extra=val",
			want: "token123",
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
	tests := []struct {
		table string
		want  bool
	}{
		{"pins", true},
		{"boards", true},
		{"invalid", false},
		{"", false},
		{"Pins", false},
		{"BOARDS", false},
		{"pin", false},
		{"board", false},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			if got := isValidTable(tt.table); got != tt.want {
				t.Errorf("isValidTable(%q) = %v, want %v", tt.table, got, tt.want)
			}
		})
	}
}

func TestFilterByInterval(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	items := []map[string]any{
		{"id": "1", "created_at": "2024-12-01T00:00:00Z"},
		{"id": "2", "created_at": "2025-03-15T00:00:00Z"},
		{"id": "3", "created_at": "2025-07-01T00:00:00Z"},
		{"id": "4", "created_at": "2025-01-01T00:00:00Z"},
		{"id": "5"},
		{"id": "6", "created_at": "not-a-date"},
	}

	t.Run("no interval", func(t *testing.T) {
		result := filterByInterval(items, source.ReadOptions{})
		if len(result) != 6 {
			t.Errorf("expected 6 items, got %d", len(result))
		}
	})

	t.Run("start and end", func(t *testing.T) {
		result := filterByInterval(items, source.ReadOptions{
			IntervalStart: &start,
			IntervalEnd:   &end,
		})
		// id=2 (March 2025) passes, id=1 (before start) fails, id=3 (after end) fails,
		// id=4 (exactly at start, not after) fails, id=5 (no date) passes, id=6 (bad date) passes
		if len(result) != 3 {
			t.Errorf("expected 3 items, got %d", len(result))
			for _, item := range result {
				t.Logf("  got: %v", item)
			}
		}
	})

	t.Run("start only", func(t *testing.T) {
		result := filterByInterval(items, source.ReadOptions{
			IntervalStart: &start,
		})
		// id=2, id=3 pass (after start), id=1 fails, id=4 fails (equal, not after)
		// id=5, id=6 pass (no parseable date)
		if len(result) != 4 {
			t.Errorf("expected 4 items, got %d", len(result))
		}
	})

	t.Run("end only", func(t *testing.T) {
		result := filterByInterval(items, source.ReadOptions{
			IntervalEnd: &end,
		})
		// id=1, id=2, id=4 pass (before/at end), id=3 fails (after end)
		// id=5, id=6 pass
		if len(result) != 5 {
			t.Errorf("expected 5 items, got %d", len(result))
		}
	})
}

func TestJsonUseNumber(t *testing.T) {
	t.Run("large integer preserved", func(t *testing.T) {
		data := []byte(`{"id": 9007199254740993}`)
		var result map[string]any
		if err := jsonUseNumber(data, &result); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		num, ok := result["id"].(json.Number)
		if !ok {
			t.Fatalf("expected json.Number, got %T", result["id"])
		}
		if num.String() != "9007199254740993" {
			t.Errorf("expected 9007199254740993, got %s", num.String())
		}
	})

	t.Run("float preserved", func(t *testing.T) {
		data := []byte(`{"value": 3.14}`)
		var result map[string]any
		if err := jsonUseNumber(data, &result); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		num, ok := result["value"].(json.Number)
		if !ok {
			t.Fatalf("expected json.Number, got %T", result["value"])
		}
		f, err := num.Float64()
		if err != nil {
			t.Fatalf("unexpected error converting to float: %v", err)
		}
		if f != 3.14 {
			t.Errorf("expected 3.14, got %f", f)
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
