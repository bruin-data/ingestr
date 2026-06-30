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
- `replace` — loads into a staging table, then atomically replaces the target's data with `INSERT OVERWRITE`. StarRocks loads into temporary partitions and swaps them in only on success, so a failed load leaves the existing target data intact. (`SWAP WITH`/`RENAME` aren't used because they can't cross databases, and ingestr stages in a separate database.)
- `append` — loads rows into the existing table.
- `merge` — upserts by primary key (the table is created as a StarRocks primary-key table). Requires `--primary-key`.

`delete+insert` and `scd2` are not currently supported for StarRocks destinations.
