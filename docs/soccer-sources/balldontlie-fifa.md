# BallDontLie FIFA Source Research

Status: BallDontLie FIFA source code exists in `pkg/source/balldontlie_fifa`. This document maps the official API surface for the selected main tables and should guide any follow-up schema changes.

Primary docs:

- API documentation: https://fifa.balldontlie.io/#fifa-world-cup-api
- OpenAPI spec: https://www.balldontlie.io/openapi/fifa.yml

## Connection

| Item | Value |
| --- | --- |
| Base URL | `https://api.balldontlie.io/fifa/worldcup/v1` |
| Auth | `Authorization: <api-key>` request header |
| Supported seasons | `2018`, `2022`, `2026` |
| Default season behavior | Most season-filtered endpoints default to `2026`; `players` and `rosters` return all editions when `seasons[]` is omitted. |
| Pagination | Cursor pagination with `per_page` max 100 and `cursor` for most list endpoints. |
| Errors | `401` auth/tier, `400` bad request, `404` not found, `406` non-json, `429` rate limited, `500/503` service errors. |

## Tier Map

| Tier | Price | Rate limit | Included selected endpoints |
| --- | ---: | ---: | --- |
| Free | $0/mo | 5 requests/min | `teams`, `stadiums` |
| ALL-STAR | $9.99/mo | 60 requests/min | `teams`, `stadiums`, `group_standings` |
| GOAT | $39.99/mo | 600 requests/min | All FIFA World Cup endpoints, including `matches`, `players`, `rosters`, `match_lineups`, `match_events`, and match stats |

Paid tiers are sport-specific. BallDontLie also offers a 48-hour GOAT trial for paid sports at the trial rate limit.

## Main Table Coverage

| Main table | Coverage | Primary endpoint | Required tier | Existing source table |
| --- | --- | --- | --- | --- |
| Teams | Yes | `GET /teams` | Free | `teams` |
| Stadiums | Yes | `GET /stadiums` | Free | `stadiums` |
| Group standings | Yes | `GET /group_standings` | ALL-STAR | `group_standings` |
| Matches | Yes | `GET /matches` | GOAT | `matches` |
| Players | Yes | `GET /players` | GOAT | `players` |
| Match events | Yes | `GET /match_events` | GOAT | `match_events` |

The existing BallDontLie source also supports `rosters`, `match_lineups`, `player_match_stats`, `team_match_stats`, `match_shots`, `match_momentum`, `match_best_players`, `match_avg_positions`, and `match_team_form`.

## Endpoint Map

### Teams

`GET /teams`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `seasons[]` | No | int array | Allowed values: `2018`, `2022`, `2026`; default `[2026]`. |

Response fields include `id`, `name`, `abbreviation`, `country_code`, and `confederation`.

### Stadiums

`GET /stadiums`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `seasons[]` | No | int array | Allowed values: `2018`, `2022`, `2026`; default `[2026]`. |

Response fields include `id`, `name`, `city`, `country`, `capacity`, `latitude`, and `longitude`.

### Group Standings

`GET /group_standings`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `seasons[]` | No | int array | Allowed values: `2018`, `2022`, `2026`; default `[2026]`. |

Response fields include nested `season`, `team`, and `group` objects plus `position`, `played`, `won`, `drawn`, `lost`, `goals_for`, `goals_against`, `goal_difference`, and `points`.

### Matches

`GET /matches`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `seasons[]` | No | int array | Allowed values: `2018`, `2022`, `2026`; default `[2026]`. |
| `match_ids[]` | No | int array | Filter to specific match IDs. |
| `team_ids[]` | No | int array | Filter to matches involving any listed team. |
| `per_page` | No | integer | Page size, max 100; default 25. |
| `cursor` | No | integer/string | Cursor for next page. |

Response fields include match ID, match number, UTC `datetime`, `status`, nested `season`, `stage`, `group`, `stadium`, home/away teams, team-source placeholders, scores by period, penalties, referee, managers, attendance, and winner metadata where available.

### Players

`GET /players`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `seasons[]` | No | int array | Allowed values: `2018`, `2022`, `2026`; when omitted, returns players across all editions. |
| `team_ids[]` | No | int array | Filter to players who appeared on listed teams. |
| `search` | No | string | Case-insensitive substring match on player name. |
| `per_page` | No | integer | Page size, max 100; default 25. |
| `cursor` | No | integer/string | Cursor for next page. |

Response fields include `id`, `name`, `short_name`, `position`, `date_of_birth`, `country_code`, `country_name`, `height_cm`, and `jersey_number`.

### Rosters

`GET /rosters`

Use this endpoint to tie players to teams and editions.

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `seasons[]` | No | int array | Allowed values: `2018`, `2022`, `2026`; when omitted, returns all editions. |
| `team_ids[]` | No | int array | Filter to team IDs. |
| `player_ids[]` | No | int array | Filter to player IDs. |
| `per_page` | No | integer | Page size, max 100; default 25. |
| `cursor` | No | integer/string | Cursor for next page. |

Response fields include nested `season`, `team_id`, nested `player`, `position`, `appearances`, `starts`, `minutes_played`, `goals`, `assists`, `yellow_cards`, `red_cards`, and `avg_rating`.

### Match Lineups

`GET /match_lineups`

Use this endpoint if the connector later expands "players" into per-match participation.

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `match_ids[]` | No | int array | Filter to match IDs. |
| `team_ids[]` | No | int array | Filter to team IDs. |
| `per_page` | No | integer | Page size, max 100; default 25. |
| `cursor` | No | integer/string | Cursor for next page. |

### Match Events

`GET /match_events`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `match_ids[]` | No | int array | Filter to match IDs. |
| `per_page` | No | integer | Page size, max 100; default 25. |
| `cursor` | No | integer/string | Cursor for next page. |

Event data covers match incidents such as goals, cards, substitutions, VAR-style events, and shootout-related events when present in the provider payload.

### Match Stats Endpoints

These are not in the requested main-table list, but the existing source already implements them and they are GOAT-tier according to the docs.

| Endpoint | Parameters | Existing table |
| --- | --- | --- |
| `GET /player_match_stats` | `match_ids[]`, `player_ids[]`, `team_ids[]`, `per_page`, `cursor` | `player_match_stats` |
| `GET /team_match_stats` | `match_ids[]`, `team_ids[]`, `per_page`, `cursor` | `team_match_stats` |
| `GET /match_shots` | `match_ids[]`, `player_ids[]`, `per_page`, `cursor` | `match_shots` |
| `GET /match_momentum` | `match_ids[]`, `per_page`, `cursor` | `match_momentum` |
| `GET /match_best_players` | `match_ids[]`, `per_page`, `cursor` | `match_best_players` |
| `GET /match_avg_positions` | `match_ids[]`, `team_ids[]`, `per_page`, `cursor` | `match_avg_positions` |
| `GET /match_team_form` | `match_ids[]`, `per_page`, `cursor` | `match_team_form` |

## Existing Connector Notes

- Current URI: `balldontlie-fifa://?api_key=<api-key>&season=2026`.
- Current connector sends `Authorization` and `seasons[]` on every table.
- Current connector handles cursor pagination and lets `ReadOptions.PageSize` reduce page size below the default.
- One follow-up to consider: `players` and `rosters` docs say omitted `seasons[]` has all-edition behavior, but the connector always sends the configured season. That is correct for the requested World Cup 2026 default.
