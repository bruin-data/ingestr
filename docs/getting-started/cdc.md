---
outline: deep
---

# Change Data Capture (CDC)

Change Data Capture (CDC) is a way of ingesting data that reads a database's own change log — every insert, update, and delete — instead of repeatedly querying the tables. ingestr can capture those changes from several databases and apply them to your destination, keeping it in sync with the source without full reloads or `updated_at`-style polling.

## What is CDC?

Most ingestion strategies read data by *querying*: they run a `SELECT`, often filtered by an incremental key such as `updated_at`, and copy the rows they get back. That works well in many cases, but it has structural limits:

- **Deletes are invisible.** A query only returns rows that still exist, so a row deleted at the source silently lingers in the destination.
- **You need a reliable change column.** Incremental loading depends on a monotonic `updated_at`/`id` column. Not every table has one, and application bugs can leave it stale.
- **Intermediate states are lost.** If a row changes three times between two runs, a query-based load only ever sees the final value.
- **Repeated full scans are expensive.** Without a good incremental key, keeping a large table fresh means re-reading it.

CDC solves these by reading the database's **transaction log** (or an equivalent change feed) — the same internal mechanism the database uses for replication and crash recovery. Because the log records *every* row-level operation in commit order, CDC can reproduce inserts, updates, and deletes exactly as they happened at the source.

## How CDC works in ingestr

Every ingestr CDC connector follows the same two-phase shape:

1. **Snapshot.** On the first run, ingestr takes a consistent snapshot of each selected table and loads it into the destination. This establishes a known baseline and records the log position that corresponds to it.
2. **Stream.** From that position onward, ingestr reads the change log and applies each insert, update, and delete to the destination. Subsequent runs resume from where the previous one left off.

The snapshot-to-stream handoff is designed to be safe: changes that overlap the boundary are applied idempotently, so no data is lost or double-counted.

### The `merge` strategy and metadata columns

