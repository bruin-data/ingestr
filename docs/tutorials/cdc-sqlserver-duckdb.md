# Replicate SQL Server to DuckDB with CDC

This walkthrough takes you from a running SQL Server database to a DuckDB file kept in sync through [Change Data Capture](/getting-started/cdc.md): an initial snapshot, then inserts, updates, and deletes picked up by re-running ingestr. It assumes you already have a SQL Server instance you can connect to and [ingestr installed](/getting-started/quickstart.md#installation).

This tutorial uses SQL Server's **log-based CDC** (the `mssql+cdc://` scheme). SQL Server also offers a lighter-weight [Change Tracking](/supported-sources/mssql.md#change-tracking) path (`mssql+ct://`) if you only need to know which rows changed.

## 1. Prepare the source

Log-based CDC is driven by the SQL Server Agent, so the Agent must be running on your instance. Then enable CDC at the database level and on the table you want to replicate. Connect as an administrator (`sysadmin`) and run:

```sql
-- enable CDC for the database
USE shop;
EXEC sys.sp_cdc_enable_db;

-- our demo table (any table with a primary key works)
CREATE TABLE dbo.customers (
    id    INT PRIMARY KEY,
    name  NVARCHAR(100) NOT NULL,
    email NVARCHAR(255)
);

-- enable CDC for the table; this creates the capture instance "dbo_customers"
EXEC sys.sp_cdc_enable_table
    @source_schema = 'dbo',
    @source_name   = 'customers',
    @role_name     = NULL;

INSERT INTO dbo.customers (id, name, email) VALUES
    (1, 'Alice', 'alice@example.com'),
    (2, 'Bob',   'bob@example.com'),
    (3, 'Carol', 'carol@example.com');
```

`sp_cdc_enable_table` starts the capture and cleanup jobs; you can confirm the capture instance with `SELECT capture_instance FROM cdc.change_tables;`. The user ingestr connects as needs `SELECT` on the table (and on the `cdc` schema, which `@role_name = NULL` leaves ungated).

## 2. Run the initial load

Run ingestr with the `mssql+cdc://` scheme to take the initial snapshot into DuckDB:

```bash
ingestr ingest \
  --source-uri "mssql+cdc://user:password@localhost:1433/shop?encrypt=disable" \
  --source-table "dbo.customers" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "dbo.customers"
```

If your password contains URL-reserved characters (like `!` or `@`), percent-encode them in the URI (`!` → `%21`). The `encrypt=disable` parameter is fine for a local instance; drop it (or set the appropriate encryption options) when connecting to a server that requires TLS.

The run snapshots the table, records its log position, and exits. Inspect the result:

```bash
duckdb warehouse.duckdb "SELECT id, name, email, _cdc_deleted FROM dbo.customers ORDER BY id;"
```

```plaintext
┌───────┬─────────┬───────────────────┬──────────────┐
│  id   │  name   │       email       │ _cdc_deleted │
├───────┼─────────┼───────────────────┼──────────────┤
│     1 │ Alice   │ alice@example.com │ false        │
│     2 │ Bob     │ bob@example.com   │ false        │
│     3 │ Carol   │ carol@example.com │ false        │
└───────┴─────────┴───────────────────┴──────────────┘
```

Alongside your columns, ingestr adds the CDC metadata columns `_cdc_lsn`, `_cdc_deleted`, and `_cdc_synced_at`.

## 3. Capture ongoing changes

Change the source:

```sql
INSERT INTO dbo.customers (id, name, email) VALUES (4, 'Dave', 'dave@example.com');
UPDATE dbo.customers SET email = 'alice@newmail.com' WHERE id = 1;
DELETE FROM dbo.customers WHERE id = 2;
```

SQL Server's capture job scans the transaction log on an interval (a few seconds by default), so give it a moment to record the changes into the CDC tables. Then run **exactly the same command again** — ingestr resumes from the log position stored in the destination's maximum `_cdc_lsn` and applies only what changed:

```bash
ingestr ingest \
  --source-uri "mssql+cdc://user:password@localhost:1433/shop?encrypt=disable" \
  --source-table "dbo.customers" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "dbo.customers"
```

```bash
duckdb warehouse.duckdb "SELECT id, name, email, _cdc_deleted FROM dbo.customers ORDER BY id;"
```

```plaintext
┌───────┬─────────┬───────────────────┬──────────────┐
│  id   │  name   │       email       │ _cdc_deleted │
├───────┼─────────┼───────────────────┼──────────────┤
│     1 │ Alice   │ alice@newmail.com │ false        │
│     2 │ Bob     │ bob@example.com   │ true         │
│     3 │ Carol   │ carol@example.com │ false        │
│     4 │ Dave    │ dave@example.com  │ false        │
└───────┴─────────┴───────────────────┴──────────────┘
```

The insert and update are applied, and the delete is a **soft delete**: `id = 2` stays in DuckDB with `_cdc_deleted = true` and its last known values intact. Filter it out at query time with `WHERE _cdc_deleted = false`. Schedule this same command (for example with cron) to keep the destination up to date, or add `--stream` to run continuously instead of once per invocation.

## 4. Start over if needed

To discard the destination state and rebuild from a fresh snapshot, add `--full-refresh`:

```bash
ingestr ingest \
  --source-uri "mssql+cdc://user:password@localhost:1433/shop?encrypt=disable" \
  --source-table "dbo.customers" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "dbo.customers" \
  --full-refresh
```

## See also

- [Change Data Capture](/getting-started/cdc.md) — how CDC works across ingestr, and the other supported platforms.
- [SQL Server source reference](/supported-sources/mssql.md) — Change Tracking, log-based CDC, and connection options.
