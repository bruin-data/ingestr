# `ingestr ingest`

The `ingest` command is a core feature of the `ingestr` tool, allowing users to transfer data from a source to a destination with optional support for incremental updates.

## Example

The following example demonstrates how to use the `ingest` command to transfer data from a source to a destination.

```bash
ingestr ingest \
   --source-uri '<your-source-uri-here>' \
   --source-table '<your-schema>.<your-table>' \
   --dest-uri '<your-destination-uri-here>'
```

## Required flags

- `--source-uri TEXT`: Required. Specifies the URI of the data source.
- `--dest-uri TEXT`: Required. Specifies the URI of the destination where data will be ingested.
- `--source-table TEXT`: Required. Defines the source table to fetch data from.

## Optional flags

- `--dest-table TEXT`: Designates the destination table to save the data. If not specified, defaults to the value of `--source-table`.
- `--incremental-key TEXT`: Identifies the column used for incremental reads or replacement. For `append` and `merge`, ingestr passes it to the source read for interval filtering when the source supports it; `append` does not compare it with the destination table. For `delete+insert`, this is the interval column used to decide which destination rows to replace, and should normally be a date, timestamp, partition column, or numeric batch column rather than a row primary key. Numeric `delete+insert` keys work when bounds are inferred from staged rows; `--interval-start` and `--interval-end` are parsed as datetime values, not numeric values. Defaults to `None`.
- `--incremental-predicate TEXT`: Appends a destination-specific SQL predicate to the target match condition for supported SQL destinations with single-table, non-full-refresh merges. Use `t` as the destination alias for BigQuery and Trino, and `target` for other supported destinations. For example: `t.event_date >= DATE_SUB(CURRENT_DATE(), INTERVAL 7 DAY)` on BigQuery, or `target.event_date >= CURRENT_DATE - INTERVAL '7 days'` on PostgreSQL. Use only when every matching destination row is guaranteed to satisfy the predicate; an incorrect predicate can insert duplicates or fail a uniqueness constraint depending on the destination's merge mechanism.
- `--incremental-strategy TEXT`: Defines the strategy for incremental updates. Options include `replace`, `truncate+insert`, `append`, `delete+insert`, `merge`, or `scd2`. The default strategy is `replace`. Not every source and destination supports every strategy; unsupported combinations fail at runtime.
- `--interval-start`: Sets the inclusive start of the interval for the incremental key and passes that start bound to the source read when the source supports interval filtering. For `delete+insert`, this becomes the lower delete bound. If omitted, ingestr can infer the lower bound from staged rows; if it cannot infer a required bound, `delete+insert` skips the delete and insert. Defaults to `None`.
- `--interval-end`: Sets the inclusive end of the interval for the incremental key and passes that end bound to the source read when the source supports interval filtering. For `delete+insert`, this becomes the upper delete bound. If omitted, ingestr can infer the upper bound from staged rows; if it cannot infer a required bound, `delete+insert` skips the delete and insert. Defaults to `None`.
- `--primary-key TEXT`: Specifies a column used to identify one logical row for `merge` and `scd2`. For `delete+insert`, some destinations can use it to deduplicate staged rows during the insert or overwrite step, but this is destination-specific. Use the flag multiple times for composite keys. Primary key values should be non-null: some destinations match null keys as equal during merge, while others reject or duplicate them. This is ingestr strategy configuration; do not rely only on a primary key constraint already existing in the destination database. Defaults to `None`.
- `--columns <name>:<type>:<source>`: Specifies the columns to be ingested. Use `name:type` to override a column's type, `name:type:source` to rename `source` to `name` with a type, or `name::source` to rename only. Multiple entries are comma-separated. Defaults to `None`.
- `--no-inference`: Skips schema inference for schema-less sources and uses `--columns` as the source schema. Requires `--columns`.
- `--mask <column_name>:<algorithm>[:param]`: Applies data masking to specified columns. Can be used multiple times for different columns. See the [Data Masking](../getting-started/data-masking.md) documentation for available algorithms and usage examples. Defaults to `None`.
- `--trim-whitespace`: Trims leading and trailing whitespace from all string column values before writing to the destination. This applies to regular batch ingestions and CDC ingestions, preserves nulls and column types, and leaves non-string columns unchanged. Defaults to `false`. Can also be set with `TRIM_WHITESPACE=true` or `INGESTR_TRIM_WHITESPACE=true`.
- `--schema-naming` Specifies what naming convention to use for table and column names on the destination. Can be `default` or `direct`.default is snake_case. `direct is case sensitive and doesn't contract underscores.
- `--stream`: Runs continuous (streaming) ingestion instead of a one-shot load. Supported by CDC sources (`postgres+cdc`, `mssql+cdc`) and message brokers (`kafka`, `amqp`). The process runs until interrupted (SIGINT/SIGTERM), flushing buffered records to the destination on an interval or record-count trigger. See [Streaming ingestion](#streaming-ingestion) below.
- `--flush-interval`: In streaming mode, flush buffered records to the destination at least this often. Defaults to `30s`. Only valid with `--stream`.
- `--flush-records`: In streaming mode, flush when this many records have been buffered. Defaults to `50000`. Only valid with `--stream`.
- `--metrics-addr`: In streaming mode, serve replication lag and throughput metrics over HTTP on this address (e.g. `127.0.0.1:6060`). Disabled unless set. Only valid with `--stream`. See [Monitoring a stream](#monitoring-a-stream) below.
- `--debug`: Enables debug logging. Some destinations print generated SQL in debug logs; parameterized queries may show placeholders such as `$1`, `?`, `@p1`, or `@p2` for values bound separately by the database driver.

The `interval-start` and `interval-end` options support various datetime formats. When both are provided, `interval-start` must be earlier than `interval-end`. Here are some examples:
- `%Y-%m-%d`: `2023-01-31`
- `%Y-%m-%dT%H:%M:%S`: `2023-01-31T15:00:00`
- `%Y-%m-%dT%H:%M:%S%z`: `2023-01-31T15:00:00+00:00`
- `%Y-%m-%dT%H:%M:%S.%f`: `2023-01-31T15:00:00.000123`
- `%Y-%m-%dT%H:%M:%S.%f%z`: `2023-01-31T15:00:00.000123+00:00`

> [!INFO]
> For the details around the incremental key and the various strategies, please refer to the [Incremental Loading](../getting-started/incremental-loading.md) section.


## Streaming ingestion

The `--stream` flag turns `ingest` into a long-running process that continuously pulls changes from the source and flushes them to the destination, rather than running once and exiting. It is supported by:

- **CDC sources** (`postgres+cdc`, `mssql+cdc`): captures every insert, update, and delete across all tables in the publication/capture set and applies them with the `merge` strategy.
- **Message brokers** (`kafka`, `amqp`): consumes messages into a fixed envelope schema — a `msg_id` primary key, a JSON `data` column holding the decoded body and metadata, and an `_ingestr_order` column (source offset / delivery tag) — and applies them with `merge` keyed on `msg_id`, keeping the latest record per key within each flush window. Schema inference is skipped (a never-ending stream has no end to infer from).

A flush happens whenever **either** `--flush-interval` (default 30s) **or** `--flush-records` (default 50000) is reached, whichever comes first. `--flush-records` is the memory bound: records are buffered until a flush.

Each flush writes the buffered records, merges them into the destination, and only then confirms the source position as durable. This gives **at-least-once delivery**: a crash before a flush completes re-delivers the un-flushed changes on restart, and the `merge` (by primary key / `msg_id`) makes replays idempotent. The stream resumes automatically — CDC from the destination's last recorded LSN, brokers from their committed offset / unacknowledged messages.

Stop a stream with `Ctrl+C` (SIGINT) or SIGTERM; ingestr performs a final flush of buffered data and exits cleanly.

```bash
# Stream all changes from a Postgres publication into BigQuery, flushing
# every 15 seconds or 100k changes, whichever comes first.
ingestr ingest \
   --source-uri 'postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub' \
   --dest-uri 'bigquery://my_project?credentials_path=/path/to/sa.json' \
   --stream \
   --flush-interval 15s \
   --flush-records 100000
