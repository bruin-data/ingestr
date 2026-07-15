# FastSpring

[FastSpring](https://fastspring.com/) is a merchant of record and e-commerce platform that handles payments, subscriptions, taxes, and invoicing for software and SaaS businesses.

ingestr supports FastSpring as a source.

## URI format

The URI format for FastSpring is as follows:

```plaintext
fastspring://?username=<api-username>&password=<api-password>
```

URI parameters:

- `username`: the API username used for authentication.
- `password`: the API password used for authentication.

You can create API credentials in the FastSpring app under **Developer Tools > APIs > API Credentials**. See the [FastSpring API reference](https://developer.fastspring.com/) for more details.

Here's a sample command that copies FastSpring accounts into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'fastspring://?username=xxx&password=yyy' \
  --source-table 'accounts' \
  --dest-uri duckdb:///fastspring.duckdb \
  --dest-table 'dest.accounts'
```

## Tables

FastSpring source allows ingesting the following resources:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `orders` | id | changed | merge | Orders and their line items, payments, taxes, and any returns. Supports date-range filtering. |
| `subscriptions` | id | changed | merge | Recurring subscriptions, including status, billing period, and pricing. Supports date-range filtering. |
| `accounts` | id | - | replace | Customer accounts, including contact details and address. |
| `products` | id | - | replace | Products in your catalog, including pricing and fulfillment settings. |
| `coupons` | id | - | replace | Coupons and their discount configuration. |

Each table is loaded by first listing the record identifiers and then fetching the full
object for each one. The identifier is stored in an `id` column, which is the primary key.

Use these exact names as the `--source-table` parameter in the `ingestr ingest` command.

`orders` and `subscriptions` support incremental loads over a date range. When
`--interval-start` / `--interval-end` are provided, ingestr fetches only records
in that window; without an interval, it fetches the full history.

```sh
ingestr ingest \
  --source-uri 'fastspring://?username=xxx&password=yyy' \
  --source-table 'orders' \
  --dest-uri duckdb:///fastspring.duckdb \
  --dest-table 'dest.orders' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```

## Reports

FastSpring's Data API can generate aggregated subscription and revenue reports:

| Table | Details |
| ----- | ------- |
| `subscription_report` | Subscription metrics (MRR, ARR, subscribers, churn, …) grouped by the fields you choose. |
| `revenue_report` | Revenue metrics grouped by the fields you choose. |

Each report has sensible default columns and grouping. To customize, use the colon
form `<report>:<columns>:<group_by>`, where `columns` and `group_by` are
comma-separated lists. Omit a part to keep its default:

- `subscription_report`: default columns and grouping.
- `subscription_report:mrr,arr,subscription_id,transaction_date`: custom columns, default grouping.
- `revenue_report:income_in_usd,product_name:product_name,transaction_month`: custom columns and grouping.
See the [subscription report](https://developer.fastspring.com/reference/generate-subscription-report)
and [revenue report](https://developer.fastspring.com/reference/generate-revenue-report)
references for the full list of columns.

The `group_by` fields become the table's primary key, so they must uniquely identify a
row.

### Incremental behavior

Reports load incrementally (merge) using the **sync date** as the incremental key:
`sync_date` for `subscription_report` and `syncdate` for `revenue_report`. This is
FastSpring's field for keeping a database in sync with the Data API: `--interval-start`
is sent as the report's sync-date filter, so each run only pulls rows that were synced
on or after that date, and results are upserted on the `group_by` primary key without
duplicates.

The sync-date column is **always requested**, even when you pass a custom `columns` list,
so incremental sync keeps working regardless of the columns you choose. The interval, when
provided, applies to every run including the first: pass `--interval-start` to fetch only
rows synced since that date, or omit it to fetch the full report.

```sh
ingestr ingest \
  --source-uri 'fastspring://?username=xxx&password=yyy' \
  --source-table 'revenue_report' \
  --dest-uri duckdb:///fastspring.duckdb \
  --dest-table 'dest.revenue_report' \
  --interval-start '2024-01-01'
```

`--interval-start` is used as the report's sync-date filter (rows synced on or after that
date); `--interval-end` is not used by reports.
