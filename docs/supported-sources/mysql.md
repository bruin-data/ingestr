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

## Change data capture
MySQL CDC is supported with the `mysql+cdc://`, `mysql+pymysql+cdc://`, and `mariadb+cdc://` URI schemes. It reads a consistent snapshot first, then resumes from the destination table's maximum `_cdc_lsn` on subsequent runs.

If the saved `_cdc_lsn` is invalid or no longer available in MySQL binary logs, the run fails instead of taking a partial snapshot. Run with `--full-refresh` to rebuild the destination from a fresh snapshot.

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