CDC runs always use the [`merge` strategy](/getting-started/incremental-loading.md#merge) so that updates and deletes can be applied by primary key — even if you pass `--incremental-strategy=replace`. Every CDC row is annotated with three metadata columns in the destination:

| Column | Meaning |
| --- | --- |
| `_cdc_lsn` | The source log position (LSN, GTID, resume token, or version) for the change. Used to order changes and to resume on the next run. |
| `_cdc_deleted` | `true` when the source row was deleted. Deletes are **soft**: the row is kept in the destination and flagged rather than physically removed. |
| `_cdc_synced_at` | When ingestr applied the change to the destination. |

Because a primary key is required to merge changes, **tables without a primary key (or an equivalent replica identity) are skipped** with a warning.

### Deletes

Deletes are captured as soft deletes: the destination keeps the row and sets `_cdc_deleted = true`. For most databases the deleted row's other columns are preserved as they were, so you retain the last known values. Some sources (SQL Server Change Tracking, PlanetScale) only emit the primary key of a deleted row, so ingestr marks the row deleted without disturbing its other columns.

### Resuming and full refresh

On every run after the first, a CDC connector resumes from the last durable position rather than re-snapshotting. If that saved position is no longer available in the source's log (for example, the log was truncated or retention expired), the run **fails instead of silently taking a partial snapshot**. To rebuild the destination from a fresh snapshot, run with `--full-refresh`.

### One-shot vs. streaming

By default a CDC run catches up to the current log position and exits — ideal for scheduled, batch-style syncs. Pass the `--stream` CLI flag to run continuously instead: ingestr stays up as a long-running process, flushing buffered changes to the destination on an interval or record-count trigger, until it is interrupted. Streaming gives at-least-once delivery, and the `merge` strategy makes replays idempotent. See [Streaming ingestion](/commands/ingest.md#streaming-ingestion) and [Monitoring a stream](/commands/ingest.md#monitoring-a-stream) for flags (`--flush-interval`, `--flush-records`, `--metrics-addr`) and lag metrics.

### Multi-table CDC

If you omit `--source-table`, most CDC connectors run in multi-table mode and replicate every eligible table in the source's capture set. Each table is snapshotted and streamed from its own position; a multi-table run is not a single global point-in-time snapshot across all tables, but each table is individually consistent. Use `--dest-schema` (or `dest_schema` in the URI) to control where multi-table output lands.

## Supported CDC platforms

ingestr selects the CDC path through a dedicated URI scheme (usually the database scheme with a `+cdc` suffix). Each platform reads changes through the mechanism native to that database:

| Platform | URI scheme(s) | Change mechanism | Docs |
| --- | --- | --- | --- |
| **PostgreSQL** | `postgres+cdc://`, `postgresql+cdc://` | Logical replication (WAL) | [Postgres CDC](/supported-sources/postgres.md#change-data-capture-postgrescdc) |
| **MySQL / MariaDB** | `mysql+cdc://`, `mysql+pymysql+cdc://`, `mariadb+cdc://` | Binary log (binlog) | [MySQL CDC](/supported-sources/mysql.md#change-data-capture) |
| **SQL Server (Change Tracking)** | `mssql+ct://`, `sqlserver+ct://`, `azuresql+ct://`, `azure-sql+ct://` | Change Tracking | [SQL Server Change Tracking](/supported-sources/mssql.md#change-tracking) |
| **SQL Server (log-based CDC)** | `mssql+cdc://`, `sqlserver+cdc://`, `azuresql+cdc://`, `azure-sql+cdc://` | SQL Server CDC capture tables | [SQL Server CDC](/supported-sources/mssql.md#change-data-capture) |
| **MongoDB** | `mongodb+cdc://`, `mongodb+srv+cdc://` | Change streams (oplog) | [MongoDB CDC](/supported-sources/mongodb.md#change-data-capture) |
| **Vitess** | `vitess+cdc://` | vtgate VStream API (gRPC) | [Vitess CDC](/supported-sources/vitess.md#change-data-capture) |
| **PlanetScale** | `ps_mysql+cdc://` | Hosted `psdbconnect` API | [PlanetScale CDC](/supported-sources/planetscale.md#change-data-capture) |

Each platform has requirements and knobs specific to its change mechanism — for example PostgreSQL needs `wal_level=logical` and a `REPLICATION`-privileged user, MySQL needs `binlog_format=ROW` with `binlog_row_image=FULL`, and SQL Server Change Tracking must be enabled per database and table. Follow the platform-specific link above for exact setup, URI parameters, and privileges.

### Platform notes at a glance

- **PostgreSQL** reads logical replication from a publication and slot. It can manage its own `ingestr_publication` or use one you supply, tracks progress in shared state tables in the destination (not the max `_cdc_lsn` of a user table), and automatically discovers tables added after setup. `TRUNCATE` is captured as a table replacement.
- **MySQL / MariaDB** stream the binary log after a consistent snapshot, resuming from the destination's maximum `_cdc_lsn`. Pin a unique `server_id` for scheduled or overlapping runs.
- **SQL Server** offers two paths: lightweight **Change Tracking** (net changes since the last version, primary key required) and **log-based CDC** (full row-level change history). Both resume from `_cdc_lsn`.
- **MongoDB** tails change streams from a replica set / Atlas cluster. Being schema-less, it uses schema inference, so the destination schema is derived from sampled documents.
- **Vitess** streams through vtgate's VStream API because a sharded cluster has no single binlog to tail; the position is a VGTID covering every shard, so it works for sharded and unsharded keyspaces alike.
- **PlanetScale** is managed Vitess but does not expose VStream externally, so ingestr uses PlanetScale's hosted `psdbconnect` API over TLS, authenticating with the database credentials already in the URI.

## CDC vs. regular incremental loading

CDC is powerful, but it is not always the right tool. Prefer CDC when:

- you need **deletes** reflected in the destination,
- your tables lack a reliable incremental key,
- you want near-real-time sync with `--stream`, or
- repeated full/incremental scans of large tables are too expensive.

Prefer [regular incremental loading](/getting-started/incremental-loading.md) when:

- the source is an API/SaaS platform or a database without a usable change log,
- you cannot enable logical replication / binlog / Change Tracking on the source,
- an `updated_at` column already gives you everything you need, or
- you only need periodic batch refreshes and simplicity matters more than delete-awareness.

## Example

A minimal PostgreSQL CDC run into DuckDB:

```bash
ingestr ingest \
    --source-uri "postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub" \
    --source-table "public.users" \
    --dest-uri "duckdb:///warehouse.duckdb" \
    --dest-table "public.users"
```

The first run snapshots `public.users` and records the WAL position; each later run applies the inserts, updates, and deletes that happened since. Add `--stream` to keep the process running and apply changes continuously.

## Hands-on tutorials

Step-by-step walkthroughs that set up a source, take the initial snapshot, and capture live inserts, updates, and deletes into DuckDB:

- [Replicate PostgreSQL to DuckDB with CDC](/tutorials/cdc-postgres-duckdb.md)
- [Replicate MySQL to DuckDB with CDC](/tutorials/cdc-mysql-duckdb.md)
- [Replicate SQL Server to DuckDB with CDC](/tutorials/cdc-sqlserver-duckdb.md)
- [Replicate MongoDB to DuckDB with CDC](/tutorials/cdc-mongodb-duckdb.md)

## See also

- [Incremental Loading](/getting-started/incremental-loading.md) — write strategies, including the `merge` strategy CDC builds on.
- [`ingest` command](/commands/ingest.md) — full CLI reference, including `--stream` and streaming flags.
- Platform CDC docs: [Postgres](/supported-sources/postgres.md#change-data-capture-postgrescdc), [MySQL](/supported-sources/mysql.md#change-data-capture), [SQL Server](/supported-sources/mssql.md#change-data-capture), [MongoDB](/supported-sources/mongodb.md#change-data-capture), [Vitess](/supported-sources/vitess.md#change-data-capture), [PlanetScale](/supported-sources/planetscale.md#change-data-capture).
