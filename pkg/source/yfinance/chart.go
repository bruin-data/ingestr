package yfinance

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

// Yahoo only serves intraday bars for a trailing window (e.g. 1m: 30 days) and caps
// the span of a single request, so windows are clamped and chunked per interval.
type intervalLimit struct {
	duration time.Duration
	lookback time.Duration
	chunk    time.Duration
}

const day = 24 * time.Hour

var intervalLimits = map[string]intervalLimit{
	"1m":  {duration: time.Minute, lookback: 29 * day, chunk: 7 * day},
	"2m":  {duration: 2 * time.Minute, lookback: 59 * day},
	"5m":  {duration: 5 * time.Minute, lookback: 59 * day},
	"15m": {duration: 15 * time.Minute, lookback: 59 * day},
	"30m": {duration: 30 * time.Minute, lookback: 59 * day},
	"60m": {duration: time.Hour, lookback: 729 * day},
	"90m": {duration: 90 * time.Minute, lookback: 59 * day},
	"1h":  {duration: time.Hour, lookback: 729 * day},
	"1d":  {duration: day},
	"1wk": {duration: 7 * day},
	"1mo": {},
}

var supportedIntervals = []string{"1m", "2m", "5m", "15m", "30m", "60m", "90m", "1h", "1d", "1wk", "1mo"}

// Earliest period1 Yahoo accepts; used as the start when no interval is given.
var maxHistoryStart = time.Unix(-2208994789, 0).UTC()

func isIntraday(interval string) bool {
	return intervalLimits[interval].lookback > 0
}

var historyColumns = []schema.Column{
	{Name: "symbol", DataType: schema.TypeString, Nullable: false},
	{Name: "timestamp", DataType: schema.TypeTimestampTZ, Nullable: false},
	{Name: "date", DataType: schema.TypeDate, Nullable: false},
	{Name: "open", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "high", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "low", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "close", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "adj_close", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "volume", DataType: schema.TypeInt64, Nullable: true},
	{Name: "currency", DataType: schema.TypeString, Nullable: true},
}

var dividendColumns = []schema.Column{
	{Name: "symbol", DataType: schema.TypeString, Nullable: false},
	{Name: "date", DataType: schema.TypeDate, Nullable: false},
	{Name: "amount", DataType: schema.TypeFloat64, Nullable: false},
	{Name: "currency", DataType: schema.TypeString, Nullable: true},
}

var splitColumns = []schema.Column{
	{Name: "symbol", DataType: schema.TypeString, Nullable: false},
	{Name: "date", DataType: schema.TypeDate, Nullable: false},
	{Name: "numerator", DataType: schema.TypeFloat64, Nullable: false},
	{Name: "denominator", DataType: schema.TypeFloat64, Nullable: false},
	{Name: "split_ratio", DataType: schema.TypeString, Nullable: true},
}

type chartResponse struct {
	Chart struct {
		Result []chartResult `json:"result"`
	} `json:"chart"`
}

type chartResult struct {
	Meta struct {
		Symbol               string `json:"symbol"`
		Currency             string `json:"currency"`
		ExchangeTimezoneName string `json:"exchangeTimezoneName"`
		GMTOffset            int    `json:"gmtoffset"`
	} `json:"meta"`
	Timestamp []int64 `json:"timestamp"`
	Events    struct {
		Dividends map[string]struct {
			Amount float64 `json:"amount"`
			Date   int64   `json:"date"`
		} `json:"dividends"`
		Splits map[string]struct {
			Date        int64   `json:"date"`
			Numerator   float64 `json:"numerator"`
			Denominator float64 `json:"denominator"`
			SplitRatio  string  `json:"splitRatio"`
		} `json:"splits"`
	} `json:"events"`
	Indicators struct {
		Quote []struct {
			Open   []*float64 `json:"open"`
			High   []*float64 `json:"high"`
			Low    []*float64 `json:"low"`
			Close  []*float64 `json:"close"`
			Volume []*float64 `json:"volume"`
		} `json:"quote"`
		AdjClose []struct {
			AdjClose []*float64 `json:"adjclose"`
		} `json:"adjclose"`
	} `json:"indicators"`
}

