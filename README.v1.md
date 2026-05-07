## ingestr v1 to ingestr v2 Migration Guide

This section documents the differences between ingestr v1 and ingestr v2 for each source and destination connector.

### New Destinations in ingestr v2

| Destination | Description |
|-------------|-------------|
| DynamoDB | AWS DynamoDB destination |
| Discard | Discard destination |
| Parquet | Parquet file destination |

### New Sources in ingestr v2

The following sources are new in ingestr v2 and were not available in ingestr v1:

| Source | Description |
|--------|-------------|
| Athena | AWS Athena query source |
| JobTread | JobTread project management API |
| PostgreSQL CDC | Change Data Capture for PostgreSQL |
| PostHog | PostHog analytics API |
| G2 | G2 review platform API |
| RabbitMQ | RabbitMQ message queue source |
| SurveyMonkey | SurveyMonkey survey and response API |

### Sources Not Yet Tested in ingestr v2

| Source | Status |
|--------|--------|
| Apple App Store | Not tested |
| Indeed | Not tested |
| G2 | Not tested |
| AppsFlyer | Not tested |
| Personio | Not tested |
| PlusVibeAI | Not tested |

### Sources Not Yet in ingestr v2

| Source | Status |
|--------|--------|
| IBM DB2 | In progress |

### Breaking Change: Double Underscores in Field Names

**Affected sources:** All sources, especially Airtable, Fluxx, and HubSpot.

ingestr v1 leaves double underscores (`__`) in field names as-is. ingestr v2 normalizes all consecutive underscores to a single underscore (`_`). This is a **breaking change** for users who reference column names with double underscores in downstream queries or transformations.

For example, a field like `hs__object_id`:
- **ingestr v1**: `hs__object_id`
- **ingestr v2**: `hs_object_id`

### Breaking Change: JSON Field Handling

**Affected sources:** Asana, Pipedrive, Jira, Notion, Couchbase

ingestr v1 flattens nested JSON fields into separate columns. ingestr v2 stores nested JSON fields as JSON (no flattening). This is a **breaking change** for users who rely on flattened column names in downstream queries.

For example, a nested field like `{"assignee": {"id": 123, "name": "Alice"}}`:
- **ingestr v1**: Creates columns `assignee_id` and `assignee_name`
- **ingestr v2**: Creates a single `assignee` JSON column containing `{"id": 123, "name": "Alice"}`

### Per-Source Differences

#### Chess.com

- Table names simplified by removing the `players_` prefix: `players_profiles` → `profiles`, `players_games` → `games`, `players_archives` → `archives`.
- `players_online_status` table removed — online status data (`last_online` field) is included in the `profiles` table.

#### Customer.io

- 5 new metrics tables added: `broadcast_metrics`, `broadcast_action_metrics`, `campaign_metrics`, `campaign_action_metrics`, `newsletter_metrics`.

#### Docebo

- The `polls` table has been merged into the `survey_answers` table. The code finds all learning objects of type `poll`, fetches their survey answers, and includes `poll_id` and `poll_title` fields in each row.

#### FundraiseUp

- `:incremental` variants added for all tables (not just donations): `events:incremental`, `fundraisers:incremental`, `recurring_plans:incremental`, `supporters:incremental`. All incremental tables use `created_at` as the incremental key.
- Incremental naming uses colon syntax (`donations:incremental`) instead of underscore (`donations_incremental`).

#### Google Sheets

- **Duplicate header handling:** When the header row contains duplicate names, ingestr v1 gives up on the header row entirely and renames every column to `col_1`, `col_2`, ..., `col_N`, treating the original header row as data. ingestr v2 keeps the original headers and disambiguates duplicates by appending `_2`, `_3`, etc. to the repeated names (e.g., `name`, `name_2`). Empty header cells become `column_<index>` instead of shifting every column to a generic name.

#### HubSpot

- New `property_history:<object>` tables added (e.g., `property_history:contacts`, `property_history:deals`) to track property change history for each object type.
- Custom objects now support date range filtering via the HubSpot search API. ingestr v1 did not filter by date range for custom objects, which could result in different record counts (e.g., fewer records in v2 when a date range is specified).

#### Jira

- 3 new tables added for custom field metadata:
  - `issue_fields`: Lists all issue fields (system and custom) with their IDs, names, and types.
  - `issue_custom_field_contexts`: Returns the contexts (projects/issue types) where each custom field applies.
  - `issue_custom_field_options`: Returns the available options for select/multi-select custom fields.

#### Oracle

- Improved `TIMESTAMP WITH TIME ZONE` precision handling:
  - **ingestr v1**: Drops the timezone offset, stores local time as UTC (incorrect absolute time).
  - **ingestr v2**: Uses `SYS_EXTRACT_UTC()` to correctly preserve the absolute UTC instant at the Oracle SQL level before the Go driver reads the value.
- Timestamps are now accurate when the source database stores data in non-UTC timezones.

#### Pipedrive

- New tables added: `activity_types`, `files`, `filters`, `notes`, `pipelines`, `leads`, `deals_participants`, `deals_flow`.

