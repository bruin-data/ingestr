# Adapty

[Adapty](https://adapty.io/) is a subscription monetization platform for mobile and web apps. ingestr supports its batch Analytics Export API and the paginated paywall list from the Server-side API.

The connector does not use webhooks or realtime event tracking. Adapty does not expose a batch endpoint for listing raw purchase transactions, so purchase activity is available through its revenue, subscription, refund, cohort, conversion, LTV, funnel, and retention analytics.

## URI format

```plaintext
adapty://?api_key=<secret-api-key>&lookback_days=30&timezone=UTC
```

Parameters:

- `api_key`: Required. The app-specific secret API key from **App Settings → General → API keys**.
- `lookback_days`: Optional. Number of days to re-fetch before an incremental analytics interval. Defaults to `30`; use `0` to disable lookback.
- `timezone`: Optional. IANA timezone used by Adapty to group analytics dates. Defaults to `UTC`.

Use the secret API key, not a public SDK key. The connector sends it as `Authorization: Api-Key <secret-api-key>`.

## Tables

| Table | Required parameters | Incremental behavior | Description |
| --- | --- | --- | --- |
| `analytics` | `chart_id` | `delete+insert` on `date` | Revenue, MRR, ARR, ARPU/ARPPU, subscriptions, trials, refunds, billing issues, installs, and related chart metrics. |
| `cohorts` | – | `delete+insert` on `date` | Cohort revenue, subscriber, subscription, ARPU, ARPPU, and ARPAS analytics. |
| `conversion` | `from_period`, `to_period` | `delete+insert` on `date` | Conversion between two subscription states. Use `from_period=null` when there is no starting state. |
| `funnel` | – | `delete+insert` on `date` | Subscription funnel and churn analytics. |
| `ltv` | – | `delete+insert` on `date` | Actual lifetime value for revenue, proceeds, and net revenue. |
| `retention` | – | `delete+insert` on `date` | Subscriber retention analytics. |
| `placements` | `placement_type` | `replace` | Exported paywall or onboarding placement configuration. |
| `paywalls` | – | `merge` on `paywall_id`, incremental key `updated_at` | All paginated paywalls, including their state, deletion marker, and nested products. |

The six analytics tables are requested one calendar day at a time and receive an ingestr-managed `date` column. When no interval is supplied, they load the last 30 days by default. `lookback_days` expands an incremental interval so mutable subscription and revenue aggregates are refreshed safely.

The paywall API does not accept an `updated_at` filter. ingestr scans its paginated list and applies the incremental interval client-side. Adapty returns archived and deleted paywalls with `state` and `is_deleted`, allowing merge loads to retain lifecycle changes.

Provider response objects and arrays, such as cohort values and paywall products, remain nested rather than being flattened.

## Analytics table parameters

Parameters are URL-style query parameters on the source table. All six analytics tables accept these optional filters:

- `compare_date`: Exactly two `YYYY-MM-DD` dates.
- `store`, `country`, `store_product_id`, `duration`
- `attribution_source`, `attribution_status`, `attribution_channel`, `attribution_campaign`, `attribution_adgroup`, `attribution_adset`, `attribution_creative`
- `offer_category`, `offer_type`, `offer_id`

List values are comma-separated. Each table also accepts its endpoint-specific parameters:

| Table | Optional parameters |
| --- | --- |
| `analytics` | `date_type`, `segmentation` |
| `cohorts` | `period_type`, `value_type`, `value_field`, `accounting_type`, `renewal_days`, `prediction_months` |
| `conversion` | `date_type`, `segmentation` |
| `funnel` | `show_value_as`, `segmentation` |
| `ltv` | `period_type`, `segmentation` |
| `retention` | `segmentation`, `use_trial` |

ingestr fixes `period_unit` to `day` and `format` to `json` so each batch has a stable daily incremental boundary.

Valid `chart_id` values are:

- `revenue`
- `mrr`
- `arr`
- `arppu`
- `subscriptions_active`
- `subscriptions_new`
- `subscriptions_renewal_cancelled`
- `subscriptions_expired`
- `trials_active`
- `trials_new`
- `trials_renewal_cancelled`
- `trials_expired`
- `grace_period`
- `billing_issue`
- `refund_events`
- `refund_money`
- `non_subscriptions`
- `arpu`
- `installs`

## Examples

Load daily purchase revenue into DuckDB:

```sh
ingestr ingest \
  --source-uri 'adapty://?api_key=secret_live_...' \
  --source-table 'analytics?chart_id=revenue' \
  --dest-uri 'duckdb:///adapty.duckdb' \
  --dest-table 'adapty.revenue'
```

Load refund totals for selected stores and countries:

```sh
ingestr ingest \
  --source-uri 'adapty://?api_key=secret_live_...&lookback_days=60' \
  --source-table 'analytics?chart_id=refund_money&store=app_store,play_store&country=us,gb' \
  --dest-uri 'duckdb:///adapty.duckdb' \
  --dest-table 'adapty.refunds'
```

Load cohort revenue by days:

```sh
ingestr ingest \
  --source-uri 'adapty://?api_key=secret_live_...' \
  --source-table 'cohorts?period_type=days&value_field=revenue&accounting_type=net_revenue&renewal_days=0,1,3,7,14,30' \
  --dest-uri 'duckdb:///adapty.duckdb' \
  --dest-table 'adapty.cohorts'
```

Load paywall placement configuration:

```sh
ingestr ingest \
  --source-uri 'adapty://?api_key=secret_live_...' \
  --source-table 'placements?placement_type=paywall' \
  --dest-uri 'duckdb:///adapty.duckdb' \
  --dest-table 'adapty.placements'
```

Load all paywall definitions:

```sh
ingestr ingest \
  --source-uri 'adapty://?api_key=secret_live_...' \
  --source-table 'paywalls' \
  --dest-uri 'duckdb:///adapty.duckdb' \
  --dest-table 'adapty.paywalls'
```

See Adapty's [Analytics Export API reference](https://adapty.io/docs/api-export-analytics) and [Server-side API reference](https://adapty.io/docs/api-adapty) for the upstream response fields and metric definitions.
