# Reddit Ads
Reddit Ads is a platform that allows businesses and marketers to create, manage, and analyze advertising campaigns on Reddit.

Ingestr supports Reddit Ads as a source.

## URI format
The URI format for Reddit Ads as a source is as follows:

```plaintext
# Recommended: refresh-token credentials (a fresh access token is minted on each run)
redditads://?client_id=<client_id>&client_secret=<client_secret>&refresh_token=<refresh_token>&account_ids=<account_ids>

# Alternative: a pre-obtained access token (expires in ~24h, so loads will fail once it lapses)
redditads://?access_token=<access_token>&account_ids=<account_ids>
```
## URI parameters:
- `account_ids`(required): A comma-separated list of Ad Account IDs specifying the Reddit Ad Accounts for which you want to retrieve data. These IDs uniquely identify the Reddit Ad Accounts associated with your business.
- `access_token`(optional): An OAuth2 access token for the Reddit Ads API. Access tokens expire (~24h), so prefer supplying `client_id` + `client_secret` + `refresh_token` instead, which lets ingestr mint a fresh access token automatically on each run.
- `client_id`(optional): Your OAuth application's client ID.
- `client_secret`(optional): Your OAuth application's client secret.
- `refresh_token`(optional): A permanent OAuth refresh token. Provide it together with `client_id` and `client_secret` to obtain a fresh access token on every run without manual re-authentication.

You must provide **either** an `access_token`, **or** `client_id` + `client_secret` + `refresh_token` (recommended). In both cases `account_ids` is required.

### Create a Reddit developer application to obtain an access token

1. Go to the [Reddit Ads Dashboard](https://ads.reddit.com/) and log in with an account that has access to your ad accounts.
2. Open **Business Manager** from the account menu, then select **Developer Applications**.
3. Click **Create a new app** and fill out the form:
   - **App name**: Your application name
   - **Redirect uri**: `http://localhost:8080/callback`
   - **Primary contact**: a business admin on the account
4. Accept the Ads API Terms and click **Create App**, then open the app to find your **client_id** (App ID) and **client_secret**.

#### Authorize your app and obtain access token
1. Direct the user to the Reddit authorization URL. Make sure to include `duration=permanent` — this is what makes Reddit return a long-lived `refresh_token`:
   ```
   https://www.reddit.com/api/v1/authorize?client_id=<client_id>&response_type=code&state=<random_string>&redirect_uri=<redirect_uri>&duration=permanent&scope=adsread
   ```
   For example:
   ```
   https://www.reddit.com/api/v1/authorize?client_id=client_123&response_type=code&state=random_xyz&redirect_uri=http://localhost:8080/callback&duration=permanent&scope=adsread
   ```
2. After authorization, Reddit redirects to your redirect URI with a `code` parameter.
3. Exchange the code for an access token:
   ```
   POST https://www.reddit.com/api/v1/access_token
   ```
   Use HTTP Basic Auth with `client_id:client_secret` and include `grant_type=authorization_code`, `code=<code>`, and `redirect_uri=<redirect_uri>` in the request body.
4. The response includes an `access_token` and a `refresh_token`. The `duration=permanent` parameter in step 1 is what makes Reddit issue the long-lived `refresh_token`.

> [!TIP]
> Access tokens expire after ~24 hours. Rather than pasting a short-lived `access_token`, supply `client_id`, `client_secret`, and the `refresh_token` in the URI — ingestr exchanges them for a fresh access token automatically on every run (`grant_type=refresh_token`), so you only authorize once.

To find the Ad Account IDs, go to the [Reddit Ads Dashboard](https://ads.reddit.com/) and navigate to your account settings. The account ID is displayed in the URL or account details.

## Tables

Reddit Ads source allows ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| accounts | id | modified_at | merge | Retrieves the ad accounts listed in `account_ids`. |
| campaigns | id | modified_at | merge | Retrieves campaigns for each ad account. |
| ad_groups | id | modified_at | merge | Retrieves ad groups for each ad account. |
| ads | id | modified_at | merge | Retrieves ads for each ad account. |
| custom_audiences | id | modified_at | merge | Retrieves custom audiences for targeting. |
| saved_audiences | id | updated_at | merge | Retrieves saved audience configurations. |
| pixels | id | modified_at | merge | Retrieves conversion tracking pixels. |
| funding_instruments | id | - | replace | Retrieves funding instruments (payment methods) for each ad account. |
| custom | [level_id, breakdowns] | date | merge | Custom reports of performance metrics by level, breakdowns, and metrics. |

Use these as `--source-table` parameter in the `ingestr ingest` command.

### Example

Retrieve all campaigns (using refresh-token credentials, recommended):
```sh
ingestr ingest \
    --source-uri "redditads://?client_id=client_123&client_secret=secret_123&refresh_token=refresh_123&account_ids=id_123,id_456" \
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
