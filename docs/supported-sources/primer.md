# Primer

[Primer](https://primer.io/) is a unified payment infrastructure that enables businesses to connect, manage, and optimize their entire payment stack through a single integration.

ingestr supports Primer as a source.

## URI format

The URI format for Primer is as follows:

```plaintext
primer://?api_key=<your-api-key>
```

URI parameters:

- `api_key`: The API key used for authentication with the Primer API

The URI is used to connect to the Primer API for extracting payment data. More details on Primer's API can be found [here](https://primer.io/docs).

## Setting up a Primer Integration

Primer requires an API key to access the API. You can obtain your API key from the Primer Dashboard.

Once you have your API key, here's a sample command that will copy the data from Primer into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "primer://?api_key=your_api_key_here" \
  --source-table "payments" \
  --dest-uri "duckdb:///primer.duckdb" \
  --dest-table "dest.payments"
```

The result of this command will be a table in the `primer.duckdb` database.

## Incremental Loading

Primer source supports incremental loading using date ranges. You can specify `--interval-start` and `--interval-end` parameters to filter payments by date:

```sh
ingestr ingest \
  --source-uri "primer://?api_key=your_api_key_here" \
  --source-table "payments" \
  --dest-uri "duckdb:///primer.duckdb" \
  --dest-table "dest.payments" \
  --interval-start "2024-01-01" \
  --interval-end "2024-01-31"
```

## Tables

Primer source allows ingesting the following sources into separate tables:

| Table | Details |
|-------|---------|
| `payments` | Contains detailed payment information including payment IDs, statuses, amounts, and transaction metadata |

Use these as `--source-table` parameter in the `ingestr ingest` command.
