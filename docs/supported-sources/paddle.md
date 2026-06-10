# Paddle

[Paddle](https://www.paddle.com/) is a merchant of record and billing platform that handles payments, subscriptions, taxes, and invoicing for software businesses.

ingestr supports Paddle (Paddle Billing) as a source.

## URI format

The URI format for Paddle is as follows:

```plaintext
paddle://?api_key=<api-key-here>
```

URI parameters:

- `api_key`: the Paddle API key used for authentication.

You can create an API key in the Paddle dashboard under **Developer tools > Authentication**. See the [Paddle API reference](https://developer.paddle.com/api-reference/overview) for more details.

Here's a sample command that copies Paddle customers into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'paddle://?api_key=pdl_live_apikey_12345' \
  --source-table 'customers' \
  --dest-uri duckdb:///paddle.duckdb \
  --dest-table 'dest.customers'
```

## Tables

Paddle source allows ingesting the following resources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `customers` | id | updated_at | merge | Customer records, including name, email, and locale. |
| `products` | id | updated_at | merge | Products you sell, including name, description, and tax category. |
| `prices` | id | updated_at | merge | Prices attached to products, including billing cycle and currency. |
| `discounts` | id | updated_at | merge | Discounts and coupon codes that can be applied to transactions and subscriptions. |
| `transactions` | id | updated_at | merge | Transactions, the core billing record. Each transaction carries the `invoice_number` Paddle generates, so this table is where invoice data lives. |
| `subscriptions` | id | updated_at | merge | Recurring subscriptions, including status, billing cycle, and scheduled changes. |
| `adjustments` | id | updated_at | merge | Adjustments such as refunds, credits, and chargebacks against transactions. |

Use these exact names as the `--source-table` parameter in the `ingestr ingest` command.


```sh
ingestr ingest \
  --source-uri 'paddle://?api_key=pdl_live_apikey_12345' \
  --source-table 'transactions' \
  --dest-uri duckdb:///paddle.duckdb \
  --dest-table 'dest.transactions' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```