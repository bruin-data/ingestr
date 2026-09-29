# Yahoo Finance

[Yahoo Finance](https://finance.yahoo.com/) provides market data for stocks, ETFs, mutual funds, indices, currencies and crypto. ingestr supports Yahoo Finance as a source for price history, dividends, splits, live quotes, company profiles, option chains, financial statements and news — the same data the [yfinance](https://github.com/ranaroussi/yfinance) Python library exposes.

No API key is required.

::: warning
Yahoo Finance has no official public API. ingestr reads the same endpoints the Yahoo Finance website uses, which Yahoo may change or throttle without notice. The data is intended for personal and research use; review [Yahoo's terms](https://legal.yahoo.com/us/en/yahoo/terms/otos/index.html) before using it commercially.
:::

## URI format

```plaintext
yfinance://
```

## Selecting symbols

Every table needs one or more Yahoo ticker symbols, passed after a colon in the table name:

```plaintext
history:AAPL,MSFT
```

Use the symbols exactly as they appear on Yahoo Finance, e.g. `BTC-USD` for Bitcoin, `^GSPC` for the S&P 500, `EURUSD=X` for EUR/USD, or `SAP.DE` for SAP on XETRA. Symbols that Yahoo doesn't recognize are skipped with a warning, except in `quotes`, `info` and `options`: these tables are fully replaced on every run, so an unknown symbol fails the run instead of silently dropping its previously loaded rows.

Options can be added as URL-style parameters:

```plaintext
history:AAPL,MSFT?interval=1h
```

## Example

Copy daily price history for Apple and Microsoft into DuckDB:

```bash
ingestr ingest \
  --source-uri 'yfinance://' \
  --source-table 'history:AAPL,MSFT' \
  --dest-uri 'duckdb:///yfinance.duckdb' \
  --dest-table 'main.prices'
```

Load only January 2024:

```bash
ingestr ingest \
  --source-uri 'yfinance://' \
  --source-table 'history:AAPL,MSFT' \
  --interval-start '2024-01-01' \
  --interval-end '2024-02-01' \
  --dest-uri 'duckdb:///yfinance.duckdb' \
  --dest-table 'main.prices'
```

## Tables

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `history` | `symbol, interval, date` (intraday: `symbol, interval, timestamp`) | `timestamp` | merge | OHLCV price bars with adjusted close. |
| `dividends` | `symbol, date` | `date` | merge | Dividend payments by ex-date. |
| `splits` | `symbol, date` | `date` | merge | Stock splits with numerator, denominator and ratio. |
| `quotes` | `symbol` | – | replace | Current quote snapshot: price, volume, market cap, 52-week range, valuation ratios and more. |
| `info` | `symbol` | – | replace | Company profile, key statistics, financial data and calendar events; one JSON column per module. |
| `options` | `contractSymbol` | – | replace | Full option chain (calls and puts) across all expirations. |
| `income_statement` | `symbol, frequency, as_of_date, metric` | – | merge | Income statement line items, one row per metric per period. |
| `balance_sheet` | `symbol, frequency, as_of_date, metric` | – | merge | Balance sheet line items, one row per metric per period. |
| `cash_flow` | `symbol, frequency, as_of_date, metric` | – | merge | Cash flow line items, one row per metric per period. |
| `news` | `uuid` | `providerPublishTime` | merge | Recent news articles for each symbol. |

### `history`

| Parameter | Default | Description |
| --------- | ------- | ----------- |
| `interval` | `1d` | Bar size: `1m`, `2m`, `5m`, `15m`, `30m`, `60m`, `90m`, `1h`, `1d`, `1wk`, `1mo`. |
| `prepost` | `false` | Include pre- and post-market bars (intraday intervals only). |

Without `--interval-start`, daily and longer intervals load the symbol's full history. Yahoo only keeps intraday bars for a limited window — 30 days for `1m`, 60 days for `2m`–`90m`, and 730 days for `60m`/`1h` — so intraday loads start at the oldest available bar.

`close` is adjusted for splits; `adj_close` also adjusts for dividends (daily and longer intervals only). `date` is the trading date in the exchange's timezone. Each row carries its `interval`, so daily-or-longer intervals (`1d`, `1wk`, `1mo`) can share one table, and intraday intervals another; keep the two groups in separate tables since their primary keys differ.

A split or dividend makes Yahoo re-adjust all earlier prices. When one falls inside a run's interval, that symbol's full daily, weekly or monthly history is reloaded so the table stays consistent. Intraday bars are not reloaded: after a split, bars loaded earlier stay on the old price scale until you run a full refresh.

### `info`

| Parameter | Default | Description |
| --------- | ------- | ----------- |
| `modules` | `assetProfile,summaryDetail,price,quoteType,defaultKeyStatistics,financialData,calendarEvents` | Comma-separated Yahoo quoteSummary modules. Others include `earnings`, `earningsHistory`, `earningsTrend`, `recommendationTrend`, `upgradeDowngradeHistory`, `institutionOwnership`, `fundOwnership`, `majorHoldersBreakdown`, `insiderHolders`, `insiderTransactions` and `secFilings`. |

```plaintext
info:AAPL?modules=price,recommendationTrend,earningsHistory
```

### Financial statements

`income_statement`, `balance_sheet` and `cash_flow` accept a `frequency` parameter: `annual` (default), `quarterly`, or `trailing` (trailing twelve months; not available for `balance_sheet`).

```plaintext
income_statement:AAPL,MSFT?frequency=quarterly
```

Each row is one line item (`metric`, e.g. `TotalRevenue`, `NetIncome`, `FreeCashFlow`) for one reporting period (`as_of_date`), with its `value` and `currency_code`. Yahoo typically provides around four years of annual and six quarters of quarterly data.

### Notes

- `--interval-start` and `--interval-end` apply to `history`, `dividends`, `splits` and `news`. Financial statements always load the full series Yahoo provides, so newly published and restated periods are picked up on every run.
- For `history`, `--interval-start` includes the whole bar it falls in (e.g. the full week or month). `--interval-end` is matched against each bar's `timestamp` (UTC), so on exchanges that open before midnight UTC (e.g. the ASX) a window can end one local trading date later than the date given.
- `news` returns the most recent articles Yahoo lists for each symbol, so it's best loaded on a schedule to build up an archive.
- Yahoo returns quotes and prices with a delay that depends on the exchange.
