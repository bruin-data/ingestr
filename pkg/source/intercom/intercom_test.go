package intercom

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantToken  string
		wantRegion string
		wantErr    bool
		errSubstr  string
	}{
		{
			name:       "valid with access_token",
			uri:        "intercom://?access_token=tok123",
			wantToken:  "tok123",
			wantRegion: "us",
		},
		{
			name:       "valid with oauth_token",
			uri:        "intercom://?oauth_token=oauthtok456",
			wantToken:  "oauthtok456",
			wantRegion: "us",
		},
		{
			name:       "access_token takes precedence over oauth_token",
			uri:        "intercom://?access_token=tok1&oauth_token=tok2",
			wantToken:  "tok1",
			wantRegion: "us",
		},
		{
			name:       "valid with eu region",
			uri:        "intercom://?access_token=tok456&region=eu",
			wantToken:  "tok456",
			wantRegion: "eu",
		},
		{
			name:       "valid with au region",
			uri:        "intercom://?access_token=tok789&region=au",
			wantToken:  "tok789",
			wantRegion: "au",
		},
		{
			name:      "missing both tokens",
			uri:       "intercom://",
			wantErr:   true,
			errSubstr: "access_token or oauth_token is required",
		},
		{
			name:      "empty access_token no oauth_token",
			uri:       "intercom://?access_token=",
			wantErr:   true,
			errSubstr: "access_token or oauth_token is required",
		},
		{
			name:      "wrong scheme",
			uri:       "http://?access_token=tok123",
			wantErr:   true,
			errSubstr: "must start with intercom://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, region, err := parseURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
			assert.Equal(t, tt.wantRegion, region)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %s to be valid", table)
	}

	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("Contacts"))
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

func TestTransformContact(t *testing.T) {
	item := map[string]interface{}{
		"id":   "123",
		"name": "Test User",
		"location": map[string]interface{}{
			"country": "US",
			"region":  "CA",
			"city":    "San Francisco",
		},
		"companies": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"id": "c1"},
				map[string]interface{}{"id": "c2"},
			},
			"total_count": json.Number("2"),
		},
	}

	transformContact(item)

	assert.Equal(t, "US", item["location_country"])
	assert.Equal(t, "CA", item["location_region"])
	assert.Equal(t, "San Francisco", item["location_city"])
	_, hasLocation := item["location"]
	assert.False(t, hasLocation)

	companyIDs := item["company_ids"].([]interface{})
	assert.Len(t, companyIDs, 2)
	assert.Equal(t, "c1", companyIDs[0])
	assert.Equal(t, "c2", companyIDs[1])
	assert.Equal(t, 2, item["companies_count"])
	_, hasCompanies := item["companies"]
	assert.True(t, hasCompanies)

	assert.NotNil(t, item["custom_attributes"])
}

func TestTransformCompany(t *testing.T) {
	item := map[string]interface{}{
		"id":   "456",
		"name": "Test Company",
		"plan": map[string]interface{}{
			"id":   "plan1",
			"name": "Pro",
		},
	}

	transformCompany(item)

	assert.Equal(t, "plan1", item["plan_id"])
	assert.Equal(t, "Pro", item["plan_name"])
	_, hasPlan := item["plan"]
	assert.False(t, hasPlan)
	assert.NotNil(t, item["custom_attributes"])
}

func TestTransformConversation(t *testing.T) {
	item := map[string]interface{}{
		"id": "789",
		"statistics": map[string]interface{}{
			"first_contact_reply_at":  json.Number("1700000000"),
			"first_admin_reply_at":    json.Number("1700000100"),
			"last_contact_reply_at":   json.Number("1700001000"),
			"last_admin_reply_at":     json.Number("1700001100"),
			"median_admin_reply_time": json.Number("100"),
			"mean_admin_reply_time":   json.Number("150"),
		},
		"conversation_parts": map[string]interface{}{
			"total_count": json.Number("5"),
		},
	}

	transformConversation(item)

	assert.Equal(t, json.Number("1700000000"), item["first_contact_reply_at"])
	assert.Equal(t, json.Number("1700000100"), item["first_admin_reply_at"])
	assert.Equal(t, json.Number("5"), item["conversation_parts_count"])
	_, hasStats := item["statistics"]
	assert.False(t, hasStats)
	_, hasParts := item["conversation_parts"]
	assert.False(t, hasParts)
}

func TestFilterByInterval(t *testing.T) {
	start := time.Unix(1700000000, 0)
	end := time.Unix(1700100000, 0)

	t.Run("no interval passes all", func(t *testing.T) {
		item := map[string]interface{}{"updated_at": json.Number("1700050000")}
		assert.True(t, filterByInterval(item, "updated_at", source.ReadOptions{}))
	})

	t.Run("within interval passes", func(t *testing.T) {
		item := map[string]interface{}{"updated_at": json.Number("1700050000")}
		assert.True(t, filterByInterval(item, "updated_at", source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}))
	})

	t.Run("before interval filtered out", func(t *testing.T) {
		item := map[string]interface{}{"updated_at": json.Number("1699999000")}
		assert.False(t, filterByInterval(item, "updated_at", source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}))
	})

	t.Run("after interval filtered out", func(t *testing.T) {
		item := map[string]interface{}{"updated_at": json.Number("1700200000")}
		assert.False(t, filterByInterval(item, "updated_at", source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}))
	})

	t.Run("missing field passes", func(t *testing.T) {
		item := map[string]interface{}{"name": "test"}
		assert.True(t, filterByInterval(item, "updated_at", source.ReadOptions{IntervalStart: &start}))
	})

	t.Run("equal to start filtered out", func(t *testing.T) {
		item := map[string]interface{}{"updated_at": json.Number("1700000000")}
		assert.False(t, filterByInterval(item, "updated_at", source.ReadOptions{IntervalStart: &start}))
	})
}

func TestExtractNextCursor(t *testing.T) {
	t.Run("with cursor", func(t *testing.T) {
		result := map[string]interface{}{
			"pages": map[string]interface{}{
				"next": map[string]interface{}{
					"starting_after": "abc123",
				},
			},
		}
		assert.Equal(t, "abc123", extractNextCursor(result))
	})

	t.Run("no next page", func(t *testing.T) {
		result := map[string]interface{}{
			"pages": map[string]interface{}{},
		}
		assert.Equal(t, "", extractNextCursor(result))
	})

	t.Run("no pages", func(t *testing.T) {
		result := map[string]interface{}{}
		assert.Equal(t, "", extractNextCursor(result))
	})
}

func TestGetUnixTimestamp(t *testing.T) {
	t.Run("json.Number", func(t *testing.T) {
		item := map[string]interface{}{"ts": json.Number("1700000000")}
		assert.Equal(t, int64(1700000000), getUnixTimestamp(item, "ts"))
	})

	t.Run("float64", func(t *testing.T) {
		item := map[string]interface{}{"ts": float64(1700000000)}
		assert.Equal(t, int64(1700000000), getUnixTimestamp(item, "ts"))
	})

	t.Run("missing field", func(t *testing.T) {
		item := map[string]interface{}{"other": "value"}
		assert.Equal(t, int64(0), getUnixTimestamp(item, "ts"))
	})

	t.Run("string value returns 0", func(t *testing.T) {
		item := map[string]interface{}{"ts": "not a number"}
		assert.Equal(t, int64(0), getUnixTimestamp(item, "ts"))
	})
}
