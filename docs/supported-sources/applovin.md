# Applovin
[AppLovin](https://www.applovin.com/) Corporation is an American mobile technology company headquartered in Palo Alto, California. AppLovin enables developers of all sizes to market, monetize, analyze and publish their apps through its mobile advertising, marketing, and analytics platforms MAX, AppDiscovery, and SparkLabs.

`ingestr` allows ingesting data from [AppDiscovery reporting API](https://developers.applovin.com/en/app-discovery/api/reporting-api/)

## URI Format

The URI format for Applovin is as follows:
```
applovin://?api_key=<your_api_key>
```

URI Parameters:
* `api_key`. report key generated from your [applovin account](https://www.applovin.com/analytics#keys).

## Setting up Applovin Integration

### Generate a Report Key
You can generate a report key from your [analytics dashboard](https://www.applovin.com/analytics#keys).

### Example: Loading Publisher Report

For this example, we'll assume that:
* `api_key` is `api_key_0`

We will run `ingestr` to save this data to a [duckdb](https://duckdb.org/) database called `report.db` under the table name `public.publisher_report`.

```sh
ingestr ingest \
    --source-uri "applovin://?api_key=api_key_0
    --source-table "publisher-report" \
    --dest-uri "duckdb:///report.db"  \
    --dest-table "public.publisher_report" 
```

### Example: Incremental loading

We will extend the [Loading Publisher Report](#example-loading-publisher-report) example to demonstrate incremental loading.

First, we run the example with a start date of `2025-01-01` and an end date of `2025-01-05`

```sh
ingestr ingest \
    --source-uri "applovin://?api_key=api_key_0
    --source-table "publisher-report" \
    --dest-uri "duckdb:///report.db"  \
    --dest-table "public.publisher_report" \
    --interval-start "2025-01-01" \
    --interval-end "2025-01-05" 
```

We can query the database to see which dates the data was ingested for:

```sh
$ duckdb report.db 'select day from public.publisher_report group by 1'
┌────────────┐
│    day     │
│    date    │
├────────────┤
│ 2025-01-01 │
│ 2025-01-02 │
│ 2025-01-03 │
│ 2025-01-04 │
│ 2025-01-05 │
└────────────┘
```
Now, we will run `ingestr` again, but we will omit start and end date to demonstrate an incremental load.

```sh
ingestr ingest \
    --source-uri "applovin://?api_key=api_key_0
    --source-table "publisher-report" \
    --dest-uri "duckdb:///report.db"  \
    --dest-table "public.publisher_report" 
```

Now we can check the database again, and we will see that the data was loaded for the rest of the days automatically.
```sh
$ duckdb report.db 'select day from public.publisher_report group by 1'
┌────────────┐
│    day     │
│    date    │
├────────────┤
│ 2025-01-01 │
│ 2025-01-02 │
│ 2025-01-03 │
│ 2025-01-04 │
│ 2025-01-05 │
│ 2025-01-06 │
│     .      │
│     .      │
│     .      │
│ 2025-01-28 │
│ 2025-01-29 │
│ 2025-01-30 │
│ 2025-01-31 │
├────────────┤
│  35 rows   │
└────────────┘
```

## Tables

| Name | Description |
| --- | --- |
| `publisher-report` | Provides daily metrics from the `report` end point using the report_type `publisher` |
| `advertiser-report` | Provides daily metrics from the `report` end point using the report_type `advertiser`|
| `advertiser-probabilistic-report` | Provides daily metrics from the `probabilisticReport` end point using the report_type `advertiser` |
| `advertiser-ska-report` | Provides daily metrics from the `skaReport` end point using the report_type `advertiser` |

## Custom Reports

`applovin` source supports custom reports. You can pass a custom report definition to `--source-table` and it will dynamically create a report for you.

The format of a custom report looks like the following:
```
custom:{endpoint}:{report_type}:{columns}
```
Where:
* `{endpoint}` is the API endpoint for applovin reports (one of `report`, `probabilisticReport` or `skaReport`)
* `{report_type}` is the [report type](https://developers.applovin.com/en/app-discovery/api/reporting-api#required-parameters) (one of `publisher` or `advertiser`)
* `{columns}` are the [columns](https://developers.applovin.com/en/app-discovery/api/reporting-api#allowed-publisher-columns) of the given report type.


### Custom Report Example
For this example, we will ingest data from `report` end point with the report type `publisher`.

We want to obtain the following columns:
* ad_type
* clicks
* country

To achieve this, we can pass the custom report defintion in `--source-table`

```sh
ingestr ingest \
    --source-uri "applovin://?api_key=api_key_0
    --source-table "custom:report:publisher:ad_type,clicks,country" \
    --dest-uri "duckdb:///report.db"  \
    --dest-table "public.custom_report" 
```

> [!NOTE]
> The `day` column is automatically added to any custom report if it is not specified in the custom report definition.