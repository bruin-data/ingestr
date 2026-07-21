---
name: bigquery-destination
description: How the ingestr BigQuery destination works — the staging→swap write path, load methods (load_job/storage_write), where dedup happens, copy-job vs CTAS swap selection, partition/cluster change handling (rename-aside), and merge partition pruning. Use when changing anything in pkg/destination/bigquery/ or BigQuery-affecting behavior in the replace/merge strategies (pkg/strategy/replace.go, pkg/strategy/merge.go). If your change alters behavior described here, update this file in the same change.
---

# BigQuery Destination — Implementation

How ingestr writes data to BigQuery, including how partition/cluster changes are handled.

> **Maintenance:** this file documents current behavior. If your change alters anything described here, update the relevant section in the same PR.

## 1. Write Path

Data flows through a staging table:

```
source  ->  staging table  ->  destination table
```

- The staging table is created first with the configured partition/clustering.
- Data is loaded into staging.
- Staging is then swapped into the destination.

## 2. Loading Data into Staging (load methods)

- **`load_job`** (default): a BigQuery load job.
- **`storage_write`**: the BigQuery Storage Write API.

Selected via the destination URI: `bigquery://project/dataset?load_method=storage_write`. Default is `load_job`.

## 3. Preparing Tables (`PrepareTable`)

- Tables are created through the BigQuery API (`BuildTableMetadata` -> `FieldSchema`, `TimePartitioning{Field}`, `Clustering{Fields}`), not SQL DDL.
- **Staging table**: prepared with `DropFirst = true` and the configured `PartitionBy` / `ClusterBy`. `DropFirst` is implemented as an **optimistic async TRUNCATE**: fire `TRUNCATE TABLE` without checking existence first (the whole flow runs in a goroutine, overlapping with source reading); if the table doesn't exist, fall back to a fresh CREATE. So an existing staging table is *truncated and reused*, not dropped and recreated.
- **Destination table**: created only if it does not already exist; if it exists, schema evolution runs instead (`addMissingColumns`, plus REQUIRED→NULLABLE column relaxation for load jobs).
- **Primary key constraint**: `BuildTableMetadata` adds an informational (`NOT ENFORCED`) `PRIMARY KEY` constraint when primary keys are present. BigQuery never enforces uniqueness with it — it is only a query-optimizer hint; ingestr's own dedup guarantees uniqueness. Whether the destination ends up carrying the constraint depends on the swap path: the copy job (and the merge/append `PrepareTable` path) inherit it from staging (pk present); CTAS does not declare one (pk absent). BigQuery caps the constraint at 16 columns — with more PKs the constraint is skipped with a warning (merge still works; the MERGE SQL keys on the in-memory PK list, not the constraint).
- **Default PK clustering** (`defaultClusteringFromPrimaryKeys`): when no `cluster_by` is configured, ingestr clusters the table by up to 4 clusterable primary-key columns (skipping repeated fields and non-clusterable types: FLOAT, BYTES, TIME, JSON) so PK-joined MERGEs can prune clustered blocks instead of scanning the whole table. Applied both at table creation (`BuildTableMetadata`) and at swap time (`effectiveClusterBy`).
- **CDC mode**: non-PK columns are made nullable in staging and keyed merge targets because CDC DELETE events carry only key values. Existing REQUIRED non-key target fields are relaxed before merge, including with `storage_write`, so delete-only tombstones can be stored.

## 4. Deduplication (where it happens)

Two places dedup by primary key, both using `QUALIFY ROW_NUMBER()`:

- **In the MERGE statement's `USING` clause** (`buildMergeSQLWithPartitionPruning`). Used by:
  - the merge write strategy (upsert into the existing target), and
  - the replace strategy's dedup path: `deduplicateStaging` runs `MergeTable` to collapse a PK-free raw staging table into a fresh, deduplicated "normalised" staging table (created in the target's dataset), then drops the raw staging.
- **In the CTAS swap select** (`buildBigQueryDedupSelect`). This only runs when primary keys are present at swap time, which for BigQuery is the fast path (source guarantees unique PKs), so it operates on already-unique data.

