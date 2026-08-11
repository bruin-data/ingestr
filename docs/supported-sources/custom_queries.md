# Custom Queries for SQL Sources

ingestr has primarily supported table replication for SQL sources due to that being a common use case. However, there are certain scenarios where loading a table only is not possible:
- you might want to load a subset of rows from a table
- you might want to load a table that has a complex query that cannot be expressed as a simple table
  - you could technically create a view in the database, but sometimes you don't have access/permissions to do so.
- you might want to do incremental loads but the table you want to load does not have an incremental key, so it needs to be joined with another table that does.

In order to support these scenarios, ingestr has added experimental support for custom queries.

> [!DANGER]
> This is an experimental feature, so do not expect it to work for all use cases. Please create an issue if you find a use case that doesn't work.

## How to use custom queries

To use a custom query, you can pass a `query:` prefix to the source name:

```bash
ingestr ingest \
    --source-uri $POSTGRES_URI \
    --dest-uri "duckdb:///mydb.db" \
    --dest-table "public.output" \
    --source-table "query:select oi.*, o.updated_at from order_items oi join orders o on oi.order_id = o.id" 
 ```

Ingestr uses SQLAlchemy to run the queries, therefore you can use any valid SQLAlchemy query.

### Incremental loads

Custom queries support incremental loads, but there are some caveats:
- the incremental key must be a column that is returned by the query
- the incremental key must be a datetime/timestamp column
- you must do your own filtering in the query for the incremental load
  - you can use the `interval_start` and `interval_end` variables to filter the data

Here's an example of how to do an incremental load:

```bash
ingestr ingest \
    --source-uri $POSTGRES_URI \
    --dest-uri "duckdb:///mydb.db" \
    --dest-table "public.output" \
    --source-table "query:select oi.*, o.updated_at from order_items oi join orders o on oi.order_id = o.id where o.updated_at > :interval_start" \
    --incremental-key updated_at \
    --incremental-strategy merge \
    --primary-key id
```

In this example, the query is filtering the data to only include rows where the `updated_at` column is greater than the `interval_start` variable.

### Parallel extract partitioning

Custom queries on PostgreSQL, MySQL, Microsoft SQL Server, SQLite, DuckDB, Snowflake, and BigQuery can split the query result into parallel extract windows. The partition column must be returned by the query and must have a unique output name.

```bash
ingestr ingest \
    --source-uri "$POSTGRES_URI" \
    --source-table "query:select oi.*, o.created_at as partition_ts, o.updated_at from order_items oi join orders o on oi.order_id = o.id where o.updated_at >= :interval_start and o.updated_at <= :interval_end" \
    --dest-uri "$DEST_URI" \
    --dest-table public.order_items \
    --incremental-key updated_at \
    --incremental-strategy merge \
    --primary-key id \
    --interval-start 2026-01-01 \
    --interval-end 2026-02-01 \
    --extract-partition-by partition_ts \
    --extract-partition-interval 1d \
    --extract-parallelism 4
```

ingestr applies each partition predicate to the result of the custom query, equivalent to:

```sql
SELECT *
FROM (<custom query>) AS __ingestr_query
WHERE __ingestr_query.partition_ts >= <window start>
  AND __ingestr_query.partition_ts < <window end>
```

The final window includes its end boundary. Numeric partition columns and `--extract-partition-interval auto` use a bounds query over the same custom-query result.

Because the query is used as a derived table, it must be a single query that is valid inside a `FROM (...)` clause. Some dialect-specific constructs, such as an unbounded `ORDER BY` in Microsoft SQL Server, cannot be used in a derived table.
