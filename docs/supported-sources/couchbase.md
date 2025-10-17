# Couchbase

Couchbase is a modern, distributed NoSQL database known for its low-latency performance, scalability, and flexible data model. It combines the best of key-value stores and document databases with powerful querying capabilities through N1QL (SQL for JSON).

ingestr supports Couchbase as a source.

## URI format

Couchbase supports two connection string formats:

### Standard format (non-TLS)
```plaintext
couchbase://username:password@host:port/bucket?scope=scope_name&collection=collection_name
```

URI parameters:
- `username`: the username to connect to the cluster
- `password`: the password for the user
- `host`: the host address of the Couchbase cluster
- `port`: the port number for the data service, default is 11210
- `bucket`: the bucket name (required)
- `scope`: the scope name (optional, defaults to `_default`)
- `collection`: the collection name (optional, can be specified in `--source-table` instead)

### Secure format (TLS)
```plaintext
couchbases://username:password@host:port/bucket?scope=scope_name&collection=collection_name
```

This uses TLS encryption for the connection. The port for secure connections defaults to 11207.

### Couchbase Capella (Cloud)
```plaintext
couchbases://username:password@cluster.cloud.couchbase.com/bucket?scope=scope_name&collection=collection_name
```

For Couchbase Capella (the managed cloud service), always use `couchbases://` for secure connections.

> [!CAUTION]
> The bucket name must be specified either in the URI path or in the `--source-table` option. Scope and collection can be specified in either location.

You can read more about Couchbase's connection strings [here](https://docs.couchbase.com/python-sdk/current/howtos/managing-connections.html).

## Source table format

The `--source-table` option for Couchbase supports multiple formats:

### Simple collection name
```plaintext
collection_name
```

This performs a simple N1QL scan: `SELECT * FROM bucket.scope.collection`.

Uses the bucket and scope from the URI. If scope is not specified in the URI, uses `_default` scope.

### Scope and collection
```plaintext
scope.collection
```

Specifies both scope and collection names. Bucket is taken from the URI.

### Full path
```plaintext
bucket.scope.collection
```

Specifies the complete path including bucket, scope, and collection. Overrides the bucket from the URI.

### Custom N1QL query format
```plaintext
collection_name:n1ql_query
```

This allows you to specify a custom N1QL query for advanced data selection and transformation.

## Custom N1QL queries

ingestr supports custom N1QL queries, similar to how SQL sources support custom queries. This allows you to perform complex data transformations, filtering, joins, and aggregations directly in Couchbase before the data is ingested.

### Basic syntax

Use the following format for custom N1QL queries:

```bash
ingestr ingest \
  --source-uri "couchbase://user:password@host:11210/bucket" \
  --source-table 'collection_name:SELECT * FROM `bucket`.`scope`.`collection` WHERE ...'
```

### Examples

#### Simple filtering
```bash
ingestr ingest \
  --source-uri "couchbase://Administrator:password@localhost:11210/travel-sample" \
  --source-table '_default:SELECT META().id as _id, * FROM `travel-sample`.`inventory`.`airline` WHERE country = "United States"' \
  --dest-uri "duckdb:///output.db" \
  --dest-table "us_airlines"
```

#### Field projection (column selection)
```bash
ingestr ingest \
  --source-uri "couchbase://user:pass@localhost:11210/ecommerce" \
  --source-table 'orders:SELECT META().id as _id, order_id, customer_id, total_amount, order_date FROM `ecommerce`.`data`.`orders`' \
  --dest-uri "postgres://user:pass@localhost/warehouse" \
  --dest-table "public.orders"
```

#### JOIN operations
```bash
ingestr ingest \
  --source-uri "couchbase://user:pass@localhost:11210/travel-sample" \
  --source-table 'airline:SELECT
    META(a).id as _id,
    a.name as airline_name,
    a.iata,
    c.name as country_name
  FROM `travel-sample`.`inventory`.`airline` a
  LEFT JOIN `travel-sample`.`inventory`.`country` c
    ON a.country = c.code' \
  --dest-uri "duckdb:///output.db" \
  --dest-table "airlines_with_countries"
```

#### Aggregation
```bash
ingestr ingest \
  --source-uri "couchbase://user:pass@localhost:11210/analytics" \
  --source-table 'events:SELECT
    user_id,
    COUNT(*) as event_count,
    MAX(created_at) as last_event_at
  FROM `analytics`.`tracking`.`user_events`
  WHERE event_type = "purchase"
  GROUP BY user_id' \
  --dest-uri "postgres://user:pass@localhost/warehouse" \
  --dest-table "public.user_purchase_summary"
```

