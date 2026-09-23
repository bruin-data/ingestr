---
name: hubspot-destination
description: How the ingestr HubSpot reverse-ETL destination works and why — the two-axis matching model (id_property + --primary-key), strategy→endpoint mapping, why merge/replace can't match hs_object_id, batch atomicity/bisection, reject-mode & write-nulls defaults, and the mirror-safety guards (writtenIDs / sawSource / incompleteFroms). Use when changing anything in pkg/destination/hubspot/ or reverse-ETL behavior in pkg/strategy/reverse_etl.go. If your change alters behavior described here, update this file in the same PR.
---

# HubSpot Destination (reverse ETL) — Decisions

How ingestr writes CRM records and associations back into HubSpot, and the non-obvious decisions behind it. Code lives in `pkg/destination/hubspot/hubspot.go`; the strategy glue is `pkg/strategy/reverse_etl.go`.

> **Maintenance:** this file documents current behavior + rationale. If your change alters anything here, update the relevant section in the same PR.

## 1. Two-axis matching

A write matches existing records on two independent things:

- **Match property** (HubSpot side) — the internal property name, from `id_property=<prop>` on the dest-table. Stored as `shaper.idProperty`.
- **Source value column** — the source column carrying the match value, from `--primary-key`. Stored as `shaper.idColumn`.

They are deliberately separate because the same value (e.g. an email) can live in any source column while the HubSpot property is fixed. `--primary-key` **never** names the property; `id_property` **never** names a source column.

`matchKey(s) = strings.ToLower(strings.TrimSpace(s))` folds case/whitespace so a value HubSpot echoes back normalized (e.g. lowercased email) can be correlated to the source value. But HubSpot unique constraints are **case-sensitive** (live-verified: `ABC` and `abc` coexist), so blind folding would collide distinct keys. `resolveMap` (`newResolveMap`) therefore stores each resolved value **both exact and folded**, and `lookup()` prefers the exact hit, falling back to folded — fixing case-collision while still tolerating HubSpot's normalization.

## 2. Strategy → endpoint mapping (`shapeRow`)

| Strategy | Action | Endpoint / behavior |
| --- | --- | --- |
| `merge` (business-key `id_property`) | `upsert` | `batch/upsert` keyed on `idProperty` — HubSpot creates-or-updates. |
| `merge`/`replace` with `id_property=hs_object_id` | — | **Rejected at parse** (see §3). |
| `update` | `update` | `batch/update` by id, or Search when the property is non-unique. Missing = reject, never create. |
| `append` | `create` | `batch/create`. Always creates; never matches. |
| `delete` | archive | `batch/archive` by id (resolved from the match value). |
| `replace` | mirror | Upsert every row, then archive records whose match value isn't in the source (§6). |

`incremental_strategy` is **required** (`RequiresExplicitStrategy`) — the framework default `replace` would mirror-archive the whole object, too destructive to inherit silently.

## 3. Why merge/replace can't match `hs_object_id`

HubSpot has **no upsert-by-record-id**: it assigns record ids and cannot create a record at a caller-supplied id. So `merge`/`replace` keyed on `hs_object_id` is rejected at parse time in `parseShaper` (`!updateOnly && idProperty == recordIDProperty`). Use `update` to match existing records by id, or a unique `id_property` to upsert. `upsert()` is `matchesRecords() && !update()`, so it is structurally false for `hs_object_id`.

Consequently **every `"update"` action routes through `sendUpdateOnly`**: a missing id (absent or archived) is a not-found reject honoring `--reject-mode`, never re-routed to create. The old `sendUpdate`/`partitionExisting`/`batchReadFound` create-re-route was deleted — it was the only thing that ever fabricated a duplicate record from a missing id.

## 4. Batch atomicity, bisection, systemic aborts

- **`create` is the only non-idempotent action**: retried only on 429 (`SetRetryOnRateLimitOnly`), never on 5xx — a retry after a server-side commit would duplicate. (Live-verified: a batch create 400/409 commits nothing, so bisecting and re-POSTing valid rows is safe.)
- A whole-batch **400/409** (record-level, `isRecordLevelStatus`) is **bisected** to isolate the bad row(s) and land the valid remainder — HubSpot batches are not atomic.
- **Systemic failures abort** regardless of `--reject-mode` (`isSystemic`/`systemicBatchError`): a structural error (matched by substring `"non-unique"`) fails every row identically, so bisecting just amplifies requests and would look tolerable under `skip` while writing nothing. This substring match is a known best-effort limitation, deliberately kept.
- 404 handling: `isRecordsAbsent404` parses the JSON `category` and matches `OBJECT_NOT_FOUND` **exactly** (not a substring of the body), so a misconfig 404 stays a hard error while a genuine "records absent" is treated as none-found.

