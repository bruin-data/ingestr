# ESPN

[ESPN's public site API](https://espnapi.com/) exposes unauthenticated JSON endpoints for scores, teams, standings, and news. These endpoints are unofficial and can change without notice.

`ingestr` supports ESPN as a public source. The default configuration targets NFL data with `sport=football` and `league=nfl`.

## URI format

```plaintext
espn://?sport=<sport-slug>&league=<league-slug>
```

URI parameters:

- `sport`: ESPN sport slug. Defaults to `football`.
- `league`: ESPN league slug. Defaults to `nfl`.
- `dates`: Optional scoreboard date or date range, such as `20260910` or `20260910-20260912`.
- `season`: Optional season year passed to scoreboard and standings requests.
- `limit`: Optional request limit for scoreboard and news. Defaults to `100`.
- `base_url`: Overrides the ESPN API base URL. Defaults to `https://site.api.espn.com`.

## Example

Load NFL scoreboard events into DuckDB:

```sh
ingestr ingest \
  --source-uri 'espn://?sport=football&league=nfl&dates=20260910-20260912' \
  --source-table 'events' \
  --dest-uri 'duckdb:///espn.duckdb' \
  --dest-table 'sports.events'
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

| Table | PK | Inc Key | Inc Strategy | Details |
| --- | --- | --- | --- | --- |
| `teams` | `id` | - | replace | Loads teams from `/apis/site/v2/sports/{sport}/{league}/teams`. |
| `scoreboard` | `id` | - | replace | Loads flattened scoreboard event rows and preserves each raw event as JSON. |
| `events` | `id` | - | replace | Alias-shaped event table for scoreboard events. |
| `competitors` | `event_id`, `competition_id`, `team_id` | - | replace | Fans out each scoreboard event into one row per competitor/team. |
| `standings` | `league_id`, `group_id`, `season`, `team_id` | - | replace | Loads standings from `/apis/v2/sports/{sport}/{league}/standings`. |
| `news` | `id` | - | replace | Loads latest league news articles from `/apis/site/v2/sports/{sport}/{league}/news`. |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

## Notes

- ESPN does not require an API key for these public endpoints.
- `--interval-start` and `--interval-end` are converted to ESPN scoreboard `dates` when the URI does not include `dates`.
- Nested ESPN objects are preserved as JSON columns while common IDs, names, scores, status, standings, and article fields are exposed as typed columns.
