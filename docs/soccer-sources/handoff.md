# Soccer API Documentation Handoff

Date: 2026-06-06

## Scope Completed

- Removed previous `worldcup26` and `openfootball` source packages and docs from this workspace.
- Kept the existing BallDontLie FIFA source package and its supported-source docs.
- Added provider research docs for only the selected services:
  - `docs/soccer-sources/api-football.md`
  - `docs/soccer-sources/balldontlie-fifa.md`
  - `docs/soccer-sources/football-data-org.md`
- Added this handoff document and linked the research docs in the VitePress sidebar.
- Implemented the API-Football source package after the documentation pass.
- Added follow-up BallDontLie FIFA test coverage and live smoke-tested the free-tier tables.
- Implemented the football-data.org source package and supported-source docs.

All three selected soccer providers now have ingestr source implementations in this workspace.

## Provider Decisions

| Provider | Keep? | Reason |
| --- | --- | --- |
| BallDontLie FIFA | Yes | Existing source code already exists; docs map the official API and tiers. |
| API-Football | Yes | Comprehensive World Cup 2026 coverage using `league=1&season=2026`; account/key required. |
| football-data.org | Yes | Lightweight established API with World Cup in free coverage; deep player/event data is plan-dependent. |
| OpenFootball | No | Deleted from this workspace per request. |
| worldcup26.ir | No | Deleted from this workspace per request; unofficial fallback source is out of scope. |

## Documentation Sources Read

- BallDontLie FIFA docs: https://fifa.balldontlie.io/#fifa-world-cup-api
- BallDontLie OpenAPI spec URL listed in docs: https://www.balldontlie.io/openapi/fifa.yml
- API-Football docs: https://www.api-football.com/documentation-v3
- API-Football World Cup guide: https://www.api-football.com/news/post/fifa-world-cup-2026-guide-to-using-data-with-api-sports
- API-Football pricing: https://www.api-football.com/pricing
- football-data.org quickstart/API reference: https://www.football-data.org/documentation/quickstart
- football-data.org v4 docs: https://docs.football-data.org/general/v4/
- football-data.org coverage: https://www.football-data.org/coverage
- football-data.org pricing: https://www.football-data.org/pricing

## Main Table Mapping Summary

| Table | BallDontLie | API-Football | football-data.org |
| --- | --- | --- | --- |
| Teams | `GET /teams` | `GET /teams?league=1&season=2026` | `GET /competitions/WC/teams?season=2026` |
| Stadiums | `GET /stadiums` | Fixture-derived plus `GET /venues?id=...` | Derived from match/team `venue` fields |
| Group standings | `GET /group_standings` | `GET /standings?league=1&season=2026` | `GET /competitions/WC/standings?season=2026` |
| Matches | `GET /matches` | `GET /fixtures?league=1&season=2026` | `GET /competitions/WC/matches?season=2026` |
| Players | `GET /players`, `GET /rosters` | `GET /players?league=1&season=2026&page=1`, `GET /players/squads?team=...` | Team squads/person resources; no league-wide players endpoint |
| Match Events | `GET /match_events` | `GET /fixtures/events?fixture=...` | Derived from unfolded goals/bookings/substitutions |

## Implementation Update: BallDontLie FIFA

Already implemented in this workspace:

- Source package: `pkg/source/balldontlie_fifa`
- URI scheme: `balldontlie-fifa://?api_key=<key>&season=2026`
- Supported tables: `teams`, `stadiums`, `group_standings`, `matches`, `players`, `rosters`, `match_lineups`, `match_events`, `player_match_stats`, `team_match_stats`, `match_shots`, `match_momentum`, `match_best_players`, `match_avg_positions`, `match_team_form`
- Docs page: `docs/supported-sources/balldontlie-fifa.md`

Behavior notes:

- Auth uses the `Authorization` header.
- Cursor pagination is handled automatically.
- The connector always sends the configured `seasons[]` value, defaulting to `2026`.
- Nested provider objects are preserved as JSON where useful, while common IDs and names are flattened into typed columns.
- Error messaging now treats `401` and `403` as authentication or plan-access failures because BallDontLie uses those statuses for tier-gated endpoints.

Follow-up test coverage added:

- `rosters` nested season/player flattening and `"null"` string normalization.
- `match_events` nested player/assist/player-in/player-out flattening.
- `ReadOptions.Limit` and `ExcludeColumns` handling.
- API auth/plan-access error handling.
- Source registry registration.

Live smoke with the provided key:

- `teams` for `season=2026` loaded 48 rows to SQLite.
- `stadiums` for `season=2026` loaded 16 rows to SQLite.
- `group_standings` for `season=2026` returned `401`, consistent with BallDontLie's documented plan-gated table access.

