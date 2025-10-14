# Allium

[Allium](https://allium.so/) is a blockchain data platform that provides access to indexed blockchain data through a powerful query interface.

ingestr supports Allium as a source.

## URI format

The URI format for Allium is as follows:

```plaintext
allium://?api_key=<api-key>&start_date=<start-date>&end_date=<end-date>
```

URI parameters:

- `api_key`: The API key used for authentication with the Allium API (required)
- Additional query parameters can be passed as needed (e.g., `start_date`, `end_date`)

The URI is used to connect to the Allium API for extracting blockchain data.

## Setting up an Allium Integration

To get your Allium API credentials:

1. Sign up for an Allium account at [allium.so](https://allium.so/)
2. Navigate to your account settings
3. Generate an API key
4. Find your query ID from the Allium explorer interface

Once you have your credentials, here's a sample command that will copy the data from Allium into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'allium://?api_key=your_api_key&start_date=2025-02-01&end_date=2025-02-02' \
  --source-table 'your_query_id' \
  --dest-uri duckdb:///allium.duckdb \
  --dest-table 'allium.query_results'
```

The result of this command will be a table in the `allium.duckdb` database.

## Tables

Allium source uses query IDs as table identifiers. The query ID should be passed as the `--source-table` parameter:

```sh
--source-table 'abc123def456'
```

Each query ID represents a specific blockchain data query that you've created in the Allium explorer.

## How it works

The Allium source connector:

1. **Starts an async query execution** using your query ID and parameters
2. **Polls for completion status** every 5 seconds (max 5 minutes)
3. **Fetches and returns the results** once the query completes successfully

## Examples

### Basic Query Ingestion

```sh
ingestr ingest \
  --source-uri 'allium://?api_key=your_api_key' \
  --source-table 'abc123def456' \
  --dest-uri duckdb:///allium.duckdb \
  --dest-table 'allium.transactions'
```

### Query with Date Parameters

```sh
ingestr ingest \
  --source-uri 'allium://?api_key=your_api_key&start_date=2025-02-01&end_date=2025-02-02' \
  --source-table 'abc123def456' \
  --dest-uri duckdb:///allium.duckdb \
  --dest-table 'allium.daily_transactions'
```

## Notes

> [!NOTE]
> - Query execution is asynchronous and may take time depending on the complexity of your query
> - The connector will wait up to 5 minutes for query completion
> - All parameters passed in the URI (except `api_key`) will be sent to the Allium API as query parameters
> - Make sure your query ID is valid and accessible with your API key
