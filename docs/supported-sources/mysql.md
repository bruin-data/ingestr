# MySQL
MySQL is an open source relational database management system, known for its speed and reliability.

ingestr supports MySQL as a source and a destination.

## URI format
The URI format for MySQL is as follows:

```plaintext
mysql://user:password@host:port/dbname
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `host`: the host address of the database server
- `port`: the port number the database server is listening on, the default is 3306
- `dbname`: the name of the database to connect to

The same URI structure and table can be used both for sources and destinations. You can read more about SQLAlchemy's MySQL dialect [here](https://docs.sqlalchemy.org/en/20/core/engines.html#mysql).

## Vitess / PlanetScale
[Vitess](https://vitess.io/) and [PlanetScale](https://planetscale.com/) are supported out of the box — use the same `mysql://` URI as above, no extra configuration needed.

By default Vitess caps queries at 100,000 rows, which would otherwise break bulk reads of larger tables. ingestr detects Vitess automatically and works around this, so large tables ingest fully.

Change data capture is also supported for Vitess — see [Change data capture](#change-data-capture) below.

## Change data capture
CDC uses the `mysql+cdc://`, `mysql+pymysql+cdc://`, and `mariadb+cdc://` URI schemes. ingestr detects the server when it connects and picks the right mechanism automatically: standard MySQL and MariaDB stream the binary log, while Vitess and PlanetScale stream changes through [VStream](https://vitess.io/docs/reference/vreplication/vstream/) — Vitess is a sharded layer with no standard binary log to tail. Both produce the same `_cdc_lsn`, `_cdc_deleted`, and `_cdc_synced_at` metadata columns and resume from the destination table's maximum `_cdc_lsn` on subsequent runs.

### MySQL & MariaDB (binary log)
This path reads a consistent snapshot first, then streams the binary log, resuming from the destination table's maximum `_cdc_lsn` on subsequent runs.

If the saved `_cdc_lsn` is invalid or no longer available in MySQL binary logs, the run fails instead of taking a partial snapshot. Run with `--full-refresh` to rebuild the destination from a fresh snapshot.

Incremental CDC runs use the `merge` strategy even if `--incremental-strategy=replace` is supplied, so updates and deletes can be applied by primary key. Use `--full-refresh` to rebuild the destination from a fresh snapshot.

Example:

```shell
ingestr ingest \
  --source-uri "mysql+cdc://user:password@host:3306/dbname?mode=batch&server_id=18888" \
  --dest-uri "sqlite:///tmp/mysql_cdc.db" \
  --source-table "orders" \
  --dest-table "orders"
```

Requirements:
- Binary logging must be enabled with `log_bin=ON`.
- `binlog_format` must be `ROW`.
- `binlog_row_image` must be `FULL`.
- `binlog_row_value_options` must not include `PARTIAL_JSON`.
- Source tables must have primary keys, or `--primary-key` must be provided.
- Source tables must not contain `ENUM`, `SET`, or `BIT` columns.
- The source user needs normal read access, permission to run `FLUSH TABLES WITH READ LOCK` for the initial snapshot, and replication privileges required to stream binary logs.

CDC URI parameters:
- `mode`: `batch`; defaults to `batch`.
- `server_id`: optional positive uint32 replication server id; generated automatically when omitted. Pin a unique value for scheduled or overlapping CDC runs.
- `dest_schema`: optional destination schema for multi-table CDC runs.
- `flavor`: `mysql` or `mariadb`; inferred from the URI scheme unless overridden.

Multi-table CDC snapshots each selected table independently and then stream each table from its own snapshot position. Each table is consistent on its own, but a multi-table run is not a single global point-in-time snapshot across all tables.

### Vitess & PlanetScale (VStream)
For Vitess and PlanetScale, ingestr streams changes through vtgate's VStream API over gRPC. Use the same `mysql+cdc://` scheme — Vitess is detected automatically. None of the binary-log requirements above apply: there is no `FLUSH TABLES`, and `log_bin`/`binlog_format`/`binlog_row_image` settings are irrelevant.

VStream performs a consistent copy-phase snapshot first, then streams changes. Position is tracked with a Vitess GTID (VGTID) serialized into `_cdc_lsn`, and subsequent runs resume from the destination's maximum `_cdc_lsn`. This works for both unsharded and sharded keyspaces, since the VGTID covers every shard. If the stored `_cdc_lsn` is invalid, the run fails instead of taking a partial snapshot — run with `--full-refresh` to rebuild. Like the binary-log path, incremental runs use the `merge` strategy so updates and deletes are applied by primary key.

VStream uses vtgate's **gRPC** port, which is different from the MySQL protocol port and cannot be derived from it, so you must supply it with `grpc_port`. The database in the URI is the Vitess keyspace.

```shell
ingestr ingest \
  --source-uri "mysql+cdc://user:password@host:3306/keyspace?grpc_port=15991&mode=batch" \
  --dest-uri "duckdb:///tmp/vitess_cdc.duckdb" \
  --source-table "keyspace.orders" \
  --dest-table "orders"
```

Vitess CDC URI parameters:
- `grpc_port`: **required** — the vtgate gRPC port (for example `15991`). The run fails with a clear error if it is missing on a Vitess server.
- `grpc_host`: optional vtgate gRPC host; defaults to the host in the URI.
- `mode`: `batch`; defaults to `batch`.
- `dest_schema`: optional destination schema for multi-table CDC runs.

Requirements:
- The vtgate gRPC endpoint must be reachable (`grpc_port`, plus `grpc_host` if it differs from the MySQL host).
- Source tables must have primary keys, or `--primary-key` must be provided.
- Source tables must not contain `ENUM`, `SET`, or `BIT` columns.
