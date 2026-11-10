# Parallel PostgreSQL snapshot contract

Status: proposed WI-2 contract, pending agreement with the WI-1 engineer. This
document describes the shared interface proposal; it does not assert that the
interface or the benchmark acceptance criteria have been implemented.

## Shared progress representation

Keep the existing `CDCSnapshotProgress` fields and add these fields in
`pkg/source/source.go`:

```go
type CDCSnapshotRangeProgress struct {
    ID       string   `json:"id"`
    After    []string `json:"a,omitempty"`
    Upper    []string `json:"u,omitempty"`
    Key      []string `json:"k,omitempty"`
    Complete bool     `json:"c,omitempty"`
}

type CDCSnapshotProgress struct {
    Boundary          string
    Key               string
    Incarnation       string
    SchemaFingerprint string
    Attempt           string
    Ranges            []CDCSnapshotRangeProgress
}
```

`Boundary` remains the original replacement and stale-row retirement boundary.
It is also the earliest snapshot boundary whose subsequent WAL must remain
available until catch-up. A resumed attempt imports a new exported snapshot and
stamps newly read rows with that snapshot's newer read LSN. Neither the newer read
LSN nor a range key advances the globally safe token `Position`.

`Attempt` identifies one replacement attempt and is stable across its resumes.
`Ranges` is the complete, immutable plan and cumulative durable-position vector
for that table. Each range has a stable ID and owns `(After, Upper]`; nil outer
bounds are open. `Key` within a range is its last covered key, or nil before the
first batch. `Complete` explicitly represents exhausted ranges, including empty
ranges. Completed ranges remain in every vector until table completion clears
the plan. Changing extraction concurrency changes scheduling, never boundaries
or identities.

Keys contain each PostgreSQL key column's text representation in key-column
order. PostgreSQL casts bound parameters to the native column types and compares
them with the same ordering and collation as the scan. Go does not sort or compare
key values. Adjacent ranges share the same serialized split point, so equality
of adjacent bounds can be validated without interpreting PostgreSQL ordering.
The planner must remove duplicate split points under PostgreSQL equality before
constructing the plan.

The retained top-level `Key` is the existing serialized JSON array used by the
serial progress format. New vector progress leaves that field empty. Readers
must not interpret the maximum range key as the table's completed prefix.

Progress remains bound to the source incarnation and schema fingerprint. The
state manager also binds it to the existing physical destination digest and
destination table name. The exported snapshot name is never persisted as a
durable identity.

## Event and durability ordering

For each table, the source emits:

1. Table announcement, when required by the existing multi-table API.
2. Exactly one replacement marker, or one resume marker retaining the original
   `Boundary` and setting `CDCSnapshotResumed`.
3. A complete progress vector recording the stable plan, followed by worker
   batches and cumulative progress updates.
4. Exactly one table completion in `SnapshotPositions`, with matching incarnation
   and schema metadata, after all ranges succeed and all their results have been
   forwarded.

One coordinator owns the cumulative vector. It forwards a batch before emitting
progress that covers that batch, and publishes newly allocated copies of the
vector and nested key slices. Workers send local range results to the coordinator;
they never publish independently assembled table vectors. Completion of a high
range cannot advance or complete a low range. A worker error cancels siblings and
prevents table completion; every queued Arrow record is released or forwarded.

The existing latest-token-per-table aggregation in `pkg/strategy/streaming.go`
is valid for these immutable cumulative vectors. The vector covers all prior
results for that table, including those emitted by other workers. No merging of
independent deltas is required. Final worker EOF does not certify destination
durability.

WI-1 must wait for every covered destination load to succeed before persisting
the accumulated progress. Completion must also wait for replacement cleanup and
destination identity checks. The staged destination path retains the same
write, merge, cleanup, persistence, and acknowledgement order. The source emits
no WAL data before its snapshot phase completes; its completion tokens do not
authorize an acknowledgement of WAL that has not been applied.

## Persistence and compatibility

`pkg/strategy/cdc_snapshot_progress.go` writes vector events as format version 2,
including the attempt and full range vector. It continues decoding version 1
single-key events. Version 1 resumes as one full-table range with the existing
last key, preserving its original replacement boundary; it is not repartitioned
around that key. This keeps existing durable rows and WAL reconciliation
semantics. New serial reads may use a one-range vector.

An initial plan or an empty-range completion is persistable without a top-level
key. The state manager keeps the newest complete vector per table; pruning must
retain it across generations until completion or explicit invalidation writes a
reset event. A token that completes a table does not also persist an in-progress
vector for it.

Reject unknown formats and malformed plans before reading or publishing a resume
marker. Validation includes attempt identity, unique range IDs, key arity,
contiguous boundaries, correct outer bounds, and consistent source and target
identity. Native ordering and containment checks run in an imported transaction.
Unsafe state must either trigger the existing fresh-replacement lifecycle or fail
closed with a recovery error; it must never silently skip rows.

Resume remains limited to supported managed streaming backfills. Without a usable
completed-table resume position, the existing initial snapshot behavior starts
fresh. Source/schema or physical destination changes invalidate progress. The
reader must check that the retained persistent logical slot can still replay the
required resume boundary before using any saved range. In particular,
`restart_lsn` and `confirmed_flush_lsn` must not exceed the required stream start.
The stream start includes the minimum original boundary of every resumed
snapshot, so updates, deletes, inserts, and key moves behind completed ranges or
saved keys remain recoverable. Missing required WAL must not lead to a partial
resume followed by certification.

## Ownership

| Files or behavior | Owner and coordination |
| --- | --- |
| `pkg/source/postgres_cdc/` range planning, bounded reader, scheduling, import lifetime, WAL validation | WI-2 |
| `pkg/source/source.go` progress fields and comments | WI-2 after WI-1 agreement |
| `pkg/strategy/cdc_snapshot_progress.go` and its regression tests | WI-2 after WI-1 agreement |
| `pkg/strategy/streaming.go` aggregation and snapshot lifecycle | Shared; cumulative vectors avoid changing aggregation behavior |
| `pkg/destination/starrocks/` loading and destination workers | WI-1 |
| Destination load durability, identity validation, and stale-row retirement | WI-1 preserves the existing contract |

The reusable range reader accepts an existing exported snapshot, current read
LSN, table schema, optional outer bounds, batching settings, and a caller-owned
concurrency budget and result/progress mechanism. The caller owns slot creation,
export lifetime, replacement orchestration, table completion, and WAL catch-up.
This permits WI-6 to reuse the reader inside one snapshot chunk without changing
chunk catch-up or hard-delete ordering.

## Required regression evidence

Persistence tests cover vector round-trip, empty-range completion, initial plan
durability, out-of-order range completion, legacy single-key decoding, newest
vector pruning, invalidation/completion resets, and destination identity changes.
Streaming tests cover failed loads, failed progress persistence after a successful
load, progress persistence before a crash, and all-ranges-complete progress
without final table completion. Replays can repeat rows but cannot skip them.

Source tests cover saved plans under changed worker counts, delayed importers,
import failure, cancelled workers, pool exhaustion, native typed/composite key
coverage, stable boundaries, schema/incarnation changes, missing retained WAL,
and changes behind already durable keys followed by full WAL catch-up comparison.
Tests exercise the staged destination path independently of WI-1. The shared
standby and combined StarRocks benchmark require separate measured evidence;
local unit or primary-container results do not satisfy those acceptance runs.
