# Adjust

[Adjust](https://www.adjust.com/) is a mobile marketing analytics platform that provides solutions for measuring and optimizing campaigns, as well as protecting user data.

ingestr supports Adjust as a source.

## URI format

The URI format for Adjust is as follows:

```plaintext
adjust://?api_key=<api-key-here>
```
Parameters:
- `api_key`: Required. The API key for the Adjust account.
- `lookback_days`: Optional. The number of days to go back than the given start date for data. Defaults to 30 days.

An API token is required to retrieve reports from the Adjust reporting API. please follow the guide to [obtain an API key](https://dev.adjust.com/en/api/rs-api/authentication/).

Once you complete the guide, you should have an API key. Let's say your API key is `nr_123`, here's a sample command that will copy the data from Adjust into a DuckDB database:

```sh
ingestr ingest --source-uri 'adjust://?api_key=nr_123' --source-table 'campaigns' --dest-uri duckdb:///adjust.duckdb --dest-table 'adjust.output'
```

The result of this command will be a table in the `adjust.duckdb` database.

### Lookback days

Adjust data may change going back, which means you'll need to change your start date to get the latest data. The `lookback_days` parameter allows you to specify how many days to go back when calculating the start date, and takes care of automatically updating the start date and getting the past data as well. It defaults to 30 days.

## Tables
Adjust source allows ingesting data from various sources:

- `campaigns`: Retrieves data for a campaign, showing the app's revenue and network costs over multiple days.
- `creatives`: Retrieves data for a creative assets, detailing the app's revenue and network costs across multiple days.
- `events`: Retrieves data for [events](https://dev.adjust.com/en/api/rs-api/events/) and event slugs.
- `custom`: Retrieves custom data based on the dimensions and metrics specified.

### Custom reports: `custom:<dimensions>:<metrics>[:<filters>]`

The custom table allows you to retrieve data based on specific dimensions and metrics, and apply filters to the data.

The format for the custom table is: 
```plaintext
custom:<dimensions>:<metrics>[:<filters>]
```

Parameters:
- `dimensions`: A comma-separated list of [dimensions](https://dev.adjust.com/en/api/rs-api/reports#dimensions) to retrieve.
- `metrics`: A comma-separated list of [metrics](https://dev.adjust.com/en/api/rs-api/reports#metrics) to retrieve.
- `filters`: A comma-separated list of [filters](https://dev.adjust.com/en/api/rs-api/reports#filters) to apply to the data.
  - Parsing the `filters` key is smart enough to handle filters that contain commas inside them.

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

Copy creatives data from Adjust into a DuckDB database:
```sh
ingestr ingest \
    --source-uri 'adjust://?api_key=nr_123' \
    --source-table 'creatives' \
    --dest-uri duckdb:///adjust.duckdb \
    --dest-table 'dest.output'
```

Copy custom data from Adjust into a DuckDB database:
```sh
ingestr ingest \
    --source-uri "adjust://?api_key=nr_123&lookback_days=2" \
    --source-table "custom:hour,app,store_id,channel,os_name,country_code,campaign_network,campaign_id_network,adgroup_network, adgroup_id_network,creative_network,creative_id_network:impressions,clicks,cost,network_cost,installs,ad_revenue,all_revenue" \
    --dest-uri duckdb:///adjust.db \
    --dest-table "mat.example"
```
