# CleverTap
[CleverTap](https://clevertap.com/) is a customer engagement and retention platform that combines analytics, segmentation, and cross-channel campaigns for mobile and web apps.

ingestr supports CleverTap as both a source and a destination.

## URI format

```
clevertap://?account_id=<account_id>&passcode=<passcode>&region=<region>&timezone=<timezone>
```

URI parameters:
- `account_id`: the CleverTap Account ID for your project.
- `passcode`: the Account Passcode, or your user passcode if your admin has enabled user-level passcodes.
- `region`: optional, the data centre your account lives in. One of `eu1`, `in1`, `us1`, `sg1`, `aps3`, `mec1`. Defaults to `eu1`. European projects appear as `global` in the dashboard, and that value works too.
- `timezone`: optional, the timezone your CleverTap project is set to, as an IANA name such as `Asia/Kolkata`. Defaults to `UTC`. **Set this to match your project**, otherwise every event time is shifted by the difference.

Find all four in the CleverTap dashboard under **Settings → Project**. Your region is also the subdomain of your dashboard URL, so `in1.dashboard.clevertap.com` means `region=in1`.

Here's a sample command that copies your user profiles into a DuckDB database:

```sh
ingestr ingest \
  --source-uri "clevertap://?account_id=TEST-ABC-123&passcode=pass_123&region=eu1" \
  --source-table "profiles" \
  --dest-uri duckdb:///clevertap.duckdb \
  --dest-table "public.profiles"
```

## Tables

CleverTap source allows ingesting the following resources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| [events](https://developer.clevertap.com/docs/get-events-api) | – | ts | delete+insert | Individual occurrences of an event, with who raised it and the event's properties |
| [profiles](https://developer.clevertap.com/docs/get-user-profiles-api) | object_id | – | replace | Your users, with their custom properties, activity summaries, and devices |
| [campaigns](https://developer.clevertap.com/docs/get-campaigns-api) | id | – | replace | Campaigns created **through the API**, with their name, schedule, and status |
| [campaign_reports](https://developer.clevertap.com/docs/get-campaign-report-api) | id | – | replace | Delivery and engagement metrics for each completed campaign created **through the API** |
| [content_blocks](https://developer.clevertap.com/docs/get-content-block-list-api) | id | updatedAt | merge | Reusable content blocks, with their type, content, and authorship |
| [message_reports](https://developer.clevertap.com/docs/get-message-reports-api) | message_id | – | replace | Per-message delivery and engagement counts |
| [event_schema](https://developer.clevertap.com/docs/get-schema) | name | – | replace | Every event defined in your project, with its properties |
| [user_properties](https://developer.clevertap.com/docs/get-schema) | name | – | replace | Every custom profile property defined in your project |
| [category_groups](https://developer.clevertap.com/docs/settings-api-endpoints) | key | – | replace | Messaging subscription groups, with the channels each one covers |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

> [!WARNING]
> `campaigns` and `campaign_reports` only ever contain campaigns **created through the CleverTap API**. Campaigns you build in the dashboard are not included.

### Choosing which events to load

`events` and `profiles` accept an `event_name` parameter, which narrows them to the events you name:

```sh
ingestr ingest \
  --source-uri "clevertap://?account_id=TEST-ABC-123&passcode=pass_123" \
  --source-table "events?event_name=App Launched" \
  --dest-uri duckdb:///clevertap.duckdb \
  --dest-table "public.app_launched"
```

Each name must match your CleverTap dashboard exactly, including spaces and capitalisation. The name is also included as a column, so several events can share one destination table.

Separate names with a comma to load more than one:

```sh
--source-table "events?event_name=Charged,App Launched"
```

Leave the parameter out and you get everything:

```sh
--source-table "events"     # every event, in one table
--source-table "profiles"   # everyone who has done anything
```

CleverTap reaches profiles through their activity, so `profiles` covers users who have raised at least one exportable event. Anyone whose only recorded activity is a notification event, which CleverTap will not export, is not included.

## Joining events to profiles

`events` carries two keys:

| Column | Identifies | Use it for |
| ------ | ---------- | ---------- |
| `identity` | the person | joining to `profiles` |
| `object_id` | one device | per-device analysis |

```sql
SELECT e.ts, e.event_name, p.name, p.profile_data
FROM events e
JOIN profiles p ON e.identity = p.identity
```

**Join on `identity`.** Someone using your app on both a phone and a laptop has a different `object_id` for each, so joining on `object_id` silently drops the events they raised on their other devices. Every device is still listed in `profiles.platform_info`.

`identity` is only set for users who have logged in, so events from anonymous visitors have no profile to join to.

## Date ranges

`events` and `content_blocks` respect `--interval-start` and `--interval-end`:

```sh
ingestr ingest \
  --source-uri "clevertap://?account_id=TEST-ABC-123&passcode=pass_123" \
  --source-table "events?event_name=Charged" \
  --dest-uri duckdb:///clevertap.duckdb \
  --dest-table "public.charged" \
  --interval-start 2024-01-01 \
  --interval-end 2024-04-01
```

The end bound is exclusive of that day's activity, so use the following day to capture a full day. With no interval at all, everything is loaded.

The other tables always load in full and ignore the interval.

## Limitations

> [!WARNING]
> As noted above, `campaigns` and `campaign_reports` cover only API-created campaigns. Dashboard campaigns never appear whatever their channel or schedule, because CleverTap offers no way to list them.

> [!NOTE]
> The `profile` column on `events` shows the user's details as they stand today, not as they were when the event happened. CleverTap keeps no history of past values, so point-in-time user attributes are not available from this source.

> [!NOTE]
> A campaign only has a report once it has completed and delivered. Campaigns that are still scheduled, running, paused, or were stopped before delivering are skipped, so `campaign_reports` usually holds fewer rows than `campaigns`.

> [!NOTE]
> Notification events such as push impressions and clicks cannot be exported from CleverTap and are skipped automatically.

# CleverTap as a destination

ingestr can write user profiles and events into CleverTap through its [Upload API](https://developer.clevertap.com/docs/upload-user-profiles-api). Each source row is sent as one profile or event record, and rows are uploaded in bulk — up to 1000 records per request.

## URI format

```
clevertap://?account_id=<account_id>&passcode=<passcode>&region=<region>
```

The parameters are the same as the source (`account_id`, `passcode`, `region`). `timezone` is not used when writing.

## What to upload

Every row you send becomes one CleverTap record. The base of the `--dest-table` value (before the `?`) selects the record type — `profiles` or `events` — and the parameters after it tell ingestr which columns carry the special fields. Every other column is uploaded as an attribute under its own name. ingestr's own `_ingestr_loaded_at` and `_ingestr_run_id` columns are never uploaded.

### Profiles

```sh
ingestr ingest \
  --source-uri "postgres://user:pass@host:5432/db" \
  --source-table "public.marketing_users" \
  --dest-uri "clevertap://?account_id=TEST-ABC-123&passcode=pass_123&region=eu1" \
  --dest-table "profiles?identity_column=email"
```

| Parameter | Required? | Description |
| --------- | --------- | ----------- |
| `identity_column` | **Required** | The source column holding each row's identifier. For example, `identity_column=email` takes each row's identifier from the `email` column. |
| `id_type` | Optional | How CleverTap resolves the identifier: `identity` (default), `objectId`, `FBID`, or `GPID`. For example, `identity_column=device_id&id_type=objectId` sends each `device_id` value as an `objectId`. |
| `on_error` | Optional | `fail` (default) fails the run if CleverTap rejects any record; `skip` warns and continues. Either way each rejected record is printed as it happens and listed with its error at the end. |

Strategy:
- **Always merged on CleverTap's side** — profiles are upserted by identity, so whichever strategy you run, re-sending a user updates their attributes instead of creating a duplicate.
- **No interval** — with no `--incremental-key`, the whole table is re-sent each run. Fine for small user bases.
- **`--incremental-key`** (such as `updated_at`) with **`--interval-start`/`--interval-end`** — sends only the rows in that window instead of the whole table. Use this for large user bases.

### Events

```sh
ingestr ingest \
  --source-uri "bigquery://my-project/analytics?credentials_path=/creds.json" \
  --source-table "analytics.purchases" \
  --dest-uri "clevertap://?account_id=TEST-ABC-123&passcode=pass_123&region=eu1" \
  --dest-table "events?identity_column=user_id&ts=purchased_at&event_name=Charged" \
  --incremental-key purchased_at \
  --interval-start 2024-01-01 \
  --interval-end 2024-01-02
```

| Parameter | Required? | Description |
| --------- | --------- | ----------- |
| `event_name` **or** `event_name_column` | **Required** | A fixed event name applied to every row (`event_name`), or a column whose value is the event name per row (`event_name_column`) for tables that mix event types. |
| `identity_column` | **Required** | The source column holding each row's identifier. For example, `identity_column=email` takes each row's identifier from the `email` column. |
| `id_type` | Optional | How CleverTap resolves the identifier: `identity` (default), `objectId`, `FBID`, or `GPID`. For example, `identity_column=device_id&id_type=objectId` sends each `device_id` value as an `objectId`. |
| `ts` | Optional | The source column holding the event timestamp. If omitted, CleverTap stamps the upload time. |
| `on_error` | Optional | `fail` (default) fails the run if CleverTap rejects any record; `skip` warns and continues. Either way each rejected record is printed as it happens and listed with its error at the end. |

Strategy:
- **Always appended on CleverTap's side** — whichever strategy you run, each uploaded event is added to the user's timeline; CleverTap never replaces or de-duplicates events, so re-sending a row creates a duplicate.
- **`--incremental-key`** (usually the same column as `ts`) with **`--interval-start`/`--interval-end`** — uploads only the events in that window, so you control exactly which events are sent each run.

> [!NOTE]
> Every record must carry an identifier (`identity`, `objectId`, `FBID`, or `GPID`); rows with an empty identity value are skipped. CleverTap resolves users by this identifier the same way a primary key deduplicates a table.

> [!NOTE]
> CleverTap accepts up to 1000 records per request and limits uploads to 3 concurrent requests per account; ingestr batches and rate-limits accordingly.
