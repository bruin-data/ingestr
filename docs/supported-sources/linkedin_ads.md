# LinkedIn Ads
LinkedIn Ads is a platform that allows businesses and marketers to create, manage, and analyze advertising campaigns.

Ingestr supports LinkedIn Ads as a source.

## URI format
The URI format for LinkedIn Ads as a source is as follows:

```plaintext
linkedinads://?access_token=<access_token>&account_ids=<account_ids>&time_granularity=<time_granularity>"
```
## URI parameters:
- `access_token`(required): Used for authentication and is necessary to access reports through the LinkedIn Ads API.
- `account_ids`(required): The comma-separated list of account IDs to retrieve data for.
- `time_granularity`(optional): The granularity of the data to retrieve. Can be `daily` or `monthly`. By default, the data is retrieved daily.

[LinkedIn Ads](https://learn.microsoft.com/en-us/linkedin/marketing/integrations/ads-reporting/ads-reporting?view=li-lms-2024-11&tabs=http#analytics-finder) requires an `access_token`, `account_ids` and `time_granularity` to retrieve reports from the LinkedIn Ads API. Please follow the guide to obtain the [credentials](https://docs.microsoft.com/en-us/linkedin/shared/authentication/authentication).

## Table: Custom Reports    
Custom reports allow you to retrieve data based on specific dimension and metrics.

Custom Table Format:
```
custom:<dimension>:<metrics>
```
### Parameters:
- `dimension`(required): The dimension to retrieve. Can be `campaign`, `account`, `creative`.
- `metrics`(required): A comma-separated list of [metrics](https://learn.microsoft.com/en-us/linkedin/marketing/integrations/ads-reporting/ads-reporting?view=li-lms-2024-11&tabs=http#metrics-available) to retrieve.
 

> [!NOTE]
> By default, Ingestr fetches data from January 1, 2018 to the current date. You can specify a custom date range using the `interval_start` and `interval_end` parameters.

### Example

Retrieve data for campaigns with `account_ids` id_123 and id_456:
```sh
ingestr ingest \                         
    --source-uri "linkedinads://?access_token=token_123&account_ids=id_123,id_456" \
    --source-table 'custom:campaign:impressions,clicks,likes' \
    --dest-uri 'duckdb:///linkedin.duckdb' \
    --dest-table 'dest.campaign'
```

The applied parameters for the report are:
- dimension: `campaign`
- metrics: `impressions`, `clicks` and `likes`

This command will retrieve data and save it to the `dest.campaign` table in the DuckDB database.

<img alt="linkedin_ads_img" src="../media/linkedin_ads.png" />
