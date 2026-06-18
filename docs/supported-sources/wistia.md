# Wistia

Wistia can be used as a source with the `wistia://` URI scheme.

```bash
ingestr ingest \
  --source-uri 'wistia://?access_token=<YOUR_WISTIA_API_TOKEN>' \
  --source-table 'medias' \
  --dest-uri duckdb:///wistia.duckdb \
  --dest-table 'dest.medias'
```

## URI

```text
wistia://?access_token=<YOUR_WISTIA_API_TOKEN>
```

The source also accepts `api_key` or `token` as aliases for `access_token`.

Optional parameters:

| Parameter | Description | Default |
| --- | --- | --- |
| `api_version` | Value sent as the `X-Wistia-API-Version` header. | `2026-03` |
| `base_url` | Override the Wistia API base URL, mostly for tests. | `https://api.wistia.com/modern` |

## Tables

Available Data API tables:

| Table | Description |
| --- | --- |
| `account` | Current account summary. |
| `token` | Current token summary. |
| `allowed_domains` | Allowed domains. |
| `folders` | Folders. |
| `folder:<folder_id>` | A single folder. |
| `folder_sharings:<folder_id>` | Sharing records for a folder. |
| `subfolders:<folder_id>` | Subfolders for a folder. |
| `medias` | Media records. |
| `media:<media_id>` | A single media record. |
| `captions` | Captions across the account. |
| `captions:<media_id>` | Captions filtered by media. |
| `media_captions:<media_id>` | Captions for a media. |
| `media_localizations:<media_id>` | Localizations for a media. |
| `media_customizations:<media_id>` | Customizations for a media. |
| `media_stats:<media_id>` | Aggregated media stats from the Data API. |
| `channels` | Channels. |
| `channel:<channel_id>` | A single channel. |
| `channel_episodes` | Channel episodes. |
| `channel_episodes_by_channel:<channel_id>` | Episodes in a channel. |
| `tags` | Tags. |
| `webinars` | Webinars. |
| `webinar:<webinar_id>` | A single webinar. |

Available Stats API tables:

| Table | Description | Incremental |
| --- | --- | --- |
| `stats_account` | Current account stats. | No |
| `stats_account_by_date` | Account stats by date. | Yes, using `date` |
| `stats_events` | Event records. | Yes, using `received_at` |
| `stats_events:<media_id>` | Event records filtered by media. | Yes, using `received_at` |
| `stats_events_by_visitor:<visitor_key>` | Event records filtered by visitor. | Yes, using `received_at` |
| `stats_visitors` | Visitors. | No |
| `stats_event:<event_key>` | A single event. | No |
| `stats_visitor:<visitor_key>` | A single visitor. | No |
| `stats_media:<media_id>` | Stats for a media. | No |
| `stats_media_by_date:<media_id>` | Stats for a media by date. | Yes, using `date` |
| `stats_media_engagement:<media_id>` | Engagement stats for a media. | No |
| `stats_project:<project_id>` | Stats for a project/folder. | No |

Date-filtered Stats API tables use `--interval-start` and `--interval-end` as Wistia `start_date` and `end_date` query parameters. For `stats_account_by_date`, ingestr sends yesterday/today when no interval is provided because the Wistia endpoint currently returns an internal server error without an explicit date range.
