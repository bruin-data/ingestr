package customerio

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
		wantKey   string
		wantURL   string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "valid US region (default)",
			uri:     "customerio://?api_key=abc123",
			wantKey: "abc123",
			wantURL: "https://api.customer.io",
		},
		{
			name:    "valid EU region",
			uri:     "customerio://?api_key=abc123&region=eu",
			wantKey: "abc123",
			wantURL: "https://api-eu.customer.io",
		},
		{
			name:    "EU region case insensitive",
			uri:     "customerio://?api_key=abc123&region=EU",
			wantKey: "abc123",
			wantURL: "https://api-eu.customer.io",
		},
		{
			name:    "explicit US region",
			uri:     "customerio://?api_key=abc123&region=us",
			wantKey: "abc123",
			wantURL: "https://api.customer.io",
		},
		{
			name:      "invalid region rejected",
			uri:       "customerio://?api_key=abc123&region=ap",
			wantErr:   true,
			errSubstr: "invalid region 'ap'",
		},
		{
			name:      "missing api_key",
			uri:       "customerio://?region=eu",
			wantErr:   true,
			errSubstr: "api_key is required",
		},
		{
			name:      "empty URI",
			uri:       "customerio://",
			wantErr:   true,
			errSubstr: "api_key is required",
		},
		{
			name:      "wrong scheme",
			uri:       "http://?api_key=abc123",
			wantErr:   true,
			errSubstr: "must start with customerio://",
		},
		{
			name:      "no query params",
			uri:       "customerio://",
			wantErr:   true,
			errSubstr: "api_key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiKey, baseURL, err := parseURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantKey, apiKey)
			assert.Equal(t, tt.wantURL, baseURL)
		})
	}
}

