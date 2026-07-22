---
outline: deep
---

# Incremental Loading
ingestr supports several write strategies for loading or refreshing destination data. Incremental loading usually means reading a bounded slice of source rows and then appending, merging, or replacing that slice in the destination instead of reloading the whole table every time.

Before you use incremental loading, you should understand 3 important keys:
- `primary_key`: the column or columns ingestr should use to identify one logical row. This is strategy configuration, not just a database constraint on the destination table. On the CLI, pass it with `--primary-key` once per key column. Primary key values should be non-null: some destinations match null keys as equal during merge, while others reject or duplicate them. If you run ingestr through an asset/orchestrator, make sure the asset passes primary keys to ingestr instead of relying only on the database table definition.
- `incremental_key`: the column ingestr should use to find or replace a bounded slice of rows. For source-side filtering with `--interval-start` and `--interval-end`, this is usually a date or timestamp column such as `created_at`, `updated_at`, or `dt`. Numeric keys such as `batch_id` are supported for `delete+insert` when bounds are inferred from staged rows; numeric extract-partition columns are configured separately with `--extract-partition-by`.
- `strategy`: the strategy to use for loading, the available strategies are:
  - `replace`: replace the existing destination table with the source directly, this is the default strategy and the simplest one.
    - This strategy isn't recommended for large tables, as it will replace the entire table and can be slow.
  - `truncate+insert`: empty the existing destination table in place, then insert the rows read from the source.
  - `append`: simply append the new rows to the destination table.
  - `merge`: merge the new rows with the existing rows in the destination table, insert the new ones and update the existing ones with the new values.
  - `delete+insert`: delete existing destination rows inside the incremental-key interval and then insert the staged rows.
  - `scd2`: keep historical versions of rows using Slowly Changing Dimension Type 2 columns.

Not every strategy is available for every source and destination. Some sources handle incrementality internally and may choose their own strategy or incremental key, and destinations only support the strategies they can execute safely.

## Replace
Replace is the default strategy, and it simply replaces the entire destination table with the source table.

The following example below will replace the entire `my_schema.some_data` table in BigQuery with the `my_schema.some_data` table in Postgres.
```bash
ingestr ingest \
    --source-uri 'postgresql://admin:admin@localhost:8837/web?sslmode=disable' \
    --source-table 'my_schema.some_data' \
    --dest-uri 'bigquery://<your-project-name>?credentials_path=/path/to/service/account.json' \
```

Here's how the replace strategy works:
- The source table is downloaded.
- The source table is uploaded to the destination, replacing the destination table.

Replace can use `--extract-partition-by` to read a bounded incremental-key interval through parallel source queries. All extract windows are loaded before the destination replacement is finalized. On destinations with a staging-based replace or transactional truncate+insert implementation, a failed extract or load leaves the existing destination data unchanged.

The interval still defines the complete replacement data set: destination rows outside `--interval-start` and `--interval-end` are not retained. Use `merge` or `delete+insert` instead when the destination should keep rows outside the extracted interval.


> [!CAUTION]
> This strategy will delete the entire destination table and replace it with the source table, use with caution.

## Truncate+Insert
Truncate+Insert empties the existing destination table in place and then inserts the rows read from the source. Unlike `replace`, it keeps the same destination table object, which can help preserve dependent views, grants, and foreign keys on destinations that support truncation.

This strategy is only available for destinations that support truncating a table in place. It is not atomic on every destination: readers may see an empty table between the truncate and the insert, and a failed insert can leave the table empty.

## Append
Append writes every row returned by the source to the destination table. By default, it appends all rows read from the source. `incremental_key` is only used for source-side filtering when the source supports it, usually together with `--interval-start` and `--interval-end`; append does not look at the destination table to find the latest value.

