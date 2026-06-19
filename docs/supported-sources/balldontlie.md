# BallDontLie

[BallDontLie](https://www.balldontlie.io/) provides FIFA World Cup API endpoints for teams, stadiums, matches, players, rosters, lineups, events, and match analytics.

`ingestr` supports BallDontLie as a keyed source for richer World Cup data, including player and match-level tables.

For endpoint parameters, plan tiers, and implementation notes across the selected soccer providers, see the [BallDontLie source research](../soccer-sources/balldontlie.md).

## URI format

```plaintext
balldontlie://?api_key=<api-key>&season=2026
```

URI parameters:

- `api_key`: BallDontLie API key. Required.
- `season`: World Cup season. Supported values are `2018`, `2022`, and `2026`. Defaults to `2026`.
- `base_url`: Overrides the API base URL. Defaults to `https://api.balldontlie.io`.

## Example

Load World Cup 2026 players into DuckDB:

```sh
ingestr ingest \
  --source-uri 'balldontlie://?api_key=<api-key>&season=2026' \
  --source-table 'players' \
  --dest-uri 'duckdb:///worldcup2026.duckdb' \
  --dest-table 'soccer.players'
```

Load World Cup 2026 match events:

```sh
ingestr ingest \
  --source-uri 'balldontlie://?api_key=<api-key>&season=2026' \
  --source-table 'match_events' \
  --dest-uri 'duckdb:///worldcup2026.duckdb' \
  --dest-table 'soccer.match_events'
```

## Tables

| Table | PK | Inc Key | Inc Strategy | Details |
| --- | --- | --- | --- | --- |
| `teams` | `id` | - | replace | Loads World Cup teams. |
| `stadiums` | `id` | - | replace | Loads stadium metadata. |
| `group_standings` | `season_year`, `team_id` | - | replace | Loads group standings; nested team, group, and season objects are kept as JSON. |
| `matches` | `id` | - | replace | Loads matches; nested season, stage, group, stadium, team, referee, and manager objects are kept as JSON. |
| `players` | `id` | - | replace | Loads player profiles. |
| `rosters` | `season_year`, `team_id`, `player_id` | - | replace | Loads season rosters; the nested player object is kept as JSON. |
| `match_lineups` | `match_id`, `team_id`, `player_id` | - | replace | Loads match lineups; the nested player object is kept as JSON. |
| `match_events` | `id` | - | replace | Loads match incidents such as goals, cards, substitutions, and shootout events. |
| `player_match_stats` | `match_id`, `player_id` | - | replace | Loads player match statistics. |
| `team_match_stats` | `match_id`, `team_id` | - | replace | Loads team match statistics. |
| `match_shots` | `id` | - | replace | Loads shot-level data. |
| `match_momentum` | `match_id`, `minute` | - | replace | Loads match momentum data. |
| `match_best_players` | `match_id`, `player_id` | - | replace | Loads best-player summaries for matches. |
| `match_avg_positions` | `match_id`, `player_id` | - | replace | Loads average player positions by match. |
| `match_team_form` | `match_id`, `team_id` | - | replace | Loads team form data by match. |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

## Notes

- The API key is sent in the `Authorization` header.
- The source handles BallDontLie cursor pagination automatically.
- BallDontLie's free tier only includes `teams` and `stadiums`; `group_standings` requires ALL-STAR, and match/player/event tables require GOAT according to the provider docs.
