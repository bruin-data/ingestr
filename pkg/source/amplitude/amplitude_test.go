package amplitude

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		want      amplitudeCredentials
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid URI, default region",
			uri:  "amplitude://?api_key=key123&secret_key=secret456",
			want: amplitudeCredentials{apiKey: "key123", secretKey: "secret456", region: "us"},
		},
		{
			name: "valid URI, eu region",
			uri:  "amplitude://?api_key=key123&secret_key=secret456&region=eu",
			want: amplitudeCredentials{apiKey: "key123", secretKey: "secret456", region: "eu"},
		},
		{
			name: "region is case-insensitive",
			uri:  "amplitude://?api_key=k&secret_key=s&region=EU",
			want: amplitudeCredentials{apiKey: "k", secretKey: "s", region: "eu"},
		},
		{
			name:      "missing api_key",
			uri:       "amplitude://?secret_key=secret456",
			wantErr:   true,
			errSubstr: "api_key is required",
		},
		{
			name:      "missing secret_key",
			uri:       "amplitude://?api_key=key123",
			wantErr:   true,
			errSubstr: "secret_key is required",
		},
		{
			name:      "invalid region",
			uri:       "amplitude://?api_key=k&secret_key=s&region=jp",
			wantErr:   true,
			errSubstr: "invalid region",
		},
		{
			name:      "wrong scheme",
			uri:       "http://?api_key=k&secret_key=s",
			wantErr:   true,
			errSubstr: "must start with amplitude://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %s to be valid", table)
	}

	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("Events"))
	assert.False(t, isValidTable("EVENTS"))
}

func TestSplitEventWindows(t *testing.T) {
	base := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("windows are hour-disjoint and cover the full range", func(t *testing.T) {
		start := base
		end := base.Add(9 * time.Hour) // 10 inclusive hours
		windows := splitEventWindows(start, end, 4)

		require.NotEmpty(t, windows)
		assert.Equal(t, start, windows[0][0])
		assert.Equal(t, end, windows[len(windows)-1][1])

		for i, w := range windows {
			assert.False(t, w[1].Before(w[0]), "window %d end before start", i)
			if i > 0 {
				// next start is exactly one hour after previous end -> no overlap, no gap
				assert.Equal(t, windows[i-1][1].Add(time.Hour), w[0], "window %d overlaps/gaps with previous", i)
			}
		}
	})

	t.Run("caps window count at available hours", func(t *testing.T) {
		windows := splitEventWindows(base, base.Add(2*time.Hour), 8)
		assert.Len(t, windows, 3)
	})

	t.Run("single window when range is one hour or n<=1", func(t *testing.T) {
		assert.Len(t, splitEventWindows(base, base, 4), 1)
		assert.Len(t, splitEventWindows(base, base.Add(5*time.Hour), 1), 1)
	})
}

func TestJsonUseNumber(t *testing.T) {
	data := []byte(`{"id": 2033513821949367296, "amount": 3.14}`)
	var result map[string]interface{}
	require.NoError(t, jsonUseNumber(data, &result))

	id, ok := result["id"].(json.Number)
	require.True(t, ok, "id should be json.Number, got %T", result["id"])
	i, err := id.Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(2033513821949367296), i)
}
