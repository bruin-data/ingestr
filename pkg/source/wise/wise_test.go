package wise

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			uri:  "wise://?api_key=test-key-123",
			want: "test-key-123",
		},
		{
			name: "valid URI without question mark",
			uri:  "wise://api_key=test-key-123",
			want: "test-key-123",
		},
		{
			name:    "missing api_key",
			uri:     "wise://?other=value",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "wise://",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://localhost",
			wantErr: true,
		},
		{
			name:    "empty api_key",
			uri:     "wise://?api_key=",
			wantErr: true,
		},
		{
			name:    "just question mark",
			uri:     "wise://?",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBalanceIntervalFiltering(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		balances      []map[string]interface{}
		intervalStart *time.Time
		intervalEnd   *time.Time
		wantCount     int
	}{
		{
			name: "no interval returns all",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2024-01-01T00:00:00Z"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			wantCount: 2,
		},
		{
			name: "filters before interval start",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2024-06-01T00:00:00Z"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "filters after interval end",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2025-03-01T00:00:00Z"},
				{"id": "2", "modificationTime": "2025-09-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "missing modificationTime is skipped",
			balances: []map[string]interface{}{
				{"id": "1"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "null modificationTime is skipped",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": nil},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "non-string modificationTime is skipped",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": 12345},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "unparseable modificationTime is skipped",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "not-a-date"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "millisecond format is accepted",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2025-03-01T12:30:45.123Z"},
			},
			intervalStart: &start,
			intervalEnd:   &end,
			wantCount:     1,
		},
		{
			name: "only interval start set",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2024-06-01T00:00:00Z"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
				{"id": "3", "modificationTime": "2026-01-01T00:00:00Z"},
			},
			intervalStart: &start,
			wantCount:     2,
		},
		{
			name: "only interval end set",
			balances: []map[string]interface{}{
				{"id": "1", "modificationTime": "2024-06-01T00:00:00Z"},
				{"id": "2", "modificationTime": "2025-03-01T00:00:00Z"},
				{"id": "3", "modificationTime": "2026-01-01T00:00:00Z"},
			},
			intervalEnd: &end,
			wantCount:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterBalances(tt.balances, tt.intervalStart, tt.intervalEnd)
			assert.Equal(t, tt.wantCount, len(got), "filtered balance count mismatch")
		})
	}
}
