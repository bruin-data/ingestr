# Fluxx

[Fluxx](https://www.fluxx.io/) is a cloud-based grants management platform designed to streamline and automate the entire grantmaking process for foundations, corporations, governments, and other funding organizations.

ingestr supports Fluxx as a source.

## URI format

The URI format for Fluxx is:

```plaintext
fluxx://<instance>?client_id=<client_id>&client_secret=<client_secret>
```

URI parameters:

- `instance`: Your Fluxx instance subdomain (e.g., `mycompany.preprod` for `https://mycompany.preprod.fluxxlabs.com`)
- `client_id`: OAuth 2.0 client ID for authentication
- `client_secret`: OAuth 2.0 client secret for authentication

## Example usage

### Basic usage - all fields

Assuming your instance is `myorg.preprod`, you can ingest grant requests into DuckDB using:

```bash
ingestr ingest \
--source-uri 'fluxx://myorg.preprod?client_id=your_client_id&client_secret=your_client_secret' \
--source-table 'grant_request' \
--dest-uri duckdb:///fluxx.duckdb \
--dest-table 'raw.grant_request'
```

### Custom field selection

You can select specific fields to ingest using the colon syntax:

```bash
ingestr ingest \
--source-uri 'fluxx://myorg.preprod?client_id=your_client_id&client_secret=your_client_secret' \
--source-table 'grant_request:id,amount_requested,amount_recommended,granted' \
--dest-uri duckdb:///fluxx.duckdb \
--dest-table 'raw.grant_request'
```

## Tables

Fluxx source currently supports the following tables:

### Core Resources
- `claim`: Grant claims and payment requests
- `organization`: Organizations (grantees, fiscal sponsors, etc.)
- `grant_request`: Grant applications and requests (300+ fields)
- `user`: User accounts and profiles
- `program`: Funding programs and initiatives
- `request_report`: Reports submitted for grants
- `request_transaction`: Financial transactions and payments
- `sub_program`: Sub-programs under main programs
- `sub_initiative`: Sub-initiatives for detailed planning

### Field Selection

Each resource contains numerous fields. You can:
1. **Ingest all fields**: Use the resource name directly (e.g., `grant_request`)
2. **Select specific fields**: Use colon syntax (e.g., `grant_request:id,name,amount_requested`)

The field selection feature is particularly useful for large resources like `grant_request` which has over 300 fields.

## Key Features

### Incremental Loading
The Fluxx source handles incrementality automatically using the `updated_at` field. You can specify date ranges using:

```bash
ingestr ingest \
--source-uri 'fluxx://myorg.preprod?client_id=your_client_id&client_secret=your_client_secret' \
--source-table 'grant_request' \
--interval-start '2024-01-01' \
--interval-end '2024-12-31' \
--dest-uri duckdb:///fluxx.duckdb \
--dest-table 'raw.grant_request'
```

### Field Type Mapping
The source automatically maps Fluxx field types to appropriate data types:
- Monetary amounts → `decimal`
- IDs and foreign keys → `bigint`
- Dates and timestamps → `date`/`timestamp`
- Boolean fields → `bool`
- Relations/arrays → `json`
- Text fields → `text`

### Data Normalization
The source handles Fluxx API inconsistencies:
- Single values for array fields are automatically wrapped in arrays
- Empty strings are converted to `NULL` for appropriate field types
- Related entity IDs are properly typed

## Authentication

Fluxx uses OAuth 2.0 with client credentials flow. To obtain credentials:

1. Contact your Fluxx administrator to create an API client
2. You'll receive a `client_id` and `client_secret`
3. Note your Fluxx instance subdomain (the part before `.fluxxlabs.com`)

## Limitations

- The Fluxx API may have rate limits depending on your subscription
- Some fields may require specific permissions to access
- Large resources like `grant_request` may take time to ingest due to their size

## Tips

1. **Start with specific fields**: For large resources, start by selecting only the fields you need
2. **Use incremental loading**: For regular syncs, use date ranges to fetch only updated records
3. **Monitor API usage**: Be aware of your API rate limits when ingesting large datasets
4. **Test with small batches**: Use `--yield-limit` to test with a small number of records first

```bash
ingestr ingest \
--source-uri 'fluxx://myorg.preprod?client_id=your_client_id&client_secret=your_client_secret' \
--source-table 'grant_request:id,name,amount_requested' \
--dest-uri duckdb:///test.duckdb \
--dest-table 'test.grant_request' \
--yield-limit 10
```