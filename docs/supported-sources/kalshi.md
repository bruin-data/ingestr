# Kalshi

[Kalshi](https://kalshi.com/) is a regulated prediction market exchange. ingestr supports Kalshi as a public read-only source for exchange status, series, events, markets, order books, trades, candlesticks, and historical market data.

No API key is required for the supported tables.

## URI format

```plaintext
kalshi://
```

Optional URI parameters are passed to supported API filters. Examples:

- `kalshi://?status=open`
- `kalshi://?series_ticker=KXHIGHNY`
- `kalshi://?event_ticker=<event-ticker>`
- `kalshi://?ticker=<market-ticker>`
- `kalshi://?tickers=<ticker-1>,<ticker-2>`

## How it works

ingestr reads Kalshi's public Trade API at:

```plaintext
https://external-api.kalshi.com/trade-api/v2
```

Each `--source-table` maps to one public endpoint. Rows include selected stable columns plus a `raw` JSON column containing the full source payload. Numeric fixed-point fields from Kalshi are preserved as strings unless the API returns a numeric JSON value.

## Interval behavior

When an endpoint supports server-side time filtering, ingestr pushes intervals into the API call:

| Table | API interval params |
| --- | --- |
| `markets` | `min_created_ts`, `max_created_ts` as Unix seconds |
| `market_trades` | `min_ts`, `max_ts` as Unix seconds |
| `historical_trades` | `min_ts`, `max_ts` as Unix seconds |
| `market_candlesticks` | required `start_ts`, `end_ts` as Unix seconds |
| `market_candlesticks_batch` | required `start_ts`, `end_ts` as Unix seconds |

`market_candlesticks` and `market_candlesticks_batch` require both `--interval-start` and `--interval-end` because Kalshi requires those parameters.

## Example

```bash
ingestr ingest \
  --source-uri 'kalshi://?status=open' \
  --source-table markets \
  --dest-uri 'duckdb:///kalshi.duckdb' \
  --dest-table kalshi.markets
```

Use a `ticker` and `event_ticker` returned by `markets` for single-market tables:

```bash
ingestr ingest \
  --source-uri 'kalshi://?ticker=<market-ticker>' \
  --source-table market_orderbook \
  --dest-uri 'duckdb:///kalshi.duckdb' \
  --dest-table kalshi.market_orderbook
```

Candlestick example:

```bash
ingestr ingest \
  --source-uri 'kalshi://?series_ticker=<series-ticker>&ticker=<market-ticker>&period_interval=60' \
  --source-table market_candlesticks \
  --interval-start '2026-01-01' \
  --interval-end '2026-01-02' \
  --dest-uri 'duckdb:///kalshi.duckdb' \
  --dest-table kalshi.market_candlesticks
```

## Tables

| Table | Required URI params | Optional URI params | PK | Inc Key | Details |
| --- | --- | --- | --- | --- | --- |
| `exchange_status` | - | - | - | - | Exchange active/trading active flags and estimated resume time. |
| `exchange_schedule` | - | - | - | - | Exchange schedule payload in `raw`. |
| `exchange_announcements` | - | - | `id` | `created_time` | Public exchange announcements. |
| `series` | - | `category`, `tags` | `ticker` | `updated_time` | Series metadata. |
| `series_by_ticker` | `series_ticker` | - | `ticker` | - | One series by ticker. |
| `events` | - | `series_ticker`, `status`, `with_nested_markets` | `event_ticker` | `updated_time` | Events and optional nested markets. |
| `event_by_ticker` | `event_ticker` | - | `event_ticker` | - | One event by ticker. |
| `markets` | - | `event_ticker`, `series_ticker`, `status`, `tickers`, `mve_filter`, `min_updated_ts`, `max_close_ts`, `min_close_ts`, `min_settled_ts`, `max_settled_ts` | `ticker` | `updated_time` | Market discovery with prices, volume, open interest, status, and `raw`. |
| `market_by_ticker` | `ticker` | - | `ticker` | - | One market by ticker. |
| `market_orderbook` | `ticker` | - | - | - | Current YES/NO bid ladders for one market. |
| `market_orderbooks` | `tickers` | - | - | - | Batch order books for comma-separated tickers. |
| `market_trades` | - | `ticker`, `is_block_trade` | `trade_id` | `created_time` | Public trades. Supports interval pushdown. |
| `market_candlesticks` | `series_ticker`, `ticker`, `period_interval` | `include_latest_before_start` | `end_period_ts` | `end_period_ts` | Candlesticks for one market. Requires intervals. |
| `market_candlesticks_batch` | `market_tickers`, `period_interval` | `include_latest_before_start` | - | - | Batch candlesticks for up to 100 market tickers. Requires intervals. |
| `historical_markets` | - | `tickers`, `event_ticker`, `series_ticker`, `status` | `ticker` | - | Archived historical markets. |
| `historical_trades` | - | `ticker`, `is_block_trade` | `trade_id` | `created_time` | Historical trades. Supports interval pushdown. |

## Notes

- Use `status=open` to find currently populated live markets. Some series return zero events or markets, so a reliable smoke-test flow is: ingest `markets` with `status=open`, take a returned `ticker` and `event_ticker`, derive the series ticker from the event ticker prefix, then call `market_by_ticker`, `event_by_ticker`, order book, trade, and candlestick tables.
- `market_candlesticks` and `market_candlesticks_batch` require both `--interval-start` and `--interval-end`. Use a short window around the selected market's active period to avoid empty results.
- Batch order books use the `tickers` URI parameter, while batch candlesticks use `market_tickers`.
- Kalshi order books return YES and NO bids; asks are implied by binary market mechanics.
- When loading into DuckDB, `--schema-naming direct` is currently the safest option for these tables because many provider field names are mixed case or already provider-specific.
- Authenticated trading, portfolio, order, account, and RFQ endpoints are not supported.
