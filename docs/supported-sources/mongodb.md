# MongoDB
MongoDB is a popular, open source NoSQL database known for its flexibility, scalability, and wide adoption in a variety of applications.

ingestr supports MongoDB as both a source and destination.

## URI format

MongoDB supports two connection string formats:

### Standard format (local/self-hosted)
```plaintext
mongodb://user:password@host:port
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `host`: the host address of the database server
- `port`: the port number the database server is listening on, default is 27017 for MongoDB

### SRV format (MongoDB Atlas)
```plaintext
mongodb+srv://user:password@cluster.xxxxx.mongodb.net/?retryWrites=true&w=majority
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `cluster.xxxxx.mongodb.net`: the cluster hostname provided by MongoDB Atlas
- Query parameters like `retryWrites` and `w` are optional but recommended for Atlas connections

> [!CAUTION]
> Do not put the database name at the end of the URI for MongoDB, instead make it a part of `--source-table` or `--dest-table` option as `database.collection` format.

The same URI structure can be used both for sources and destinations. You can read more about MongoDB's connection string format [here](https://docs.mongodb.com/manual/reference/connection-string/).

## Source table format

The `--source-table` option for MongoDB supports two formats:

### Basic format
```plaintext
database.collection
```

This performs a simple collection scan, equivalent to `db.collection.find()`.

### Custom aggregation format
```plaintext
database.collection:[aggregation_pipeline]
```

This allows you to specify a custom MongoDB aggregation pipeline as a JSON array.

## Custom aggregations

ingestr supports custom MongoDB aggregation pipelines, similar to how SQL sources support custom queries. This allows you to perform complex data transformations, filtering, and projections directly in MongoDB before the data is ingested.

### Basic syntax

Use the following format for custom aggregations:

```bash
ingestr ingest \
  --source-uri "mongodb://user:password@host:port" \
  --source-table 'database.collection:[{"$match": {...}}, {"$project": {...}}]'
```

### Examples

#### Simple filtering
```bash
ingestr ingest \
  --source-uri "mongodb://localhost:27017" \
  --source-table 'mydb.users:[{"$match": {"status": "active"}}]'
```

#### Complex aggregation with grouping
```bash
ingestr ingest \
  --source-uri "mongodb://localhost:27017" \
  --source-table 'mydb.orders:[
    {"$match": {"status": "completed"}},
    {"$group": {
      "_id": "$customer_id",
      "total_orders": {"$sum": 1},
      "total_amount": {"$sum": "$amount"}
    }}
  ]'
```

#### Projection and transformation
```bash
ingestr ingest \
  --source-uri "mongodb://localhost:27017" \
  --source-table 'mydb.products:[
    {"$project": {
      "name": 1,
      "price": 1,
      "category": 1,
      "price_usd": {"$multiply": ["$price", 1.1]}
    }}
  ]'
```

### Incremental loads with custom aggregations

Custom aggregations support incremental loading when combined with the `--incremental-key` option. The incremental key must be included in the projected fields of your aggregation pipeline.

#### Using interval placeholders

You can use `:interval_start` and `:interval_end` placeholders in your aggregation pipeline, which will be automatically replaced with the actual datetime values during incremental loads:

```bash
ingestr ingest \
  --source-uri "mongodb://localhost:27017" \
  --source-table 'mydb.events:[
    {"$match": {
      "created_at": {
        "$gte": ":interval_start",
        "$lt": ":interval_end"
      }
    }},
    {"$project": {
      "_id": 1,
      "event_type": 1,
      "user_id": 1,
      "created_at": 1
    }}
  ]' \
  --incremental-key "created_at"
```

#### Requirements for incremental loads

When using incremental loads with custom aggregations:

1. **Incremental key projection**: The field specified in `--incremental-key` must be included in your projection
2. **Datetime type**: The incremental key should be a datetime field
3. **Pipeline validation**: ingestr validates that your aggregation pipeline properly projects the incremental key

### Validation and error handling

ingestr performs several validations on custom aggregation pipelines:

- **JSON validation**: Ensures the aggregation pipeline is valid JSON
- **Array format**: Aggregation pipelines must be JSON arrays
- **Incremental key validation**: When using `--incremental-key`, validates that the key is projected in the pipeline
- **Clear error messages**: Provides specific error messages for common issues

### Limitations

- **Parallel loading**: Custom aggregations don't support parallel loading due to MongoDB cursor limitations. The loader automatically falls back to sequential processing.
- **Arrow format**: When using Arrow data format with custom aggregations, data is converted to Arrow format after loading rather than using native MongoDB Arrow integration.