So the swap reads either the normalised staging (dedup already done) or the raw staging (no dedup needed).

## 5. Swap: Staging -> Destination (replace strategy, `SwapTable`)

Two swap mechanisms:

- **copy job** (`swapTableWithCopyJob`): `CreateDisposition = CreateIfNeeded`, `WriteDisposition = WriteTruncate`. Free (no query billing). If the target does not exist it is created, inheriting staging's schema and partition. Cannot dedup and cannot read the streaming buffer.
- **CTAS**: `CREATE OR REPLACE TABLE target [PARTITION BY ...] [CLUSTER BY ...] AS SELECT ... FROM staging`. Billed (scans staging). Reads the streaming buffer. *Can* dedup (`buildBigQueryDedupSelect` emits a `QUALIFY` when PKs are present at swap) — but in practice that only fires on the fast path, where the data is already unique, so it's a no-op; when CTAS reads a normalised staging the PKs have been nilled and the select is a bare `SELECT *`. The `CLUSTER BY` uses the effective clustering (configured `cluster_by`, or the default PK clustering).

Before either path, `SwapTable` ensures the **target dataset exists** (replace only PrepareTables the staging side; CREATE OR REPLACE and copy jobs do not auto-create datasets). After a successful swap, the **staging table is deleted** (best-effort, 30s timeout).

Branch:

```
if load method == load_job AND no primary keys at swap:
    copy job
else:
    CTAS   (needed for dedup, or for storage_write's streaming buffer)
```

The branch keys on primary keys being empty **at swap time**, not on the source lacking a primary key. In the dedup path, `replace.go` collapses duplicates into the normalised staging (via `MergeTable`) and then NILS the primary keys, so "no PKs at swap" is a consequence of dedup, not of a missing source PK. That is why the copy job can read a normalised staging table.

**Swap selection (replace strategy):**

