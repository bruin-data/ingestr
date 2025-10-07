# HTTP

ingestr supports reading CSV, JSON, and Parquet files from public HTTP/HTTPS URLs. This allows you to ingest data from publicly accessible file URLs directly into your databases.

## URI format

The URI format for HTTP sources is as follows:

```plaintext
http://example.com/path/to/file.csv
https://example.com/path/to/file.json
https://example.com/path/to/file.parquet
```

## Supported file formats

The HTTP source supports the following file formats:

- **CSV** (`.csv`) - Comma-separated values files
- **JSON** (`.json`, `.jsonl`) - JSON objects and JSON Lines format
- **Parquet** (`.parquet`) - Apache Parquet columnar format

The file format is automatically inferred from the URL extension. You can also explicitly specify the format using the `file_format` parameter.

## Usage

### Basic example

```bash
ingestr ingest \
    --source-uri "https://example.com/data.csv" \
    --dest-uri "duckdb:///local.duckdb" \
    --dest-table "my_table"
```

### Example with Phantombuster CSV

```bash
ingestr ingest \
    --source-uri "https://phantombuster.s3.amazonaws.com/hNQdB02WKv0/l7xqy7HlU0wyWIvPjqKk5Q/dts_size.csv" \
    --dest-uri "postgres://user:pass@localhost:5432/mydb" \
    --dest-table "phantombuster_data"
```

### Example with JSON file

```bash
ingestr ingest \
    --source-uri "https://api.example.com/export/data.json" \
    --dest-uri "snowflake://user:pass@account/database/schema" \
    --dest-table "json_data"
```

### Example with Parquet file

```bash
ingestr ingest \
    --source-uri "https://storage.example.com/data.parquet" \
    --dest-uri "bigquery://project/dataset" \
    --dest-table "parquet_data"
```

## Parameters

### `file_format`

Optional. Explicitly specify the file format if it cannot be inferred from the URL extension.

**Valid values:** `csv`, `json`, `parquet`

**Example:**

```bash
ingestr ingest \
    --source-uri "https://example.com/data?format=csv" \
    --source-file-format "csv" \
    --dest-uri "duckdb:///local.duckdb" \
    --dest-table "my_table"
```

### `chunksize`

Optional. Number of records to process at once. This helps manage memory usage for large files.

**Default values:**
- CSV: 10,000 rows
- JSON: 1,000 objects
- Parquet: 10,000 rows

**Example:**

```bash
ingestr ingest \
    --source-uri "https://example.com/large-file.csv" \
    --source-chunksize 5000 \
    --dest-uri "postgres://user:pass@localhost:5432/mydb" \
    --dest-table "chunked_data"
```

## Notes

- The HTTP source downloads the entire file before processing, so ensure you have sufficient memory for large files
- Authentication is not currently supported; only publicly accessible URLs can be used
- The file must be accessible without requiring cookies, headers, or other authentication mechanisms
- For very large files, consider using chunked transfer or a dedicated file storage source (e.g., S3, GCS)

## Limitations

- Only supports public URLs (no authentication)
- The entire file is downloaded into memory before processing
- No support for incremental loading