func (s *YFinanceSource) fetchChart(ctx context.Context, symbol string, start, end time.Time, params url.Values) (*chartResult, error) {
	params.Set("period1", strconv.FormatInt(start.Unix(), 10))
	params.Set("period2", strconv.FormatInt(end.Unix(), 10))

	path := "/v8/finance/chart/" + url.PathEscape(symbol)
	var resp chartResponse
	err := s.get(ctx, path, params, false, "chart for "+symbol, &resp)
	// An unbounded read must never come back silently empty; let Yahoo pick the range instead.
	if errors.Is(err, errNoData) && !start.After(maxHistoryStart) {
		params.Del("period1")
		params.Del("period2")
		params.Set("range", "max")
		err = s.get(ctx, path, params, false, "chart for "+symbol, &resp)
	}
	if err != nil {
		return nil, err
	}
	if len(resp.Chart.Result) == 0 {
		return nil, fmt.Errorf("chart for %s: %w", symbol, errSymbolNotFound)
	}
	return &resp.Chart.Result[0], nil
}

// exchangeLocation resolves the exchange timezone so daily bars get the local trading date.
func exchangeLocation(res *chartResult) *time.Location {
	if res.Meta.ExchangeTimezoneName != "" {
		if loc, err := time.LoadLocation(res.Meta.ExchangeTimezoneName); err == nil {
			return loc
		}
	}
	return time.FixedZone("exchange", res.Meta.GMTOffset)
}

func historyWindow(interval string, opts source.ReadOptions, now time.Time) (time.Time, time.Time) {
	limit := intervalLimits[interval]

	start := maxHistoryStart
	if limit.lookback > 0 {
		start = now.Add(-limit.lookback)
	}
	if opts.IntervalStart != nil {
		if limit.lookback > 0 && opts.IntervalStart.Before(start) {
			output.Warnf("Warning: Yahoo only serves %s bars for the last %d days; starting from %s\n", interval, int(limit.lookback/day)+1, start.Format(time.RFC3339))
		} else {
			start = opts.IntervalStart.UTC()
		}
	}

	end := now
	if opts.IntervalEnd != nil && opts.IntervalEnd.Before(now) {
		end = opts.IntervalEnd.UTC()
	}
	return start, end
}

func (s *YFinanceSource) readHistory(ctx context.Context, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	start, end := historyWindow(spec.interval, opts, time.Now().UTC())
	if !start.Before(end) {
		config.Debug("[YFINANCE] Empty history window %s..%s", start, end)
		return nil
	}

	cols := historyColumns
	if isIntraday(spec.interval) {
		cols = withoutColumn(historyColumns, "adj_close")
	}
	chunk := intervalLimits[spec.interval].chunk

	for _, symbol := range spec.symbols {
		for chunkStart := start; chunkStart.Before(end); {
			if err := ctx.Err(); err != nil {
				return err
			}
			chunkEnd := end
			if chunk > 0 && chunkStart.Add(chunk).Before(end) {
				chunkEnd = chunkStart.Add(chunk)
			}

			config.Debug("[YFINANCE] Fetching %s history for %s from %s to %s", spec.interval, symbol, chunkStart.Format(time.RFC3339), chunkEnd.Format(time.RFC3339))
			params := url.Values{
				"interval":             {spec.interval},
				"includePrePost":       {strconv.FormatBool(spec.prepost)},
				"includeAdjustedClose": {"true"},
			}
			res, err := s.fetchChart(ctx, symbol, chunkStart, chunkEnd, params)
			if errors.Is(err, errNoData) {
				chunkStart = chunkEnd
				continue
			}
			if errors.Is(err, errSymbolNotFound) {
				output.Warnf("Warning: no history for %s, skipping: %v\n", symbol, err)
				break
			}
			if err != nil {
				return err
			}

			items := historyRows(res, symbol, spec.interval, spec.prepost, chunkStart, chunkEnd)
			if err := sendItems(ctx, results, items, cols, opts, "history"); err != nil {
				return err
			}
			config.Debug("[YFINANCE] Fetched %d %s bars for %s", len(items), spec.interval, symbol)
			chunkStart = chunkEnd
		}
	}
	return nil
}

