# Adjust

[Adjust](https://www.adjust.com/) is a mobile marketing analytics platform that provides solutions for measuring and optimizing campaigns, as well as protecting user data.

ingestr supports Adjust as a source.

## URI format

The URI format for Adjust is as follows:

```plaintext
adjust://?api_key=<api-key-here>&lookback_days=40
```
Parameters:
- `api_key`: Required. The API key for the Adjust account.
- `lookback_days`: Optional. The number of days to go back than the given start date for data. Defaults to 30 days.

An API token is required to retrieve reports from the Adjust reporting API. please follow the guide to [obtain an API key](https://dev.adjust.com/en/api/rs-api/authentication/).

Once you complete the guide, you should have an API key. Let's say your API key is `nr_123`, here's a sample command that will copy the data from Adjust into a DuckDB database:

```sh
ingestr ingest --source-uri 'adjust://?api_key=nr_123' \
--source-table 'campaigns' \
--dest-uri duckdb:///adjust.duckdb \
--dest-table 'adjust.output'
```

The result of this command will be a table in the `adjust.duckdb` database.

### App Token Filtering

You can filter data for a specific app by appending `:<app_token>` to the source table name. Multiple app tokens can be separated by commas:

```sh
# Single app token
ingestr ingest --source-uri 'adjust://?api_key=nr_123' \
--source-table 'campaigns:abc123xyz' \
--dest-uri duckdb:///adjust.duckdb \
--dest-table 'adjust.output'

# Multiple app tokens
ingestr ingest --source-uri 'adjust://?api_key=nr_123' \
--source-table 'campaigns:abc123,def456' \
--dest-uri duckdb:///adjust.duckdb \
--dest-table 'adjust.output'
```

This works for `events`, `campaigns`, and `creatives` tables. For custom tables, use the `app_token__in` filter in the filters section instead (see below).

### Attribution Types

The `campaigns` and `creatives` tables default to `click,engaged_ad` attribution. You can override which attribution types are included with the `attribution_types` query parameter. Valid values are `click`, `impression`, and `engaged_ad`, comma-separated:

> [!WARNING]
> Adjust is changing its API-side default on **July 13, 2026** to include **all** attribution types (including `impression`). ingestr currently pins `click,engaged_ad` for these tables to preserve existing behavior. To keep your metrics stable regardless of Adjust's default, set `attribution_types` explicitly for the behavior you want.

```sh
# Include impressions as well
ingestr ingest --source-uri 'adjust://?api_key=nr_123' \
--source-table 'creatives?attribution_types=click,impression,engaged_ad' \
--dest-uri duckdb:///adjust.duckdb \
--dest-table 'adjust.output'

# Combined with an app token
ingestr ingest --source-uri 'adjust://?api_key=nr_123' \
--source-table 'creatives?app_token=abc123&attribution_types=click,engaged_ad' \
--dest-uri duckdb:///adjust.duckdb \
--dest-table 'adjust.output'
```

The existing `creatives:abc123` colon form (app token only) continues to work; `attribution_types` can only be set through the query parameter. For custom tables, pass `attribution_types` in the filters section instead.

### Revenue cohort metrics

The `campaigns` and `creatives` tables pull revenue by cohort window from D0 up to D120 (`all_revenue`, `ad_revenue`, and `revenue`, each at day 0, 1, 3, 7, 14, 21, 30, 60, 90, and 120). These are the cumulative (`_total_`) cohort variants defined in Adjust's [Datascape metrics glossary](https://help.adjust.com/en/article/datascape-metrics-glossary) (see also [How cohorts work](https://help.adjust.com/en/article/how-cohorts-work)); D120 is the maximum cohort day the Report Service API supports. These columns always appear in the destination with a fixed `decimal(38, 9)` type, but their values depend on the account and the age of the cohort:

- A `dN` value only fills in once the cohort is at least `N` days old; recent days report `0`/null for the longer windows and fill in over time (this is what `lookback_days` re-fetches).
- `ad_revenue_*` requires an ad-revenue integration on the account; `revenue_*`/`all_revenue_*` require purchase/revenue events. Accounts without these report `0`/null rather than dropping the column.

### Lookback days

Adjust data may change going back, which means you'll need to change your start date to get the latest data. The `lookback_days` parameter allows you to specify how many days to go back when calculating the start date, and takes care of automatically updating the start date and getting the past data as well. It defaults to 30 days.

## Tables
Adjust source allows ingesting data from various sources:

| Table           | PK/Merge Key | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| [Events](https://dev.adjust.com/en/api/rs-api/events)        | id | –  |        replace     | Retrieves data for [events](https://dev.adjust.com/en/api/rs-api/events/) and event slugs.              |                                        |
| [campaigns](https://dev.adjust.com/en/api/rs-api/reports) | day | –                | merge            | Retrieves data for a campaign, showing the app's revenue and network costs over multiple cohort windows (D0–D120). `Columns:` campaign, day, app, app_token, store_type, channel, country, installs, network_cost, and `{all_revenue,ad_revenue,revenue}_total_d{0,1,3,7,14,21,30,60,90,120}` |
| [creatives](https://dev.adjust.com/en/api/rs-api/reports)   | day | -     | merge  | Retrieves data for a creative assets, detailing the app's revenue and network costs across multiple cohort windows (D0–D120). `Columns:` campaign, day, app, app_token, store_type, channel, country, adgroup, creative, installs, network_cost, and `{all_revenue,ad_revenue,revenue}_total_d{0,1,3,7,14,21,30,60,90,120}` |
| `custom`   | `configurable` | -     | merge  | Retrieves custom data based on the dimensions and metrics specified. Please refer to the `custom reports` section below for more information.

#### Custom reports: `custom:<dimensions>:<metrics>[:<filters>]`

The custom table allows you to retrieve data based on specific dimensions and metrics, and apply filters to the data.

The format for the custom table is: 
```plaintext
custom:<dimensions>:<metrics>[:<filters>]
```

Parameters:
- `dimensions`: A comma-separated list of [dimensions](https://dev.adjust.com/en/api/rs-api/reports#dimensions) to retrieve.
- `metrics`: A comma-separated list of [metrics](https://dev.adjust.com/en/api/rs-api/reports#metrics) to retrieve.
- `filters`: A comma-separated list of [filters](https://dev.adjust.com/en/api/rs-api/reports#filters) to apply to the data. For example, `app_token__in=abc123` filters results to a specific app.

> [!WARNING]
> Custom tables require a time-based dimension for efficient operation, such as `hour`, `day`, `week`, `month`, or `year`.

 ## Examples

Copy campaigns data from Adjust into a DuckDB database:
```sh
ingestr ingest \
    --source-uri 'adjust://?api_key=nr_123' \
    --source-table 'campaigns' \
    --dest-uri duckdb:///adjust.duckdb \
    --dest-table 'dest.output'
```

Copy creatives data filtered by app token:
```sh
ingestr ingest \
    --source-uri 'adjust://?api_key=nr_123' \
    --source-table 'creatives:abc123xyz' \
    --dest-uri duckdb:///adjust.duckdb \
    --dest-table 'dest.output'
```

Copy custom data from Adjust into a DuckDB database:
```sh
ingestr ingest \
    --source-uri "adjust://?api_key=nr_123&lookback_days=2" \
    --source-table "custom:hour,app,store_id,channel,os_name,country_code,campaign_network,campaign_id_network,adgroup_network,adgroup_id_network,creative_network,creative_id_network:impressions,clicks,cost,network_cost,installs,ad_revenue,all_revenue" \
    --dest-uri duckdb:///adjust.db \
    --dest-table "mat.example"
```

Copy custom data filtered by app token:
```sh
ingestr ingest \
    --source-uri "adjust://?api_key=nr_123" \
    --source-table "custom:day,campaign,app:installs,clicks:app_token__in=abc123xyz" \
    --dest-uri duckdb:///adjust.db \
    --dest-table "mat.example"
```