## 5. reject-mode & write-nulls (defaults live in the pipeline)

- `--reject-mode`: `fail` (default; write valid rows, then error with the reject list), `fail_fast` (stop at first bad row), `skip` (write valid rows, report rejects, exit 0). `fail`/`skip` both write all valid rows — they differ only in exit status. Default `fail` is applied in `reverse_etl.go` (`"" → fail`).
- `--write-nulls`: clear-by-default. A null cell is written as `""` (clears the field) unless `--write-nulls=false` (omit → leave existing value). **The default is applied in `Pipeline.Run` via `config.ResolveWriteNulls`, not in the strategy** — `reverse_etl.go` passes `job.Config.WriteNulls` as-is. Do not add a second copy of that default in the strategy layer (it was tried and reverted; single source of truth is the pipeline). Only property-writing strategies are affected; `delete`/associations send no properties.

## 6. Mirror safety (replace)

`replace` upserts every row, then `finalizeMirror` archives records not in the source. Three guards prevent it from destroying data:

- **`writtenIDs` (`*sync.Map`)** — every record id HubSpot returns this run (create + upsert) is remembered by id, so `listStaleIDs` never archives a record this run just wrote even if its stored match value was reformatted/case-folded.
- **`sawSource` (`atomic.Bool`)** — set in `writeBatch`; if the source produced **0 rows**, `finalizeMirror` skips the archive sweep and warns, so a transient empty extract can't wipe the whole object. (Intentional clear-out is what `delete` is for.)
- **`incompleteFroms` (`*sync.Map`, associations)** — a From record whose desired To set couldn't be fully resolved this run (a To key didn't match) is excluded from `finalizeAssociationMirror`, so an unresolved key never unlinks a live association the source meant to keep.

## 7. Associations

Dest-table `obj1+obj2`. Two source key columns from `--primary-key k1,k2` (obj1 then obj2, positional: `fromColumn=primaryKeys[0]`, `toColumn=primaryKeys[1]`); two match properties from `id_property=fromProp,toProp` (positional; empty side = value is already a record id). Order is positional and unchecked — wrong order silently swaps sides.

- Strategies: `merge` (add links), `delete` (unlink), `replace` (mirror each From's links). `update`/`append` fail fast.
- **Default vs labeled**: with no `label`/`association_type`, links go through the default association (`associate/default`). A `label` (resolved to a type id) or numeric `association_type` (+optional `association_category`, else resolved) scopes to one type. For `delete`/`replace`, a labeled unlink uses `labels/archive` and removes **only** that type; an unlabeled archive removes **every** type between the pair.
- A key column holding a list/array explodes to the cartesian product (deduped within a row); re-linking the same pair across rows is harmless (idempotent).

## 8. Reject annotation

`annotate` (records) tags each rejection with the offending record's key: 1:1 when counts match, the single key for a bisected chunk, else maps by the id HubSpot names in the error `context` (`{"ids":[...]}`, seen on not-found 207s). `annotateAssociations` uses position only — live check showed association errors come back as a **whole-batch 400 with no per-record `errors` array** (and use `objectId`/`INVALID_OBJECT_IDS`, not `ids`), so they bisect to a single link and are named by position; there is no context-id path to rely on.

## 9. Live-verified HubSpot facts (don't re-derive from guesses)

- Unique constraints are **case-sensitive** (`ABC` and `abc` are distinct records).
- Property definitions expose **no** normalization flag — `email` and `firstname` are both `type=string`/`fieldType=text`; only phone is `phonenumber`. This drove the two-tier `resolveMap` (exact+folded) instead of a hardcoded normalized-property list.
- **Upsert by a unique property against an already-archived record creates a NEW live record** (new `hs_object_id`, same value); it does **not** resurrect the archived one or error. HubSpot's own `batch/upsert` behavior — ingestr does not intervene.
- Archived records are invisible to `batch/read` **even with `archived:true`**; only the single-object `GET /{id}?archived=true` returns them.
- Archive of a missing id → 204 (idempotent).
- Labeled association archive: `POST /crm/v4/associations/{from}/{to}/batch/labels/archive`.

## 10. Custom objects & column rename

- Custom objects are addressed by **internal name**, **fully-qualified name**, or **objectTypeId** (resolved via the schemas API and cached) — never the display label (two schemas may share a label).
- `--columns` supports **rename only** (`dest_property::source_column`); an entry carrying a type is rejected (HubSpot fixes property types). The rename is applied upstream by the pipeline's `ColumnRenamer` before the batch reaches the destination, so the shaper just writes each column under its post-rename name. HubSpot does not create properties on the fly — every written column must map to an existing property.
