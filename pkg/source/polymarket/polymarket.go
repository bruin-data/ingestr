package polymarket

import (
	"context"

	"github.com/bruin-data/ingestr/internal/config"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	pm "github.com/bruin-data/ingestr/pkg/source/predictionmarkets"
)

const (
	gammaURL = "https://gamma-api.polymarket.com"
	clobURL  = "https://clob.polymarket.com"
	dataURL  = "https://data-api.polymarket.com"
)

type Source struct {
	api *pm.JSONAPISource
}

func NewSource() *Source {
	return &Source{}
}

func (s *Source) Schemes() []string {
	return []string{"polymarket"}
}

func (s *Source) HandlesIncrementality() bool {
	return true
}

func (s *Source) Connect(ctx context.Context, uri string) error {
	params, err := pm.ParseURI(uri, "polymarket")
	if err != nil {
		return err
	}

	s.api = &pm.JSONAPISource{
		Scheme: "polymarket",
		Params: params,
		Clients: map[string]*ingestrhttp.Client{
			gammaURL: pm.NewClient(gammaURL, 5, 2),
			clobURL:  pm.NewClient(clobURL, 5, 2),
			dataURL:  pm.NewClient(dataURL, 5, 2),
		},
		Tables: tables(),
	}
	config.Debug("[POLYMARKET] Connected")
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
		"events": {
			Name: "events", BaseURL: gammaURL, Path: "/events/keyset", ResultPath: []string{"events"},
			QueryParams: []string{"order", "ascending", "slug", "closed", "live", "active", "archived", "featured", "tag_id", "tag_slug", "series_id", "include_chat", "include_template", "include_markets"},
			Columns:     eventColumns(), PrimaryKeys: []string{"id"}, IncrementalKey: "updatedAt", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationKeyset, LimitParam: "limit", LimitDefault: 100, LimitMax: 500, CursorParam: "after_cursor", CursorPath: []string{"next_cursor"},
			IntervalStartParam: "start_date_min", IntervalEndParam: "end_date_max", IntervalRFC3339: true,
		},
		"markets": {
			Name: "markets", BaseURL: gammaURL, Path: "/markets/keyset", ResultPath: []string{"markets"},
			QueryParams: []string{"order", "ascending", "slug", "closed", "active", "archived", "clob_token_ids", "condition_ids", "question_ids", "tag_id", "related_tags", "include_tag", "rfq_enabled"},
			Columns:     marketColumns(), PrimaryKeys: []string{"id"}, IncrementalKey: "updatedAt", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationKeyset, LimitParam: "limit", LimitDefault: 100, LimitMax: 100, CursorParam: "after_cursor", CursorPath: []string{"next_cursor"},
			IntervalStartParam: "start_date_min", IntervalEndParam: "end_date_max", IntervalRFC3339: true,
		},
		"tags": {
			Name: "tags", BaseURL: gammaURL, Path: "/tags", ResultPath: nil,
			QueryParams: []string{"limit", "offset", "order", "ascending", "include_template"},
			Columns:     tagColumns(), PrimaryKeys: []string{"id"}, IncrementalKey: "updatedAt", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationOffset, LimitParam: "limit", LimitDefault: 100, LimitMax: 100, OffsetParam: "offset",
		},
		"series": {
			Name: "series", BaseURL: gammaURL, Path: "/series", ResultPath: nil,
			QueryParams: []string{"limit", "offset", "order", "ascending", "closed", "active", "archived"},
			Columns:     seriesColumns(), PrimaryKeys: []string{"id"}, IncrementalKey: "updatedAt", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationOffset, LimitParam: "limit", LimitDefault: 100, LimitMax: 100, OffsetParam: "offset",
		},
		"comments": {
			Name: "comments", BaseURL: gammaURL, Path: "/comments", ResultPath: nil,
			QueryParams: []string{"limit", "offset", "market", "user", "parent_entity_id", "parent_entity_type"},
			Columns:     commonColumns("id", "body", "createdAt", "updatedAt", "userAddress", "parentEntityID"), PrimaryKeys: []string{"id"}, IncrementalKey: "createdAt", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationOffset, LimitParam: "limit", LimitDefault: 100, LimitMax: 1000, OffsetParam: "offset",
		},
		"search": {
			Name: "search", BaseURL: gammaURL, Path: "/public-search", ResultPath: nil,
			QueryParams: []string{"q", "limit", "events_status", "markets_status"},
			Columns:     commonColumns("id", "slug", "title", "type"),
			LimitParam:  "limit", LimitDefault: 50, LimitMax: 100,
		},
		"orderbook": {
			Name: "orderbook", BaseURL: clobURL, Path: "/book", ResultPath: nil,
			QueryParams: []string{"token_id"}, RequiredParams: []string{"token_id"},
			Columns: orderbookColumns(), PrimaryKeys: []string{"asset_id"}, Strategy: config.StrategyReplace,
		},
		"price": {
			Name: "price", BaseURL: clobURL, Path: "/price", ResultPath: nil,
			QueryParams: []string{"token_id", "side"}, RequiredParams: []string{"token_id", "side"},
			Columns: commonColumns("price"), Strategy: config.StrategyReplace,
		},
		"midpoint": {
			Name: "midpoint", BaseURL: clobURL, Path: "/midpoint", ResultPath: nil,
			QueryParams: []string{"token_id"}, RequiredParams: []string{"token_id"},
			Columns: commonColumns("mid"), Strategy: config.StrategyReplace,
		},
		"spread": {
			Name: "spread", BaseURL: clobURL, Path: "/spread", ResultPath: nil,
			QueryParams: []string{"token_id"}, RequiredParams: []string{"token_id"},
			Columns: commonColumns("spread"), Strategy: config.StrategyReplace,
		},
		"last_trade_price": {
			Name: "last_trade_price", BaseURL: clobURL, Path: "/last-trade-price", ResultPath: nil,
			QueryParams: []string{"token_id"}, RequiredParams: []string{"token_id"},
			Columns: commonColumns("price", "side"), Strategy: config.StrategyReplace,
		},
		"price_history": {
			Name: "price_history", BaseURL: clobURL, Path: "/prices-history", ResultPath: []string{"history"},
			QueryParams: []string{"market", "interval", "fidelity"}, RequiredParams: []string{"market"},
			Columns:        []schema.Column{{Name: "t", DataType: schema.TypeTimestampTZ, Nullable: true}, {Name: "p", DataType: schema.TypeFloat64, Nullable: true}, rawColumn()},
			PrimaryKeys:    []string{"t"},
			IncrementalKey: "t", Strategy: config.StrategyMerge,
			IntervalStartParam: "startTs", IntervalEndParam: "endTs",
		},
		"trades": {
			Name: "trades", BaseURL: dataURL, Path: "/trades", ResultPath: nil,
			QueryParams: []string{"limit", "offset", "takerOnly", "filterType", "filterAmount", "market", "eventId", "user", "side"},
			Columns:     tradeColumns(), PrimaryKeys: []string{"transactionHash"}, IncrementalKey: "timestamp", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationOffset, LimitParam: "limit", LimitDefault: 100, LimitMax: 10000, OffsetParam: "offset",
		},
		"positions": {
			Name: "positions", BaseURL: dataURL, Path: "/positions", ResultPath: nil,
			QueryParams: []string{"user", "market", "limit", "offset"}, RequiredParams: []string{"user"},
			Columns:    commonColumns("proxyWallet", "asset", "conditionId", "size", "avgPrice", "currentValue", "cashPnl", "percentPnl", "title", "slug", "outcome"),
			Pagination: pm.PaginationOffset, LimitParam: "limit", LimitDefault: 100, LimitMax: 10000, OffsetParam: "offset",
		},
		"closed_positions": {
			Name: "closed_positions", BaseURL: dataURL, Path: "/closed-positions", ResultPath: nil,
			QueryParams: []string{"user", "market", "limit", "offset"}, RequiredParams: []string{"user"},
			Columns:    commonColumns("proxyWallet", "asset", "conditionId", "size", "avgPrice", "realizedPnl", "title", "slug", "outcome"),
			Pagination: pm.PaginationOffset, LimitParam: "limit", LimitDefault: 100, LimitMax: 10000, OffsetParam: "offset",
		},
		"activity": {
			Name: "activity", BaseURL: dataURL, Path: "/activity", ResultPath: nil,
			QueryParams: []string{"user", "limit", "offset", "type"}, RequiredParams: []string{"user"},
			Columns:        commonColumns("proxyWallet", "timestamp", "type", "asset", "conditionId", "size", "price", "title", "slug", "outcome", "transactionHash"),
			PrimaryKeys:    []string{"transactionHash"},
			IncrementalKey: "timestamp", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationOffset, LimitParam: "limit", LimitDefault: 100, LimitMax: 10000, OffsetParam: "offset",
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
	return commonColumns("id", "ticker", "slug", "title", "description", "category", "active", "closed", "archived", "featured", "restricted", "liquidity", "volume", "openInterest", "startDate", "endDate", "createdAt", "updatedAt")
}

func marketColumns() []schema.Column {
	return commonColumns("id", "question", "conditionId", "slug", "category", "description", "outcomes", "outcomePrices", "volume", "liquidity", "active", "closed", "archived", "enableOrderBook", "clobTokenIds", "startDate", "endDate", "createdAt", "updatedAt")
}

func tagColumns() []schema.Column {
	return commonColumns("id", "label", "slug", "forceShow", "forceHide", "publishedAt", "createdAt", "updatedAt")
}

func seriesColumns() []schema.Column {
	return commonColumns("id", "ticker", "slug", "title", "subtitle", "seriesType", "active", "closed", "archived", "createdAt", "updatedAt")
}

func orderbookColumns() []schema.Column {
	return commonColumns("market", "asset_id", "timestamp", "hash", "bids", "asks", "min_order_size", "tick_size", "neg_risk", "last_trade_price")
}

func tradeColumns() []schema.Column {
	return commonColumns("proxyWallet", "asset", "conditionId", "size", "price", "timestamp", "title", "slug", "eventSlug", "outcome", "outcomeIndex", "name", "pseudonym", "side", "transactionHash")
}

func rawColumn() schema.Column {
	return schema.Column{Name: "raw", DataType: schema.TypeJSON, Nullable: true}
}

func inferType(name string) schema.DataType {
	switch name {
	case "active", "closed", "archived", "featured", "restricted", "enableOrderBook", "forceShow", "forceHide", "neg_risk":
		return schema.TypeBoolean
	case "liquidity", "volume", "openInterest", "size", "price", "avgPrice", "currentValue", "cashPnl", "percentPnl", "realizedPnl", "last_trade_price", "tick_size", "min_order_size":
		return schema.TypeFloat64
	case "timestamp", "startDate", "endDate", "createdAt", "updatedAt", "publishedAt":
		return schema.TypeTimestampTZ
	case "outcomeIndex":
		return schema.TypeInt64
	case "outcomes", "outcomePrices", "clobTokenIds", "bids", "asks":
		return schema.TypeJSON
	default:
		return schema.TypeString
	}
}
