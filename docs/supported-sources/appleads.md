# Apple Ads

Apple Ads (Apple Search Ads) is Apple's advertising
platform for the App Store. App developers buy placements in the App Store's
Search results, Today tab, "Suggested" list, and product pages — billed per
tap or per install. Ads are rendered from the advertiser's App Store Connect
listing; there is no separate creative upload.

This source uses the [Apple Ads Platform API](https://developer.apple.com/documentation/apple-ads-platform-api)
(`api.ads.apple.com/v1`) to ingest the structural data behind those ads —
campaigns, ad groups, ads, and creatives — and their performance reports.


## URI format

```
appleads://?client_id=<client_id>&team_id=<team_id>&key_id=<key_id>&ad_account_id=<ad_account_id>&key_path=<path>
```

Or with a base64-encoded key:

```
appleads://?client_id=<client_id>&team_id=<team_id>&key_id=<key_id>&ad_account_id=<ad_account_id>&key_base64=<base64>
```

## URI parameters

- **`client_id`** *(required)* — Apple-issued API client identifier, format `SEARCHADS.<uuid>`.
- **`team_id`** *(required)* — Apple-issued team identifier, format `SEARCHADS.<uuid>`. Usually the same value as `client_id`.
- **`key_id`** *(required)* — Apple-issued key identifier for your uploaded public key (short UUID).
- **`ad_account_id`** *(required)* — Numeric ad account ID. Comma-separated for multi-account ingestion (e.g. `ad_account_id=111,222,333`).
- **`org_id`** — Accepted as an alias for `ad_account_id`, so URIs written for the v5 API keep working. For accounts migrated from v5 the ad account ID is the same number as the old organization ID.
- **`key_path`** *(one of)* — Absolute filesystem path to the EC private key PEM file (P-256).
- **`key_base64`** *(one of)* — Base64-encoded contents of the EC private key PEM file. Use when no file system access is available.


## Setup instructions

For Apple's official walkthrough of the same flow, see
[Implementing OAuth for the Apple Search Ads API](https://developer.apple.com/documentation/apple_ads/implementing-oauth-for-the-apple-search-ads-api).

### 1. Generate an EC key pair

Apple Ads uses ES256 (ECDSA P-256). Generate the pair locally — Apple never
sees your private key.

```bash
openssl ecparam -genkey -name prime256v1 -noout -out private-key.pem
openssl ec -in private-key.pem -pubout -out public-key.pem
```

### 2. Create an API user in Apple Ads

1. Sign in to [Apple Ads](https://app.searchads.apple.com/) with an account that has admin permissions.
2. Go to **Account Settings → API**.
3. Invite a new API user and assign it a role (typically **API Campaign Manager**).
4. Upload the `public-key.pem` file you generated.
5. Apple will display:
   - **Client ID** (`SEARCHADS.<uuid>`)
   - **Team ID** (`SEARCHADS.<uuid>`)
   - **Key ID** (short UUID)

Copy these three values.

### 3. Find your Ad Account ID

Ad account IDs are numeric and visible in:

- Apple Ads UI → **Settings → Overview**.
- The Apple Ads UI URL when browsing your account.


## Tables

### Entity tables

| Table | PK | Inc Key | Inc Strategy | Details |
|---|---|---|---|---|
| `campaigns` | `[orgId, id]` | `modificationTime` | merge | All ad campaigns in the ad account, including budget, status, targeting, and timeframe. Deleted campaigns are included. |
| `ad_groups` | `[orgId, id]` | `modificationTime` | merge | Targeting groups inside each campaign — bids, audience/device/geo targeting, and keyword-matching settings. |
| `ads` | `[orgId, id]` | `modificationTime` | merge | Individual ads within each ad group, linking a creative to its ad group and serving status. |
| `creatives` | `[orgId, id]` | `modificationTime` | merge | Creative assets registered to the ad account (custom product pages, text, media) that ads reference. |

### Report tables

Report tables return performance metrics (impressions, taps, installs, spend, etc.) from the Platform API's [App Store reports](https://developer.apple.com/documentation/apple-ads-platform-api/apps-reports-endpoints). Dates are UTC.

| Table | PK | Inc Key | Inc Strategy | Details |
|---|---|---|---|---|
| `campaign_reports` | `[orgId, campaignId]` | `modificationTime` | merge | Campaign-level performance metrics. |
| `ad_group_reports` | `[orgId, campaignId, adGroupId]` | `modificationTime` | merge | Ad group-level performance metrics. |
| `ad_reports` | `[orgId, campaignId, adId]` | `modificationTime` | merge | Ad-level performance metrics. |

Report table names support an optional `:granularity:groupBy` suffix to control time breakdown and dimensional grouping:

- Granularity — hourly, daily, weekly, or monthly. When set, date is added to the primary key.
- GroupBy — one or more dimensions separated by commas. Each dimension is added to the primary key. `campaign_reports` and `ad_group_reports` accept any of `countryOrRegion`, `ageRange`, `gender`, `deviceClass`, `adminArea`, `locality`, `countryCode`. `ad_reports` is restricted to `countryOrRegion` only and does not support `hourly`.


**Granularity constraints** (from [Apple docs](https://developer.apple.com/documentation/apple-ads-platform-api/appsreportingrequest)). Longer ranges are split into windows the API accepts:

| Granularity | Min date range | Max lookback |
|---|---|---|
| `hourly` | — | 30 days |
| `daily` | — | 90 days |
| `weekly` | > 14 days | 24 months |
| `monthly` | > 90 days (3 months) | 24 months |

## Example

### Basic — ingest all campaigns

```bash
ingestr ingest \
  --source-uri="appleads://?client_id=SEARCHADS.xxxx&team_id=SEARCHADS.xxxx&key_id=xxxx&ad_account_id=11111111&key_path=/path/to/private-key.pem" \
  --source-table=campaigns \
  --dest-uri="duckdb:///tmp/apple_ads.duckdb" \
  --dest-table=main.campaigns
```

### Incremental — only rows modified in the last day

```bash
ingestr ingest \
  --source-uri="appleads://?client_id=...&team_id=...&key_id=...&ad_account_id=...&key_path=..." \
  --source-table=ad_groups \
  --dest-uri="duckdb:///tmp/apple_ads.duckdb" \
  --dest-table=main.ad_groups \
  --interval-start=2026-04-21 \
  --interval-end=2026-04-22
```

ingestr will load ad groups whose `modificationTime` falls within the
`[interval-start, interval-end)` window.

### Multiple ad accounts

```bash
ingestr ingest \
  --source-uri="appleads://?client_id=...&team_id=...&key_id=...&ad_account_id=11111111,98765432,11223344&key_path=..." \
  --source-table=campaigns \
  --dest-uri="duckdb:///tmp/apple_ads.duckdb" \
  --dest-table=main.campaigns
```

All campaigns from all three ad accounts land in a single table, distinguished
by the `orgId` column (which holds the ad account ID; the name is kept so
existing tables keep merging).

### Aggregated campaign report (no granularity, no groupBy)

```bash
ingestr ingest \
  --source-uri="appleads://?client_id=...&team_id=...&key_id=...&ad_account_id=...&key_path=..." \
  --source-table=campaign_reports \
  --dest-uri="duckdb:///tmp/apple_ads.duckdb" \
  --dest-table=main.campaign_reports
```

### Daily campaign report by country

```bash
ingestr ingest \
  --source-uri="appleads://?client_id=...&team_id=...&key_id=...&ad_account_id=...&key_path=..." \
  --source-table='campaign_reports:daily:countryOrRegion' \
  --dest-uri="duckdb:///tmp/apple_ads.duckdb" \
  --dest-table=main.campaign_reports_daily_country
```

### Aggregated report grouped by gender (no time breakdown)

```bash
ingestr ingest \
  --source-uri="appleads://?client_id=...&team_id=...&key_id=...&ad_account_id=...&key_path=..." \
  --source-table='campaign_reports::gender' \
  --dest-uri="duckdb:///tmp/apple_ads.duckdb" \
  --dest-table=main.campaign_reports_gender
```

### Monthly metrics grouped by country and gender

```bash
ingestr ingest \
  --source-uri="appleads://?client_id=...&team_id=...&key_id=...&ad_account_id=...&key_path=..." \
  --source-table='campaign_reports:monthly:countryOrRegion,gender' \
  --dest-uri="duckdb:///tmp/apple_ads.duckdb" \
  --dest-table=main.campaign_reports_monthly_country_gender
```

## Notes

- **Moving from the v5 version of this source.** Apple turns off the Campaign
  Management API v5 on January 26, 2027. Table names, primary keys and the
  `orgId` column are unchanged, and `org_id` in the URI still works. What
  changes is the shape of the rows, because the Platform API returns different
  fields: for example campaigns carry `promotedObjectId`, `dailyBudget.value`
  and `targeting` instead of `adamId`, `dailyBudgetAmount` and
  `countriesOrRegions`, and report metrics use `cpt` and `cpm`
  instead of `avgCPT` and `avgCPM`. Downstream models
  that read those columns need updating.
- **Apple's `id` is not globally unique.** It is only unique within an
  ad account. Always join on the composite `[orgId, id]` when querying across
  accounts.
