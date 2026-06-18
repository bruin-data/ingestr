package kalshi

import (
	"context"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	pm "github.com/bruin-data/ingestr/pkg/source/predictionmarkets"
)

const baseURL = "https://external-api.kalshi.com/trade-api/v2"

type Source struct {
	api *pm.JSONAPISource
}

func NewSource() *Source {
	return &Source{}
}

func (s *Source) Schemes() []string {
	return []string{"kalshi"}
}

func (s *Source) HandlesIncrementality() bool {
	return true
}

func (s *Source) Connect(ctx context.Context, uri string) error {
	params, err := pm.ParseURI(uri, "kalshi")
	if err != nil {
		return err
	}
	s.api = &pm.JSONAPISource{
		Scheme: "kalshi",
		Params: params,
		Client: pm.NewClient(baseURL, 5, 2),
		Tables: tables(),
	}
	config.Debug("[KALSHI] Connected")
	return nil
}

func (s *Source) Close(ctx context.Context) error {
	return s.api.Close(ctx)
}

func (s *Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	return s.api.GetTable(ctx, req)
}

func tables() map[string]pm.TableSpec {
	return map[string]pm.TableSpec{
		"exchange_status": {
			Name: "exchange_status", Path: "/exchange/status", ResultPath: nil,
			Columns: commonColumns("exchange_active", "trading_active", "exchange_estimated_resume_time"),
		},
		"exchange_schedule": {
			Name: "exchange_schedule", Path: "/exchange/schedule", ResultPath: nil,
			Columns: commonColumns("schedule"),
		},
		"exchange_announcements": {
			Name: "exchange_announcements", Path: "/exchange/announcements", ResultPath: []string{"announcements"},
			Columns:     commonColumns("id", "title", "body", "created_time", "updated_time"),
			PrimaryKeys: []string{"id"}, IncrementalKey: "created_time", Strategy: config.StrategyMerge,
		},
		"series": {
			Name: "series", Path: "/series", ResultPath: []string{"series"},
			QueryParams: []string{"limit", "cursor", "category", "tags"},
			Columns:     commonColumns("ticker", "title", "category", "frequency", "created_time", "updated_time"),
			PrimaryKeys: []string{"ticker"}, IncrementalKey: "updated_time", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationCursor, LimitParam: "limit", LimitDefault: 100, LimitMax: 1000, CursorParam: "cursor", CursorPath: []string{"cursor"},
		},
		"series_by_ticker": {
			Name: "series_by_ticker", Path: "/series/{series_ticker}", ResultPath: []string{"series"},
			QueryParams: []string{"series_ticker"}, RequiredParams: []string{"series_ticker"},
			Columns:     commonColumns("ticker", "title", "category", "frequency", "created_time", "updated_time"),
			PrimaryKeys: []string{"ticker"}, Strategy: config.StrategyReplace,
		},
		"events": {
			Name: "events", Path: "/events", ResultPath: []string{"events"},
			QueryParams: []string{"limit", "cursor", "series_ticker", "status", "with_nested_markets"},
			Columns:     eventColumns(), PrimaryKeys: []string{"event_ticker"}, IncrementalKey: "updated_time", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationCursor, LimitParam: "limit", LimitDefault: 100, LimitMax: 1000, CursorParam: "cursor", CursorPath: []string{"cursor"},
		},
		"event_by_ticker": {
			Name: "event_by_ticker", Path: "/events/{event_ticker}", ResultPath: []string{"event"},
			QueryParams: []string{"event_ticker"}, RequiredParams: []string{"event_ticker"},
			Columns: eventColumns(), PrimaryKeys: []string{"event_ticker"}, Strategy: config.StrategyReplace,
		},
		"markets": {
			Name: "markets", Path: "/markets", ResultPath: []string{"markets"},
			QueryParams: []string{"limit", "cursor", "event_ticker", "series_ticker", "status", "tickers", "mve_filter", "min_updated_ts", "max_close_ts", "min_close_ts", "min_settled_ts", "max_settled_ts"},
			Columns:     marketColumns(), PrimaryKeys: []string{"ticker"}, IncrementalKey: "updated_time", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationCursor, LimitParam: "limit", LimitDefault: 100, LimitMax: 1000, CursorParam: "cursor", CursorPath: []string{"cursor"},
			IntervalStartParam: "min_created_ts", IntervalEndParam: "max_created_ts",
		},
		"market_by_ticker": {
			Name: "market_by_ticker", Path: "/markets/{ticker}", ResultPath: []string{"market"},
			QueryParams: []string{"ticker"}, RequiredParams: []string{"ticker"},
			Columns: marketColumns(), PrimaryKeys: []string{"ticker"}, Strategy: config.StrategyReplace,
		},
		"market_orderbook": {
			Name: "market_orderbook", Path: "/markets/{ticker}/orderbook", ResultPath: []string{"orderbook_fp"},
			QueryParams: []string{"ticker"}, RequiredParams: []string{"ticker"},
			Columns: commonColumns("yes_dollars", "no_dollars"),
		},
		"market_orderbooks": {
			Name: "market_orderbooks", Path: "/markets/orderbooks", ResultPath: []string{"orderbooks"},
			QueryParams: []string{"tickers"}, RequiredParams: []string{"tickers"},
			Columns: commonColumns("ticker", "orderbook_fp"),
		},
		"market_trades": {
			Name: "market_trades", Path: "/markets/trades", ResultPath: []string{"trades"},
			QueryParams: []string{"limit", "cursor", "ticker", "is_block_trade"},
			Columns:     tradeColumns(), PrimaryKeys: []string{"trade_id"}, IncrementalKey: "created_time", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationCursor, LimitParam: "limit", LimitDefault: 100, LimitMax: 1000, CursorParam: "cursor", CursorPath: []string{"cursor"},
			IntervalStartParam: "min_ts", IntervalEndParam: "max_ts",
		},
		"market_candlesticks": {
			Name: "market_candlesticks", Path: "/series/{series_ticker}/markets/{ticker}/candlesticks", ResultPath: []string{"candlesticks"},
			QueryParams: []string{"series_ticker", "ticker", "period_interval", "include_latest_before_start"}, RequiredParams: []string{"series_ticker", "ticker", "period_interval"},
			Columns:        candleColumns(),
			PrimaryKeys:    []string{"end_period_ts"},
			IncrementalKey: "end_period_ts", Strategy: config.StrategyMerge,
			IntervalStartParam: "start_ts", IntervalEndParam: "end_ts", RequireInterval: true,
		},
		"market_candlesticks_batch": {
			Name: "market_candlesticks_batch", Path: "/markets/candlesticks", ResultPath: []string{"markets"},
			QueryParams: []string{"market_tickers", "period_interval", "include_latest_before_start"}, RequiredParams: []string{"market_tickers", "period_interval"},
			Columns:            commonColumns("market_ticker", "candlesticks"),
			IntervalStartParam: "start_ts", IntervalEndParam: "end_ts", RequireInterval: true,
		},
		"historical_markets": {
			Name: "historical_markets", Path: "/historical/markets", ResultPath: []string{"markets"},
			QueryParams: []string{"limit", "cursor", "tickers", "event_ticker", "series_ticker", "status"},
			Columns:     marketColumns(), PrimaryKeys: []string{"ticker"}, Strategy: config.StrategyMerge,
			Pagination: pm.PaginationCursor, LimitParam: "limit", LimitDefault: 100, LimitMax: 1000, CursorParam: "cursor", CursorPath: []string{"cursor"},
		},
		"historical_trades": {
			Name: "historical_trades", Path: "/historical/trades", ResultPath: []string{"trades"},
			QueryParams: []string{"limit", "cursor", "ticker", "is_block_trade"},
			Columns:     tradeColumns(), PrimaryKeys: []string{"trade_id"}, IncrementalKey: "created_time", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationCursor, LimitParam: "limit", LimitDefault: 100, LimitMax: 1000, CursorParam: "cursor", CursorPath: []string{"cursor"},
			IntervalStartParam: "min_ts", IntervalEndParam: "max_ts",
		},
	}
}