The following example appends rows from the `my_schema.some_data` table in Postgres to the `my_schema.some_data` table in BigQuery, reading only source rows whose `updated_at` is on or after `2021-01-02`.
```bash
ingestr ingest \
    --source-uri 'postgresql://admin:admin@localhost:8837/web?sslmode=disable' \
    --source-table 'my_schema.some_data' \
    --dest-uri 'bigquery://<your-project-name>?credentials_path=/path/to/service/account.json' \
    --incremental-strategy append \
    --incremental-key updated_at \
    --interval-start '2021-01-02'
```

### Example

Let's assume you had the following source table:
| id | name | updated_at |
|----|------|------------|
| 1  | John | 2021-01-01 |
| 2  | Jane | 2021-01-01 |

#### Initial full ingestion
First, assume you loaded the existing rows into the destination table. Here's how your destination looks:

| id | name | updated_at |
|----|------|------------|
| 1  | John | 2021-01-01 |
| 2  | Jane | 2021-01-01 |

#### Second Ingestion, no new data
When the source-side filter returns no rows, the destination table remains the same.

#### Third Ingestion, new data
Let's say John changed his name to Johnny, and that row's `updated_at` was updated to `2021-01-02`, e.g. your source:
| id | name   | updated_at |
|----|--------|------------|
| 1  | Johnny | 2021-01-02 |
| 2  | Jane   | 2021-01-01 |


When you run append with a source-side filter such as `--interval-start '2021-01-02'`, ingestr appends the rows returned by that source read. Here's how your destination looks like now:
| id | name   | updated_at |
|----|--------|------------|
| 1  | John   | 2021-01-01 |
| 2  | Jane   | 2021-01-01 |
| 1  | Johnny | 2021-01-02 |

**Notice the last row in the table:** it's the new row that was ingested from the source table.

The behavior is the same for any rows matching the source-side filter. ingestr does not inspect destination state for append filtering.

