# RevenueCat

[RevenueCat](https://www.revenuecat.com/) is a complete solution for implementing in-app subscriptions and purchases across all platforms, with real-time analytics and infrastructure for scaling subscription businesses.

ingestr supports RevenueCat as a source.

## URI format

The URI format for RevenueCat is:

```plaintext
revenuecat://?api_key=<api_key>&project_id=<project_id>
```

URI parameters:

- `api_key`: The API v2 secret key with Bearer token format used for authentication with the RevenueCat API.
- `project_id`: The RevenueCat project ID (required for customers, products, and related resources).

## Example usage

Assuming your API key is `rcat_v2_abc123` and project ID is `proj_abc123`, you can ingest customers into DuckDB using:

```bash
ingestr ingest \
--source-uri 'revenuecat://?api_key=rcat_v2_abc123&project_id=proj_abc123' \
--source-table 'customers' \
--dest-uri duckdb:///revenuecat.duckdb \
--dest-table 'dest.customers'
```

To ingest projects (no project_id required):

```bash
ingestr ingest \
--source-uri 'revenuecat://?api_key=rcat_v2_abc123' \
--source-table 'projects' \
--dest-uri duckdb:///revenuecat.duckdb \
--dest-table 'dest.projects'
```



## Tables

RevenueCat source allows ingesting the following tables:

- `projects`: Fetches all projects from your RevenueCat account.
- `customers`: Fetches all customers with nested purchases and subscriptions data.
- `products`: Fetches all products configured in your RevenueCat project.

Use these as the `--source-table` parameter in the `ingestr ingest` command.

## Notes
- The `project_id` parameter is required for customers and products tables but not for projects.