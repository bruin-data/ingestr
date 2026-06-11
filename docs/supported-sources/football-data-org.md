# football-data.org

[football-data.org](https://www.football-data.org/) provides soccer competition data, including World Cup teams, fixtures, standings, and plan-dependent deep match and squad data.

`ingestr` supports football-data.org as a keyed source for FIFA World Cup data. The default configuration targets World Cup 2026 with `competition=WC` and `season=2026`.

For endpoint parameters, plan tiers, and implementation notes across the selected soccer providers, see the [football-data.org source research](../soccer-sources/football-data-org.md).

## URI format

```plaintext
football-data://?api_key=<api-token>&competition=WC&season=2026
```

URI parameters:

- `api_key`: football-data.org API token. Required.
- `competition`: Competition code or ID. Defaults to `WC`.
- `season`: Season year. Defaults to `2026`.
- `matchday`: Optional matchday filter for `matches`, `stadiums`, and `match_events`.
- `status`: Optional match status filter for `matches`, `stadiums`, and `match_events`.
- `date_from` / `date_to`: Optional date filters passed as `dateFrom` and `dateTo`.
- `stage`: Optional match stage filter.
- `group`: Optional group filter.
- `unfold_goals`, `unfold_bookings`, `unfold_subs`, `unfold_lineups`: Optional `true`/`false` flags for `matches`. These require plan access when football-data.org gates deep data.
- `base_url`: Overrides the API base URL. Defaults to `https://api.football-data.org/v4`.

## Example

Load World Cup 2026 matches into DuckDB:

```sh
ingestr ingest \
  --source-uri 'football-data://?api_key=<api-token>&competition=WC&season=2026' \
  --source-table 'matches' \
  --dest-uri 'duckdb:///worldcup2026.duckdb' \
  --dest-table 'soccer.matches'
```

Load derived World Cup 2026 match events:

```sh
ingestr ingest \
  --source-uri 'football-data://?api_key=<api-token>&competition=WC&season=2026' \
  --source-table 'match_events' \
  --dest-uri 'duckdb:///worldcup2026.duckdb' \
  --dest-table 'soccer.match_events'
```

## Tables

| Table | PK | Inc Key | Inc Strategy | Details |
| --- | --- | --- | --- | --- |
| `teams` | `id` | - | replace | Loads teams from `/competitions/<competition>/teams?season=<season>`. |
| `stadiums` | `venue_key` | - | replace | Derives distinct venue names from teams and matches. |
| `group_standings` | `competition_id`, `season_id`, `stage`, `standing_type`, `group_name`, `team_id` | - | replace | Loads and flattens `/competitions/<competition>/standings`. |
| `matches` | `id` | - | replace | Loads and flattens `/competitions/<competition>/matches`. |
| `players` | `team_id`, `id` | - | replace | Loads competition teams, hydrates `/teams/<id>`, and flattens squad members when plan access includes squads. |
| `match_events` | `event_key` | - | replace | Fetches matches with goal, booking, and substitution unfold headers, then normalizes those arrays into event rows. |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

## Notes

- The API token is sent in the `X-Auth-Token` header.
- football-data.org rate limits depend on the account plan; the free plan is 10 requests per minute.
- `players` and `match_events` depend on deep-data plan access. If the token cannot access squads or unfolded match arrays, the source returns the provider's authentication or plan-access error.
- `stadiums` is derived because football-data.org exposes venue names on team and match resources rather than a dedicated stadium endpoint.
- Nested provider objects are preserved as JSON columns while common IDs, names, scores, and status fields are exposed as typed columns.
