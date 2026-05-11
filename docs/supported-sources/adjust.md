# Adjust

[Adjust](https://www.adjust.com/) is a mobile marketing analytics platform that provides solutions for measuring and optimizing campaigns, as well as protecting user data.

ingestr supports Adjust as a source.

## URI format

The URI format for Adjust is as follows:

```plaintext
adjust://?api_key=<api-key-here>&lookback_days=40
```
Parameters:
- `api_key`: Required. The API key for the Adjust account.
- `lookback_days`: Optional. The number of days to go back than the given start date for data. Defaults to 30 days.

An API token is required to retrieve reports from the Adjust reporting API. please follow the guide to [obtain an API key](https://dev.adjust.com/en/api/rs-api/authentication/).

Once you complete the guide, you should have an API key. Let's say your API key is `nr_123`, here's a sample command that will copy the data from Adjust into a DuckDB database:

```sh
ingestr ingest --source-uri 'adjust://?api_key=nr_123' \
--source-table 'campaigns' \
--dest-uri duckdb:///adjust.duckdb \
--dest-table 'adjust.output'
```

The result of this command will be a table in the `adjust.duckdb` database.

### App Token Filtering

You can filter data for a specific app by appending `:<app_token>` to the source table name. Multiple app tokens can be separated by commas:

```sh
# Single app token
ingestr ingest --source-uri 'adjust://?api_key=nr_123' \
--source-table 'campaigns:abc123xyz' \
--dest-uri duckdb:///adjust.duckdb \
--dest-table 'adjust.output'

# Multiple app tokens
ingestr ingest --source-uri 'adjust://?api_key=nr_123' \
--source-table 'campaigns:abc123,def456' \
--dest-uri duckdb:///adjust.duckdb \
--dest-table 'adjust.output'
```

This works for `events`, `campaigns`, and `creatives` tables. For custom tables, use the `app_token__in` filter in the filters section instead (see below).

### Lookback days

Adjust data may change going back, which means you'll need to change your start date to get the latest data. The `lookback_days` parameter allows you to specify how many days to go back when calculating the start date, and takes care of automatically updating the start date and getting the past data as well. It defaults to 30 days.

## Tables
Adjust source allows ingesting data from various sources:

| Table           | PK/Merge Key | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| [Events](https://dev.adjust.com/en/api/rs-api/events)        | id | –  |        replace     | Retrieves data for [events](https://dev.adjust.com/en/api/rs-api/events/) and event slugs.              |                                        |
| [campaigns](https://dev.adjust.com/en/api/rs-api/reports) | day | –                | merge            | Retrieves data for a campaign, showing engagement, install, cost and revenue metrics over multiple days. `Dimensions:` campaign, day, app, app_token, store_type, channel, country. `Metrics:` a comprehensive set of universally-available metrics covering engagement (clicks, impressions), installs, sessions, costs, effective costs (eCPI/eCPC/eCPM), rates, non-cohort revenue, and cohort revenue/ROAS/LTV/retention/paying-user metrics for periods d0, d1, d3, d7, d14, d21, d30, d60, d90. SKAN, subscription, fraud, uninstall and ATT metrics are not requested by default — use a `custom` table to fetch those. See [Metrics reference](#metrics-reference) for the full list. |
| [creatives](https://dev.adjust.com/en/api/rs-api/reports)   | day | -     | merge  | Retrieves data for creative assets across multiple days. `Dimensions:` campaign, day, app, app_token, store_type, channel, country, adgroup, creative. `Metrics:` same default set as `campaigns` above. See [Metrics reference](#metrics-reference) for the full list. |
| `custom`   | `configurable` | -     | merge  | Retrieves custom data based on the dimensions and metrics specified. Please refer to the `custom reports` section below for more information.

#### Custom reports: `custom:<dimensions>:<metrics>[:<filters>]`

The custom table allows you to retrieve data based on specific dimensions and metrics, and apply filters to the data.

The format for the custom table is: 
```plaintext
custom:<dimensions>:<metrics>[:<filters>]
```

Parameters:
- `dimensions`: A comma-separated list of [dimensions](https://dev.adjust.com/en/api/rs-api/reports#dimensions) to retrieve.
- `metrics`: A comma-separated list of [metrics](https://dev.adjust.com/en/api/rs-api/reports#metrics) to retrieve.
- `filters`: A comma-separated list of [filters](https://dev.adjust.com/en/api/rs-api/reports#filters) to apply to the data. For example, `app_token__in=abc123` filters results to a specific app.

> [!WARNING]
> Custom tables require a time-based dimension for efficient operation, such as `hour`, `day`, `week`, `month`, or `year`.

## Metrics reference

The Adjust [Datascape metrics glossary](https://help.adjust.com/en/article/datascape-metrics-glossary) lists 1,000+ metrics. The tables below cover every metric you can request via the `metrics` parameter of a `custom` report.

### Cohorted vs non-cohorted metrics

The **Cohorted** column tells you how the metric is bucketed in time:

- **Non-cohorted (`No`)** — aggregated by **event date**. `installs` on `2026-01-10` counts installs that happened on that day, regardless of when those users were first acquired.
- **Cohorted (`Yes`)** — aggregated by **install/reinstall date**. `revenue_total_d7` on `2026-01-10` counts revenue from users *attributed on `2026-01-10`* during their first 7 days post-install.

Cohort metrics fall into two flavours:

- **Cumulative** (suffix `_total_{cohort_period}`) — sums activity from install through period N (D0..DN, W0..WN, M0..MN).
- **Non-cumulative** (suffix `_{cohort_period}` only) — measures activity that happened **strictly within** period N.

`{cohort_period}` resolves to a day (`d0`–`d120`), week (`w0`–`w52`), or month (`m0`–`m36`) suffix. `{event_slug}` is the slug of an event configured in your Adjust account.

### Similar but different metrics

Several metrics look near-identical at a glance. The distinctions:

- **`clicks` family**
  - `clicks` — clicks Adjust measured (network-reported for SANs, directly measured for non-SANs).
  - `network_clicks` — clicks reported by the ad network's API.
  - `attribution_clicks` — total clicks Adjust measured for campaigns.
  - `paid_clicks` — clicks that have associated ad-spend data attached.

- **`impressions` family** mirrors clicks: `impressions`, `network_impressions`, `attribution_impressions`, `paid_impressions`.

- **`installs` family**
  - `installs` — Adjust-attributed installs.
  - `network_installs` — installs reported by the network.
  - `paid_installs` — installs joined with ad spend.
  - `organic_installs` — installs attributed to Organic.
  - `non_organic_installs` — installs **not** attributed to Organic.
  - `network_installs_diff` / `network_installs_diff_signed` — gap between Adjust and network counts.

- **`cost` family**
  - `cost` — total ad spend (alias for `adjust_cost`).
  - `adjust_cost` — spend collected via Adjust's ad-spend engagement method.
  - `network_cost` — spend pulled from the network API.
  - `network_cost_diff` — `adjust_cost - network_cost`.

- **eCPI variants** — same idea, different denominator and numerator:
  - `ecpi_all` = `Ad Spend / Installs`
  - `network_ecpi` = `Network Ad Spend / Network Installs`
  - `ecpi` = `Network Ad Spend / Paid Installs`
  - `skad_ecpi` = `Network SKAN Ad Spend / SKAN Installs`

- **eCPM variants**
  - `ecpm` — uses attribution-side spend and paid impressions.
  - `network_ecpm` — uses network spend and network impressions.

- **Revenue trio**
  - `revenue` — in-app purchase revenue.
  - `ad_revenue` — revenue from in-app ads.
  - `all_revenue` = `revenue + ad_revenue`.
  - Each has a cohort variant (`cohort_revenue`, `cohort_ad_revenue`, `cohort_all_revenue`) aggregated by install date.

- **ROAS variants** (all cohorted)
  - `roas_iap` — ad spend payback from IAP only.
  - `roas_ad` — ad spend payback from ad revenue only.
  - `roas` — ad spend payback from all revenue.
  - Periodic variants: `roas_iap_{cohort_period}`, `roas_ad_{cohort_period}`, `roas_{cohort_period}`.

- **LTV variants** (cohorted)
  - `lifetime_value_{cohort_period}` (All), `lifetime_value_ad_{cohort_period}` (Ad-only), `lifetime_value_iap_{cohort_period}` (IAP-only).
  - Each has a `paying_user_lifetime_value_*` version that divides by paying-user size instead of total cohort size.

- **`*_total_{cohort_period}` vs `*_total_in_cohort_{cohort_period}`**
  - `_total_` — includes every user attributed within the date range, even if they haven't completed N days yet.
  - `_total_in_cohort_` — only includes users who have fully completed the N-period window. Use this for like-for-like cohort comparisons.

- **Cumulative vs non-cumulative cohort**
  - `revenue_total_d7` — cumulative IAP revenue from install through day 7.
  - `revenue_d7` — IAP revenue **on day 7 only** post-install.

- **Event metrics**
  - `events` — total count of all triggered events (non-cohort).
  - `{event_slug}_events` — count of one specific event slug (non-cohort).
  - `{event_slug}_{cohort_period}_events_cohort` — same event, cohorted by install date.
  - `{event_slug}_{cohort_period}_conversions_cohort` — count of users who triggered the event at least once.

- **Active user averages** — `daus`, `waus`, `maus` are the same idea over daily, weekly, monthly windows.

- **Paying users**
  - `paying_users_{cohort_period}` — paying users on day N.
  - `first_paying_users_{cohort_period}` — users whose first purchase happened by day N.
  - `paying_user_size_{cohort_period}` — paying users active for the entire N period.

### Conversion metrics (non-cohort)

| Metric | API ID | Definition | Formula | Cohorted |
|---|---|---|---|---|
| ATT - Authorized Users | `att_status_authorized` | Users with authorized ATT status. | — | No |
| ATT - Not Determined Users | `att_status_non_determined` | Users with not-determined ATT status. | — | No |
| ATT - Denied Users | `att_status_denied` | Users with denied ATT status. | — | No |
| ATT - Restricted Users | `att_status_restricted` | Users with restricted ATT status. | — | No |
| ATT Consent Rate | `att_consent_rate` | Consent rate among users shown the ATT prompt. | `att_status_authorized / (att_status_denied + att_status_authorized)` | No |
| Avg. DAUs | `daus` | Average unique daily active users in the timeframe. | `(D0 DAU + D1 DAU + … + DN DAU) / number of days` | No |
| Avg. WAUs | `waus` | Average unique weekly active users. | `(W0 WAU + … + WN WAU) / number of weeks` | No |
| Avg. MAUs | `maus` | Average unique monthly active users. | `(M0 MAU + … + MN MAU) / number of months` | No |
| Base Sessions | `base_sessions` | Sessions excluding installs and reattributions. | — | No |
| Clicks | `clicks` | Adjust-measured clicks (network for SANs, direct for non-SANs). | — | No |
| Clicks (Attribution) | `attribution_clicks` | Total clicks measured for campaigns. | — | No |
| Clicks (Network) | `network_clicks` | Clicks reported by the network. | — | No |
| Click Conversion Rate (CCR) | `click_conversion_rate` | Average clicks needed for a user install. | `Installs / Clicks * 100` | No |
| Click Through Rate (CTR) | `ctr` | Click rate per impression served. | `Clicks / Impressions * 100` | No |
| Deattributions | `deattributions` | Users removed from the first attribution due to a reattribution. | — | No |
| Event | `{event_slug}_events` | Non-cohorted event trigger count per period. | — | No |
| Total Events | `events` | Total count of all triggered events. | — | No |
| First Reinstalls | `first_reinstalls` | First-time reinstalls per period. Requires Uninstall/Reinstall add-on. | — | No |
| First Uninstalls | `first_uninstalls` | First-time uninstalls per period. Requires Uninstall/Reinstall add-on. | — | No |
| GDPR Forgets | `gdpr_forgets` | Users exercising the GDPR right to be forgotten. | — | No |
| Impressions | `impressions` | Ad impressions (network for SANs, direct for non-SANs). | — | No |
| Impressions (Attribution) | `attribution_impressions` | Total impressions measured for campaigns. | — | No |
| Impressions (Network) | `network_impressions` | Impressions reported by the network. | — | No |
| Impression Conversion Rate (ICR) | `impression_conversion_rate` | Install rate per impressions served. | `Installs / Impressions * 100` | No |
| Installs | `installs` | Count of app installs. | — | No |
| Installs (Network) | `network_installs` | Installs reported by the network. | — | No |
| Installs Diff (Network) | `network_installs_diff` | Absolute gap between Network and Adjust installs. | `\|network_installs - installs\|` | No |
| Installs Diff (Network) (Signed) | `network_installs_diff_signed` | Signed gap between Network and Adjust installs. | `network_installs - installs` | No |
| Installs per Mile (IPM) | `installs_per_mile` | Installs per 1,000 impressions. | `(Installs / Impressions) * 1000` | No |
| Limit Ad Tracking Installs | `limit_ad_tracking_installs` | Installs from devices with LAT enabled. | — | No |
| Limit Ad Tracking Rate | `limit_ad_tracking_install_rate` | Share of installs from LAT-enabled devices. | `limit_ad_tracking_installs / installs` | No |
| Limit Ad Tracking Reattributions | `limit_ad_tracking_reattributions` | Reattributions from LAT-enabled devices. | — | No |
| Limit Ad Tracking Reattribution Rate | `limit_ad_tracking_reattribution_rate` | Share of reattributions from LAT-enabled devices. | `limit_ad_tracking_reattributions / reattributions` | No |
| Non-Organic Installs | `non_organic_installs` | Installs not attributed to Organic. | — | No |
| Organic Installs | `organic_installs` | Installs attributed to Organic. | — | No |
| Reattribution | `reattributions` | Total reattributions that occurred. | — | No |
| Reattribution Reinstalls | `reattribution_reinstalls` | Reinstalls that resulted in a reattribution. | — | No |
| Redownload installs | `redownload_installs` | Redownload installs reported on the new attribution source. | — | No |
| Redownload deinstalls | `redownload_deinstalls` | Redownload installs reported on the previous attribution source. | — | No |
| Redownload reattributions | `redownload_reattributions` | Reattributions from redownload sessions that didn't qualify as installs. | — | No |
| Redownload sessions | `redownload_sessions` | Redownload sessions received for the app. | — | No |
| Reinstalls | `reinstalls` | Total reinstalls. Requires Uninstall/Reinstall add-on. | — | No |
| Renewals | `renewals` | Subscription renewal count. | — | No |
| Sessions | `sessions` | Sessions including installs, reinstalls and reattributions. | `base_sessions + installs + reattributions` | No |
| Uninstalls | `uninstalls` | Count of uninstalls. Requires Uninstall/Reinstall add-on. | — | No |
| Uninstalls (Cohort) | `uninstall_cohort` | Uninstalls from users that installed within the timeframe. | — | Yes |

### Ad spend metrics (non-cohort)

| Metric | API ID | Definition | Formula | Cohorted |
|---|---|---|---|---|
| Ad Spend | `cost` | Total ad spend. | `click_cost + impression_cost + install_cost + event_cost` | No |
| Ad Spend (Attribution) | `adjust_cost` | Ad spend captured via Adjust's engagement method. | `click_cost + impression_cost + install_cost + event_cost` (attribution source) | No |
| Ad Spend (Network) | `network_cost` | Ad spend pulled from the network API. | `click_cost + impression_cost + install_cost + event_cost` (network source — values may differ from attribution) | No |
| Ad Spend Diff (Network) | `network_cost_diff` | Gap between Attribution and Network ad spend. | `adjust_cost - network_cost` | No |
| Click Cost | `click_cost` | Cost of clicks. | — | No |
| Clicks (Paid) | `paid_clicks` | Clicks that have ad-spend data attached. | — | No |
| eCPI (All Installs) | `ecpi_all` | Effective cost per install across all installs. | `Ad Spend / Installs` | No |
| eCPI (Network) | `network_ecpi` | Effective cost per install from network API. | `network_cost / network_installs` | No |
| eCPI (Paid Installs) | `ecpi` | Effective cost per install on paid installs. | `network_cost / paid_installs` | No |
| eCPI (SKAdNetwork) | `skad_ecpi` | Effective cost per SKAN install. | `network_ad_spend_skan / skad_installs` | No |
| eCPM (Attribution) | `ecpm` | Effective cost per 1,000 paid impressions (attribution side). | `(cost / paid_impressions) * 1000` | No |
| eCPM (Network) | `network_ecpm` | Effective cost per 1,000 network impressions. | `(network_cost / network_impressions) * 1000` | No |
| eCPC | `ecpc` | Effective cost per click. | `cost / paid_clicks` | No |
| Event cost | `event_cost` | Cost of events. | — | No |
| Impression cost | `impression_cost` | Cost of impressions. | — | No |
| Impressions (Paid) | `paid_impressions` | Impressions with associated ad-spend data. | — | No |
| Install Cost | `install_cost` | Cost of installs. | — | No |
| Installs (Paid) | `paid_installs` | Installs with associated ad-spend data. | — | No |

### Revenue metrics

| Metric | API ID | Definition | Formula | Cohorted |
|---|---|---|---|---|
| Ad Impressions | `ad_impressions` | Count of ads served to end-users. | — | No |
| Ad Revenue | `ad_revenue` | Revenue from in-app ads. | — | No |
| Ad Revenue (Cohort) | `cohort_ad_revenue` | Cumulative ad revenue for the install/reinstall cohort. | — | Yes |
| Ad RPM | `ad_rpm` | Ad revenue per 1,000 ad impressions. | `(ad_revenue / ad_impressions) * 1000` | No |
| Revenue | `revenue` | In-app purchase revenue. | — | No |
| Revenue (Cohort) | `cohort_revenue` | Cumulative IAP revenue for the install/reinstall cohort. | — | Yes |
| All Revenue | `all_revenue` | Revenue from all sources. | `ad_revenue + revenue` | No |
| All Revenue (Cohort) | `cohort_all_revenue` | Cumulative all-source revenue for the cohort. | `cohort_revenue + cohort_ad_revenue` | Yes |
| ARPDAU (All) | `arpdau` | Average revenue per daily active user (all sources). | `all_revenue_total / total DAU` | No |
| ARPDAU (Ad) | `arpdau_ad` | Average ad revenue per daily active user. | `ad_revenue / total DAU` | No |
| ARPDAU (IAP) | `arpdau_iap` | Average IAP revenue per daily active user. | `revenue / total DAU` | No |
| Gross profit | `gross_profit` | Revenue minus ad spend. | `all_revenue - cost` | No |
| Gross profit (Cohort) | `cohort_gross_profit` | Cohorted gross profit. | `cohort_revenue - cost` | Yes |
| Return On Investment (ROI) | `return_on_investment` | Cohorted gross profit divided by ad spend. | `cohort_gross_profit / cost` | Yes |
| Revenue Events | `revenue_events` | Revenue-generating events triggered. | — | No |
| Revenue To Cost Ratio (RCR) | `revenue_to_cost` | Revenue-to-cost ratio. | `cohort_revenue / cost` | Yes |
| ROAS (All Revenue) | `roas` | Return on ad spend (all revenue). | `(cohort_revenue + cohort_ad_revenue) / cost` | Yes |
| ROAS (Ad Revenue) | `roas_ad` | Return on ad spend (ad revenue only). | `cohort_ad_revenue / cost` | Yes |
| ROAS (IAP Revenue) | `roas_iap` | Return on ad spend (IAP only). | `cohort_revenue / cost` | Yes |

### Cumulative cohort metrics

These metrics use the suffix `_total_{cohort_period}`, where `{cohort_period}` is one of `d0`–`d120`, `w0`–`w52`, or `m0`–`m36`. They sum activity from install through period N.

| Metric | API ID | Definition | Formula | Cohorted |
|---|---|---|---|---|
| N days Ad Impressions Total | `ad_impressions_total_{cohort_period}` | Cumulative ads served to the cohort through N. | — | Yes |
| N days Ad Impressions Total in Cohort | `ad_impressions_total_in_cohort_{cohort_period}` | Same, only counting users who completed the full N period. | — | Yes |
| N days Event (Conversions) | `{event_slug}_{cohort_period}_conversions_cohort` | Users completing the event by day/week/month N post-install. | — | Yes |
| N days Event (Events) | `{event_slug}_{cohort_period}_events_cohort` | Event count by day/week/month N post-install. | — | Yes |
| N days Event (Revenue) | `{event_slug}_{cohort_period}_revenue_cohort` | IAP revenue from the event N days/weeks/months post-install. | — | Yes |
| N days Event (Converted User Size) | `{event_slug}_{cohort_period}_converted_user_size_cohort` | Users who converted on the event and completed N+ periods. | — | Yes |
| N days Events per Conversion (Events) | `{event_slug}_{cohort_period}_events_per_conversion_cohort` | Events per converting user. | `events_cohort / conversions_cohort` | Yes |
| N days Events per Conversion (Revenue) | `{event_slug}_{cohort_period}_revenue_per_conversion_cohort` | Revenue per converting user. | `revenue_cohort / conversions_cohort` | Yes |
| N days Event (Event Rate) | `{event_slug}_{cohort_period}_events_rate_cohort` | Events divided by cohort size. | `events_cohort / cohort_size` | Yes |
| N days Event (Conversions Rate) | `{event_slug}_{cohort_period}_conversions_rate_cohort` | Conversions divided by cohort size. | `conversions_cohort / cohort_size` | Yes |
| N days Revenue Total | `revenue_total_{cohort_period}` | Cumulative IAP revenue for the cohort. | — | Yes |
| N days Revenue Total Per User | `revenue_total_per_user_{cohort_period}` | Cumulative revenue per user. | `revenue_total / cohort_size` | Yes |
| N days Revenue Total Per Paying User | `revenue_total_per_paying_user_{cohort_period}` | Cumulative revenue per paying user. | `revenue_total / first_paying_users_total` | Yes |
| N days Revenue Total In Cohort | `revenue_total_in_cohort_{cohort_period}` | Revenue from users who fully completed the N period. | — | Yes |
| N days Revenue Events Total | `revenue_events_total_{cohort_period}` | Cumulative count of revenue events. | — | Yes |
| N days Revenue Events Total in Cohort | `revenue_events_total_in_cohort_{cohort_period}` | Same, only completed-N users. | — | Yes |
| N days Revenue Events Total per Paying User | `revenue_events_total_per_paying_user_{cohort_period}` | Revenue events per paying user. | `revenue_events_total_in_cohort / first_paying_users_total` | Yes |
| N days Ad Revenue Total | `ad_revenue_total_{cohort_period}` | Cumulative ad revenue for the cohort. | — | Yes |
| N days Ad Revenue Total Per User | `ad_revenue_total_per_user_{cohort_period}` | Cumulative ad revenue per user. | `ad_revenue_total / cohort_size` | Yes |
| N days Ad Revenue Total in Cohort | `ad_revenue_total_in_cohort_{cohort_period}` | Ad revenue from users who completed N periods. | — | Yes |
| N days All Revenue Total | `all_revenue_total_{cohort_period}` | Cumulative all-source revenue. | `ad_revenue_total + revenue_total` | Yes |
| N days All Revenue Total Per User | `all_revenue_total_per_user_{cohort_period}` | Cumulative all-source revenue per user. | `all_revenue_total / cohort_size` | Yes |
| N days All Revenue Total in Cohort | `all_revenue_total_in_cohort_{cohort_period}` | All-source revenue from completed-N users. | `ad_revenue_total_in_cohort + revenue_total_in_cohort` | Yes |
| N days LTV (Ad) All Users | `lifetime_value_ad_{cohort_period}` | Lifetime value (ad revenue, all users). | `ad_revenue_total_in_cohort / cohort_size` | Yes |
| N days LTV (Ad) Paying Users | `paying_user_lifetime_value_ad_{cohort_period}` | Lifetime value per paying user (ad revenue). | `ad_revenue_total_in_cohort / paying_user_size` | Yes |
| N days LTV (All) All Users | `lifetime_value_{cohort_period}` | Lifetime value (all revenue, all users). | `all_revenue_total_in_cohort / cohort_size` | Yes |
| N days LTV (All) Paying Users | `paying_user_lifetime_value_{cohort_period}` | Lifetime value per paying user (all revenue). | `all_revenue_total_in_cohort / paying_user_size` | Yes |
| N days LTV (IAP) All Users | `lifetime_value_iap_{cohort_period}` | Lifetime value (IAP, all users). | `revenue_total_in_cohort / cohort_size` | Yes |
| N days LTV (IAP) Paying Users | `paying_user_lifetime_value_iap_{cohort_period}` | Lifetime value per paying user (IAP). | `revenue_total_in_cohort / paying_user_size` | Yes |
| N days ROAS (Ad Revenue) | `roas_ad_{cohort_period}` | Cohorted ad-revenue ROAS. | `ad_revenue_total / cost` | Yes |
| N days ROAS (All Revenue) | `roas_{cohort_period}` | Cohorted all-revenue ROAS. | `all_revenue_total / cost` | Yes |
| N days ROAS (IAP Revenue) | `roas_iap_{cohort_period}` | Cohorted IAP-revenue ROAS. | `revenue_total / cost` | Yes |
| N days Total Paying Users | `first_paying_users_total_{cohort_period}` | Cumulative users with at least one purchase. | — | Yes |
| N days Paying Users Conversion Rate Total | `cumulative_paying_users_conversion_rate_{cohort_period}` | Cumulative paying-user conversion rate. | `first_paying_users_total / cohort_size` | Yes |
| N days First Reinstalls Total | `first_reinstalls_total_{cohort_period}` | Cumulative first-time reinstalls. | — | Yes |
| N days Reinstalls Total | `reinstalls_total_{cohort_period}` | Cumulative reinstalls. | — | Yes |
| N days First Uninstalls Total | `first_uninstalls_total_{cohort_period}` | Cumulative first-time uninstalls. | — | Yes |
| N days Uninstalls Total | `uninstalls_total_{cohort_period}` | Cumulative uninstalls. | — | Yes |
| N days GDPR Forget Users Total | `gdpr_forgets_total_{cohort_period}` | Cumulative GDPR forgets. | — | Yes |

### Non-cumulative cohort metrics

These metrics use the suffix `_{cohort_period}` (no `_total`). They measure activity that happened **strictly within** period N.

| Metric | API ID | Definition | Formula | Cohorted |
|---|---|---|---|---|
| N days Ad Impressions | `ad_impressions_{cohort_period}` | Ads served during period N. | — | Yes |
| N days Ad Revenue | `ad_revenue_{cohort_period}` | Ad revenue during period N. | — | Yes |
| N days Ad RPM | `ad_rpm_{cohort_period}` | Ad RPM during period N. | `(ad_revenue / ad_impressions) * 1000` | Yes |
| N days All Revenue | `all_revenue_{cohort_period}` | All-source revenue during period N. | — | Yes |
| N days All Revenue Per User | `all_revenue_per_user_{cohort_period}` | All-source revenue per user during period N. | `all_revenue / cohort_size` | Yes |
| N days Cohort Size | `cohort_size_{cohort_period}` | Users attributed for at least N days/weeks/months. | — | Yes |
| N days Deattributions | `deattributions_{cohort_period}` | Deattributions during period N. | — | Yes |
| N days Cost per Event (Events) | `{event_slug}_{cohort_period}_events_cost_cohort` | Ad spend per event count. | `cost / events_cohort` | Yes |
| N days Cost per Event (Conversions) | `{event_slug}_{cohort_period}_conversions_cost_cohort` | Ad spend per converting user. | `cost / conversions_cohort` | Yes |
| N days Cost Per Paying User | `cost_per_paying_user_{cohort_period}` | Ad spend per paying user during N. | `cost / first_paying_users_total` | Yes |
| N days Deattributions Per User | `deattributions_per_user_{cohort_period}` | Deattributions per user during N. | `deattributions / cohort_size` | Yes |
| N days Event (Events per Period) | `{event_slug}_{cohort_period}_events_per_period` | Event triggers during N. | — | Yes |
| N days Event (Revenue per Period) | `{event_slug}_{cohort_period}_revenue_per_period` | IAP revenue from event during N. | — | Yes |
| N days First Reinstalls | `first_reinstalls_{cohort_period}` | First-time reinstalls during N. | — | Yes |
| N days First Uninstalls | `first_uninstalls_{cohort_period}` | First-time uninstalls during N. | — | Yes |
| N days GDPR Forgets | `gdpr_forgets_{cohort_period}` | GDPR forgets during N. | — | Yes |
| N days Non-Install Sessions | `non_install_sessions_{cohort_period}` | Non-install sessions during N. | — | Yes |
| N days First-Time Paying Users | `first_paying_users_{cohort_period}` | First-time paying users during N. | — | Yes |
| N days First-Time Paying Users Rate | `first_time_paying_user_conversion_rate_{cohort_period}` | First-time paying-user share of retained users. | `first_paying_users / retained_users` | Yes |
| N days Paying User Size | `paying_user_size_{cohort_period}` | Paying users active for the full N period. | — | Yes |
| N days Paying User Conversion Rate | `paying_user_conversion_rate_{cohort_period}` | Cohort share that became paying users on day N. | `first_paying_users / cohort_size` | Yes |
| N days Paying Users | `paying_users_{cohort_period}` | Users with an in-app purchase on day N. | — | Yes |
| N days Paying Users Rate | `paying_user_rate_{cohort_period}` | Paying users as a share of cohort. | `paying_users / cohort_size` | Yes |
| N days Reattributions | `reattributions_{cohort_period}` | Reattributions during N. | — | Yes |
| N days Reattributions per Deattribution | `reattributions_per_deattribution_{cohort_period}` | Reattribution-to-deattribution ratio. | `reattributions / deattributions` | Yes |
| N days Reattributions Per User | `reattributions_per_user_{cohort_period}` | Reattributions per user. | `reattributions / cohort_size` | Yes |
| N days Reinstalls | `reinstalls_{cohort_period}` | Reinstalls during N. | — | Yes |
| N days Retained Users | `retained_users_{cohort_period}` | Users active through period N. | — | Yes |
| N days Retention Rate All Users | `retention_rate_{cohort_period}` | Retained users as a share of cohort. | `retained_users / cohort_size` | Yes |
| N days Retention Rate Paying Users | `paying_users_retention_rate_{cohort_period}` | Paying-user retention rate. | `paying_users / retained_users` | Yes |
| N days Revenue | `revenue_{cohort_period}` | IAP revenue during N. | — | Yes |
| N days Revenue Events | `revenue_events_{cohort_period}` | Revenue events during N. | — | Yes |
| N days Revenue Events Per Active User | `revenue_events_per_active_user_{cohort_period}` | Revenue events per retained user. | `revenue_events / retained_users` | Yes |
| N days Revenue Events Per Paying User | `revenue_events_per_paying_user_{cohort_period}` | Revenue events per paying user. | `revenue_events / paying_users` | Yes |
| N days Revenue Events Per User | `revenue_events_per_user_{cohort_period}` | Revenue events per user. | `revenue_events / cohort_size` | Yes |
| N days Revenue Per Paying User | `revenue_per_paying_user_{cohort_period}` | IAP revenue per paying user. | `revenue / paying_users` | Yes |
| N days Revenue Per User | `revenue_per_user_{cohort_period}` | IAP revenue per user. | `revenue / cohort_size` | Yes |
| N days Sessions | `sessions_{cohort_period}` | Sessions during N. | — | Yes |
| N days Sessions Per User | `sessions_per_user_{cohort_period}` | Sessions per user during N. | `sessions / cohort_size` | Yes |
| N days Time Spent | `time_spent_{cohort_period}` | Seconds spent in app during N. | — | Yes |
| N days Time Spent Per Active User | `time_spent_per_active_user_{cohort_period}` | Seconds per active user. | `time_spent / retained_users` | Yes |
| N days Time Spent Per Session | `time_spent_per_session_{cohort_period}` | Seconds per session. | `time_spent / non_install_sessions` | Yes |
| N days Time Spent Per User | `time_spent_per_user_{cohort_period}` | Seconds per user. | `time_spent / cohort_size` | Yes |
| N days Uninstalls | `uninstalls_{cohort_period}` | Uninstalls during N. | — | Yes |

### SKAdNetwork metrics (non-cohort)

| Metric | API ID | Definition | Formula | Cohorted |
|---|---|---|---|---|
| Conversion Bit 1–6 (SKAN) | `conversion_1` … `conversion_6` | SKAN postbacks where conversion event N triggered. | — | No |
| Conversion Value 0 (SKAN) | `conversion_value_0` | SKAN postbacks with conversion value 0 (install). | — | No |
| Conversion Value 1–63 (SKAN) | `conversion_value_1` … `conversion_value_63` | SKAN postbacks with the given conversion value. | — | No |
| Conversion Value greater than 0 (SKAN) | `skad_conversion_value_gt_0` | SKAN postbacks with conversion value > 0. | `valid_conversions - conversion_value_0` | No |
| Conversion Value Null (SKAN) | `skad_conversion_value_null` | SKAN postbacks with null conversion value. | `(skad_installs + skad_reinstalls) - valid_conversions` | No |
| Conversion Value Total (SKAN) | `conversion_value_total` | Sum of all conversion values. | `Σ conversion_value_n * n` | No |
| Conversion Value Null Rate (SKAN) | `skad_conversion_value_null_rate` | Null conversion-value rate. | `skad_conversion_value_null / skad_total_installs` | No |
| Coarse conversion value (1st/2nd/3rd Postback × null/none/low/medium/high) | `skad_coarse_conversion_values_{null,none,low,medium,high}_{0,1,2}` | Coarse conversion value counts per postback. | — | No |
| Event eCR (SKAN) Min/Avg/Max | `{event_slug}_skan_event_ecr_{min,est,max}` | Effective event conversion rate from SKAN. | `events_{min,est,max} / valid_conversions` | No |
| Installs (SKAN) | `skad_installs` | Valid SKAN postbacks with `redownload=false`. | — | No |
| Qualifiers (SKAN) | `skad_qualifiers` | Touchpoints that didn't win SKAN attribution. | — | No |
| Invalid Payloads (SKAN) | `invalid_payloads` | SKAN postbacks with invalid attribution signature. | — | No |
| Reinstalls (SKAN) | `skad_reinstalls` | Valid SKAN postbacks with `redownload=true`. | — | No |
| Total conversions (SKAN) | `skad_total_installs` | Installs + reinstalls from SKAN. | `skad_installs + skad_reinstalls` | No |
| Valid Conversions (SKAN) | `valid_conversions` | SKAN postbacks with valid conversion value. | `skad_total_installs - skad_conversion_value_null` | No |
| eCPA (SKAN) | `{event_slug}_skan_ecpa` | Effective cost per action for an event. | `(network_ad_spend_skan * valid_conversions) / (event_total * skad_installs)` | No |
| Ad Spend (SKAN) | `network_ad_spend_skan` | Ad spend on SKAN campaigns from network API. | — | No |
| ROAS (SKAN) Min/Avg/Max | `skad_revenue_{min,est,max}_roas` | ROAS using SKAN revenue ranges. | `(skan_total_revenue * skad_installs) / (network_ad_spend_skan * valid_conversions)` | No |
| ROI (SKAN) Min/Avg/Max | `skad_revenue_{min,est,max}_roi` | ROI using SKAN revenue ranges. | `(skan_revenue * skad_installs) / (network_ad_spend_skan * valid_conversions) - 1` | No |
| RPU - Ad Rev (SKAN) Min/Avg/Max | `skan_ad_rpu_{min,est,max}` | Ad RPU from SKAN. | `skan_ad_revenue_{min,est,max} / valid_conversions` | No |
| RPU - IAP (SKAN) Min/Avg/Max | `skan_iap_rpu_{min,est,max}` | IAP RPU from SKAN. | — | No |
| Event RPU (SKAN) Min/Avg/Max | `{event_slug}_skan_event_rpu_{min,est,max}` | Event-level RPU from SKAN. | `revenue_{min,est,max} / valid_conversions` | No |
| Total RPU (SKAN) Min/Avg/Max | `skan_total_rpu_{min,est,max}` | Total RPU from SKAN. | `skan_total_revenue_{min,est,max} / valid_conversions` | No |
| Ad Revenue (SKAN) Min/Avg/Max | `skad_ad_revenue_{min,est,max}` | Ad revenue from SKAN ranges. | — | No |
| In-App Revenue (SKAN) Min/Avg/Max | `iap_revenue_revenue_{min,est,max}` | IAP revenue from SKAN ranges. | — | No |
| Total Revenue (SKAN) Min/Avg/Max | `skan_total_revenue_{min,est,max}` | Total SKAN revenue from buckets. | — | No |
| Event (SKAN) Min/Avg/Max | `{event_slug}_events_{min,est,max}` | Event count from SKAN postbacks. | — | No |
| Event Revenue (SKAN) Min/Avg/Max | `{event_slug}_revenue_{min,est,max}` | Event revenue from SKAN postbacks. | — | No |
| Total Revenue Events (SKAN) Min/Avg/Max | `revenue_events_{min,est,max}` | Total SKAN revenue events. | — | No |
| Direct Total Installs (SKAN) | `skad_direct_total_installs` | Direct installs + reinstalls. | `skad_direct_installs + skad_direct_reinstalls` | No |
| Direct Installs (SKAN) | `skad_direct_installs` | Direct SKAN postbacks where `redownload=false`. | — | No |
| Direct Reinstalls (SKAN) | `skad_direct_reinstalls` | Direct SKAN postbacks where `redownload=true`. | — | No |
| Direct Invalid Payloads (SKAN) | `skad_direct_invalid_payloads` | Direct SKAN postbacks with invalid signature. | — | No |
| Direct Valid Conversions (SKAN) | `skad_direct_valid_conversions` | Direct SKAN postbacks with valid conversion value. | — | No |
| Direct Conversion Value Null (SKAN) | `skad_direct_conversion_value_null` | Direct SKAN postbacks with null conversion value. | `(skad_direct_installs + skad_direct_reinstalls) - skad_direct_valid_conversions` | No |
| Direct Conversion Value > 0 (SKAN) | `skad_direct_conversion_value_gt_0` | Direct SKAN postbacks with conversion value > 0. | `skad_direct_valid_conversions - skad_direct_conversion_value_0` | No |
| Direct Conversion Bit 1–6 (SKAN) | `skad_direct_conversion_1` … `skad_direct_conversion_6` | Direct SKAN postbacks where conversion event N triggered. | — | No |
| Direct Conversion Value 0–63 (SKAN) | `skad_direct_conversion_value_0` … `skad_direct_conversion_value_63` | Direct SKAN postbacks with the given conversion value. | — | No |

### Subscription metrics (non-cohort)

| Metric | API ID | Definition | Formula | Cohorted |
|---|---|---|---|---|
| Activations | `subscrevnt_activation_events` | Subscription activations. | — | No |
| Billing retry | `subscrevnt_billing_retry_events` | Trial expired without cancellation (iOS). | — | No |
| Cancellations | `subscrevnt_cancellation_events` | Subscription cancellations. | — | No |
| Discounted offers | `subscrevnt_discounted_offer_events` | Activations using a discount. | — | No |
| Expirations | `subscrevnt_expiration_events` | Subscription expirations. | — | No |
| First conversion | `subscrevnt_first_conversion_events` | First-conversion events. | — | No |
| Grace period | `subscrevnt_grace_period_events` | Entries into grace period. | — | No |
| On hold | `subscrevnt_on_hold_events` | Account hold entries (Android). | — | No |
| Paused | `subscrevnt_paused_events` | Subscription pauses (Android). | — | No |
| Price accepted | `subscrevnt_price_accepted_events` | Price-change confirmations. | — | No |
| Price declined | `subscrevnt_price_declined_events` | Price-change declines (iOS). | — | No |
| Reactivations | `subscrevnt_reactivation_events` | Subscription reactivations. | — | No |
| Renewals | `subscrevnt_renewal_events` | Subscription renewals. | — | No |
| Renewals from billing retry | `subscrevnt_renewal_from_billing_retry_events` | Renewals after billing recovery. | — | No |
| Refunds | `subscrevnt_refund_events` | Refunded transactions (iOS). | — | No |
| Revoked | `subscrevnt_revoked_events` | Revocations before expiration. | — | No |
| Trials started | `subscrevnt_trial_started_events` | Trial starts. | — | No |
| Activation revenue | `subscrevnt_activation_revenue` | Revenue from first-time activations. | — | No |
| Discounted offer revenue | `subscrevnt_discounted_offer_revenue` | Revenue from discounted purchases. | — | No |
| Reactivation revenue | `subscrevnt_reactivation_revenue` | Revenue from reactivations. | — | No |
| Refund revenue | `subscrevnt_refund_revenue` | Refund revenue (iOS). | — | No |
| Renewal revenue | `subscrevnt_renewal_revenue` | Revenue from renewals. | — | No |
| Renewal from billing retry revenue | `subscrevnt_renewal_from_billing_retry_revenue` | Revenue from billing-retry renewals. | — | No |
| Subscription revenue | `subscrevnt_revenue` | Total subscription revenue. | sum of all `subscrevnt_*_revenue` | No |
| Unknown revenue | `subscrevnt_unknown_revenue` | Revenue from undefined subscription events. | — | No |

### Cumulative subscription cohort metrics

Use suffix `_total_{cohort_period}`. Cohorted by install date.

| Metric | API ID | Definition | Cohorted |
|---|---|---|---|
| N days Activations Total | `subscription_activation_events_total_{cohort_period}` | Cumulative activations. | Yes |
| N days Billing retry Total | `subscription_billing_retry_events_total_{cohort_period}` | Cumulative billing retries (iOS). | Yes |
| N days Cancellations Total | `subscription_cancellation_events_total_{cohort_period}` | Cumulative cancellations. | Yes |
| N days Discounted offers Total | `subscription_discounted_offer_events_total_{cohort_period}` | Cumulative discounted activations. | Yes |
| N days Expirations Total | `subscription_expiration_events_total_{cohort_period}` | Cumulative expirations. | Yes |
| N days First conversion Total | `subscription_first_conversion_events_total_{cohort_period}` | Cumulative first conversions. | Yes |
| N days Grace period Total | `subscription_grace_period_events_total_{cohort_period}` | Cumulative grace-period entries. | Yes |
| N days On hold Total | `subscription_on_hold_events_total_{cohort_period}` | Cumulative account holds (Android). | Yes |
| N days Paused Total | `subscription_paused_events_total_{cohort_period}` | Cumulative pauses (Android). | Yes |
| N days Price accepted Total | `subscription_price_accepted_events_total_{cohort_period}` | Cumulative price-change confirms. | Yes |
| N days Price declined Total | `subscription_price_declined_events_total_{cohort_period}` | Cumulative price-change declines (iOS). | Yes |
| N days Reactivations Total | `subscription_reactivation_events_total_{cohort_period}` | Cumulative reactivations. | Yes |
| N days Refunds Total | `subscription_refund_events_total_{cohort_period}` | Cumulative refunds (iOS). | Yes |
| N days Renewals Total | `subscription_renewal_events_total_{cohort_period}` | Cumulative renewals. | Yes |
| N days Renewal from billing retry Total | `subscription_renewal_from_billing_retry_events_total_{cohort_period}` | Cumulative billing-retry renewals. | Yes |
| N days Revoked Total | `subscription_revoked_events_total_{cohort_period}` | Cumulative revocations. | Yes |
| N days Trials started Total | `subscription_trial_started_events_total_{cohort_period}` | Cumulative trial starts. | Yes |
| N days Revenue total (activations) | `subscription_activation_revenue_total_{cohort_period}` | Cumulative activation revenue. | Yes |
| N days Revenue total (discounted offers) | `subscription_discounted_offer_revenue_total_{cohort_period}` | Cumulative discounted-offer revenue. | Yes |
| N days Revenue total (reactivations) | `subscription_reactivation_revenue_total_{cohort_period}` | Cumulative reactivation revenue. | Yes |
| N days Revenue total (refunds) | `subscription_refund_revenue_total_{cohort_period}` | Cumulative refund revenue (iOS). | Yes |
| N days Revenue total (renewals) | `subscription_renewal_revenue_total_{cohort_period}` | Cumulative renewal revenue. | Yes |
| N days Revenue total (retries) | `subscription_renewal_from_billing_retry_revenue_total_{cohort_period}` | Cumulative billing-retry revenue. | Yes |
| N days Revenue total (other) | `subscription_unknown_revenue_total_{cohort_period}` | Cumulative undefined-event revenue. | Yes |
| N days Subscription Revenue Total | `subscription_revenue_total_{cohort_period}` | Cumulative subscription revenue. | Yes |
| N days Subscription Revenue Total in Cohort | `subscription_subscription_revenue_total_in_cohort_{cohort_period}` | Subscription revenue from completed-N users. | Yes |
| N days Conversion rates | `subscription_{event_from}_to_{event_to}_rate_{cohort_period}` | Conversion rate between two subscription events. | Yes |

### Non-cumulative subscription cohort metrics

Use suffix `_{cohort_period}` (no `_total`).

| Metric | API ID | Definition | Cohorted |
|---|---|---|---|
| N days Activations | `subscription_activation_events_{cohort_period}` | Activations during period N. | Yes |
| N days Billing retry | `subscription_billing_retry_events_{cohort_period}` | Billing retries during N (iOS). | Yes |
| N days Cancellations | `subscription_cancellation_events_{cohort_period}` | Cancellations during N. | Yes |
| N days Discounted offers | `subscription_discounted_offer_events_{cohort_period}` | Discounted activations during N. | Yes |
| N days Expirations | `subscription_expiration_events_{cohort_period}` | Expirations during N. | Yes |
| N days First conversion | `subscription_first_conversion_events_{cohort_period}` | First conversions during N. | Yes |
| N days Grace period | `subscription_grace_period_events_{cohort_period}` | Grace-period entries during N. | Yes |
| N days On hold | `subscription_on_hold_events_{cohort_period}` | Account holds during N (Android). | Yes |
| N days Paused | `subscription_paused_events_{cohort_period}` | Pauses during N (Android). | Yes |
| N days Price accepted | `subscription_price_accepted_events_{cohort_period}` | Price-change confirms during N. | Yes |
| N days Price declined | `subscription_price_declined_events_{cohort_period}` | Price-change declines during N (iOS). | Yes |
| N days Reactivations | `subscription_reactivation_events_{cohort_period}` | Reactivations during N. | Yes |
| N days Refunds | `subscription_refund_events_{cohort_period}` | Refunds during N (iOS). | Yes |
| N days Renewals | `subscription_renewal_events_{cohort_period}` | Renewals during N. | Yes |
| N days Renewals from billing retry | `subscription_renewal_from_billing_retry_events_{cohort_period}` | Billing-retry renewals during N. | Yes |
| N days Revoked | `subscription_revoked_events_{cohort_period}` | Revocations during N. | Yes |
| N days Trials started | `subscription_trial_started_events_{cohort_period}` | Trial starts during N. | Yes |
| N days Activation revenue | `subscription_activation_revenue_{cohort_period}` | Activation revenue during N. | Yes |
| N days Discounted offer revenue | `subscription_discounted_offer_revenue_{cohort_period}` | Discounted-offer revenue during N. | Yes |
| N days Reactivation revenue | `subscription_reactivation_revenue_{cohort_period}` | Reactivation revenue during N. | Yes |
| N days Refund revenue | `subscription_refund_revenue_{cohort_period}` | Refund revenue during N (iOS). | Yes |
| N days Renewal revenue | `subscription_renewal_revenue_{cohort_period}` | Renewal revenue during N. | Yes |
| N days Renewal from billing retry revenue | `subscription_renewal_from_billing_retry_revenue_{cohort_period}` | Billing-retry revenue during N. | Yes |
| N days Unknown revenue | `subscription_unknown_revenue_{cohort_period}` | Undefined-event revenue during N. | Yes |
| N days Subscription Revenue | `subscription_revenue_{cohort_period}` | Total subscription revenue during N. | Yes |
| N days Subscription ROAS | `subscription_roas_{cohort_period}` | Subscription ROAS for period N. | Yes |

### Fraud metrics (non-cohort)

| Metric | API ID | Definition | Formula | Cohorted |
|---|---|---|---|---|
| Invalid Signature Rejected Install Rate | `rejected_install_invalid_signature_rate` | Rejected installs with invalid signature, as a rate. | `rejected_installs_invalid_signature / (installs + rejected_installs)` | No |
| Rejected Installs | `rejected_installs` | Installs identified and rejected as fraudulent. | — | No |
| Rejected Install Rate | `rejected_install_rate` | Share of installs rejected as fraudulent. | `(rejected_installs - organic) / (installs - organic - untrusted + rejected - organic_rejected)` | No |
| Rejected Installs Anonymous IP | `rejected_installs_anon_ip` | Installs rejected from anonymous IPs. | — | No |
| Rejected Installs Anonymous IP Rate | `rejected_install_anon_ip_rate` | Anonymous-IP install rejection rate. | `rejected_installs_anon_ip / (installs + rejected_installs)` | No |
| Rejected Installs Click Injection | `rejected_installs_click_injection` | Installs rejected for click injection. | — | No |
| Rejected Installs Click Injection Rate | `rejected_install_click_injection_rate` | Click-injection rejection rate. | `rejected_installs_click_injection / (installs + rejected_installs)` | No |
| Rejected Installs Distribution Outlier | `rejected_installs_distribution_outlier` | Installs rejected for distribution outliers. | — | No |
| Rejected Installs Distribution Outlier Rate | `rejected_install_distribution_outlier_rate` | Distribution-outlier rejection rate. | `rejected_installs_distribution_outlier / (installs + rejected_installs)` | No |
| Rejected Installs Malformed Advertising ID | `rejected_install_malformed_advertising_id` | Installs rejected for malformed ad IDs. | — | No |
| Rejected Installs Malformed Advertising ID Rate | `rejected_install_malformed_advertising_id_rate` | Malformed ad-ID rejection rate. | — | No |
| Rejected Installs SDK Signature | `rejected_installs_invalid_signature` | Installs rejected for invalid/missing SDK signature. | — | No |
| Rejected Installs Too Many Engagements | `rejected_installs_too_many_engagements` | Installs rejected for excessive engagements. | — | No |
| Rejected Installs Too Many Engagements Rate | `rejected_install_too_many_engagements_rate` | Excessive-engagements rejection rate. | `rejected_installs_too_many_engagements / (installs + rejected_installs)` | No |
| Rejected Reattribution | `rejected_reattributions` | Reattributions identified and rejected as fraudulent. | — | No |
| Rejected Reattribution Rate | `rejected_reattribution_rate` | Share of reattributions rejected. | `rejected_reattributions / (reattributions + rejected_reattributions)` | No |
| Rejected Reattributions Anonymous IP | `rejected_reattributions_anon_ip` | Reattributions rejected from anonymous IPs. | — | No |
| Rejected Reattributions Anonymous IP Rate | `rejected_reattribution_anon_ip_rate` | Anonymous-IP reattribution rejection rate. | `rejected_reattributions_anon_ip / (reattributions + rejected_reattributions)` | No |
| Rejected Reattributions Click Injection | `rejected_reattributions_click_injection` | Reattributions rejected for click injection. | — | No |
| Rejected Reattributions Click Injection Rate | `rejected_reattributions_click_injection_rate` | Click-injection reattribution rejection rate. | `rejected_reattributions_click_injection / (reattributions + rejected_reattributions)` | No |
| Rejected Reattributions Distribution Outlier | `rejected_reattributions_distribution_outlier` | Reattributions rejected for distribution outliers. | — | No |
| Rejected Reattributions Distribution Outlier Rate | `rejected_reattribution_distribution_outlier_rate` | Distribution-outlier reattribution rejection rate. | `rejected_reattributions_distribution_outlier / (reattributions + rejected_reattributions)` | No |
| Rejected Reattributions Too Many Engagements | `rejected_reattributions_too_many_engagements` | Reattributions rejected for excessive engagements. | — | No |
| Rejected Reattributions Too Many Engagements Rate | `rejected_reattribution_too_many_engagements_rate` | Excessive-engagements reattribution rejection rate. | `rejected_reattributions_too_many_engagements / (reattributions + rejected_reattributions)` | No |

### Assist metrics (non-cohort)

| Metric | API ID | Definition | Formula | Cohorted |
|---|---|---|---|---|
| Assisted Installs | `assisted_installs` | Installs that qualified for attribution but weren't selected. | — | No |
| Assisting Engagements | `qualifiers` | Touchpoints considered but not awarded attribution. | — | No |
| Assisting Impressions | `impression_based_qualifiers` | Impression touchpoints not awarded attribution. | — | No |
| Assisting Clicks | `click_based_qualifiers` | Click touchpoints not awarded attribution. | — | No |
| Average Engagements per Assisted Install | `qualifiers_per_assisted_installs` | Engagements per assisted install. | `qualifiers / assisted_installs` | No |
| Average Impressions per Assisted Install | `impression_based_qualifiers_per_assisted_installs` | Assisting impressions per assisted install. | `impression_based_qualifiers / assisted_installs` | No |
| Average Clicks per Assisted Install | `click_based_qualifiers_per_assisted_installs` | Assisting clicks per assisted install. | `click_based_qualifiers / assisted_installs` | No |
| Assisting Clicks for Reattributions | `click_based_reattribution_qualifiers` | Click touchpoints not awarded reattribution. | — | No |
| Assisting Impressions for Reattributions | `impression_based_reattribution_qualifiers` | Impression touchpoints not awarded reattribution. | — | No |
| Assisting Engagements for Reattributions | `reattribution_qualifiers` | Touchpoints not awarded reattribution. | — | No |
| Assisted Reattributions | `assisted_reattributions` | Reattributions that qualified but weren't selected. | — | No |
| Non-Assisted Installs | `non_assisted_installs` | Installs with no qualifying touchpoints in the window. | `installs - assisted_installs` | No |

### Insight metrics (non-cohort)

| Metric | API ID | Definition | Formula | Cohorted |
|---|---|---|---|---|
| Average revenue per event | `average_revenue_per_event` | Average revenue per event from the install cohort. | `total revenue of event / event trigger count` | No |
| Incremental revenue | `incremental_revenue` | Extra revenue versus a control group. | `(actual incremental - mean incremental) * average_revenue_per_event` | No |
| Incremental ROAS | `incremental_roas` | ROAS for incremental analysis. | — | No |

 ## Examples

Copy campaigns data from Adjust into a DuckDB database:
```sh
ingestr ingest \
    --source-uri 'adjust://?api_key=nr_123' \
    --source-table 'campaigns' \
    --dest-uri duckdb:///adjust.duckdb \
    --dest-table 'dest.output'
```

Copy creatives data filtered by app token:
```sh
ingestr ingest \
    --source-uri 'adjust://?api_key=nr_123' \
    --source-table 'creatives:abc123xyz' \
    --dest-uri duckdb:///adjust.duckdb \
    --dest-table 'dest.output'
```

Copy custom data from Adjust into a DuckDB database:
```sh
ingestr ingest \
    --source-uri "adjust://?api_key=nr_123&lookback_days=2" \
    --source-table "custom:hour,app,store_id,channel,os_name,country_code,campaign_network,campaign_id_network,adgroup_network,adgroup_id_network,creative_network,creative_id_network:impressions,clicks,cost,network_cost,installs,ad_revenue,all_revenue" \
    --dest-uri duckdb:///adjust.db \
    --dest-table "mat.example"
```

Copy custom data filtered by app token:
```sh
ingestr ingest \
    --source-uri "adjust://?api_key=nr_123" \
    --source-table "custom:day,campaign,app:installs,clicks:app_token__in=abc123xyz" \
    --dest-uri duckdb:///adjust.db \
    --dest-table "mat.example"
```
