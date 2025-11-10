# Snapchat Ads

Snapchat Ads is an advertising platform that enables businesses to create, manage, and analyze ad campaigns targeting Snapchat's user base.

ingestr supports Snapchat Ads as a source using [Snapchat Marketing API](https://marketingapi.snapchat.com/docs/).

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

Please follow the [Snapchat Marketing API documentation](https://marketingapi.snapchat.com/docs/#authentication) for detailed setup instructions.

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

These resources can fetch data for a specific ad account or all ad accounts in the organization:

| Table           | PK | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `campaigns` | id | updated_at | merge | Retrieves all campaigns for ad account(s) |
| `adsquads` | id | updated_at | merge | Retrieves all ad squads for ad account(s) |
| `ads` | id | updated_at | merge | Retrieves all ads for ad account(s) |
| `invoices` | id | updated_at | merge | Retrieves all invoices for ad account(s) |
| `event_details` | id | updated_at | merge | Retrieves all event details (pixel events) for ad account(s) |
| `creatives` | id | updated_at | merge | Retrieves all creatives for ad account(s) |
| `segments` | id | updated_at | merge | Retrieves all audience segments for ad account(s) |

### Ad Account-specific Resources Usage

For ad account-level resources, you can either:

1. **Fetch data for all ad accounts** in the organization (requires `organization_id`):
```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'campaigns' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns'
```

2. **Fetch data for a specific ad account** using the format `table:ad_account_id`:
```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret' \
  --source-table 'campaigns:ad_account_id_123' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.campaigns'
```

## Examples

### Fetch organizations
```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret' \
  --source-table 'organizations' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.organizations'
```

### Fetch all ad accounts
```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'adaccounts' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.adaccounts'
```

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

### Fetch creatives for all ad accounts
```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret&organization_id=org_id' \
  --source-table 'creatives' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.creatives'
```

### Fetch audience segments for a specific ad account
```sh
ingestr ingest \
  --source-uri 'snapchatads://?refresh_token=token&client_id=id&client_secret=secret' \
  --source-table 'segments:22225ba982815' \
  --dest-uri 'duckdb:///snapchat.duckdb' \
  --dest-table 'dest.segments'
```

## Incremental Loading

Most resources support incremental loading using the `updated_at` field with a `merge` write disposition. This means that subsequent runs will only fetch records that have been updated since the last run, making the data pipeline more efficient.

Resources with `replace` disposition (`transactions`, `members`, `roles`) will completely replace the data on each run.

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

Note: The `transactions` resource supports API-side date filtering, while other resources use client-side filtering based on the `updated_at` field.
