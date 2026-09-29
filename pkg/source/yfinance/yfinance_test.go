package yfinance

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	assert.NoError(t, parseURI("yfinance://"))
	assert.Error(t, parseURI("yahoo://"))
	assert.Error(t, parseURI("yfinance://?symbols=AAPL"))
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), table)
	}
	for _, table := range []string{"", "History", "prices", "capital_gains"} {
		assert.False(t, isValidTable(table), table)
	}
}

func TestParseTableSpec(t *testing.T) {
	tests := []struct {
		raw     string
		want    tableSpec
		wantErr string
	}{
		{raw: "history:aapl, msft,AAPL", want: tableSpec{name: "history", symbols: []string{"AAPL", "MSFT"}, interval: "1d"}},
		{raw: "history?symbols=BTC-USD,^GSPC&interval=1h&prepost=true", want: tableSpec{name: "history", symbols: []string{"BTC-USD", "^GSPC"}, interval: "1h", prepost: true}},
		{raw: "history:AAPL?interval=5m", want: tableSpec{name: "history", symbols: []string{"AAPL"}, interval: "5m"}},
		{raw: "dividends:AAPL", want: tableSpec{name: "dividends", symbols: []string{"AAPL"}}},
		{raw: "income_statement:AAPL", want: tableSpec{name: "income_statement", symbols: []string{"AAPL"}, frequency: "annual"}},
		{raw: "cash_flow:AAPL?frequency=Trailing", want: tableSpec{name: "cash_flow", symbols: []string{"AAPL"}, frequency: "trailing"}},
		{raw: "info:AAPL", want: tableSpec{name: "info", symbols: []string{"AAPL"}, modules: defaultInfoModules}},
		{raw: "info:AAPL?modules=price,esgScores", want: tableSpec{name: "info", symbols: []string{"AAPL"}, modules: []string{"price", "esgScores"}}},
		{raw: "prices:AAPL", wantErr: "unsupported table"},
		{raw: "history", wantErr: "requires at least one symbol"},
		{raw: "history:,", wantErr: "requires at least one symbol"},
		{raw: "history:AAPL?interval=2h", wantErr: "unsupported interval"},
		{raw: "history:AAPL?interval=3mo", wantErr: "unsupported interval"},
		{raw: "history:AAPL?prepost=true", wantErr: "only supported for intraday intervals"},
		{raw: "quotes:AAPL?interval=1d", wantErr: "only supported for the history table"},
		{raw: "history:AAPL?frequency=annual", wantErr: "only supported for the income_statement"},
		{raw: "quotes:AAPL?modules=price", wantErr: "only supported for the info table"},
		{raw: "balance_sheet:AAPL?frequency=trailing", wantErr: "unsupported frequency"},
		{raw: "income_statement:AAPL?frequency=monthly", wantErr: "unsupported frequency"},
		{raw: "history:AAPL?foo=bar", wantErr: "unknown table parameter"},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := parseTableSpec(tt.raw)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSupportedIntervalsMatchLimits(t *testing.T) {
	assert.Len(t, intervalLimits, len(supportedIntervals))
	for _, interval := range supportedIntervals {
		_, ok := intervalLimits[interval]
		assert.True(t, ok, interval)
	}
}

func TestHistoryWindow(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	ts := func(s string) *time.Time {
		v, err := time.Parse(time.RFC3339, s)
		require.NoError(t, err)
		return &v
	}

	start, end := historyWindow("1d", source.ReadOptions{}, now)
	assert.Equal(t, maxHistoryStart, start)
	assert.Equal(t, now, end)

	start, end = historyWindow("1d", source.ReadOptions{IntervalStart: ts("2024-01-01T00:00:00Z"), IntervalEnd: ts("2024-02-01T00:00:00Z")}, now)
	assert.Equal(t, *ts("2024-01-01T00:00:00Z"), start)
	assert.Equal(t, *ts("2024-02-01T00:00:00Z"), end)

	start, _ = historyWindow("1m", source.ReadOptions{}, now)
	assert.Equal(t, now.Add(-29*day), start)

	start, _ = historyWindow("1m", source.ReadOptions{IntervalStart: ts("2020-01-01T00:00:00Z")}, now)
	assert.Equal(t, now.Add(-29*day), start, "intraday start is clamped to Yahoo's lookback")

	_, end = historyWindow("1d", source.ReadOptions{IntervalEnd: ts("2030-01-01T00:00:00Z")}, now)
	assert.Equal(t, now, end, "future end is clamped to now")
}

func f(v float64) *float64 { return &v }

func decodeChart(t *testing.T, body string) *chartResult {
	t.Helper()
	var resp chartResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	require.NotEmpty(t, resp.Chart.Result)
	return &resp.Chart.Result[0]
}

func TestHistoryRows(t *testing.T) {
	res := decodeChart(t, `{"chart":{"result":[{
		"meta":{"symbol":"AAPL","currency":"USD","exchangeTimezoneName":"America/New_York","gmtoffset":-14400},
		"timestamp":[1717421400,1717507800,1717594200],
		"indicators":{
			"quote":[{"open":[115.0,null,118.0],"high":[116.0,null,119.0],"low":[114.0,null,117.0],"close":[115.5,null,118.5],"volume":[1000,null,3000]}],
			"adjclose":[{"adjclose":[115.1,null,118.1]}]
		}}]}}`)

	window := func(s, e int64) (time.Time, time.Time) { return time.Unix(s, 0), time.Unix(e, 0) }
	start, end := window(0, 1800000000)
	items := historyRows(res, "AAPL", "1d", false, start, end)

	require.Len(t, items, 2, "all-null bars are dropped")
	assert.Equal(t, "2024-06-03", items[0]["date"])
	assert.Equal(t, time.Unix(1717421400, 0).UTC(), items[0]["timestamp"])
	assert.Equal(t, 115.5, items[0]["close"])
	assert.Equal(t, 115.1, items[0]["adj_close"])
	assert.Equal(t, int64(1000), items[0]["volume"])
	assert.Equal(t, "USD", items[0]["currency"])

	intraday := historyRows(res, "AAPL", "1h", false, start, end)
	_, hasAdj := intraday[0]["adj_close"]
	assert.False(t, hasAdj, "intraday rows carry no adj_close")

	start, end = window(1717421400, 1717594200)
	items = historyRows(res, "AAPL", "1d", false, start, end)
	require.Len(t, items, 1, "window is start-inclusive, end-exclusive")
	assert.Equal(t, "2024-06-03", items[0]["date"])
}

func TestMergeLiveBar(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04:05", s, ny)
		require.NoError(t, err)
		return v.UTC()
	}

	t.Run("daily keeps the live row for the same day", func(t *testing.T) {
		bars := []bar{
			{ts: at("2026-09-25 09:30:00"), close: f(1)},
			{ts: at("2026-09-28 09:30:00"), close: f(2), volume: f(10)},
			{ts: at("2026-09-28 13:05:12"), close: f(3), volume: f(12)},
		}
		got := mergeLiveBar(bars, "1d", false, ny)
		require.Len(t, got, 2)
		assert.Equal(t, 3.0, *got[1].close)
	})

	t.Run("intraday folds the live row into its bar", func(t *testing.T) {
		bars := []bar{
			{ts: at("2026-09-28 11:30:00"), open: f(10), high: f(12), low: f(9), close: f(11), volume: f(100)},
			{ts: at("2026-09-28 12:07:41"), open: f(11), high: f(13), low: f(10), close: f(12.5), volume: f(5)},
		}
		got := mergeLiveBar(bars, "1h", false, ny)
		require.Len(t, got, 1)
		assert.Equal(t, 10.0, *got[0].open)
		assert.Equal(t, 13.0, *got[0].high)
		assert.Equal(t, 9.0, *got[0].low)
		assert.Equal(t, 12.5, *got[0].close)
		assert.Equal(t, 105.0, *got[0].volume)
		assert.Equal(t, at("2026-09-28 11:30:00"), got[0].ts)
	})

	t.Run("monthly folds a same-month live row", func(t *testing.T) {
		bars := []bar{
			{ts: at("2026-09-01 00:00:00"), high: f(5), low: f(1), close: f(3)},
			{ts: at("2026-09-28 13:00:00"), high: f(6), low: f(2), close: f(4)},
		}
		got := mergeLiveBar(bars, "1mo", false, ny)
		require.Len(t, got, 1)
		assert.Equal(t, 6.0, *got[0].high)
		assert.Equal(t, 1.0, *got[0].low)
		assert.Equal(t, 4.0, *got[0].close)
	})

	t.Run("duplicate final row is dropped", func(t *testing.T) {
		bars := []bar{{ts: at("2026-09-28 11:30:00"), close: f(1)}, {ts: at("2026-09-28 11:30:00"), close: f(1)}}
		assert.Len(t, mergeLiveBar(bars, "1h", false, ny), 1)
	})

	t.Run("live row fills the null placeholder of the current bar", func(t *testing.T) {
		bars := []bar{
			{ts: at("2026-09-28 12:15:00"), open: f(10), high: f(10), low: f(10), close: f(10), volume: f(7)},
			{ts: at("2026-09-28 12:16:00")},
			{ts: at("2026-09-28 12:16:13"), open: f(11), high: f(11), low: f(11), close: f(11), volume: f(0)},
		}
		got := mergeLiveBar(bars, "1m", false, ny)
		require.Len(t, got, 2)
		assert.Equal(t, at("2026-09-28 12:16:00"), got[1].ts)
		assert.Equal(t, 11.0, *got[1].open)
		assert.Equal(t, 11.0, *got[1].close)
		assert.Equal(t, 0.0, *got[1].volume)
	})

	t.Run("trailing null bar never replaces a real bar", func(t *testing.T) {
		bars := []bar{{ts: at("2026-09-28 09:30:00"), close: f(1)}, {ts: at("2026-09-28 09:30:00")}}
		got := mergeLiveBar(bars, "1d", false, ny)
		require.Len(t, got, 2)
		assert.Equal(t, 1.0, *got[0].close)
	})

	t.Run("distinct bars are untouched", func(t *testing.T) {
		bars := []bar{{ts: at("2026-09-28 11:30:00"), close: f(1)}, {ts: at("2026-09-28 12:30:00"), close: f(2)}}
		assert.Len(t, mergeLiveBar(bars, "1h", false, ny), 2)
		assert.Len(t, mergeLiveBar(bars[:1], "1h", false, ny), 1)
	})

	t.Run("prepost minute-aligned row is a new session bar", func(t *testing.T) {
		bars := []bar{{ts: at("2026-09-28 15:30:00"), close: f(1)}, {ts: at("2026-09-28 16:00:00"), close: f(2)}}
		assert.Len(t, mergeLiveBar(bars, "1h", true, ny), 2)
	})
}

func TestExchangeLocationFallsBackToOffset(t *testing.T) {
	res := &chartResult{}
	res.Meta.ExchangeTimezoneName = "Not/AZone"
	res.Meta.GMTOffset = 3600
	_, offset := time.Unix(0, 0).In(exchangeLocation(res)).Zone()
	assert.Equal(t, 3600, offset)
}

func TestUnwrapRaw(t *testing.T) {
	var in map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader([]byte(`{
		"shares":{"raw":2399,"fmt":"2.4k","longFmt":"2,399"},
		"postMarketChange":{},
		"history":[{"price":{"raw":1.5,"fmt":"1.50"},"firm":"X"}],
		"nested":{"raw":1,"other":"kept"}
	}`)))
	dec.UseNumber()
	require.NoError(t, dec.Decode(&in))

	out := unwrapRaw(in).(map[string]interface{})
	assert.Equal(t, json.Number("2399"), out["shares"])
	assert.Nil(t, out["postMarketChange"])
	assert.Equal(t, json.Number("1.5"), out["history"].([]interface{})[0].(map[string]interface{})["price"])
	assert.Equal(t, map[string]interface{}{"raw": json.Number("1"), "other": "kept"}, out["nested"])
}