func commonColumns(names ...string) []schema.Column {
	cols := make([]schema.Column, 0, len(names)+1)
	for _, name := range names {
		cols = append(cols, schema.Column{Name: name, DataType: inferType(name), Nullable: true})
	}
	return append(cols, rawColumn())
}

func eventColumns() []schema.Column {
	return commonColumns("event_ticker", "series_ticker", "sub_title", "title", "category", "strike_date", "strike_period", "created_time", "updated_time", "markets")
}

func marketColumns() []schema.Column {
	return commonColumns("ticker", "event_ticker", "market_type", "title", "subtitle", "yes_sub_title", "no_sub_title", "status", "created_time", "updated_time", "open_time", "close_time", "expiration_time", "settlement_ts", "yes_bid_dollars", "yes_ask_dollars", "no_bid_dollars", "no_ask_dollars", "last_price_dollars", "volume_fp", "volume_24h_fp", "open_interest_fp", "liquidity_dollars")
}

func tradeColumns() []schema.Column {
	return commonColumns("trade_id", "ticker", "count_fp", "yes_price_dollars", "no_price_dollars", "created_time", "is_block_trade")
}

func candleColumns() []schema.Column {
	return commonColumns("end_period_ts", "yes_bid", "yes_ask", "price", "volume_fp", "open_interest_fp")
}

func rawColumn() schema.Column {
	return schema.Column{Name: "raw", DataType: schema.TypeJSON, Nullable: true}
}

func inferType(name string) schema.DataType {
	switch name {
	case "exchange_active", "trading_active", "is_block_trade":
		return schema.TypeBoolean
	case "created_time", "updated_time", "open_time", "close_time", "expiration_time", "settlement_ts", "strike_date", "exchange_estimated_resume_time":
		return schema.TypeTimestampTZ
	case "yes_bid_dollars", "yes_ask_dollars", "no_bid_dollars", "no_ask_dollars", "last_price_dollars", "liquidity_dollars":
		return schema.TypeFloat64
	case "markets", "schedule", "yes_dollars", "no_dollars", "orderbook_fp", "candlesticks", "yes_bid", "yes_ask", "price":
		return schema.TypeJSON
	default:
		return schema.TypeString
	}
}
