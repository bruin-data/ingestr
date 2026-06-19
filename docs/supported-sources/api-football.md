# API-Football

[API-Football](https://www.api-football.com/) provides soccer data from API-SPORTS, including World Cup teams, fixtures, standings, players, venues, and match events.

`ingestr` supports API-Football as a keyed source for FIFA World Cup data. The default configuration targets World Cup 2026 with `league=1` and `season=2026`.

For endpoint parameters, plan tiers, and implementation notes across the selected soccer providers, see the [API-Football source research](../soccer-sources/api-football.md).

## URI format

```plaintext
api-football://?api_key=<api-key>&league=1&season=2026
```

URI parameters:

- `api_key`: API-Football API key. Required.
- `league`: API-Football league ID. Defaults to `1` for FIFA World Cup.
- `season`: Season year. Defaults to `2026`.
- `timezone`: Optional timezone passed to fixture requests.
- `base_url`: Overrides the API base URL. Defaults to `https://v3.football.api-sports.io`.

## Example

Load World Cup 2026 matches into DuckDB:

```sh
ingestr ingest \
  --source-uri 'api-football://?api_key=<api-key>&league=1&season=2026' \
  --source-table 'matches' \
  --dest-uri 'duckdb:///worldcup2026.duckdb' \
  --dest-table 'soccer.matches'
```

Load World Cup 2026 match events:

```sh
ingestr ingest \
  --source-uri 'api-football://?api_key=<api-key>&league=1&season=2026' \
  --source-table 'match_events' \
  --dest-uri 'duckdb:///worldcup2026.duckdb' \
  --dest-table 'soccer.match_events'
```

## Tables

| Table | PK | Inc Key | Inc Strategy | Details |
| --- | --- | --- | --- | --- |
| `teams` | `id` | - | replace | Loads teams from `/teams?league=<league>&season=<season>`. |
| `stadiums` | `id` | - | merge | Derives venue IDs from fixtures and hydrates each venue through `/venues?id=<id>`. |
| `group_standings` | `league_id`, `season`, `group_name`, `team_id` | - | merge | Loads group standings from `/standings`. |
| `matches` | `id` | - | merge | Loads fixtures from `/fixtures`. |
| `players` | `id` | - | replace | Loads paginated player rows from `/players`. |
| `match_events` | `event_key` | - | merge | Fetches fixtures, then loads events from `/fixtures/events?fixture=<id>`. |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

## Notes

- The API key is sent in the `x-apisports-key` header.
- API-Football free plans may not expose future seasons such as `2026`; the source surfaces the provider's plan/season error when access is denied.
- `players` follows API-Football page pagination automatically.
- `stadiums` and `match_events` are fixture-derived because API-Football does not expose World Cup-scoped venue or all-event endpoints.
- When `--interval-start` and `--interval-end` are both set, the fixture-derived tables (`matches`, `stadiums`, `match_events`) filter server-side via the `/fixtures` `from`/`to` parameters. `teams`, `group_standings`, and `players` have no time filter and always return the full league/season. With no interval, all tables fetch everything.
- Nested provider objects are preserved as JSON columns; the schema is inferred from the data. Each table also exposes the primary-key fields (e.g. `id`, or `league_id`/`season`/`group_name`/`team_id`) as top-level typed columns so merge strategies can de-duplicate.
- Strategy follows incremental capability. A table uses `merge` when its fetch honors the ingestion interval — `matches`, `stadiums`, and `match_events` all source from `/fixtures` and apply its `from`/`to` filter — or when its rows carry an update timestamp (`group_standings` via `standing.update`). `teams` and `players` have neither (their endpoints take no time filter and their rows have no update field), so they use `replace` with a full fetch.
