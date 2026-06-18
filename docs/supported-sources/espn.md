# ESPN

ESPN exposes a set of unauthenticated JSON endpoints at `site.api.espn.com` that cover scores, teams, standings, and news. These endpoints are unofficial and can change without notice.

`ingestr` supports ESPN as a public source. No authentication is required. The default configuration targets NFL data with `sport=football` and `league=nfl`.

## URI format

```plaintext
espn://?sport=<sport-slug>&league=<league-slug>
```

URI parameters:

- `sport`: ESPN sport slug. Defaults to `football`.
- `league`: ESPN league slug. Defaults to `nfl`.
- `season`: Optional season year passed to scoreboard and standings requests.
- `limit`: Optional request limit for scoreboard and news. Defaults to `100`.
- `base_url`: Overrides the ESPN API base URL. Defaults to `https://site.api.espn.com`.

The scoreboard window comes from `--interval-start` / `--interval-end`, which the source converts to ESPN's `dates=YYYYMMDD[-YYYYMMDD]` query parameter (UTC).

## Example

Load NFL scoreboard events into DuckDB:

```sh
ingestr ingest \
  --source-uri 'espn://?sport=football&league=nfl' \
  --source-table 'scoreboard' \
  --interval-start 2026-09-10 \
  --interval-end 2026-09-12 \
  --dest-uri 'duckdb:///espn.duckdb' \
  --dest-table 'sports.scoreboard'
```

Load NBA teams:

```sh
ingestr ingest \
  --source-uri 'espn://?sport=basketball&league=nba' \
  --source-table 'teams' \
  --dest-uri 'duckdb:///espn.duckdb' \
  --dest-table 'sports.nba_teams'
```

## Tables

| Table | PK | Inc Key | Default Strategy | Details |
| --- | --- | --- | --- | --- |
| `teams` | `id` | - | `replace` | Loads teams from `/apis/site/v2/sports/{sport}/{league}/teams`. Roster snapshot. |
| `scoreboard` | `id` | - | `merge` | Loads scoreboard events from `/apis/site/v2/sports/{sport}/{league}/scoreboard`. Use `merge` to accumulate events across interval runs. |
| `competitors` | `event_id`, `competition_id`, `team_id` | - | `merge` | Fans out each scoreboard event into one row per competitor/team. |
| `standings` | `league_id`, `group_id`, `season`, `team_id` | - | `replace` | Loads standings from `/apis/v2/sports/{sport}/{league}/standings`. Latest snapshot for the given season. |
| `news` | `id` | - | `merge` | Loads latest league news articles from `/apis/site/v2/sports/{sport}/{league}/news`. Accumulates over runs. |

Use these as the `--source-table` parameter in the `ingestr ingest` command. Pass `--incremental-strategy` to override the default for any table.
