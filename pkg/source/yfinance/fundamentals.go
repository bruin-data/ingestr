package yfinance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

// Yahoo's fundamentals history starts in the mid-1980s; yfinance uses the same floor.
var fundamentalsStart = time.Unix(493590046, 0).UTC()

var statementColumns = []schema.Column{
	{Name: "symbol", DataType: schema.TypeString, Nullable: false},
	{Name: "frequency", DataType: schema.TypeString, Nullable: false},
	{Name: "as_of_date", DataType: schema.TypeDate, Nullable: false},
	{Name: "period_type", DataType: schema.TypeString, Nullable: true},
	{Name: "metric", DataType: schema.TypeString, Nullable: false},
	{Name: "value", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "currency_code", DataType: schema.TypeString, Nullable: true},
}

func statementMetrics(table string) []string {
	switch table {
	case "income_statement":
		return incomeStatementMetrics
	case "balance_sheet":
		return balanceSheetMetrics
	case "cash_flow":
		return cashFlowMetrics
	}
	return nil
}

type timeseriesPoint struct {
	AsOfDate      string `json:"asOfDate"`
	PeriodType    string `json:"periodType"`
	CurrencyCode  string `json:"currencyCode"`
	ReportedValue struct {
		Raw *float64 `json:"raw"`
	} `json:"reportedValue"`
}

func (s *YFinanceSource) readStatement(ctx context.Context, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	metrics := statementMetrics(spec.name)
	types := make([]string, len(metrics))
	for i, m := range metrics {
		types[i] = spec.frequency + m
	}

	start := fundamentalsStart
	if opts.IntervalStart != nil {
		start = opts.IntervalStart.UTC()
	}
	end := time.Now().UTC()
	if opts.IntervalEnd != nil && opts.IntervalEnd.Before(end) {
		end = opts.IntervalEnd.UTC()
	}

	for _, symbol := range spec.symbols {
		if err := ctx.Err(); err != nil {
			return err
		}

		var resp struct {
			Timeseries struct {
				Result []map[string]json.RawMessage `json:"result"`
			} `json:"timeseries"`
		}
		params := url.Values{
			"symbol":  {symbol},
			"type":    {strings.Join(types, ",")},
			"period1": {strconv.FormatInt(start.Unix(), 10)},
			"period2": {strconv.FormatInt(end.Unix(), 10)},
		}
		err := s.get(ctx, "/ws/fundamentals-timeseries/v1/finance/timeseries/"+url.PathEscape(symbol), params, false, spec.name+" for "+symbol, &resp)
		if errors.Is(err, errSymbolNotFound) {
			output.Warnf("Warning: no %s for %s, skipping: %v\n", spec.name, symbol, err)
			continue
		}
		if err != nil {
			return err
		}

		items, err := statementRows(resp.Timeseries.Result, symbol, spec.frequency)
		if err != nil {
			return fmt.Errorf("failed to parse %s for %s: %w", spec.name, symbol, err)
		}
		// period2 is inclusive server-side; keep the window end-exclusive like the other tables.
		items = slices.DeleteFunc(items, func(item map[string]interface{}) bool {
			asOf, err := time.Parse(time.DateOnly, item["as_of_date"].(string))
			return err == nil && !inInterval(asOf, opts)
		})
		if err := sendItems(ctx, results, items, statementColumns, opts, spec.name); err != nil {
			return err
		}
		config.Debug("[YFINANCE] Fetched %d %s values for %s", len(items), spec.name, symbol)
	}
	return nil
}

// statementRows turns each per-metric series (keyed by its type, e.g. "annualTotalRevenue")
// into one long-format row per reporting date.
func statementRows(series []map[string]json.RawMessage, symbol, frequency string) ([]map[string]interface{}, error) {
	var items []map[string]interface{}
	for _, entry := range series {
		var meta struct {
			Type []string `json:"type"`
		}
		if err := json.Unmarshal(entry["meta"], &meta); err != nil {
			return nil, fmt.Errorf("invalid series metadata: %w", err)
		}
		if len(meta.Type) == 0 {
			continue
		}
		seriesType := meta.Type[0]
		raw, ok := entry[seriesType]
		if !ok {
			continue
		}

		var points []*timeseriesPoint
		if err := json.Unmarshal(raw, &points); err != nil {
			return nil, fmt.Errorf("invalid %s series: %w", seriesType, err)
		}
		metric := strings.TrimPrefix(seriesType, frequency)
		for _, p := range points {
			if p == nil || p.AsOfDate == "" {
				continue
			}
			items = append(items, map[string]interface{}{
				"symbol":        symbol,
				"frequency":     frequency,
				"as_of_date":    p.AsOfDate,
				"period_type":   p.PeriodType,
				"metric":        metric,
				"value":         deref(p.ReportedValue.Raw),
				"currency_code": p.CurrencyCode,
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a["as_of_date"] != b["as_of_date"] {
			return a["as_of_date"].(string) < b["as_of_date"].(string)
		}
		return a["metric"].(string) < b["metric"].(string)
	})
	return items, nil
}
