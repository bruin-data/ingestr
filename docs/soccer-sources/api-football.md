# API-Football Source Research

Status: implemented in `pkg/source/api_football` with supported-source docs at `docs/supported-sources/api-football.md`.

Primary docs:

- API documentation: https://www.api-football.com/documentation-v3
- World Cup 2026 guide: https://www.api-football.com/news/post/fifa-world-cup-2026-guide-to-using-data-with-api-sports
- Pricing: https://www.api-football.com/pricing

## Connection

| Item | Value |
| --- | --- |
| Base URL | `https://v3.football.api-sports.io` |
| Auth | `x-apisports-key: <api-key>` request header |
| World Cup identifiers | `league=1`, `season=2026` |
| Response envelope | `get`, `parameters`, `errors`, `results`, `paging`, `response` |
| Pagination | Endpoint-specific. `players` uses `page`; some endpoints return one page for league/season filters. |
| Rate limits | Plan quota based. Free is 10 requests/minute and 100 requests/day; Pro 7,500/day; Ultra 75,000/day; Mega 150,000/day. |

API-Football states that all plans include all competitions and endpoints, with free plans limited by available seasons. For World Cup 2026, verify actual free-plan access with a live key before promising free production coverage.

## Tier Map

| Plan | Monthly price | Request quota | Endpoint availability |
| --- | ---: | ---: | --- |
| Free | $0 | 100/day | All endpoints, but available seasons are limited. |
| Pro | $19 | 7,500/day | All competitions and endpoints. |
| Ultra | $29 | 75,000/day | All competitions and endpoints. |
| Mega | $39 | 150,000/day | All competitions and endpoints. |

## Main Table Coverage

| Main table | Coverage | Primary endpoint | Required tier | Notes |
| --- | --- | --- | --- | --- |
| Teams | Yes | `GET /teams?league=1&season=2026` | Free or paid, subject to free season access | Response includes `team` and a nested `venue` object. |
| Stadiums | Partial/direct and fixture-derived | `GET /venues`, plus venues embedded in `/fixtures` and `/teams` | Free or paid, subject to free season access | There is no World Cup stadium list filter; derive venue IDs from fixtures, then hydrate with `/venues?id=...` when venue IDs are present. |
| Group standings | Yes | `GET /standings?league=1&season=2026` | Free or paid, subject to free season access | World Cup guide says this returns all 12 group tables. |
| Matches | Yes | `GET /fixtures?league=1&season=2026` | Free or paid, subject to free season access | Use `id` or `ids` for detail refresh; `ids` accepts up to 20 fixture IDs separated by `-`. |
| Players | Yes | `GET /players?league=1&season=2026&page=1` | Free or paid, subject to free season access | Paginated. Also consider `/players/squads?team=...` for squad-shaped extraction. |
| Match events | Yes | `GET /fixtures/events?fixture=<fixture_id>` | Free or paid, subject to free season access | The World Cup guide says fixture and event data updates every 15 seconds for live matches. |

## Endpoint Map

### Competition Coverage

`GET /leagues`

Use this as the first call for a connector run to verify that `league=1&season=2026` has the coverage flags expected for fixtures, events, lineups, fixture statistics, player statistics, standings, players, injuries, predictions, and odds.

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `id` | No | integer | Use `1` for World Cup. |
| `season` | No | YYYY integer | Use `2026` for World Cup 2026. |
| `team` | No | integer | Filter leagues by team ID. |
| `name` | No | string | Search by league name. |
| `country` | No | string | Filter by country. |
| `code` | No | 2-6 character string | Filter by country code. |
| `type` | No | `league` or `cup` | Filter by competition type. |
| `current` | No | `true` or `false` | Active seasons only. |
| `search` | No | string, 3+ chars | Search league or country. |
| `last` | No | integer, 2 digits max | Recently added leagues/cups. |

Recommended World Cup call:

```http
GET /leagues?id=1&season=2026
x-apisports-key: <api-key>
```

### Teams

`GET /teams`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `id` | No | integer | Single team by API-Football team ID. |
| `name` | No | string | Single team by exact-ish name. |
| `league` | No | integer | Use `1` for World Cup teams. |
| `season` | No | YYYY integer | Required with `league` for tournament teams. |
| `country` | No | string | Country name. |
| `code` | No | 3-character string | Team code. |
| `venue` | No | integer | Teams for a venue ID. |
| `search` | No | string, 3+ chars | Team or country search. |

Recommended World Cup call:

```http
GET /teams?league=1&season=2026
```

Ingestion shape: one `teams` row per `response[]`, with `id` lifted from `team.id` and the raw `team` and `venue` objects preserved as JSON columns (no field flattening). Do not treat team-home venue as World Cup match stadium without cross-checking fixtures.

### Stadiums / Venues

`GET /venues`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `id` | No | integer | Hydrate one venue ID. |
| `name` | No | string | Filter by venue name. |
| `city` | No | string | Filter by city. |
| `country` | No | string | Filter by country. |
| `search` | No | string, 3+ chars | Search name, city, or country. |

Recommended World Cup flow:

1. Fetch `GET /fixtures?league=1&season=2026`.
2. Extract unique `fixture.venue.id` values where non-null.
3. Fetch `GET /venues?id=<venue_id>` per venue if the fixture object does not contain enough stadium fields.

