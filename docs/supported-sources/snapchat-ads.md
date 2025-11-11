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



### Fetch campaigns for all ad accounts
```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'campaigns' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns'
```

### Fetch campaigns for a specific ad account
```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret' \
  --source-table 'campaigns:22225ba982815' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns'
```

## Date Filtering

You can filter data by date range using the `interval_start` and `interval_end` parameters:

```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'campaigns' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns' \
  --interval-start '2024-01-01' \
  --interval-end '2024-01-31'
```
