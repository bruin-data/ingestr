# Criteo

[Criteo](https://www.criteo.com/) is a leading performance marketing technology company that provides personalized retargeting and customer acquisition solutions. Their Marketing Solutions API enables advertisers to access comprehensive campaign performance data.

ingestr supports Criteo as a source.

## URI format

The URI format for Criteo is as follows:

```plaintext
criteo://?client_id=<client_id>&client_secret=<client_secret>&access_token=<access_token>&currency=<currency>&advertiser_ids=<advertiser_ids>&lookback_days=<lookback_days>
```

URI parameters:

- `client_id`: Required. Your Criteo API client ID.
- `client_secret`: Required. Your Criteo API client secret.
- `access_token`: Optional. Pre-existing access token (if not provided, will use client credentials flow).
- `currency`: Optional. Currency for the report (default: USD). Supports 20+ currencies including EUR, GBP, JPY, etc.
- `advertiser_ids`: Optional. Comma-separated list of advertiser IDs to filter.
- `lookback_days`: Optional. Number of days to look back from today (default: 30).

## Setting up a Criteo Integration

To set up a Criteo integration, you need to:

1. Create a developer account at [Criteo Developer Portal](https://developers.criteo.com/)
2. Create an organization and app
3. Get your client ID and client secret

Once you have your credentials, here's a sample command that will copy campaign statistics from Criteo into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'criteo://?client_id=your_client_id&client_secret=your_client_secret' \
  --source-table 'custom:Day,AdsetId:Displays,Clicks,AdvertiserCost' \
  --dest-uri 'duckdb:///criteo.duckdb' \
  --dest-table 'criteo.campaigns'
```

## Tables

Criteo source provides flexible campaign statistics reporting through a single custom table format:

### Custom reports: `custom:<dimensions>:<metrics>`

The custom table allows you to retrieve campaign statistics based on specific dimensions and metrics you define.

The format for the custom table is:
```plaintext
custom:<dimensions>:<metrics>
```

Parameters:
- `dimensions`: A comma-separated list of dimensions to retrieve
- `metrics`: A comma-separated list of metrics to retrieve

> [!WARNING]
> Custom tables require at least one time-based dimension for efficient operation: `Hour`, `Day`, `Week`, `Month`, or `Year`.

#### Available Dimensions

- **Time**: `Hour`, `Day`, `Week`, `Month`, `Year`
- **IDs**: `AdsetId`, `CampaignId`, `AdvertiserId`, `CategoryId`, `ProductId`
- **Geography**: `Country`, `Region`, `City`
- **Technology**: `Device`, `Os`, `Browser`, `Environment`

#### Available Metrics

- **Basic**: `Displays`, `Clicks`, `AdvertiserCost`, `Ctr`, `Cpc`, `Cpm`
- **Conversions**: `PostViewConversions`, `PostClickConversions`
- **Revenue**: `Revenue`, `RevenuePostView`, `RevenuePostClick`, `SalesPostView`, `SalesPostClick`

## Features

- **Always UTC**: All timestamps are automatically converted to UTC timezone
- **Multiple Currency Support**: Support for 20+ currencies (USD, EUR, GBP, JPY, AUD, CAD, etc.)
- **Incremental Loading**: Automatic handling of incremental data updates
- **OAuth 2.0 Support**: Supports both client credentials flow and pre-existing access tokens
- **Rate Limiting**: Built-in handling of API rate limits with automatic retries
- **Flexible Reporting**: Define custom dimensions and metrics combinations

## Examples

Copy basic campaign statistics with default dimensions and metrics:
```sh
ingestr ingest \
  --source-uri 'criteo://?client_id=your_client_id&client_secret=your_client_secret' \
  --source-table 'custom:Day,AdsetId:Displays,Clicks,AdvertiserCost,Ctr,Cpc,Cpm' \
  --dest-uri 'duckdb:///criteo.duckdb' \
  --dest-table 'criteo.campaigns'
```

Copy detailed statistics with additional dimensions and EUR currency:
```sh
ingestr ingest \
  --source-uri 'criteo://?client_id=your_client_id&client_secret=your_client_secret&currency=EUR' \
  --source-table 'custom:Day,CampaignId,Country,Device:Displays,Clicks,AdvertiserCost,PostViewConversions,PostClickConversions,Revenue' \
  --dest-uri 'postgres://user:pass@localhost/db' \
  --dest-table 'criteo.detailed'
```

Copy hourly performance data:
```sh
ingestr ingest \
  --source-uri 'criteo://?client_id=your_client_id&client_secret=your_client_secret' \
  --source-table 'custom:Hour,CampaignId:Displays,Clicks,Revenue' \
  --dest-uri 'bigquery://project/dataset' \
  --dest-table 'criteo.hourly'
```

Filter by specific advertiser IDs with geographic breakdown:
```sh
ingestr ingest \
  --source-uri 'criteo://?client_id=your_client_id&client_secret=your_client_secret&advertiser_ids=12345,67890' \
  --source-table 'custom:Day,Country,Region:Displays,Clicks,AdvertiserCost' \
  --dest-uri 'snowflake://account/database/schema' \
  --dest-table 'criteo.geo_performance'
```

Weekly aggregated data with conversion metrics:
```sh
ingestr ingest \
  --source-uri 'criteo://?client_id=your_client_id&client_secret=your_client_secret&lookback_days=90' \
  --source-table 'custom:Week,CampaignId,Device:Displays,Clicks,PostViewConversions,PostClickConversions,Revenue' \
  --dest-uri 'duckdb:///criteo_weekly.db' \
  --dest-table 'weekly_conversions' \
  --interval-start '2024-01-01' \
  --interval-end '2024-03-31'
``` 