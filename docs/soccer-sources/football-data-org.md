# football-data.org Source Research

Status: implemented in `pkg/source/football_data_org` with supported-source docs at `docs/supported-sources/football-data-org.md`.

Primary docs:

- Quickstart/API reference: https://www.football-data.org/documentation/quickstart
- v4 docs: https://docs.football-data.org/general/v4/
- Coverage: https://www.football-data.org/coverage
- Pricing: https://www.football-data.org/pricing

## Connection

| Item | Value |
| --- | --- |
| Base URL | `https://api.football-data.org/v4` |
| Auth | `X-Auth-Token: <api-token>` request header |
| World Cup identifier | Competition code `WC`; competition ID is `2000` in current examples |
| Response shape | Resource-specific JSON objects, commonly with `filters`, `resultSet`, and resource arrays such as `matches`, `teams`, or `standings`. |
| Throttling | Free registered clients are 10 requests/minute; paid plans increase the per-minute quota. |
| Deep match fields | Controlled by headers: `X-Unfold-Lineups`, `X-Unfold-Bookings`, `X-Unfold-Subs`, and `X-Unfold-Goals`. |

World Cup appears in the free tier coverage list. Deep data such as lineups, substitutions, cards, goal scorers, and squads is paid-plan territory according to the pricing page.

## Tier Map

| Plan | Monthly price | Rate limit | Relevant coverage |
| --- | ---: | ---: | --- |
| Anonymous | $0 | 100 requests/24h | Area and competition list resources only. |
| Free | €0 | 10/min | 12 competitions including Worldcup; delayed scores/schedules, fixtures, league tables. |
| Free w/ Livescores | €12 | 20/min | Same 12 competitions with live scores. |
| ML Pack Light | €29 | 20/min | Adds advanced trend/form data and 10 seasons of history. |
| Free + Deep Data | €29 | 30/min | Adds lineups, substitutions, goal scorers, bookings/cards, and squads. |
| Standard | €49 | 60/min | 30 competitions plus deep data. |
| Advanced | €99 | 100/min | 50 competitions plus deep data. |
| Pro | €199 | 120/min | 100 competitions plus deep data. |
| Odds add-on | €15 | N/A | Pre-match home/draw/away odds; requires a regular plan. |
| Statistic add-on | €15 | N/A | Corners, free kicks, goal kicks, offsides, fouls, possession, saves, throw-ins, shots, cards; requires a regular plan. |

## Main Table Coverage

| Main table | Coverage | Primary endpoint | Required tier | Notes |
| --- | --- | --- | --- | --- |
| Teams | Yes | `GET /competitions/WC/teams?season=2026` | Free for basic team list; deep data for squads | Use `/teams/{id}` to hydrate a team and squad when plan allows. |
| Stadiums | Partial | Match/team `venue` string fields | Free for basic strings | No dedicated stadium resource with capacity/geo fields. Treat as a derived table from matches and teams. |
| Group standings | Yes | `GET /competitions/WC/standings?season=2026` | Free for league tables | Standings behavior differs by competition type; World Cup group tables should be flattened by `stage`, `type`, and `group`. |
| Matches | Yes | `GET /competitions/WC/matches?season=2026` | Free for fixtures/scores; live/deep fields need paid plan | Use unfold headers for goals, bookings, substitutions, and lineups. |
| Players | Partial | `/teams/{id}` squad, `/persons/{id}`, `/competitions/WC/scorers` | Deep data for squads/scorers | There is no league-wide players endpoint equivalent to API-Football. |
| Match events | Partial | Unfolded match fields via `X-Unfold-*` headers | Deep data plan | Events are separate arrays for goals, bookings, substitutions, and lineups rather than one timeline endpoint. |

## Endpoint Map

### Competitions

`GET /competitions`

Use to discover accessible competitions and confirm `WC` availability for the authenticated client.

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `areas` | No | comma-separated area IDs | Filter competition list by area. |

`GET /competitions/{id}`

Use `id` as either numeric ID or code such as `WC`.

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| Path `{id}` | Yes | integer or string code | Use `WC` for FIFA World Cup. |

The competition response includes available seasons, current season metadata, winner when known, and competition `plan`.

### Teams

`GET /competitions/{id}/teams`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| Path `{id}` | Yes | integer or string code | Use `WC`. |
| `season` | No | YYYY integer | Use `2026` for World Cup 2026. |

Recommended World Cup call:

```http
GET /competitions/WC/teams?season=2026
X-Auth-Token: <api-token>
```

`GET /teams/{id}`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| Path `{id}` | Yes | integer | Hydrate one team. |

Team detail includes base fields such as `id`, `name`, `shortName`, `tla`, `crest`, `address`, `website`, `founded`, `clubColors`, `venue`, running competitions, coach, squad, staff, and `lastUpdated` when available to the plan.

### Stadiums

football-data.org does not expose a first-class stadium/venue endpoint. Build a derived `stadiums` table by extracting and deduplicating:

- `matches[].venue` from `/competitions/WC/matches`.
- `team.venue` from `/competitions/WC/teams` or `/teams/{id}`.

