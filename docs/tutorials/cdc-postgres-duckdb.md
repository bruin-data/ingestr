# Replicate PostgreSQL to DuckDB with CDC

This walkthrough takes you from a running PostgreSQL database to a DuckDB file that stays in sync through [Change Data Capture](/getting-started/cdc.md): an initial snapshot, then live inserts, updates, and deletes. It assumes you already have a PostgreSQL server you can connect to and [ingestr installed](/getting-started/quickstart.md#installation).

For the full reference on the connector's options, see [Postgres → Change Data Capture](/supported-sources/postgres.md#change-data-capture-postgrescdc).

## 1. Prepare the source

Logical replication must be enabled. Check the current setting:

```sql
SHOW wal_level;   -- must return "logical"
```

If it returns `replica` (the default), set `wal_level = logical` in `postgresql.conf` (along with enough `max_wal_senders` and `max_replication_slots`, e.g. `10` each) and **restart** the server — this parameter cannot be changed at runtime.

The connecting user needs the `REPLICATION` attribute. Using an admin/superuser is the simplest option for a first run; ingestr will then create and manage its own publication automatically. For a least-privilege setup, see [Using a dedicated replication user](#using-a-dedicated-replication-user) below.

We'll replicate a small `customers` table. Create one to follow along (or point the commands at a table of your own — it just needs a primary key):

```sql
CREATE TABLE public.customers (
    id    INTEGER PRIMARY KEY,
    name  TEXT NOT NULL,
    email TEXT
);
INSERT INTO public.customers (id, name, email) VALUES
    (1, 'Alice', 'alice@example.com'),
    (2, 'Bob',   'bob@example.com'),
    (3, 'Carol', 'carol@example.com');
```

## 2. Run the initial load

Run ingestr once to take the initial snapshot into DuckDB. The `postgres+cdc://` scheme selects the CDC path:

```bash
ingestr ingest \
  --source-uri "postgres+cdc://user:password@localhost:5432/shop?sslmode=disable" \
  --source-table "public.customers" \
  --dest-uri "duckdb:///shop.duckdb" \
  --dest-table "public.customers"
```

By default the run snapshots the table, catches up to the current WAL position, and exits. Inspect the result:

```bash
duckdb shop.duckdb "SELECT id, name, email, _cdc_deleted FROM public.customers ORDER BY id;"
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

### How the snapshot and LSN fit together

With `postgres+cdc://`, ingestr handles the initial load and its WAL boundary together: it creates a replication slot, reads a consistent snapshot, and applies changes from the matching LSN. Source writes can continue during the copy. You do not need to look up an LSN or pass `--cdc-start-lsn` for this workflow.

Once the run succeeds, keep both the replication slot on PostgreSQL and ingestr's managed CDC state in the destination. Repeating the same command resumes from those saved checkpoints. The latest `_cdc_lsn` in a data row is not the connector's recovery checkpoint, and `--cdc-start-lsn` cannot rewind an existing connector.

For large tables, [chunked snapshots](/supported-sources/postgres.md#large-initial-snapshots) let ingestr save progress during the initial load. The default snapshot reader may need to restart an interrupted initial copy.

To perform the initial load separately and supply its LSN yourself, follow [Load a PostgreSQL snapshot with ingestr and start CDC](/tutorials/cdc-postgres-snapshot.md). It shows how to obtain a matching snapshot and LSN and load that snapshot using ingestr while source writes continue.

## 3. Capture ongoing changes

To keep the destination in sync continuously, run the same command with `--stream`. The process snapshots first (if needed) and then tails the WAL, flushing changes on the `--flush-interval`, until you stop it with `Ctrl+C`:

```bash
ingestr ingest \
  --source-uri "postgres+cdc://user:password@localhost:5432/shop?sslmode=disable" \
  --source-table "public.customers" \
  --dest-uri "duckdb:///shop.duckdb" \
  --dest-table "public.customers" \
  --stream --flush-interval 2s
```

Leave it running and, from another session, change the source:

```sql
INSERT INTO public.customers (id, name, email) VALUES (4, 'Dave', 'dave@example.com');
UPDATE public.customers SET email = 'alice@newmail.com' WHERE id = 1;
DELETE FROM public.customers WHERE id = 2;
```

Within a few seconds the stream reports a flush cycle. Stop it with `Ctrl+C` (`Streaming ingestion stopped.`) and re-inspect the destination:

```bash
duckdb shop.duckdb "SELECT id, name, email, _cdc_deleted FROM public.customers ORDER BY id;"
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

The insert and update are applied, and the delete is a **soft delete**: `id = 2` stays in DuckDB with `_cdc_deleted = true` and its last known values intact. Filter it out at query time with `WHERE _cdc_deleted = false`.

> Prefer scheduled batch syncs over a long-running process? Run the command **without** `--stream` on a cron schedule — each run catches up to the current WAL position and exits.

## 4. Start over if needed

To discard the destination state and rebuild from a fresh snapshot, add `--full-refresh`:

```bash
ingestr ingest \
  --source-uri "postgres+cdc://user:password@localhost:5432/shop?sslmode=disable" \
  --source-table "public.customers" \
  --dest-uri "duckdb:///shop.duckdb" \
  --dest-table "public.customers" \
  --full-refresh
```

## Using a dedicated replication user

Instead of an admin user, you can run CDC as a least-privilege role. It needs `REPLICATION` and `SELECT` on the tables, plus a publication created ahead of time (a non-owner cannot create one). As an admin:

```sql
CREATE ROLE ingestr_cdc WITH LOGIN PASSWORD 'cdcpass' REPLICATION;
GRANT USAGE ON SCHEMA public TO ingestr_cdc;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO ingestr_cdc;
CREATE PUBLICATION ingestr_pub FOR TABLE public.customers;
```

Then point ingestr at that publication with the `publication` URI parameter:

```bash
ingestr ingest \
  --source-uri "postgres+cdc://ingestr_cdc:cdcpass@localhost:5432/shop?sslmode=disable&publication=ingestr_pub" \
  --source-table "public.customers" \
  --dest-uri "duckdb:///shop.duckdb" \
  --dest-table "public.customers"
```

To add more tables later, extend the publication yourself with `ALTER PUBLICATION ingestr_pub ADD TABLE ...`.

## See also

- [Change Data Capture](/getting-started/cdc.md) — how CDC works across ingestr, and the other supported platforms.
- [Postgres source reference](/supported-sources/postgres.md) — all URI parameters and CDC internals.
- [`ingest` command](/commands/ingest.md#streaming-ingestion) — streaming flags and monitoring.
