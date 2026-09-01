---
name: add-source
description: Implement a new ingestr source (API/SaaS connector) from scratch against a vendor's HTTP API. Use when a task asks to add a new source/connector, wire it into the URI registry, design its tables and incremental behavior, add its rate limiting, or write its tests. Covers researching the vendor API, creating the source package, the post-implementation review checklist, and verifying the output against the live account.
---

# Add an ingestr Source

This skill covers adding a new API / SaaS source. These are built from scratch against the
vendor's HTTP API, so there's nothing to diff against — the vendor's official docs and the
existing Go sources in `pkg/source/` are the source of truth.

## Workflow

1. Study the vendor API (section 1). Don't write Go until endpoints, pagination, filtering,
   and the table set are mapped out.
2. Ask the user for test-account credentials (section 2).
3. Look up rate limits and compute limiter values (section 3).
4. Create the source package (section 4), then run `make generate` and `make format`.
5. Work through the review checklist (section 5) and fix what it turns up.
6. Verify the output against the live account (section 6) and write tests (section 7).
7. Add docs and report the per-table design back to the user (section 8).

## 1. Study the vendor's API first

The most important step. Read the official docs and map out, per endpoint:

- URL, query params, required headers/auth.
- Pagination style — per endpoint, not per API. Some return everything in one call; others
  use cursor, page-number, or offset pagination.
- Which endpoints take time-based filter params, and the exact operator/field (e.g.
  `updated_since`, `from`/`to`, `>=` vs `>`). Operators differ per endpoint — check each.
- Response shape — nested envelope vs flat array, and which objects are deeply nested.
- Rate limits (section 3).

Then decide the table set and per-table behavior:

- Table names, primary key(s), incremental key, merge vs replace (see the checklist rules).
- Don't flatten nested objects. Pass each provider object through as-is so it lands as a JSON
  column; only lift the primary-key field(s) to top-level columns.
- Every URI param you expose must be wired end to end: parse → store on the struct → use in a
  real request. A parsed-but-unused param is a bug.
- Validate constrained params (e.g. `environment` must be `production`/`sandbox`) with a clear
  error.

Read 2-3 existing sources as reference — e.g. `attio` for schema inference, plus one that
paginates and one that does server-side date filtering. Reuse their patterns and helpers
instead of inventing new ones.

## 2. Ask the user for test-account credentials

Testing needs a real account with data. Ask the user for the credentials (API key/token,
client ID/secret, account ID, etc.) for an account that has at least one record in every table
you plan to support. They're only needed locally while building and testing. Never commit
credentials or tokens.

## 3. Look up API rate limits

Find the vendor's rate-limit docs. Note the limit type (per-minute, per-second, concurrent)
and the actual numbers. If different endpoints have different limits, give each group its own
HTTP client and limiter — don't apply the lowest limit to everything.

Compute `rateLimit` / `rateLimitBurst` at ~80% of the documented limit:

- Per-minute: `rateLimit = (limit * 0.8) / 60.0`
- Per-second: `rateLimit = limit * 0.8`
- `rateLimitBurst` is usually 5. For low per-minute caps, size it so `burst + rateLimit*60`
  stays under the cap — e.g. a 10 req/min tier needs burst ≤ 2 (burst 5 + ~7.8 refilled ≈ 12.8
  in the first minute, over the cap).

Comment the `rateLimit` constant with the vendor's actual limit. If the connector only targets
a free tier, don't document the full plan-quota table — it implies behavior the limiter
doesn't act on.

## 4. Create the source package

Create `pkg/source/<source>/` with `register.go` and `<source>.go`.

`register.go` self-registers the source in an `init()`. Without it the source is unreachable
from a URI even after `make generate`. Match the existing sources:

```go
package <source>

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"<scheme>"},
		func() interface{} { return New<Source>Source() },
	)
}
```

In `<source>.go`, using existing sources as reference, implement: `constants`,
`supportedTables`, the source struct, `New*Source()`, `HandlesIncrementality`, `Schemes`,
`Connect`, `Close`, `parseURI`, `GetTable`, `isValidTable`, the `read` dispatcher, and
`paginateAndSend`.

### OAuth token refresh

If the vendor uses OAuth, accept both a ready `access_token` and the refresh credentials
(`client_id` / `client_secret` / `refresh_token`) in the URI. When no access token is given,
mint one in `Connect` from the refresh credentials via a `refreshAccessToken` helper, then set
it with `httpclient.WithAuth(...)`. Access tokens usually expire in hours; the refresh token is
long-lived. See `redditads` and `quickbooks`.

### Schema design — prefer inference over hand-written types

- Set `KnownSchema: false` and let the pipeline infer types. Build records with
  `arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)` — nil columns means
  every field is emitted as Unknown and inferred. Don't hand-maintain column type blocks;
  `SchemaFn` can just return an error.