Recommended columns for a first connector: `venue_name`, `source_context` (`match` or `team`), `team_id` nullable, `match_id` nullable, and provider raw JSON for future enrichment.

### Group Standings

`GET /competitions/{id}/standings`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| Path `{id}` | Yes | integer or string code | Use `WC`. |
| `season` | No | YYYY integer | Use `2026`. |
| `matchday` | No | integer | Snapshot by matchday. |
| `date` | No | `YYYY-MM-DD` | Snapshot by date. |

Recommended World Cup call:

```http
GET /competitions/WC/standings?season=2026
```

Flatten `standings[].table[]` into one row per team per standing block, preserving `stage`, `type`, `group`, `position`, `team`, `playedGames`, `form`, `won`, `draw`, `lost`, `points`, `goalsFor`, `goalsAgainst`, and `goalDifference`.

### Matches

`GET /competitions/{id}/matches`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| Path `{id}` | Yes | integer or string code | Use `WC`. |
| `season` | No | YYYY integer | Use `2026`. |
| `matchday` | No | integer | Matchday filter. |
| `status` | No | status enum | Example: `SCHEDULED`, `TIMED`, `IN_PLAY`, `PAUSED`, `FINISHED`, `LIVE`. |
| `dateFrom` | No | `YYYY-MM-DD` | Inclusive start date. |
| `dateTo` | No | `YYYY-MM-DD` | Exclusive end date in v4 behavior. |
| `stage` | No | stage enum | Example: `GROUP_STAGE`, `LAST_32`, `LAST_16`, `QUARTER_FINALS`, `SEMI_FINALS`, `THIRD_PLACE`, `FINAL`. |
| `group` | No | group enum | `GROUP_A` through `GROUP_L`. |

Recommended World Cup call:

```http
GET /competitions/WC/matches?season=2026
```

Optional deep-data headers:

| Header | Use |
| --- | --- |
| `X-Unfold-Lineups: true` | Include lineups in match list responses. |
| `X-Unfold-Bookings: true` | Include cards/bookings. |
| `X-Unfold-Subs: true` | Include substitutions. |
| `X-Unfold-Goals: true` | Include goals. |

`GET /matches`

Cross-competition match list.

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| `competitions` | No | comma-separated IDs/codes | Use `WC` when querying across competitions. |
| `ids` | No | comma-separated match IDs | Hydrate selected matches. |
| `date` | No | `YYYY-MM-DD` | Matches on one date. |
| `dateFrom` | No | `YYYY-MM-DD` | Start date, with `dateTo`. |
| `dateTo` | No | `YYYY-MM-DD` | End date. |
| `status` | No | status enum | Filter by match status. |

`GET /matches/{id}`

Fetch one match by ID. Use when detail refresh is cheaper or clearer than list refresh.

### Players

football-data.org models players as `Person` resources and squad members nested under teams/matches.

`GET /persons/{id}`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| Path `{id}` | Yes | integer | Hydrate one player/person. |

`GET /persons/{id}/matches`

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| Path `{id}` | Yes | integer | Person ID. |
| `dateFrom` | No | `YYYY-MM-DD` | Start date. |
| `dateTo` | No | `YYYY-MM-DD` | End date. |
| `status` | No | status enum | Filter by match status. |
| `competitions` | No | comma-separated IDs/codes | Use `WC` for World Cup. |
| `limit` | No | integer | Result limit. |
| `offset` | No | integer | Pagination offset. |

`GET /competitions/{id}/scorers`

Use this as a limited player-stat source for scorers/assists.

| Parameter | Required | Type | Use |
| --- | --- | --- | --- |
| Path `{id}` | Yes | integer or code | Use `WC`. |
| `season` | No | YYYY integer | Use `2026`. |
| `matchday` | No | integer | Compare scorer lists at a matchday. |

For a comprehensive `players` table, planned connector should start from `/competitions/WC/teams?season=2026`, then hydrate `/teams/{id}` and flatten the `squad` array if the selected plan includes squads.

### Match Events

There is no single `/events` endpoint. Build `match_events` from unfolded match arrays:

| Event family | Request header | Expected fields |
| --- | --- | --- |
| Goals | `X-Unfold-Goals: true` | minute, injury time, type, team, scorer, assist, score. |
| Bookings/cards | `X-Unfold-Bookings: true` | minute, team, player, card type. |
| Substitutions | `X-Unfold-Subs: true` | minute, team, player out/in. |
| Lineups | `X-Unfold-Lineups: true` | starting/bench players and team formations. |

Recommended ingestion flow:

1. Fetch `/competitions/WC/matches?season=2026` with unfold headers enabled when the key has deep-data access.
2. Create one normalized event row per item in `goals`, `bookings`, and `substitutions`.
3. Store lineups separately or use them to enrich player participation, because they are not chronological events.

## Implementation Notes

- Proposed URI: `football-data://?api_key=<token>&competition=WC&season=2026`.
- Use `X-Auth-Token`, not a query parameter.
- Default competition should be `WC` for this World Cup-focused source.
- Support `base_url` for tests.
- Treat stadiums and match events as derived tables because the API does not provide first-class stadium or unified event resources.
- Avoid promising player/event completeness on the free plan; mark deep player/event fields as subscription-dependent.