> [!TIP]
> The `append` strategy allows you to keep a version history of your data, as it will keep appending the new rows to the destination table. You can use it to build [Slowly Changing Dimensions (SCD) Type 2](https://en.wikipedia.org/wiki/Slowly_changing_dimension#Type_2:_add_new_row) tables, for example.


## Merge
Merge will merge source rows with existing rows in the destination table. It uses `primary_key` to decide whether a staged row updates an existing row or inserts a new one. By default, it will merge all rows read from the source. If you only want to read rows after a cursor, provide an `incremental_key` with interval bounds, or use a source that handles incrementality internally.

The following example merges rows from the `my_schema.some_data` table in Postgres into the `my_schema.some_data` table in BigQuery, reading only source rows whose `updated_at` is on or after `2021-01-02`.
```bash
ingestr ingest \
    --source-uri 'postgresql://admin:admin@localhost:8837/web?sslmode=disable' \
    --source-table 'my_schema.some_data' \
    --dest-uri 'bigquery://<your-project-name>?credentials_path=/path/to/service/account.json' \
    --incremental-strategy merge \
    --incremental-key updated_at \
    --interval-start '2021-01-02' \
    --primary-key id
```

Here's how the merge strategy works:
- ingestr writes the source rows to a staging table.
- If multiple staged rows have the same `primary_key`, most SQL destinations deduplicate the staged rows before merge. On destinations that support ordered merge deduplication, `incremental_key` makes the row with the highest incremental key value win; otherwise the winning row may be destination-dependent.
- If a staged row's `primary_key` already exists in the destination table, ingestr updates the destination row with the staged values.
- If a staged row's `primary_key` does not exist in the destination table, ingestr inserts it.
- Destination rows whose `primary_key` is not present in staging remain unchanged.

> [!WARNING]
> `primary_key` values should be non-null. Some destinations match null keys as equal in merge conditions, while others may reject them or allow duplicate null-key rows. Null primary keys can therefore cause unexpected updates or duplicate rows.

In pseudocode, the pattern is:

```sql
CREATE OR REPLACE TABLE staging AS <rows read from source>;

MERGE INTO target AS t
USING (
  SELECT *
  FROM staging
  QUALIFY ROW_NUMBER() OVER (
    PARTITION BY primary_key
    ORDER BY incremental_key DESC -- where supported and provided
  ) = 1
) AS s
ON t.primary_key = s.primary_key
WHEN MATCHED THEN UPDATE SET ...
WHEN NOT MATCHED THEN INSERT (...);
```

The exact SQL differs by destination, but the important inputs are the same: `primary_key` controls row identity, and `incremental_key` can control source filtering when interval bounds or source-specific state are in use. On destinations with ordered staged-row deduplication, `incremental_key` also controls latest-row tie breaking. If no `incremental_key` is provided, duplicate staged rows with the same `primary_key` may still be collapsed, but the winning row is not ordered by a cursor.

### Example

Let's assume you had the following source table:
| id | name | updated_at |
|----|------|------------|
| 1  | John | 2021-01-01 |
| 2  | Jane | 2021-01-01 |

#### Initial full ingestion
First, assume you loaded the existing rows into the destination table. Here's how your destination looks:

| id | name | updated_at |
|----|------|------------|
| 1  | John | 2021-01-01 |
| 2  | Jane | 2021-01-01 |

#### Second Ingestion, no new data
When the source-side filter returns no rows, the destination table remains the same.

#### Third Ingestion, new data
Let's say John changed his name to Johnny, e.g. your source:
| id | name   | updated_at |
|----|--------|------------|
| 1  | Johnny | 2021-01-02 |
| 2  | Jane   | 2021-01-01 |
    
When you run merge with a source-side filter such as `--interval-start '2021-01-02'`, ingestr stages the rows returned by that source read and merges them into the destination table. Here's how your destination looks like now:
| id | name   | updated_at |
|----|--------|------------|
| 1  | Johnny | 2021-01-02 |
| 2  | Jane   | 2021-01-01 |

**Notice the first row in the table:** it's the updated row that was ingested from the source table.

The behavior is the same for any rows matching the source-side filter. ingestr does not inspect destination state for merge filtering.

> [!TIP]
> The `merge` strategy is different from the `append` strategy, as it will update the existing rows in the destination table with the new values from the source table. It's useful when you want to keep the latest version of your data in the destination table.

> [!CAUTION]
> For the cases where there's a primary key match, the `merge` strategy will **update** the existing rows in the destination table with the new values from the source table. Use with caution, as it can lead to data loss if not used properly, as well as data processing charges if your data warehouse charges for updates.

### Limiting the destination scan

`--incremental-predicate` appends a destination-specific SQL condition to the target match for supported SQL destinations. This can prune old partitions that cannot contain matching rows:

```bash
ingestr ingest \
    --source-uri 'duckdb:///tmp/source.duckdb' \
    --source-table 'updates' \
    --dest-uri 'bigquery://my-project/analytics' \
    --dest-table 'events' \
    --incremental-strategy merge \
    --primary-key id \
    --incremental-predicate "t.event_date >= DATE_SUB(CURRENT_DATE(), INTERVAL 7 DAY)"
```

The destination alias is `t` for BigQuery and Trino, and `target` for Snowflake, Redshift, Databricks, Microsoft SQL Server, Azure Synapse, Microsoft Fabric, Oracle, MySQL, PlanetScale, Vitess, PostgreSQL, SQLite, DuckDB, and DuckLake. Source aliases are destination-specific; predicates should normally reference only destination columns.

The predicate does not filter source rows. It only limits which destination rows can match or be updated. Use a predicate only when every destination row that may match the incoming primary keys is inside the selected range. Otherwise, the merge can insert a duplicate or a constrained destination can reject the insert.

Destinations whose merge implementation has no target-side SQL match condition do not accept this option. This includes ClickHouse, StarRocks, CrateDB, Cassandra, DynamoDB, MongoDB, Iceberg, OneLake, and discard.

## Delete+Insert
Delete+Insert replaces a slice of the destination table. It stages the rows read from the source, works out the interval covered by those staged rows and any explicit bounds, deletes destination rows whose `incremental_key` falls inside that interval, and inserts the staged rows back into the destination. When you provide `--interval-start` or `--interval-end`, ingestr also passes those bounds to the source read, so sources that support interval filtering may return only that slice before the destination replacement happens.

The `incremental_key` is required and should be a date, timestamp, or numeric column that defines the slice to replace, such as a partition date or batch ID. A primary key such as `id` is usually the wrong choice for `delete+insert`; provide `primary_key` separately only if you also want destinations that support it to collapse duplicate staged rows during the insert or overwrite step. Numeric incremental keys are supported when ingestr infers the replacement bounds from staged rows; `--interval-start` and `--interval-end` are parsed as datetime values, so explicit CLI bounds are intended for date and timestamp keys.

The following example stages only rows with `dt = 2021-01-02` and lets ingestr infer that replacement interval from the staged rows.
```bash
ingestr ingest \
    --source-uri 'postgresql://admin:admin@localhost:8837/web?sslmode=disable' \
    --source-table "query:SELECT * FROM my_schema.some_data WHERE dt = '2021-01-02'" \
    --dest-uri 'bigquery://<your-project-name>?credentials_path=/path/to/service/account.json' \
    --dest-table 'my_schema.some_data' \
    --incremental-strategy delete+insert \
    --incremental-key dt \
    --columns 'dt:date'
```

Here's how the delete+insert strategy works:
- The new rows from the source table will be inserted into a staging table in the destination database.
- ingestr determines the interval to replace:
  - The start and end bounds are resolved independently.
  - Each bound can be supplied explicitly with `--interval-start` or `--interval-end`.
  - Any omitted bound is inferred from the minimum or maximum `incremental_key` value found in the staging table.
  - If either bound is omitted and cannot be inferred from staged rows, ingestr skips the delete and insert.
- The existing rows in the destination table whose `incremental_key` is between the interval start and end are deleted.
- The staged rows are inserted into the destination table.

In pseudocode, the pattern is:

```sql
CREATE OR REPLACE TABLE staging AS <rows read from source>;

start_bound = COALESCE(--interval-start, MIN(staging.incremental_key));
end_bound   = COALESCE(--interval-end,   MAX(staging.incremental_key));

BEGIN TRANSACTION; -- or a destination-specific atomic/overwrite operation

DELETE FROM target
WHERE incremental_key >= start_bound
  AND incremental_key <= end_bound;

INSERT INTO target (columns...)
SELECT columns...
FROM staging;

COMMIT;
```

Some destinations send the bounds as query parameters instead of literal values. For example, SQL Server-style logs may show `@p1` and `@p2`; these are the interval start and interval end values bound by ingestr at execution time.

A few important notes about the `delete+insert` strategy: 
- it does not guarantee the order of the rows in the destination table, as it will delete and insert the rows in the destination table.
- it does not deduplicate by `incremental_key`, which means you may have multiple rows with the same `incremental_key` in the destination table.
- `primary_key` deduplication is destination-specific. Some destinations collapse duplicate staged rows during the insert or overwrite step and keep the latest row per primary key by `incremental_key`; others insert every staged row.
- explicit `--interval-start` and `--interval-end` values are parsed as datetimes. For numeric `incremental_key` values, let ingestr infer the bounds from staged rows instead of passing explicit CLI bounds.
- use `--debug` to print more strategy details and generated SQL where the destination supports it. Parameterized queries may still show placeholders such as `$1`, `?`, `@p1`, or `@p2` instead of the bound values.
- atomicity is destination-specific. Many SQL destinations wrap the delete and insert in a transaction or atomic script; ClickHouse uses an `ALTER TABLE ... DELETE` mutation followed by an insert.

> [!WARNING]
> **Breaking change:** `delete+insert` is no longer supported for **CrateDB** and **Trino** destinations. ingestr previously ran them as two non-atomic statements that could leave the table in a partially-updated state on failure or under concurrent runs. ingestr now fails this strategy up front for these destinations instead of loading data unsafely. Use `merge` or `replace` instead. See the [CrateDB](/supported-sources/cratedb) and [Trino](/supported-sources/trino) destination notes for details.

### Example
Let's assume you had the following source table slice:
| id | name | dt         |
|----|------|------------|
| 1  | John | 2021-01-02 |
| 2  | Jane | 2021-01-02 |

#### Initial full ingestion
First, assume you loaded the existing rows into the destination table. Here's how your destination looks:
| id | name | dt         |
|----|------|------------|
| 1  | John | 2021-01-02 |
| 2  | Jane | 2021-01-02 |

#### Second Ingestion, no changed data
When the same source rows are returned for the interval, ingestr deletes and reinserts the interval and the destination table ends up the same. If there are no staged rows and a bound cannot be inferred, ingestr skips the delete and insert operation.

> [!CAUTION]
> If the source read does not include every row that should exist for the interval, the missing rows will be deleted from the destination table for that interval. This is why `delete+insert` is usually used with date, timestamp, or batch intervals.

#### Third Ingestion, new data
Let's say John changed his name to Johnny, e.g. your source:
| id | name   | dt         |
|----|--------|------------|
| 1  | Johnny | 2021-01-02 |
| 2  | Jane   | 2021-01-02 |

When you run the command again for `dt = 2021-01-02`, it deletes the existing rows in that interval and then inserts the staged rows from the source table. Here's how your destination looks like now:
| id | name   | dt         |
|----|--------|------------|
| 1  | Johnny | 2021-01-02 |
| 2  | Jane   | 2021-01-02 |

**Notice the first row in the table:** it's the updated row that was ingested from the source table.

The behavior is the same for any replacement interval. ingestr replaces the interval covered by staged rows or explicit bounds; it does not inspect destination state to choose a high-watermark.

> [!TIP]
> The `delete+insert` strategy is useful when you want to replace complete slices of the destination table. It deletes destination rows inside the `incremental_key` interval and then inserts the staged source rows, which also makes it useful for backfills.

## SCD2
SCD2 keeps historical versions of rows instead of overwriting them in place. It requires `primary_key` and adds `_scd_valid_from`, `_scd_valid_to`, and `_scd_is_current` columns to track which version of each row is current.

When `incremental_key` is provided, ingestr can use it for source-side filtering just like `merge`. SCD2 also treats the run as a partial slice and does not soft-delete destination keys that are missing from staging. Without `incremental_key`, SCD2 treats missing staged keys as absent from the full source snapshot and can close their current records.

SCD2 is destination-specific. Use it only on destinations that support the `scd2` strategy.

## Conclusion
Incremental loading allows you to ingest a bounded source slice into the destination table. It's useful when you want to keep the destination table up-to-date with the source table, as well as when you want to keep a version history of your data in the destination table. However, there are a few things to keep in mind when using incremental loading:

- If you can and your data is not huge, use the `replace` strategy, as it's the simplest strategy and it will replace the entire destination table with the source table, which will always give you a clean exact replica of the source table.
- If you need to preserve the existing destination table object, and your destination supports it, use `truncate+insert`.
- If you want to keep a version history of your data, use the `append` strategy, as it will keep appending the new rows to the destination table, which will give you a version history of your data.
- If you want to keep the latest version of your data in the destination table and your table has a natural primary key, such as a user ID, use the `merge` strategy, as it will update the existing rows in the destination table with the new values from the source table.
- If you want to replace complete slices of the destination table or backfill data, use the `delete+insert` strategy with a date, timestamp, or batch interval.
- If you want a managed version history with current-row markers, use `scd2` on a destination that supports it.

> [!TIP]
> Even though the document tries to explain, there's no better learning than trying it yourself. You can use the [Quickstart](/getting-started/quickstart.md) to try the incremental loading strategies yourself.
