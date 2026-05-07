package frankfurter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrankfurterURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantBase  string
		wantError string
	}{
		{"default base", "frankfurter://", "EUR", ""},
		{"custom base", "frankfurter://?base=USD", "USD", ""},
		{"lowercase base", "frankfurter://?base=gbp", "GBP", ""},
		{"empty base falls back to default", "frankfurter://?base=", "EUR", ""},
		{"extra params ignored", "frankfurter://?base=JPY&foo=bar", "JPY", ""},
		{"invalid scheme", "postgres://localhost", "", "must start with frankfurter://"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, err := parseFrankfurterURI(tt.uri)
			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantBase, base)
			}
		})
	}
}

func TestFlattenRates_IncludesBaseCurrency(t *testing.T) {
	s := &FrankfurterSource{}
	rates := map[string]float64{
		"USD": 1.12,
		"GBP": 0.86,
	}

	items := s.flattenRates("2025-01-15", "EUR", rates)

	// Should have 3 rows: base (EUR) + USD + GBP
	assert.Len(t, items, 3)

	// First item should be the base currency with rate 1.0
	assert.Equal(t, "EUR", items[0]["currency_code"])
	assert.Equal(t, "EUR", items[0]["base_currency"])
	assert.Equal(t, 1.0, items[0]["rate"])
	assert.Equal(t, "2025-01-15", items[0]["date"])
}

func TestFlattenRates_SortedByCurrencyCode(t *testing.T) {
	s := &FrankfurterSource{}
	rates := map[string]float64{
		"USD": 1.12,
		"GBP": 0.86,
		"AUD": 1.63,
	}

	items := s.flattenRates("2025-01-15", "EUR", rates)

	// First is base (EUR), then sorted: AUD, GBP, USD
	assert.Equal(t, "EUR", items[0]["currency_code"])
	assert.Equal(t, "AUD", items[1]["currency_code"])
	assert.Equal(t, "GBP", items[2]["currency_code"])
	assert.Equal(t, "USD", items[3]["currency_code"])
}

func TestFlattenRates_EmptyRates(t *testing.T) {
	s := &FrankfurterSource{}
	rates := map[string]float64{}

	items := s.flattenRates("2025-01-15", "EUR", rates)

	// Only the base currency row
	assert.Len(t, items, 1)
	assert.Equal(t, "EUR", items[0]["currency_code"])
	assert.Equal(t, 1.0, items[0]["rate"])
}

func TestFlattenRates_CorrectRateValues(t *testing.T) {
	s := &FrankfurterSource{}
	rates := map[string]float64{
		"USD": 1.0856,
		"JPY": 162.34,
	}

	items := s.flattenRates("2025-01-15", "EUR", rates)

	rateMap := make(map[string]float64)
	for _, item := range items {
		rateMap[item["currency_code"].(string)] = item["rate"].(float64)
	}

	assert.Equal(t, 1.0, rateMap["EUR"])
	assert.Equal(t, 1.0856, rateMap["USD"])
	assert.Equal(t, 162.34, rateMap["JPY"])
}

func TestToDateString(t *testing.T) {
	ts := time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)
	var nilTs *time.Time

	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{"nil", nil, ""},
		{"time.Time", ts, "2025-03-15"},
		{"*time.Time", &ts, "2025-03-15"},
		{"nil *time.Time", nilTs, ""},
		{"RFC3339 string", "2025-03-15T10:30:00Z", "2025-03-15"},
		{"plain date string", "2025-03-15", "2025-03-15"},
		{"unsupported type", 12345, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, toDateString(tt.input))
		})
	}
}