## Implementation Update: API-Football

Implemented in this workspace:

- Source package: `pkg/source/api_football`
- URI scheme: `api-football://?api_key=<key>&league=1&season=2026`
- Supported tables: `teams`, `stadiums`, `group_standings`, `matches`, `players`, `match_events`
- Docs page: `docs/supported-sources/api-football.md`

Behavior notes:

- Auth uses the `x-apisports-key` header.
- `players` follows API-Football page pagination.
- `stadiums` is fixture-derived, then hydrates each fixture venue through `/venues?id=<id>`.
- `match_events` is fixture-derived, then fans out through `/fixtures/events?fixture=<id>`.
- `match_events` uses a deterministic `event_key` because API-Football event rows do not include their own event ID.
- Live smoke with the provided key succeeded for `league=1&season=2022` on the `teams` table and loaded 32 rows.
- Live smoke for `league=1&season=2026` was rejected by API-Football because the provided key is on a free plan without access to that future season.

Verification completed:

- `go test ./pkg/source/api_football`
- `go test ./pkg/source/api_football ./pkg/source/balldontlie_fifa`
- `go test -short ./...`
- `npm run docs:build`

## Implementation Update: football-data.org

Implemented in this workspace:

- Source package: `pkg/source/football_data_org`
- URI scheme: `football-data://?api_key=<token>&competition=WC&season=2026`
- Supported tables: `teams`, `stadiums`, `group_standings`, `matches`, `players`, `match_events`
- Docs page: `docs/supported-sources/football-data-org.md`

Behavior notes:

- Auth uses the `X-Auth-Token` header.
- The connector defaults to `competition=WC` and `season=2026`.
- `teams`, `group_standings`, and `matches` map directly to football-data.org competition endpoints.
- `stadiums` is derived from distinct team and match `venue` names because there is no first-class stadium endpoint.
- `players` loads competition teams, then hydrates `/teams/{id}` and flattens `squad` rows when the token has squad access.
- `match_events` fetches matches with `X-Unfold-Goals`, `X-Unfold-Bookings`, and `X-Unfold-Subs`, then normalizes goals, bookings, and substitutions into deterministic `event_key` rows.
- Optional match filters are supported through URI query parameters: `matchday`, `status`, `date_from`, `date_to`, `stage`, and `group`.
- Optional unfold headers for the `matches` table are controlled by `unfold_goals`, `unfold_bookings`, `unfold_subs`, and `unfold_lineups`.
- Deep player and event data is subscription-dependent; `401` and `403` are reported as authentication or plan-access failures.

Verification completed:

- `go test ./pkg/source/football_data_org`

Live smoke with a provided football-data.org token:

- `teams` for `competition=WC&season=2026` loaded 48 rows to SQLite.
- `matches` for `competition=WC&season=2026` loaded 104 rows to SQLite.
- `group_standings` for `competition=WC&season=2026` loaded 144 rows to SQLite.
- `stadiums` completed successfully with 0 rows because the current football-data.org WC 2026 team and match payloads do not include venue names yet.
- `match_events` completed successfully with 0 rows because the current WC 2026 match payloads have no unfolded goal, booking, or substitution events yet.
- `players` with `--sql-limit=1` loaded 1 squad row successfully, confirming team-detail squad access and flattening.
- A full `players` run hit football-data.org's live `429` rate limit while hydrating `/teams/{id}` for all teams. Full squad ingestion needs throttling or a higher-rate plan.

## Next Implementation Plan

1. Keep BallDontLie source as the reference implementation for keyed HTTP soccer providers.
2. Add request throttling for football-data.org full `players` hydration or document an operational rate-limit setting if throttling is made configurable.
3. Re-test football-data.org `stadiums` and `match_events` after venue names and played matches become available in the WC 2026 payloads.
4. Re-test API-Football `season=2026` with a paid key or another key that includes future-season access.
5. Re-test BallDontLie paid-tier tables (`group_standings`, `matches`, `players`, `match_events`, and match analytics) with a key whose plan includes those endpoints.
6. Run `go test -short ./...` after each connector implementation.

## Known Gaps

- API-Football's official docs page is behind Cloudflare for raw shell fetches, but indexed official snippets and the official World Cup guide provide the endpoint and parameter details needed for these docs.
- Some provider availability is subscription-dependent and must be verified with live keys.
- The provided BallDontLie key can access free-tier `teams` and `stadiums`, but not `group_standings`; paid-tier BallDontLie tables still need live verification with a higher-plan key.
- football-data.org does not expose a first-class stadium endpoint or unified event timeline, so those tables use derived extraction.
- football-data.org full squad hydration currently needs rate-limit handling because free-plan tokens can hit `429` while fetching all `/teams/{id}` details.
