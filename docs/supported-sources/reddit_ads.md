# Reddit Ads
Reddit Ads is a platform that allows businesses and marketers to create, manage, and analyze advertising campaigns on Reddit.

Ingestr supports Reddit Ads as a source.

## URI format
The URI format for Reddit Ads as a source is as follows:

```plaintext
redditads://?access_token=<access_token>&account_ids=<account_ids>
```
## URI parameters:
- `access_token`(required): The OAuth2 access token used for authentication with the Reddit Ads API. This token grants access to the advertising data associated with your Reddit Ads accounts.
- `account_ids`(required): A comma-separated list of Ad Account IDs specifying the Reddit Ad Accounts for which you want to retrieve data. These IDs uniquely identify the Reddit Ad Accounts associated with your business.

Reddit Ads requires an `access_token` and `account_ids` to retrieve data from the [Reddit Ads API v3](https://ads-api.reddit.com/docs/v3/). Please follow these steps to obtain the `access_token` and `account_ids`.

### Create a Reddit developer application to obtain an access token
1. Go to [Reddit App Preferences](https://www.reddit.com/prefs/apps) and log in with your Reddit account.
2. Click "create another app..." at the bottom of the page.
3. Fill out the form:
   - **Name**: Your application name
   - **Type**: Select "web app"
   - **Redirect URI**: A valid redirect URI for your OAuth flow (e.g., `http://localhost:8080/callback`)
4. Click "create app" and note your **client_id** (shown under the app name) and **client_secret**.

#### Authorize your app and obtain access token
1. Direct the user to the Reddit authorization URL:
   ```
   https://www.reddit.com/api/v1/authorize?client_id=<client_id>&response_type=code&state=<random_string>&redirect_uri=<redirect_uri>&duration=permanent&scope=adsread
   ```
2. After authorization, Reddit redirects to your redirect URI with a `code` parameter.
3. Exchange the code for an access token:
   ```
   POST https://www.reddit.com/api/v1/access_token
   ```
   Use HTTP Basic Auth with `client_id:client_secret` and include `grant_type=authorization_code`, `code=<code>`, and `redirect_uri=<redirect_uri>` in the request body.
4. The response includes an `access_token` and a `refresh_token`.

> [!NOTE]
> Access tokens expire after approximately 1 hour. Use the `refresh_token` with `grant_type=refresh_token` to obtain a new access token when needed.

To find the Ad Account IDs, go to the [Reddit Ads Dashboard](https://ads.reddit.com/) and navigate to your account settings. The account ID is displayed in the URL or account details.

## Tables

Reddit Ads source allows ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| [accounts](https://ads-api.reddit.com/docs/v3/) | id | - | replace | Retrieves all ad accounts accessible by the authenticated user. |
| [campaigns](https://ads-api.reddit.com/docs/v3/) | id | - | replace | Retrieves campaigns for each ad account. |
| [ad_groups](https://ads-api.reddit.com/docs/v3/) | id | - | replace | Retrieves ad groups for each ad account. |
| [ads](https://ads-api.reddit.com/docs/v3/) | id | - | replace | Retrieves ads for each ad account. |
| [posts](https://ads-api.reddit.com/docs/v3/) | id | - | replace | Retrieves ad posts (creatives) for each ad account. |
| [custom_audiences](https://ads-api.reddit.com/docs/v3/) | id | - | replace | Retrieves custom audiences for targeting. |
| [saved_audiences](https://ads-api.reddit.com/docs/v3/) | id | - | replace | Retrieves saved audience configurations. |
| [pixels](https://ads-api.reddit.com/docs/v3/) | id | - | replace | Retrieves conversion tracking pixels. |
| [funding_instruments](https://ads-api.reddit.com/docs/v3/) | id | - | replace | Retrieves funding instruments (payment methods) for each ad account. |
| [custom](https://ads-api.reddit.com/docs/v3/) | [level_id, breakdowns] | date | merge | Custom reports allow you to retrieve performance data based on specific levels, breakdowns, and metrics. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

### Example

Retrieve all campaigns:
```sh
ingestr ingest \
    --source-uri "redditads://?access_token=token_123&account_ids=id_123,id_456" \
    --source-table 'campaigns' \
    --dest-uri 'duckdb:///reddit.duckdb' \
    --dest-table 'dest.campaigns'
```

Retrieve all ad groups:
```sh
ingestr ingest \
    --source-uri "redditads://?access_token=token_123&account_ids=id_123" \
    --source-table 'ad_groups' \
    --dest-uri 'duckdb:///reddit.duckdb' \
    --dest-table 'dest.ad_groups'
```

### Custom Reports

The `custom` table uses the Reddit Ads [Reports API](https://ads-api.reddit.com/docs/v3/) to pull advertising performance reports. This allows you to retrieve metrics like impressions, clicks, and spend broken down by dimensions such as date, country, or community.

**Format:**
```
custom:<level>,<breakdowns>:<metrics>
```

**Parameters:**
- `level`(required): The first element specifies the reporting level. Must be one of: `account`, `campaign`, `ad_group`, `ad`.
- `breakdowns`(optional): Comma-separated list of breakdowns after the level. Valid breakdowns: `date`, `country`, `region`, `community`, `placement`, `device_os`, `gender`, `interest`, `keyword`, `carousel_card`. Maximum 2 breakdowns per report.
- `metrics`(required): A comma-separated list of metrics to retrieve. Common metrics include: `impressions`, `reach`, `clicks`, `spend`, `ecpm`, `ctr`, `cpc`, and various video and conversion metrics.

> [!NOTE]
> By default, ingestr fetches data from January 1, 2020 to today's date. You can specify a custom date range using the `--interval-start` and `--interval-end` parameters.

> [!NOTE]
> Monetary metrics (`spend`, `ecpm`, `cpc`) are automatically converted from microcurrency to standard currency values.

### Custom Reports Examples

Retrieve daily campaign performance data:
```sh
ingestr ingest \
    --source-uri "redditads://?access_token=token_123&account_ids=id_123,id_456" \
    --source-table 'custom:campaign,date:impressions,clicks,spend' \
    --dest-uri 'duckdb:///reddit.duckdb' \
    --dest-table 'dest.campaign_daily'
```

The applied parameters for the report are:
- level: `CAMPAIGN`
- breakdowns: `date`
- metrics: `IMPRESSIONS`, `CLICKS`, `SPEND`

Retrieve ad group performance by country for a specific date range:
```sh
ingestr ingest \
    --source-uri "redditads://?access_token=token_123&account_ids=id_123" \
    --source-table 'custom:ad_group,date,country:impressions,reach,ctr' \
    --dest-uri 'duckdb:///reddit.duckdb' \
    --dest-table 'dest.ad_group_country' \
    --interval-start '2024-10-15' \
    --interval-end '2024-12-31'
```

The applied parameters for the report are:
- level: `AD_GROUP`
- breakdowns: `date`, `country`
- metrics: `IMPRESSIONS`, `REACH`, `CTR`

Retrieve account-level spend data:
```sh
ingestr ingest \
    --source-uri "redditads://?access_token=token_123&account_ids=id_123,id_456" \
    --source-table 'custom:account,date:spend,impressions' \
    --dest-uri 'duckdb:///reddit.duckdb' \
    --dest-table 'dest.account_spend'
```

The applied parameters for the report are:
- level: `ACCOUNT`
- breakdowns: `date`
- metrics: `SPEND`, `IMPRESSIONS`

This command will retrieve data and save it to the destination table in the DuckDB database.
