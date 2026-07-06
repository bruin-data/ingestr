---
name: add-source
description: Implement a new ingestr source (API/SaaS connector) from scratch against a vendor's HTTP API. Use when a task asks to add a new source/connector, wire it into the URI registry, design its tables and incremental behavior, add its rate limiting, or write its tests. Covers researching the vendor API, creating the source package, the post-implementation review checklist, and verifying the output against the live account.
---

# Add an ingestr Source

This skill covers adding a new **API / SaaS source** to ingestr. These sources are
implemented **from scratch** against the vendor's HTTP API — there is no prior
implementation to mirror or diff against, so the vendor's official API docs and the
existing Go sources in `pkg/source/` are the source of truth.


## Workflow

1. **Study the vendor API first** (see below). Do not write any Go until the endpoints,
   pagination, filtering, and table set are mapped out.
2. **Ask the user for test-account credentials** to use during implementation and testing.
3. **Look up rate limits** and compute safe limiter values.
4. **Create the source package** under `pkg/source/<source>/` with `register.go` and
   `<source>.go`.
5. **Run `make generate` and `make format`.**
6. **Work through the review checklist** below and fix discrepancies.
7. **Verify the output** by running the source into DuckDB and checking it against the
   live account and the vendor API/UI.
8. **Report the per-table design** (filtering, strategy, incremental key) to the user.

## 1. Study the vendor's API BEFORE writing any code

This is the most important step.

1. **Read the vendor's official API docs** and map out, per endpoint:
   - URL, query params, and required headers/auth.
   - **Pagination style per endpoint** — do NOT assume all endpoints paginate the same
     way. Some return everything in one call; others use cursor, page-number, or offset
     pagination.
   - Which endpoints accept **time-based query params** for server-side filtering, and the
     exact operator/field (e.g. `updated_since`, `from`/`to`, `greater-or-equal` vs
     `greater-than`). Operators can differ per endpoint — verify each.
   - Response shape: which endpoints wrap data in a nested envelope vs return a flat array;
     which objects are deeply nested.
   - Rate limits (see step 3).
2. **Decide the table set and per-table behavior** from the API:
   - Table names, primary key(s), incremental key, and merge/replace strategy (see the
     decision rules in the checklist).
   - Nested/complex objects: do NOT flatten or extract fields — pass each provider object
     through as-is so it lands as a JSON column. Only lift the primary-key field(s) to
     top-level columns.
   - **Every URI parameter you expose must be wired through**: parse → store on the source
     struct → use in an actual API request. A parsed-but-unused parameter is a bug.
   - **Validate optional parameters** where the API constrains them (e.g. restrict
     `environment` to `production`/`sandbox`) and return a clear error for invalid values.
3. **Study 2-3 existing sources** in `pkg/source/` as reference — e.g. `attio` for schema
   inference, plus one that paginates and one that does server-side date filtering. Match
   their structure and reuse the same per-source patterns and helper functions rather than
   inventing new ones.

## 2. Ask the user for test-account credentials

Testing a source needs a real account with data. **Ask the user to provide the credentials**
(API key/token, client ID/secret, account ID, etc.) for a test account that has at least one
record in **every** table you plan to support.

The credentials are only needed while implementing and testing the source (building the
source URI, running it into DuckDB, and running the tests). **Never commit credentials or
tokens to the repository.**

## 3. Look up API rate limits

- Find the vendor's official rate-limit docs. Identify the limit type (per-minute,
  per-second, concurrent) and the actual numbers.
- **Check whether different endpoints have different limits.** If they do, use separate
  HTTP clients with separate rate limiters per group — do not apply the lowest limit to all
  endpoints.
- Compute safe `rateLimit` / `rateLimitBurst` values (~80% of the documented limit):
  - Per-minute limits: `rateLimit = (limit * 0.8) / 60.0`
  - Per-second limits: `rateLimit = limit * 0.8`
  - `rateLimitBurst` is typically 5, but for low per-minute caps size it so
    `burst + rateLimit*60` stays under the per-minute limit. Example: a 10 req/min tier
    needs burst ≤ 2, because burst 5 plus ~7.8 refilled tokens ≈ 12.8 requests in the first
    minute, over the cap.
  - If the connector targets only a free tier, don't advertise the full plan-quota table in
    the docs — it implies behavior the limiter doesn't act on.
- Add a comment on the `rateLimit` constant explaining the vendor's actual limit.

## 4. Create the source package

