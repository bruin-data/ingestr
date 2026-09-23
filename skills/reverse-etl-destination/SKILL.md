---
name: reverse-etl-destination
description: Design notes for reverse-ETL destinations — destinations that write rows back into an external API instead of a warehouse. Read before adding/changing a destination under pkg/destination/ that targets an API, or reverse-ETL code in pkg/strategy/reverse_etl.go. Reference impl: pkg/destination/hubspot. Update this file if you change the behavior it describes.
---

# Reverse-ETL destinations

Destinations that push rows into an external API (CRM, marketing tool). No staging table, no SQL, no swap — each row is an API call, batched. Reference impl: `pkg/destination/hubspot/`, strategy glue in `pkg/strategy/reverse_etl.go`.

## Start here: research the write API
The API decides which strategies are possible and how they map — you don't invent operations, you expose what it supports. Before writing code, find out:
- Which write ops exist: create / update / upsert / delete-or-archive, and whether there's a batch variant.
- Native upsert? If yes, `merge` maps straight to it. If no, you simulate (update-then-create) or drop `merge`.
- What you can match on (which fields), and whether the id is server-assigned (if so, no upsert-by-id → that combo is impossible, reject it).
- Uniqueness rules and case-sensitivity; soft-delete vs hard-delete; associations/relationships.
- Batch limits, rate limits, idempotency, and partial-failure/error shape.

Decide the strategy set and mappings from this — not the reverse.

## Framework hooks
- Implement `destination.ReverseETLDestination` (`IsReverseETL()`) — turns on RETL flags/strategies and drops the SQL-only checks (e.g. merge without a PK).
- Capability markers, not hardcoding: `RequiresExplicitStrategy()`, `SupportsReplace/Append/Merge/DeleteInsert/SCD2Strategy()`, `SupportsAtomicSwap() = false`.
- Strategies go through `executeReverseETL`; each `Execute` checks `IsReverseETL` and delegates, each `Validate` calls `validateReverseETLReject`.
- Defaults are resolved once in `Pipeline.Run`, not per-strategy.
- `PrepareTable` has no table to build — use it for config validation / object-type resolution instead (HubSpot checks unknown properties there).

## Setup
- Auth: reuse the source connection; the token needs write scopes on the target objects (plus schema-read for custom objects).
- Dest-table: encode the object + params in the dest-table string (`obj?a=b&c=d`), parsed via `tablespec` — the asset `name` can't hold `?`/`=`/`&`.

## Matching (two axes — keep them separate)
- Match field = the remote property, from a dest-table param (`id_property`).
- Value column = the source column, from `--primary-key`.
- Don't let one name the other.
- Normalization: APIs may lowercase/trim stored values. Correlate with a folded key, but keep the exact value too — uniqueness may be case-sensitive. See `resolveMap` (exact + folded, exact wins).

## Strategy → API op
- `merge` → upsert, `update` → update-only (no match = reject, never create), `append` → create, `delete` → archive, `replace` → mirror.
- Reject impossible configs at parse time (HubSpot: merge/replace on `hs_object_id` — no upsert-by-server-id).
- Fail fast on unsupported combos (associations reject `update`/`append`).

## Batching
- Know each endpoint's idempotency. Non-idempotent create: retry only on 429 (pre-commit), never 5xx.
- Batches aren't atomic: bisect on record-level errors (400/409) to land the good rows.
- Systemic failures (fail every row the same way) abort regardless of `--reject-mode` — don't bisect (`isSystemic`).
- Match API error category/code exactly, not by substring (`isRecordsAbsent404`).

## Serialization
- Convert Arrow cells to the API's wire format (string/JSON): timestamps, JSON arrays, nulls. See `propertyValue`.
- Watch API-specific quirks (HubSpot multi-select is a `;`-joined list; a leading `;` appends instead of replacing).

## Rate limiting
- `WriteParallel` fans out batches concurrently — the destination still needs its own rate limiter to bound the real request rate; cap parallelism to what the API allows.

## Flags
- `--reject-mode`: `fail` (default; write valid, then error) / `fail_fast` (stop at first) / `skip` (write valid, report, exit 0). Only per-record problems are skippable.
- `--write-nulls`: clear vs omit for null cells. Pick a default, document it, apply it once in the pipeline (`config.ResolveWriteNulls`). Use a `...Set` bool so the default only kicks in when the flag is truly unset. Don't default it in two places.

## Rejections
- Collect across batches, report at the end.
- Tag each reject with the record it belongs to: by position when unambiguous (1:1 or bisected singleton), else by ids in the error context. Best-effort — leave blank if the API doesn't say.

## Mirror / replace safety
- Require an explicit strategy (don't inherit a destructive default).
- 0 source rows → skip the delete sweep, warn (`sawSource`).
- Never delete records written this run (`writtenIDs`).
- Never reconcile an entity whose desired set didn't fully resolve (`incompleteFroms`).

## Associations (if the API links records)
- Two source keys + two match fields, positional (side-1 then side-2); wrong order silently swaps.
- Default link vs named type/label; scope a delete to the managed type (untyped delete removes all links between the pair).
- List/array cells explode to the cartesian product, deduped per row.

## Verify live, don't guess
Check against a real sandbox and write down what you find:
- case-sensitivity of unique fields
- upsert against an archived record: resurrect, new duplicate, or error?
- batch atomicity on partial failure
- partial-batch error payload shape
- are archived records visible to reads, and via which endpoint?

## Testing
- `httptest`-backed tests for request shapes and the bisection/reject paths (assert what hits each endpoint).
- Unit tests for row shaping (strategy → action/payload).
- Live sandbox for behavior you can't fake — see "Verify live".

## Misc
- Resolve object/entity names to stable ids and cache; never match on display labels.
- `--columns` is rename-only where the API fixes types (`dest::source`, no type). Applied upstream by `ColumnRenamer`. Don't create remote fields on the fly.
