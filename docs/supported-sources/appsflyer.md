# AppsFlyer

[AppsFlyer](https://www.appsflyer.com/) is a mobile marketing analytics and attribution platform that helps businesses track, measure, and optimize their app marketing efforts across various channels.

ingestr supports AppsFlyer as a source.

> [!WARNING]
> AppsFlyer uses different names for input dimensions vs their name in the output schema. For instance, in order to obtain campaign information, you need to use the `c` dimension; however, in the output schema, the resulting column will be called `campaign`.


## URI Format

The URI format for AppsFlyer is as follows:

```plaintext
appsflyer://?api_key=<api-key>
```

An API token is required to retrieve reports from the AppsFlyer API, please [follow the guide to obtain an API key](https://support.appsflyer.com/hc/en-us/articles/360004562377-Managing-AppsFlyer-tokens)

Let's say your API key is `ey123`, here's a sample command that will copy the data from AppsFlyer into a DuckDB database:

```bash
ingestr ingest \
    --source-uri 'appsflyer://?api_key=ey123' \
    --source-table 'campaigns' \
    --dest-uri duckdb:///appsflyer.duckdb \
    --dest-table 'appsflyer.output'
```

The result of this command will be a table in the `appsflyer.duckdb` database.

## Supported Tables

ingestr integrates with the [Master Report API](https://dev.appsflyer.com/hc/reference/master_api_get) of AppsFlyer, which allows you to retrieve data for the following tables:

## Tables

| Name | PK/Merge Key | Inc Key | Inc Strategy |  Details |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| [campaigns](https://dev.appsflyer.com/hc/reference/master_api_get) | install_time | install_time | merge| Retrieves data for campaigns, detailing the app's costs, loyal users, total installs, and revenue over multiple days.`columns:`  app_id, campaign, geo, install_time, average_ecpi, clicks, cohort_day_1_revenue_per_user, cohort_day_1_total_revenue_per_user, cohort_day_14_revenue_per_user, cohort_day_14_total_revenue_per_user, cohort_day_21_revenue_per_user, cohort_day_21_total_revenue_per_user, cohort_day_3_revenue_per_user, cohort_day_3_total_revenue_per_user, cohort_day_7_revenue_per_user, cohort_day_7_total_revenue_per_user, cost, impressions, installs, loyal_users, retention_day_7, revenue, roi, uninstalls |
| [creatives](https://dev.appsflyer.com/hc/reference/master_api_get) | install_time | install_time | merge| Retrieves data for a creative asset, including revenue and cost. `columns:` geo, app_id, install_time, campaign, adset_id, adset, ad_id, impressions, clicks, installs, cost, revenue, average_ecpi, loyal_users, uninstalls, roi  |
| `custom:<dimensions>:<metrics>` | Dynamic (dimensions + install_time) | install_time | merge| Retrieves data for custom tables, which can be specified by the user. Please refer to the `custom Tables` section below for more information. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

### Custom Tables

You can also ingest custom tables by providing a list of dimensions and metrics.

The format for the custom table is:

```plaintext
custom:<dimension1>,<dimension2>,<metric1>,<metric2>
```
Parameters:
- `dimensions`: A comma-separated list of [dimensions](https://support.appsflyer.com/hc/en-us/articles/213223166-Master-API-user-acquisition-metrics-via-API#groupings) to retrieve.
- `metrics`: A comma-separated list of [metrics](https://support.appsflyer.com/hc/en-us/articles/213223166-Master-API-user-acquisition-metrics-via-API#kpis) to retrieve.

For custom tables, ingestr will use the given dimensions as the primary key to deduplicate the data.

> [!NOTE]
> ingestr will add `install_time` as the primary key to the table by default if it is not provided as one of the dimensions.


 ## Examples

Copy campaigns data from Appsflyer into a DuckDB database:
```sh
ingestr ingest \
    --source-uri 'appsflyer://?api_key=ey123' \
    --source-table 'campaigns' \
    --dest-uri duckdb:///appsflyer.duckdb \
    --dest-table 'dest.output'
```

Copy creatives data from Appsflyer into a DuckDB database:
```sh
ingestr ingest \
    --source-uri 'appsflyer://?api_key=ey123' \
    --source-table 'creatives' \
    --dest-uri duckdb:///appsflyer.duckdb \
    --dest-table 'dest.output'
```

Copy custom data from Appsflyer into a DuckDB database:
```sh
ingestr ingest \
    --source-uri "appsflyer://?api_key=ey123" \
    --source-table "custom:app_id,geo,install_time,clicks,install" \
    --dest-uri duckdb:///appsflyer.db \
    --dest-table "mat.example"
```