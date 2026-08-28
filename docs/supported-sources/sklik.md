# Sklik

[Sklik](https://www.sklik.cz/) is the paid-search advertising platform of Seznam.cz, the dominant Czech search engine.

ingestr supports Sklik as a source through the [Sklik API](https://api.sklik.cz/drak/), a JSON-RPC interface.

## URI format

```plaintext
sklik://?token=<api_token>
```

URI parameters:

- `token`: Required. The permanent API token generated in the Sklik UI.

The token belongs to a single Sklik account. To load several accounts into one destination table, run ingestr once per token; every row carries a `_user_id` column identifying the account it came from (see [Account column](#account-column)).

Sklik's API is session-based: `client.loginByToken` exchanges the permanent token for a rolling session that changes on every response. The connector handles this, including re-login when a session expires mid-run.

## Example usage

```bash
ingestr ingest \
  --source-uri 'sklik://?token=<api_token>' \
  --source-table campaigns \
  --dest-uri duckdb:///sklik.duckdb \
  --dest-table main.campaigns
```

Report tables accept `--interval-start` / `--interval-end` to bound the date window:

```bash
ingestr ingest \
  --source-uri 'sklik://?token=<api_token>' \
  --source-table campaign_stats_daily \
  --interval-start 2026-01-01 \
  --dest-uri duckdb:///sklik.duckdb \
  --dest-table main.campaign_stats_daily
```

## Entity tables

Snapshots of current state. They have no date dimension — each run returns the account as it stands now.

| Table | Primary key | Strategy | Data |
|---|---|---|---|
| `campaigns` | `id` | merge | Campaigns with budget, status and settings |
| `groups` | `id` | merge | Ad groups |
| `ads` | `id` | merge | Ads |
| `keywords` | `id` | merge | Keywords, including match type and bids |
| `conversions` | `id` | merge | Conversion definitions |

## Report tables

One row per entity per day, built through Sklik's asynchronous `createReport` / `readReport` pair.

| Table | Primary key | Incremental key | Strategy | Data |
|---|---|---|---|---|
| `campaign_stats_daily` | `id`, `date` | `date` | merge | Daily campaign performance |
| `search_queries` | `query`, `keyword_id`, `date` | `date` | merge | Search terms that triggered ads |

`campaign_stats_daily` exposes `impressions`, `clicks`, `ctr`, `avgCpc`, `conversions`, `conversionValue`, `transactions`, `clickMoney`, `impressionMoney`, `totalMoney`, `pno`, `ish`, `exhaustedBudget` and `stoppedBySchedule`.

`search_queries` includes `keyword_id` in its primary key because Sklik reports the same search term once per matching keyword. Keying on `(query, date)` alone would keep one arbitrary keyword's row per query and silently drop the rest of the spend.

## Account column

Every row gets a `_user_id` column holding the Sklik account id. Sklik never returns it in a row payload — ingestr adds it, so the underscore prefix marks it as ingestr-owned, the same convention as `_ingestr_loaded_at`.

It is deliberately **not** part of any primary key. If you load several accounts into one destination table and two of them ever share an entity id, the merge would keep only one of the rows. Loading each account into its own table avoids the question entirely.

## Notes and limitations

### Empty reports and throttling

Sklik rate-limits per account per minute, and when you exceed the limit **the report comes back as a successful, empty response** rather than an error:

```json
{"status":200,"statusMessage":"OK","report":[]}
```

There is no `429` or error message, so throttling and a genuinely empty report are indistinguishable. When `createReport` returns a positive `totalCount` but `readReport` has no rows, ingestr retries with linear backoff for about seven minutes before returning an error.

### Search-term reports must be scoped to campaigns

`queries.createReport` returns an empty report with `status: 200` unless the restriction filter scopes it to campaigns, groups or keywords. ingestr scopes `search_queries` reports to the account's campaigns.

### `displayColumns` enums are per-endpoint

Each `*.readReport` method has its own enum of valid `displayColumns`. Sklik returns `400 Bad arguments` if any requested column is invalid. For example, `ctr` is valid for `campaigns.readReport` but not `queries.readReport`, so calculate search-query CTR from `clicks` and `impressions` instead.

### Request size

Sklik rejects requests that ask for too much data at once with `406 You are requiring too much data in one request`. ingestr pages reports at 50 entities and caps the rows requested per call, sizing the window automatically from the interval you ask for.