Create `pkg/source/<source>/` with `register.go` and `<source>.go`.

`register.go` self-registers the source in an `init()` by calling
`registry.RegisterSource` with its URI scheme(s) and a constructor. Without this the source
is unreachable from a URI even after `make generate`. Match the existing sources exactly:

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

In `<source>.go`, using existing sources as reference, implement:
`constants`, `supportedTables`, the source struct, `New*Source()`, `HandlesIncrementality`,
`Schemes`, `Connect`, `Close`, `parseURI`, `GetTable`, `isValidTable`, the `read`
dispatcher, and `paginateAndSend`.

**Schema design — prefer inference over hand-written types** (see `attio` as reference):
- Set `KnownSchema: false` and let the pipeline infer types from the data: build records
  with `arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)` (nil
  columns → every field is emitted as Unknown and inferred). Do NOT hand-maintain column
  type blocks. `SchemaFn` can just return an error.
- Do NOT flatten nested objects. Pass each provider object through as-is so it lands as a
  JSON column (inference maps `map`/`[]` to JSON). Only lift the primary-key field(s) to
  top-level columns so merge can de-duplicate (e.g. `id` from `team.id`).
- Stream one batch per response/page/fan-out unit via the `results` channel; never buffer
  the whole result set into one slice before emitting.
- **Comments:** add them only where the code isn't self-explanatory (a non-obvious API
  quirk, or the "why" behind a choice). Keep each comment to at most 2 lines.

For each table in `supportedTables`, implement a `read<TableName>` function:

```go
func (s *XxxSource) readTableName(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[XXX] reading table_name")
	// ...
	return nil
}
```

Then run:

```bash
make generate   # adds a blank import of the new package to internal/registry/imports/imports.gen.go so its init() runs
make format
```

## 5. Post-implementation review checklist

First re-check the whole implementation against the vendor's API docs: verify table names,
endpoints, primary keys, incremental keys, strategies, and any special handling (filters,
sub-types, field normalization). Fix discrepancies before continuing.

### Correctness
- [ ] **Matches the vendor API**: table names, endpoints, primary keys, incremental keys,
  strategies are correct per the docs.
- [ ] **Every parsed URI param is actually used**: trace each param `parseURI` →
  credentials struct → source struct → API request. Parsed-but-never-sent is a bug.
- [ ] **Optional param validation**: constrained optional params (e.g. environment must be
  `production`/`sandbox`) are validated with a clear error.
- [ ] **No `opts.Limit` in API sources**: the pipeline handles row limiting. Only
  database/SQL sources use `opts.Limit`.
- [ ] **Merge vs replace per table**: use `merge` only when the table can be loaded
  incrementally — its endpoint accepts a time filter (directly or via a parent endpoint),
  OR its rows carry an update timestamp, OR its rows are append-only/immutable with a stable
  primary key. Otherwise use `replace` with a full fetch. `append` is a plain insert with no
  dedup and duplicates on re-run — prefer `merge` keyed on a stable PK for append-only data.
- [ ] **Incremental filtering decided per table**: for EVERY table decide server-side
  (query params, with exact operator/field), client-side (fetch all, filter in code), no
  filtering (replace), or special logic (sub-type loops, custom filters). Do not assume all
  endpoints support the same operators.
- [ ] **Streaming, not buffering**: records go to `results` as each page arrives.
- [ ] **Context cancellation**: every pagination loop checks `ctx.Done()` at the top of
  each iteration and returns `ctx.Err()`.

### Pagination & limits
- [ ] **`maxPageSize` constant** controls page size — no magic numbers inline.
- [ ] **`maxPages` guard** if the API can return unbounded pages without a `next` cursor,
  plus a `config.Debug` log when it triggers.
- [ ] **Cursor vs. offset**: uses the correct pagination style for the API.

### Performance
- [ ] **Parallelism considered**: if the API supports fetching independent sub-resources in
  parallel (per-object records, per-list entries), use a worker pool.
- [ ] **Server-side vs. client-side filtering** per table:
  1. If the endpoint accepts time-based query params, pass the interval as a query param
     (server-side).
  2. Else if response objects include a date field, apply client-side filtering via
     `filterItemsByInterval`.
  3. Else fetch all records without filtering.
  4. For tables derived from another endpoint, apply the interval on the parent endpoint
     that accepts it, and thread `opts` through to the parent fetch.

