---
outline: deep
---

# Parallel Extract Partitioning

By default, ingestr reads a source table with a single query. For a large table that one query becomes the bottleneck: the source streams rows over one connection while the rest of the pipeline waits.

**Extract partitioning** splits that read into many smaller queries, called *windows*, and runs several of them at the same time. Each window covers a slice of one column's value range, for example one day of `created_at` or 100,000 consecutive `id`s. The results of all windows flow into the same load, so the destination sees one ordinary ingestion, just produced faster.

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
> Extract partitioning is about *reading* faster from the source. It is unrelated to [`--partition-by`](/commands/ingest#partitioning), which controls how the *destination* table is laid out.

## Supported sources

Extract partitioning works for SQL database sources, both for whole tables and for [custom queries](/supported-sources/custom_queries):

| Source | Tables | Custom queries |
| --- | --- | --- |
| [PostgreSQL](/supported-sources/postgres) | Yes | Yes |
| [MySQL](/supported-sources/mysql) | Yes | Yes |
| [Microsoft SQL Server](/supported-sources/mssql) | Yes | Yes |
| [SQLite](/supported-sources/sqlite) | Yes | Yes |
| [DuckDB](/supported-sources/duckdb) | Yes | Yes |
| [Snowflake](/supported-sources/snowflake) | Yes | Yes |
| [BigQuery](/supported-sources/bigquery) | Yes | Yes |

[MongoDB](/supported-sources/mongodb) collections also support partitioning, with integer columns only. Any other source rejects `--extract-partition-by` with an error rather than silently ignoring it.

## `--extract-partition-by` vs `--extract-partition-interval`

The two flags answer different questions:

- `--extract-partition-by` is **which column** to split on, for example `created_at` or `id`. It decides what a window is filtered on.
- `--extract-partition-interval` is **how wide** each window is, for example `1d` or `100000`. It decides how many windows there are.

Given a column and a range, the interval cuts the range into pieces. `--extract-partition-by created_at --extract-partition-interval 1d` over January produces 31 windows of one day each; `--extract-partition-interval 7d` produces five. `--extract-parallelism` then controls how many of those windows run at the same time.

You always need the column. The interval is optional: if you set only `--extract-partition-by`, the interval defaults to `auto`. Setting an interval without a column is an error.

## Options

| Flag | Environment variable | Description |
| --- | --- | --- |
| `--extract-partition-by` | `INGESTR_EXTRACT_PARTITION_BY` | The source column to split on. Must be a `date`, `timestamp`, `timestamptz`, or integer column. Matched against the source schema case-insensitively. |
| `--extract-partition-interval` | `INGESTR_EXTRACT_PARTITION_INTERVAL` | The width of each window. Defaults to `auto` when `--extract-partition-by` is set. |
| `--extract-parallelism` | `INGESTR_EXTRACT_PARALLELISM` | How many windows are read at once. Defaults to `5`. |

### Choosing the interval

| Value | Meaning | Applies to |
| --- | --- | --- |
| `auto` | Pick a width that produces about four windows per worker (`4 × --extract-parallelism`). | Date, timestamp, and integer columns |
| `1h`, `30m`, `7d`, `2w` | A duration. `d` (24 hours) and `w` (7 days) are accepted in addition to Go durations such as `90m`. | Date and timestamp columns |
| `10000` | An integer step. | Integer columns |

A few rules apply:

- A `date` column needs an interval of at least `24h`.
- A duration on an integer column, or an integer step on a date/timestamp column, is rejected.
- A single run cannot produce more than 100,000 windows. Increase the interval or use `auto` if you hit that limit.

`auto` is a good starting point. It aims for several windows per worker so that a slow window does not leave the other workers idle, but it assumes values are spread evenly across the range (see [Skewed data](#skewed-data)).

## How it works

1. **Resolve the range.** For time columns the range is `--interval-start` to `--interval-end`. If the partition column differs from the incremental key, ingestr runs one `MIN`/`MAX` bounds query to narrow the range to where data actually exists. Integer columns always use a bounds query.
2. **Cut it into windows.** Windows are half-open, `column >= start AND column < end`, except the final window, which includes its end boundary so no row is dropped. Rows where the partition column is `NULL` are read by one extra `column IS NULL` window, when bounds discovery runs.
3. **Read windows in parallel.** Up to `--extract-parallelism` workers each take the next window and run it as its own query on its own connection. Record batches from all workers are merged into a single stream for the load, with a one-batch buffer per worker so a slow destination applies backpressure to the source instead of piling up memory.
4. **Fail together.** If any window fails, the remaining windows are cancelled and the run fails.

Each window is an ordinary query. For a table, a window looks like this:

```sql
SELECT *
FROM public.events
WHERE created_at >= '2026-01-01' AND created_at <= '2026-02-01'  -- incremental interval
  AND created_at >= '2026-01-05' AND created_at <  '2026-01-06'  -- this window
```

### Worked examples

**Time column, explicit width.** `--interval-start 2026-01-01 --interval-end 2026-02-01 --extract-partition-interval 1d` yields 31 daily windows. With `--extract-parallelism 5`, five run at a time and each worker picks up a new day as soon as it finishes the previous one.

**Time column, `auto`.** The same 31-day range with `--extract-parallelism 5` targets 20 windows, so each is about 37 hours wide.

**Integer column.** Partitioning by `id` needs no dates at all. ingestr discovers the minimum and maximum, then slices the range:

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

For `id` values from 1 to 1,000,000 this produces ten windows of 100,000 ids. With `--extract-partition-interval auto` it would produce 32 windows (`4 × 8`).

> [!TIP]
> Time columns require `--interval-start` and `--interval-end`. If you want to load an entire large table in parallel without choosing dates, partition by an integer column such as the primary key.

## Custom queries

[Custom queries](/supported-sources/custom_queries) can be partitioned too. The partition column must be returned by the query and must have a unique output name. ingestr wraps the query as a derived table and applies each window to it:

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

Two things differ from table sources:

- The query must be valid inside a `FROM (...)` clause. A single `SELECT` works; constructs that are illegal in a derived table, such as an unbounded `ORDER BY` in SQL Server, do not.
- You still filter by the incremental interval yourself, using `:interval_start` and `:interval_end` as in any [custom query](/supported-sources/custom_queries#incremental-loads). The partition column is then used only to carve up the result.

## Interaction with strategies and destinations

Extract partitioning changes how rows are *read*, not what is written, so it works with every [incremental strategy](/getting-started/incremental-loading): `replace`, `append`, `merge`, `delete+insert`, and `scd2`. A few behaviors are worth knowing:

- **Replace** loads every window before the destination is finalized. If a window or the load fails, the existing destination data is left unchanged on destinations that replace through staging or a transaction. The interval defines the entire replacement data set, so rows outside `--interval-start` and `--interval-end` are not kept. A destination that would replace in place without being able to stage the complete extract first is rejected up front.
- **SCD2** requires `--incremental-key` when extract partitioning is used.
- **Destination parallelism** follows `--extract-parallelism` when you set it explicitly, so raising it also increases concurrent writes to the destination.

The following cannot be combined with extract partitioning, and ingestr rejects them before reading anything: `--sql-limit`, `--stream`, `--full-refresh`, CDC sources, and change-tracking sources. When `--incremental-key` is set, both `--interval-start` and `--interval-end` are required.

## Tradeoffs

Parallel extraction trades extra work and weaker consistency for wall-clock speed. It is not free, and it is not always faster.

### What you gain

- **Throughput.** A single query is limited by one connection, one server-side scan, and one network stream. Several windows overlap these costs, which usually matters most for high-latency sources and for sources that serialize results slowly.
- **Shorter queries.** Each window is a short query rather than one long scan, which helps with statement timeouts and with sources that cancel long-running reads.

### What it costs

**More load on the source.** `--extract-parallelism` queries run concurrently, each holding a connection and doing its own scan work. On a busy production database, pick a modest value and consider a read replica. MySQL sources use a pool of at most 10 connections, so values above that give no extra read concurrency there.

**An index on the partition column.** Every window filters on the partition column. With an index, each window is a cheap range scan. Without one, *each window scans the whole table*, so 30 windows can do 30 times the work of the single query. Partition by an indexed column (typically the primary key or a created/updated timestamp), or leave partitioning off.

**No single consistent snapshot.** Each window is a separate query at a slightly different moment, so the union of windows is not a point-in-time snapshot. If rows are inserted, deleted, or have their partition column updated while the run is in progress, a row can be read twice (moved into a later window) or missed (moved out of a window that already ran). This matters most for:

- `append`, where a duplicated row becomes a duplicate in the destination. Prefer `merge` with a primary key when the source is changing.
- Partitioning by a column that changes, such as `updated_at`. A column that is set once, such as `created_at` or an auto-incrementing `id`, is far safer.

If you need a transactionally consistent copy, use a single-query read or a [CDC snapshot](/getting-started/cdc) instead.

**An extra bounds query.** When the partition column differs from the incremental key, and always for integer columns, ingestr first runs `SELECT MIN(col), MAX(col), COUNT(*), COUNT(col)` over the same filtered rows. With an index this is nearly free; without one it is another full scan. For custom queries it executes the full query once more.

**Custom queries run once per window.** The query is re-executed for every window. A cheap query is fine, but an expensive join or aggregation is repeated N times, and many optimizers cannot push the window predicate down through a derived table, so each execution may compute the whole result before filtering. Where possible, put the heavy work in a view or a table and partition that, or keep the window count low with a larger interval.

**Warehouse cost.** On Snowflake and BigQuery, each window is a separate query. Compute or bytes scanned are billed per window, so a warehouse table that is not clustered or partitioned on the partition column can cost much more than one scan. Partition by the column the warehouse table is organized on.

**Little benefit for embedded databases.** SQLite and DuckDB read from a local file; a single query is often already fast, and concurrent readers share one disk. Test before assuming a speed-up.

**Memory.** Each in-flight window buffers record batches, so peak memory grows with `--extract-parallelism`. The destination may add its own buffers on top.

### Skewed data

Windows have equal *width*, not equal *row counts*. If 80% of your rows were created on three busy days, those windows dominate the run, and the other workers sit idle once their quiet windows finish. To limit the effect:

- Use `auto` or a smaller interval, so heavy regions are split across more windows and the workers stay busy.
- Choose a column that is evenly distributed. A sequential `id` is usually more uniform than a timestamp.
- Avoid very small intervals. Thousands of tiny windows add per-query overhead (planning, connection round-trips) that can outweigh the parallelism. A good target is somewhere between a few dozen and a few hundred windows per run.

### Choosing parameters

| Situation | Suggestion |
| --- | --- |
| First try on a large table | Partition by an indexed integer primary key with `auto`, and `--extract-parallelism 4` to `8`. |
| Time-bounded incremental load | Partition by the incremental key itself with a `1d` or `1h` interval. No bounds query is needed. |
| Source is a production primary | Keep parallelism low, or read from a replica. |
| Source rows change during the load | Use `merge` with a primary key, and partition by an immutable column. |
| Custom query is expensive | Raise the interval to cut the window count, or materialize the query first. |

## Troubleshooting

| Error | Cause |
| --- | --- |
| `extract partition column "x" was not found in source schema` | The column is not in the table or is not returned by the custom query. |
| `extract partition column "x" must be date, timestamp_ntz, timestamp, or integer` | Strings, decimals, and floats cannot be partitioned. Cast to a supported type in a custom query. |
| `extract partitioning requires interval start and end` | Time columns need `--interval-start` and `--interval-end`. |
| `extract partition interval must be at least 24h for date partition column` | A `date` column cannot be split into sub-day windows. |
| `extract partitioning would create more than 100000 windows` | The interval is too small for the range. Increase it or use `auto`. |
| `source table "x" does not support extract partitioning` | The source is not one of the [supported sources](#supported-sources). |