#### Complex filtering with subqueries
```bash
ingestr ingest \
  --source-uri "couchbase://user:pass@localhost:11210/ecommerce" \
  --source-table 'orders:SELECT META().id as _id, *
  FROM `ecommerce`.`orders`.`transactions`
  WHERE customer_id IN (
    SELECT RAW user_id
    FROM `ecommerce`.`users`.`vip`
    WHERE tier = "platinum"
  )' \
  --dest-uri "duckdb:///output.db" \
  --dest-table "platinum_customer_orders"
```

### Incremental loads with custom queries

Custom N1QL queries support incremental loading when combined with the `--incremental-key` option. You should include the incremental key field in your SELECT clause.

```bash
ingestr ingest \
  --source-uri "couchbase://user:pass@localhost:11210/analytics" \
  --source-table '_default:SELECT
    META().id as _id,
    event_id,
    user_id,
    event_type,
    created_at
  FROM `analytics`.`tracking`.`events`
  WHERE created_at >= "2024-01-01T00:00:00Z"' \
  --dest-uri "postgres://user:pass@localhost/warehouse" \
  --dest-table "public.events" \
  --incremental-key "created_at" \
  --interval-start "2024-01-01T00:00:00Z" \
  --interval-end "2024-12-31T23:59:59Z"
```

#### Requirements for incremental loads

When using incremental loads with custom N1QL queries:

1. **Incremental key projection**: The field specified in `--incremental-key` must be included in your SELECT clause
2. **Datetime type**: The incremental key should be a datetime/timestamp field
3. **Proper indexing**: Ensure you have an index on the incremental key field for optimal performance

### Best practices

#### Use META().id for document keys
Always include `META().id as _id` in your SELECT to preserve the document key:

```sql
SELECT META().id as _id, * FROM `bucket`.`scope`.`collection`
```

#### Create appropriate indexes
Before running queries, create indexes on fields you'll filter or join on:

```sql
CREATE INDEX idx_created_at ON `bucket`.`scope`.`collection`(created_at);
CREATE INDEX idx_status ON `bucket`.`scope`.`collection`(status);
```

You can create indexes via the Couchbase Web Console or using the cluster.query() method.