- Don't flatten nested objects (inference maps `map`/`[]` to JSON). Only lift the primary-key
  field(s) to top-level columns so merge can dedupe (e.g. `id` from `team.id`).
- Stream one batch per response/page/fan-out unit via the `results` channel. Never buffer the
  whole result set into one slice before emitting.
- Bound each batch by bytes (see below) whenever you accumulate rows before emitting.
- Comment only where the code isn't self-explanatory — a non-obvious API quirk, or the "why"
  behind a choice. Keep comments to at most 2 lines.

### Bounding a batch by bytes

Any loop that accumulates rows into a slice before emitting must honor `opts.MaxBatchBytes`
(the cap derived from `--batch-size`). Before appending a row that would push the batch over
the cap, flush what you have and reset the byte counter with the batch. Use
`arrowconv.RowBytes(item)` for the estimate — never hand-roll a size calc.

```go
if opts.MaxBatchBytes > 0 {
    rowBytes := arrowconv.RowBytes(item)
    if len(batch) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
        flush() // emit batch, then inside flush: batch = nil; accBytes = 0
    }
    accBytes += rowBytes
}
batch = append(batch, item)
```

Guard every check with `opts.MaxBatchBytes > 0`. Zero means "no cap" and only exists as the
struct default — the CLI rejects a non-positive `--batch-size` and defaults to 512 MiB, so in
practice it's always positive. Keep `accBytes` a function-local `var` and reset it with the
batch on every flush path (in-loop, trailing-after-loop, error) — a missed reset leaks the
count into the next batch. A source that emits exactly one row per response and never grows a
slice can skip the cap.

### Query params on the table string

When a `source_table` carries URL-style query params (e.g. `boards?board_ids=1,2&linked=true`),
don't hand-split it. Use the shared `pkg/tablespec` parser:

```go
var p mondayParams // struct with mapstructure tags
path, hasParams, err := tablespec.Parse(name, &p, tablespec.WithListSeparator(","))
```

`Parse` returns the base path, decodes params onto your struct via `mapstructure` tags, coerces
types (repeated/separator-joined → `[]string`, bare flag → `bool`, dotted keys → nested), and
rejects unknown keys so a typo errors instead of being silently dropped. When `hasParams` is
false, fall back to your legacy table-string parsing. See `monday`, `adjust`, `sharepoint`.

### Per-table read functions

For each table in `supportedTables`, implement a `read<TableName>`:

```go
func (s *XxxSource) readTableName(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[XXX] reading table_name")
	// ...
	return nil
}
```

`opts source.ReadOptions` carries what each read needs: the incremental window
`IntervalStart` / `IntervalEnd` (both `*time.Time`; nil means unbounded on that side) and
`IncrementalKey`, plus `MaxBatchBytes` and `ExcludeColumns`. That's how "the interval" below
reaches a table — pass it as a query param, or filter on it client-side.

Then run:

```bash
make generate   # blank-imports the new package so its init() runs
make format
```

Finally, add the scheme to the hand-maintained catalog in `internal/server/connectors.go`
(`GetConnectors()`) — e.g.
`genericURIConnector("trello", "Trello", []string{"trello"}, true, false)`. `register.go` only
wires URI resolution; `TestConnectorCatalogCoversRegisteredSchemes` fails without the catalog
entry.

## 5. Post-implementation review checklist

First re-check the whole implementation against the vendor docs — table names, endpoints,
primary keys, incremental keys, strategies, special handling (filters, sub-types, field
normalization). Fix discrepancies before continuing.

### Correctness

- [ ] **Matches the vendor API**: table names, endpoints, PKs, incremental keys, strategies.
- [ ] **Returns everything**: many endpoints default to only active/open items. Pass the param
  that returns the full set — Trello `filter=all`, or `include_archived` / `state=all` /
  `deleted=true` elsewhere — and confirm by archiving a record.
- [ ] **Every parsed URI param is used**: trace each one `parseURI` → credentials struct →
  source struct → API request.
- [ ] **Constrained params validated** with a clear error.
- [ ] **No `opts.Limit` in API sources**: the pipeline handles row limiting. Only database/SQL
  sources use it.
- [ ] **Merge vs replace, per table**: use `merge` only when the table can load incrementally —
  its endpoint accepts a time filter (directly or via a parent), its rows carry an update
  timestamp, or its rows are append-only/immutable with a stable PK. Otherwise `replace` with a
  full fetch. Avoid `append`: it's a plain insert that duplicates on re-run — prefer `merge`
  keyed on a stable PK for append-only data.
- [ ] **Incremental filtering decided per table**: server-side (query params, exact
  operator/field), client-side (fetch all, filter in code), none (replace), or special
  (sub-type loops, custom filters).
- [ ] **Streaming, not buffering**: records go to `results` as each page arrives.
- [ ] **Context cancellation**: every pagination loop checks `ctx.Done()` at the top and
  returns `ctx.Err()`.