```

> [!INFO]
> **Postgres publications.** Pass `publication=<name>` to use a publication you manage yourself. If you omit it, ingestr creates and maintains a publication named `ingestr_publication`, refreshing it on every run to include every logged table that has a replica identity (a primary key, `REPLICA IDENTITY FULL`, or a replica-identity index). Tables that are unlogged, or that lack a replica identity, are skipped with a warning — their changes either never reach the WAL or would make `UPDATE`/`DELETE` on the source fail.

> [!INFO]
> **New tables.** Postgres CDC picks up tables created after ingestion started. A batch run detects them at startup; a stream additionally re-checks the source on an interval (`discover_interval` URI parameter, default `30s`). When a running stream finds a new eligible table, it exits before changing destination data and asks its supervisor to restart it. The restarted process snapshots the table and then streams its retained WAL. User-managed publications are respected, so add the table to the publication yourself. See the [Postgres CDC documentation](../supported-sources/postgres.md#change-data-capture-postgrescdc) for details.

> [!INFO]
> Column-level schema changes are picked up at startup. If a table's columns change while a stream is running, restart the stream to apply the new schema. Run streaming ingestion under a supervisor (systemd, Kubernetes, etc.) so it restarts after transient source/destination outages.

### Monitoring a stream

Passing `--metrics-addr` starts a small HTTP server for the lifetime of the stream that exposes [Prometheus](https://prometheus.io/docs/instrumenting/exposition_formats/) metrics at `/metrics`. It is off unless the flag is set, and the address is bound before ingestion starts, so a port conflict fails immediately rather than half-way through a run. Only ingestr's own metrics are served — the Go runtime and process collectors are deliberately excluded.

```bash
ingestr ingest \
   --source-uri 'postgres+cdc://user:pass@localhost:5432/mydb' \
   --dest-uri 'bigquery://my_project?credentials_path=/path/to/sa.json' \
   --stream \
   --metrics-addr 127.0.0.1:6060

