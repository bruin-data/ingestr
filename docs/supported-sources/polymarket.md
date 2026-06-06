# Polymarket

[Polymarket](https://polymarket.com/) is a prediction market platform. ingestr supports Polymarket as a public read-only source for market discovery, prices, order books, trades, and public wallet activity.

No API key is required for the supported tables.

## URI format

```plaintext
polymarket://
```

Optional URI parameters are passed to supported API filters. Examples:

- `polymarket://?closed=false`
- `polymarket://?tag_id=2`
- `polymarket://?token_id=<clob-token-id>`
- `polymarket://?user=<wallet-address>`
- `polymarket://?market=<condition-id-or-asset-id>`

## How it works

Polymarket has three public API surfaces:

- Gamma API (`https://gamma-api.polymarket.com`) for events, markets, tags, series, comments, and search.
- CLOB API (`https://clob.polymarket.com`) for order books and price data.
- Data API (`https://data-api.polymarket.com`) for trades, positions, and activity.

The connector maps each `--source-table` to one read-only endpoint. Rows include selected stable columns plus a `raw` JSON column with the full source payload.

## Interval behavior

When the endpoint supports server-side time filtering, ingestr maps:

- `--interval-start` to the provider's lower-bound time parameter.
- `--interval-end` to the provider's upper-bound time parameter.

Polymarket interval mappings:

| Table | API interval params |
| --- | --- |
| `events` | `start_date_min`, `end_date_max` as RFC3339 timestamps |
| `markets` | `start_date_min`, `end_date_max` as RFC3339 timestamps |
| `price_history` | `startTs`, `endTs` as Unix seconds |

Tables not listed above do not have a documented API-side time filter, so intervals are not pushed down for those tables.

## Example

```bash
ingestr ingest \
  --source-uri 'polymarket://?closed=false' \
  --source-table markets \
  --dest-uri 'duckdb:///polymarket.duckdb' \
  --dest-table polymarket.markets \
  --interval-start '2025-01-01' \
  --interval-end '2030-01-01'
```

## Tables

| Table | Required URI params | Optional URI params | PK | Inc Key | Details |
| --- | --- | --- | --- | --- | --- |
| `events` | - | `order`, `ascending`, `slug`, `closed`, `live`, `active`, `archived`, `featured`, `tag_id`, `tag_slug`, `series_id`, `include_chat`, `include_template`, `include_markets` | `id` | `updatedAt` | Polymarket events from Gamma keyset pagination. Returns event identifiers, title, category, status flags, volume/liquidity/open interest, dates, and `raw`. |
| `markets` | - | `order`, `ascending`, `slug`, `closed`, `active`, `archived`, `clob_token_ids`, `condition_ids`, `question_ids`, `tag_id`, `related_tags`, `include_tag`, `rfq_enabled` | `id` | `updatedAt` | Polymarket markets from Gamma keyset pagination. Returns market ids, question, condition id, outcomes, prices, CLOB token ids, volume/liquidity, dates, and `raw`. |
| `tags` | - | `limit`, `offset`, `order`, `ascending`, `include_template` | `id` | `updatedAt` | Tags/categories. |
| `series` | - | `limit`, `offset`, `order`, `ascending`, `closed`, `active`, `archived` | `id` | `updatedAt` | Event series metadata. |
| `comments` | - | `market`, `user`, `parent_entity_id`, `parent_entity_type` | `id` | `createdAt` | Public comments. |
| `search` | - | `q`, `events_status`, `markets_status` | - | - | Public search results. |
| `orderbook` | `token_id` | - | `asset_id` | - | CLOB order book for one token. Returns bids, asks, tick size, last trade price, and `raw`. |
| `price` | `token_id`, `side` | - | - | - | Best price for a token side (`BUY` or `SELL`). |
| `midpoint` | `token_id` | - | - | - | Current midpoint price. |
| `spread` | `token_id` | - | - | - | Current bid/ask spread. |
| `last_trade_price` | `token_id` | - | - | - | Last trade price and side. |
| `price_history` | `market` | `interval`, `fidelity` | `t` | `t` | Historical price points for a CLOB asset id. |
| `trades` | - | `takerOnly`, `filterType`, `filterAmount`, `market`, `eventId`, `user`, `side` | `transactionHash` | `timestamp` | Public trade history from Data API. |
| `positions` | `user` | `market` | - | - | Current positions for a public wallet. |
| `closed_positions` | `user` | `market` | - | - | Closed positions for a public wallet. |
| `activity` | `user` | `type` | `transactionHash` | `timestamp` | Public wallet activity. |

## Notes

- CLOB pricing tables use token ids from the `clobTokenIds` field in `markets`.
- `raw` is a JSON column containing the full API object.
- Trading, order placement, bridge, relayer, and wallet mutation endpoints are not supported.