#### RevenueCat

- `customer_ids` table removed. Customer data is fetched directly through the `customers` table.

#### Shopify

- `price_rules` table removed (was deprecated in ingestr v1, use `discounts` table instead).

#### Zendesk

- **`calls` table note:** ingestr v1 documents the calls table as incremental (merge by `id`, `updated_at`) but the code serves the non-incremental endpoint. ingestr v2 implements both `calls` (replace strategy, full fetch) and `calls_incremental` (merge strategy with `updated_at` incremental key) as separate tables.

#### Facebook Ads

- Same tables, but different output is expected:
  - **ad_creatives**: ingestr v2 produces deterministic output. ingestr v1 may return different row orders across runs.
  - **facebook_insights**: ingestr v2 uses different primary keys (level-aware and breakdown-aware). ingestr v1 used placeholder values in PKs.

#### Zoom

- `users` strategy changed from merge to replace.
- `participants` no longer uses `join_time` as incremental key (still merge strategy).

#### Cursor

- The `daily_usage_data` table can now fetch data beyond the 30-day API limit. ingestr v2 automatically chunks requests into 30-day windows, so users can specify any date range and the source will handle pagination transparently.

#### MongoDB

- ingestr v2 may require more memory than ingestr v1 for large collections due to the Arrow-based in-memory data representation. This can cause OOM failures on memory-constrained cloud environments. Consider adjusting memory limits or using smaller batch sizes when ingesting large MongoDB collections.

### Destination Changes

#### CrateDB

**Changes:** CrateDB now supports all write strategies: `replace`, `merge`, `append`, `delete+insert`, and `scd2`. Previously only `replace` was supported.

### Sources with No Table-Level Changes

The following sources have the same tables in both ingestr v1 and ingestr v2:

Airtable, Allium, Amazon Kinesis, Amazon S3 (Blobstore), Anthropic, Apache Arrow, Applovin, Applovin Max, Asana, Attio, BigQuery, Bruin, ClickUp, Couchbase, Databricks, DuckDB, Dune, DynamoDB, Elasticsearch, Fireflies, Fluxx, Frankfurter, Freshdesk, GitHub, Google Ads, Google Analytics, Google Cloud Storage, Gorgias, Hostaway, HTTP, InfluxDB, Intercom, Isoc Pulse, Kafka, Klaviyo, Linear, Mixpanel, MongoDB, MotherDuck, MSSQL, MySQL, Notion, Phantombuster, Pinterest, PostgreSQL, Primer, QuickBooks, Redshift, Salesforce, Smartsheets, Socrata, Solidgate, Spanner, TikTok Ads, Trustpilot, Wise, CSV, JSONL, SFTP, HANA

---

## Destination Testing Status

| Destination | Test Status | Details |
|------------|------------|---------|
| postgres | Automated (integration) | Testcontainers |
| mysql | Automated (integration) | Testcontainers |
| mssql | Automated (integration) | Testcontainers |
| clickhouse | Automated (integration) | Testcontainers |
| duckdb | Automated (integration) | Testcontainers |
| sqlite | Automated (integration) | File-based |
| cratedb | Automated (integration) | Testcontainers, uses UNNEST for 6x faster writes |
| bigquery | Manual testing | Requires credentials |
| snowflake | Manual testing | Requires credentials |
| databricks | Not tested | Requires credentials |
| redshift | Not tested | Requires credentials |
| mongodb | Manual testing | |
| athena | Unit tests only | |
| blobstore (S3/GCS/Azure) | Automated (integration) | Testcontainers (MinIO) |
| csv | Automated (integration) | File-based |
| parquet | Not tested | File-based |
| trino | Not tested | |


## Source Testing Status

