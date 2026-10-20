# StarRocks

[StarRocks](https://www.starrocks.io/) is a high-performance analytical (OLAP) database. Alongside its own internal storage, StarRocks can query open lakehouse table formats — such as **Apache Iceberg**, **Apache Hudi**, **Apache Hive**, and **Delta Lake** — through external catalogs.

ingestr supports StarRocks as a source (including tables exposed through external lakehouse catalogs) and as a destination.

## URI format
The URI format for StarRocks is as follows:

```plaintext
starrocks://<username>:<password>@<host>:<port>/<database>
starrocks://<username>:<password>@<host>:<port>/<catalog>/<database>
```

URI parameters:
- `username`: the StarRocks user (required)
- `password`: the password for the user (optional, depending on authentication)
- `host`: the StarRocks FE (frontend) hostname or IP address
- `port`: the FE query port that speaks the MySQL protocol (default: `9030`)
- `path`: the default catalog and database for unqualified table names. A single segment (`/<database>`) sets the default database and leaves the catalog at `default_catalog`; two segments (`/<catalog>/<database>`) set both. Anything specified in the source table name takes priority over these defaults.
- `ssl` (query parameter, optional): enable a TLS-encrypted connection. Use `ssl=true` to connect over TLS and verify the server certificate (recommended for managed/cloud StarRocks), or `ssl=skip-verify` to use TLS without certificate verification.

StarRocks speaks the MySQL wire protocol, so it is reached through the FE query port (`9030` by default), not the HTTP port.

Example with TLS:

```plaintext
starrocks://<username>:<password>@<host>:<port>/<database>?ssl=true
```

## Table naming
StarRocks organizes tables as `catalog.database.table`. You can specify a source table in any of these forms:

```plaintext
table                       # uses the default catalog and database from the URI
database.table              # uses the default catalog from the URI; database overrides the URI default
catalog.database.table      # fully qualified; overrides the URI defaults entirely
```

The table name always takes priority over the catalog/database defaults from the URI.

- `default_catalog` is StarRocks' built-in internal storage.
- Any other catalog name refers to an [external catalog](https://docs.starrocks.io/docs/data_source/catalog/catalog_overview/) you have created in StarRocks (Iceberg, Hudi, Hive, Delta Lake, JDBC, ...).

## Examples

### Read an internal table into DuckDB
```bash
ingestr ingest \
    --source-uri 'starrocks://root:pass@localhost:9030/analytics' \
    --source-table 'analytics.events' \
    --dest-uri 'duckdb:///output.db' \
    --dest-table 'main.events'
```

### Read an Iceberg table via an external catalog
Given an Iceberg catalog named `iceberg_catalog` configured in StarRocks:

```bash
ingestr ingest \
    --source-uri 'starrocks://root:pass@localhost:9030/analytics' \
    --source-table 'iceberg_catalog.lakehouse.trips' \
    --dest-uri 'postgresql://user:pass@localhost:5432/warehouse' \
    --dest-table 'public.trips'
```

### Read a Hudi table via an external catalog
```bash
ingestr ingest \
    --source-uri 'starrocks://root:pass@localhost:9030/analytics' \
    --source-table 'hudi_catalog.lakehouse.payments' \
    --dest-uri 'bigquery://my-project/analytics' \
    --dest-table 'analytics.payments'
```

### Set the default database in the URI
With a single path segment, unqualified table names resolve against that database in the internal catalog. Here `events` resolves to `default_catalog.analytics.events`:

```bash
ingestr ingest \
    --source-uri 'starrocks://root:pass@localhost:9030/analytics' \
    --source-table 'events' \
    --dest-uri 'duckdb:///output.db' \
    --dest-table 'main.events'
```

### Set the default catalog and database in the URI
With two path segments (`/<catalog>/<database>`), unqualified table names resolve against that external catalog — useful when reading many tables from one lakehouse catalog. Here `trips` resolves to `iceberg_catalog.lakehouse.trips`:

```bash
ingestr ingest \
    --source-uri 'starrocks://root:pass@localhost:9030/iceberg_catalog/lakehouse' \
    --source-table 'trips' \
    --dest-uri 'duckdb:///output.db' \
    --dest-table 'main.trips'
```

The URI path only provides defaults for the parts the table name omits. A `database.table` source table overrides the URI's default database; a `catalog.database.table` source table overrides both the default catalog and database.

## Incremental loading
StarRocks supports incremental loads. Provide an incremental key column (e.g. an event timestamp) together with `--interval-start`/`--interval-end` so each run only pulls rows in that window, and use `merge` with a primary key to upsert them:

```bash
ingestr ingest \
    --source-uri 'starrocks://root:pass@localhost:9030/analytics' \
    --source-table 'analytics.events' \
    --dest-uri 'duckdb:///output.db' \
    --dest-table 'main.events' \
    --incremental-strategy merge \
    --incremental-key updated_at \
    --primary-key id \
    --interval-start 2024-01-01 \
    --interval-end 2024-02-01
```

The interval flags are optional. If you omit them, ingestr reads all rows and `merge` upserts them on the primary key — an idempotent full refresh rather than a windowed incremental pull.

## Custom queries
You can read the result of an arbitrary SQL query instead of a table. See [Custom Queries](/supported-sources/custom_queries.md) for details.

## Using StarRocks as a destination
StarRocks can also be a destination. ingestr creates the table (a duplicate-key table, or a primary-key table when a primary key is given) and loads rows through StarRocks' [Stream Load](https://docs.starrocks.io/docs/loading/StreamLoad/) HTTP API.

Stream Load uses the FE HTTP port (default `8030`), which is separate from the query port (`9030`). Set it with the `http_port` query parameter if it differs:

```bash
ingestr ingest \
    --source-uri 'postgresql://user:pass@localhost:5432/sourcedb' \
    --source-table 'public.events' \
    --dest-uri 'starrocks://root:pass@localhost:9030/analytics?http_port=8030' \
    --dest-table 'analytics.events'
```

Destination URI parameters:
- `http_port` (optional): the FE HTTP port used for Stream Load. Defaults to `8030`.
- `replication_num` (optional): the replica count for created tables. If omitted, the cluster default is used; set it to `1` for single-backend clusters.
- `ssl` (optional): same TLS handling as the source (applies to the query-port connection).

### Supported write dispositions
- `replace` — atomically replaces the target table, including its schema and keys. A failed load leaves the existing table intact.
- `append` — loads rows into the existing table. If the source has a primary key (detected automatically or given with `--primary-key`), the table is created as a StarRocks primary-key table, so appended rows with an existing key replace the stored row instead of adding a duplicate.
- `merge` — upserts by primary key (the table is created as a StarRocks primary-key table). ingestr detects source primary keys automatically; use `--primary-key` to specify them explicitly. An existing target must be a primary-key table with the same key columns; otherwise the run fails before merging into it and you need `--full-refresh` to rebuild it.

`delete+insert` and `scd2` are not currently supported for StarRocks destinations.

### Schema evolution
ingestr adds new columns, relaxes nullability, and applies the type changes StarRocks can make in place: wider integers, integer or `FLOAT` to `DOUBLE`, longer `VARCHAR`, `DECIMAL` with at least as many integer digits and as much scale, `DATE` to `DATETIME` and back, `VARCHAR` to a number, `DATE`, or `JSON`, and numbers or `JSON` to a `VARCHAR` long enough for any value. Other changes fail before any DDL runs, for example `DATE` to `VARCHAR`, `ARRAY` element changes, changes to primary-key or hash-distribution columns, `FLOAT`/`DOUBLE` on a sort-key column, type or nullability changes in Unique or Aggregate key tables, and type or nullability changes to columns of types ingestr does not create (such as `LARGEINT`, `CHAR`, `MAP`, `STRUCT`); rerun with `--full-refresh` to rebuild the table. ingestr waits for asynchronous StarRocks schema changes to finish before loading rows.

### PostgreSQL CDC

StarRocks supports PostgreSQL CDC snapshots, incremental catch-up, continuous streaming, restart from durable checkpoints, and full refresh. Keyed tables use StarRocks Primary Key tables, including composite keys. Changes are ordered by `_cdc_lsn`; replaying an older change cannot overwrite a newer row or resurrect a deleted row. Unchanged PostgreSQL TOAST values are preserved across updates, including multiple updates in one flush and explicit changes to `NULL`.

Enable PostgreSQL logical replication (`wal_level=logical`) and give the source user the `REPLICATION` privilege and access to the replicated tables. A custom publication must include inserts, updates, deletes, and truncates. See [PostgreSQL CDC](/supported-sources/postgres.md#change-data-capture-postgrescdc) for publication setup, replica identity, and key changes involving TOAST values.

Replicate one table:

```bash
ingestr ingest \
    --source-uri 'postgres+cdc://user:pass@localhost:5432/sourcedb?publication=analytics_pub' \
    --source-table 'public.events' \
    --dest-uri 'starrocks://root:pass@localhost:9030/analytics?http_port=8030&replication_num=1' \
    --dest-table 'analytics.events' \
    --incremental-strategy merge
```

The first run takes a snapshot; subsequent runs resume and catch up before exiting. Add `--stream --flush-interval 5s --flush-records 10000` for continuous ingestion; `--flush-interval` bounds how long a change waits before it is loaded, even under a steady stream of changes. Use `--full-refresh` to replace the mirror from a fresh snapshot.

To replicate all tables in a publication, omit `--source-table` and `--dest-table`. Tables are created in the database from the destination URI; add `dest_schema=<database>` to the source URI to use a different one. Use `--cdc-table-naming table` to retain source table names, or leave the default to prefix names with their source schema. A comma-separated `--source-table` selects a subset. See [Multi-table CDC](/getting-started/cdc.md#multi-table-cdc).

Deletes remain in keyed tables as tombstones. Query the current mirror with:

```sql
SELECT * FROM analytics.events WHERE NOT _cdc_deleted;
```

Source `TRUNCATE` clears the affected target before subsequent changes are applied. When a streaming run re-snapshots a keyed table after source DDL (for example `DROP COLUMN`), the target is not emptied: snapshot rows overwrite the existing rows in place, and once the snapshot completes, rows it did not contain are marked `_cdc_deleted` at the snapshot position. Readers filtering on `NOT _cdc_deleted` keep seeing the full table throughout, and earlier tombstones are preserved. Keyless tables, and targets holding positions at or beyond the new snapshot, are still truncated first. Source key changes require a full refresh.

CDC state is stored in `_bruin_staging.cdc_state`, with ownership metadata in `_bruin_staging.cdc_targets` and one `cdc_claim_*` view per target. The append-only state log uses a Duplicate Key table so long connector/event identifiers do not exceed StarRocks' primary-key size limit. Claim views prevent two different connectors from owning the same target. The connector identity includes the source, the destination host, port, and database, and the table selection; changing `http_port`, `replication_num`, or `ssl` keeps the same connector. The destination user needs permissions to create databases, tables, and views, inspect table metadata and load transactions, and select, insert, alter, delete, truncate, and drop the managed tables. Preserve the state tables and claim views across runs. Staging tables left in `_bruin_staging` by a run that was killed are dropped by the next run of the same connector.

Stream Load rejects filtered or unselected rows. Batches whose JSON body would exceed StarRocks' 100 MB Stream Load limit are split automatically. Stream Loads, CDC merges, and cross-database replacement copies wait for committed loads to become query-visible before advancing. The client must be able to reach the backend HTTP address returned by the FE redirect. For a single-container Docker lab, expose the BE's `8040` port and use that mapped port as `http_port`, with the mapped FE `9030` port for SQL.

StarRocks versions with older load metadata, including 3.3.5 and earlier, use `SHOW PROC` to find INSERT transactions. This requires the system `OPERATE` privilege. These servers expose a limited history of recent transactions; ingestion stops if the transaction cannot be found or its visibility cannot be verified. StarRocks 3.3.6 and newer provide the load metadata used for direct transaction lookup.

Limits and operational requirements:

- Tested with PostgreSQL 16.15, MySQL 8.0, and StarRocks 3.3.22. Destination tables are in StarRocks' internal catalog.
- StarRocks Primary Key restrictions apply, including the cluster's encoded primary-key size limit (128 bytes by default). Bounded source strings map to `VARCHAR` sized in bytes (4 bytes per source character) and unbounded strings to `VARCHAR(1048576)`; tables created by earlier ingestr versions keep their `VARCHAR(65533)` columns until a full refresh. Decimals map to at most 38 digits of precision; unsupported values fail the load. Floating-point `NaN` and infinities load as `NULL`.
- Keyless PostgreSQL tables with `REPLICA IDENTITY FULL` become append-only change logs. They retain duplicate source rows and may contain replayed events after recovery. Filtering `_cdc_deleted` does not turn these logs into a current-state mirror.
- PostgreSQL serializes runs through its replication slot and source lock; MySQL through the destination lease. Keep one writer per target and avoid external drop/recreate or schema changes while ingestion is running. Physical table identities are checked before and after writes; those checks do not atomically lock out external DDL. Multi-table ingestion does not provide an atomic transaction across destination tables.

### MySQL CDC

StarRocks supports MySQL CDC (`mysql+cdc://`) snapshots, binlog catch-up, restart from durable checkpoints, and full refresh, single-table and multi-table. See [MySQL change data capture](/supported-sources/mysql.md#change-data-capture) for source setup (row-based binlog with full row images and GTIDs). By default each run snapshots or catches up to the binlog position captured at start and exits; add `--stream --flush-interval 5s` to keep reading the binlog continuously.

```bash
ingestr ingest \
    --source-uri 'mysql+cdc://user:pass@localhost:3306/shop?server_id=23001' \
    --dest-uri 'starrocks://root:pass@localhost:9030/analytics?http_port=8030' \
    --incremental-strategy merge
```

Runs of the same connector are serialized by a lease table in `_bruin_staging` (`cdc_lease_*`), renewed every few seconds. A run that finds the lease held waits up to about 45 seconds before failing, which lets a restart succeed after a crashed run's lease expires. Target identities are checked immediately before and after each merge, truncate, and full-refresh swap; StarRocks cannot lock out external DDL during those statements, so keep one writer per target. Multi-table runs merge tables one after another rather than in one transaction: readers can briefly see tables at different binlog positions, and after a crash the next run replays the uncommitted batch, which converges because merges are ordered by `_cdc_lsn`.

The MySQL CDC source stops on any DDL against a captured table (for example `ALTER TABLE ... ADD COLUMN`) and asks for `--full-refresh`, which re-snapshots the connector's tables and swaps them in atomically per table.

The Docker regression suite covers merge ordering, tombstones, TOAST, ownership claims, checkpoint replay/pruning, schema evolution, multi-table snapshots, catch-up, restart, truncation, streaming, in-place re-snapshots, and full refresh:

```bash
INTEGRATION_BACKENDS=starrocks \
go test -tags integration -count=1 -v -run '^TestStarRocksCDC$' ./tests/integration
```
