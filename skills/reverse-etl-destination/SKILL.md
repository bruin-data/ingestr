---
name: reverse-etl-destination
description: Design notes for reverse-ETL destinations — destinations that write rows back into an external API instead of a warehouse. Read before adding/changing a destination under pkg/destination/ that targets an API, or reverse-ETL code in pkg/strategy/reverse_etl.go. Reference impl: pkg/destination/hubspot. Update this file if you change the behavior it describes.
---

# Reverse-ETL destinations

Destinations that push rows into an external API (CRM, marketing tool). No staging table, no SQL, no swap — each row is an API call, batched. Reference impls: `pkg/destination/hubspot/` (whole-batch failures, bisection, associations) and `pkg/destination/salesforce/` (per-record results, native upsert, describe-driven validation). Strategy glue in `pkg/strategy/reverse_etl.go`.

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
- The write-nulls default is resolved once in `Pipeline.Run`, not per-strategy.
- `PrepareTable` has no table to build — use it for config validation / object-type resolution instead (HubSpot checks unknown properties there).

## Setup
- Auth: reuse the source connection; the token needs write scopes on the target objects (plus schema-read for custom objects).
- Dest-table: encode the object + params in the dest-table string (`obj?a=b&c=d`), parsed via `tablespec` — the asset `name` can't hold `?`/`=`/`&`.

## Matching (two axes — keep them separate)
- Match field = the remote property, from a dest-table param (`id_property`).
- Value column = the source column, from `--primary-key`.
- Don't let one name the other.
- Default the match field only for update/delete (the record id). Merge/replace — which create unmatched rows — require both the match field and the source column explicitly; don't infer an upsert key.
- Normalization: APIs may lowercase/trim stored values. Correlate with a folded key, but keep the exact value too — uniqueness may be case-sensitive. See `resolveMap` (exact + folded, exact wins).

## Strategy → API op
- `merge` → upsert, `update` → update-only (no match = reject, never create), `append` → create, `delete` → archive, `replace` → mirror.
- Reject impossible configs at parse time (HubSpot: merge/replace on `hs_object_id` — no upsert-by-server-id).
- Fail fast on unsupported combos (associations reject `update`/`append`).
- `append` never matches: refuse the match-field param (`external_id`/`id_property`) up front, since someone setting it expects matching. Accept `--primary-key` with a warning, because Bruin passes it for every asset with primary-key columns; use it only to name rejected rows.