### Pagination & limits

- [ ] **`maxPageSize` constant** controls page size — no inline magic numbers.
- [ ] **`maxPages` guard** if the API can return unbounded pages without a `next` cursor, with a
  `config.Debug` log when it triggers.
- [ ] **Correct pagination style** (cursor vs offset).
- [ ] **Batch bounded by `opts.MaxBatchBytes`** on any loop that accumulates rows (section 4).

### Performance

- [ ] **Parallelism considered**: if the API supports fetching independent sub-resources in
  parallel (per-object records, per-list entries), use a worker pool.
- [ ] **Filtering chosen per table**, in this order:
  1. Endpoint accepts time-based params → pass the interval as a query param (server-side).
  2. Else objects include a date field → filter client-side via `filterItemsByInterval`.
  3. Else fetch all records unfiltered.
  4. Table derived from another endpoint → apply the interval on the parent that accepts it, and
     thread `opts` through to the parent fetch.

### Robustness

- [ ] **Rate limiter applied**: `httpclient.WithRateLimiter(rateLimit, rateLimitBurst)` (from
  `pkg/http`) is set in `Connect`, with `rateLimit` commented with the vendor's limit.
- [ ] **Errors name the endpoint/table** (e.g. `"failed to fetch tickets: %w"`).
- [ ] **Non-success HTTP handled**: every response checks `resp.IsSuccess()` and returns a
  descriptive error with status code and body.
- [ ] **Large integer precision**: decode with `decoder.UseNumber()` if IDs/counts may exceed
  float64 precision.

### Parallel mode safety

Parallel reads are a per-source pattern, not a shared helper — there's no repo-wide
`readParallel`. If a table benefits from splitting its time range across workers, write a
source-local one (see `klaviyo`, or `stripe`'s `readParallelAdaptive`). Most sources don't need
one.

- [ ] **Only parallelize tables that support BOTH start AND end server-side filters.** It splits
  the range into non-overlapping windows; if the API rejects the end filter, workers fetch
  overlapping data and produce duplicates. Verify both operators before parallelizing; otherwise
  call the read function directly.
- [ ] **Test parallel mode with a wide date range** (1+ year) so it actually splits into
  multiple workers, and compare the row count against a single-worker run to confirm no
  duplicates.

### Consistency

- [ ] **Follow existing patterns**: before adding any pattern (error handling, test utils, URI
  construction), check how 2-3 existing sources do it. Don't introduce a pattern no other source
  uses.
- [ ] **Every source-struct field is used.** Dead fields are bugs waiting to happen.

## 6. Verify the output against the live account

The running source is the source of truth. Run it into DuckDB and inspect every table. This is
local verification only — don't commit the captured data as fixtures (it's the user's account
data; see section 7).

1. Run each table:
   ```bash
   go run . ingest --source-uri="<source_uri>" --source-table=<table> \
     --dest-uri="duckdb:///tmp/<source>_<table>.duckdb" --dest-table=main.<table> --yes
   ```
2. `DESCRIBE` the table — confirm every non-JSON column has the right type and nested
   objects/arrays landed as JSON (not flattened or stringified).
3. `SELECT COUNT(*)` and compare against the vendor's raw API (walk all pages) and the UI.
4. Spot-check a known record (`SELECT * WHERE <pk>='<id>'`) against the raw API — watch
   timestamp unit/precision, integer precision, nulls.
5. Report a per-table summary: schema, row count, strategy, anything surprising. Any mismatch is
   a bug — fix it and re-run.

## 7. Tests

Only commit tests that exercise pure logic — never tests that embed data from the test account.
Its row counts, field values, and schemas must not be baked into fixtures. Validate against the
live account locally instead (section 6).

Unit tests go in `pkg/source/<source>/<source>_test.go` (same package, not `_test`):

- `TestParseURI`: valid URI, each required field missing, wrong scheme, edge cases.
- `TestIsValidTable`: every supported table true; unknown/empty/wrong-case false.
- Any source-specific parsing helpers with normal, edge-case, and invalid inputs.
- `TestJsonUseNumber` if the source uses `decoder.UseNumber()`.

For incremental logic (local verification, not committed): for one server-side and one
client-side filtered table, run against the real API and confirm by hand that (1) a range
covering the data returns the expected rows, (2) a range before the data returns 0 rows, (3) no
interval returns all records.

Run `make format`, `make lint`, and `make test` when done.

## 8. Docs & reporting

- Add a user-facing page under `docs/supported-sources/<source>.md` describing the data and
  usage — not query params, filtering, or rate-limit mechanics.
- Report a per-table breakdown to the user: filtering type (server/client/none) and exact
  syntax, incremental key and strategy, rate-limit tier, whether parallelism is on, the default
  behavior when no interval is given, and any notable design decisions or deviations from the
  common per-source helpers (e.g. `paginateAndSend`).
