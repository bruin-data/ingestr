# Braze
[Braze](https://www.braze.com/) is a customer engagement platform used to orchestrate cross-channel messaging campaigns and analyze customer behavior.

ingestr supports Braze as a source.

## URI format

```
braze://?api_key=<rest-api-key>&endpoint=<rest-endpoint>
```

URI parameters:
- `api_key` is a Braze REST API key with access to the relevant export endpoints. You can create one in the Braze dashboard under **Settings → APIs and Identifiers**.
- `endpoint` is your instance's REST endpoint host, e.g. `rest.iad-01.braze.com`. Braze is multi-instance, so the host depends on which cluster your account is on — find it in the dashboard or in the [API overview](https://www.braze.com/docs/api/basics). The `https://` scheme is optional.

Here's a sample command that copies campaigns from Braze into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "braze://?api_key=YOUR_REST_API_KEY&endpoint=rest.iad-01.braze.com" \
  --source-table "campaigns" \
  --dest-uri duckdb:///braze.duckdb \
  --dest-table "public.campaigns"
```

## Tables

Braze source allows ingesting the following resources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| --- | --- | --- | --- | --- |
| [campaigns](https://www.braze.com/docs/api/endpoints/export/campaigns/get_campaigns) | id | last_edited | merge | Marketing campaigns (including archived) with their name, tags, and API flags. |
| [canvases](https://www.braze.com/docs/api/endpoints/export/canvas/get_canvases) | id | last_edited | merge | Canvas (journey) definitions (including archived) with their name and tags. |
| [segments](https://www.braze.com/docs/api/endpoints/export/segments/get_segment) | id | – | replace | Audience segments with their name and analytics-tracking flag. |
| [events](https://www.braze.com/docs/api/endpoints/export/custom_events/get_custom_events) | event_name | – | replace | Names of the custom events tracked in the workspace. |
| [products](https://www.braze.com/docs/api/endpoints/export/purchases/get_list_product_id) | product_id | – | replace | Product IDs seen in purchase events. |
| [kpi_dau](https://www.braze.com/docs/api/endpoints/export/kpi/get_kpi_dau_date) | time | time | merge | Daily active users by date. |
| [kpi_mau](https://www.braze.com/docs/api/endpoints/export/kpi/get_kpi_mau_30_days) | time | time | merge | Monthly active users (rolling 30-day) by date. |
| [kpi_new_users](https://www.braze.com/docs/api/endpoints/export/kpi/get_kpi_daily_new_users_date) | time | time | merge | New users by date. |
| [kpi_uninstalls](https://www.braze.com/docs/api/endpoints/export/kpi/get_kpi_uninstalls_date) | time | time | merge | App uninstalls by date. |
| [event_series](https://www.braze.com/docs/api/endpoints/export/custom_events/get_custom_events_analytics) | time, event_name | time | merge | Daily occurrence count per custom event. Fetches all events by default; an optional `event_series:<name>[,<name>]` filter limits it. |
| [user_data](https://www.braze.com/docs/api/endpoints/export/user_data/post_users_segment) | braze_id, segment_id | – | replace | Users in a segment with their email/push subscription state and profile fields (a point-in-time snapshot). |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

### User data

The `user_data` table exports the users of one or more segments along with their subscription state. At least one segment id is required and passed as a comma-separated suffix on the table name:

```
--source-table "user_data:<segment_id>"
--source-table "user_data:<segment_id_1>,<segment_id_2>"
```

You can find segment identifiers in the Braze dashboard. Each run produces a fresh full snapshot of those segments' users, and every row is tagged with the `segment_id` it came from (so the same user can appear under multiple segments). Exported fields include the subscription state (`email_subscribe`, `push_subscribe`, plus the opt-in/unsubscribe timestamps Braze returns automatically alongside them), identifiers (`braze_id`, `external_id`, `email`, `phone`), profile fields (name, country, language, time zone, …), and per-product `purchases`.

### Per-app KPIs

By default the `kpi_*` tables aggregate across all apps in the workspace. To break a KPI down by app, append a comma-separated list of app identifiers (found in the Braze dashboard under **Settings → APIs and Identifiers**) to the table name:

```
--source-table "kpi_dau:app-one-id,app-two-id"
```

Each row is then tagged with an `app_id` column. This is only supported on `kpi_*` tables.

## Incremental loading

`campaigns`, `canvases`, and the `kpi_*` tables load incrementally — use `--interval-start` and `--interval-end` to limit the data to a time range. When no interval is provided, the list tables fetch all records and the KPI tables fetch the most recent 100 days. The `segments`, `events`, and `products` tables are always fully refreshed.
