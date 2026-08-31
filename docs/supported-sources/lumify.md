# Lumify

[Lumify](https://lumify.ai) is an agent-ready sports intelligence API covering schedules, live scores, odds, betting splits, and explainable bet confidence across 8+ sports.

`ingestr` supports Lumify as a keyed source for sports reference data and event windows.

## URI format

```plaintext
lumify://?api_key=<api-key>&sport=nba
```

URI parameters:

- `api_key`: Lumify API key (`lmfy-...`). Required. Get a free instant key (no signup) at [lumify.ai/docs/ai](https://lumify.ai/docs/ai), or create a persistent key at [lumify.ai/api-keys](https://lumify.ai/api-keys).
- `sport`: Optional sport slug filter applied to `seasons`, `teams`, `players`, `events`, and `leagues` (for example `nba`, `nfl`, `mlb`, `nhl`, `soccer`, `tennis`, `golf`, `mma`).
- `league`: Optional league slug filter applied to `teams` and `events`.
- `base_url`: Overrides the API base URL. Defaults to `https://lumify.ai`.

## Example

Load NBA teams into DuckDB:

```sh
ingestr ingest \
  --source-uri 'lumify://?api_key=<api-key>&sport=nba' \
  --source-table 'teams' \
  --dest-uri 'duckdb:///sports.duckdb' \
  --dest-table 'lumify.teams'
```

Load events for a date window:

```sh
ingestr ingest \
  --source-uri 'lumify://?api_key=<api-key>&sport=nba' \
  --source-table 'events' \
  --dest-uri 'duckdb:///sports.duckdb' \
  --dest-table 'lumify.events' \
  --interval-start '2026-07-01T00:00:00Z' \
  --interval-end '2026-07-08T00:00:00Z'
```

## Tables

| Table | PK | Inc Key | Inc Strategy | Details |
| --- | --- | --- | --- | --- |
| `sports` | `id` | - | replace | Loads sports from `/v1/sports`. Nested `leagues` are kept as JSON. |
| `leagues` | `id` | - | replace | Flattens nested leagues from `/v1/sports` and adds `sport_id`, `sport_slug`, and `sport_name`. |
| `seasons` | `id` | - | replace | Loads seasons from `/v1/seasons`. Nested `sport` and `league` objects are kept as JSON. |
| `teams` | `id` | - | replace | Loads paginated teams from `/v1/teams`. |
| `players` | `id` | - | replace | Loads paginated players from `/v1/players`. |
| `events` | `id` | - | merge | Loads events from `/v1/events` for an interval window, with `include_scores=true` so participants/scores are present. |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

## Notes

- The API key is sent as `Authorization: Bearer <api_key>`.
- `teams`, `players`, and `events` handle Lumify `after_id` cursor pagination automatically.
- `events` uses `--interval-start` / `--interval-end` when provided. If omitted, the source defaults to the last 7 days through now. Intervals longer than 90 days are chunked to match the Lumify API limit.
- Each successful request consumes Lumify API credits. Prefer scoped `sport` / `league` filters and tight event intervals in production syncs.
- Docs: [https://lumify.ai/docs](https://lumify.ai/docs) · OpenAPI: [https://lumify.ai/openapi.json](https://lumify.ai/openapi.json)
