# Postgres
Postgres is an open source, object-relational database system that provides reliability, data integrity, and correctness.

ingestr supports Postgres as both a source and destination.

## URI format
The URI format for Postgres is as follows:

```plaintext
postgresql://<username>:<password>@<host>:<port>/<database-name>?sslmode=<sslmode>
```

URI parameters:
- `username`: the user name to connect to the database
- `password`: the password for the user
- `host`: the host address of the database server
- `port`: the port number the database server is listening on, the default is 5432
- `database-name`: the name of the database to connect to
- `sslmode`: optional, the SSL mode to use when connecting to the database

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's Postgres dialect [here](https://docs.sqlalchemy.org/en/14/dialects/postgresql.html).

## Change Data Capture (postgres+cdc)

The `postgres+cdc://` scheme reads changes through PostgreSQL logical replication instead of querying tables: an initial snapshot of each table followed by every insert, update, and delete, applied to the destination with the `merge` strategy. It requires `wal_level=logical` on the source and a user with the `REPLICATION` privilege.

```plaintext
postgres+cdc://<username>:<password>@<host>:<port>/<database-name>?publication=<name>&slot=<name>&discover_interval=<duration>
```

By default a CDC run catches up with the current WAL position and exits. Pass the `--stream` CLI flag to ingest continuously instead.

CDC-specific URI parameters (all optional):
- `publication`: the logical-replication publication to read from. When omitted, ingestr creates and maintains a publication named `ingestr_publication` covering every logged table with a usable replica identity, reconciling it on every run.
- `slot`: the replication slot name. When omitted, ingestr derives a stable name from the publication.
- `mode`: **deprecated and ignored.** Continuous ingestion is controlled by `--stream`. `mode=batch` is accepted as a no-op; `mode=stream` is rejected unless `--stream` is also passed.
- `dest_schema`: a schema/dataset prefix for destination table names.
- `discover_interval`: how often a running stream re-checks the source for new tables (default `30s`, e.g. `1m`, `10s`). Set to `0` or `off` to disable mid-stream discovery.

Without `--source-table`, CDC runs in multi-table mode and replicates every table in the publication. Deletes are soft: rows are kept in the destination with `_cdc_deleted = true`.

### New tables

Tables created on the source after CDC has been set up are picked up automatically:

- **Without `--stream`**: the next run detects tables that have no state in the destination, snapshots their existing rows through a temporary replication slot (the main slot's position is untouched), and then streams their changes alongside the other tables.
- **With `--stream`**: the running stream re-checks the source every `discover_interval`. When a new table appears, ingestr adds it to the managed publication, backfills its existing rows, creates the destination table on the fly, and continues streaming — the other tables are not interrupted, and no data is lost while the stream rebuilds (the replication slot retains WAL during the pause).

With a user-managed publication (`publication=` supplied), ingestr never alters the publication: a new table is picked up after you run `ALTER PUBLICATION ... ADD TABLE` yourself (or immediately, if the publication was created `FOR ALL TABLES`).

The backfill-plus-stream handoff is safe under the `merge` strategy: changes that fall in the overlap between the snapshot and the WAL stream are applied idempotently by primary key. Tables without a primary key (or replica identity) cannot be part of logical replication and are skipped with a warning.
