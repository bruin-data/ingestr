# Chargebee

[Chargebee](https://www.chargebee.com/) is a subscription billing and revenue management platform that handles recurring billing, invoicing, payments, and subscription lifecycle management.

## URI format

The URI format for Chargebee is as follows:

```plaintext
chargebee://<site>?api_key=<api-key-here>
```

URI parameters:

- `site`: your Chargebee site name (the subdomain in `https://<site>.chargebee.com`).
- `api_key`: the Chargebee API key used for authentication.

Chargebee authenticates with HTTP Basic Auth using the API key. You can create an API key in the Chargebee dashboard under **Settings > Configure Chargebee > API Keys**. See the [Chargebee API reference](https://apidocs.chargebee.com/docs/api) for more details.

Here's a sample command that copies Chargebee customers into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'chargebee://acme-test?api_key=test_xxx' \
  --source-table 'customers' \
  --dest-uri duckdb:///chargebee.duckdb \
  --dest-table 'dest.customers'
```

## Tables

Chargebee source allows ingesting the following resources:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `customers` | id | updated_at | merge | Customer records, including billing details, contact info, and custom fields. |
| `subscriptions` | id | updated_at | merge | Recurring subscriptions, including status, billing cycle, and subscription items. |
| `invoices` | id | updated_at | merge | Invoices generated for subscriptions and one-off charges, including line items and amounts. |
| `transactions` | id | updated_at | merge | Payment, refund, and credit transactions linked to invoices. |
| `orders` | id | updated_at | merge | Orders generated for invoices, including fulfillment status and shipping details. |
| `events` | id | occurred_at | merge | Activity events such as subscription and customer changes, useful for change-style ingestion. |

Use these exact names as the `--source-table` parameter in the `ingestr ingest` command.

```sh
ingestr ingest \
  --source-uri 'chargebee://acme-test?api_key=test_xxx' \
  --source-table 'subscriptions' \
  --dest-uri duckdb:///chargebee.duckdb \
  --dest-table 'dest.subscriptions' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```