- Source has PKs, not guaranteed-unique -> dedup runs: raw -> normalised staging via MERGE, then PKs nilled. `load_job` uses the copy job, `storage_write` uses CTAS. Both read the normalised (already-deduped) staging.
- No PKs -> no dedup: raw staging. `load_job` uses the copy job, `storage_write` uses CTAS.
- PKs guaranteed-unique (fast path) -> no dedup, PKs kept: CTAS (its QUALIFY dedup is a no-op since the data is already unique). Reads the raw staging.
- `storage_write` always uses CTAS (a copy job can't read the streaming buffer).

So the copy job reads a normalised staging only in the "source has PKs + needs dedup + `load_job`" case; CTAS on `load_job` happens only in the fast-path (guaranteed-unique) case.

## 6. Partition / Cluster Change (rename-aside)

BigQuery cannot change a table's partitioning in place:

- `CREATE OR REPLACE` rejects a table with a different partitioning spec.
- A copy job cannot repartition an existing table.

So a changed `partition_by`/`cluster_by` requires recreating the table.

`SwapTable` detects a mismatch once (`partitionOrClusterMismatch`) and, on a mismatch, first runs a guard, then recreates the target via a rename-aside shared by both swap paths (copy job and CTAS).

**Guard (`recreateSpecGuard`):** a recreate keeps only the *configured* spec, so it refuses to proceed when it would silently drop a live spec half the user didn't configure — e.g. the table is partitioned but no `partition_by` is configured (a clustering change would drop the partitioning), or the table is clustered but the effective `cluster_by` is empty. The error tells the user to pass the missing option to keep (or change) it.

**Rename-aside steps:**

1. **Drop the target's primary-key constraint if present:**
   ```sql
   ALTER TABLE target DROP PRIMARY KEY IF EXISTS
   ```
   BigQuery refuses to RENAME a table that has a `PRIMARY KEY` constraint. The constraint is informational (`NOT ENFORCED`) and the recreated target's spec comes from the swap, so dropping it is safe. `IF EXISTS` makes this a no-op when the target has none (e.g. a CTAS-created target, which never has one).
2. **Rename the existing target out of the way:**
   ```sql
   ALTER TABLE target RENAME TO target__ingestr_repartition_<nonce>
   ```
   (the nonce is random; retried with a new nonce on the rare name collision). RENAME is not idempotent — a retried job can report failure even though an earlier attempt committed — so on a rename error the code probes whether the rename actually landed (`tableRenameLanded`) before treating it as failed.
3. **Set a 24h expiration on the aside table** (crash safety net if a later drop never runs):
   ```sql
   ALTER TABLE <aside> SET OPTIONS(expiration_timestamp = TIMESTAMP_ADD(CURRENT_TIMESTAMP(), INTERVAL 24 HOUR))
   ```
4. **Run the swap** (copy job or CTAS) into a fresh target with the new spec. Because the target name is now free, `CreateIfNeeded` / `CREATE OR REPLACE` creates it fresh with the new partition/clustering (no "different partitioning spec" error).
5. **Success:** drop the aside table (best-effort; the expiration cleans it up otherwise).
6. **Failure:** restore. But first, a special case: the swap may have "failed" only because `ctx` was canceled (Ctrl-C, timeout) while the job actually committed server-side — so if the **new target already exists, it is kept** (not clobbered) and the aside is left to expire. Otherwise the aside is renamed back to target — restoring its **original expiration first** (captured before the rename; `NULL` if it had none), because the 24h aside expiration survives the rename and would otherwise self-destruct the restored target — then the swap error is returned. The restore runs on a context detached from the (possibly canceled) request context.

The old data is kept until the new table is confirmed, so a failed swap is recoverable (rename back). All of RENAME, SET OPTIONS, DROP, and the copy job are free (0 bytes billed); only storage is paid.

> **Caveat:** recreating the target drops any table-level metadata ingestr does not itself manage but a user may have set manually on the destination — table/partition expiration, `require_partition_filter`, description, labels, column policy tags, row-level access policies, and CMEK. (The CTAS path already reset these via `CREATE OR REPLACE` regardless; the copy-job path preserved them on an in-place `WRITE_TRUNCATE` but not through a rename-aside recreate.)

**Scope:** this runs only in `SwapTable`, i.e. only for the replace strategy (and full-refresh, which forces replace). Merge and append upsert/insert in place and do not repartition.

## 7. Partition / Cluster Mismatch Check (`partitionOrClusterMismatch`)

The check only runs at all when a spec is configured (`partition_by` set, or the effective `cluster_by` non-empty — which includes the *default PK clustering*, so a clustering mismatch can trigger a recreate even with no user `cluster_by`). An empty configured spec means "leave as-is".

Returns `true` (recreate) if any of these differ from the configured spec:

- Range partitioning is present on the table (ingestr never creates range partitioning).
- Partition field differs, or is absent when a partition is configured (compared case-insensitively).
- Partition type differs from what ingestr creates (an unset type is treated as the default, DAY).
- Clustering fields differ by count, order, or name (case-insensitive) from the effective `cluster_by`.

## 8. `partition_by` / `cluster_by` Naming

- `partition_by` and `cluster_by` are normalized through the same naming convention as the columns (default snake_case; no-op under direct naming), because they name destination columns.
- Applies to both the user-specified value and a `PartitionedTable` source hint.
- Example: `partition_by "updatedAt"` becomes `"updated_at"` under snake_case, matching the destination column.

## 9. Merge Strategy (`MergeTable`)

- Runs a MERGE statement:
  ```sql
  MERGE target AS t
  USING (SELECT ... FROM staging QUALIFY ROW_NUMBER() OVER (PARTITION BY pk ...) = 1) AS s
  ON t.pk = s.pk
  WHEN MATCHED THEN UPDATE SET ...
  WHEN NOT MATCHED THEN INSERT ...
  ```
- **ON condition:** null-safe (`t.pk = s.pk OR (t.pk IS NULL AND s.pk IS NULL)`) for nullable PKs; bare `t.pk = s.pk` for non-nullable PKs (the null-safe OR disables clustered block pruning). "Non-nullable" means REQUIRED in the target table *or* NOT NULL in the ingestion schema.
- **Target partition pruning** (`buildMergePartitionPruning`): when the target's time-partition field is part of the merge PK (type DATE/TIMESTAMP/DATETIME, and not subject to a type cast), the script `DECLARE`s the staging min/max (and a has-null flag) of that field and adds a `t.<partition> BETWEEN min AND max` predicate to the `ON` clause, so the MERGE scans only the affected target partitions instead of the whole table. Skipped (with a debug log of the reason) when the partition field isn't a PK, has an unsupported type, or requires casting.
- **Type casts** (`buildCastMap`): staging/target schemas are compared and mismatched columns are `CAST` on the staging side of the MERGE.
- **CDC ordering:** CDC matches use the primary key (plus safe partition pruning) without the user incremental predicate; that predicate applies only to matched updates so an existing row cannot become a duplicate insert. Matched updates require a greater `_cdc_lsn`, except an equal-LSN delete may supersede an active row. The merge source combines the latest active row image with the latest overall CDC metadata; row data is applied only when that active image is not older than the target, while newer delete metadata can still advance independently. Its synthetic composition aliases are allocated case-insensitively around source columns. An unknown delete-only key is materialized as a payload-null tombstone so its LSN fences stale replay. PostgreSQL CDC is admitted because its source lease serializes runs. Other incremental CDC sources are rejected for BigQuery unless the pipeline can acquire a managed run lease, because BigQuery's informational primary key cannot prevent two concurrent MERGEs from inserting the same absent key.
- Operates in place on the existing destination; does not repartition. If the target was just created asynchronously by `PrepareTable` (the dedup path's normalised staging), MergeTable waits for that pending creation before running, so the MERGE doesn't 404.

## 10. Append Strategy

- Inserts/appends rows directly into the destination table; no swap.

(BigQuery also implements `DeleteInsertTable` — DELETE + INSERT wrapped in a `BEGIN TRANSACTION` script — and SCD2 SQL builders, used by the delete+insert and SCD2 strategies. Neither repartitions.)

## 11. Relevant BigQuery Constraints (all verified live)

- A table's partitioning cannot be altered in place; changing it requires recreating the table.
- `CREATE OR REPLACE TABLE` cannot replace a table with a different partitioning spec ("Instead, DROP the table, and then recreate it").
- A copy job (`WRITE_TRUNCATE`) cannot repartition an existing table ("incompatible partitioning spec").
- `ALTER TABLE RENAME` cannot rename onto an existing table (must drop/rename first); it is same-dataset only.
- A table with a `PRIMARY KEY` constraint cannot be renamed; drop it first (`ALTER TABLE ... DROP PRIMARY KEY IF EXISTS`).
- A `PRIMARY KEY` constraint is always `NOT ENFORCED` (BigQuery has no enforced mode); it is a query-optimizer hint, not a uniqueness guarantee.
- DDL (`DROP`/`CREATE`/`ALTER`) is not allowed inside a transaction, so there is no atomic drop+recreate.
- An `expiration_timestamp` survives `ALTER TABLE RENAME`.
- Copy jobs, RENAME, SET OPTIONS and DROP are free (0 bytes billed); CTAS is billed as a query.

## 12. Key Files

- `pkg/destination/bigquery/bigquery.go` — `PrepareTable`, `SwapTable`, `renameAsideSwap`, `renameTargetAside`, `restoreTargetFromAside`, `runCTASSwap`, `partitionOrClusterMismatch`, `recreateSpecGuard`, `effectiveClusterBy`, `MergeTable`, merge partition pruning helpers.
- `pkg/destination/bigquery/load_job.go` — `swapTableWithCopyJob` (copy job), load-job writes.
- `pkg/destination/bigquery/storage_write_arrow.go` — Storage Write API writes.
- `pkg/destination/bigquery/mapper.go` — `BuildTableMetadata`, `BuildBigQuerySchema`, `defaultClusteringFromPrimaryKeys`.
- `pkg/strategy/replace.go` — replace flow, `deduplicateStaging` (raw -> normalised).
- `pkg/strategy/merge.go` — merge flow.
- `pkg/pipeline/pipeline.go` — naming convention, `partition_by`/`cluster_by` normalization.
