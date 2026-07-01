# Google Search Console
[Google Search Console](https://search.google.com/search-console/about) is a free service from Google that helps you monitor, maintain, and troubleshoot your site's presence in Google Search results, exposing search analytics, indexed sites, and submitted sitemaps.

ingestr supports Google Search Console as a source.

## URI format
The URI format for Google Search Console is as follows:

```plaintext
gsc://?credentials_path=/path/to/service/account.json&site_url=<site_url>
```
Alternatively, you can use base64 encoded credentials:

```plaintext
gsc://?credentials_base64=<base64_encoded_credentials>&site_url=<site_url>
```

URI parameters:
- `credentials_path`: **Optional**. The path to the service account JSON file. If omitted, the source uses [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials) (the `GOOGLE_APPLICATION_CREDENTIALS` env var, the `gcloud auth application-default login` token on your machine).
- `credentials_base64`: **Optional**. The base64-encoded service account JSON (alternative to `credentials_path`).
- `site_url`: The property to read from. Use the full URL of a URL-prefix property (e.g. `https://example.com/`) or `sc-domain:example.com` for a domain property. Multiple properties can be supplied as a comma-separated list.

To authenticate with your own Google account instead of a service account key, run `gcloud auth application-default login` and omit both credential parameters:

```plaintext
gsc://?site_url=sc-domain:example.com
```

Your account must have access to the property in Search Console, and the Search Console API must be enabled on your project.

The scheme `googlesearchconsole://` is also accepted as an alias for `gsc://`.

## Setting up a Google Search Console Integration

To connect to Google Search Console, you need to create a Google Cloud service account and grant it access to your Search Console property.

### Step 1: Create a Google Cloud Project

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select an existing one

### Step 2: Enable the Search Console API

1. In the Cloud Console, go to **APIs & Services** → **Library**
2. Search for "Google Search Console API"
3. Click on it and then click **Enable**

### Step 3: Create a Service Account

1. Go to **APIs & Services** → **Credentials**
2. Click **Create Credentials** → **Service Account**
3. Enter a name (e.g., "gsc-integration") and click **Create**
4. Skip the optional steps and click **Done**

### Step 4: Generate a JSON Key

1. Click on the service account you just created
2. Go to the **Keys** tab
3. Click **Add Key** → **Create new key**
4. Select **JSON** and click **Create** — the key file downloads automatically

### Step 5: Grant Access in Search Console

1. Open [Google Search Console](https://search.google.com/search-console)
2. Select your property and go to **Settings** → **Users and permissions**
3. Click **Add user**
4. Enter the service account email (found in your JSON file as `client_email`)
5. Select at least the **Restricted** permission and click **Add**

The JSON file path is your `credentials_path`, and your property is the `site_url` for the ingestr URI.

## Tables

Google Search Console exposes the following tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `<granularity>:<dimensions>` | site_url, date + dimensions | date | merge | Search Analytics report at the chosen time granularity (hourly or daily), grouped by the given dimensions. |
| `searchAppearance` | site_url, searchAppearance | - | replace | Search Analytics grouped by search appearance type (must be requested on its own). |
| `sites` | site_url | - | replace | The list of sites the service account can access, with permission level. |
| `sitemaps` | site_url, path | - | replace | The sitemaps submitted for each configured property. |

### Search Analytics

A Search Analytics table is named with a **time granularity** followed by an optional, comma-separated list of [dimensions](https://developers.google.com/webmaster-tools/v1/searchanalytics/query) to group by:

```plaintext
<granularity>:<dimension>,<dimension>,...
```

**Granularity** is one of the time dimensions the Search Analytics API supports:

- `hourly` — hourly buckets. Only the most recent ~10 days are available, and the API's `HOURLY_ALL` data state is used automatically.
- `daily` — daily buckets.

> The Search Console web UI also offers "Weekly" and "Monthly" views, but the API itself only returns hourly or daily data.

**Dimensions** (optional) may be any of: `country`, `device`, `page`, `query`. They are validated before the request is made.

`searchAppearance` is special: the API does not allow it alongside any other dimension, so it is exposed as its own [`searchAppearance`](#search-appearance) table rather than a dimension here.

Every Search Analytics row includes the `clicks`, `impressions`, `ctr`, and `position` metrics, plus the time bucket (`date`), the requested dimensions, and the `site_url`.

The data is aggregated by the API automatically (by page for `page` reports, by property otherwise — the default `AUTO` aggregation type). The most recent days can be preliminary and may change; re-running with an overlapping `--interval-start` / `--interval-end` updates those rows via the merge strategy. Without an interval the previous 30 days are loaded.

#### Example

```sh
ingestr ingest \
    --source-uri "gsc://?credentials_path=/path/to/service_account.json&site_url=https://example.com/" \
    --source-table "daily:query,country" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.search_analytics"
```

This command retrieves a daily report grouped by query and country and saves it to the `dest.search_analytics` table in the DuckDB database.

### Search appearance

Search appearance data cannot be combined with any other dimension, so it has its own table. It groups all data by search result feature over the selected date range:

```sh
ingestr ingest \
    --source-uri "gsc://?credentials_path=/path/to/service_account.json&site_url=https://example.com/" \
    --source-table "searchAppearance" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.search_appearance"
```

### Example: sites

```sh
ingestr ingest \
    --source-uri "gsc://?credentials_path=/path/to/service_account.json&site_url=https://example.com/" \
    --source-table "sites" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.sites"
```

## More examples

### Daily by page

```sh
ingestr ingest \
    --source-uri "gsc://?credentials_path=/path/to/service_account.json&site_url=sc-domain:example.com" \
    --source-table "daily:page" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.daily_page"
```

### Daily by device

```sh
ingestr ingest \
    --source-uri "gsc://?credentials_path=/path/to/service_account.json&site_url=sc-domain:example.com" \
    --source-table "daily:device" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.daily_device"
```

### Daily by every dimension

```sh
ingestr ingest \
    --source-uri "gsc://?credentials_path=/path/to/service_account.json&site_url=sc-domain:example.com" \
    --source-table "daily:query,page,country,device" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.daily_all"
```

### Daily with an explicit date range

The `date` column is the incremental key, so an interval loads just that window. Useful for backfills.

```sh
ingestr ingest \
    --source-uri "gsc://?credentials_path=/path/to/service_account.json&site_url=sc-domain:example.com" \
    --source-table "daily:query" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.daily_query" \
    --interval-start 2024-01-01 --interval-end 2024-03-31
```

> Search Analytics keeps roughly the last **16 months** of data and lags **2–3 days**, so pick a range inside that window.

### Hourly by query

Hourly data is only available for the most recent ~10 days, so pass a recent range. The `date` column holds an hourly timestamp.

```sh
ingestr ingest \
    --source-uri "gsc://?credentials_path=/path/to/service_account.json&site_url=sc-domain:example.com" \
    --source-table "hourly:query" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.hourly_query" \
    --interval-start 2024-05-20 --interval-end 2024-05-27
```

### Sitemaps

```sh
ingestr ingest \
    --source-uri "gsc://?credentials_path=/path/to/service_account.json&site_url=sc-domain:example.com" \
    --source-table "sitemaps" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.sitemaps"
```

### Multiple properties at once

Pass several properties (mixed Domain and URL-prefix types are fine) as a comma-separated `site_url`. Every row carries its own `site_url`, so they land in one table.

```sh
ingestr ingest \
    --source-uri "gsc://?credentials_path=/path/to/service_account.json&site_url=sc-domain:example.com,https://blog.example.com/" \
    --source-table "daily:query" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.daily_query"
```

### Authenticate with Application Default Credentials

Omit the credential parameters to use your machine's ADC (after `gcloud auth application-default login`):

```sh
ingestr ingest \
    --source-uri "gsc://?site_url=sc-domain:example.com" \
    --source-table "daily:query,country" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.daily_query_country"
```

### Authenticate with base64-encoded credentials

```sh
ingestr ingest \
    --source-uri "gsc://?credentials_base64=<base64_encoded_credentials>&site_url=sc-domain:example.com" \
    --source-table "daily:query" \
    --dest-uri "duckdb:///search_console.duckdb" \
    --dest-table "dest.daily_query"
```
