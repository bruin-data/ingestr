# Recurly

[Recurly](https://recurly.com/) is a subscription management and recurring billing platform that handles subscriptions, invoicing, payments, and revenue recognition.

## URI format

The URI format for Recurly is as follows:

```plaintext
recurly://?api_key=<api-key-here>
```

URI parameters:

- `api_key`: the Recurly private API key used for authentication.
- `region` (optional): the data center region, either `us` (default) or `eu`.

Recurly authenticates with HTTP Basic Auth using the private API key. You can find your private API key in the Recurly dashboard under **Integrations > API Credentials**. See the [Recurly API reference](https://recurly.com/developers/api/) for more details.

Here's a sample command that copies Recurly accounts into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'recurly://?api_key=<api-key-here>' \
  --source-table 'accounts' \
  --dest-uri duckdb:///recurly.duckdb \
  --dest-table 'dest.accounts'
```

## Tables

Recurly source allows ingesting the following resources:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `accounts` | id | updated_at | merge | Customer accounts, including billing details, contact info, and custom fields. |
| `subscriptions` | id | updated_at | merge | Recurring subscriptions, including status, billing cycle, and subscription add-ons. |
| `invoices` | id | updated_at | merge | Invoices generated for subscriptions and one-off charges, including line items, taxes, and discounts. |
| `transactions` | id | updated_at | merge | Payment, refund, and verification transactions linked to invoices. |
| `plans` | id | updated_at | merge | Subscription plans, including pricing, billing intervals, and add-ons. |

Use these exact names as the `--source-table` parameter in the `ingestr ingest` command.

```sh
ingestr ingest \
  --source-uri 'recurly://?api_key=<api-key-here>' \
  --source-table 'subscriptions' \
  --dest-uri duckdb:///recurly.duckdb \
  --dest-table 'dest.subscriptions' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```