curl -s localhost:6060/metrics | grep -E '^ingestr_replication|^ingestr_stream_table'
```

ingestr publishes:

| Metric | Meaning |
|---|---|
| `ingestr_replication_*{source}` | Replication lag for the current source (see below). Absent when the source cannot report lag. |
| `ingestr_stream_rows_synced_total` | Cumulative rows written **and** confirmed durable since the process started. |
| `ingestr_stream_flush_cycles_total` | Number of completed flush cycles. |
| `ingestr_stream_last_synced_timestamp_seconds` | Unix time of the last successful commit. |
| `ingestr_stream_table_*{table}` | The same row counts and timestamp, broken out per destination table. |

The row counters advance only after a flush's destination write **and** its source-position commit have both succeeded, so they count durable rows rather than merely written ones. `ingestr_stream_last_synced_timestamp_seconds` also advances on cycles that commit a position without writing rows, which is what makes it usable as a staleness alarm: if it stops moving, the stream is stuck.

The `ingestr_replication_*` series carry a `source` label and depend on the engine, because "lag" is not the same quantity everywhere:

- **Postgres** (`postgres+cdc`) reports `ingestr_replication_bytes_behind`: the distance between the server's WAL head and the position ingestr has confirmed durable. This is the same number as `pg_current_wal_lsn() - confirmed_flush_lsn` for the replication slot, so it is what predicts unbounded WAL growth on the source. It is the value to alert on.
- **MongoDB** (`mongodb+cdc`) reports `ingestr_replication_seconds_behind`: the gap between the server's `operationTime` and the cluster time of the last processed change event. Both clocks are server-side, so an idle collection converges to zero instead of drifting upward.
- **SQL Server** (`mssql+cdc`) reports `ingestr_replication_seconds_behind`: the change time between the processed LSN and the capture watermark, via `sys.fn_cdc_map_lsn_to_time`. SQL Server's `binary(10)` LSNs are ordered but their difference is not a log distance, so no `bytes_behind` is published.

Fields that an engine cannot express are omitted rather than reported as a misleading zero. Postgres, for instance, has no per-LSN timestamp, so it publishes no `ingestr_replication_seconds_behind`. Message-broker sources report no replication series at all.

## General flags

- `--help`: Displays the help message and exits the command.

## Examples

### Ingesting a CSV file to DuckDB

```bash
ingestr ingest \
   --source-uri 'csv://input.csv' \
   --source-table 'sample' \
   --dest-uri 'duckdb://output.duckdb'
```

### Copy a table from Postgres to DuckDB

```bash
ingestr ingest \
   --source-uri 'postgresql://myuser:mypassword@localhost:5432/mydatabase?sslmode=disable' \
   --source-table 'public.input_table' \
   --dest-uri 'duckdb://output.duckdb' \
   --dest-table 'public.output_table'
