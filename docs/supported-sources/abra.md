# ABRA Flexi

[ABRA Flexi](https://www.flexibee.eu/) (formerly Flexibee) is a Czech cloud accounting and ERP system. ingestr reads it through the REST API at `https://<account>.flexibee.eu/c/<company>/<evidence>.json`.

Unlike most SaaS connectors, this one is **schema-driven rather than table-driven**. Flexi publishes a machine-readable schema for every register at `/<evidence>/properties.json`, so the connector derives each table's columns and types from the API at read time. There is no fixed table list: any evidence in the company is a valid `--source-table`.

## URI format

```plaintext
abra://<account>.flexibee.eu?username=<user>&password=<password>&company=<company-code>
```

Parameters:

- `username`: Required. The Flexi API user.
- `password`: Required. That user's password. URL-encode it if it contains reserved characters.
- `company`: Required. The company database code as it appears in the REST path, e.g. `acme_s_r_o_`. **Never defaulted** — one credential typically reaches every company in the account, and pointing the wrong one at a destination loads the wrong set of books with no error.
- `page_size`: Optional. Rows per request. Defaults to `1000`.
- `rate_limit`: Optional. Requests per second. Defaults to `4`.
- `include_expensive`: Optional. Whether to include properties Flexi flags as expensive to compute. Defaults to `true`.

`flexibee://` is accepted as an alias for `abra://`.

The company codes available to a credential can be listed with `GET /c.json`.

## Tables

The table name is the **evidence path** — the same identifier that appears in the REST URL. Common ones:

| Table | Description |
| --- | --- |
| `faktura-vydana` / `faktura-vydana-polozka` | Issued invoices and their lines |
| `faktura-prijata` / `faktura-prijata-polozka` | Received invoices and their lines |
| `banka` / `banka-polozka` | Bank documents and lines |
| `pokladni-pohyb` | Cash movements |
| `interni-doklad` / `interni-doklad-polozka` | Internal documents and lines |
| `adresar` | Address book (counterparties) |
| `pohledavka` / `zavazek` | Receivables and liabilities |
| `ucetni-osnova` | Chart of accounts |
| `stredisko` | Cost centres |
| `kurz`, `sazba-dph`, `stat` | Exchange rates, VAT rates, countries |

`GET /c/<company>/evidence-list.json` lists every evidence in a company.

## Incremental behavior

Tables use `merge` on the primary key `id`, with `lastUpdate` as the incremental key. When an interval is supplied, the connector pushes it to Flexi as a server-side filter:

```plaintext
lastUpdate gte '2026-01-15T10:30:00.000Z'
```

Only the start bound is applied. Re-running a wider window costs requests, never correctness, because `merge` deduplicates on `id`.

Evidences that have an `id` but no usable `lastUpdate` are re-read in full on every run and still merge correctly.

## Notes and limitations

- **Not every evidence is a table.** Some are derived views — the accounting journal `ucetni-denik`, the account-movement view `pohyb-na-uctech`, the VAT ledger `podklady-dph` and around a dozen report endpoints return rows whose `id` is `-1`. They cannot be deduplicated or windowed, so the connector **refuses them at plan time** with an explanatory error rather than loading rows that would be appended in full on every run.
- **Some report endpoints require parameters** and return HTTP 400 when read as a plain table (`stav-skladu-k-datu`, `kontrolni-hlaseni-dph`, `souhrnne-hlaseni-dph`, and similar). These are not supported.
- **Numeric values use field-specific precision and scale.** When Flexi publishes `digits` and `decimal` metadata, the connector emits a decimal column with that shape. Numeric fields without valid metadata use float64.
- **Calendar dates keep their calendar day.** Flexi serialises dates with a UTC offset (`2026-01-01+01:00`); the offset is discarded so an invoice issued on the 1st does not move to the previous month. Datetimes are true instants and are normalised to UTC.
- **Relation and select fields expand into three columns** — `mena`, `mena@ref`, `mena@showAs`. Since `@` is not portable as a column name, characters outside `[A-Za-z0-9_]` are replaced with `_`, giving `mena`, `mena_ref`, `mena_showAs`. Casing is otherwise preserved.
- **Paging is ordered by `id`**, not by `lastUpdate`, so that rows edited during a long read cannot shift across page boundaries and be skipped or repeated.
