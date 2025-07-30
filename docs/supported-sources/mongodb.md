# MongoDB
MongoDB is a popular, open source NoSQL database known for its flexibility, scalability, and wide adoption in a variety of applications.

ingestr supports MongoDB as a source.

## URI format
The URI format for MongoDB is as follows:

```plaintext
mongodb://user:password@host:port
```

URI parameters:
- `user`: the user name to connect to the database
- `password`: the password for the user
- `host`: the host address of the database server
- `port`: the port number the database server is listening on, default is 27017 for MongoDB


> [!CAUTION]
> Do not put the database name at the end of the URI for MongoDB, instead make it a part of `--source-table` option as `database.collection` format.


You can read more about MongoDB's connection string format [here](https://docs.mongodb.com/manual/reference/connection-string/).

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
