package frankfurter

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/output"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureOutput redirects the output package to a buffer for the duration of fn
// and returns whatever was written, restoring the previous writers afterwards.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	prevOut, prevErr, prevMode := output.Current()
	t.Cleanup(func() { output.Init(prevOut, prevErr, prevMode) })
	var buf bytes.Buffer
	output.Init(&buf, &buf, output.ModeText)
	fn()
	return buf.String()
}

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

func TestFrankfurterSource_DeprecationWarning(t *testing.T) {
	t.Run("warns when base supplied in URI", func(t *testing.T) {
		out := captureOutput(t, func() {
			s := NewFrankfurterSource()
			require.NoError(t, s.Connect(context.Background(), "frankfurter://?base=USD"))
		})
		assert.Contains(t, out, "deprecated")
		assert.Contains(t, out, "source-table")
	})

	t.Run("no warning without base in URI", func(t *testing.T) {
		out := captureOutput(t, func() {
			s := NewFrankfurterSource()
			require.NoError(t, s.Connect(context.Background(), "frankfurter://"))
		})
		assert.Empty(t, out)
	})
}

func TestFrankfurterSource_GetTable_StripsInlineBase(t *testing.T) {
	s := NewFrankfurterSource()
	require.NoError(t, s.Connect(context.Background(), "frankfurter://"))

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "latest:USD"})
	require.NoError(t, err)
	// The inline base must not leak into the table name.
	assert.Equal(t, "latest", table.Name())
}

func TestFrankfurterSource_GetTable_UnsupportedTableWithInlineBase(t *testing.T) {
	s := NewFrankfurterSource()
	require.NoError(t, s.Connect(context.Background(), "frankfurter://"))

	_, err := s.GetTable(context.Background(), source.TableRequest{Name: "invalid:USD"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported table")
}

func TestFrankfurterSource_InlineBaseOverridesURI(t *testing.T) {
	var gotBase string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBase = r.URL.Query().Get("base")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"base":"` + gotBase + `","date":"2024-01-01","rates":{"EUR":0.9}}`))
	}))
	defer server.Close()

	s := NewFrankfurterSource()
	require.NoError(t, s.Connect(context.Background(), "frankfurter://?base=EUR"))
	_ = s.client.Close()
	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(server.URL+"/"),
		ingestrhttp.WithDisableRetry(),
	)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "latest:USD"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	for range results {
	}

	assert.Equal(t, "USD", gotBase)
}

func TestFrankfurterSource_FallsBackToURIBase(t *testing.T) {
	var gotBase string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBase = r.URL.Query().Get("base")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"base":"` + gotBase + `","date":"2024-01-01","rates":{"USD":1.1}}`))
	}))
	defer server.Close()

	s := NewFrankfurterSource()
	require.NoError(t, s.Connect(context.Background(), "frankfurter://?base=GBP"))
	_ = s.client.Close()
	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(server.URL+"/"),
		ingestrhttp.WithDisableRetry(),
	)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "latest"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	for range results {
	}

	assert.Equal(t, "GBP", gotBase)
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
