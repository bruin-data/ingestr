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

These resources can fetch data for a specific ad account, multiple ad accounts, or all ad accounts in the organization. All of these resources support the following formats:
- `table:ad_account_id` - fetch data for a specific ad account
- `table:id1,id2,id3` - fetch data for multiple ad accounts

| Table           | PK | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `campaigns` | id | updated_at | merge | Retrieves all campaigns for ad account(s). Supports `campaigns:ad_account_id` or `campaigns:id1,id2,id3` |
| `adsquads` | id | updated_at | merge | Retrieves all ad squads for ad account(s). Supports `adsquads:ad_account_id` or `adsquads:id1,id2,id3` |
| `ads` | id | updated_at | merge | Retrieves all ads for ad account(s). Supports `ads:ad_account_id` or `ads:id1,id2,id3` |
| `invoices` | id | updated_at | merge | Retrieves all invoices for ad account(s). Supports `invoices:ad_account_id` or `invoices:id1,id2,id3` |
| `event_details` | id | updated_at | merge | Retrieves all event details (pixel events) for ad account(s). Supports `event_details:ad_account_id` or `event_details:id1,id2,id3` |
| `creatives` | id | updated_at | merge | Retrieves all creatives for ad account(s). Supports `creatives:ad_account_id` or `creatives:id1,id2,id3` |
| `segments` | id | updated_at | merge | Retrieves all audience segments for ad account(s). Supports `segments:ad_account_id` or `segments:id1,id2,id3` |

### Stats / Measurement Data

Snapchat Ads source supports fetching stats/measurement data for campaigns, ad squads, ads, and ad accounts through dedicated stats resources.

#### Stats Resources

| Table           | Inc Strategy | Primary Key | Details                                                                                                                                        |
| --------------- | ------------------- | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `campaigns_stats` | merge | campaign_id, adsquad_id, ad_id, start_time, end_time | Retrieves stats for all campaigns. Supports breakdowns by `ad` or `adsquad` |
| `ads_stats` | merge | campaign_id, adsquad_id, ad_id, start_time, end_time | Retrieves stats for all ads. No breakdown supported (already lowest level) |
| `ad_squads_stats` | merge | campaign_id, adsquad_id, ad_id, start_time, end_time | Retrieves stats for all ad squads. Supports breakdown by `ad` |
| `ad_accounts_stats` | merge | campaign_id, adsquad_id, ad_id, start_time, end_time | Retrieves stats for all ad accounts. Supports breakdowns by `ad`, `adsquad`, or `campaign` |

**Note:** When breakdown is not specified, `adsquad_id` and `ad_id` will be `NULL` in the results.

#### Stats Table Format

```plaintext
<resource_name>:<granularity>,<fields>
<resource_name>:<breakdown>,<granularity>,<fields>
```

**Parameters:**

- `resource_name` (required): One of `campaigns_stats`, `ads_stats`, `ad_squads_stats`, `ad_accounts_stats`
- `breakdown` (optional): Object-level breakdown. Valid values depend on the resource:
  - `campaigns_stats`: `ad`, `adsquad`
  - `ad_squads_stats`: `ad`
  - `ad_accounts_stats`: `ad`, `adsquad`, `campaign`
  - `ads_stats`: No breakdown supported
- `granularity` (required): Time granularity - `TOTAL`, `DAY`, `HOUR`, or `LIFETIME`
- `fields` (required): Metrics to retrieve (comma-separated). Examples: `impressions`, `spend`, `swipes`, `conversion_purchases`, etc.

**Format Examples:**
- Without breakdown: `campaigns_stats:DAY,impressions,spend,swipes`
- With ad breakdown: `campaigns_stats:ad,HOUR,impressions,spend`
- With adsquad breakdown: `campaigns_stats:adsquad,DAY,impressions,swipes`
- Ad account stats with campaign breakdown: `ad_accounts_stats:campaign,DAY,spend`

## Examples

### Fetch campaigns for all ad accounts
```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'campaigns' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns'
```

### Fetch campaigns for specific ad accounts
```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'campaigns:account_id_1,account_id_2,account_id_3' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns'
```

### Fetch campaign stats without breakdown (hourly granularity)

```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'campaigns_stats:HOUR,impressions,spend,swipes' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns_stats' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```

Result will include `campaign_id`, but `adsquad_id` and `ad_id` will be NULL.

### Fetch campaign stats with ad breakdown

```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'campaigns_stats:ad,DAY,impressions,spend' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns_stats' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```

Result will include `campaign_id` and `ad_id`, but `adsquad_id` will be NULL.

### Fetch ad account stats with campaign breakdown

```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'ad_accounts_stats:campaign,LIFETIME,spend' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.ad_accounts_stats' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```

### Combining multiple breakdowns in the same table

Since all stats resources use the same primary key structure, you can ingest data with different breakdowns into the same table. The merge strategy will append new rows when breakdown IDs differ:

```sh
# First: ingest without breakdown
ingestr ingest \
  --source-uri 'snapchatads://...' \
  --source-table 'campaigns_stats:HOUR,impressions,spend' \
  --dest-table 'dest.campaigns_stats' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'

# Second: ingest with ad breakdown (appends to same table)
ingestr ingest \
  --source-uri 'snapchatads://...' \
  --source-table 'campaigns_stats:ad,HOUR,impressions,spend' \
  --dest-table 'dest.campaigns_stats' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```

The table will contain both campaign-level stats (where `ad_id` is NULL) and ad-level breakdown stats (where `ad_id` is populated).
