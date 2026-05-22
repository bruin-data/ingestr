---
outline: deep
---

# Ingestr v1 Migration Guide

If you're using ingestr today, this page tells you what changes when you move to ingestr v1, what you need to do, and where you might get tripped up.

The good news: same `ingest` command, same flags, same URIs. Most pipelines run unchanged.

The not-so-good news: there are a handful of things that look different in your data, and a couple of sources behave differently by default. [Things to watch out for](#things-to-watch-out-for)

## What is ingestr v1?

It's ingestr, rewritten from the ground up — but:

- **Faster** on most workloads, with lower memory use for the same job.
- Adds **new sources, destinations, and strategies**

You can think of v1 as the same product, version 2 point oh.

## What's new

### New strategies

On top of the existing strategies (`replace`, `append`, `merge`, `delete+insert`), v1 adds:

- **`truncate+insert`** — empty the destination table and load the new rows in one go.
- **`scd2`** — Slowly Changing Dimension Type 2: keeps a row history per primary key (with `valid_from` / `valid_to` columns) so you can see how a record changed over time.
- **`replace`** — Now uses double buffering to reduce transaction times duration large loads.


## Things to watch out for

These are the changes that can make your downstream queries break or your numbers look different. Worth a careful read.

### Nested JSON is no longer flattened

legacy ingestr flattens nested JSON fields into separate columns. ingestr v1 keeps them as a single JSON column.

For example, a nested field like `{ "assignee": { "id": 123, "name": "Alice" } }`:

- **ingestr v0:** creates columns `assignee_id` and `assignee_name`.
- **ingestr v1:** creates a single `assignee` JSON column containing `{"id": 123, "name": "Alice"}`.

**What you need to do:** if your queries used `assignee_id` or `assignee_name`, update them to extract those fields from the JSON column. 

### Default strategy when `--incremental-strategy` is not set

For sources that don't handle incrementality internally (e.g., S3, GCS, Azure Blob, and others), v1 now defaults to `replace` when no `--incremental-strategy` flag is provided. legacy ingestr would use sometimes
use different incremental strategies for these, depending on the source implementation.

**What you need to do:** if you were relying on a different default strategy for any of these sources, set it explicitly with `--incremental-strategy` to whatever you actually want.

## Source level differences

Most sources behave the same way. The list below is sources where you'll notice something has changed in the data itself or the table list.

### BigQuery

If you ingest from BigQuery, the service account now needs the role **`roles/bigquery.readSessionUser`** in addition to whatever it already has. Without it, reads will fail.

This is because v1 reads BigQuery via the Storage Read API, which has a stricter permission model than the regular query API v1 used.

**What you need to do:** ask whoever manages your GCP IAM to add this role to your ingestion service account.

### Chess.com

- Table names simplified by removing the `players_` prefix: `players_profiles` → `profiles`, `players_games` → `games`, `players_archives` → `archives`.
- `players_online_status` table removed — online status data (`last_online` field) is included in the `profiles` table.

**What you need to do:** rename your `--source-table` references and update any downstream queries that joined `players_online_status`.

### Docebo

- The `polls` table is folded into `survey_answers`. Each row in `survey_answers` for a poll-type learning object now has `poll_id` and `poll_title` columns.

**What you need to do:** stop ingesting `polls` and look for that data in `survey_answers` instead.

### Facebook Ads

Same tables as before, but a few of them are computed differently:

- **`ad_creatives`:** now deterministic. Running it twice on the same data gives the same row order. v1 sometimes returned different orders across runs.
- **`facebook_insights`:** more columns (breakdowns, custom fields). Primary keys now include the level and breakdown. v1 used placeholder PK values.
- **`leads`:** only fetches from campaigns with a leads-related objective. Faster, but you won't see leads from non-leads campaigns.

**What you need to do:** if you're comparing row counts side-by-side between two versions, expect them not to match exactly for these three tables.

### FundraiseUp

- Incremental loads now available for every table, not just `donations`. New tables: `events:incremental`, `fundraisers:incremental`, `recurring_plans:incremental`, `supporters:incremental`. All use `created_at` as the incremental key.
- Naming convention changed:
  - **legacy ingestr:** `donations_incremental` (underscore)
  - **ingestr v1:** `donations:incremental` (colon)

**What you need to do:** if you used `donations_incremental`, change it to `donations:incremental`.

### Google Sheets

Better handling of duplicate column headers:

- **legacy ingestr:** if any header was duplicated, v1 gave up on the whole header row and renamed every column to `col_1`, `col_2`, etc., shoving the original headers into the data.
- **ingestr v1:** original headers are kept; only the duplicated ones get a suffix (`name`, `name_2`, `name_3`). Empty header cells become `column_<index>`.

**What you need to do:** if you had Google Sheet pipelines where the columns were called `col_1`, `col_2`, …, expect them to come through with the real header names now.

### HubSpot

HubSpot has the most v1-specific changes of any source. Worth reading carefully if you have HubSpot pipelines.

#### New tables

- **`pipelines`** — every sales/service pipeline across all CRM object types (deals, tickets, appointments, courses, listings, orders, services, leads).
- **`pipeline_stages`** — every stage for every pipeline. Useful for joining stage names onto deal records.
- **`property_history:<object>`** — for every CRM object (e.g., `property_history:contacts`, `property_history:deals`). One row per property change. Available for: `contacts`, `companies`, `deals`, `tickets`, `products`, `quotes`, `calls`, `emails`, `feedback_submissions`, `line_items`, `meetings`, `notes`, `tasks`, `carts`, `discounts`, `fees`, `invoices`, `commerce_payments`, `taxes`, and custom objects via `property_history:custom:<objectType>`.

#### New column on every CRM object: `_archived_at`

v1 fetches archived (soft-deleted) records too, alongside live records:

- **`_archived_at` column:** `NULL` for live records, populated for archived ones.
- Your destination table will contain records that legacy ingestr silently dropped after they were archived in HubSpot.

**What you need to do:** if your downstream models assume "every row in this table exists in HubSpot", filter on `_archived_at IS NULL`. Otherwise you'll start seeing soft-deleted records.

#### Filter property history to specific properties

To track only a few properties instead of everything, append a comma-separated list to the table name:

```bash
ingestr ingest --use-gong \
    --source-uri='hubspot://...' \
    --source-table='property_history:contacts:firstname,lastname,email' \
    --dest-uri='...'
```

Without the suffix, every property's history is fetched (which can be a lot of data).

#### Override associations per table

By default, each CRM table fetches a fixed set of associations (e.g., `contacts` brings `companies`, `deals`, `products`, `tickets`, `quotes`). To override:

```bash
--source-table='contacts:companies,deals'   # only fetch companies and deals associations
--source-table='contacts:'                   # no associations at all
```

Useful for skipping associations you don't need to make ingestion faster.

#### Custom objects respect `--interval-start`

- **legacy ingestr:** ignored the date range for custom objects — always returned everything.
- **ingestr v1:** honors `--interval-start` via the Search API.

You may see fewer records than legacy ingestr when a date range is set. That's correct behavior, not a regression.

#### Incremental loading via time intervals

- **With `--interval-start`:** ingestr uses the CRM Search API filtered by your incremental key (only records modified since the given date).
- **Without it (or with `--full-refresh`):** ingestr will use the CRM List API for a complete fetch.

Make sure your scheduler is passing `--interval-start` if you want incremental.

**Fetch all data:**

```bash
ingestr ingest --use-gong \
    --source-uri='hubspot://...' \
    --source-table=contacts \
    --dest-uri='...' \
    --full-refresh
```

**Fetch incremental data:**

```bash
ingestr ingest --use-gong \
    --source-uri='hubspot://...' \
    --source-table=contacts \
    --dest-uri='...' \
    --interval-start=2024-01-01T00:00:00Z
```

**Affected tables:** this applies to all 19 CRM object tables — `contacts`, `companies`, `deals`, `tickets`, `products`, `quotes`, `calls`, `emails`, `feedback_submissions`, `line_items`, `meetings`, `notes`, `tasks`, `carts`, `discounts`, `fees`, `invoices`, `commerce_payments`, `taxes` — and custom objects.

**Unaffected tables:** `owners` and `schemas` always use the List API regardless of flags and makes a complete fetch.

### Intercom

- The `tickets` table is no longer available in v1.

### Jira

Three new tables expose the metadata behind your Jira custom fields:

- **`issue_fields`** — every field (system + custom) with its id, name, and type.
- **`issue_custom_field_contexts`** — which projects and issue types each custom field applies to.
- **`issue_custom_field_options`** — the available options for select/multi-select fields.

These are useful if you want to interpret custom field IDs in your downstream models.

### MongoDB

For very large MongoDB collections, v1 can use more memory than legacy ingestr. The same MongoDB job may run out of memory on a small worker.

**What you need to do:** if you hit OOM:

- Raise your worker memory.
- Lower `--page-size`.
- Split the load by date with `--interval-start` / `--interval-end`.

### Notion

When using the Notion source, passing `*` as the `--source-table` value will fetch **all databases** accessible. You can also still fetch a specific database by its ID.

```bash
# Fetch all databases
ingestr ingest --use-gong --source-uri=notion://... --source-table='*' --dest-uri=...

# Fetch a specific database by ID
ingestr ingest --use-gong --source-uri=notion://... --source-table=<database-id> --dest-uri=...
```

### RevenueCat

- The `customer_ids` table is gone.
- Customer ID information is now part of the `customers` table.

### Shopify

- The `price_rules` table is gone (deprecated). Use `discounts` instead.

### Stripe

- **legacy ingestr:** a Stripe run pulls your full history.
- **ingestr v1:** the default is "events-based" — only fetches things that changed in the last 30 days, and merges them into your destination table.

This is faster and what you actually want for a recurring schedule. But on the **first** run after you migrate, it means you won't have your full history.

**What you need to do:** on the first run, add `--full-refresh` to pull everything. After that, run normally:

```bash
ingestr ingest --use-gong --source-uri=stripe://... --source-table=customer --dest-uri=... --full-refresh
```

If you need to backfill more than 30 days at any point, set `--interval-start` to a date older than 30 days and ingestr will fall back to the regular Stripe API.

**Affected tables:** the events-based default applies to tables that support event type filtering: `account`, `application_fee`, `charge`, `checkout_session`, `coupon`, `credit_note`, `customer`, `dispute`, `invoice`, `invoice_item`, `payment_intent`, `payment_link`, `payment_method`, `payout`, `plan`, `price`, `product`, `promotion_code`, `quote`, `refund`, `review`, `setup_intent`, `subscription`, `subscription_schedule`, `tax_rate`, `top_up`, `transfer`.

**Unaffected tables:** these continue to use a full listing and are not subject to the 30-day window: `balance_transaction`, `event`, `shipping_rate`, `apple_pay_domain`, `setup_attempt`, `subscription_item`, `tax_code`, `tax_id`, `webhook_endpoint`.

### Zendesk

`calls` and `calls_incremental` are now two separate tables.

- **`calls`** — full fetch (replace strategy).
- **`calls_incremental`** — merge by `id`, using `updated_at` as the incremental key.

### Zoom

Two changes:

- **`users`** — now uses `replace` instead of `merge` by default.
- **`participants`** — no longer uses `join_time` as the incremental key. Strategy is still `merge` by `id`, but with no incremental key — every run does a full fetch and upserts by `id`.


## Have questions or run into a problem?

You don't have to figure this out on your own. If something doesn't behave the way you expect, your numbers don't match, or you just want a sanity check before flipping a pipeline over to ingestr v1 — please reach out.

- **Fastest:** the **#ingestr** channel in the [Bruin Slack community](https://join.slack.com/t/bruindatacommunity/shared_invite/zt-2dl2i8foy-bVsuMUauHeN9M2laVm3ZVg). The maintainers and other users are there, and most migration questions get answered the same day.
- **For something the team can track:** open an issue at [github.com/bruin-data/ingestr](https://github.com/bruin-data/ingestr/issues) with the source/destination, the command you ran, and what you saw.

Don't worry about asking "small" questions — if you're unsure about something here, someone else probably is too.
