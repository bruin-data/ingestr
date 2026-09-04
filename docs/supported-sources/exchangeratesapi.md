# Exchangeratesapi.io

[exchangeratesapi.io](https://exchangeratesapi.io/) (an APILayer product) serves current and
historical foreign exchange rates.

> [!WARNING]
> **On the free tier, use [Frankfurter](./frankfurter.md) instead.** exchangeratesapi.io's free
> tier serves the ECB reference rates with base EUR only — the same numbers Frankfurter serves
> with no API key at all. This source is only worth its key on a **paid** plan, which adds a
> switchable base currency, ~172 currencies, and — the reason it exists here — **weekend rows**.
> The ECB does not publish on weekends, so Frankfurter has no Saturday or Sunday rates. If your
> models join FX on an exact date rather than as-of, that gap becomes a zero rather than a
> carried-forward rate.

> [!CAUTION]
> **Historical rates are not reproducible, so do not use this source to rebuild history.** The
> API answers with what it believes today; asking again later returns a different answer for the
> same past date. Measured on 16 years of stored rates from one production pipeline, two
> collectors overlapping on 32,089 `(date, currency)` pairs disagreed on 31,878 of them (99.3%),
> by up to 10.1%. Stored rates are a record of what was quoted at the time — preserve them, and
> use this source to move forward.

## URI format

```plaintext
exchangeratesapi://?access_key=<your-access-key>&base=<currency-code>
```

URI parameters:
- `access_key` (**required**): your exchangeratesapi.io API access key.
- `base` (optional, defaults to `EUR`): the base currency for the returned rates. Changing the
  base requires a paid plan. It deliberately does **not** default to anything else — a silent
  base change produces plausible, wrong money.

## Tables

| Table | Grain | Strategy | Notes |
|---|---|---|---|
| `exchange_rates` | one row per (date, base, currency) | `merge` on `date` | The main table. Honours `--interval-start` / `--interval-end`. |
| `latest` | one row per (base, currency) | `merge` | Most recent published rates. |
| `symbols` | one row per currency | `replace` | Currency codes and names. |

Columns for `exchange_rates` and `latest`: `date`, `base`, `currency`, `exchange_rate`. Each day
includes a base-to-base identity row with `exchange_rate = 1.0`, so converting an amount already
in the base currency finds a row instead of a NULL.

The base currency can also be given in the table name, which takes precedence over the URI:
`--source-table 'exchange_rates:CZK'`.

## Example

```sh
ingestr ingest \
  --source-uri "exchangeratesapi://?access_key=$EXCHANGERATESAPI_KEY&base=CZK" \
  --source-table 'exchange_rates' \
  --dest-uri "duckdb:///fx.duckdb" \
  --dest-table 'raw.rates' \
  --interval-start 2026-08-07 --interval-end 2026-08-10
```

## One request per day

There is no bulk endpoint available on the plans this source was built against. The `/timeseries`
endpoint exists but returns HTTP 403 `function_access_restricted` on anything below the top tiers,
while the single-date endpoint works — so `exchange_rates` issues **one HTTP request per day** in
the requested interval.

A nightly run asks for one or two days, and a week's catch-up costs seven requests. To stop an
accidental `--interval-start 2010-01-01` from firing thousands of requests and burning a monthly
quota in a single run, intervals longer than **400 days** are refused with an explicit error.

Note also that ingestr requires `--interval-start` to be strictly earlier than `--interval-end`;
a single-day run should ask for the day and the day after.

## Errors

Failures come back as proper HTTP status codes with an `{"error": {...}}` body — note there is no
`success: false` field to test, the `success` key is simply absent. The two you are most likely to
meet:

- `invalid_access_key` (HTTP 401) — the key is wrong or expired.
- `function_access_restricted` (HTTP 403) — the endpoint is above your plan. If you see this on
  `exchange_rates`, you are probably on the free tier; use Frankfurter.

The access key is a query parameter on every request, so this source never logs a URL and never
builds an error message out of a raw response body.
