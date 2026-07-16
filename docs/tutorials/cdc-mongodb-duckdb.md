# Replicate MongoDB to DuckDB with CDC

This walkthrough takes you from a running MongoDB deployment to a DuckDB file kept in sync through [Change Data Capture](/getting-started/cdc.md): an initial snapshot of a collection, then inserts, updates, and deletes picked up by re-running ingestr. It assumes you already have a MongoDB deployment you can connect to and [ingestr installed](/getting-started/quickstart.md#installation).

## 1. Prepare the source

MongoDB CDC reads [change streams](https://www.mongodb.com/docs/manual/changeStreams/), which are only available on a **replica set** (or a sharded cluster) — they don't work against a standalone `mongod`. MongoDB Atlas clusters are replica sets already. For a self-managed server, start it with `--replSet` and initialize the set once:

```javascript
// in mongosh, connected to the server
rs.initiate();
```

MongoDB is schema-less, so ingestr infers the destination schema from the documents it reads. We'll replicate a `customers` collection in a `shop` database. Insert a few documents to follow along:

```javascript
use shop;
db.customers.insertMany([
  { _id: 1, name: "Alice", email: "alice@example.com" },
  { _id: 2, name: "Bob",   email: "bob@example.com" },
  { _id: 3, name: "Carol", email: "carol@example.com" }
]);
```

## 2. Run the initial load

Run ingestr with the `mongodb+cdc://` scheme. The source table is addressed as `database.collection`:

```bash
ingestr ingest \
  --source-uri "mongodb+cdc://user:password@localhost:27017/" \
  --source-table "shop.customers" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "shop.customers"
```

> Use `mongodb+srv+cdc://` instead for an Atlas / SRV connection string. If you connect to a single-node replica set through a mapped port (common in local Docker setups), add `?directConnection=true` so the driver talks to that node directly instead of trying to reach the set's advertised member address.

The run snapshots the collection, records a change-stream resume position, and exits. Inspect the result:

```bash
duckdb warehouse.duckdb "SELECT _id, name, email, _cdc_deleted FROM shop.customers ORDER BY _id;"
```

```plaintext
┌───────┬─────────┬───────────────────┬──────────────┐
│  _id  │  name   │       email       │ _cdc_deleted │
├───────┼─────────┼───────────────────┼──────────────┤
│     1 │ Alice   │ alice@example.com │ false        │
│     2 │ Bob     │ bob@example.com   │ false        │
│     3 │ Carol   │ carol@example.com │ false        │
└───────┴─────────┴───────────────────┴──────────────┘
```

The document's `_id` becomes the primary key. Alongside your fields, ingestr adds the CDC metadata columns `_cdc_lsn` (a change-stream resume token), `_cdc_deleted`, and `_cdc_synced_at`.

## 3. Capture ongoing changes

Change the source:

```javascript
db.customers.insertOne({ _id: 4, name: "Dave", email: "dave@example.com" });
db.customers.updateOne({ _id: 1 }, { $set: { email: "alice@newmail.com" } });
db.customers.deleteOne({ _id: 2 });
```

Now run **exactly the same command again**. Instead of re-snapshotting, ingestr resumes the change stream from the token stored in the destination and applies only what changed:

```bash
ingestr ingest \
  --source-uri "mongodb+cdc://user:password@localhost:27017/" \
  --source-table "shop.customers" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "shop.customers"
```

```bash
duckdb warehouse.duckdb "SELECT _id, name, email, _cdc_deleted FROM shop.customers ORDER BY _id;"
```

```plaintext
┌───────┬─────────┬───────────────────┬──────────────┐
│  _id  │  name   │       email       │ _cdc_deleted │
├───────┼─────────┼───────────────────┼──────────────┤
│     1 │ Alice   │ alice@newmail.com │ false        │
│     2 │ Bob     │ bob@example.com   │ true         │
│     3 │ Carol   │ carol@example.com │ false        │
│     4 │ Dave    │ dave@example.com  │ false        │
└───────┴─────────┴───────────────────┴──────────────┘
```

The insert and update are applied, and the delete is a **soft delete**: `_id = 2` stays in DuckDB with `_cdc_deleted = true`. MongoDB delete events carry only the `_id`, so ingestr marks the row deleted while preserving the values it already has. Filter deleted rows out at query time with `WHERE _cdc_deleted = false`. Schedule this same command (for example with cron) to keep the destination up to date, or add `--stream` to run continuously instead of once per invocation.

## 4. Start over if needed

To discard the destination state and rebuild from a fresh snapshot, add `--full-refresh`:

```bash
ingestr ingest \
  --source-uri "mongodb+cdc://user:password@localhost:27017/" \
  --source-table "shop.customers" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "shop.customers" \
  --full-refresh
```

## See also

- [Change Data Capture](/getting-started/cdc.md) — how CDC works across ingestr, and the other supported platforms.
- [MongoDB source reference](/supported-sources/mongodb.md) — connection formats, source-table syntax, and aggregations.
