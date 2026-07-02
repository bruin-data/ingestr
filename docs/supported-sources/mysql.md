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

## TLS / SSL
Servers that require encrypted connections reject plain connections with `server does not allow insecure connections, client must use SSL/TLS`. Enable TLS with the `tls` query parameter:

```plaintext
mysql://user:password@host:port/dbname?tls=true
```

Accepted values:
- `tls=true`: connect over TLS and verify the server certificate against the system roots.
- `tls=skip-verify`: connect over TLS but skip certificate verification (use for self-signed certificates).
- `tls=preferred`: use TLS if the server offers it, otherwise fall back to plaintext.

## Vitess & PlanetScale
Vitess and PlanetScale speak the MySQL wire protocol but are selected with their own URI schemes so ingestr uses the correct read, write, and CDC paths:

- **Vitess** — use the `vitess://` scheme. See [Vitess](/supported-sources/vitess.md).
- **PlanetScale** — use the `ps_mysql://` scheme. See [PlanetScale](/supported-sources/planetscale.md).

Pointing a `mysql://` URI at a Vitess or PlanetScale server fails fast with a message telling you to switch to the dedicated scheme.

## Change data capture
CDC uses the `mysql+cdc://`, `mysql+pymysql+cdc://`, and `mariadb+cdc://` URI schemes for standard MySQL and MariaDB, which stream the binary log. It produces the `_cdc_lsn`, `_cdc_deleted`, and `_cdc_synced_at` metadata columns and resumes from the destination table's maximum `_cdc_lsn` on subsequent runs. (Vitess and PlanetScale have their own CDC schemes — see [Vitess](/supported-sources/vitess.md) and [PlanetScale](/supported-sources/planetscale.md).)

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
