package allium

import (
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
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
			uri:  "allium://?api_key=test-key-123",
			want: "test-key-123",
		},
		{
			name: "valid URI without question mark",
			uri:  "allium://api_key=test-key-123",
			want: "test-key-123",
		},
		{
			name:    "missing api_key",
			uri:     "allium://?other=value",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "allium://",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://localhost",
			wantErr: true,
		},
		{
			name:    "empty api_key",
			uri:     "allium://?api_key=",
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

func TestIsValidTable(t *testing.T) {
	tests := []struct {
		table string
		valid bool
	}{
		{"query:abc123", true},
		{"query:abc123:network=ethereum", true},
		{"query:", false},
		{"invalid", false},
		{"queries:abc", false},
		{"", false},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.valid, isValidTable(tt.table), "isValidTable(%q)", tt.table)
	}
}

func TestParseTable(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		wantID     string
		wantParams map[string]interface{}
	}{
		{
			name:       "query ID only",
			table:      "query:abc123",
			wantID:     "abc123",
			wantParams: map[string]interface{}{},
		},
		{
			name:   "query ID with single param",
			table:  "query:abc123:network=ethereum",
			wantID: "abc123",
			wantParams: map[string]interface{}{
				"network": "ethereum",
			},
		},
		{
			name:   "query ID with multiple params",
			table:  "query:abc123:network=ethereum&min_value=1000",
			wantID: "abc123",
			wantParams: map[string]interface{}{
				"network":   "ethereum",
				"min_value": "1000",
			},
		},
		{
			name:   "query ID with run config params",
			table:  "query:abc123:limit=5000&compute_profile=standard",
			wantID: "abc123",
			wantParams: map[string]interface{}{
				"limit":           "5000",
				"compute_profile": "standard",
			},
		},
		{
			name:       "query ID with empty params",
			table:      "query:abc123:",
			wantID:     "abc123",
			wantParams: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotParams := parseTable(tt.table)
			assert.Equal(t, tt.wantID, gotID)
			assert.Equal(t, tt.wantParams, gotParams)
		})
	}
}

func TestBuildParameters(t *testing.T) {
	s := &AlliumSource{}

	t.Run("no intervals no user params", func(t *testing.T) {
		params := s.buildParameters(source.ReadOptions{}, nil)
		assert.Empty(t, params)
	})

	t.Run("with intervals", func(t *testing.T) {
		start := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC)
		params := s.buildParameters(source.ReadOptions{
			IntervalStart: &start,
			IntervalEnd:   &end,
		}, nil)

		assert.Equal(t, "2024-01-15", params["start_date"])
		assert.Equal(t, fmt.Sprintf("%d", start.Unix()), params["start_timestamp"])
		assert.Equal(t, "2024-02-15", params["end_date"])
		assert.Equal(t, fmt.Sprintf("%d", end.Unix()), params["end_timestamp"])
	})

	t.Run("user params override intervals", func(t *testing.T) {
		start := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
		userParams := map[string]interface{}{
			"start_date": "2023-06-01",
			"custom":     "value",
		}
		params := s.buildParameters(source.ReadOptions{
			IntervalStart: &start,
		}, userParams)

		assert.Equal(t, "2023-06-01", params["start_date"])
		assert.Equal(t, "value", params["custom"])
	})
}
