# DuckDB

DuckDB is an in-memory database designed to be fast and reliable.

ingestr supports DuckDB as both a source and a destination, and also supports reading from / writing to **DuckLake** lakehouse tables backed by DuckDB.

## URI format
The URI format for DuckDB is as follows:

```plaintext
duckdb:///<database-file>
```

URI parameters:
- `database-file`: the path to the DuckDB database file

The same URI structure can be used both for sources and destinations.

---

## DuckLake

[DuckLake](https://ducklake.select/) is a lakehouse table format developed by the DuckDB team. Data is stored as Parquet files in object storage (S3 / GCS / S3-compatible); table metadata (schemas, snapshots, file lists) lives in a regular SQL database (DuckDB, SQLite, or Postgres).

ingestr can read from and write to DuckLake tables using the `ducklake://` URI scheme. The same URI shape works for both source and destination:

```bash
# Read from a DuckLake source
ingestr ingest \
  --source-uri="ducklake://?catalog_type=...&storage_type=..." \
  --source-table="schema.table" \
  --dest-uri="postgres://..." \
  --dest-table="schema.table"

# Write to a DuckLake destination
ingestr ingest \
  --source-uri="mysql://..." \
  --source-table="schema.table" \
  --dest-uri="ducklake://?catalog_type=...&storage_type=..." \
  --dest-table="schema.table"
```

### URI format

```plaintext
ducklake://?
  catalog_type=<duckdb|sqlite|postgres>
  &catalog_path=<file-path>                       # duckdb / sqlite catalogs
  &catalog_host=<host>                            # postgres catalog
  &catalog_port=<port>                            # postgres, optional (default: 5432)
  &catalog_database=<db>                          # postgres catalog
  &catalog_username=<user>                        # postgres catalog
  &catalog_password=<pass>                        # postgres catalog

  &storage_type=<s3|gcs>
  &storage_path=<s3://bucket/prefix | gs://bucket/prefix>
  &storage_region=<region>                        # optional
  &storage_endpoint=<endpoint>                    # S3-compatible (MinIO, R2, B2, Tigris)
  &storage_url_style=<path|vhost>                 # path required for most S3-compatible
  &storage_use_ssl=<true|false>                   # optional, default true
  &storage_access_key=<key>
  &storage_secret_key=<secret>
  &storage_session_token=<token>                  # optional, AWS STS
```

All values must be URL-encoded if they contain `&`, `=`, `/`, `?` or other reserved characters.

### Catalog options

| Catalog | When to use |
|---|---|
| `duckdb` | Local development, single-user. Catalog is a `.duckdb` file. |
| `sqlite` | Same shape as `duckdb` but the metadata file is universally readable. |
| `postgres` | Multi-user / production. Catalog lives in a Postgres database. |

> [!NOTE]
> If you are using `duckdb` as your catalog type, you're limited to a single client. Switch to `sqlite` or `postgres` if multiple processes need to read/write the lake concurrently.

#### DuckDB

```plaintext
catalog_type=duckdb
catalog_path=/data/metadata.duckdb
```

#### SQLite

```plaintext
catalog_type=sqlite
catalog_path=/data/metadata.sqlite
```

#### Postgres

```plaintext
catalog_type=postgres
catalog_host=metastore.internal
catalog_port=5432                     # optional, defaults to 5432
catalog_database=ducklake_meta
catalog_username=lake_user
catalog_password=lake_password
```

### Storage options

#### AWS S3

```plaintext
storage_type=s3
storage_path=s3://my-ducklake-bucket/lake
storage_region=us-east-1              # optional, DuckDB defaults to us-east-1
storage_access_key=AKIA...
storage_secret_key=...
storage_session_token=...             # optional, for AWS STS temporary credentials
```

#### S3-compatible (MinIO, R2, B2, Tigris, on-prem)

| Field | Required | Default | Description |
|---|---|---|---|
| `storage_endpoint` | yes | (AWS S3) | Endpoint host:port — `minio.local:9000`, `<account>.r2.cloudflarestorage.com`, `fly.storage.tigris.dev`, etc. |
| `storage_url_style` | yes | `vhost` | `path` is required for MinIO and most non-AWS S3-compatible backends. |
| `storage_use_ssl` | no | `true` | Set to `false` for plain-HTTP local dev (MinIO without TLS). |

#### GCS

```plaintext
storage_type=gcs
storage_path=gs://my-bucket/lake
storage_access_key=GOOG...
storage_secret_key=...
```

GCS uses S3 interoperability (HMAC) credentials, not OAuth. Create HMAC keys in the [GCP Console → Storage → Settings → Interoperability](https://console.cloud.google.com/storage/settings;tab=interoperability).

---

### Examples

#### MinIO + DuckDB catalog (local development)

```bash
ingestr ingest \
  --source-uri="mysql://user:pass@host/db" \
  --source-table="public.orders" \
  --dest-uri="ducklake://?catalog_type=duckdb&catalog_path=/data/metadata.duckdb&storage_type=s3&storage_path=s3://ducklake/warehouse&storage_endpoint=minio.local:9000&storage_url_style=path&storage_use_ssl=false&storage_access_key=minioadmin&storage_secret_key=minioadmin" \
  --dest-table="public.orders"
```

#### Postgres catalog + AWS S3 (production)

```bash
ingestr ingest \
  --source-uri="postgres://app-db:5432/prod" \
  --source-table="public.events" \
  --dest-uri="ducklake://?catalog_type=postgres&catalog_host=metastore.prod&catalog_database=ducklake_meta&catalog_username=lake_user&catalog_password=${LAKE_PASSWORD}&storage_type=s3&storage_path=s3://my-bucket/lake&storage_access_key=${AWS_ACCESS_KEY_ID}&storage_secret_key=${AWS_SECRET_ACCESS_KEY}" \
  --dest-table="public.events"
```

#### Reading from DuckLake to Postgres

```bash
ingestr ingest \
  --source-uri="ducklake://?catalog_type=postgres&catalog_host=metastore.prod&catalog_database=ducklake_meta&catalog_username=lake_user&catalog_password=${LAKE_PASSWORD}&storage_type=s3&storage_path=s3://my-bucket/lake&storage_access_key=${AWS_ACCESS_KEY_ID}&storage_secret_key=${AWS_SECRET_ACCESS_KEY}" \
  --source-table="analytics.daily_revenue" \
  --dest-uri="postgres://reporting-db:5432/reports" \
  --dest-table="public.daily_revenue"
```

### Required vs optional fields

| Field | Required | Notes |
|---|---|---|
| `catalog_type` | yes | One of `duckdb`, `sqlite`, `postgres` |
| `catalog_path` | yes (duckdb / sqlite) | — |
| `catalog_host` | yes (postgres) | — |
| `catalog_database` | yes (postgres) | — |
| `catalog_username` | yes (postgres) | — |
| `catalog_password` | yes (postgres) | — |
| `catalog_port` | no | Defaults to `5432` |
| `storage_type` | yes | One of `s3`, `gcs` |
| `storage_path` | yes | Bucket/path the lake writes to |
| `storage_access_key` | yes | — |
| `storage_secret_key` | yes | — |
| `storage_endpoint` | yes for S3-compatible | Omit for real AWS S3 |
| `storage_url_style` | yes for S3-compatible | `path` for MinIO, R2, B2, etc. |
| `storage_use_ssl` | no | Set `false` for plain-HTTP local dev |
| `storage_region` | no | DuckDB defaults to `us-east-1`; use `auto` for Cloudflare R2 |
| `storage_session_token` | no | AWS STS temporary credentials |

Invalid or incomplete URIs are rejected at parse time with a clear error message — no subprocess is spawned without a complete configuration.
