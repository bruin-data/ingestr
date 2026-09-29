package yfinance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

var quoteColumns = []schema.Column{
	{Name: "symbol", DataType: schema.TypeString, Nullable: false},
	{Name: "regularMarketTime", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var infoColumns = []schema.Column{
	{Name: "symbol", DataType: schema.TypeString, Nullable: false},
}

var optionColumns = []schema.Column{
	{Name: "contractSymbol", DataType: schema.TypeString, Nullable: false},
	{Name: "symbol", DataType: schema.TypeString, Nullable: false},
	{Name: "option_type", DataType: schema.TypeString, Nullable: false},
	{Name: "expiration", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "lastTradeDate", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var newsColumns = []schema.Column{
	{Name: "uuid", DataType: schema.TypeString, Nullable: false},
	{Name: "providerPublishTime", DataType: schema.TypeTimestampTZ, Nullable: true},
}

func (s *YFinanceSource) readQuotes(ctx context.Context, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	for i := 0; i < len(spec.symbols); i += quoteChunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := spec.symbols[i:min(i+quoteChunkSize, len(spec.symbols))]

		var resp struct {
			QuoteResponse struct {
				Result []map[string]interface{} `json:"result"`
			} `json:"quoteResponse"`
		}
		params := url.Values{"symbols": {strings.Join(chunk, ",")}}
		if err := s.get(ctx, "/v7/finance/quote", params, true, "quotes", &resp); err != nil {
			return err
		}

		// quotes is a replace table, so skipping a symbol would drop its previously loaded row.
		if missing := missingSymbols(chunk, resp.QuoteResponse.Result); len(missing) > 0 {
			return fmt.Errorf("yahoo returned no quotes for %s; remove unknown or delisted symbols", strings.Join(missing, ", "))
		}
		if err := sendItems(ctx, results, resp.QuoteResponse.Result, quoteColumns, opts, "quotes"); err != nil {
			return err
		}
		config.Debug("[YFINANCE] Fetched %d quotes", len(resp.QuoteResponse.Result))
	}
	return nil
}

func missingSymbols(requested []string, quotes []map[string]interface{}) []string {
	returned := make(map[string]bool, len(quotes))
	for _, q := range quotes {
		if sym, ok := q["symbol"].(string); ok {
			returned[strings.ToUpper(sym)] = true
		}
	}
	var missing []string
	for _, sym := range requested {
		if !returned[sym] {
			missing = append(missing, sym)
		}
	}
	return missing
}

func (s *YFinanceSource) readInfo(ctx context.Context, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	for _, symbol := range spec.symbols {
		if err := ctx.Err(); err != nil {
			return err
		}

		var resp struct {
			QuoteSummary struct {
				Result []map[string]interface{} `json:"result"`
			} `json:"quoteSummary"`
		}
		params := url.Values{
			"modules":   {strings.Join(spec.modules, ",")},
			"formatted": {"false"},
		}
		if err := s.get(ctx, "/v10/finance/quoteSummary/"+url.PathEscape(symbol), params, true, "info for "+symbol, &resp); err != nil {
			return err
		}
		if len(resp.QuoteSummary.Result) == 0 {
			return fmt.Errorf("info for %s: %w", symbol, errSymbolNotFound)
		}

		item := map[string]interface{}{"symbol": symbol}
		for module, value := range resp.QuoteSummary.Result[0] {
			item[module] = unwrapRaw(value)
		}
		if err := sendItems(ctx, results, []map[string]interface{}{item}, infoColumns, opts, "info"); err != nil {
			return err
		}
		config.Debug("[YFINANCE] Fetched info for %s", symbol)
	}
	return nil
}

// unwrapRaw replaces Yahoo's {"raw": 1.5, "fmt": "1.50"} display wrappers (which
// formatted=false leaves in some modules) with the raw value; empty wrappers become null.
func unwrapRaw(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		if len(t) == 0 {
			return nil
		}
		if raw, ok := t["raw"]; ok && isDisplayWrapper(t) {
			return raw
		}
		for k, inner := range t {
			t[k] = unwrapRaw(inner)
		}
		return t
	case []interface{}:
		for i, inner := range t {
			t[i] = unwrapRaw(inner)
		}
		return t
	default:
		return v
	}
}

func isDisplayWrapper(m map[string]interface{}) bool {
	for k := range m {
		if k != "raw" && k != "fmt" && k != "longFmt" {
			return false
		}
	}
	return true
}

type optionChainResponse struct {
	OptionChain struct {
		Result []struct {
			ExpirationDates []int64 `json:"expirationDates"`
			Options         []struct {
				Calls []map[string]interface{} `json:"calls"`
				Puts  []map[string]interface{} `json:"puts"`
			} `json:"options"`
		} `json:"result"`
	} `json:"optionChain"`
}

func (s *YFinanceSource) fetchOptionChain(ctx context.Context, symbol string, expiration int64) (*optionChainResponse, error) {
	params := url.Values{}
	if expiration > 0 {
		params.Set("date", strconv.FormatInt(expiration, 10))
	}
	var resp optionChainResponse
	if err := s.get(ctx, "/v7/finance/options/"+url.PathEscape(symbol), params, true, "options for "+symbol, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *YFinanceSource) readOptions(ctx context.Context, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	total := 0
	for _, symbol := range spec.symbols {
		if err := ctx.Err(); err != nil {
			return err
		}

		first, err := s.fetchOptionChain(ctx, symbol, 0)
		if err != nil {
			return err
		}
		// Yahoo answers an unknown symbol the same way as one without listed options.
		if len(first.OptionChain.Result) == 0 || len(first.OptionChain.Result[0].ExpirationDates) == 0 {
			output.Warnf("Warning: no options listed for %s\n", symbol)
			continue
		}
		expirations := first.OptionChain.Result[0].ExpirationDates

		// The unparameterized call already returns the nearest expiration's chain.
		for i, exp := range expirations {
			if err := ctx.Err(); err != nil {
				return err
			}
			chain := first
			if i > 0 {
				if chain, err = s.fetchOptionChain(ctx, symbol, exp); err != nil {
					return err
				}
			}

			var items []map[string]interface{}
			for _, r := range chain.OptionChain.Result {
				for _, o := range r.Options {
					items = appendContracts(items, symbol, "call", o.Calls)
					items = appendContracts(items, symbol, "put", o.Puts)
				}
			}
			if err := sendItems(ctx, results, items, optionColumns, opts, "options"); err != nil {
				return err
			}
			total += len(items)
			config.Debug("[YFINANCE] Fetched %d contracts for %s expiring %s", len(items), symbol, time.Unix(exp, 0).UTC().Format("2006-01-02"))
		}
	}
	// options is a replace table; an empty read would wipe the previous snapshot.
	if total == 0 {
		return fmt.Errorf("yahoo returned no option contracts for %s", strings.Join(spec.symbols, ", "))
	}
	return nil
}

func appendContracts(items []map[string]interface{}, symbol, optionType string, contracts []map[string]interface{}) []map[string]interface{} {
	for _, c := range contracts {
		c["symbol"] = symbol
		c["option_type"] = optionType
		items = append(items, c)
	}
	return items
}

func (s *YFinanceSource) readNews(ctx context.Context, spec tableSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	seen := map[string]bool{}
	for _, symbol := range spec.symbols {
		if err := ctx.Err(); err != nil {
			return err
		}

		var resp struct {
			News []map[string]interface{} `json:"news"`
		}
		params := url.Values{
			"q":                {symbol},
			"quotesCount":      {"0"},
			"newsCount":        {strconv.Itoa(newsCount)},
			"enableFuzzyQuery": {"false"},
		}
		if err := s.get(ctx, "/v1/finance/search", params, false, "news for "+symbol, &resp); err != nil {
			return err
		}

		var items []map[string]interface{}
		for _, n := range resp.News {
			uuid, _ := n["uuid"].(string)
			if uuid == "" || seen[uuid] {
				continue
			}
			if published, ok := epochSeconds(n["providerPublishTime"]); ok && !inInterval(published, opts) {
				continue
			}
			seen[uuid] = true
			items = append(items, n)
		}
		if err := sendItems(ctx, results, items, newsColumns, opts, "news"); err != nil {
			return err
		}
		config.Debug("[YFINANCE] Fetched %d news items for %s", len(items), symbol)
	}
	return nil
}

func epochSeconds(v interface{}) (time.Time, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return time.Time{}, false
	}
	i, err := n.Int64()
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(i, 0).UTC(), true
}
