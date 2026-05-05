package mailchimp

import (
	"bytes"
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
		wantSvr   string
		wantError string
	}{
		{
			name:    "valid with server param",
			uri:     "mailchimp://?api_key=abc123-us16&server=us16",
			wantKey: "abc123-us16",
			wantSvr: "us16",
		},
		{
			name:    "valid server extracted from key suffix",
			uri:     "mailchimp://?api_key=abc123-us10",
			wantKey: "abc123-us10",
			wantSvr: "us10",
		},
		{
			name:    "server param overrides key suffix",
			uri:     "mailchimp://?api_key=abc123-us16&server=us20",
			wantKey: "abc123-us16",
			wantSvr: "us20",
		},
		{
			name:      "wrong scheme",
			uri:       "postgres://host/db",
			wantError: "invalid mailchimp URI: must start with mailchimp://",
		},
		{
			name:      "missing api_key",
			uri:       "mailchimp://?server=us16",
			wantError: "api_key is required in mailchimp URI",
		},
		{
			name:      "empty uri",
			uri:       "mailchimp://",
			wantError: "api_key is required in mailchimp URI",
		},
		{
			name:      "api_key without server suffix",
			uri:       "mailchimp://?api_key=abc123",
			wantError: "server is required in mailchimp URI",
		},
		{
			name:      "api_key with empty suffix",
			uri:       "mailchimp://?api_key=abc123-",
			wantError: "server is required in mailchimp URI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parseURI(tt.uri)
			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantKey, creds.apiKey)
				assert.Equal(t, tt.wantSvr, creds.server)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	validTables := []string{
		"account",
		"audiences", "automations", "campaigns", "connected_sites",
		"conversations", "ecommerce_stores", "facebook_ads", "landing_pages", "reports",
		"account_exports", "authorized_apps", "batches", "campaign_folders", "chimp_chatter",
		"reports_advice", "reports_domain_performance", "reports_locations",
		"reports_sent_to", "reports_sub_reports", "reports_unsubscribed",
		"lists_activity", "lists_clients", "lists_growth_history",
		"lists_interest_categories", "lists_locations", "lists_merge_fields", "lists_segments",
	}

	for _, table := range validTables {
		assert.True(t, isValidTable(table), "expected %q to be valid", table)
	}

	invalidTables := []string{"", "unknown", "AUDIENCES", "Campaigns", "users", "contacts"}
	for _, table := range invalidTables {
		assert.False(t, isValidTable(table), "expected %q to be invalid", table)
	}
}

func TestDecodeItemsUseNumber(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		key       string
		wantError bool
		check     func(t *testing.T, items []map[string]interface{})
	}{
		{
			name: "large integer preserved as json.Number",
			body: `{"items": [{"id": 9007199254740993, "name": "test"}]}`,
			key:  "items",
			check: func(t *testing.T, items []map[string]interface{}) {
				require.Len(t, items, 1)
				id, ok := items[0]["id"].(json.Number)
				require.True(t, ok, "expected json.Number, got %T", items[0]["id"])
				assert.Equal(t, "9007199254740993", id.String())
			},
		},
		{
			name: "float preserved",
			body: `{"items": [{"value": 3.14}]}`,
			key:  "items",
			check: func(t *testing.T, items []map[string]interface{}) {
				require.Len(t, items, 1)
				val, ok := items[0]["value"].(json.Number)
				require.True(t, ok)
				f, err := val.Float64()
				require.NoError(t, err)
				assert.InDelta(t, 3.14, f, 0.001)
			},
		},
		{
			name:      "invalid JSON returns error",
			body:      `{not valid json}`,
			key:       "items",
			wantError: true,
		},
		{
			name: "missing key returns nil",
			body: `{"other": []}`,
			key:  "items",
			check: func(t *testing.T, items []map[string]interface{}) {
				assert.Nil(t, items)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := decodeItems([]byte(tt.body), tt.key)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				tt.check(t, items)
			}
		})
	}
}

