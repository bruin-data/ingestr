package g2

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantToken string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "valid URI",
			uri:       "g2://?api_token=abc123",
			wantToken: "abc123",
		},
		{
			name:      "valid URI with host",
			uri:       "g2://default?api_token=abc123",
			wantToken: "abc123",
		},
		{
			name:      "missing api_token",
			uri:       "g2://",
			wantErr:   true,
			errSubstr: "api_token query parameter is required",
		},
		{
			name:      "empty api_token",
			uri:       "g2://?api_token=",
			wantErr:   true,
			errSubstr: "api_token query parameter is required",
		},
		{
			name:      "wrong scheme",
			uri:       "http://?api_token=abc123",
			wantErr:   true,
			errSubstr: "must start with g2://",
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
			assert.Equal(t, tt.wantToken, got.apiToken)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %s to be valid", table)
	}

	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("Products"))
}

func TestFlattenJSONAPIData(t *testing.T) {
	t.Run("flattens attributes with id", func(t *testing.T) {
		data := []interface{}{
			map[string]interface{}{
				"id":   "123",
				"type": "products",
				"attributes": map[string]interface{}{
					"name":   "Test Product",
					"domain": "example.com",
				},
			},
		}

		items := flattenJSONAPIData(data)
		require.Len(t, items, 1)
		assert.Equal(t, "123", items[0]["id"])
		assert.Equal(t, "Test Product", items[0]["name"])
		assert.Equal(t, "example.com", items[0]["domain"])
	})

	t.Run("missing attributes creates empty map with id", func(t *testing.T) {
		data := []interface{}{
			map[string]interface{}{
				"id":   "123",
				"type": "products",
			},
		}

		items := flattenJSONAPIData(data)
		require.Len(t, items, 1)
		assert.Equal(t, "123", items[0]["id"])
	})

	t.Run("empty data returns empty slice", func(t *testing.T) {
		items := flattenJSONAPIData([]interface{}{})
		assert.Empty(t, items)
	})
}

func TestJsonUseNumber(t *testing.T) {
	t.Run("preserves large integers", func(t *testing.T) {
		data := []byte(`{"id": 2033513821949367296, "name": "test"}`)
		var result map[string]interface{}
		err := jsonUseNumber(data, &result)
		require.NoError(t, err)

		id, ok := result["id"].(json.Number)
		require.True(t, ok, "id should be json.Number, got %T", result["id"])
		assert.Equal(t, "2033513821949367296", id.String())
	})

	t.Run("preserves floats", func(t *testing.T) {
		data := []byte(`{"score": 3.14}`)
		var result map[string]interface{}
		err := jsonUseNumber(data, &result)
		require.NoError(t, err)

		score, ok := result["score"].(json.Number)
		require.True(t, ok)
		f, err := score.Float64()
		require.NoError(t, err)
		assert.InDelta(t, 3.14, f, 0.001)
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		data := []byte(`{invalid}`)
		var result map[string]interface{}
		err := jsonUseNumber(data, &result)
		require.Error(t, err)
	})
}
