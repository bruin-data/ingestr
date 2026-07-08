# Apache Iceberg

[Apache Iceberg](https://iceberg.apache.org/) is an open table format for large analytic datasets.

ingestr supports Iceberg as a destination.

## URI format

The Iceberg destination uses the catalog backend in the URI scheme:

```plaintext
iceberg+<catalog-backend>://<catalog-location>?storage=<storage-backend>&...
```

Supported catalog schemes:

- `iceberg+sqlite`
- `iceberg+postgres`
- `iceberg+rest`
- `iceberg+glue`
- `iceberg+hadoop`
- `iceberg+hive`
- `iceberg+sql` for advanced pass-through SQL catalog options
- `iceberg` with `catalog=<catalog-type>` or `type=<catalog-type>`

Common URI parameters:

- `catalog_name` (optional): logical catalog name used by the Iceberg client. Defaults to `ingestr`.
- `storage=s3`: use S3 or an S3-compatible object store.
- `bucket`: S3 bucket name. Combined with `prefix` to produce the Iceberg warehouse location.
- `prefix` (optional): path prefix inside the bucket.
- `endpoint` (optional): S3-compatible endpoint such as `localhost:9000`.
- `use_ssl=false` (optional): use plain HTTP for S3-compatible local storage.
- `access_key_id`, `secret_access_key`, `session_token`, `region`: S3 or Glue credentials and region aliases.
- `warehouse`: advanced override for the Iceberg warehouse location, such as `s3://bucket/warehouse`.
- `warehouse_path`: local warehouse path alias for non-S3 catalog setups.
- `create_namespace` (optional): create the destination namespace automatically. Defaults to `true`.
- `table_location` (optional): explicit table location template. Supports `{namespace}`, `{namespace_dot}`, `{table}`, `{identifier}`, and `{identifier_dot}`.
- `table_path` (optional): path template appended under `bucket` and `prefix`, for example `{namespace}/{table}`.
- `table.<key>` (optional): table properties passed to Iceberg table creation, for example `table.write.format.default=parquet`.

Advanced Iceberg-Go catalog properties are still accepted and passed through, including the older `iceberg+sql://?uri=...` form.

## Examples

### SQLite catalog with local MinIO

```bash
ingestr ingest \
  --source-uri "jsonl://$PWD/events.jsonl" \
  --source-table events.jsonl \
  --dest-uri "iceberg+sqlite://$PWD/state/catalog.db?storage=s3&bucket=ingestr-iceberg&endpoint=localhost:9000&use_ssl=false&access_key_id=minioadmin&secret_access_key=minioadmin&region=us-east-1&table_path={namespace}/{table}&table.write.format.default=parquet" \
  --dest-table demo.events \
  --incremental-strategy replace \
  --primary-key id
```

### Local Hadoop catalog with local filesystem storage

```bash
ingestr ingest \
  --source-uri 'postgresql://user:pass@localhost:5432/app' \
  --source-table public.orders \
  --dest-uri 'iceberg+hadoop:///tmp/iceberg-warehouse' \
  --dest-table analytics.orders \
  --incremental-strategy append
```

### REST catalog with S3 storage

```bash
ingestr ingest \
  --source-uri 'mysql://user:pass@mysql.internal:3306/app' \
  --source-table orders \
  --dest-uri 'iceberg+rest://catalog.internal:8181?storage=s3&bucket=warehouse&prefix=prod&region=us-east-1' \
  --dest-table sales.orders \
  --incremental-strategy append
```

### AWS Glue catalog

```bash
ingestr ingest \
  --source-uri 'snowflake://user:pass@acct/db/schema?warehouse=COMPUTE_WH' \
  --source-table raw.events \
  --dest-uri 'iceberg+glue://?region=us-east-1&storage=s3&bucket=company-lake&prefix=iceberg&table_path={namespace}/{table}' \
  --dest-table analytics.events \
  --incremental-strategy replace \
  --primary-key id
```

### Hive metastore with MinIO

```bash
ingestr ingest \
  --source-uri 'duckdb:///tmp/source.duckdb' \
  --source-table main.clicks \
  --dest-uri 'iceberg+hive://localhost:9083?storage=s3&bucket=warehouse&endpoint=localhost:9000&use_ssl=false&access_key_id=minioadmin&secret_access_key=minioadmin&region=us-east-1' \
  --dest-table web.clicks \
  --incremental-strategy replace
```

### Postgres SQL catalog with S3

```bash
ingestr ingest \
  --source-uri 'bigquery://project/dataset' \
  --source-table events \
  --dest-uri 'iceberg+postgres://iceberg_user:secret@metadata-db.internal:5432/iceberg_catalog?storage=s3&bucket=company-lake&prefix=warehouse&region=eu-west-1' \
  --dest-table analytics.events \
  --incremental-strategy replace \
  --primary-key event_id
```

## Table naming

Use an Iceberg table identifier in `--dest-table`, usually `namespace.table`.

For nested namespaces, use dot-separated identifiers such as `lake.analytics.events`.

## Supported write dispositions

Iceberg supports `append`, `replace`, `merge`, `delete+insert`, `truncate+insert`, and `scd2`.

`replace` writes a new Iceberg overwrite snapshot for the destination table. The incremental strategies are implemented natively: each run stages the incoming rows in a temporary Iceberg table and then commits a single atomic snapshot with the changes.

- `merge` upserts by primary key: rows with duplicate primary keys are deduplicated (the highest `--incremental-key` value wins when one is set), existing rows are updated in place, and net-new rows are inserted. CDC streams are merged with delete awareness.
- `delete+insert` deletes the rows whose incremental key falls inside the loaded interval and inserts the staged rows (deduplicated by primary key when one is set).
- `truncate+insert` empties the table in place (keeping schema, partition spec, and history) and reloads it.
- `scd2` maintains slowly-changing-dimension history with `_scd_valid_from`, `_scd_valid_to`, and `_scd_is_current` columns.

### Merge modes and memory usage

`merge` and `scd2` default to **merge-on-read** on format v2 tables: the affected keys are superseded by an Iceberg equality delete file and the replacement rows are appended, in one atomic snapshot. Rows stream through disk-backed sorts for primary-key deduplication, so memory usage stays constant regardless of increment size. `delete+insert` streams the staged rows into a copy-on-write overwrite whose delete predicate is just the interval bounds, so it is constant-memory as well.

Merge-on-read requires readers that understand Iceberg v2 equality deletes (Spark, Trino, Flink, and DuckDB all do), and read amplification grows until the table is compacted. Set `table.write.merge.mode=copy-on-write` on the destination URI to force copy-on-write snapshots instead.

Some situations fall back to the copy-on-write join automatically because they must read the matched target rows:

- CDC merges (deletes mark `_cdc_deleted` while preserving the row's data),
- targets with destination-only columns (columns removed from the source keep their values on updated rows),
- tables partitioned by a column outside the merge key (equality deletes are partition-routed, so a row that changed partitions would otherwise be missed),
- format v1 tables and `write.merge.mode=copy-on-write`.

These fallback paths materialize the staged rows and the target rows they affect in memory, so their memory usage grows with the increment size.

## Data type handling

ingestr maps source Arrow batches to Iceberg schemas and evolves existing tables by adding new columns and applying safe Iceberg type promotions.

JSON and unknown ingestr values are stored as Iceberg strings.