func TestDecodeItemUseNumber(t *testing.T) {
	body := []byte(`{"id": 9007199254740993, "name": "test"}`)
	item, err := decodeItem(body)
	require.NoError(t, err)

	id, ok := item["id"].(json.Number)
	require.True(t, ok)
	assert.Equal(t, "9007199254740993", id.String())
}

func TestFilterItemsByInterval(t *testing.T) {
	items := []map[string]interface{}{
		{"id": "1", "date_created": "2025-01-01T00:00:00Z"},
		{"id": "2", "date_created": "2025-06-15T12:00:00Z"},
		{"id": "3", "date_created": "2026-01-01T00:00:00Z"},
		{"id": "4"},
	}

	t.Run("no filter", func(t *testing.T) {
		result := filterItemsByInterval(items, []string{"date_created"}, nil, nil)
		assert.Len(t, result, 4)
	})

	t.Run("start only", func(t *testing.T) {
		start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
		result := filterItemsByInterval(items, []string{"date_created"}, &start, nil)
		assert.Len(t, result, 3) // items 2, 3, and 4 (no timestamp = included)
	})

	t.Run("end only", func(t *testing.T) {
		end := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
		result := filterItemsByInterval(items, []string{"date_created"}, nil, &end)
		assert.Len(t, result, 3) // items 1, 2, and 4 (no timestamp = included)
	})

	t.Run("start and end", func(t *testing.T) {
		start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
		result := filterItemsByInterval(items, []string{"date_created"}, &start, &end)
		assert.Len(t, result, 2) // items 2 and 4
	})

	t.Run("empty fields", func(t *testing.T) {
		start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
		result := filterItemsByInterval(items, nil, &start, nil)
		assert.Len(t, result, 4) // no filtering if no fields
	})
}

func TestGetNestedField(t *testing.T) {
	item := map[string]interface{}{
		"id": "123",
		"last_message": map[string]interface{}{
			"timestamp": "2025-01-01T00:00:00Z",
		},
	}

	assert.Equal(t, "123", getNestedField(item, "id"))
	assert.Equal(t, "2025-01-01T00:00:00Z", getNestedField(item, "last_message.timestamp"))
	assert.Nil(t, getNestedField(item, "nonexistent.field"))
	assert.Nil(t, getNestedField(item, "id.nested"))
}

func TestFirstTimestamp(t *testing.T) {
	t.Run("string timestamp", func(t *testing.T) {
		item := map[string]interface{}{"created": "2025-06-15T12:00:00Z"}
		ts, ok := firstTimestamp(item, []string{"created"})
		assert.True(t, ok)
		assert.Equal(t, 2025, ts.Year())
	})

	t.Run("empty string", func(t *testing.T) {
		item := map[string]interface{}{"created": ""}
		_, ok := firstTimestamp(item, []string{"created"})
		assert.False(t, ok)
	})

	t.Run("nil value", func(t *testing.T) {
		item := map[string]interface{}{"created": nil}
		_, ok := firstTimestamp(item, []string{"created"})
		assert.False(t, ok)
	})

	t.Run("nested field", func(t *testing.T) {
		item := map[string]interface{}{
			"last_message": map[string]interface{}{
				"timestamp": "2025-01-01T00:00:00Z",
			},
		}
		ts, ok := firstTimestamp(item, []string{"last_message.timestamp"})
		assert.True(t, ok)
		assert.Equal(t, 2025, ts.Year())
	})
}

// Ensure decoder.UseNumber() is used (verify via direct JSON decoding)
func TestJsonUseNumber(t *testing.T) {
	largeID := "9007199254740993"
	raw := []byte(`{"id": ` + largeID + `, "status": "active"}`)

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var result map[string]interface{}
	require.NoError(t, decoder.Decode(&result))

	num, ok := result["id"].(json.Number)
	require.True(t, ok, "expected json.Number, got %T", result["id"])
	assert.Equal(t, largeID, num.String())
}
