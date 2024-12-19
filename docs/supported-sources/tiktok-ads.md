# TikTok Ads
TikTok Ads is an advertising platform that enables businesses and marketers to create, manage, and analyze ad campaigns targeting TikTok's user base.

Ingestr supports TikTok Ads as a Source.

## URI format
The URI format for TikTok Ads as a Source is as follows:

```plaintext
tiktok://?access_token=<ACCESS_TOKEN>&advertiser_id=<ADVERTISER_ID>
```
## URI parameters:
- `access_token` (required): Used for authentication and is necessary to access reports through the TikTok Marketing API.
- `advertiser_id` (required): The unique identifier for the advertiser's account, required to retrieve campaign and ad data for a specific advertiser.

TikTok requires an `access_token` and `advertiser_id` to retrieve reports from the TikTok marketing API. Please follow the guide to obtain the [credentials](https://business-api.tiktok.com/portal/docs?id=1738373141733378).

## Table: Custom Reports
Custom reports allow you to retrieve data based on specific `dimensions`, `metrics`, and `filters`.

Custom Table Format:
```
custom:<dimensions>:<metrics>:<filter_name,filter_values>
```
### Parameters:
- `dimensions`(required): A comma-separated list of [dimensions](https://business-api.tiktok.com/portal/docs?id=1751443956638721) to retrieve.
- `metrics`(required): A comma-separated list of [metrics](https://business-api.tiktok.com/portal/docs?id=1751443967255553) to retrieve.
- `filters` (optional): Filters are specified in the format `<filter_name,filter_values>`. 
    - `filter_name`: The name of the filter (e.g. campaign_ids).
    - `filter_values`: A comma-separated list of one or more values associated with the filter name (e.g., camp_id123,camp_id456). Only the `IN` filter type is supported. Learn more about [filters](https://business-api.tiktok.com/portal/docs?id=1751443975608321.). 

For example: 
```
custom:campaign_id,country_code:clicks,cpc:campaign_ids,camp_id123,camp_id456
```

- `campaign_id, country_code`: These are the `dimensions`.
- `clicks, cpc`: These are the `metrics` being retrieved.
- `campaign_ids`: This is the `filter name` being applied.
- `camp_id123, camp_id456`: These are the `filter values`.

Let’s see a `sample command` that retrieves campaign performance data from TikTok Ads and saves it into a DuckDB database.

In this example, the `access_token` is token_123, the `advertiser_id` is 0594720014, the `dimensions` are campaign_id and stat_time_day, and the `metrics` are clicks and cpc.

You can optionally specify the following parameters:

- `interval-start`: The start date for data retrieval.
- `interval-end`: The end date for data retrieval.
- `page-size`: The number of records fetched per page (default is 1000).

If these `flags` are not provided, Ingestr will fetch data for the `last 90 days` and use the default page size of `1000`.

```sh
ingestr ingest \
    --source-uri "tiktok://?access_token=token_123&advertiser_id=0594720014" \
    --source-table "custom:campaign_id,stat_time_day:clicks,cpc" \
    --dest-uri "duckdb:///campaigns.duckdb" \
    --dest-table "dest.clicks" \
    --interval-start "2024-12-06" \
    --interval-end "2024-12-12"
```
This command will retrieve data for the specified date range and save it to the `dest.clicks` table in the DuckDB database.

<img alt="titok_ads_img" src="../media/tiktok.png" />



