# AllRatesToday

[AllRatesToday](https://allratestoday.com/) serves the official exchange rate tables that central
banks and tax authorities publish (for example the ECB reference rates, the RBI reference rate,
the NBP table A middle rates or the Bank of England spot rates), more than 120 institutions in all.

ingestr uses the keyless open endpoints, so no account or API key is needed. Use this source when
you need the rate a specific institution published, for example for statutory accounting or tax
reporting in a currency other than EUR. For plain ECB reference rates with history,
[Frankfurter](./frankfurter.md) is the simpler choice.

> [!NOTE]
> The open endpoints serve only the **latest** table each institution has published, so this
> source cannot backfill history. Schedule it (daily, after the institution's publication time)
> and the `merge` strategy keeps one set of rows per published date. Requests are rate limited
> per IP. The data is free to use, including commercially, with a visible link to
> allratestoday.com wherever the rates are shown; see the
> [open API terms](https://allratestoday.com/open-api/).

## URI format

```plaintext
allratestoday://
```

There are no URI parameters.

## Tables

| Table | PK | Inc Key | Inc Strategy | Details |
|---|---|---|---|---|
| `central_banks` | `code` | – | replace | Institutions with a published table: code, name, latest published date, cadence and a stale flag. |
| `central_bank_rates:<bank>` | `bank`, `date`, `base`, `quote`, `rate_type` | – | merge | The latest published table of one institution, e.g. `central_bank_rates:rbi`. |

Each row of `central_bank_rates` reads as "1 `base` = `rate` `quote`" on `date`, as published.
`rate_type` is what the institution calls the rate (`reference`, `middle`, `buy`, `sell`, ...).
Banks that publish buy and sell rates produce one row per type.

## Example

List the institutions, then load the Reserve Bank of India reference rates into DuckDB:

```bash
ingestr ingest \
  --source-uri 'allratestoday://' \
  --source-table 'central_banks' \
  --dest-uri 'duckdb:///rates.duckdb' \
  --dest-table 'main.central_banks'

ingestr ingest \
  --source-uri 'allratestoday://' \
  --source-table 'central_bank_rates:rbi' \
  --dest-uri 'duckdb:///rates.duckdb' \
  --dest-table 'main.rbi_rates'
```

An unknown bank code, or an institution with no published table yet, fails with the API's error
message (HTTP 404).
