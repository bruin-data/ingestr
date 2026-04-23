# Apple Ads

Apple Ads (Apple Search Ads) is Apple's advertising
platform for the App Store. App developers buy placements in the App Store's
Search results, Today tab, "Suggested" list, and product pages — billed per
tap or per install. Ads are rendered from the advertiser's App Store Connect
listing; there is no separate creative upload.

This source uses the [Apple Ads Campaign Management API v5](https://developer.apple.com/documentation/apple_ads)
to ingest the structural data behind those ads — campaigns, ad groups, ads,
and creatives.


## URI format

```
appleads://?client_id=<client_id>&team_id=<team_id>&key_id=<key_id>&org_id=<org_id>&key_path=<path>
```

Or with a base64-encoded key:

```
appleads://?client_id=<client_id>&team_id=<team_id>&key_id=<key_id>&org_id=<org_id>&key_base64=<base64>
```

## URI parameters

- **`client_id`** *(required)* — Apple-issued API client identifier, format `SEARCHADS.<uuid>`.
- **`team_id`** *(required)* — Apple-issued team identifier, format `SEARCHADS.<uuid>`. Usually the same value as `client_id`.
- **`key_id`** *(required)* — Apple-issued key identifier for your uploaded public key (short UUID).
- **`org_id`** *(required)* — Numeric organization/account ID. Comma-separated for multi-account ingestion (e.g. `org_id=111,222,333`).
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

### 3. Find your Organization ID

Organization IDs are numeric and visible in:

- Apple Ads UI → **Settings → Overview**.
- The Apple Ads UI URL when browsing your account.


## Tables

| Table | PK | Inc Key | Inc Strategy | Details |
|---|---|---|---|---|
| `campaigns` | `[orgId, id]` | `modificationTime` | merge | All ad campaigns in the organization, including budget, status, countries/regions, and timeframe. |
| `ad_groups` | `[orgId, id]` | `modificationTime` | merge | Targeting groups inside each campaign — bids, audience/device/geo targeting, and keyword-matching settings. |
| `ads` | `[orgId, id]` | `modificationTime` | merge | Individual ads within each ad group, linking a creative to its ad group and serving status. |
| `creatives` | `[orgId, id]` | `modificationTime` | merge | Creative assets registered to the organization (custom product pages, text, media) that ads reference. |

## Example

### Basic — ingest all campaigns

```bash
ingestr ingest \
  --source-uri="appleads://?client_id=SEARCHADS.xxxx&team_id=SEARCHADS.xxxx&key_id=xxxx&org_id=11111111&key_path=/path/to/private-key.pem" \
  --source-table=campaigns \
  --dest-uri="duckdb:///tmp/apple_ads.duckdb" \
  --dest-table=main.campaigns
```

### Incremental — only rows modified in the last day

```bash
ingestr ingest \
  --source-uri="appleads://?client_id=...&team_id=...&key_id=...&org_id=...&key_path=..." \
  --source-table=ad_groups \
  --dest-uri="duckdb:///tmp/apple_ads.duckdb" \
  --dest-table=main.ad_groups \
  --interval-start=2026-04-21 \
  --interval-end=2026-04-22
```

ingestr will load ad groups whose `modificationTime` falls within the
`[interval-start, interval-end)` window.

### Multi-organization

```bash
ingestr ingest \
  --source-uri="appleads://?client_id=...&team_id=...&key_id=...&org_id=11111111,98765432,11223344&key_path=..." \
  --source-table=campaigns \
  --dest-uri="duckdb:///tmp/apple_ads.duckdb" \
  --dest-table=main.campaigns
```

All campaigns from all three organizations land in a single table, distinguished
by the `orgId` column.

## Notes

- **The [current API](https://developer.apple.com/documentation/apple_ads/apple-search-ads-campaign-management-api-5) will be sunset on January 26, 2027.**
  Apple has announced the [Apple Ads Platform API](https://developer.apple.com/documentation/apple_ads)
  as the replacement. A separate `apple_ads_platform` source may be added to
  cover that API when it becomes generally available.
- **Apple's `id` is not globally unique.** It is only unique within an
  organization. Always join on the composite `[orgId, id]` when querying across
  orgs.
