# Replicate MySQL to DuckDB with CDC

This walkthrough takes you from a running MySQL database to a DuckDB file kept in sync through [Change Data Capture](/getting-started/cdc.md): an initial snapshot, then inserts, updates, and deletes picked up by re-running ingestr. It assumes you already have a MySQL server you can connect to and [ingestr installed](/getting-started/quickstart.md#installation).

For the full reference on the connector's options, see [MySQL → Change data capture](/supported-sources/mysql.md#change-data-capture).

## 1. Prepare the source

MySQL CDC reads the binary log, so it must be enabled in `ROW` format with full row images. Check the current settings:

```sql
SELECT @@log_bin, @@binlog_format, @@binlog_row_image;
```

You want `@@log_bin = 1`, `@@binlog_format = ROW`, and `@@binlog_row_image = FULL`. On MySQL 8.0 these are the defaults; if not, set them in your server config (`log_bin`, `binlog_format=ROW`, `binlog_row_image=FULL`) and restart.

The connecting user needs read access plus replication privileges. A dedicated user is straightforward:

```sql
CREATE USER 'ingestr_cdc'@'%' IDENTIFIED BY 'cdcpass';
GRANT SELECT, RELOAD, REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO 'ingestr_cdc'@'%';
FLUSH PRIVILEGES;
```

`SELECT` and `RELOAD` cover the consistent initial snapshot; `REPLICATION SLAVE` and `REPLICATION CLIENT` cover streaming the binary log.

We'll replicate a small `customers` table. Create one to follow along (or point the commands at a table of your own — it just needs a primary key):

```sql
CREATE TABLE customers (
    id    INT PRIMARY KEY,
    name  VARCHAR(100) NOT NULL,
    email VARCHAR(255)
);
INSERT INTO customers (id, name, email) VALUES
    (1, 'Alice', 'alice@example.com'),
    (2, 'Bob',   'bob@example.com'),
    (3, 'Carol', 'carol@example.com');
```

## 2. Run the initial load

Run ingestr with the `mysql+cdc://` scheme to take the initial snapshot into DuckDB. Pin a unique `server_id` so the connector has a stable replication identity:

```bash
ingestr ingest \
  --source-uri "mysql+cdc://ingestr_cdc:cdcpass@localhost:3306/shop?server_id=1001" \
  --source-table "customers" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "shop.customers"
```

The run snapshots the table, records its binary-log position, and exits. Inspect the result:

```bash
duckdb warehouse.duckdb "SELECT id, name, email, _cdc_deleted FROM shop.customers ORDER BY id;"
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
INSERT INTO customers (id, name, email) VALUES (4, 'Dave', 'dave@example.com');
UPDATE customers SET email = 'alice@newmail.com' WHERE id = 1;
DELETE FROM customers WHERE id = 2;
```

Now run **exactly the same command again**. Instead of re-snapshotting, ingestr resumes from the binary-log position stored in the destination's maximum `_cdc_lsn` and applies only what changed:

```bash
ingestr ingest \
  --source-uri "mysql+cdc://ingestr_cdc:cdcpass@localhost:3306/shop?server_id=1001" \
  --source-table "customers" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "shop.customers"
```

```bash
duckdb warehouse.duckdb "SELECT id, name, email, _cdc_deleted FROM shop.customers ORDER BY id;"
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

The insert and update are applied, and the delete is a **soft delete**: `id = 2` stays in DuckDB with `_cdc_deleted = true` and its last known values intact. Filter it out at query time with `WHERE _cdc_deleted = false`. Schedule this same command (for example with cron) to keep the destination continuously up to date; each run picks up where the previous one stopped.

## 4. Start over if needed

If the saved position is no longer available in MySQL's binary logs (for example the logs were purged), the run fails rather than taking a partial snapshot. To discard the destination state and rebuild from a fresh snapshot, add `--full-refresh`:

```bash
ingestr ingest \
  --source-uri "mysql+cdc://ingestr_cdc:cdcpass@localhost:3306/shop?server_id=1001" \
  --source-table "customers" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "shop.customers" \
  --full-refresh
```

## See also

- [Change Data Capture](/getting-started/cdc.md) — how CDC works across ingestr, and the other supported platforms.
- [MySQL source reference](/supported-sources/mysql.md) — all URI parameters, requirements, and CDC internals.