The official World Cup guide emphasizes 16 stadiums, but the generic `/venues` endpoint does not expose `league` or `season` filters, so stadium extraction should be fixture-derived.

### Group Standings

`GET /standings`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `season` | Yes | YYYY integer | Use `2026`. |
| `league` | No | integer | Use `1` for all World Cup group tables. |
| `team` | No | integer | Filter to a specific team. |

Recommended World Cup call:

```http
GET /standings?league=1&season=2026
```

Ingestion shape: one row per team per group from `league.standings[][]`. The composite key (`league_id`, `season`, `group_name`, `team_id`) is lifted to top-level columns; the raw `standing` object (with nested `all/home/away`) and the `league` object (minus its embedded `standings` array) are preserved as JSON columns.

### Matches

`GET /fixtures`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `id` | No | integer | Single fixture. Detail response includes events, lineups, fixture stats, and player stats when available. |
| `ids` | No | `id-id-id`, max 20 | Batch fixture details. |
| `live` | No | `all` or `leagueId-leagueId` | Live fixtures. |
| `date` | No | `YYYY-MM-DD` | Fixtures on one date. |
| `league` | No | integer | Use `1` for World Cup. |
| `season` | No | YYYY integer | Use `2026` with league/team/player filters. |
| `team` | No | integer | Fixtures for one team. |
| `last` | No | integer, 2 digits max | Last N fixtures. |
| `next` | No | integer, 2 digits max | Next N fixtures. |
| `from` | No | `YYYY-MM-DD` | Start date. |
| `to` | No | `YYYY-MM-DD` | End date. |
| `round` | No | string | Round name from `/fixtures/rounds`. |
| `status` | No | status code(s) | Examples include `NS`, `FT`, or combined values such as `1H-HT-2H-ET-P-BT-LIVE`. |
| `venue` | No | integer | Venue ID. |
| `timezone` | No | string | Timezone from `/timezone`; default response dates are usable as UTC-aware timestamps. |

Recommended World Cup call:

```http
GET /fixtures?league=1&season=2026
```

### Rounds

`GET /fixtures/rounds`

Use this as support data for match filtering and bracket navigation.

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `league` | Yes | integer | Use `1`. |
| `season` | Yes | YYYY integer | Use `2026`. |
| `current` | No | `true` or `false` | Return only active round. |
| `dates` | No | `true` or `false` | Include round dates. |
| `timezone` | No | string | Date timezone. |

### Players

`GET /players`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `id` | No | integer | Player ID. |
| `team` | No | integer | Team roster/player stats for a season. |
| `league` | No | integer | Use `1` for all World Cup players. |
| `season` | Conditionally | YYYY integer | Required with `id`, `league`, or `team`. |
| `search` | No | string, 4+ chars | Requires `league` or `team`. |
| `page` | No | integer | Pagination page; default `1`. |

Recommended World Cup call:

```http
GET /players?league=1&season=2026&page=1
```

Pagination continues until `paging.current == paging.total`.

### Player Squads

`GET /players/squads`

Use this when a squad-first model is preferred over the stats-heavy `/players` response.

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `team` | No | integer | Current squad for a team. |
| `player` | No | integer | Teams associated with a player. |

At least one of `team` or `player` is required.

### Match Events

`GET /fixtures/events`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `fixture` | Yes | integer | Fixture ID. |
| `team` | No | integer | Filter to one team. |
| `player` | No | integer | Filter to one player. |
| `type` | No | string | Event type filter. |

Event types documented by API-Football include goals, cards, substitutions, and VAR. Details include normal goal, own goal, penalty, missed penalty, yellow card, red card, goal cancelled, and penalty confirmed.

Recommended ingestion flow:

1. Fetch all World Cup fixtures.
2. For historical or scheduled refresh, call `/fixtures/events?fixture=<id>` per fixture.
3. For live refresh, call `/fixtures?live=all` or `/fixtures?league=1&season=2026&status=...`, then batch detail calls with `ids`.

## Implementation Notes

- URI: `api-football://?api_key=<key>&league=1&season=2026`, with optional `timezone` and `base_url`.
- Default `league=1` and `season=2026` for the World Cup use case.
- Schema is inferred from the data (`KnownSchema: false`); nested objects are preserved as JSON columns and only primary-key fields are lifted to typed top-level columns.
- Strategies: `merge` where incremental loading is possible — `matches`, `stadiums`, and `match_events` source from `/fixtures` and honor its `from`/`to` interval; `group_standings` carries a `standing.update` timestamp. `teams` and `players` have neither, so they use `replace` with a full fetch.
- Batches stream per response: one batch per page for `/players`, one per fixture for `match_events`.
- Server-side interval filtering uses the `/fixtures` `from`/`to` params (applied only when both `--interval-start` and `--interval-end` are set); other endpoints have no time filter.
- Rate limiter is set to ~80% of the free tier's 10 requests/minute (`rateLimit = 0.13` req/s, burst 5).
- Free plans cap the `/players` `page` parameter at 3, so full `players` extraction requires a paid plan.
- Store API-Football IDs as provider IDs; do not try to normalize team/player IDs across services in the first connector.
