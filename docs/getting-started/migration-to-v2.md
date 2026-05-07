---
outline: deep
---

# Moving from ingestr v1 to ingestr v2

If you're using ingestr today, this page tells you what changes when you move to ingestr v2, what you need to do, and where you might get tripped up.

The good news: same `ingest` command, same flags, same URIs. Most pipelines run unchanged.

The not-so-good news: there are a handful of things that look different in your data, and a couple of sources behave differently by default. Read the [Things to watch out for](#things-to-watch-out-for) section before you flip the switch on anything important.

## What is ingestr v2?

It's ingestr, rewritten from the ground up — but:

- **Faster** on most workloads, with lower memory use for the same job (one MongoDB caveat — see [MongoDB](#mongodb)).
- Adds **new sources, destinations, and strategies** that v1 doesn't have.

You can think of v2 as the same product, version 2.

## The timeline (please read this first)

You don't have to do anything immediately. There's a 30-day grace period.

- **For the first 30 days after v2 is announced:** default is v1. Your existing pipelines keep running on v1 with no work on your side. If you want to opt into v2 early, add `use_gong: true` to its parameters (or pass `--use-gong` on the CLI) — see [How to run on v2](#how-to-run-on-v2) below.
- **After 30 days:** default flips to v2 for every pipeline. There is no opt-out. Plan your migration to be done — or at least tested — within the 30-day window.

## How to run on v2

Nothing to install. v2 is bundled into the ingestr you already have. To run a pipeline on v2:

- **Pass `--use-gong`** on the command line (locally), or
- **Add `use_gong: true`** to the `parameters` block of your Bruin asset YAML, or
- **Run on Bruin Cloud**, which is already on v2 by default, or
- **Wait for the 30-day grace period to end** — after that, every run uses v2.

The command and flags are otherwise unchanged:

```bash
ingestr ingest \
    --source-uri 'postgres://user:pass@host:5432/db' \
    --source-table 'public.users' \
    --dest-uri 'bigquery://my-project?credentials_path=/path/to/sa.json' \
    --dest-table 'raw.users'
```

Your existing scheduler scripts and environment variables (`SOURCE_URI`, `DESTINATION_URI`, `INCREMENTAL_STRATEGY`, etc.) continue to work — v2 reads the same variable names.

There are two ways to opt into v2 early during the grace period:

### Option 1: pass `--use-gong` on the CLI

Add the flag to any individual run that should use v2:

```bash
ingestr ingest --use-gong \
    --source-uri 'postgres://user:pass@localhost:5432/mydb?sslmode=disable' \
    --source-table 'public.users' \
    --dest-uri 'bigquery://my-project?credentials_path=/path/to/sa.json' \
    --dest-table 'raw.users'
```

### Option 2: set `use_gong: true` in your Bruin asset YAML

For a Bruin pipeline, add `use_gong: true` to the asset's `parameters` block:

```yaml
name: raw.users
type: ingestr

parameters:
  source_connection: my_postgres
  source_table: public.users
  destination: bigquery
  use_gong: true   
```

- The next run of that asset uses v2.
- Other assets in the same pipeline stay on v1 unless they have their own `use_gong: true`.

### Bruin Cloud

**Bruin Cloud always uses v2.** Cloud doesn't honor the grace period.

### If something goes wrong

If you find a problem on v2 during the grace period:

- Drop the `--use-gong` flag, or
- Remove `use_gong: true` from the asset YAML.

Either way, you're back on v1 for that pipeline. Then come tell us in the community channel — see [Have questions or run into a problem?](#have-questions-or-run-into-a-problem) at the bottom of this page.

After the 30-day window ends, v2 is the only option, so flagging issues during the grace period is the time to do it.

## Things to watch out for

These are the changes that can make your downstream queries break or your numbers look different. Worth a careful read.

### Nested JSON is no longer flattened

**Most visibly affected sources:** Asana, Pipedrive, Jira, Notion, Couchbase. These are the highest-impact cases — but the change applies to **any** source where nested objects exist (see [Sources where nothing changed](#sources-where-nothing-changed) for the general note).

ingestr v1 flattens nested JSON fields into separate columns. ingestr v2 keeps them as a single JSON column.

For example, a nested field like `{ "assignee": { "id": 123, "name": "Alice" } }`:

- **ingestr v1:** creates columns `assignee_id` and `assignee_name`.
- **ingestr v2:** creates a single `assignee` JSON column containing `{"id": 123, "name": "Alice"}`.

**What you need to do:** if your queries used `assignee_id` or `assignee_name`, update them to extract those fields from the JSON column. 

### Default strategy when `--incremental-strategy` is not set

For sources that don't handle incrementality internally (e.g., S3, GCS, Azure Blob, and others), v2 now defaults to `replace` when no `--incremental-strategy` flag is provided. v1 sometimes used a different default.

**What you need to do:** if you were relying on a different default strategy for any of these sources, set it explicitly with `--incremental-strategy` to whatever you actually want.

## Things that look different per source

Most sources behave the same way. The list below is sources where you'll notice something has changed in the data itself or the table list.

### BigQuery

If you ingest from BigQuery, the service account now needs the role **`roles/bigquery.readSessionUser`** in addition to whatever it already has. Without it, reads will fail.

This is because v2 reads BigQuery via the Storage Read API, which has a stricter permission model than the regular query API v1 used.

**What you need to do:** ask whoever manages your GCP IAM to add this role to your ingestion service account before you run v2 against BigQuery.

### Chess.com

- Table names simplified by removing the `players_` prefix: `players_profiles` → `profiles`, `players_games` → `games`, `players_archives` → `archives`.
- `players_online_status` table removed — online status data (`last_online` field) is included in the `profiles` table.

**What you need to do:** rename your `--source-table` references and update any downstream queries that joined `players_online_status`.

### Customer.io

- 5 new metrics tables added: `broadcast_metrics`, `broadcast_action_metrics`, `campaign_metrics`, `campaign_action_metrics`, `newsletter_metrics`.
- Existing tables are unchanged.

### Cursor

- The `daily_usage_data` table can now fetch any date range — it automatically pages through 30-day chunks.
- In v1 you were limited to 30 days at a time.

### Docebo

- The `polls` table is folded into `survey_answers`. Each row in `survey_answers` for a poll-type learning object now has `poll_id` and `poll_title` columns.

**What you need to do:** stop ingesting `polls` and look for that data in `survey_answers` instead.

### Facebook Ads

Same tables as before, but a few of them are computed differently:

- **`ad_creatives`:** now deterministic. Running it twice on the same data gives the same row order. v1 sometimes returned different orders across runs.
- **`facebook_insights`:** more columns (breakdowns, custom fields). Primary keys now include the level and breakdown. v1 used placeholder PK values.
- **`leads`:** only fetches from campaigns with a leads-related objective. Faster, but you won't see leads from non-leads campaigns.

**What you need to do:** if you're comparing v1 and v2 row counts side-by-side, expect them not to match exactly for these three tables. The v2 numbers are generally the right ones.

### FundraiseUp

- Incremental loads now available for every table, not just `donations`. New tables: `events:incremental`, `fundraisers:incremental`, `recurring_plans:incremental`, `supporters:incremental`. All use `created_at` as the incremental key.
- Naming convention changed:
  - **ingestr v1:** `donations_incremental` (underscore)
  - **ingestr v2:** `donations:incremental` (colon)

**What you need to do:** if you used `donations_incremental` in v1, change it to `donations:incremental` in v2.

### Google Sheets

Better handling of duplicate column headers:

- **ingestr v1:** if any header was duplicated, v1 gave up on the whole header row and renamed every column to `col_1`, `col_2`, etc., shoving the original headers into the data.
- **ingestr v2:** original headers are kept; only the duplicated ones get a suffix (`name`, `name_2`, `name_3`). Empty header cells become `column_<index>`.

**What you need to do:** if you had Google Sheet pipelines where the columns were called `col_1`, `col_2`, …, expect them to come through with the real header names now.

### HubSpot

HubSpot has the most v2-specific changes of any source. Worth reading carefully if you have HubSpot pipelines.

#### New tables

- **`pipelines`** — every sales/service pipeline across all CRM object types (deals, tickets, appointments, courses, listings, orders, services, leads).
- **`pipeline_stages`** — every stage for every pipeline. Useful for joining stage names onto deal records.
- **`property_history:<object>`** — for every CRM object (e.g., `property_history:contacts`, `property_history:deals`). One row per property change. Available for: `contacts`, `companies`, `deals`, `tickets`, `products`, `quotes`, `calls`, `emails`, `feedback_submissions`, `line_items`, `meetings`, `notes`, `tasks`, `carts`, `discounts`, `fees`, `invoices`, `commerce_payments`, `taxes`, and custom objects via `property_history:custom:<objectType>`.

#### New column on every CRM object: `_archived_at`

v2 fetches archived (soft-deleted) records too, alongside live records:

- **`_archived_at` column:** `NULL` for live records, populated for archived ones.
- Your destination table will contain records that v1 silently dropped after they were archived in HubSpot.

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

- **ingestr v1:** ignored the date range for custom objects — always returned everything.
- **ingestr v2:** honors `--interval-start` via the Search API.

You may see fewer records than v1 when a date range is set. That's correct behavior, not a regression.

#### Search vs List API

- **With `--interval-start`:** v2 uses the CRM Search API filtered by your incremental key (only records modified since the given date).
- **Without it (or with `--full-refresh`):** v2 uses the CRM List API for a complete fetch.

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

### InfluxDB

By default, v2's InfluxDB source works the same way as v1 (Flux queries, "long" output — one row per field per timestamp).

If you're on a newer InfluxDB product (Cloud Serverless, Cloud Dedicated, InfluxDB 3 Core / Enterprise / Clustered), you can opt into the SQL-based v3 client by adding `influxdb3=true` to the source URI. That mode returns "wide" output with fields as columns.

**What you need to do:**

- On InfluxDB OSS v2 or Cloud TSM: nothing.
- On InfluxDB 3 products: add `influxdb3=true` to your URI.

### Intercom

- The `tickets` table is no longer available in v2.
- Other Intercom tables — `contacts`, `companies`, `conversations`, `articles`, `tags`, `segments`, `admins`, `teams`, `data_attributes` — work the same way.

**What you need to do:** if you ingest `tickets` from Intercom today, plan to remove or replace that pipeline before the 30-day window closes. There's no v2 equivalent.

### Jira

Three new tables expose the metadata behind your Jira custom fields:

- **`issue_fields`** — every field (system + custom) with its id, name, and type.
- **`issue_custom_field_contexts`** — which projects and issue types each custom field applies to.
- **`issue_custom_field_options`** — the available options for select/multi-select fields.

These are useful if you want to interpret custom field IDs in your downstream models.

### MongoDB

For very large MongoDB collections, v2 can use more memory than v1 did. The same MongoDB job may run out of memory on a small worker.

**What you need to do:** if you hit OOM:

- Raise your worker memory.
- Lower `--page-size`.
- Split the load by date with `--interval-start` / `--interval-end`.

### Notion

When using the Notion source, passing `*` as the `--source-table` value will fetch **all databases** accessible by the integration token. You can also still fetch a specific database by its ID.

```bash
# Fetch all databases
ingestr ingest --use-gong --source-uri=notion://... --source-table='*' --dest-uri=...

# Fetch a specific database by ID
ingestr ingest --use-gong --source-uri=notion://... --source-table=<database-id> --dest-uri=...
```

### Oracle

Timestamps with timezones are now correct:

- **ingestr v1:** silently dropped the timezone offset. A value like `2024-01-01 10:00:00 +05:00` was stored as `2024-01-01 10:00:00 UTC` (wrong absolute time).
- **ingestr v2:** preserves the actual UTC instant.

**What you need to do:** be aware that timestamps coming out of Oracle in non-UTC timezones will be different (and now correct) in v2. If you've built logic on top of the broken v1 behavior, you'll need to revisit it.

### Pipedrive

New tables you can now ingest:

- `activity_types`
- `files`
- `filters`
- `notes`
- `pipelines`
- `leads`
- `deals_participants`
- `deals_flow`

### RevenueCat

- The `customer_ids` table is gone.
- Customer ID information is now part of the `customers` table.

### Shopify

- The `price_rules` table is gone (deprecated in v1 already).
- Use `discounts` instead.

### Stripe

- **ingestr v1:** a Stripe run pulls your full history.
- **ingestr v2:** the default is "events-based" — only fetches things that changed in the last 30 days, and merges them into your destination table.

This is faster and what you actually want for a recurring schedule. But on the **first** run after you migrate, it means you won't have your full history.

**What you need to do:** on the first run, add `--full-refresh` to pull everything. After that, run normally:

```bash
ingestr ingest --use-gong --source-uri=stripe://... --source-table=customer --dest-uri=... --full-refresh
```

If you need to backfill more than 30 days at any point, set `--interval-start` to a date older than 30 days and v2 will fall back to the regular Stripe API.

**Affected tables:** the events-based default applies to tables that support event type filtering: `account`, `application_fee`, `charge`, `checkout_session`, `coupon`, `credit_note`, `customer`, `dispute`, `invoice`, `invoice_item`, `payment_intent`, `payment_link`, `payment_method`, `payout`, `plan`, `price`, `product`, `promotion_code`, `quote`, `refund`, `review`, `setup_intent`, `subscription`, `subscription_schedule`, `tax_rate`, `top_up`, `transfer`.

**Unaffected tables:** these continue to use a full listing and are not subject to the 30-day window: `balance_transaction`, `event`, `shipping_rate`, `apple_pay_domain`, `setup_attempt`, `subscription_item`, `tax_code`, `tax_id`, `webhook_endpoint`.

### Zendesk

`calls` and `calls_incremental` are now two separate tables. v1's `calls` was documented as incremental but secretly served the non-incremental endpoint, which was confusing. In v2:

- **`calls`** — full fetch (replace strategy).
- **`calls_incremental`** — merge by `id`, using `updated_at` as the incremental key.

**What you need to do:** decide which one you actually want and point your pipeline at it.

### Zoom

Two changes:

- **`users`** — now uses `replace` instead of `merge` by default.
- **`participants`** — no longer uses `join_time` as the incremental key. Still uses `merge` strategy.

## What's new

These don't affect existing pipelines — they're just things v2 can do that v1 couldn't.

### New sources you can ingest from

- **AWS Athena** — query S3 data lakes through Athena and load the results.
- **Apple Ads** — Apple Search Ads (iOS App Store advertising).
- **PostgreSQL CDC** — change-data-capture (logical replication) instead of full snapshots.
- **PostHog** — product analytics.
- **JobTread** — project management API.
- **G2** — G2 reviews.
- **RabbitMQ** — pull messages off a RabbitMQ queue.
- **SurveyMonkey** — surveys and responses.

### New destinations you can write to

- **DynamoDB** — write directly to a DynamoDB table.
- **Parquet** — write to local Parquet files.
- **Discard** — a "throw it away" destination, useful when you just want to test that a source works without filling up a real database.

### New strategies

On top of the v1 strategies (`replace`, `append`, `merge`, `delete+insert`), v2 adds:

- **`truncate+insert`** — empty the destination table and load the new rows in one go.
- **`scd2`** — Slowly Changing Dimension Type 2: keeps a row history per primary key (with `valid_from` / `valid_to` columns) so you can see how a record changed over time.

### CrateDB destination got better

- All strategies now supported: `replace`, `merge`, `append`, `delete+insert`, `scd2`. Previously only `replace`.

## Source readiness in v2

### Not yet in v2

- **IBM Db2** — connector is in progress. If you depend on Db2, reach out in the community channel before the grace period ends so we can help you plan around it.

### Available but not heavily tested yet

These work, but treat the first run as a smoke test and check the output before relying on it:

- Apple App Store
- Indeed
- G2
- AppsFlyer
- Personio
- PlusVibeAI

### Sources where nothing changed

If your source isn't called out anywhere above, it produces the same tables with the same per-table behavior in v1 and v2. The JSON-flattening change still applies across all sources where nested objects exist.

## A safe order to migrate

If you want to be careful about it:

1. Pick **one** non-critical pipeline. Run it on v2 — locally with `--use-gong`, or in Bruin by adding `use_gong: true` to the asset's parameters — against a test destination (or use the new `discard://` destination if you just want to see whether it works).
2. Compare row counts and a sample of rows against your v1 output.
3. Read the sections above carefully and check the results — apply any per-source steps that are relevant to your pipeline before rolling out further.
4. Add `use_gong: true` to a few more assets and let them run for a day or two. Watch for surprises.
5. Roll out to the rest of your pipelines, one batch at a time. Or just stop adding the flag and let the day-31 auto-flip do the work.

## Have questions or run into a problem?

You don't have to figure this out on your own. If something doesn't behave the way you expect, your numbers don't match v1, or you just want a sanity check before flipping a pipeline over to v2 — please reach out.

- **Fastest:** the **#ingestr** channel in the [Bruin Slack community](https://join.slack.com/t/bruindatacommunity/shared_invite/zt-2dl2i8foy-bVsuMUauHeN9M2laVm3ZVg). The maintainers and other users are there, and most migration questions get answered the same day.
- **For something the team can track:** open an issue at [github.com/bruin-data/ingestr](https://github.com/bruin-data/ingestr/issues) with the source/destination, the command you ran, and what you saw in v1 vs v2.

Don't worry about asking "small" questions — if you're unsure about something here, someone else probably is too.
