# Breaking Changes

## Default Strategy for Sources That Cannot Handle Incrementality

**Version:** v0.1.11

### Summary

For sources that cannot handle incrementality internally (i.e., sources where `HandlesIncrementality()` returns `false`), the default strategy when no `--incremental-strategy` flag is provided is now **`replace`**.

If you were relying on a different default strategy for sources that cannot handle incrementality, you must now explicitly specify the strategy using the `--incremental-strategy` flag.

### Affected Sources

This change affects blobstore sources (S3, GCS, Azure Blob) that previously used a different default strategy.

---

## Stripe Source: Default Behavior Changed to Events-Based Incremental Loading

### Summary

The Stripe source now uses **events-based incremental loading** by default for most tables. This means a default run will only pick up objects that had events in the last 30 days (or since the last `--interval-start`), using a **merge** strategy. It will not fetch your entire Stripe history.

### 30-Day Interval Limit

The Stripe Events API only retains events for 30 days. If `--interval-start` is set to more than 30 days ago, the source will automatically fall back to **sync incremental mode** (direct API listing with date filters). To use the events endpoint, set `--interval-start` to within the last 30 days.

### How to Fetch All Data

If you need to load all data from a Stripe table, use `--full-refresh`:

```bash
gong ingest --source-uri=stripe://... --source-table=customer --dest-uri=... --full-refresh
```

### Affected Tables

This applies to all tables that support event type filtering, including: `account`, `application_fee`, `charge`, `checkout_session`, `coupon`, `credit_note`, `customer`, `dispute`, `invoice`, `invoice_item`, `payment_intent`, `payment_link`, `payment_method`, `payout`, `plan`, `price`, `product`, `promotion_code`, `quote`, `refund`, `review`, `setup_intent`, `subscription`, `subscription_schedule`, `tax_rate`, `top_up`, `transfer`.

Tables without event support (e.g., `balance_transaction`, `event`, `shipping_rate`, `apple_pay_domain`, `setup_attempt`, `subscription_item`, `tax_code`, `tax_id`, `webhook_endpoint`) are unaffected and continue to use a full listing.

---

## HubSpot Source: Full Refresh Required for Complete Data Fetch

### Summary

The HubSpot source uses **search-based incremental loading** when `--interval-start` is provided. If `--interval-start` is not provided or `--full-refresh` is used, all data will be fetched using the CRM List API.

### How It Works

- **Full refresh** (`--full-refresh`): Fetches all data using the CRM List API. Use this when you need a complete dataset.
- **Incremental** (`--interval-start`): Fetches only records modified since the given date using the CRM Search API with a date filter on the incremental key.

### How to Fetch All Data

```bash
gong ingest --source-uri=hubspot://... --source-table=contacts --dest-uri=... --full-refresh
```

### How to Fetch Incremental Data

```bash
gong ingest --source-uri=hubspot://... --source-table=contacts --dest-uri=... --interval-start=2024-01-01T00:00:00Z
```

### Affected Tables

This applies to all 19 CRM object tables: `contacts`, `companies`, `deals`, `tickets`, `products`, `quotes`, `calls`, `emails`, `feedback_submissions`, `line_items`, `meetings`, `notes`, `tasks`, `carts`, `discounts`, `fees`, `invoices`, `commerce_payments`, `taxes`, and custom objects.

The `owners` and `schemas` tables always use the List API and are unaffected.

---

## InfluxDB Source: v2 Client (Flux) Used by Default, v3 Client Available via URI Option

### Summary

The InfluxDB source uses the **v2 client with Flux queries** (`/api/v2/query`) by default. This returns data in **long/unpivoted format** (one row per field: `time`, `field`, `value`, `measurement`, plus tag columns).

To use the **v3 client with SQL** (Apache Arrow Flight gRPC), add `influxdb3=true` to the source URI. This returns data in **wide/pivoted format** (one row per timestamp, fields as columns).

### URI Format

```bash
# Default (v2, Flux, unpivoted)
gong ingest --source-uri="influxdb://host?token=TOKEN&org=ORG&bucket=BUCKET" --source-table=cpu --dest-uri=...

# v3 (SQL, pivoted)
gong ingest --source-uri="influxdb://host?token=TOKEN&org=ORG&bucket=BUCKET&influxdb3=true" --source-table=cpu --dest-uri=...
```

### Client Compatibility

| InfluxDB Product              | v2 Client (default)     | v3 Client (`influxdb3=true`) |
|-------------------------------|-------------------------|------------------------------|
| InfluxDB OSS v2 / Cloud (TSM)| Read & Write (Flux)     | Not applicable               |
| InfluxDB Cloud Serverless     | Write only              | Read & Write (SQL)           |
| InfluxDB Cloud Dedicated      | Write only              | Read & Write (SQL)           |
| InfluxDB 3 Core / Enterprise  | Write only              | Read & Write (SQL)           |
| InfluxDB Clustered            | Write only              | Read & Write (SQL)           |

### Key Differences

| Feature           | v2 Client (default)                          | v3 Client (`influxdb3=true`)                |
|-------------------|----------------------------------------------|---------------------------------------------|
| Query protocol    | `/api/v2/query` (Flux)                       | Apache Arrow Flight RPC (SQL / InfluxQL)    |
| Output format     | Long/unpivoted (field per row)               | Wide/pivoted (fields as columns)            |
| Row count example | `cpu` with 3 fields × 6 points = **18 rows** | `cpu` with 3 fields × 6 points = **6 rows** |

### When to Use Which

- Use **default (v2)** if you are on InfluxDB OSS v2 or Cloud (TSM), or if you want the same output format as the Python ingestr tool.
- Use **`influxdb3=true`** if you are on InfluxDB 3 products (Cloud Serverless, Cloud Dedicated, Core, Enterprise, Clustered) and want pivoted tabular output.

---

## Notion Source: Wildcard `*` Fetches All Databases

### Summary

When using the Notion source, passing `*` as the `--source-table` value will fetch **all databases** accessible by the integration token. You can also still fetch a specific database by its ID.

### Usage

```bash
# Fetch all databases
gong ingest --source-uri=notion://... --source-table='*' --dest-uri=...

# Fetch a specific database by ID
gong ingest --source-uri=notion://... --source-table=<database-id> --dest-uri=...
```

## HubSpot Source: Property History Tables Added

### Summary

The HubSpot source now supports **property history tables** for all CRM object types. These tables return one row per property change, enabling tracking of how properties changed over time.

### New Tables

For every CRM object table, a corresponding `property_history:<table>` table is now available: `property_history:contacts`, `property_history:companies`, `property_history:deals`, `property_history:tickets`, `property_history:products`, `property_history:quotes`, `property_history:calls`, `property_history:emails`, `property_history:feedback_submissions`, `property_history:line_items`, `property_history:meetings`, `property_history:notes`, `property_history:tasks`, `property_history:carts`, `property_history:discounts`, `property_history:fees`, `property_history:invoices`, `property_history:commerce_payments`, `property_history:taxes`.

The `owners` and `schemas` tables do not have history variants.

Custom objects also support history via `property_history:custom:<objectType>` (e.g., `property_history:custom:myObject`).

---

## BigQuery Source: Arrow reader requires roles/bigquery.readSessionUser

### Summary
BigQuery source requires additional permissions that ingestr doesn't. This comes from ADBC and outside of our control for now. 

see: https://github.com/apache/arrow-adbc/issues/3282

### Impact
Migrated pipelines may break due to insufficient permissions. This only impacts existing users.