type bar struct {
	ts                            time.Time
	open, high, low, close, adjCl *float64
	volume                        *float64
}

func (b bar) empty() bool {
	return b.open == nil && b.high == nil && b.low == nil && b.close == nil
}

func historyRows(res *chartResult, symbol, interval string, prepost bool, start, end time.Time) []map[string]interface{} {
	var quote struct{ Open, High, Low, Close, Volume []*float64 }
	if len(res.Indicators.Quote) > 0 {
		q := res.Indicators.Quote[0]
		quote.Open, quote.High, quote.Low, quote.Close, quote.Volume = q.Open, q.High, q.Low, q.Close, q.Volume
	}
	var adj []*float64
	if len(res.Indicators.AdjClose) > 0 {
		adj = res.Indicators.AdjClose[0].AdjClose
	}

	bars := make([]bar, 0, len(res.Timestamp))
	for i, ts := range res.Timestamp {
		bars = append(bars, bar{
			ts:     time.Unix(ts, 0).UTC(),
			open:   at(quote.Open, i),
			high:   at(quote.High, i),
			low:    at(quote.Low, i),
			close:  at(quote.Close, i),
			adjCl:  at(adj, i),
			volume: at(quote.Volume, i),
		})
	}

	// The in-progress bar arrives as an all-null placeholder followed by the live row,
	// so merge before dropping null bars.
	loc := exchangeLocation(res)
	bars = mergeLiveBar(bars, interval, prepost, loc)

	intraday := isIntraday(interval)
	items := make([]map[string]interface{}, 0, len(bars))
	for _, b := range bars {
		// Yahoo emits all-null bars for halted sessions and event-only timestamps.
		if b.empty() {
			continue
		}
		if b.ts.Before(start) || !b.ts.Before(end) {
			continue
		}
		item := map[string]interface{}{
			"symbol":    symbol,
			"timestamp": b.ts,
			"date":      b.ts.In(loc).Format("2006-01-02"),
			"open":      deref(b.open),
			"high":      deref(b.high),
			"low":       deref(b.low),
			"close":     deref(b.close),
			"volume":    nil,
			"currency":  res.Meta.Currency,
		}
		if b.volume != nil {
			item["volume"] = int64(*b.volume)
		}
		if !intraday {
			item["adj_close"] = deref(b.adjCl)
		}
		items = append(items, item)
	}
	return items
}

// mergeLiveBar folds the separate "live" row Yahoo appends while a market is open
// into the bar it belongs to, so merge keys don't pick up a stray partial row.
func mergeLiveBar(bars []bar, interval string, prepost bool, loc *time.Location) []bar {
	n := len(bars)
	if n < 2 {
		return bars
	}
	prev, last := bars[n-2], bars[n-1]
	if last.empty() {
		return bars
	}
	if last.ts.Equal(prev.ts) {
		if prev.empty() {
			return append(bars[:n-2], last)
		}
		return bars[:n-1]
	}

	p, l := prev.ts.In(loc), last.ts.In(loc)
	if interval == "1d" {
		if p.Format("2006-01-02") == l.Format("2006-01-02") {
			return append(bars[:n-2], last)
		}
		return bars
	}

	var sameInterval bool
	switch interval {
	case "1wk":
		sameInterval = daysBetween(p, l) < 7
	case "1mo":
		sameInterval = monthsBetween(p, l) == 0
	default:
		sameInterval = l.Sub(p) < intervalLimits[interval].duration
	}
	if !sameInterval {
		return bars
	}
	if prepost && isIntraday(interval) && l.Second() == 0 {
		return bars
	}

	merged := prev
	if merged.open == nil {
		merged.open = last.open
	}
	merged.high = maxPtr(prev.high, last.high)
	merged.low = minPtr(prev.low, last.low)
	merged.close = last.close
	merged.adjCl = last.adjCl
	if last.volume != nil {
		v := *last.volume
		if prev.volume != nil {
			v += *prev.volume
		}
		merged.volume = &v
	}
	return append(bars[:n-2], merged)
}

// daysBetween counts calendar days so a DST shift can't make two weeks look 167h apart.
func daysBetween(a, b time.Time) int {
	civil := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
	return int(civil(b).Sub(civil(a)) / day)
}

