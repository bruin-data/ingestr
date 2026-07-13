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
- `--incremental-key TEXT`: Identifies the key used for incremental data strategies. Defaults to `None`.
- `--incremental-strategy TEXT`: Defines the strategy for incremental updates. Options include `replace`, `append`, `delete+insert`, or `merge`. The default strategy is `replace`.
- `--interval-start`: Sets the start of the interval for the incremental key. Defaults to `None`.
- `--interval-end`: Sets the end of the interval for the incremental key. Defaults to `None`.
- `--primary-key TEXT`: Specifies the primary key for the merge operation. Defaults to `None`.
- `--columns <name>:<type>:<source>`: Specifies the columns to be ingested. Use `name:type` to override a column's type, `name:type:source` to rename `source` to `name` with a type, or `name::source` to rename only. Multiple entries are comma-separated. Defaults to `None`.
- `--no-inference`: Skips schema inference for schema-less sources and uses `--columns` as the source schema. Requires `--columns`.
- `--mask <column_name>:<algorithm>[:param]`: Applies data masking to specified columns. Can be used multiple times for different columns. See the [Data Masking](../getting-started/data-masking.md) documentation for available algorithms and usage examples. Defaults to `None`.
- `--trim-whitespace`: Trims leading and trailing whitespace from all string column values before writing to the destination. This applies to regular batch ingestions and CDC ingestions, preserves nulls and column types, and leaves non-string columns unchanged. Defaults to `false`. Can also be set with `TRIM_WHITESPACE=true` or `INGESTR_TRIM_WHITESPACE=true`.
- `--schema-naming` Specifies what naming convention to use for table and column names on the destination. Can be `default` or `direct`.default is snake_case. `direct is case sensitive and doesn't contract underscores.
- `--stream`: Runs continuous (streaming) ingestion instead of a one-shot load. Supported by CDC sources (`postgres+cdc`, `mssql+cdc`) and message brokers (`kafka`, `amqp`). The process runs until interrupted (SIGINT/SIGTERM), flushing buffered records to the destination on an interval or record-count trigger. See [Streaming ingestion](#streaming-ingestion) below.
- `--flush-interval`: In streaming mode, flush buffered records to the destination at least this often. Defaults to `30s`. Only valid with `--stream`.
- `--flush-records`: In streaming mode, flush when this many records have been buffered. Defaults to `50000`. Only valid with `--stream`.
- `--metrics-addr`: In streaming mode, serve replication lag and throughput metrics over HTTP on this address (e.g. `127.0.0.1:6060`). Disabled unless set. Only valid with `--stream`. See [Monitoring a stream](#monitoring-a-stream) below.

The `interval-start` and `interval-end` options support various datetime formats, here are some examples:
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

Passing `--metrics-addr` starts a small HTTP server for the lifetime of the stream that exposes Go [expvar](https://pkg.go.dev/expvar) metrics at `/debug/vars`. It is off unless the flag is set, and the address is bound before ingestion starts, so a port conflict fails immediately rather than half-way through a run.

```bash
ingestr ingest \
   --source-uri 'postgres+cdc://user:pass@localhost:5432/mydb' \
   --dest-uri 'bigquery://my_project?credentials_path=/path/to/sa.json' \
   --stream \
   --metrics-addr 127.0.0.1:6060

curl -s localhost:6060/debug/vars | jq '.ingestr_replication, .ingestr_stream_tables'
```

Alongside Go's standard `cmdline` and `memstats`, ingestr publishes:

| Key | Meaning |
|---|---|
| `ingestr_replication` | Replication lag for the current source (see below). `{"streaming": false}` when the source cannot report lag. |
| `ingestr_stream_rows_synced` | Cumulative rows written **and** confirmed durable since the process started. |
| `ingestr_stream_flush_cycles` | Number of completed flush cycles. |
| `ingestr_stream_last_synced_unix` | Unix time of the last successful commit. |
| `ingestr_stream_tables` | The same row counts and timestamp, broken out per destination table. |

The row counters advance only after a flush's destination write **and** its source-position commit have both succeeded, so they count durable rows rather than merely written ones. `ingestr_stream_last_synced_unix` also advances on cycles that commit a position without writing rows, which is what makes it usable as a staleness alarm: if it stops moving, the stream is stuck.

What `ingestr_replication` contains depends on the engine, because "lag" is not the same quantity everywhere:

- **Postgres** (`postgres+cdc`) reports `bytes_behind`: the distance between the server's WAL head and the position ingestr has confirmed durable. This is the same number as `pg_current_wal_lsn() - confirmed_flush_lsn` for the replication slot, so it is what predicts unbounded WAL growth on the source. It is the value to alert on.
- **MongoDB** (`mongodb+cdc`) reports `seconds_behind`: the gap between the server's `operationTime` and the cluster time of the last processed change event. Both clocks are server-side, so an idle collection converges to zero instead of drifting upward.
- **SQL Server** (`mssql+cdc`) reports `seconds_behind`: the change time between the processed LSN and the capture watermark, via `sys.fn_cdc_map_lsn_to_time`. SQL Server's `binary(10)` LSNs are ordered but their difference is not a log distance, so no `bytes_behind` is published.

Fields that an engine cannot express are omitted rather than reported as a misleading zero. Postgres, for instance, has no per-LSN timestamp, so it publishes no `seconds_behind`. Message-broker sources report no lag block at all.

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

### Incrementally ingest a table from Postgres to BigQuery

```bash
ingestr ingest 
   --source-uri 'postgresql://myuser:mypassword@localhost:5432/mydatabase?sslmode=disable' \
   --source-table 'public.users' \
   --dest-uri 'bigquery://my_project?credentials_path=/path/to/service/account.json&location=EU' \
   --dest-table 'raw.users' \
   --incremental-key 'updated_at' \
   --incremental-strategy 'delete+insert'
```

### Load an interval of data from Postgres to BigQuery using a date column

```bash
ingestr ingest 
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
ingestr ingest 
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
