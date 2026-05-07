package fundraiseup

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI_Valid(t *testing.T) {
	key, err := parseURI("fundraiseup://?api_key=test123")
	require.NoError(t, err)
	assert.Equal(t, "test123", key)
}

func TestParseURI_ValidWithoutQuestionMark(t *testing.T) {
	key, err := parseURI("fundraiseup://api_key=test123")
	require.NoError(t, err)
	assert.Equal(t, "test123", key)
}

func TestParseURI_MissingAPIKey(t *testing.T) {
	_, err := parseURI("fundraiseup://")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key is required")
}

func TestParseURI_EmptyAPIKey(t *testing.T) {
	_, err := parseURI("fundraiseup://?api_key=")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key is required")
}

func TestParseURI_WrongScheme(t *testing.T) {
	_, err := parseURI("postgres://?api_key=test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must start with fundraiseup://")
}

func TestParseURI_ExtraParams(t *testing.T) {
	key, err := parseURI("fundraiseup://?api_key=mykey&extra=value")
	require.NoError(t, err)
	assert.Equal(t, "mykey", key)
}

func TestIsValidTable_AllSupported(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %q to be valid", table)
	}
}

func TestIsValidTable_Invalid(t *testing.T) {
	invalid := []string{"", "unknown", "Donations", "EVENTS", "donation", "recurring-plans", "donations:INCREMENTAL"}
	for _, table := range invalid {
		assert.False(t, isValidTable(table), "expected %q to be invalid", table)
	}
}

func TestJsonUseNumber_LargeInteger(t *testing.T) {
	data := []byte(`{"id": 9007199254740993}`)
	var result map[string]any
	require.NoError(t, jsonUseNumber(data, &result))

	num, ok := result["id"].(json.Number)
	require.True(t, ok, "expected json.Number, got %T", result["id"])
	assert.Equal(t, "9007199254740993", num.String())
}

func TestJsonUseNumber_Float(t *testing.T) {
	data := []byte(`{"amount": 19.99}`)
	var result map[string]any
	require.NoError(t, jsonUseNumber(data, &result))

	num, ok := result["amount"].(json.Number)
	require.True(t, ok)
	f, err := num.Float64()
	require.NoError(t, err)
	assert.InDelta(t, 19.99, f, 0.001)
}

func TestJsonUseNumber_InvalidJSON(t *testing.T) {
	data := []byte(`{invalid}`)
	var result map[string]any
	assert.Error(t, jsonUseNumber(data, &result))
}

func TestToRFC3339_Nil(t *testing.T) {
	assert.Equal(t, "", toRFC3339(nil))
}

func TestToRFC3339_ValidTime(t *testing.T) {
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	assert.Equal(t, "2026-01-15T10:30:00Z", toRFC3339(&ts))
}

func TestToRFC3339_NonUTC(t *testing.T) {
	loc := time.FixedZone("EST", -5*3600)
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, loc)
	result := toRFC3339(&ts)
	assert.Equal(t, "2026-01-15T15:30:00Z", result)
}

func TestConstants(t *testing.T) {
	assert.Equal(t, 2.0, rateLimit)
	assert.Equal(t, 3, rateLimitBurst)
	assert.Equal(t, 100, maxPageSize)
	assert.Equal(t, "https://api.fundraiseup.com/v1", baseURL)
}

func TestTableConfigs_IncrementalTables(t *testing.T) {
	incrementalTables := []string{
		"donations:incremental",
		"events:incremental",
		"fundraisers:incremental",
		"recurring_plans:incremental",
		"supporters:incremental",
	}
	for _, table := range incrementalTables {
		tc, ok := tableConfigs[table]
		require.True(t, ok, "tableConfigs should contain %q", table)
		assert.True(t, tc.hasDateFilter, "%q should have hasDateFilter=true", table)
		assert.Equal(t, "created_at", tc.incrementalKey, "%q should have incrementalKey=created_at", table)
		assert.Equal(t, config.StrategyMerge, tc.strategy, "%q should use merge strategy", table)
	}
}

func TestTableConfigs_ReplaceTables(t *testing.T) {
	replaceTables := []string{
		"donations",
		"events",
		"fundraisers",
		"recurring_plans",
		"supporters",
	}
	for _, table := range replaceTables {
		tc, ok := tableConfigs[table]
		require.True(t, ok, "tableConfigs should contain %q", table)
		assert.False(t, tc.hasDateFilter, "%q should have hasDateFilter=false", table)
		assert.Empty(t, tc.incrementalKey, "%q should have no incrementalKey", table)
		assert.Equal(t, config.StrategyReplace, tc.strategy, "%q should use replace strategy", table)
	}
}

func TestTableConfigs_Endpoints(t *testing.T) {
	expected := map[string]string{
		"donations":                   "/donations",
		"donations:incremental":       "/donations",
		"events":                      "/events",
		"events:incremental":          "/events",
		"fundraisers":                 "/fundraisers",
		"fundraisers:incremental":     "/fundraisers",
		"recurring_plans":             "/recurring_plans",
		"recurring_plans:incremental": "/recurring_plans",
		"supporters":                  "/supporters",
		"supporters:incremental":      "/supporters",
	}
	for table, endpoint := range expected {
		tc, ok := tableConfigs[table]
		require.True(t, ok, "tableConfigs should contain %q", table)
		assert.Equal(t, endpoint, tc.endpoint, "%q should use endpoint %q", table, endpoint)
	}
}

func TestSupportedTablesCount(t *testing.T) {
	assert.Len(t, supportedTables, 10)
	assert.Len(t, tableConfigs, 10)
}

func TestSchemes(t *testing.T) {
	s := NewFundraiseUpSource()
	assert.Equal(t, []string{"fundraiseup"}, s.Schemes())
}

func TestHandlesIncrementality(t *testing.T) {
	s := NewFundraiseUpSource()
	assert.True(t, s.HandlesIncrementality())
}

func TestIncrementalTableResolvesToBaseEndpoint(t *testing.T) {
	cases := []struct {
		table    string
		endpoint string
	}{
		{"donations:incremental", "/donations"},
		{"events:incremental", "/events"},
		{"fundraisers:incremental", "/fundraisers"},
		{"recurring_plans:incremental", "/recurring_plans"},
		{"supporters:incremental", "/supporters"},
	}
	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			cfg, ok := tableConfigs[tc.table]
			require.True(t, ok)
			assert.Equal(t, tc.endpoint, cfg.endpoint,
				"%q should resolve to base endpoint %q, not include ':incremental'", tc.table, tc.endpoint)
			assert.NotContains(t, cfg.endpoint, "incremental",
				"endpoint should not contain 'incremental'")

			// base table should share the same endpoint
			baseName := tc.table[:len(tc.table)-len(":incremental")]
			baseCfg, ok := tableConfigs[baseName]
			require.True(t, ok, "base table %q should exist", baseName)
			assert.Equal(t, cfg.endpoint, baseCfg.endpoint,
				"%q and %q should use the same endpoint", tc.table, baseName)
		})
	}
}