func monthsBetween(a, b time.Time) int {
	return (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month())
}

func at(values []*float64, i int) *float64 {
	if i < len(values) {
		return values[i]
	}
	return nil
}

func deref(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func maxPtr(a, b *float64) *float64 {
	if a == nil || (b != nil && *b > *a) {
		return b
	}
	return a
}

func minPtr(a, b *float64) *float64 {
	if a == nil || (b != nil && *b < *a) {
		return b
	}
	return a
}

func withoutColumn(cols []schema.Column, name string) []schema.Column {
	out := make([]schema.Column, 0, len(cols))
	for _, c := range cols {
		if c.Name != name {
			out = append(out, c)
		}
	}
	return out
}

// fetchEvents requests monthly bars so the payload stays small; the events block is
// independent of the bar interval.
func (s *YFinanceSource) fetchEvents(ctx context.Context, symbol, events string, opts source.ReadOptions) (*chartResult, error) {
	start := maxHistoryStart
	if opts.IntervalStart != nil {
		start = opts.IntervalStart.UTC().Add(-45 * day)
	}
	end := time.Now().UTC()
	if opts.IntervalEnd != nil && opts.IntervalEnd.Before(end) {
		end = opts.IntervalEnd.UTC().Add(45 * day)
	}
	if !start.Before(end) {
		return nil, errNoData
	}
	return s.fetchChart(ctx, symbol, start, end, url.Values{"interval": {"1mo"}, "events": {events}})
}

func (s *YFinanceSource) readDividends(ctx context.Context, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	for _, symbol := range spec.symbols {
		if err := ctx.Err(); err != nil {
			return err
		}
		res, err := s.fetchEvents(ctx, symbol, "div", opts)
		if errors.Is(err, errNoData) {
			continue
		}
		if errors.Is(err, errSymbolNotFound) {
			output.Warnf("Warning: no dividends for %s, skipping: %v\n", symbol, err)
			continue
		}
		if err != nil {
			return err
		}

		loc := exchangeLocation(res)
		var items []map[string]interface{}
		for _, d := range res.Events.Dividends {
			ts := time.Unix(d.Date, 0).UTC()
			if !inInterval(ts, opts) {
				continue
			}
			items = append(items, map[string]interface{}{
				"symbol":   symbol,
				"date":     ts.In(loc).Format("2006-01-02"),
				"amount":   d.Amount,
				"currency": res.Meta.Currency,
			})
		}
		sortByDate(items)
		if err := sendItems(ctx, results, items, dividendColumns, opts, "dividends"); err != nil {
			return err
		}
		config.Debug("[YFINANCE] Fetched %d dividends for %s", len(items), symbol)
	}
	return nil
}

func (s *YFinanceSource) readSplits(ctx context.Context, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	for _, symbol := range spec.symbols {
		if err := ctx.Err(); err != nil {
			return err
		}
		res, err := s.fetchEvents(ctx, symbol, "split", opts)
		if errors.Is(err, errNoData) {
			continue
		}
		if errors.Is(err, errSymbolNotFound) {
			output.Warnf("Warning: no splits for %s, skipping: %v\n", symbol, err)
			continue
		}
		if err != nil {
			return err
		}

		loc := exchangeLocation(res)
		var items []map[string]interface{}
		for _, sp := range res.Events.Splits {
			ts := time.Unix(sp.Date, 0).UTC()
			if !inInterval(ts, opts) {
				continue
			}
			items = append(items, map[string]interface{}{
				"symbol":      symbol,
				"date":        ts.In(loc).Format("2006-01-02"),
				"numerator":   sp.Numerator,
				"denominator": sp.Denominator,
				"split_ratio": sp.SplitRatio,
			})
		}
		sortByDate(items)
		if err := sendItems(ctx, results, items, splitColumns, opts, "splits"); err != nil {
			return err
		}
		config.Debug("[YFINANCE] Fetched %d splits for %s", len(items), symbol)
	}
	return nil
}

func sortByDate(items []map[string]interface{}) {
	sort.Slice(items, func(i, j int) bool {
		return items[i]["date"].(string) < items[j]["date"].(string)
	})
}