#### Use backticks for identifiers
Always use backticks (\`) around bucket, scope, and collection names:

```sql
SELECT * FROM `bucket`.`scope`.`collection`
```

#### Filter early
Use WHERE clauses to filter data as early as possible:

```sql
-- Good
SELECT * FROM `bucket`.`scope`.`collection` WHERE status = 'active'

-- Less efficient
SELECT * FROM `bucket`.`scope`.`collection`
```

### Validation and error handling

ingestr performs several validations on N1QL queries:

- **Syntax validation**: Basic N1QL syntax checking
- **Connection validation**: Verifies cluster connectivity
- **Query execution**: Tests query execution before full data load
- **Clear error messages**: Provides specific error messages for common issues

### Limitations

- **KV operations**: Custom queries use N1QL, not Key-Value operations. For direct key access, use the KV mode (see below)
- **Query timeout**: Long-running queries may timeout. Default timeout is 30 seconds
- **Memory usage**: Large result sets are processed in chunks (default: 10,000 documents per chunk)

### Performance considerations

- **Index usage**: Always create indexes on fields used in WHERE, JOIN, and ORDER BY clauses
- **Chunk size**: Adjust chunk_size for memory optimization (default: 10,000)
- **Limit rows**: Use LIMIT in queries for testing or when you need only a subset
- **Query explain**: Use `EXPLAIN` in the Couchbase console to optimize query plans
- **Avoid SELECT \***: Project only the fields you need to reduce network transfer

## Key-Value mode

For high-performance document retrieval by key, Couchbase supports a Key-Value (KV) mode that bypasses N1QL:

```python
# Using programmatically (not available via CLI yet)
from ingestr.src.couchbase import couchbase_collection

resource = couchbase_collection(
    connection_string="couchbase://localhost",
    username="Administrator",
    password="password",
    bucket="mybucket",
    scope="myscope",
    collection="mycollection",
    kv_mode=True,
    keys=["doc_1", "doc_2", "doc_3"]
)
```

KV mode is ideal for:
- Fetching specific documents by known keys
- High-throughput, low-latency operations
- Bulk document retrieval with known key lists

## Common use cases

### ETL from operational database to data warehouse
```bash
ingestr ingest \
  --source-uri "couchbase://user:pass@prod-cluster:11210/ecommerce" \
  --source-table "orders.transactions" \
  --dest-uri "snowflake://account.region/database/schema?warehouse=WH&role=LOADER" \
  --dest-table "raw.ecommerce_orders" \
  --incremental-key "order_date" \
  --interval-start "2024-01-01"
```

### Migrate data between Couchbase clusters
```bash
ingestr ingest \
  --source-uri "couchbase://admin:pass@old-cluster:11210/myapp" \
  --source-table "data.users" \
  --dest-uri "couchbase://admin:pass@new-cluster:11210/myapp" \
  --dest-table "data.users"
```

### Extract and transform for analytics
```bash
ingestr ingest \
  --source-uri "couchbase://user:pass@cluster:11210/analytics" \
  --source-table 'events:SELECT
    DATE_TRUNC_STR(created_at, "day") as event_date,
    event_type,
    COUNT(*) as count
  FROM `analytics`.`tracking`.`events`
  GROUP BY DATE_TRUNC_STR(created_at, "day"), event_type' \
  --dest-uri "postgres://user:pass@analytics-db/warehouse" \
  --dest-table "public.daily_events"
```

### Replicate to analytical database
```bash
ingestr ingest \
  --source-uri "couchbase://user:pass@prod:11210/myapp" \
  --source-table "inventory.products" \
  --dest-uri "clickhouse://user:pass@analytics-cluster:9000/mydb" \
  --dest-table "products"
```

## Troubleshooting

### Connection Issues

**Problem**: `Connection refused` or timeout errors

**Solutions**:
1. Verify the cluster is accessible: `ping <host>`
2. Check ports are open (11210 for couchbase://, 11207 for couchbases://)
3. Ensure credentials are correct
4. For Couchbase Capella, verify IP allowlist settings

### Index Missing

**Problem**: Query is slow or times out

**Solution**: Create indexes on filtered/sorted fields:
```sql
CREATE PRIMARY INDEX ON `bucket`.`scope`.`collection`;
-- Or better, create specific indexes:
CREATE INDEX idx_field ON `bucket`.`scope`.`collection`(field_name);
```

### Query Timeout

**Problem**: `Query exceeded timeout`

**Solutions**:
1. Create appropriate indexes
2. Add LIMIT clause for testing
3. Break large queries into smaller chunks
4. Increase timeout in query options (programmatic use)

### Memory Issues

**Problem**: Out of memory during large data loads

**Solution**: Reduce chunk_size in programmatic usage:
```python
couchbase_collection(
    ...,
    chunk_size=1000  # Default is 10000
)
```

### Authentication Failed

**Problem**: `Authentication failed`

**Solutions**:
1. Verify username and password are correct
2. Check user has proper permissions (at least Data Reader role)
3. For Capella, ensure database credentials (not organization credentials) are used

## Security best practices

1. **Use TLS**: Always use `couchbases://` for production environments
2. **Least privilege**: Create read-only users for data extraction
3. **Credential management**: Store credentials in environment variables or secrets manager
4. **IP allowlisting**: Use IP allowlists in Couchbase Capella
5. **Certificate validation**: Validate server certificates in production

## Performance tips

1. **Create indexes**: Index all fields used in WHERE, JOIN, and ORDER BY clauses
2. **Use projection**: Select only needed fields instead of `SELECT *`
3. **Chunk processing**: Data is processed in chunks (default 10,000 docs)
4. **Parallel loading**: Currently not supported for N1QL queries, but available for KV operations
5. **Query optimization**: Use EXPLAIN to understand query execution plans
6. **Limit results**: Use LIMIT for testing queries before full loads

## Supported Couchbase versions

- Couchbase Server 6.x
- Couchbase Server 7.x
- Couchbase Capella (cloud)

## Additional resources

- [Couchbase N1QL Reference](https://docs.couchbase.com/server/current/n1ql/n1ql-language-reference/index.html)
- [Couchbase Python SDK](https://docs.couchbase.com/python-sdk/current/hello-world/start-using-sdk.html)
- [Couchbase Indexing Best Practices](https://docs.couchbase.com/server/current/learn/services-and-indexes/indexes/indexing-and-query-perf.html)
- [Couchbase Data Modeling](https://docs.couchbase.com/server/current/learn/data/document-data-model.html)
