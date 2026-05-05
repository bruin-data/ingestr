package applovin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAppLovinURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantKey   string
		wantError bool
	}{
		{
			name:    "valid URI with api_key",
			uri:     "applovin://?api_key=test_key_123",
			wantKey: "test_key_123",
		},
		{
			name:    "valid URI with extra params",
			uri:     "applovin://?api_key=my_key&other=value",
			wantKey: "my_key",
		},
		{
			name:      "missing api_key",
			uri:       "applovin://?other=value",
			wantError: true,
		},
		{
			name:      "empty api_key",
			uri:       "applovin://?api_key=",
			wantError: true,
		},
		{
			name:      "invalid URI",
			uri:       "://invalid",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parseAppLovinURI(tt.uri)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantKey, key)
			}
		})
	}
}

func TestParseTimeInterval(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		start     interface{}
		end       interface{}
		wantError bool
	}{
		{
			name:  "valid time.Time values",
			start: now,
			end:   now.AddDate(0, 0, 7),
		},
		{
			name:  "valid *time.Time values",
			start: &now,
			end:   func() *time.Time { t := now.AddDate(0, 0, 7); return &t }(),
		},
		{
			name:      "nil start",
			start:     nil,
			end:       now,
			wantError: true,
		},
		{
			name:      "nil end",
			start:     now,
			end:       nil,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := parseTimeInterval(tt.start, tt.end)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.False(t, start.IsZero())
				assert.False(t, end.IsZero())
			}
		})
	}
}

func TestParseTimestamp(t *testing.T) {
	now := time.Now()
	defaultTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		value    interface{}
		defVal   time.Time
		expected time.Time
	}{
		{
			name:     "time.Time value",
			value:    now,
			defVal:   defaultTime,
			expected: now,
		},
		{
			name:     "*time.Time value",
			value:    &now,
			defVal:   defaultTime,
			expected: now,
		},
		{
			name:     "nil returns default",
			value:    nil,
			defVal:   defaultTime,
			expected: defaultTime,
		},
		{
			name:     "nil pointer returns default",
			value:    (*time.Time)(nil),
			defVal:   defaultTime,
			expected: defaultTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTimestamp(tt.value, tt.defVal)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExcludeColumns(t *testing.T) {
	tests := []struct {
		name     string
		columns  []string
		exclude  map[string]bool
		expected []string
	}{
		{
			name:     "exclude some columns",
			columns:  []string{"a", "b", "c", "d"},
			exclude:  map[string]bool{"b": true, "d": true},
			expected: []string{"a", "c"},
		},
		{
			name:     "exclude none",
			columns:  []string{"a", "b", "c"},
			exclude:  map[string]bool{},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "exclude all",
			columns:  []string{"a", "b"},
			exclude:  map[string]bool{"a": true, "b": true},
			expected: []string{},
		},
		{
			name:     "exclude non-existent",
			columns:  []string{"a", "b"},
			exclude:  map[string]bool{"x": true, "y": true},
			expected: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := excludeColumns(tt.columns, tt.exclude)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDimensionColumns(t *testing.T) {
	tests := []struct {
		name     string
		columns  []string
		expected []string
	}{
		{
			name:     "day in middle moves to first",
			columns:  []string{"country", "day", "platform"},
			expected: []string{"day", "country", "platform"},
		},
		{
			name:     "day already first",
			columns:  []string{"day", "country", "platform"},
			expected: []string{"day", "country", "platform"},
		},
		{
			name:     "no day column",
			columns:  []string{"country", "platform"},
			expected: []string{"country", "platform"},
		},
		{
			name:     "filters out non-dimensions",
			columns:  []string{"day", "clicks", "country", "impressions"},
			expected: []string{"day", "country"},
		},
		{
			name:     "empty columns",
			columns:  []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getDimensionColumns(tt.columns)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCreateCustomReportTable(t *testing.T) {
	source := NewAppLovinSource()

	tests := []struct {
		name      string
		spec      string
		wantError bool
	}{
		{
			name:      "valid spec",
			spec:      "custom:report:publisher:day,country,clicks",
			wantError: false,
		},
		{
			name:      "valid advertiser spec",
			spec:      "custom:probabilisticReport:advertiser:day,campaign,impressions",
			wantError: false,
		},
		{
			name:      "invalid format - missing parts",
			spec:      "custom:report",
			wantError: true,
		},
		{
			name:      "invalid report_type",
			spec:      "custom:report:invalid_type:day,country",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table, err := source.createCustomReportTable(tt.spec)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, table)
			}
		})
	}
}
