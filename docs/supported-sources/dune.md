# Dune

[Dune](https://dune.com/) is a blockchain analytics platform that provides access to on-chain data through SQL queries and a powerful API.

ingestr supports Dune as a source.

## URI format

The URI format for Dune is as follows:

```plaintext
dune://?api_key=<api-key>
```

URI parameters:

- `api_key`: The API key used for authentication with the Dune API (required)
- `performance`: The engine tier to use for query execution, either `medium` (default) or `large` (optional)

## Setting up a Dune Integration

To get your Dune API key:

1. Log in to your [Dune](https://dune.com/) account
2. From the left sidebar, click **APIs and Connectors**
3. Go to the **API keys** tab
4. Click **New key**
5. Copy the generated API key and store it securely

Once you have your API key, here's a sample command that will copy data from Dune into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'dune://?api_key=your_api_key' \
  --source-table 'queries' \
  --dest-uri duckdb:///dune.duckdb \
  --dest-table 'dune.queries'
```

The result of this command will be a table in the `dune.duckdb` database.

## Tables

The `--source-table` parameter supports three formats:

| Format | Example | Description |
|--------|---------|-------------|
| `queries` | `queries` | Lists all saved queries |
| `query:<id>` | `query:1234567` | Executes a saved query by its numeric ID |
| `query:<id>:<params>` | `query:1234567:bar=1000&foo=value` | Executes a saved query with [query parameters](#query-parameters) |
| `sql:<raw SQL>` | `sql:SELECT * FROM ethereum.transactions LIMIT 100` | Executes raw SQL directly |

## Query Parameters

Dune queries support four types of parameters (these are Dune query parameters, not API parameters):

- `number` — numeric values, e.g. `bar=1000`
- `text` — text/string values, e.g. `foo=value`
- `date` — date values, e.g. `start_date=2024-01-01 00:00:00`
- `enum` — list selection (called "list" in the UI) -  only accepts values from the predefined list

Parameters are passed in the `--source-table` using the format `query:<id>:<key=value&key=value>`. All values are passed as strings — Dune handles the type conversion based on how the parameter is defined in the query.

## How it works

The Dune source connector:

1. **Executes a query** via the Dune API
2. **Polls for completion status** every 5 seconds (max 12 hours) for SQL and saved query executions
3. **Fetches and returns the results** with pagination once the query completes successfully

## Examples

### List Saved Queries

```sh
ingestr ingest \
  --source-uri 'dune://?api_key=your_api_key' \
  --source-table 'queries' \
  --dest-uri duckdb:///dune.duckdb \
  --dest-table 'dune.queries'
```

### Execute a Saved Query

```sh
ingestr ingest \
  --source-uri 'dune://?api_key=your_api_key' \
  --source-table 'query:1234567' \
  --dest-uri duckdb:///dune.duckdb \
  --dest-table 'dune.results'
```

### Execute a Saved Query with Parameters

```sh
ingestr ingest \
  --source-uri 'dune://?api_key=your_api_key' \
  --source-table 'query:1234567:bar=1000&foo=value' \
  --dest-uri duckdb:///dune.duckdb \
  --dest-table 'dune.filtered_results'
```

### Execute Raw SQL

```sh
ingestr ingest \
  --source-uri 'dune://?api_key=your_api_key' \
  --source-table 'sql:SELECT block_number, hash FROM ethereum.transactions LIMIT 100' \
  --dest-uri duckdb:///dune.duckdb \
  --dest-table 'dune.transactions'
```

## Notes
> - Query execution is asynchronous and may take time depending on the complexity of your query
> - The connector will wait up to 12 hours for query completion
> - Use `--interval-start` and `--interval-end` flags to pass date parameters to your Dune query
> - The dates will be automatically converted to:
>   - `start_date` and `end_date` parameters in the format `YYYY-MM-DD`
>   - `start_timestamp` and `end_timestamp` parameters in the format `YYYY-MM-DD HH:MM:SS`
> - **Default dates**: If not specified, defaults to 2 days ago (00:00) to yesterday (00:00)
> - Dune API does not have a native timestamp parameter type (see [query parameters](#query-parameters)). In your query, use `CAST` to convert them, e.g.: `AND block_time >= CAST('{{start_timestamp}}' AS TIMESTAMP)`
> - Custom parameters will override default parameters if they have the same name
> - Make sure your query ID is valid and accessible with your API key

