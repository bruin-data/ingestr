# Cursor

[Cursor](https://cursor.com/) is an AI-powered code editor built for productivity. The Cursor API provides access to team usage data, spending information, and detailed usage events.

ingestr supports Cursor as a source.

## URI format

The URI format for Cursor is as follows:

```plaintext
cursor://?api_key=<api-key-here>
```

URI parameters:

- `api_key`: API key for authentication (get from your Cursor team settings)

The URI is used to connect to the Cursor API for extracting data.

## Setting up a Cursor Integration

To set up a Cursor integration, you need to:

1. Log in to your Cursor account
2. Navigate to your team settings
3. Generate an API key for API access

Once you have your API key, here's a sample command that will copy the data from Cursor into a DuckDB database:

```sh
ingestr ingest \
    --source-uri 'cursor://?api_key=your_api_key' \
    --source-table 'team_members' \
    --dest-uri duckdb:///cursor.duckdb \
    --dest-table 'team_members.data'
```

The result of this command will be a table in the `cursor.duckdb` database.

## Tables

Cursor source allows ingesting the following sources into separate tables:

| Table | Description | Requires Interval | Details |
| ----- | ----------- | ----------------- | ------- |
| `team_members` | Team member information | No | Contains member names, emails, and roles. Uses GET endpoint. |
| `daily_usage_data` | Daily usage statistics | Optional | Contains daily metrics like lines added/deleted, AI requests, model usage. Date range cannot exceed 30 days when specified. |
| `team_spend` | Team spending data | No | Contains spending information for the current billing cycle including per-member costs and request counts. |
| `filtered_usage_events` | Detailed usage events | Optional | Contains granular usage event data including timestamps, models, token usage, and costs. Most detailed data source. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

## Examples

### Basic Usage - Team Members

```sh
ingestr ingest \
    --source-uri 'cursor://?api_key=your_api_key' \
    --source-table 'team_members' \
    --dest-uri duckdb:///cursor.duckdb \
    --dest-table 'team_members'
```

### Daily Usage Data with Date Range

```sh
ingestr ingest \
    --source-uri 'cursor://?api_key=your_api_key' \
    --source-table 'daily_usage_data' \
    --dest-uri duckdb:///cursor.duckdb \
    --dest-table 'daily_usage' \
    --interval-start '2024-10-01' \
    --interval-end '2024-10-24'
```

**Note:** Date range cannot exceed 30 days. If you need more than 30 days of data, make multiple requests with different date ranges.

### Daily Usage Data without Date Range

```sh
ingestr ingest \
    --source-uri 'cursor://?api_key=your_api_key' \
    --source-table 'daily_usage_data' \
    --dest-uri duckdb:///cursor.duckdb \
    --dest-table 'daily_usage'
```

When no date range is provided, the API returns the last 30 days of data by default.

### Team Spending Data

```sh
ingestr ingest \
    --source-uri 'cursor://?api_key=your_api_key' \
    --source-table 'team_spend' \
    --dest-uri duckdb:///cursor.duckdb \
    --dest-table 'team_spend'
```

Returns spending data for the current billing cycle.

### Filtered Usage Events

```sh
ingestr ingest \
    --source-uri 'cursor://?api_key=your_api_key' \
    --source-table 'filtered_usage_events' \
    --dest-uri duckdb:///cursor.duckdb \
    --dest-table 'usage_events' \
    --interval-start '2024-10-01' \
    --interval-end '2024-10-24'
```

Most detailed data source with per-request information including token usage and costs.

## Features

### Automatic Pagination

All Cursor endpoints support automatic pagination (100 records per page by default). The source handles pagination automatically, so you don't need to worry about fetching all pages.

### Optional Date Filtering

`daily_usage_data` and `filtered_usage_events` tables support optional date filtering:
- When dates are provided, only data within that range is fetched
- When dates are omitted, the API returns default data (typically last 30 days)
- **Important:** Date range cannot exceed 30 days

### Error Handling

The source includes comprehensive error handling for:
- Authentication failures (401)
- Bad requests (400) with specific hints for date range limits
- Rate limiting and server errors
- Network timeouts and connection issues

> [!TIP]
> For daily usage data, you can fetch data without specifying dates to get the most recent 30 days automatically.

> [!WARNING]
> The `daily_usage_data` and `filtered_usage_events` endpoints have a 30-day limit per request. If you need more than 30 days of historical data, make multiple requests with different date ranges.

> [!NOTE]
> - `team_members` uses a GET endpoint
> - All other endpoints use POST with JSON payloads
> - All data is returned with `max_table_nesting=0` to keep the schema flat
