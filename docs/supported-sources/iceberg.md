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
- `rest_use_ssl` (optional, REST catalog): use HTTPS for the REST catalog connection (default is HTTP). Alias: `catalog_use_ssl`.
- `storage=s3`: use S3 or an S3-compatible object store.
- `bucket`: S3 bucket name. Combined with `prefix` to produce the Iceberg warehouse location.
- `prefix` (optional): path prefix inside the bucket.
- `endpoint` (optional): S3-compatible endpoint such as `localhost:9000`.
- `use_ssl=false` (optional): use plain HTTP for S3-compatible local storage.
- `access_key_id`, `secret_access_key`, `session_token`, `region`: S3 or Glue credentials and region aliases.
- `gcs.keypath` (optional, GCS): path to a Google Cloud service-account JSON key for a `gs://` warehouse. Without it, GCS uses Application Default Credentials.
- `warehouse`: advanced override for the Iceberg warehouse location, such as `s3://bucket/warehouse`.
- `warehouse_path`: local warehouse path alias for non-S3 catalog setups.
- `create_namespace` (optional): create the destination namespace automatically. Defaults to `true`.
- `table_location` (optional): explicit table location template. Supports `{namespace}`, `{namespace_dot}`, `{table}`, `{identifier}`, and `{identifier_dot}`.
- `table_path` (optional): path template appended under `bucket` and `prefix`, for example `{namespace}/{table}`.
- `table.<key>` (optional): table properties passed to Iceberg table creation, for example `table.write.format.default=parquet`.

Advanced Iceberg-Go catalog properties are still accepted and passed through, including the older `iceberg+sql://?uri=...` form.

::: info
For non-AWS S3-compatible stores (MinIO, GCS interop, Cloudflare R2), ingestr handles S3 compatibility automatically. Set `s3.compat-mode=false` to disable it.
:::

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

::: warning Hadoop catalog on object storage needs `allow-unsafe-commits=true`
The Hadoop catalog only commits atomically on a real local or HDFS filesystem. For an object-storage warehouse (`s3://…`, `gs://…`) you must add `allow-unsafe-commits=true`, otherwise ingestr fails to connect.

```bash
ingestr ingest \
  --source-uri 'postgresql://user:pass@localhost:5432/app' \
  --source-table public.orders \
  --dest-uri 'iceberg+hadoop://?storage=s3&warehouse=s3://company-lake/warehouse&allow-unsafe-commits=true&region=eu-west-1' \
  --dest-table analytics.orders \
  --incremental-strategy append
```
:::

### REST catalog with S3 storage

```bash
ingestr ingest \
  --source-uri 'mysql://user:pass@mysql.internal:3306/app' \
  --source-table orders \
  --dest-uri 'iceberg+rest://catalog.internal:8181?storage=s3&bucket=warehouse&prefix=prod&region=us-east-1&rest_use_ssl=true' \
  --dest-table sales.orders \
  --incremental-strategy append
```

