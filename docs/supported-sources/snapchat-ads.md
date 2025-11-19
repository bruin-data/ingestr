# Snapchat Ads

Snapchat Ads is an advertising platform that enables businesses to create, manage, and analyze ad campaigns targeting Snapchat's user base.

ingestr supports Snapchat Ads as a source using [Snapchat ADS API](https://developers.snap.com/api/marketing-api/Ads-API/introduction).

## URI format

The URI format for Snapchat Ads is as follows:

```plaintext
snapchatads://?refresh_token=<refresh_token>&client_id=<client_id>&client_secret=<client_secret>&organization_id=<organization_id>
```

URI parameters:

- `refresh_token` (required): OAuth refresh token for Snapchat Marketing API authentication.
- `client_id` (required): OAuth client ID for your Snapchat Marketing API app.
- `client_secret` (required): OAuth client secret for your Snapchat Marketing API app.
- `organization_id` (optional): Organization ID. Required for most resources except `organizations`.

All parameters are used for authentication and authorization with Snapchat Marketing API.

## Setting up a Snapchat Ads Integration

To set up Snapchat Ads integration, you need to:

1. Create a Snapchat Business Account
2. Create a Snapchat Marketing API app
3. Obtain OAuth credentials (client_id, client_secret, refresh_token)
4. Get your organization_id from the Snapchat Ads Manager

Please follow the [Snapchat Ads API documentation](https://developers.snap.com/api/marketing-api/Ads-API/authentication) for detailed setup instructions.

Once you have your credentials, here's a sample command that will copy data from Snapchat Ads into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=your_token&client_id=your_client_id&client_secret=your_secret&organization_id=your_org_id' \
  --source-table 'campaigns' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns'
```

## Tables

Snapchat Ads source allows ingesting the following resources into separate tables:

### Organization-level Resources

These resources require only authentication credentials:

| Table           | PK | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `organizations` | id | updated_at | merge | Retrieves all organizations for the authenticated user |
| `fundingsources` | id | updated_at | merge | Retrieves all funding sources for the organization (requires `organization_id`) |
| `billingcenters` | id | updated_at | merge | Retrieves all billing centers for the organization (requires `organization_id`) |
| `adaccounts` | id | updated_at | merge | Retrieves all ad accounts for the organization (requires `organization_id`) |
| `transactions` | - | - | replace | Retrieves all transactions for the organization (requires `organization_id`) |
| `members` | - | - | replace | Retrieves all members of the organization (requires `organization_id`) |
| `roles` | - | - | replace | Retrieves all roles for the organization (requires `organization_id`) |

### Ad Account-level Resources

These resources can fetch data for a specific ad account or all ad accounts in the organization. All of these resources support the `table:ad_account_id` format to fetch data for a specific ad account.

| Table           | PK | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `campaigns` | id | updated_at | merge | Retrieves all campaigns for ad account(s). Supports `campaigns:ad_account_id` |
| `adsquads` | id | updated_at | merge | Retrieves all ad squads for ad account(s). Supports `adsquads:ad_account_id` |
| `ads` | id | updated_at | merge | Retrieves all ads for ad account(s). Supports `ads:ad_account_id` |
| `invoices` | id | updated_at | merge | Retrieves all invoices for ad account(s). Supports `invoices:ad_account_id` |
| `event_details` | id | updated_at | merge | Retrieves all event details (pixel events) for ad account(s). Supports `event_details:ad_account_id` |
| `creatives` | id | updated_at | merge | Retrieves all creatives for ad account(s). Supports `creatives:ad_account_id` |
| `segments` | id | updated_at | merge | Retrieves all audience segments for ad account(s). Supports `segments:ad_account_id` |

### Stats / Measurement Data

Snapchat Ads source supports fetching stats/measurement data for campaigns, ad squads, ads, and ad accounts through dedicated stats resources.

#### Stats Resources

| Table           | Inc Strategy | Details                                                                                                                                        |
| --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `campaigns_stats` | replace | Retrieves stats for all campaigns in the organization or specific ad account |
| `ads_stats` | replace | Retrieves stats for all ads in the organization or specific ad account |
| `ad_squads_stats` | replace | Retrieves stats for all ad squads in the organization or specific ad account |
| `ad_accounts_stats` | replace | Retrieves stats for all ad accounts in the organization (Note: only `spend` field is supported) |

#### Stats Table Format

```plaintext
<resource_name>:<granularity>[:<fields>][:<options>]
```

Or with specific ad account:

```plaintext
<resource_name>:<ad_account_id>:<granularity>[:<fields>][:<options>]
```

**Parameters:**

- `resource_name`: One of `campaigns_stats`, `ads_stats`, `ad_squads_stats`, `ad_accounts_stats`
- `ad_account_id` (optional): Specific ad account ID to fetch stats for
- `granularity`: Time granularity - `TOTAL`, `DAY`, `HOUR`, or `LIFETIME`
- `fields` (optional): Metrics requested (comma-separated). Default: `impressions,spend`
- `options` (optional): Additional parameters in `key=value,key=value` format

**Available Options:**

| Parameter | Description | Values |
|-----------|-------------|--------|
| `breakdown` | Object-level breakdown | `ad`, `adsquad` (Campaign only), `campaign` (Ad Account only) |
| `dimension` | Insight-level breakdown | `GEO`, `DEMO`, `INTEREST`, `DEVICE` |
| `pivot` | Pivot for insights breakdown | `country`, `region`, `dma`, `gender`, `age_bucket`, `interest_category_id`, `interest_category_name`, `operating_system`, `make`, `model` |
| `swipe_up_attribution_window` | Attribution window for swipe ups | `1_DAY`, `7_DAY`, `28_DAY` (default) |
| `view_attribution_window` | Attribution window for views | `none`, `1_HOUR`, `3_HOUR`, `6_HOUR`, `1_DAY` (default), `7_DAY`, `28_DAY` |
| `action_report_time` | Principle for conversion reporting | `conversion` (default), `impression` |
| `conversion_source_types` | Conversion source breakout by platform | `web`, `app`, `offline`, `total`, `total_off_platform`, `total_on_platform` |
| `omit_empty` | Omit records with zero data | `false` (default), `true` |
| `position_stats` | Position metric breakdown for Snap Ads | `true` |
| `test` | Return sample (fake) stats | `false` (default), `true` |

## Examples

### Fetch campaigns for all ad accounts
```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'campaigns' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns'
```

### Fetch stats for all campaigns (DAY granularity)

```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'campaigns_stats:DAY:impressions,spend,swipes' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns_stats' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```

### Fetch stats with options

```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'campaigns_stats:DAY:impressions,spend:breakdown=ad,swipe_up_attribution_window=28_DAY' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns_stats' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```

### Fetch ad account stats (LIFETIME with spend only)

```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'ad_accounts_stats:LIFETIME:spend' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.ad_accounts_stats'
```