## Batching
- Know each endpoint's idempotency. Non-idempotent create: retry only on 429 (pre-commit), never 5xx.
- **First check whether the API reports per-record results.** If it returns one positionally-aligned result per input (Salesforce sObject Collections with `allOrNone=false`), there is nothing to bisect — failures are already attributed, and any non-success HTTP status is systemic by definition. Only APIs that fail a whole batch on one bad row (HubSpot) need bisection.
- Batches aren't atomic: bisect on record-level errors (400/409) to land the good rows.
- Systemic failures (fail every row the same way) abort regardless of `--reject-mode` — don't bisect (`isSystemic`).
- Retry transient per-record failures before rejecting them: Salesforce's `UNABLE_TO_LOCK_ROW` means the record was not written, so re-send just those records with backoff (`withLockRetry`), even for create.
- If the API has an async bulk path (Salesforce Bulk API 2.0), expose it as `load_method` on the URI (matching BigQuery's naming). Salesforce defaults to `bulk`: ingestr re-sends every row on each run (no change detection), so REST's one call per 200 rows can exhaust the org's daily API limit, which is also why Hightouch and Census default to bulk. `rest` stays available for `fail_fast` and small, latency-sensitive syncs. Never fall back from a failed bulk write to REST: rows the job already wrote would be sent twice. Reuse the shaping and funnel it at the send chokepoint so strategies stay identical. Bulk result files may not keep input order, so attribute rejects by the echoed match column, not by position. Rejects only exist after the job finishes, so refuse `fail_fast` up front.
- Match API error category/code exactly, not by substring (`isRecordsAbsent404`).

## Serialization
- Convert Arrow cells to the API's wire format (string/JSON): timestamps, JSON arrays, nulls. See `propertyValue`.
- Check whether the API is string-only (HubSpot) or wants native JSON types (Salesforce) — sending `"42"` where a number is expected can pass validation and land the wrong type.
- Clearing a field differs per API: empty string (HubSpot) vs explicit JSON `null` (Salesforce).
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

## Relationships as fields (if the API has no link objects)
Some APIs model relationships as a lookup field on the record itself (Salesforce `Contact.AccountId`), so there is no association mode to build — you write the field. Support addressing the parent by its external key rather than its server id: map a dotted column (`Account.Ext_Id__c`) onto the nested object the API expects (`setField`). Without that the user has to resolve parent ids themselves.

Check whether a lookup can point to several object types (Salesforce `referenceTo` has more than one entry: `Task.Who`/`What`, `Owner` on Lead/Case/Task/custom objects). Salesforce then needs the parent type in the nested object (`"attributes": {"type": "Contact"}`); without it the **whole request** fails with a `JSON_PARSER_ERROR`, not a per-record reject. ingestr takes the object from a three-part column, `Who.Contact.Ext_Id__c` (bulk translates it to Salesforce's `Contact:Who.Ext_Id__c` header), defaults a polymorphic `Owner` to User, and refuses any other untyped polymorphic column in `PrepareTable`. Don't use `:` in a column-name convention — `--columns` (and Bruin's `source_column`, which goes through it) splits on `:`. A row that fills two typed columns for one relationship is a per-row reject.

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
- does a same-value update change anything? HubSpot: a true no-op (no `lastmodifieddate` bump, no history). Salesforce: every update bumps `LastModifiedDate`/`SystemModstamp` and fires triggers/flows even when no value changed, so an unchanged re-run is not free there.

## Schema validation in PrepareTable
If the API can describe the target object, validate there rather than per record: unknown columns, and columns the strategy can't write (Salesforce splits `createable`/`updateable`, so upsert needs both). Exclude the server-assigned id from written fields entirely — it is never writable, and a warehouse round trip always carries it. Keep validation best-effort: a describe the token can't read should warn and continue, not block a valid load.

## Misc
- Resolve object/entity names to stable ids and cache; never match on display labels.
- `--columns` is rename-only where the API fixes types (`dest::source`, no type). Applied upstream by `ColumnRenamer`. Don't create remote fields on the fly.

## Testing
Unit tests:
- `httptest`-backed tests for request shapes and the bisection/reject paths (assert what hits each endpoint).
- Unit tests for row shaping (strategy → action/payload), including NaN/Inf, null vs empty, and short-form ids.

A live suite is required before calling a destination done. Unit tests prove request shapes. They do not prove the API behaves the way you assumed. Treat the live suite as part of the change, not a follow-up.

### Conditions for a valid live run

A run only counts if all of these hold:

- **Known start state.** The target account is empty, apart from records the vendor seeds that can't be removed (e.g. Salesforce's Standard Price Book). Wipe it with the destination itself (see Cleanup below) or the vendor API, then confirm counts are 0 before starting.
- **Source is a real warehouse, not a CSV.** Use a warehouse source (MotherDuck/DuckDB) so Arrow types (decimal, timestamp, date, bool, list, NaN floats) reach the destination as they would in production. CSV makes everything a string and hides type bugs.
- **Cases run in order, and each depends only on earlier ones.** Seed parents before children. Snapshot server ids (read the vendor back through its ingestr *source* into the warehouse) before cases that match on record id.
- **Every case has three checks.** A case passes only when all three agree:
  1. the exit code (0, or non-zero for expected failures),
  2. the expected substrings in the output (reject codes, keys, warnings),
  3. an **independent read** of the remote state (SOQL, the vendor's REST read, the UI). Never trust ingestr's own success message as proof that data landed.
- **Expected failures must prove nothing was written.** For every pre-flight validation error, check afterwards that the keys in that source table don't exist remotely.
- **The binary under test is the current build.** Rebuild (`make build`) after every fix and re-run the **whole** suite, not just the fixed case. Fixes regress neighbours.
- **Mind sandbox storage and quotas.** Dev orgs are small (Salesforce: 5 MB, and the Recycle Bin counts toward it). Cap volume cases, empty the recycle bin between full runs, and treat a quota error as a harness problem, not a pass.
- **Secrets stay out of artifacts.** Credentials live in one `chmod 600` env file. The harness redacts client ids, secrets, tokens and warehouse tokens from every saved output. Grep the generated runbook/results (and `unzip -p` any xlsx) for the raw secrets before sharing.
- **Clean up at the end.** Delete children before parents, and confirm the account is back to the start state so the next run is valid.

### Test matrix

Cover every row that applies to your API.

#### Seeding and idempotency
- Create parents, then children linked to them by the parent's external key.
- Self-referencing lookup (Account.Parent).
- A junction / many-to-many object (OpportunityContactRole).
- A custom object with **every field type** the API has: text, long text, number, currency, percent, date, datetime, checkbox, picklist, multi-picklist, email, phone, url, lookup.
- Re-run the same merge: record count unchanged, no duplicates.

#### Strategies × match fields
For each strategy (`merge`, `append`, `update`, `delete`, `replace`), match on each kind of field the API supports:
- The external/unique key.
- The server record id (the default for update/delete). Also test short-form ids if the API has them (Salesforce 15-character ids) and a malformed id.
- A non-unique field, which should update or delete **every** match.
- A case-insensitive lookup field (Email): match `ADA@EXAMPLE.COM` to `ada@example.com`.
- A numeric key: `1001.00` must match a stored `1001`.
- A lookup/reference field used as the match (delete junction rows by parent id).

Edge rows in the same runs:
- A key that doesn't exist (reject `NOT_FOUND` for update/delete, never create).
- A row with a null key (skipped with a warning).
- `append` re-run, which creates duplicates. That is expected and should be documented.
- `append` onto an existing unique value: rejected per row and named by `--primary-key`; `append` with the match-field param fails before writing.
- A delete the API refuses because of dependants (Salesforce `DELETE_FAILED` on a Contact with a Case).

#### Nulls
- `--write-nulls=false`: nulls are omitted and existing values stay.
- Default: nulls clear the field. Verify the field is actually empty remotely.
- Nulls are controlled by `--write-nulls` only. A dest-table `write_nulls=` param is refused as unknown; don't add per-table copies of run flags.
- NaN / +Inf / -Inf floats follow the write-nulls rule. They are not sent as strings and must not be silently dropped.
- An empty string vs null, if the API distinguishes them.

#### Reject modes
Use one source table with good rows and several kinds of bad row: a missing required field, a value that is too long, an unknown parent, plus a good create.
- `fail` (default): good rows land, the run exits non-zero, and every reject is listed with its key.
- `skip`: the same rows land, exit 0, rejects reported.
- `fail_fast`: stops at the first failing request; later batches are not sent.
- The same key twice in one run: the API's behaviour is reported with a hint that says what to fix.
- Quota/storage errors abort regardless of reject mode. They must not produce one reject per row.

#### Relationships
- Link by the parent's external key, relink to another parent, unlink with null, and check that a null with `--write-nulls=false` leaves the link alone.
- Link by the parent's raw record id.
- Lookup through a non-External-ID unique field (`Owner.Email`, `Owner.Username`).
- Lookups that can point to several object types: by id, by external key with the object named, and without the object (refused before writing).
- For association APIs (HubSpot): link, unlink, a labelled type, and mirror per from-record.

#### Column mapping and naming
- `--columns dest::source` rename. `--primary-key` names the **renamed** column.
- `--primary-key` naming the pre-rename column fails clearly.
- `--columns` with a type is refused (rename-only).
- `--schema-naming` is ignored with a warning, and names are sent verbatim.
- Upper/mixed-case source column names resolve to the API's field names.

#### Values and types
- Every type round-trips correctly. Check the stored value, not just "no error". Note the API's precision (Salesforce DateTime keeps whole seconds).
- Bad values reject per row (bad picklist, unparseable date, text in a number) while good rows in the same batch land.
- Writing to a formula/read-only field is refused before any write.

#### Mirror / replace
- v1 then v2: an update, a create, and removal of every record not in the source.
- An empty source: the sweep is skipped with a warning.
- A rejected row under `fail`: the sweep is skipped and the run fails. Under `skip`: the sweep runs.
- A row with no key is created and survives its own sweep.

#### Alternate write path
If there is a bulk/async path, re-run a representative subset through it: merge, update by a case-insensitive field, delete, replace, append, rejects, and clearing a field. Also:
- Refuse options that can't work up front (bulk + `fail_fast`).
- Results may come back unordered, so check each reject is attributed to the right key.

#### Volume
- More rows than one request holds (several requests' worth) on every path, including create, update and delete.
- Record wall time for REST vs bulk.
- A long-running async job exercises the poll backoff: a bug that only shows up after minutes of polling won't appear in a 10-row test.

#### Pre-flight validation
Each case must fail **before any write**. Check that its keys don't exist afterwards.
- Missing, unsupported or invalid strategy (none given, `delete+insert`, `scd2`).
- Match-field problems: missing, the server id used for an upsert, not an External ID field, doesn't exist.
- Key-column problems: missing `--primary-key`, composite `--primary-key`, `--primary-key` column not in the source, update with the default id match but no id column.
- Unknown object; a display label passed instead of the API name, including a label that collides with another object's.
- A source column that isn't a field; a read-only field; an unknown relationship in a dotted column.
- An unknown dest-table param (including another destination's param name).
- An invalid `--reject-mode`.

#### Auth and URI
- Every supported auth method, including a pre-minted access token.
- An explicit older API version.
- A wrong secret, an invalid token, a missing domain, an unknown auth method, an invalid load method, an API version that doesn't exist, and a missing password for a password flow. Each should fail **at connect** with a message naming the problem, not at the first write.

#### Cleanup
Snapshot every object through the source, then delete children before parents (activities, cases, opportunities, junctions, contacts, accounts). This doubles as a delete-at-scale test.

### Harness

Keep the suite data-driven so it can be re-run and handed to a human tester:

- A setup script that creates every source table (`CREATE OR REPLACE`). It's the one reset button.
- A list of cases. Each has an id, a title, an optional SQL pre-step (e.g. building a key table from a snapshot), the command, the expected outcome (success/fail), required output substrings, an independent check query, and an optional check function.
- A runner that executes every case in order, applies the three checks, redacts secrets, and records PASS/FAIL per case.
- A generator that turns cases + results into a human runbook (command, expectation, check query and verified output per case) and a checklist with a result column.

### Bugs that typically only show up live

These kinds of bugs pass `httptest` and surface in a live run:

- Non-finite floats (NaN/Inf) silently dropped instead of following the write-nulls rule.
- Quota or storage errors producing one reject per row instead of aborting the run.
- Bad connection settings (API version, expired token) caught only at the first write, with a misleading error, instead of at connect.
- Unsupported strategies accepted up front and failing later.
- Transient network errors on login failing the whole run instead of being retried.
- A hint that blames the wrong cause for a reject (e.g. duplicate keys within one run vs. an existing record).
- Display labels that match more than one object.
- Test expectations that disagree with the API (e.g. stored precision of timestamps). Fix the expectation, not the code, when the API is the authority.
- Short-form or differently-cased ids that don't match what the API returns.
- Async poll loops that misbehave only after minutes of waiting.