func TestStatementRows(t *testing.T) {
	var resp struct {
		Timeseries struct {
			Result []map[string]json.RawMessage `json:"result"`
		} `json:"timeseries"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"timeseries":{"result":[
		{"meta":{"symbol":["AAPL"],"type":["annualTotalRevenue"]},"timestamp":[1,2],
		 "annualTotalRevenue":[
			{"asOfDate":"2024-09-30","periodType":"12M","currencyCode":"USD","reportedValue":{"raw":391035000000.0,"fmt":"391.04B"}},
			null,
			{"asOfDate":"2023-09-30","periodType":"12M","currencyCode":"USD","reportedValue":{"raw":383285000000.0}}
		 ]},
		{"meta":{"symbol":["AAPL"],"type":["annualNetIncome"]}}
	]}}`), &resp))

	items, err := statementRows(resp.Timeseries.Result, "AAPL", "annual")
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, map[string]interface{}{
		"symbol":        "AAPL",
		"frequency":     "annual",
		"as_of_date":    "2023-09-30",
		"period_type":   "12M",
		"metric":        "TotalRevenue",
		"value":         383285000000.0,
		"currency_code": "USD",
	}, items[0])
	assert.Equal(t, "2024-09-30", items[1]["as_of_date"])
}

func TestEpochSeconds(t *testing.T) {
	got, ok := epochSeconds(json.Number("1790669350"))
	require.True(t, ok)
	assert.Equal(t, time.Unix(1790669350, 0).UTC(), got)

	_, ok = epochSeconds("1790669350")
	assert.False(t, ok)
}

func TestInInterval(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	opts := source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}

	assert.True(t, inInterval(start, opts))
	assert.False(t, inInterval(end, opts))
	assert.False(t, inInterval(start.Add(-time.Second), opts))
	assert.True(t, inInterval(start.AddDate(-10, 0, 0), source.ReadOptions{}))
}

func TestMissingSymbols(t *testing.T) {
	quotes := []map[string]interface{}{{"symbol": "AAPL"}, {"symbol": "btc-usd"}}
	assert.Equal(t, []string{"NOPE"}, missingSymbols([]string{"AAPL", "BTC-USD", "NOPE"}, quotes))
	assert.Empty(t, missingSymbols([]string{"AAPL"}, quotes))
}
