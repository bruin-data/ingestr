# Allium

[Allium](https://allium.so/) is a blockchain data platform that provides access to indexed blockchain data through a powerful query interface.

ingestr supports Allium as a source.

## URI format

The URI format for Allium is as follows:

```plaintext
allium://?api_key=<api-key>
```

URI parameters:

- `api_key`: The API key used for authentication with the Allium API (required)

The URI is used to connect to the Allium API for extracting blockchain data.

Query parameters should be passed using ingestr's `--interval-start` and `--interval-end` flags.

## Setting up an Allium Integration

To get your Allium API credentials:

1. Sign up for an Allium account at [allium.so](https://allium.so/)
2. Navigate to your account settings
3. Generate an API key
4. Find your query ID from the Allium explorer interface

Once you have your credentials, here's a sample command that will copy the data from Allium into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'allium://?api_key=your_api_key' \
  --source-table 'query:your_query_id' \
  --interval-start '2025-02-01' \
  --interval-end '2025-02-02' \
  --dest-uri duckdb:///allium.duckdb \
  --dest-table 'allium.query_results'
```

The result of this command will be a table in the `allium.duckdb` database.

## Query ID Format

Allium source uses query IDs to identify which query to execute. The query ID should be passed as the `--source-table` parameter with the `query:` prefix.

| Format | Example | Description |
|--------|---------|-------------|
| `query:<query_id>` | `query:abc123def456` | The query ID from Allium explorer |
| `query:<query_id>:<params>` | `query:abc123def456:network=ethereum&min_value=1000` | Query ID with custom parameters |
| `query:<query_id>:<params>` | `query:abc123def456:limit=5000&compute_profile=standard` | Query ID with run_config parameters |

Each query ID represents a specific blockchain data query that you've created in the Allium explorer.

### Run Config Parameters

Special parameters that control query execution (part of `run_config`):

- `limit`: Limit the number of rows in the result (max 250,000)
- `compute_profile`: Compute profile identifier

These parameters are passed in the same format as custom parameters but are used for query execution control.

### Custom Query Parameters

You can pass additional custom parameters to your Allium query using the format:
```
query:<query_id>:param1=value1&param2=value2
```

These custom parameters will be merged with the default date parameters. If a custom parameter has the same name as a default parameter, the custom value will take precedence.

## How it works

The Allium source connector:

1. **Starts an async query execution** using your query ID and parameters
2. **Polls for completion status** every 5 seconds (max 12 hours)
3. **Fetches and returns the results** once the query completes successfully

## Examples

### Basic Query Ingestion (without date filters)

```sh
ingestr ingest \
  --source-uri 'allium://?api_key=your_api_key' \
  --source-table 'query:abc123def456' \
  --dest-uri duckdb:///allium.duckdb \
  --dest-table 'allium.transactions'
```

### Query with Date Parameters

```sh
ingestr ingest \
  --source-uri 'allium://?api_key=your_api_key' \
  --source-table 'query:abc123def456' \
  --interval-start '2025-02-01' \
  --interval-end '2025-02-02' \
  --dest-uri duckdb:///allium.duckdb \
  --dest-table 'allium.daily_transactions'
```

### Query with Custom Parameters

```sh
ingestr ingest \
  --source-uri 'allium://?api_key=your_api_key' \
  --source-table 'query:abc123def456:network=ethereum&min_value=1000' \
  --interval-start '2025-02-01' \
  --interval-end '2025-02-02' \
  --dest-uri duckdb:///allium.duckdb \
  --dest-table 'allium.filtered_transactions'
```

In this example, the query will receive both the default date parameters and the custom parameters `network` and `min_value`.

### Query with Run Config Parameters

```sh
ingestr ingest \
  --source-uri 'allium://?api_key=your_api_key' \
  --source-table 'query:abc123def456:limit=5000&compute_profile=standard' \
  --interval-start '2025-02-01' \
  --interval-end '2025-02-02' \
  --dest-uri duckdb:///allium.duckdb \
  --dest-table 'allium.query_results'
```

This example limits the result to 5000 rows and uses the 'standard' compute profile.

### Query with Both Custom and Run Config Parameters

```sh
ingestr ingest \
  --source-uri 'allium://?api_key=your_api_key' \
  --source-table 'query:abc123def456:network=ethereum&limit=10000&compute_profile=large' \
  --interval-start '2025-02-01' \
  --interval-end '2025-02-02' \
  --dest-uri duckdb:///allium.duckdb \
  --dest-table 'allium.filtered_transactions'
```

This example combines custom query parameters (`network`) with run config parameters (`limit` and `compute_profile`).

## Notes

> [!NOTE]
> - Query execution is asynchronous and may take time depending on the complexity of your query
> - The connector will wait up to 12 hours for query completion
> - Use `--interval-start` and `--interval-end` flags to pass date parameters to your Allium query
> - The dates will be automatically converted to:
>   - `start_date` and `end_date` parameters in the format `YYYY-MM-DD`
>   - `start_timestamp` and `end_timestamp` parameters as Unix timestamps (seconds since epoch)
> - **Default dates**: If not specified, defaults to 2 days ago (00:00) to yesterday (00:00)
> - Custom parameters can be added to the source table format: `query:your_query_id:param1=value1&param2=value2`
> - Run config parameters (`limit`, `compute_profile`) are also passed in the source table format
> - Custom parameters will override default parameters if they have the same name
> - The `limit` parameter has a maximum value of 250,000 rows
> - Make sure your query ID is valid and accessible with your API key
