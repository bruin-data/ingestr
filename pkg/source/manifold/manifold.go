package manifold

import (
	"context"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	pm "github.com/bruin-data/ingestr/pkg/source/predictionmarkets"
)

const baseURL = "https://api.manifold.markets"

type Source struct {
	api *pm.JSONAPISource
}

func NewSource() *Source {
	return &Source{}
}

func (s *Source) Schemes() []string {
	return []string{"manifold"}
}

func (s *Source) HandlesIncrementality() bool {
	return true
}

func (s *Source) Connect(ctx context.Context, uri string) error {
	params, err := pm.ParseURI(uri, "manifold")
	if err != nil {
		return err
	}
	s.api = &pm.JSONAPISource{
		Scheme: "manifold",
		Params: params,
		Client: pm.NewClient(baseURL, 8, 4),
		Tables: tables(),
	}
	config.Debug("[MANIFOLD] Connected")
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
		"markets": {
			Name: "markets", Path: "/v0/markets", ResultPath: nil,
			QueryParams: []string{"limit", "sort", "order", "userId", "groupId"},
			Columns:     marketColumns(), PrimaryKeys: []string{"id"}, IncrementalKey: "createdTime", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationBefore, LimitParam: "limit", LimitDefault: 500, LimitMax: 1000, BeforeParam: "before", BeforeField: "id",
		},
		"search_markets": {
			Name: "search_markets", Path: "/v0/search-markets", ResultPath: nil,
			QueryParams: []string{"term", "sort", "filter", "creatorId", "contractType", "topicSlug", "limit", "offset", "minLiquidity", "maxLiquidity"},
			Columns:     marketColumns(), PrimaryKeys: []string{"id"}, IncrementalKey: "createdTime", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationOffset, LimitParam: "limit", LimitDefault: 100, LimitMax: 1000, OffsetParam: "offset",
			IntervalEndParam: "beforeTime", IntervalUnixMillis: true,
		},
		"market_by_id": {
			Name: "market_by_id", Path: "/v0/market/{market_id}", ResultPath: nil,
			QueryParams: []string{"market_id"}, RequiredParams: []string{"market_id"},
			Columns: marketColumns(), PrimaryKeys: []string{"id"}, Strategy: config.StrategyReplace,
		},
		"market_by_slug": {
			Name: "market_by_slug", Path: "/v0/slug/{contract_slug}", ResultPath: nil,
			QueryParams: []string{"contract_slug"}, RequiredParams: []string{"contract_slug"},
			Columns: marketColumns(), PrimaryKeys: []string{"id"}, Strategy: config.StrategyReplace,
		},
		"market_probability": {
			Name: "market_probability", Path: "/v0/market/{market_id}/prob", ResultPath: nil,
			QueryParams: []string{"market_id"}, RequiredParams: []string{"market_id"},
			Columns: commonColumns("prob", "answerProbs"),
		},
		"market_probabilities": {
			Name: "market_probabilities", Path: "/v0/market-probs", ResultPath: nil,
			QueryParams: []string{"ids"}, RequiredParams: []string{"ids"},
			Columns: commonColumns("id", "prob", "answerProbs"),
		},
		"market_positions": {
			Name: "market_positions", Path: "/v0/market/{market_id}/positions", ResultPath: nil,
			QueryParams: []string{"market_id", "order", "top", "bottom", "userId", "answerId"}, RequiredParams: []string{"market_id"},
			Columns: commonColumns("userId", "contractId", "answerId", "shares", "profit", "hasShares", "lastBetTime"),
		},
		"bets": {
			Name: "bets", Path: "/v0/bets", ResultPath: nil,
			QueryParams: []string{"userId", "username", "contractId", "contractSlug", "kinds", "order"},
			Columns:     betColumns(), PrimaryKeys: []string{"id"}, IncrementalKey: "createdTime", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationBefore, LimitParam: "limit", LimitDefault: 500, LimitMax: 1000, BeforeParam: "before", BeforeField: "id",
			IntervalStartParam: "afterTime", IntervalEndParam: "beforeTime", IntervalUnixMillis: true,
		},
		"comments": {
			Name: "comments", Path: "/v0/comments", ResultPath: nil,
			QueryParams: []string{"contractId", "contractSlug", "userId", "order"},
			Columns:     commentColumns(), PrimaryKeys: []string{"id"}, IncrementalKey: "createdTime", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationPage, LimitParam: "limit", LimitDefault: 500, LimitMax: 1000, PageParam: "page",
		},
		"groups": {
			Name: "groups", Path: "/v0/groups", ResultPath: nil,
			QueryParams: []string{"availableToUserId"},
			Columns:     commonColumns("id", "slug", "name", "about", "createdTime", "creatorId"),
			PrimaryKeys: []string{"id"}, IncrementalKey: "createdTime", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationTime, TimeParam: "beforeTime", TimeField: "createdTime",
			IntervalEndParam: "beforeTime", IntervalUnixMillis: true,
		},
		"group_by_slug": {
			Name: "group_by_slug", Path: "/v0/group/{group_slug}", ResultPath: nil,
			QueryParams: []string{"group_slug"}, RequiredParams: []string{"group_slug"},
			Columns: commonColumns("id", "slug", "name", "about", "createdTime", "creatorId"),
		},
		"group_by_id": {
			Name: "group_by_id", Path: "/v0/group/by-id/{group_id}", ResultPath: nil,
			QueryParams: []string{"group_id"}, RequiredParams: []string{"group_id"},
			Columns: commonColumns("id", "slug", "name", "about", "createdTime", "creatorId"),
		},
		"users": {
			Name: "users", Path: "/v0/users", ResultPath: nil,
			QueryParams: []string{"limit"},
			Columns:     userColumns(), PrimaryKeys: []string{"id"}, Strategy: config.StrategyMerge,
			Pagination: pm.PaginationBefore, LimitParam: "limit", LimitDefault: 500, LimitMax: 1000, BeforeParam: "before", BeforeField: "id",
		},
		"user_by_username": {
			Name: "user_by_username", Path: "/v0/user/{username}", ResultPath: nil,
			QueryParams: []string{"username"}, RequiredParams: []string{"username"},
			Columns: userColumns(), PrimaryKeys: []string{"id"},
		},
		"user_by_id": {
			Name: "user_by_id", Path: "/v0/user/by-id/{user_id}", ResultPath: nil,
			QueryParams: []string{"user_id"}, RequiredParams: []string{"user_id"},
			Columns: userColumns(), PrimaryKeys: []string{"id"},
		},
		"user_portfolio": {
			Name: "user_portfolio", Path: "/v0/get-user-portfolio", ResultPath: nil,
			QueryParams: []string{"userId"}, RequiredParams: []string{"userId"},
			Columns: portfolioColumns(),
		},
		"user_portfolio_history": {
			Name: "user_portfolio_history", Path: "/v0/get-user-portfolio-history", ResultPath: nil,
			QueryParams: []string{"userId", "period"}, RequiredParams: []string{"userId", "period"},
			Columns:        portfolioColumns(),
			PrimaryKeys:    []string{"timestamp"},
			IncrementalKey: "timestamp", Strategy: config.StrategyMerge,
		},
		"user_contract_metrics": {
			Name: "user_contract_metrics", Path: "/v0/get-user-contract-metrics-with-contracts", ResultPath: nil,
			QueryParams: []string{"userId", "order", "offset", "perAnswer"}, RequiredParams: []string{"userId"},
			Columns:    commonColumns("userId", "contractId", "answerId", "from", "hasShares", "hasNoShares", "totalShares", "profit", "lastBetTime"),
			Pagination: pm.PaginationOffset, LimitParam: "limit", LimitDefault: 100, LimitMax: 1000, OffsetParam: "offset",
		},
		"transactions": {
			Name: "transactions", Path: "/v0/txns", ResultPath: nil,
			QueryParams: []string{"token", "toId", "fromId", "category"},
			Columns:     commonColumns("id", "toId", "fromId", "toType", "fromType", "token", "amount", "category", "createdTime", "description", "data"),
			PrimaryKeys: []string{"id"}, IncrementalKey: "createdTime", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationOffset, LimitParam: "limit", LimitDefault: 100, LimitMax: 100, OffsetParam: "offset",
			IntervalStartParam: "after", IntervalEndParam: "before", IntervalUnixMillis: true,
		},
		"leagues": {
			Name: "leagues", Path: "/v0/leagues", ResultPath: nil,
			QueryParams: []string{"userId", "season", "cohort"},
			Columns:     commonColumns("userId", "season", "cohort", "rank", "manaEarned", "data"),
		},
		"boost_history": {
			Name: "boost_history", Path: "/v0/get-boost-history", ResultPath: nil,
			QueryParams: []string{"contractId", "postId", "userId", "includePending"},
			Columns:     commonColumns("id", "contentType", "contentId", "contractId", "postId", "title", "slug", "url", "userId", "userName", "userUsername", "createdTime", "startTime", "endTime", "funded", "paymentType", "isFree", "manaPurchaseTxnId"),
			PrimaryKeys: []string{"id"}, IncrementalKey: "createdTime", Strategy: config.StrategyMerge,
			Pagination: pm.PaginationOffset, LimitParam: "limit", LimitDefault: 100, LimitMax: 1000, OffsetParam: "offset",
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

func marketColumns() []schema.Column {
	return commonColumns("id", "slug", "question", "creatorId", "creatorName", "creatorUsername", "outcomeType", "mechanism", "createdTime", "closeTime", "resolutionTime", "lastUpdatedTime", "lastBetTime", "lastCommentTime", "isResolved", "isCancelled", "volume", "volume24Hours", "probability", "url", "pool", "answers")
}

func betColumns() []schema.Column {
	return commonColumns("id", "userId", "contractId", "answerId", "outcome", "amount", "shares", "probBefore", "probAfter", "createdTime", "isFilled", "isCancelled", "limitProb", "fees", "fills")
}

func commentColumns() []schema.Column {
	return commonColumns("id", "contractId", "contractSlug", "userId", "userName", "userUsername", "createdTime", "text", "content", "likes")
}

func userColumns() []schema.Column {
	return commonColumns("id", "name", "username", "avatarUrl", "createdTime", "bio", "website", "twitterHandle", "balance", "profitCached")
}

func portfolioColumns() []schema.Column {
	return commonColumns("userId", "timestamp", "investmentValue", "cashInvestmentValue", "balance", "cashBalance", "spiceBalance", "totalDeposits", "totalCashDeposits", "loanTotal", "profit", "dailyProfit")
}

func rawColumn() schema.Column {
	return schema.Column{Name: "raw", DataType: schema.TypeJSON, Nullable: true}
}

func inferType(name string) schema.DataType {
	switch name {
	case "createdTime", "closeTime", "resolutionTime", "lastUpdatedTime", "lastBetTime", "lastCommentTime", "timestamp", "startTime", "endTime":
		return schema.TypeTimestampTZ
	case "isResolved", "isCancelled", "isFilled", "hasShares", "hasNoShares", "funded", "isFree":
		return schema.TypeBoolean
	case "amount", "shares", "probBefore", "probAfter", "limitProb", "volume", "volume24Hours", "probability", "balance", "profitCached", "investmentValue", "cashInvestmentValue", "cashBalance", "spiceBalance", "totalDeposits", "totalCashDeposits", "loanTotal", "profit", "dailyProfit", "rank", "manaEarned", "totalShares":
		return schema.TypeFloat64
	case "pool", "answers", "fees", "fills", "content", "likes", "data", "answerProbs":
		return schema.TypeJSON
	default:
		return schema.TypeString
	}
}