### Robustness
- [ ] **Rate limiter applied**: `httpclient.WithRateLimiter(rateLimit, rateLimitBurst)`
  (from `pkg/http`) is set in `Connect`, with the `rateLimit` constant commented with the
  vendor's documented limit.
- [ ] **Error messages include the endpoint/table name** (e.g. `"failed to fetch tickets: %w"`).
- [ ] **Non-success HTTP status handled**: every response checks `resp.IsSuccess()` and
  returns a descriptive error with the status code and body.
- [ ] **Large integer precision**: decode JSON with `decoder.UseNumber()` if the API
  returns IDs/counts that may exceed float64 precision.

### Parallel mode safety
Parallel reads are a **per-source pattern**, not a shared helper — there is no repo-wide
`readParallel` to call. If a table benefits from splitting its time range across workers,
implement a source-local `readParallel` (see `klaviyo`, or `stripe`'s `readParallelAdaptive`,
for reference); most sources don't need one.
- [ ] **Only add a parallel read for tables that support BOTH start AND end server-side
  filters.** It splits the time range into non-overlapping windows; if the API rejects the
  end filter, workers fetch overlapping data and produce duplicates. Verify both operators
  are accepted before parallelizing a table; otherwise call the read function directly.
- [ ] **Test parallel mode with wide date ranges** (1+ year) so the range actually splits
  into multiple workers, and compare row count against a single-worker run to confirm no
  duplicates.

### Consistency with existing sources
- [ ] **Follow existing patterns**: before adding any pattern (error handling, test utils,
  URI construction), check how 2-3 existing sources do it. Don't introduce a new pattern no
  other source uses.
- [ ] **Every source-struct field is actively used.** Dead fields are bugs waiting to happen.

## 6. Verify the output against the live account

The running source IS the source of truth. Run it into DuckDB and inspect it to confirm the
schema, row counts, and column types are correct for **every** table. This is a **local
verification step only** — do NOT commit the captured data as test fixtures (the account is
the user's, so its data must not land in the repo; see Tests).

1. Run the source per table:
   ```bash
   go run . ingest --source-uri="<source_uri>" --source-table=<table> \
     --dest-uri="duckdb:///tmp/<source>_<table>.duckdb" --dest-table=main.<table> --yes
   ```
2. Inspect the schema (`DESCRIBE`) — confirm every non-JSON column has the right type and
   nested objects/arrays landed as JSON (not flattened or stringified).
3. Get the row count (`SELECT COUNT(*)`) and compare it against what the vendor's raw API
   returns (walk all pages) and what the vendor UI shows.
4. Spot-check a known record (`SELECT * WHERE <pk>='<id>'`) and compare every scalar field
   against the raw API value (watch timestamp unit/precision, integer precision, nulls).
5. Report a per-table summary: schema, row count, strategy, and anything surprising
   (unexpected nulls, type quirks, empty tables). Any mismatch is a bug in the source — fix
   it, then re-run.

## 7. Tests

Only commit tests that exercise **pure logic** — never tests that embed data from the test
account. The credentials belong to the user, so their account's row counts, field values,
and schemas must not be baked into committed fixtures. Validate against the live account
locally instead (see step 6).

- **Unit tests** — `pkg/source/<source>/<source>_test.go` (same package, not `_test`) for
  pure logic:
  - `TestParseURI`: valid URI, each required field missing, wrong scheme, edge cases.
  - `TestIsValidTable`: every supported table true; unknown/empty/wrong-case false.
  - Any source-specific parsing helpers with normal, edge-case, and invalid inputs.
  - `TestJsonUseNumber` if the source uses `decoder.UseNumber()`.
- **Incremental logic (local verification, not committed)** — for one server-side and one
  client-side filtered table, run against the real API and confirm by hand: (1) a range
  covering the data returns the expected rows, (2) a range entirely before the data returns
  0 rows, (3) no interval returns all records.

Run `make format`, `make lint`, and `make test` when done.

## 8. Docs & reporting

- Add a user-facing docs page under `docs/supported-sources/<source>.md` describing the
  data and usage — not query params, filtering, or rate-limit mechanics.
- Report to the user a per-table breakdown: filtering type (server/client/none) and exact
  filter syntax, incremental key and strategy, rate-limit tier, whether parallelism is on;
  the default behavior when no interval is provided; any deviations from the common
  per-source helper patterns (e.g. `paginateAndSend`); and
  any notable design decisions (merge vs replace, added end filter, parallelized fan-out).