func TestExtractID(t *testing.T) {
	tests := []struct {
		name  string
		item  map[string]interface{}
		field string
		want  string
	}{
		{
			name:  "string ID",
			item:  map[string]interface{}{"id": "abc123"},
			field: "id",
			want:  "abc123",
		},
		{
			name:  "json.Number ID",
			item:  map[string]interface{}{"id": json.Number("12345")},
			field: "id",
			want:  "12345",
		},
		{
			name:  "float64 ID",
			item:  map[string]interface{}{"id": float64(42)},
			field: "id",
			want:  "42",
		},
		{
			name:  "missing field",
			item:  map[string]interface{}{"name": "test"},
			field: "id",
			want:  "",
		},
		{
			name:  "nil value",
			item:  map[string]interface{}{"id": nil},
			field: "id",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractID(tt.item, tt.field)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractStringField(t *testing.T) {
	tests := []struct {
		name  string
		item  map[string]interface{}
		field string
		want  string
	}{
		{
			name:  "string value",
			item:  map[string]interface{}{"cio_id": "cust_123"},
			field: "cio_id",
			want:  "cust_123",
		},
		{
			name:  "numeric value stringified",
			item:  map[string]interface{}{"cio_id": json.Number("456")},
			field: "cio_id",
			want:  "456",
		},
		{
			name:  "missing field",
			item:  map[string]interface{}{},
			field: "cio_id",
			want:  "",
		},
		{
			name:  "nil value",
			item:  map[string]interface{}{"cio_id": nil},
			field: "cio_id",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStringField(tt.item, tt.field)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilterByUnixTimestamp(t *testing.T) {
	start := time.Unix(1000, 0)
	end := time.Unix(2000, 0)

	items := []map[string]interface{}{
		{"id": "1", "updated": json.Number("500")},
		{"id": "2", "updated": json.Number("1000")},
		{"id": "3", "updated": json.Number("1500")},
		{"id": "4", "updated": json.Number("2000")},
		{"id": "5", "updated": json.Number("2500")},
		{"id": "6"},
	}

	t.Run("filter with start and end", func(t *testing.T) {
		filtered := filterByUnixTimestamp(items, "updated", &start, &end)
		assert.Len(t, filtered, 3)
		assert.Equal(t, "2", filtered[0]["id"])
		assert.Equal(t, "3", filtered[1]["id"])
		assert.Equal(t, "4", filtered[2]["id"])
	})

	t.Run("filter with start only", func(t *testing.T) {
		filtered := filterByUnixTimestamp(items, "updated", &start, nil)
		assert.Len(t, filtered, 4)
	})

	t.Run("filter with end only", func(t *testing.T) {
		filtered := filterByUnixTimestamp(items, "updated", nil, &end)
		assert.Len(t, filtered, 4)
	})

	t.Run("no filter", func(t *testing.T) {
		filtered := filterByUnixTimestamp(items, "updated", nil, nil)
		assert.Len(t, filtered, 6)
	})
}

func TestGetUnixTimestamp(t *testing.T) {
	tests := []struct {
		name   string
		item   map[string]interface{}
		field  string
		want   int64
		wantOK bool
	}{
		{
			name:   "json.Number integer",
			item:   map[string]interface{}{"ts": json.Number("1609459200")},
			field:  "ts",
			want:   1609459200,
			wantOK: true,
		},
		{
			name:   "json.Number float",
			item:   map[string]interface{}{"ts": json.Number("1609459200.5")},
			field:  "ts",
			want:   1609459200,
			wantOK: true,
		},
		{
			name:   "float64",
			item:   map[string]interface{}{"ts": float64(1609459200)},
			field:  "ts",
			want:   1609459200,
			wantOK: true,
		},
		{
			name:   "missing field",
			item:   map[string]interface{}{},
			field:  "ts",
			want:   0,
			wantOK: false,
		},
		{
			name:   "nil value",
			item:   map[string]interface{}{"ts": nil},
			field:  "ts",
			want:   0,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := getUnixTimestamp(tt.item, tt.field)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestJsonDecode(t *testing.T) {
	t.Run("preserves large integers", func(t *testing.T) {
		data := []byte(`{"id": 2033513821949367296, "name": "test"}`)
		result, err := jsonDecode(data)
		require.NoError(t, err)

		id, ok := result["id"].(json.Number)
		require.True(t, ok, "id should be json.Number, got %T", result["id"])
		assert.Equal(t, "2033513821949367296", id.String())
	})

	t.Run("preserves floats", func(t *testing.T) {
		data := []byte(`{"score": 3.14}`)
		result, err := jsonDecode(data)
		require.NoError(t, err)

		score, ok := result["score"].(json.Number)
		require.True(t, ok)
		f, err := score.Float64()
		require.NoError(t, err)
		assert.InDelta(t, 3.14, f, 0.001)
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		data := []byte(`{invalid}`)
		_, err := jsonDecode(data)
		require.Error(t, err)
	})
}

func TestExtractItems(t *testing.T) {
	t.Run("valid array", func(t *testing.T) {
		body := map[string]interface{}{
			"broadcasts": []interface{}{
				map[string]interface{}{"id": "1"},
				map[string]interface{}{"id": "2"},
			},
		}
		items := extractItems(body, "broadcasts")
		assert.Len(t, items, 2)
	})

	t.Run("missing key", func(t *testing.T) {
		body := map[string]interface{}{}
		items := extractItems(body, "broadcasts")
		assert.Nil(t, items)
	})

	t.Run("not an array", func(t *testing.T) {
		body := map[string]interface{}{
			"broadcasts": "not an array",
		}
		items := extractItems(body, "broadcasts")
		assert.Nil(t, items)
	})

	t.Run("skips non-map items", func(t *testing.T) {
		body := map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"id": "1"},
				"not a map",
				map[string]interface{}{"id": "3"},
			},
		}
		items := extractItems(body, "data")
		assert.Len(t, items, 2)
	})
}

func TestFlattenMetricsSeries(t *testing.T) {
	t.Run("valid series", func(t *testing.T) {
		body := map[string]interface{}{
			"series": map[string]interface{}{
				"series": []interface{}{
					map[string]interface{}{"metric": json.Number("100")},
					map[string]interface{}{"metric": json.Number("200")},
				},
			},
		}

		items, err := flattenMetricsSeries(body, "days")
		require.NoError(t, err)
		assert.Len(t, items, 2)
		assert.Equal(t, "days", items[0]["period"])
		assert.Equal(t, 0, items[0]["step_index"])
		assert.Equal(t, "days", items[1]["period"])
		assert.Equal(t, 1, items[1]["step_index"])
	})

	t.Run("missing series key", func(t *testing.T) {
		body := map[string]interface{}{}
		items, err := flattenMetricsSeries(body, "days")
		require.NoError(t, err)
		assert.Nil(t, items)
	})
}

func TestGetMaxSteps(t *testing.T) {
	assert.Equal(t, 24, getMaxSteps("hours", false))
	assert.Equal(t, 45, getMaxSteps("days", false))
	assert.Equal(t, 12, getMaxSteps("weeks", false))
	assert.Equal(t, 12, getMaxSteps("months", false))
	assert.Equal(t, 121, getMaxSteps("months", true))
	assert.Equal(t, 12, getMaxSteps("unknown", false))
}
