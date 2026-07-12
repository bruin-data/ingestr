# Apache Iceberg

[Apache Iceberg](https://iceberg.apache.org/) is an open table format for large analytic datasets.

ingestr supports Iceberg as a destination.

## URI format

The Iceberg destination uses the catalog backend in the URI scheme:

```plaintext
iceberg+<catalog-backend>://<catalog-location>?storage=<storage-backend>&...
```

Supported catalog backends:

- `iceberg+sqlite`
- `iceberg+postgres`
- `iceberg+rest`
- `iceberg+nessie` (Iceberg REST endpoint, defaults to `/iceberg`)
- `iceberg+polaris` (Apache Polaris or Snowflake Open Catalog, defaults to HTTPS and `/api/catalog`; put a different endpoint path in the URI when required)
- `iceberg+s3tables` (Amazon S3 Tables REST catalog with SigV4)
- `iceberg+glue`
- `iceberg+hadoop`
- `iceberg+hive`
- `iceberg+sql` for advanced pass-through SQL catalog options
- `iceberg` with `catalog=<catalog-type>` or `type=<catalog-type>`

Catalog metadata and table file storage are configured independently where the catalog permits it. Table files can use:

- a local filesystem, commonly with the Hadoop or SQL catalog;
- Amazon S3 or an S3-compatible service such as MinIO with `storage=s3`;
- Google Cloud Storage with `storage=gcs`;
- Azure Data Lake Storage with `storage=azure` or `storage=adls`.

Amazon S3 Tables is a managed catalog and storage combination: it uses the S3 Tables bucket ARN as `warehouse`, signs REST requests with the `s3tables` SigV4 service, and requires exactly one lowercase namespace plus a lowercase table and column names.

ingestr rejects explicit `table.write.data.path` and `table.write.metadata.path` values for every catalog. External file roots cannot be proven isolated across tables: orphan cleanup or a physical table purge could delete another table's files, while failed writes outside the table root could leak permanently. Existing tables that already carry either property are refused for writes, orphan cleanup, and physical purge until an administrator safely relocates the files and removes the property. Use a namespace-qualified `table_location` or `table_path` template to place each table's complete data and metadata under its own root.

Common URI parameters:

- `catalog_name` (optional): non-empty logical catalog name used by the Iceberg client. It also isolates entries that share one SQL catalog database. When omitted, SQL-backed catalogs retain the legacy logical name `sql`; all other backends use `ingestr`.
- `storage=s3`, `storage=gcs`, or `storage=azure`: use S3-compatible storage, Google Cloud Storage, or Azure Data Lake Storage. `storage=adls` is an alias for Azure.
- `bucket`: S3/GCS bucket name. For Azure, use `container` (with `bucket` accepted as an alias).
- `prefix` (optional): path prefix inside the bucket.
- `endpoint` (optional): object-store or emulator endpoint such as `localhost:9000`.
- `use_ssl=false` (optional): use plain HTTP for a local S3, GCS, or Azure emulator. A conflicting explicit endpoint scheme is rejected.
- `access_key_id`, `secret_access_key`, `session_token`, `region`: S3 or Glue credentials and region aliases.
- `gcs_json_key`, `gcs_key_path`, `gcs_credential_type`, `gcs_use_json_api`: GCS credential and API options. If no GCS credential is supplied, Application Default Credentials are used.
- `account_name`, `account_key`: Azure account and shared-key credentials. Azure also supports `sas_token`, `connection_string`, `managed_identity`, and `client_id`.
- `azure_host`, `adls_scheme`: advanced Azure location overrides for sovereign clouds or `abfs`/`wasb` URI variants.
- `warehouse`: advanced override for the Iceberg warehouse location, such as `s3://bucket/warehouse`.
- `warehouse_path`: local warehouse path alias for non-S3 catalog setups.
- `create_namespace` (optional): create the destination namespace automatically. Defaults to `true`.
- `check_namespace` (optional): existing namespace used by `ingestr check` when `create_namespace=false`. Dot-separated nested namespaces are supported.
- `table_location` (optional): explicit table location template. Supports `{namespace}`, `{namespace_dot}`, `{table}`, `{identifier}`, and `{identifier_dot}`. Writable destinations require a namespace-qualified per-table root: use `{identifier}`, `{identifier_dot}`, or a namespace placeholder together with `{table}`.
- `table_path` (optional): path template appended under `bucket` and `prefix`, for example `{namespace}/{table}`.
- `partition_spec` (optional): Iceberg partition expression, for example `customer_id,day(created_at),bucket[16](tenant_id)`.
- `table.<key>` (optional): table properties applied to Iceberg tables, for example `table.write.format.default=parquet`.

REST catalog presets also accept `catalog_prefix`, `oauth_token`, or the pair `oauth_client_id` and `oauth_client_secret`. Nessie accepts `nessie_branch` and `nessie_warehouse`, which are encoded in its Iceberg REST endpoint path; use these instead of `catalog_prefix` or `warehouse`. Nessie itself must be configured with the named warehouse and storage location; any client-side storage parameters only configure file access. Polaris requires its catalog name in `warehouse` and optionally accepts `polaris_realm`. Amazon S3 Tables requires `region` and an S3 Tables bucket ARN in `warehouse`; ingestr configures the `s3tables` SigV4 service automatically.

Advanced Iceberg-Go catalog properties are still accepted and passed through, including the older `iceberg+sql://?uri=...` form.

The backend encoded in an `iceberg+<catalog-backend>` scheme cannot be overridden with a conflicting `catalog` or `type` query parameter. Use the generic `iceberg` scheme when selecting the backend entirely through `catalog` or `type`.

### Mutable table properties

`table.<key>` parameters are not limited to table creation. During preparation of an existing table, ingestr commits any configured property whose value differs from the table's current value. Only properties present in the destination URI are added or updated; omitting a previously configured key does not remove it.

For append and incremental runs, property reconciliation is a separate metadata commit during table preparation. For `replace`, configured properties, the replacement schema, partition spec, commit-token reset, and replacement files are staged together, so a failed extract or file write leaves the previous rows and metadata intact. Properties managed internally by ingestr, including staging ownership, expiration, CDC state, and the commit-token ledger, cannot be overridden through the URI.

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

### REST catalog with GCS storage

```bash
ingestr ingest \
  --source-uri 'postgresql://user:pass@localhost:5432/app' \
  --source-table public.orders \
  --dest-uri 'iceberg+rest://catalog.internal:8181?catalog_use_ssl=true&storage=gcs&bucket=company-lake&prefix=iceberg&gcs_key_path=/var/run/secrets/gcp.json' \
  --dest-table analytics.orders \
  --incremental-strategy replace
```

### REST catalog with Azure Data Lake Storage

```bash
ingestr ingest \
  --source-uri 'postgresql://user:pass@localhost:5432/app' \
  --source-table public.orders \
  --dest-uri 'iceberg+rest://catalog.internal:8181?catalog_use_ssl=true&storage=azure&container=warehouse&account_name=companylake&prefix=iceberg&managed_identity=true' \
  --dest-table analytics.orders \
  --incremental-strategy replace
```

### Nessie

```bash
ingestr ingest \
  --source-uri 'postgresql://user:pass@localhost:5432/app' \
  --source-table public.orders \
  --dest-uri 'iceberg+nessie://nessie.internal:19120?nessie_branch=experiments&nessie_warehouse=sales' \
  --dest-table analytics.orders \
  --incremental-strategy merge \
  --primary-key id
```

### Apache Polaris or Snowflake Open Catalog

```bash
ingestr ingest \
  --source-uri 'postgresql://user:pass@localhost:5432/app' \
  --source-table public.orders \
  --dest-uri 'iceberg+polaris://catalog.example.com?warehouse=production&oauth_client_id=client-id&oauth_client_secret=client-secret&scope=PRINCIPAL_ROLE:ALL&polaris_realm=POLARIS' \
  --dest-table analytics.orders \
  --incremental-strategy replace
```

### Amazon S3 Tables

```bash
ingestr ingest \
  --source-uri 'postgresql://user:pass@localhost:5432/app' \
  --source-table public.orders \
  --dest-uri 'iceberg+s3tables://?region=us-east-1&warehouse=arn%3Aaws%3As3tables%3Aus-east-1%3A123456789012%3Abucket%2Fanalytics' \
  --dest-table analytics.orders \
  --incremental-strategy replace
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

## End-to-end connection check

Use `ingestr check` to verify the catalog and table storage together before starting an ingestion:

```bash
ingestr check \
  --dest-uri 'iceberg+rest://catalog.internal:8181?storage=s3&bucket=warehouse&prefix=prod&region=us-east-1' \
  --timeout 2m
```

This is an active read/write check, not a catalog ping. It creates a temporary managed table, writes `id=1`, reloads and scans the row through Iceberg file IO, physically purges the table, and verifies cleanup. With `create_namespace=true`, it also creates and removes a temporary namespace. With `create_namespace=false`, set `check_namespace` to the exact existing namespace in which the temporary table may be created; ingestr never chooses an arbitrary namespace.

For example, to check access without granting namespace creation:

```bash
ingestr check \
  --dest-uri 'iceberg+rest://catalog.internal:8181?warehouse=production&create_namespace=false&check_namespace=analytics'
```

The credentials therefore need catalog and object-store permissions to create, write, read, list, and physically purge a table, plus namespace create/drop permissions when automatic namespace creation is enabled. The check uses its own unpartitioned `id` schema; destination `partition_spec` settings do not alter the temporary table. The timeout defaults to two minutes; cleanup gets a separate bounded attempt even when the check fails or is canceled.

## Table naming

Use an Iceberg table identifier in `--dest-table`, usually `namespace.table`.

For nested namespaces, use dot-separated identifiers such as `lake.analytics.events`.

## Partitioning and clustering

`--partition-by` accepts a comma-separated Iceberg partition expression. A bare column uses the identity transform; transformed fields use Iceberg syntax such as `year(created_at)`, `month(created_at)`, `day(created_at)`, `hour(created_at)`, `bucket[16](customer_id)`, or `truncate[8](postal_code)`:

```bash
ingestr ingest \
  ... \
  --partition-by 'day(created_at),bucket[16](tenant_id)' \
  --cluster-by 'tenant_id,created_at'
```

The URI parameter `partition_spec` accepts the same expression and takes precedence over `--partition-by`. A non-empty partition expression on an existing table evolves its current Iceberg partition spec; files written under older specs remain readable and are not rewritten automatically.

`--cluster-by` accepts comma-separated columns. ingestr records them as the table's default Iceberg sort order using ascending identity transforms with nulls first, and physically sorts incoming rows with a disk-backed external sort. Compatible identity sort orders are inherited on later runs. Clustering keeps memory bounded but needs temporary local disk and uses a single ordered file-writing stream after the sort, so it can reduce write parallelism.

## Supported write dispositions

Iceberg supports `append`, `replace`, `merge`, `delete+insert`, `truncate+insert`, and `scd2`.

`replace` writes a new Iceberg overwrite snapshot for the destination table. `replace`, `merge`, `delete+insert`, `truncate+insert`, and `scd2` publish each table's changes in one atomic Iceberg snapshot. Iceberg's `truncate+insert` implementation preserves the existing layout while committing supported schema evolution and replacement files together, so a failed reload leaves both the previous schema and rows unchanged.

- `merge` upserts by primary key: rows with duplicate primary keys are deduplicated (the highest `--incremental-key` value wins when one is set), existing rows are updated in place, and net-new rows are inserted. CDC streams are merged with delete awareness.
- `delete+insert` deletes the rows whose incremental key falls inside the loaded interval and inserts the staged rows (deduplicated by primary key when one is set).
- `truncate+insert` evolves the table schema in place, keeps its partition spec, sort order, properties, and history, and atomically replaces its rows.
- `scd2` maintains slowly-changing-dimension history with `_scd_valid_from`, `_scd_valid_to`, and `_scd_is_current` columns.

### Append retry semantics

Tokenless `append` runs use ordinary append semantics: submitting the same rows twice appends them twice. ingestr does not infer retries from row content because two intentionally identical increments are indistinguishable from an accidental retry.

When a source supplies a durable commit token, ingestr stores its identifier in both snapshot metadata and a bounded table-property ledger. Retrying an already committed token is drained without creating another snapshot. Exact retry protection therefore depends on a stable source token; without one, orchestrators should avoid retrying an append whose commit outcome is unknown or deduplicate the output downstream.

### Streaming brokers

In `--stream` mode, broker sources default to `merge` when `--incremental-strategy` is omitted. Their fixed streaming envelope contains a source-derived `msg_id`, JSON `data`, and `_ingestr_order`; merging on `msg_id` makes normal at-least-once redelivery idempotent at the destination row level.

Explicitly selecting `--incremental-strategy append` disables that primary-key merge and is intentionally at-least-once. ingestr writes a flush durably before acknowledging its broker position, but a crash, rebalance, or retry with different flush boundaries can still append a redelivered message more than once. Broker flushes carry source commit tokens, which protect retries of the same flush boundary but cannot equate differently bounded flushes. Keep the default `merge` for destination-level redelivery deduplication, or deduplicate explicit append output downstream by `msg_id`.

### Merge modes and memory usage

`merge` and `scd2` default to **merge-on-read** on format v2 tables: the affected keys are superseded by an Iceberg equality delete file and the replacement rows are appended, in one atomic snapshot. Rows stream through disk-backed sorts for primary-key deduplication, so memory usage stays constant regardless of increment size. `delete+insert` streams the staged rows into a copy-on-write overwrite whose delete predicate is just the interval bounds, so it is constant-memory as well.

Merge-on-read requires readers that understand Iceberg v2 equality deletes (Spark, Trino, Flink, and DuckDB all do), and read amplification grows until the table is compacted. Set `table.write.merge.mode=copy-on-write` on the destination URI to force copy-on-write snapshots instead.

Some situations fall back to the copy-on-write join automatically because they must read the matched target rows:

- CDC merges (deletes mark `_cdc_deleted` while preserving the row's data),
- targets with destination-only columns (columns removed from the source keep their values on updated rows),
- tables partitioned by a column outside the merge key (equality deletes are partition-routed, so a row that changed partitions would otherwise be missed),
- format v1 tables and `write.merge.mode=copy-on-write`.

These fallback paths use disk-backed primary-key sorts and a streaming merge join, then atomically replace the table files. Memory is bounded by the spill configuration rather than the increment size, at the cost of scanning the current target and using temporary local disk proportional to the staged and target data.

PostgreSQL initial snapshots in `--stream` mode are written to a managed hidden Iceberg table. Snapshot pages and schema changes remain invisible until the final durable source position is available; ingestr then publishes the completed rows, supported schema evolution, commit token, and CDC cursor in one target-table commit. A crash before publication leaves the previous target unchanged and the next snapshot reset safely rebuilds the hidden table.

## Lifecycle and maintenance

Iceberg table drops are physical purges. Before dropping a table, ingestr enables `gc.enabled=true` when necessary and requires the catalog's purge operation to remove the catalog entry and the table's data and metadata files. It never falls back to a catalog-only drop when purge is unsupported or its result is unknown. Amazon S3 Tables owns both the catalog and storage, so ingestr uses its server-managed purge operation directly and does not require a client filesystem journal for the bucket ARN. A durable catalog ownership table records the original UUID and blocks ingestr recreation until an uncertain S3 Tables purge is reconciled; immediately before purge, ingestr renews that fence and rejects a changed UUID. External catalog writers must honor the same ownership table because the S3 Tables purge API itself has no conditional UUID parameter. Other catalogs persist a validated cleanup journal outside the table root before dropping the catalog entry. Physical deletion is attempted only after catalog absence is confirmed, is idempotently resumed after interruption, and the journal is removed only after the table root is empty. UUID and location checks prevent a stale journal from deleting a live or reused table identifier.

Any temporary staging table created for `replace`, `merge`, `delete+insert`, `truncate+insert`, streaming, or `scd2` is a managed Iceberg table with a persisted 24-hour expiration deadline. Long writes heartbeat that lease while materializing source data, then reserve a fresh lease window for file writing and commit. After successful writes, ingestr scans each namespace at most once per hour for expired managed tables and physically purges only tables carrying its ownership and expiration markers. Cleanup claims an expired table with a conditional metadata commit immediately before purge; a concurrent lease refresh wins the conflict and cancels deletion. The table involved in the active write is also excluded from its own process's scan.

Regular destination tables are not maintained automatically by default. Enable maintenance with the following URI table property:

```plaintext
table.ingestr.maintenance.enabled=true
```

Once enabled, maintenance runs at most hourly after a successful data commit and uses these conservative defaults:

- compact undersized data files and apply position/equality delete files atomically on unsorted tables;
- expire snapshots older than 7 days while retaining at least 3 snapshots;
- delete unreferenced data, delete, manifest, and old metadata files only after they are 72 hours old;
- enable Iceberg manifest merging for future commits;
- retain 100 previous metadata versions.

Snapshot expiration first commits metadata without deleting any files. Orphan cleanup runs only after that commit is confirmed, so an unknown catalog commit result cannot make ingestr delete files that may still be live. The orphan age cannot be configured below 24 hours; this protects in-flight writers, readers, and commits with delayed or unknown status.

Maintenance is a post-commit hook, not a background scheduler: a table with no successful writes does not run configured maintenance. The hook is synchronous with the ingest command but is best-effort after the data is already durable; a maintenance failure is logged in debug output and does not roll back or report failure for the successful data commit. Enabled maintenance gets a 30-minute deadline.

Data-file compaction preserves ingestr's ascending identity sort orders by externally sorting each partition group and tagging every replacement file with the table's current sort-order ID. Sort orders using transforms, descending direction, or a different null order fail explicitly instead of producing incorrectly tagged files. Snapshot expiration, orphan cleanup, and maintenance property updates remain available for those tables.

An explicit `table_location` or `table_path` must give every table a namespace-qualified physical root. Use `{identifier}`, `{identifier_dot}`, or both `{namespace}` (or `{namespace_dot}`) and `{table}`. A template containing only `{table}` is rejected because equal table names in different namespaces would share a root. Orphan cleanup also checks the catalog for overlapping table locations at runtime. Managed staging tables are not compacted because they are short-lived and will be physically purged.

All maintenance settings are supplied as `table.<key>` URI parameters:

| Key after `table.` | Default when enabled | Description |
| --- | ---: | --- |
| `ingestr.maintenance.interval-ms` | `3600000` | Minimum delay between maintenance runs. |
| `ingestr.maintenance.compact-data-files` | `true` | Compact data files and safely removable delete files. |
| `ingestr.maintenance.target-file-size-bytes` | table write target | Target compacted file size. |
| `ingestr.maintenance.min-input-files` | `5` | Minimum input files in a compaction group. Use `1` to compact a single data file with accumulated deletes. |
| `ingestr.maintenance.delete-file-threshold` | `5` | Delete files that make a data file eligible for compaction. |
| `ingestr.maintenance.expire-snapshots` | `true` | Enable snapshot expiration. |
| `ingestr.maintenance.snapshot-max-age-ms` | `604800000` | Maximum snapshot age (7 days). |
| `ingestr.maintenance.min-snapshots-to-keep` | `3` | Minimum retained snapshots. |
| `ingestr.maintenance.delete-orphans` | `true` | Delete grace-aged files not referenced by confirmed metadata. |
| `ingestr.maintenance.orphan-file-age-ms` | `259200000` | Orphan grace period (72 hours, minimum 24 hours). |
| `ingestr.maintenance.manifest-merge` | `true` | Enable Iceberg manifest merging. |
| `ingestr.maintenance.manifest-min-count` | `100` | Manifest count that triggers merging. |
| `ingestr.maintenance.manifest-target-size-bytes` | `8388608` | Target merged manifest size (8 MiB). |
| `ingestr.maintenance.previous-metadata-versions` | `100` | Previous metadata versions retained before grace-aged cleanup. |

For example, this enables hourly maintenance, retains seven daily snapshots, and compacts tables once at least ten small files can be grouped:

```plaintext
iceberg+hadoop:///tmp/iceberg-warehouse?table.ingestr.maintenance.enabled=true&table.ingestr.maintenance.snapshot-max-age-ms=604800000&table.ingestr.maintenance.min-snapshots-to-keep=7&table.ingestr.maintenance.min-input-files=10
```

## Data type handling

ingestr maps source Arrow batches to Iceberg schemas and evolves existing tables by adding new columns and applying safe Iceberg type promotions.

JSON and unknown ingestr values are stored as Iceberg strings.

Iceberg lists, nested lists, structs, maps, fixed-width binary, and required or optional collection elements are represented recursively and retain their nullability. These types participate in Arrow writes, reads, spill sorting, copy-on-write merges, and atomic reloads. Iceberg variants and nanosecond timestamp types remain unsupported: variants have no corresponding ingestr logical type, while nanosecond timestamps are rejected rather than truncated to ingestr's microsecond convention.

Columns removed from the source use soft removal: the Iceberg field and its historical values remain, and a required field is changed to optional so future rows can contain `NULL`. Identifier fields cannot be soft-removed. Adding a required field to an existing table without an Iceberg initial default is rejected; make the field nullable or use `replace` so every row is rewritten.

The names `_file`, `_pos`, `_deleted`, `_spec_id`, `_partition`, `_row_id`, and `_last_updated_sequence_number` are reserved Iceberg metadata names. ingestr rejects them case-insensitively before creating or mutating a table. It does not automatically rename them because a destination-only rename cannot be mapped back to source columns reliably.