::: info
`iceberg+rest://host:port` uses HTTP by default; add `rest_use_ssl=true` for HTTPS. (`use_ssl` is separate — it's for the S3 storage endpoint, not the catalog.)
:::

::: tip A REST catalog server must be running and pre-configured
Unlike the sqlite/postgres catalogs (which are plain databases ingestr writes to directly), `iceberg+rest` talks to a **running REST catalog server** at the URI's host:port — you start and configure it yourself. The server holds its **own warehouse location, storage backend, and credentials**, because it manages the table metadata (and typically writes `metadata.json` to storage itself). The `--dest-uri` still supplies the client's storage credentials for writing the data files.

For example, the reference `apache/iceberg-rest-fixture` is configured through environment variables before you run ingestr:

```bash
docker run -d -p 8181:8181 \
  -e CATALOG_WAREHOUSE=s3://warehouse/ \
  -e CATALOG_IO__IMPL=org.apache.iceberg.aws.s3.S3FileIO \
  -e CATALOG_S3_ENDPOINT=http://minio:9000 \
  -e AWS_ACCESS_KEY_ID=minioadmin -e AWS_SECRET_ACCESS_KEY=minioadmin -e AWS_REGION=us-east-1 \
  apache/iceberg-rest-fixture
```
:::

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
  --dest-uri 'iceberg+hive://localhost:9083?storage=s3&warehouse=s3a://warehouse&endpoint=localhost:9000&use_ssl=false&access_key_id=minioadmin&secret_access_key=minioadmin&region=us-east-1' \
  --dest-table web.clicks \
  --incremental-strategy replace
```

::: warning The metastore needs its own storage configuration
The Hive metastore creates the table directory itself, so it must reach the storage independently — the `--dest-uri` credentials configure only the ingestr client and never reach the metastore. Use the Hadoop scheme in `warehouse` (`s3a://…`, not `s3://`), put the right connector jar on the metastore's classpath (`hadoop-aws` for S3, `gcs-connector` for GCS), and set the properties below in its `core-site.xml`. Without it the metastore fails with `No FileSystem for scheme "s3"` or `S3AFileSystem not found`. A `file://` warehouse needs none of this.
:::

#### Metastore `core-site.xml` per backend

**MinIO / S3-compatible** — warehouse `s3a://bucket`, jar `hadoop-aws`:

| property | value | why |
|----------|-------|-----|
| `fs.s3a.endpoint` | `http://minio:9000` | the S3-compatible endpoint |
| `fs.s3a.access.key` | `minioadmin` | access key |
| `fs.s3a.secret.key` | `minioadmin` | secret key |
| `fs.s3a.path.style.access` | `true` | MinIO uses path-style URLs, not virtual-host |
| `fs.s3a.connection.ssl.enabled` | `false` | endpoint is plain HTTP |

**AWS S3** — warehouse `s3a://bucket`, jar `hadoop-aws` (omit `fs.s3a.endpoint` to use real AWS):

| property | value | why |
|----------|-------|-----|
| `fs.s3a.access.key` | `AKIA…` | access key |
| `fs.s3a.secret.key` | `…` | secret key |
| `fs.s3a.session.token` | `…` | only for temporary (STS) credentials |
| `fs.s3a.aws.credentials.provider` | `org.apache.hadoop.fs.s3a.TemporaryAWSCredentialsProvider` | only when a session token is used |
| `fs.s3a.endpoint.region` | `eu-north-1` | the bucket's region |

**Google Cloud Storage** — warehouse `gs://bucket`, jar `gcs-connector`:

| property | value | why |
|----------|-------|-----|
| `fs.gs.impl` | `com.google.cloud.hadoop.fs.gcs.GoogleHadoopFileSystem` | register the `gs` filesystem |
| `fs.AbstractFileSystem.gs.impl` | `com.google.cloud.hadoop.fs.gcs.GoogleHadoopFS` | AbstractFileSystem binding |
| `fs.gs.auth.service.account.enable` | `true` | authenticate with a service account |
| `fs.gs.auth.service.account.json.keyfile` | `/path/to/sa.json` | the SA key file, mounted into the metastore |

The credentials here are for the **metastore only**; the ingestr client still gets its own storage credentials from the `--dest-uri`.

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

The postgres catalog forwards standard PostgreSQL connection parameters from the URI to the catalog database connection, so you can secure or tune it — e.g. append `&sslmode=require` for a managed database like Neon, RDS, or Cloud SQL:

```plaintext
iceberg+postgres://user:pass@host:5432/iceberg_catalog?storage=s3&bucket=company-lake&sslmode=require&sslrootcert=/path/ca.pem
```

Recognized connection parameters: `sslmode`, `sslcert`, `sslkey`, `sslrootcert`, `sslpassword`, `sslcrl`, `sslcrldir`, `sslsni`, `sslcompression`, `requiressl`, `connect_timeout`, `application_name`, `fallback_application_name`, `target_session_attrs`, `tcp_user_timeout`, `options`, `service`, `servicefile`, `passfile`, `krbsrvname`, and `replication`. Any other query parameter is treated as an Iceberg/storage option, not a database connection setting.

## Table naming

Use an Iceberg table identifier in `--dest-table`, usually `namespace.table`.

For nested namespaces, use dot-separated identifiers such as `lake.analytics.events`.

## Supported write dispositions

Iceberg supports `append`, `replace`, `merge`, `delete+insert`, and `scd2`.

`replace` writes a new Iceberg overwrite snapshot for the destination table. The incremental strategies are implemented natively: each run stages the incoming rows in a temporary Iceberg table and then commits a single atomic snapshot with the changes.

- `merge` upserts by primary key: rows with duplicate primary keys are deduplicated (the highest `--incremental-key` value wins when one is set), existing rows are updated in place, and net-new rows are inserted. CDC streams are merged with delete awareness.
- `delete+insert` deletes the rows whose incremental key falls inside the loaded interval and inserts the staged rows (deduplicated by primary key when one is set).
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
