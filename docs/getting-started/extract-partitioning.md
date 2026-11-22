---
outline: deep
---

# Parallel Extract Partitioning

By default, ingestr reads a source table with a single query over a single connection. For large tables, that one query is often the bottleneck.

Extract partitioning splits the read into smaller queries, called *windows*, and runs several of them concurrently. Each window covers a slice of one column's range, for example one day of `created_at` or 100,000 consecutive `id`s. All windows feed the same load, so the destination sees one ordinary ingestion.

```bash
ingestr ingest \
    --source-uri "$POSTGRES_URI" \
    --source-table public.events \
    --dest-uri "$DEST_URI" \
    --dest-table analytics.events \
    --incremental-key created_at \
    --incremental-strategy merge \
    --primary-key id \
    --interval-start 2026-01-01 \
    --interval-end 2026-02-01 \
    --extract-partition-by created_at \
    --extract-partition-interval 1d \
    --extract-parallelism 4
```

This reads January 2026 as 31 one-day windows, four at a time.

> [!NOTE]
> Extract partitioning only affects how data is *read* from the source. It is unrelated to [`--partition-by`](/commands/ingest#partitioning), which controls how the *destination* table is partitioned.

## Supported sources

Extract partitioning is supported for tables and [custom queries](/supported-sources/custom_queries) on [PostgreSQL](/supported-sources/postgres), [MySQL](/supported-sources/mysql), [Microsoft SQL Server](/supported-sources/mssql), [SQLite](/supported-sources/sqlite), [DuckDB](/supported-sources/duckdb), [Snowflake](/supported-sources/snowflake), and [BigQuery](/supported-sources/bigquery).

[MongoDB](/supported-sources/mongodb) collections are also supported, with restrictions: the partition field must hold integer values and be the leading field of an index, and aggregation pipelines cannot be partitioned.

Any other source rejects `--extract-partition-by` with an error.

## Options

| Flag | Environment variable | Description |
| --- | --- | --- |
| `--extract-partition-by` | `INGESTR_EXTRACT_PARTITION_BY` | The source column to split on. Must be a `date`, `timestamp`, `timestamptz`, or integer column. Matched case-insensitively. |
| `--extract-partition-interval` | `INGESTR_EXTRACT_PARTITION_INTERVAL` | The width of each window. Defaults to `auto` when `--extract-partition-by` is set. |
| `--extract-parallelism` | `INGESTR_EXTRACT_PARALLELISM` | How many windows are read at once. Defaults to `5`. |
| `--destination-parallelism` | `DESTINATION_PARALLELISM` or `INGESTR_DESTINATION_PARALLELISM` | Requests destination write concurrency independently of reads. Must be a positive integer; support and limits depend on the destination and load method. |

`--extract-partition-by` decides **which column** windows are filtered on; `--extract-partition-interval` decides **how wide** each window is, and therefore how many there are. Over January, `--extract-partition-by created_at --extract-partition-interval 1d` produces 31 windows, while `7d` produces five. `--extract-parallelism` then sets how many of those windows run at the same time.

The column is required; the interval is optional. Setting an interval without a column is an error.

### Interval values

- `auto`: picks a width that produces about four windows per worker (`4 × --extract-parallelism`). Works for all column types. On `date` columns the width is never less than one day.
- A duration such as `30m`, `1h`, `7d`, or `2w`, for date and timestamp columns. `d` (24 hours) and `w` (7 days) are accepted in addition to Go durations.
- A positive integer step such as `10000`, for integer columns.

A `date` column needs a duration of at least `24h`. Using a duration on an integer column, or an integer step on a date or timestamp column, is an error. A single run cannot produce more than 100,000 windows.

`auto` is a good starting point. Having several windows per worker means one slow window does not leave the others idle, but it assumes values are spread evenly across the range (see [Skewed data](#skewed-data)).

## How it works

1. **Resolve the range.** For date and timestamp columns, the range is `--interval-start` to `--interval-end`, so both are required. If the partition column is not the incremental key, ingestr also runs a bounds query (`MIN`, `MAX`, and counts over the rows in the interval) to narrow the range to where data exists. Integer columns always use a bounds query.
2. **Cut it into windows.** Windows are half-open (`column >= start AND column < end`), except the last, which includes its end boundary. When a bounds query runs and finds `NULL` values in the partition column, one extra `column IS NULL` window reads them.
3. **Read windows in parallel.** Up to `--extract-parallelism` workers each take the next window and run it as its own query. Batches from all workers are merged into a single stream for the load. Buffers are small, so a slow destination slows down reading instead of accumulating data in memory.
4. **Fail together.** If any window fails, the remaining windows are cancelled and the run fails.

For a table, each window is an ordinary query with an extra filter:

```sql
SELECT *
FROM public.events
WHERE created_at >= '2026-01-01' AND created_at <= '2026-02-01'  -- incremental interval
  AND created_at >= '2026-01-05' AND created_at <  '2026-01-06'  -- this window
```

With `auto` instead of `1d`, the same 31-day range and `--extract-parallelism 5` target 20 windows, each about 37 hours wide.

### Integer columns

Partitioning by an integer column such as the primary key does not need `--interval-start` or `--interval-end` unless `--incremental-key` is set. ingestr discovers the minimum and maximum and slices the range between them, which makes this the simplest way to read an entire large table in parallel:

```bash
ingestr ingest \
    --source-uri "$MYSQL_URI" \
    --source-table shop.order_items \
    --dest-uri "$DEST_URI" \
    --dest-table raw.order_items \
    --extract-partition-by id \
    --extract-partition-interval 100000 \
    --extract-parallelism 8
```

For `id` values from 1 to 1,000,000, this produces ten windows of 100,000 ids. With `auto`, it produces 32 windows (`4 × 8`).

## Custom queries

[Custom queries](/supported-sources/custom_queries) can be partitioned on the same sources. ingestr wraps the query as a derived table and applies each window to its result:

```sql
SELECT *
FROM (<custom query>) AS __ingestr_query
WHERE __ingestr_query.partition_ts >= <window start>
  AND __ingestr_query.partition_ts <  <window end>
```

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

Compared to tables:

- The partition column must be one of the query's output columns. Give it a distinct alias, as with `partition_ts` above.
- The query must be valid inside `FROM (...)`. Constructs that are not allowed in a derived table, such as an unbounded `ORDER BY` in SQL Server, fail.
- You still filter by the incremental interval yourself with `:interval_start` and `:interval_end`, as in any [incremental custom query](/supported-sources/custom_queries#incremental-loads). The partition column only splits the result.

## Strategies and limitations

Extract partitioning changes how rows are read, not what is written, so it works with every [incremental strategy](/getting-started/incremental-loading):

- **`replace`** loads every window before the destination replacement is finalized. On destinations that replace through staging or a transaction, a failed window or load leaves existing data unchanged. The interval is the complete replacement data set, so destination rows outside `--interval-start` and `--interval-end` are not kept. Destinations that would replace in place without being able to stage the complete extract are rejected before reading.
- **`scd2`** requires `--incremental-key` when extract partitioning is used.

Use `--extract-parallelism 2 --destination-parallelism 8` to read two windows at a time while requesting eight destination workers. When `--destination-parallelism` is omitted, destination write parallelism follows an explicitly set `--extract-parallelism`; if neither is set, destination-specific defaults apply. Actual write concurrency depends on the destination and load method.

ingestr rejects these combinations before reading anything:

- `--sql-limit`, `--stream`, or `--full-refresh`
- CDC and change-tracking sources
- `--incremental-key` without both `--interval-start` and `--interval-end`

## Tradeoffs

Parallel reads usually reduce wall-clock time for large extracts, and each window is a shorter query, which helps with statement timeouts. They are not free, and not always faster.

**Load on the source.** Up to `--extract-parallelism` queries run at once, each on its own connection. On a busy production database, keep parallelism modest or read from a replica. The MySQL source uses at most 10 connections, so higher values add no read concurrency there.

**Index the partition column.** Every window filters on the partition column. With an index, each window is a range scan; without one, each window may scan the whole table, multiplying the work of a single query. Partition by an indexed column, typically the primary key or a creation timestamp.

**No consistent snapshot.** Windows run as separate queries at slightly different times, so together they are not a point-in-time snapshot. If rows are inserted, deleted, or have their partition column updated during the run, a row can be read twice or missed. With `append`, a row read twice becomes a duplicate; prefer `merge` with a primary key when the source is changing. Partition by a column that does not change, such as `created_at` or an auto-incrementing `id`, rather than `updated_at`. If you need a consistent copy, use a single-query read or a [CDC snapshot](/getting-started/cdc).

**Bounds query.** When a bounds query runs, it aggregates over the same rows the windows will read, so it is cheapest when the partition column is indexed. For a custom query, it executes the full query once more.

**Custom queries run once per window.** Expensive joins or aggregations are repeated for every window, and depending on the database, each run may compute the full result before the window filter is applied. Use a larger interval to reduce the window count, or materialize the query into a table and partition that.

**Per-query cost.** On warehouses such as Snowflake and BigQuery, every window is a separate query. If the table is not clustered or partitioned on the partition column, each window may scan far more data than it returns.

**Memory.** Each in-flight window buffers record batches, so peak memory grows with `--extract-parallelism`.

### Skewed data

Windows have equal *width*, not equal *row counts*. If most rows fall on a few busy days, those windows dominate the run while other workers sit idle. Use `auto` or a smaller interval to spread heavy ranges over more windows, or partition by a more evenly distributed column such as a sequential `id`. Avoid very small intervals: every window is a separate query with its own overhead.

## Troubleshooting

| Error | Cause |
| --- | --- |
| `extract partition column "x" was not found in source schema` | The column is not in the table or not returned by the custom query. |
| `extract partition column "x" must be date, timestamp_ntz, timestamp, or integer` | Other types, such as strings, decimals, and floats, cannot be partitioned. In a custom query, cast the column to a supported type. |
| `extract partitioning requires interval start and end` | Date and timestamp partition columns need `--interval-start` and `--interval-end`. |
| `extract partition interval must be at least 24h for date partition column "x"` | A `date` column cannot be split into sub-day windows. |
| `extract partitioning would create more than 100000 windows` | The interval is too small for the range. Increase it or use `auto`. |
| `MongoDB extract partition column "x" must be the leading field of an index` | Create an index that starts with the partition field. |
| `source table "x" does not support extract partitioning` | The source is not one of the [supported sources](#supported-sources). |