```

### Replace a staged date slice from Postgres to BigQuery

```bash
ingestr ingest \
   --source-uri 'postgresql://myuser:mypassword@localhost:5432/mydatabase?sslmode=disable' \
   --source-table "query:SELECT * FROM public.users WHERE dt = '2023-01-01'" \
   --dest-uri 'bigquery://my_project?credentials_path=/path/to/service/account.json&location=EU' \
   --dest-table 'raw.users' \
   --incremental-key 'dt' \
   --incremental-strategy 'delete+insert' \
   --columns 'dt:date'
```

### Load an interval of data from Postgres to BigQuery using a date column

```bash
ingestr ingest \
   --source-uri 'postgresql://myuser:mypassword@localhost:5432/mydatabase?sslmode=disable' \
   --source-table 'public.users' \
   --dest-uri 'bigquery://my_project?credentials_path=/path/to/service/account.json&location=EU' \
   --dest-table 'raw.users' \
   --incremental-key 'dt' \
   --incremental-strategy 'delete+insert' \
   --interval-start '2023-01-01' \
   --interval-end '2023-01-31' \
   --columns 'dt:date'
```

### Load a specific query from Postgres to Snowflake

```bash
ingestr ingest \
   --source-uri 'postgresql://myuser:mypassword@localhost:5432/mydatabase?sslmode=disable' \
   --dest-uri 'snowflake://user:password@account/dbname?warehouse=COMPUTE_WH&role=my_role' \
   --source-table 'query:SELECT * FROM public.users as pu JOIN public.orders as o ON pu.id = o.user_id WHERE pu.dt BETWEEN :interval_start AND :interval_end' \
   --dest-table 'raw.users' \
   --incremental-key 'dt' \
   --incremental-strategy 'delete+insert' \
   --interval-start '2023-01-01' \
   --interval-end '2023-01-31' \
   --columns 'dt:date'
```

### Ingesting with Data Masking

```bash
ingestr ingest \
   --source-uri 'postgresql://user:pass@localhost/customers' \
   --source-table 'customer_data' \
   --dest-uri 'duckdb:///masked_customers.db' \
   --dest-table 'masked_customers' \
   --mask 'email:hash' \
   --mask 'phone:partial:3' \
   --mask 'ssn:redact' \
   --mask 'salary:round:5000'
```

This example demonstrates masking sensitive customer data:
- Email addresses are hashed for consistent anonymization
- Phone numbers show only first and last 3 digits
- SSNs are completely redacted
- Salaries are rounded to nearest $5000

### Trimming whitespace from string values

```bash
ingestr ingest \
   --source-uri 'postgresql://user:pass@localhost/app?sslmode=disable' \
   --source-table 'public.customers' \
   --dest-uri 'duckdb:///warehouse.duckdb' \
   --dest-table 'raw.customers' \
   --trim-whitespace
```

This trims leading and trailing whitespace from string values as data streams through ingestr. For example, `"  Alice  "` becomes `"Alice"` and `"\tA-123\n"` becomes `"A-123"`. Interior whitespace, such as `"ACME  Inc"`, is preserved.

### Overriding column types

Use `--columns` to set the type of one or more columns on the destination. Each entry is `name:type`, and multiple entries are comma-separated:

```bash
ingestr ingest \
   --source-uri 'postgresql://user:pass@localhost/app?sslmode=disable' \
   --source-table 'public.customers' \
   --dest-uri 'snowflake://user:password@account/dbname?warehouse=COMPUTE_WH' \
   --dest-table 'raw.customers' \
   --columns 'id:bigint,signup_date:date,balance:decimal(18,2)'
```

Supported types include `bigint`, `int`, `smallint`, `tinyint`, `float`, `double`, `decimal(p,s)`, `string`, `text`, `varchar(n)`, `boolean`, `date`, `timestamp`, `timestamp_ntz`, `json`, `uuid`, and `binary`.

#### Sized string types

String types accept an optional length, so you can create a bounded column such as `varchar(50)` instead of an unbounded text column:

```bash
ingestr ingest \
   --source-uri 'postgresql://user:pass@localhost/app?sslmode=disable' \
   --source-table 'public.customers' \
   --dest-uri 'postgresql://user:pass@localhost/warehouse?sslmode=disable' \
   --dest-table 'raw.customers' \
   --columns 'name:varchar(100),email:varchar(255)'
```

The types that accept a length are `varchar(n)`, `string(n)`, and `text(n)` — all equivalent. A string type given without a length (`varchar`, `string`, or `text`) creates an unbounded column.

> [!INFO]
> For more examples, please refer to the specific platforms' documentation on the sidebar.