| Source | Test Status | Details |
|--------|------------|---------|
| **Database Sources** | | |
| postgres | Automated (integration) | Testcontainers |
| postgres_cdc | Automated (integration) | Testcontainers + unit tests |
| mysql | Automated (integration) | Testcontainers |
| mssql | Automated (integration) | Testcontainers |
| clickhouse | Automated (integration) | Testcontainers |
| duckdb | Automated (integration) | Testcontainers |
| blobstore | Automated (integration) | Testcontainers (MinIO) |
| bigquery | Manual testing | Requires credentials |
| snowflake | Not tested | Requires credentials |
| databricks | Not tested | Requires credentials |
| redshift | Not tested | Requires credentials |
| mongodb | Manual testing | |
| spanner | Manual testing + unit tests | |
| athena | Unit tests only | Config + mapper tests |
| influxdb | Manual testing + Automated (integration) | |
| oracle | Manual testing + unit| |
| hana | Manual testing | |
| elasticsearch | Automated (integration) + Manual testing | |
| couchbase | Manual testing | May add integration tests |
| sqlite | Manual testing + unit tests | File-based |
| motherduck | Manual testing | |
| trino | Manual testing + unit tests | |
| synapse | Not tested | |
| **API Sources** | | |
| fireflies | Automated (integration + unit) | |
| frankfurter | Automated (integration + unit) | |
| linear | Automated (integration + unit) | |
| shopify | Automated (integration + unit) | Free tier expired, see notes |
| stripe | Automated (integration + unit) | |
| clickup | Automated (integration) | |
| freshdesk | Automated (integration + unit) | |
| quickbooks | Automated (integration + unit) | |
| github | Automated (integration) | |
| jira | Automated (integration + unit) | Free tier, API token expires 2026-03-30 |
| monday | Automated (integration) | |
| salesforce | Automated (integration) | |
| slack | Automated (integration) | |
| zoom | Automated (integration) | See notes |
| anthropic | Manual testing | |
| cursor | Manual testing | |
| fluxx | Manual testing | |
| phantombuster | Manual testing | |
| smartsheet | Manual testing | |
| trustpilot | Manual testing | |
| applovin | Manual testing | |
| applovinmax | Manual testing + unit tests | |
| chess | Manual testing | |
| docebo | Manual testing | |
| socrata | Manual testing | |
| solidgate | Manual testing | |
| linkedin_ads | Manual testing | |
| zendesk | Manual testing + unit tests | Trial expires ~2026-03-23, see notes |
| personio | Unit tests only | Mocked, no real API calls |
| plusvibeai | Unit tests only | Mocked, no real API calls |
| primer | Unit tests only | Mocked, no real API calls |
| hubspot | Manual testing Partial | See notes |
| attio | Manual testing | |
| googleads | Manual testing | |
| google_analytics | Manual testing | |
| facebook_ads | Manual testing | See notes |
| allium | Manual testing | |
| tiktok | Manual testing | |
| snapchat_ads | Manual testing | |
| gorgias | Manual testing + Automated (integration) | |
| bruin | Automated (integration + unit) | |
| fundraiseup | Manual testing + unit tests | |
| dune | Manual testing | |
| adjust | Manual testing + unit tests | |
| revenuecat | Automated (integration + unit) | |
| appsflyer | Unit tests only | |
| mailchimp | Automated (integration + unit) | |
| mixpanel | Automated (integration + unit) | |
| appstore | Not tested | |
| kinesis | Unit tests only | Requires AWS credentials |
| intercom | Automated (integration) + Manual testing | |
| wise | Automated (integration) + Manual testing | Balances table could not be tested |
| airtable | Automated (integration) + Manual testing | |
| customer_io | Automated (integration) + Manual testing | 14-day trial |
| hostaway | Manual testing | |
| notion | Automated (integration) + Manual testing | |
| google_sheets | Manual testing | |
| http | Manual testing | |
| kafka | Manual testing | |
| indeed | Not tested | |
| g2 | Not tested | |
| jobtread | Manual testing | |
| surveymonkey | Manual testing + unit tests | |
| **File Sources** | | |
| mmap | Unit tests only | |
| json | Manual testing | |
| jsonl | Automated (integration) + Manual testing | Used in destination conformance tests |
| csv | Manual testing | |
| sftp | Manual testing | |



### Test Status Legend

- **Automated (integration)**: Has integration tests that run against real or containerized services
- **Automated (integration + unit)**: Has both integration tests and unit tests for helpers/mappers
- **Unit tests only**: Has unit tests for parsing, mapping, or helpers, but no live integration test
- **Manual testing**: Tested manually against real APIs, but no automated tests exist
- **Not tested**: No automated tests exist

### Notes

- **shopify**: Free tier expired, integration tests can no longer be run. Tests were passing before expiration. The `transactions` and `balance` tables require Shopify Payments to be enabled on the store, which was not available on the test store — these two tables are untested.
- **zoom**: The `meeting_participants` table requires the `meeting:read:registrant:admin` scope, which is only available on paid Zoom plans. This table could not be tested.
- **hubspot**: Some tables require premium HubSpot credentials. Custom tables could not be tested.
- **googleads**: Custom tables need a fix due to a primary key issue.
- **facebook_ads**: Manually tested all tables (campaigns, ad_sets, ads, ad_creatives, leads, facebook_insights). The implementations of ad_creatives, leads, and insights differ from ingestr and could not be directly compared:
  - **ad_creatives**: Uses a two-phase approach (collect IDs via /ads, batch-fetch via /?ids=) for deterministic output. ingestr's /adcreatives endpoint has non-deterministic pagination.
  - **leads**: Filters by campaign objective (OUTCOME_LEADS/LEAD_GENERATION) first to avoid unnecessary API calls. ingestr fetches all ads regardless.
  - **facebook_insights**: Uses async reporting API with level-aware and breakdown-aware primary keys. ingestr uses fixed PKs with placeholder values (no_campaign_id, etc.) and missing breakdown/custom field columns.
- **pipedrive**: 9 tables couldn't be tested (requires paid account): activity_types, files, filters, notes, pipelines, stages, leads, deals_participants, deals_flow.
- **zendesk**: 20/21 tables tested manually (trial account). `chats` table requires the Zendesk Chat add-on which is not available on trial. Trial expires ~2026-03-23.
