# Cloudflare D1

[Cloudflare D1](https://developers.cloudflare.com/d1/) is a managed SQL database with SQLite semantics. ingestr supports D1 as a destination through Cloudflare's HTTPS API.

## Setup

Create a D1 database before running ingestr, then obtain:

- Your Cloudflare account ID.
- The D1 database ID (a UUID), available in the database's Cloudflare dashboard or the output of `wrangler d1 list`. Use the ID, not the database name.
- A Cloudflare API token with **Account > D1 > Edit** permission for the account containing the database. The API reference calls this permission **D1 Write**. Follow Cloudflare's [D1 API token instructions](https://developers.cloudflare.com/d1/tutorials/import-to-d1-with-rest-api/#1-create-a-d1-api-token).

ingestr creates destination tables as needed. It does not create the D1 database or require a deployed Worker.

## URI format

```text
d1://<account_id>/<database_id>?api_token=<api_token>
```

`cloudflare-d1://` is an alias for `d1://`. All three parameters are required. URL-encode special characters in the API token when constructing the URI.

## Usage

Copy a PostgreSQL table into D1:

```shell
ingestr ingest \
  --source-uri 'postgresql://user:password@localhost:5432/app' \
  --source-table 'public.customers' \
  --dest-uri 'd1://<account_id>/<database_id>?api_token=<api_token>' \
  --dest-table 'customers' \
  --incremental-strategy 'replace'
```

Use an unqualified destination table name such as `customers`. The SQLite default namespace, `main.customers`, is also accepted. Other schema-qualified names such as `public.customers` are not supported.

For repeated loads, merge rows by primary key:

```shell
ingestr ingest \
  --source-uri 'postgresql://user:password@localhost:5432/app' \
  --source-table 'public.customers' \
  --dest-uri 'd1://<account_id>/<database_id>?api_token=<api_token>' \
  --dest-table 'customers' \
  --incremental-strategy 'merge' \
  --primary-key 'id' \
  --incremental-key 'updated_at'
```

The primary key can be omitted when ingestr detects it from the source.

## Write strategies

| Strategy | Behavior |
| --- | --- |
| `replace` | Load a staging table, then replace the destination table. |
| `append` | Insert new rows into the destination table. |
| `merge` | Update matching primary keys and insert new keys. |
| `delete+insert` | Replace destination rows in the inclusive incremental-key interval, deduplicating incoming rows by primary key when one is configured. |

The final replacement, merge, or delete-and-insert is submitted as a single API batch. Staging tables are created in the same D1 database, so allow storage space for both the existing data and the incoming load.

`scd2`, CDC ingestion, and custom incremental predicates are not supported. Schema evolution can add nullable columns; changes to existing column types fail with an error.

## Data types

| Source type | D1 storage |
| --- | --- |
| Integers | `INTEGER`, preserving the full signed 64-bit range |
| Booleans | `INTEGER` (`0` or `1`) |
| Floating-point numbers | `REAL` |
| Decimals | `TEXT`, preserving decimal precision |
| Binary | `BLOB` |
| Strings, UUIDs | `TEXT` |
| Dates, times, timestamps | `TEXT`; timestamps use UTC ISO 8601 with six fractional digits |
| JSON, arrays, nested values | `TEXT` containing JSON |

Null values remain SQL `NULL`. Unsigned integers above the signed 64-bit maximum, `NaN`, and infinite floating-point values are rejected.

ingestr declares temporal, decimal, JSON, and array columns with text-affinity type names such as `TIMESTAMP_TEXT`, `DECIMAL_TEXT(38,4)`, and `JSON_TEXT`. These declarations preserve their logical types when ingestr inspects the table on later loads; the values are stored as text.

Decimals use text ordering in D1, so they cannot be used as incremental keys for `merge` or `delete+insert`. Choose an integer or temporal incremental column instead.

## Limits

D1 supports at most 100 columns per table and 100 bound parameters per query. ingestr splits inserts into batches that respect the parameter limit and serializes writes. Increasing the number of destination workers does not increase D1 write parallelism. See [Cloudflare's D1 limits](https://developers.cloudflare.com/d1/platform/limits/) for database size, row size, SQL length, and query-duration limits.

The column limit includes ingestr's `_ingestr_loaded_at` and `_ingestr_run_id` metadata columns. Use `--no-load-timestamp` and `--no-run-id` when all 100 columns are needed for source data.

The REST API is also subject to [Cloudflare API rate limits](https://developers.cloudflare.com/fundamentals/api/reference/limits/). Large loads are constrained by these limits and D1's query execution limits.

## Live integration test

The opt-in API contract test requires a disposable, pre-created D1 database and a token with D1 Write permission. Set `INGESTR_D1_TEST_URI` to its destination URI, then run:

```shell
go test -tags integration -count=1 -v -run '^TestD1LiveBatchContract$' ./pkg/destination/d1
```

The test creates and drops a uniquely named table. It verifies multi-statement writes, integer and binary bindings, and rollback when a later statement in an API batch fails. It skips when `INGESTR_D1_TEST_URI` is unset.