### Performance considerations

- Use `$match` stages early in your pipeline to filter data as soon as possible
- Add appropriate indexes to support your aggregation pipeline
- Consider using `$limit` to restrict the number of documents processed
- For large datasets, MongoDB's `allowDiskUse: true` option is automatically enabled for aggregation pipelines

## Change data capture

ingestr can keep a destination in sync with a MongoDB collection through [change streams](https://www.mongodb.com/docs/manual/changeStreams/) using the `mongodb+cdc://` and `mongodb+srv+cdc://` URI schemes.

```bash
ingestr ingest \
  --source-uri "mongodb+cdc://user:password@localhost:27017/" \
  --source-table "shop.customers" \
  --dest-uri "duckdb:///warehouse.duckdb" \
  --dest-table "shop.customers"
```

This path reads a consistent snapshot of the collection first, then follows the change stream for inserts, updates, and deletes. It produces the `_cdc_lsn` (a change-stream resume token), `_cdc_deleted`, and `_cdc_synced_at` metadata columns and resumes from the stored token on subsequent runs. Incremental runs use the `merge` strategy so changes are applied by `_id`; deletes are soft (`_cdc_deleted = true`, and because MongoDB delete events carry only the `_id`, the row's other values are left untouched). Run with `--full-refresh` to rebuild from a fresh snapshot, or `--stream` to ingest continuously instead of once per invocation.

CDC URI parameters:
- `dest_schema`: optional destination schema for multi-table CDC runs. Ignored when `--source-table` is set; the destination is then `--dest-table`.

Requirements:
- Change streams require a **replica set** (or sharded cluster); they aren't available on a standalone `mongod`. Atlas clusters already satisfy this.
- As a schema-less source, MongoDB CDC infers the destination schema from the documents it reads (see [schema inference](/getting-started/core-concepts.md)); nested documents and arrays land as JSON.

For a full walkthrough — initializing a replica set and replicating a collection into DuckDB — see [Replicate MongoDB to DuckDB with CDC](/tutorials/cdc-mongodb-duckdb.md).

## Using MongoDB Atlas as a source

MongoDB Atlas can be used as a source to extract data using the SRV connection string format.

```bash
ingestr ingest \
  --source-uri "mongodb+srv://username:password@cluster0.xxxxx.mongodb.net/?retryWrites=true&w=majority" \
  --source-table "mydb.users" \
  --dest-uri "duckdb:///local.duckdb" \
  --dest-table "analytics.users"
```

> [!NOTE]
> When using MongoDB Atlas as a source, ensure your IP address is whitelisted in Network Access settings. You can find this under Security > Network Access in your Atlas dashboard.

All the custom aggregation features described above work with MongoDB Atlas as well:

```bash
ingestr ingest \
  --source-uri "mongodb+srv://username:password@cluster0.xxxxx.mongodb.net/?retryWrites=true&w=majority" \
  --source-table 'mydb.orders:[{"$match": {"status": "completed"}}]' \
  --dest-uri "duckdb:///local.duckdb" \
  --dest-table "analytics.completed_orders"
```

## Using MongoDB as a destination

MongoDB can be used as a destination to load data from various sources. The `--dest-table` option follows the same format: `database.collection`.

### MongoDB Atlas

```bash
ingestr ingest \
  --source-uri "postgres://user:pass@localhost:5432/mydb" \
  --source-table "public.users" \
  --dest-uri "mongodb+srv://username:password@cluster0.xxxxx.mongodb.net/?retryWrites=true&w=majority" \
  --dest-table "mydb.users"
```

> [!NOTE]
> When using MongoDB Atlas as a destination, ensure your IP address is whitelisted in Network Access settings.

### Local MongoDB with authentication

```bash
ingestr ingest \
  --source-uri "csv:///path/to/data.csv" \
  --source-table "data" \
  --dest-uri "mongodb://username:password@localhost:27017/?authSource=admin" \
  --dest-table "mydb.mycollection"
```

### Local MongoDB without authentication

```bash
ingestr ingest \
  --source-uri "csv:///path/to/data.csv" \
  --source-table "data" \
  --dest-uri "mongodb://localhost:27017" \
  --dest-table "mydb.mycollection"
```

> [!TIP]
> By default, ingestr uses a "replace" strategy which deletes existing data in the collection before loading new data. The target database and collection will be created automatically if they don't exist.
