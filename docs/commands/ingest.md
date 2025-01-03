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
- `--columns <column_name>:<column_type>`: Specifies the columns to be ingested. Defaults to `None`.

The `interval-start` and `interval-end` options support various datetime formats, here are some examples:
- `%Y-%m-%d`: `2023-01-31`
- `%Y-%m-%dT%H:%M:%S`: `2023-01-31T15:00:00`
- `%Y-%m-%dT%H:%M:%S%z`: `2023-01-31T15:00:00+00:00`
- `%Y-%m-%dT%H:%M:%S.%f`: `2023-01-31T15:00:00.000123`
- `%Y-%m-%dT%H:%M:%S.%f%z`: `2023-01-31T15:00:00.000123+00:00`

> [!INFO]
> For the details around the incremental key and the various strategies, please refer to the [Incremental Loading](../getting-started/incremental-loading.md) section.

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

> [!INFO]
> For more examples, please refer to the specific platforms' documentation on the sidebar.