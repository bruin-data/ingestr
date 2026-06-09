# Manifold

[Manifold](https://manifold.markets/) is a prediction market platform. ingestr supports Manifold as a public read-only source for markets, probabilities, bets, comments, groups, users, public portfolio metrics, transactions, leagues, and boost history.

No API key is required for the supported tables.

## URI format

```plaintext
manifold://
```

Optional URI parameters are passed to supported API filters. Examples:

- `manifold://?userId=<user-id>`
- `manifold://?username=<username>`
- `manifold://?market_id=<market-id>`
- `manifold://?contract_slug=<market-slug>`
- `manifold://?groupId=<group-id>`
- `manifold://?term=bitcoin`

## How it works

ingestr reads the public Manifold API at:

```plaintext
https://api.manifold.markets
```

Each `--source-table` maps to one public endpoint. Rows include selected stable columns plus a `raw` JSON column containing the full source payload. Manifold timestamps are Unix milliseconds; ingestr converts timestamp columns to its standard timestamp type.

## Interval behavior

Quick summary: intervals are creation-time filters for the Manifold tables that support them. For `bets` and `transactions`, both start and end are sent to the API. For `search_markets` and `groups`, only `--interval-end` is sent, so the request asks for items created before that time.

Important: using an incremental strategy with intervals for market discovery can miss markets that were created outside the interval but updated inside it. Manifold market responses include update timestamps, and the `markets` endpoint can sort by `updated-time`, but it does not provide updated-time start/end filters. A reliable update-time sync would need to request markets sorted by `updated-time`, page through results, and filter the returned rows client-side.

When the endpoint supports server-side time filtering, ingestr pushes intervals into the API call:

| Table | API interval params |
| --- | --- |
| `bets` | `afterTime`, `beforeTime` as Unix milliseconds. These filter bet creation time. |
| `transactions` | `after`, `before` as Unix milliseconds. These filter transaction creation time. |
| `search_markets` | `beforeTime` from `--interval-end` as Unix milliseconds. This is a market created-time pagination cursor for `sort=newest`; `--interval-start` is not pushed down. |
| `groups` | `beforeTime` from `--interval-end` as Unix milliseconds. This filters group/topic creation time; `--interval-start` is not pushed down. |

Tables not listed above do not have a documented API-side time filter, so intervals are not pushed down for those tables.

## Example

```bash
ingestr ingest \
  --source-uri 'manifold://?term=bitcoin&sort=newest' \
  --source-table search_markets \
  --dest-uri 'duckdb:///manifold.duckdb' \
  --dest-table manifold.search_markets
```

Bets with interval pushdown:

```bash
ingestr ingest \
  --source-uri 'manifold://?contractSlug=<market-slug>' \
  --source-table bets \
  --interval-start '2025-01-01' \
  --interval-end '2030-01-01' \
  --dest-uri 'duckdb:///manifold.duckdb' \
  --dest-table manifold.bets
```

Market probabilities for multiple markets use repeated `ids` URI parameters:

```bash
ingestr ingest \
  --source-uri 'manifold://?ids=<market-id-1>&ids=<market-id-2>' \
  --source-table market_probabilities \
  --dest-uri 'duckdb:///manifold.duckdb' \
  --dest-table manifold.market_probabilities
```

## Tables

| Table | Required URI params | Optional URI params | PK | Inc Key | Details |
| --- | --- | --- | --- | --- | --- |
| `markets` | - | `sort`, `order`, `userId`, `groupId` | `id` | `createdTime` | Public market list. Returns ids, slug, question, creator fields, outcome type, lifecycle timestamps, probability/volume fields, and `raw`. |
| `search_markets` | - | `term`, `sort`, `filter`, `creatorId`, `contractType`, `topicSlug`, `minLiquidity`, `maxLiquidity` | `id` | `createdTime` | Search/filter markets. Supports interval end as `beforeTime`. |
| `market_by_id` | `market_id` | - | `id` | - | Full market by id. |
| `market_by_slug` | `contract_slug` | - | `id` | - | Full market by slug. |
| `market_probability` | `market_id` | - | - | - | Current probability for one market. Multiple choice markets return answer probabilities in `raw`. |
| `market_probabilities` | `ids` | - | - | - | Current probabilities for up to 100 market ids. Repeat `ids` in the URI for multiple markets. |
| `market_positions` | `market_id` | `order`, `top`, `bottom`, `userId`, `answerId` | - | - | Position information for one market. |
| `bets` | - | `userId`, `username`, `contractId`, `contractSlug`, `kinds`, `order` | `id` | `createdTime` | Public bets. Supports interval pushdown. |
| `comments` | - | `contractId`, `contractSlug`, `userId`, `order` | `id` | `createdTime` | Public comments. |
| `groups` | - | `availableToUserId` | `id` | `createdTime` | Public groups/topics. Supports interval end as `beforeTime`. |
| `group_by_slug` | `group_slug` | - | - | - | One group by slug. |
| `group_by_id` | `group_id` | - | - | - | One group by id. |
| `users` | - | - | `id` | - | Public users. |
| `user_by_username` | `username` | - | `id` | - | Public user by username. |
| `user_by_id` | `user_id` | - | `id` | - | Public user by id. |
| `user_portfolio` | `userId` | - | - | - | Current public portfolio metrics for a user. |
| `user_portfolio_history` | `userId`, `period` | - | `timestamp` | `timestamp` | Historical portfolio metrics. `period` is `daily`, `weekly`, `monthly`, or `allTime`. |
| `user_contract_metrics` | `userId` | `order`, `perAnswer` | - | - | User contract metrics with market contracts. |
| `transactions` | - | `token`, `toId`, `fromId`, `category` | `id` | `createdTime` | Public transactions. Supports interval pushdown. |
| `leagues` | - | `userId`, `season`, `cohort` | - | - | Public league standings. |
| `boost_history` | - | `contractId`, `postId`, `userId`, `includePending` | `id` | `createdTime` | Contract and post boost history. |

## Notes

- A practical smoke-test flow is: ingest `markets`, use a returned `id` or `slug` for market detail/probability/bets/comments, ingest `users`, then use a returned `id` or `username` for user and portfolio tables.
- `market_probabilities` expects repeated `ids` parameters for multiple markets. A single comma-separated `ids` value is sent as one string and is rejected by the Manifold API.
- `boost_history` may return no rows for many markets. The Manifold API response is wrapped in a `boosts` array, so empty boost history should be expected for ordinary markets.
- When loading into DuckDB, `--schema-naming direct` is currently the safest option for Manifold tables with camelCase columns such as `createdTime`, `userId`, and `contractId`.
- `raw` is a JSON column containing the full API object.
- Manifold's public API documents a rate limit of 500 requests per minute per IP.
- Authenticated write endpoints for betting, market creation, comments, liquidity, bounty, selling, resolving, and moderation are not supported.
