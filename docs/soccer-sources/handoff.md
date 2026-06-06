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

No football-data.org ingester has been implemented yet.

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

## Next Implementation Plan

1. Keep BallDontLie source as the reference implementation for keyed HTTP soccer providers.
2. Implement football-data.org next:
   - URI: `football-data://?api_key=<token>&competition=WC&season=2026`.
   - Tables: same main six, but document `stadiums` and `match_events` as derived.
   - Test unfold headers and subscription-dependent deep data gracefully.
3. Re-test API-Football `season=2026` with a paid key or another key that includes future-season access.
4. Run `go test -short ./...` after each connector implementation.

## Known Gaps

- API-Football's official docs page is behind Cloudflare for raw shell fetches, but indexed official snippets and the official World Cup guide provide the endpoint and parameter details needed for these docs.
- Some provider availability is subscription-dependent and must be verified with live keys.
- football-data.org does not expose a first-class stadium endpoint or unified event timeline, so those tables will need derived extraction.
