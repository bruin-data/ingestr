# Load a PostgreSQL snapshot with ingestr and start CDC

This walkthrough separates the initial table copy from change capture: obtain a snapshot and its matching log sequence number (LSN), load that snapshot with ingestr, then start PostgreSQL CDC from the saved LSN. Ordinary inserts, updates, and deletes can continue during the copy.

If you want ingestr to handle everything in one run, use `postgres+cdc://` without `--cdc-start-lsn`. It coordinates the snapshot and WAL boundary automatically; subsequent runs resume from saved destination checkpoints. See [Replicate PostgreSQL to DuckDB with CDC](/tutorials/cdc-postgres-duckdb.md). The separate workflow below is useful when you need to control the initial load and explicitly adopt it as a CDC baseline.

## Before you start

The example copies `shop.public.customers` on port `5432` into `warehouse.public.customers` on port `5433`. Replace the connection details with your own. Use a fresh destination table and a new replication slot dedicated to this connector.

- Enable `wal_level=logical` and provide enough replication slots and WAL sender connections. The source user needs `REPLICATION`, schema access, and `SELECT` on the complete table. See [source setup](/tutorials/cdc-postgres-duckdb.md#_1-prepare-the-source).
- Use a table with a primary key. The example assumes the `public.customers` table from that tutorial. Create the `warehouse` database beforehand; ingestr will create its destination table.
- Allow the destination user to create and alter tables and write ingestr's managed staging state.
- Keep table definitions, replica identity, and publication membership fixed throughout the handoff. Postpone `TRUNCATE` as well: it and table-rewriting DDL can invalidate the assumptions of an older snapshot. [PostgreSQL documents these MVCC limitations](https://www.postgresql.org/docs/16/mvcc-caveats.html).
- Plan enough source storage for WAL generated during the entire copy and catch-up. An unused replication slot retains WAL, even after its creating session disconnects.

Use a direct connection to the same PostgreSQL primary for the export and every snapshot read. This walkthrough does not cover standby exports or a failover during the handoff.

## 1. Create the publication

In a normal SQL session on the source, create a publication containing the complete table. Run this as the table owner with `CREATE` privilege on the database, or as an administrator:

```sql
CREATE PUBLICATION customers_pub FOR TABLE public.customers;
```

The default publication includes inserts, updates, deletes, and truncates. Do not use a row filter or a publication column list for this workflow. The snapshot reader must also see every source row: a role restricted by row-level security is not sufficient.

## 2. Obtain the snapshot and its LSN together

In **terminal A**, open an interactive replication connection:

```bash
psql -X 'postgresql://user:password@localhost:5432/shop?replication=database'
```

Run this command at the `psql` prompt, outside a transaction:

```sql
CREATE_REPLICATION_SLOT customers_slot LOGICAL pgoutput EXPORT_SNAPSHOT;
```

PostgreSQL returns a row like this:

```text
    slot_name   | consistent_point |    snapshot_name    | output_plugin
----------------+------------------+---------------------+---------------
 customers_slot | 0/16B6C50        | 00000003-0000001B-1 | pgoutput
```

Save both values from **your actual output**:

| Value | What it is used for |
|---|---|
| `snapshot_name` | The `snapshot=` URI parameter for the initial ingestr load. |
| `consistent_point` | The `--cdc-start-lsn` boundary when starting CDC. |

**Leave terminal A connected and idle until every initial load finishes. Do not run another SQL command there.** A new command or a closed replication connection expires the exported snapshot. Avoid `psql -c` for slot creation because it exits immediately. The slot is persistent because the command does not specify `TEMPORARY`. See PostgreSQL's [replication protocol](https://www.postgresql.org/docs/16/protocol-replication.html).

The two returned values describe one coordinated handoff: the exported snapshot contains the baseline, and the slot supplies subsequent changes. An independently sampled `pg_current_wal_lsn()`, a timestamp, or a slot created after an unrelated copy cannot establish that relationship. See [exported snapshots for logical decoding](https://www.postgresql.org/docs/16/logicaldecoding-explanation.html#LOGICALDECODING-EXPLANATION-EXPORTEDSNAPSHOTS).

## 3. Load the snapshot with ingestr

In **terminal B**, record the values returned in terminal A. These are illustrative values; replace them:

```bash
SNAPSHOT_ID='00000003-0000001B-1'
START_LSN='0/16B6C50'
```

Run a regular PostgreSQL ingestion with `snapshot=`:

```bash
ingestr ingest \
  --source-uri "postgres://user:password@localhost:5432/shop?snapshot=${SNAPSHOT_ID}" \
  --source-table public.customers \
  --dest-uri 'postgres://user:password@localhost:5433/warehouse' \
  --dest-table public.customers \
  --incremental-strategy replace
```

Ingestr imports the specified snapshot into its source read transactions. The copy sees the rows as of that snapshot even if the application changes them while ingestion runs. An invalid or expired snapshot fails the load; ingestr does not silently substitute a current snapshot.

Copy all rows and all data columns without incremental filters, SQL limits, or transformations. Wait for the command to succeed before starting CDC. `replace` replaces the selected destination table; use the new destination prepared for this workflow.

For several tables, include them all in the publication before creating the slot, and repeat the load for each table using **the same `SNAPSHOT_ID`**. Keep terminal A open until all loads succeed. Every destination table selected for CDC must represent this same boundary; separate snapshots taken at different times do not form a valid multi-table baseline.

## 4. Release the export session and check the slot

After all initial loads succeed, close terminal A's `psql` session with:

```text
\q
```

Keep the replication slot. In a separate normal SQL session on the source, inspect it:

```sql
SELECT slot_name, plugin, database, active, temporary,
       restart_lsn, confirmed_flush_lsn, wal_status,
       pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn))
         AS retained_wal
FROM pg_replication_slots
WHERE slot_name = 'customers_slot';
```

The slot must be an inactive, persistent `pgoutput` slot in `shop`, with available WAL. Neither `restart_lsn` nor `confirmed_flush_lsn` may be ahead of your saved `START_LSN`. Do not attach another consumer or call functions that consume or advance the slot while loading the snapshot.

This query also helps monitor WAL retention while loading; run it in a separate connection, never terminal A. `retained_wal` estimates the WAL span since the slot's restart position, not the table's change volume. Monitor disk capacity and `max_slot_wal_keep_size`; `wal_status = 'unreserved'` means needed WAL is at risk of removal and `lost` means the slot is unusable. See [`pg_replication_slots`](https://www.postgresql.org/docs/16/view-pg-replication-slots.html).

## 5. Start CDC from the saved LSN

In terminal B, use the original `START_LSN` with the slot and publication created above:

```bash
ingestr ingest \
  --source-uri 'postgres+cdc://user:password@localhost:5432/shop?publication=customers_pub&slot=customers_slot' \
  --source-table public.customers \
  --dest-uri 'postgres://user:password@localhost:5433/warehouse' \
  --dest-table public.customers \
  --cdc-snapshot-mode never \
  --cdc-start-lsn "${START_LSN}" \
  --stream
```

Ingestr adopts the loaded table, adds CDC metadata, and applies changes accumulated since the snapshot. `never` prevents it from taking another initial or recovery snapshot. Omit `--stream` to catch up and exit.

For a multi-table baseline loaded into `public.customers` and `public.orders`, use:

```bash
ingestr ingest \
  --source-uri 'postgres+cdc://user:password@localhost:5432/shop?publication=customers_pub&slot=customers_slot&dest_schema=public' \
  --source-table public.customers,public.orders \
  --dest-uri 'postgres://user:password@localhost:5433/warehouse' \
  --cdc-table-naming table \
  --cdc-snapshot-mode never \
  --cdc-start-lsn "${START_LSN}" \
  --stream
```

This requires `public.orders` to have been included in steps 1–3. `--cdc-table-naming table` makes the targets match the names used during loading; the default multi-table naming with `dest_schema` would use `public.public_customers` and `public.public_orders`.

Untouched baseline rows have NULL CDC metadata. Deletes are soft by default. Query active rows using:

```sql
SELECT id, name, email
FROM public.customers
WHERE COALESCE(_cdc_deleted, false) = false;
```

Ingestr checks schema compatibility and the slot's WAL availability, but cannot prove that the rows you loaded came from the correct snapshot. Compare the completed baseline against that same snapshot when validating the copy; a count against the changing live source is not a consistency check.

## 6. Resume subsequent runs

After stopping the stream, rerun the same CDC command. Keeping the original `--cdc-start-lsn` and `--cdc-snapshot-mode never` is allowed: ingestr uses its latest saved checkpoint, not the original boundary. You can also omit both flags:

```bash
ingestr ingest \
  --source-uri 'postgres+cdc://user:password@localhost:5432/shop?publication=customers_pub&slot=customers_slot' \
  --source-table public.customers \
  --dest-uri 'postgres://user:password@localhost:5433/warehouse' \
  --dest-table public.customers \
  --stream
```

The saved `never` policy still applies. Keep the source/destination identity, slot, publication, selected tables, naming, and managed staging namespace unchanged. Preserve the destination's `cdc_state` and `cdc_targets` tables alongside the destination data. For the multi-table example, keep its original selection and naming flags on every restart.

## What can go wrong

| Situation | What to do |
|---|---|
| The initial copy fails while terminal A is still open and idle. | Retry the failed `replace` load with the same snapshot. Start CDC only after every selected table loads successfully. |
| The exporter disconnects before all tables finish loading. | The snapshot cannot be reopened by knowing its LSN. If the complete baseline was not successfully loaded, establish a new snapshot/slot pair and reload every selected table. Do not mix old and new copies. |
| All loads succeeded, but CDC has not started yet. | The exported snapshot can expire now. The original persistent slot and retained WAL are what CDC needs. Keep the saved LSN. |
| WAL is lost, the slot is dropped/recreated, or another consumer advances it past the boundary. | A higher LSN would skip changes. Establish a new consistent baseline and connector, or restore a valid matching baseline, state, and WAL. |
| The copy used a normal `postgres://` URI without `snapshot=` while writes continued. | It is not tied to the slot's snapshot. Repeat the load correctly; reading the current LSN afterward cannot repair the gap. |
| The load is slow. | Monitor retained WAL and source disk usage. Long-lived snapshots also delay vacuum cleanup. Plan capacity or use ingestr's automatic [chunked snapshots](/supported-sources/postgres.md#large-initial-snapshots). |
| CDC bootstrap or streaming is interrupted. | Retry the same CDC command with its original boundary and intact state. Do not rerun the initial `replace` load over an adopted destination. |
| Schema, keys, selected tables, or destination identity change after adoption. | Snapshot mode `never` stops when it cannot safely continue. Restore the compatible setup or deliberately establish a new external baseline and connector. `--full-refresh` is rejected for this saved policy. |

After adoption, source `TRUNCATE` still clears the destination, and keyed changes may replay after a failure. Neither `never` nor the saved LSN prevents source deletes or truncates from being applied. Retire abandoned replication slots only after confirming they are no longer needed; leaving them indefinitely retains source WAL.

For the complete bootstrap restrictions and recovery behavior, see [CDC without snapshots](/supported-sources/postgres.md#cdc-without-snapshots).
