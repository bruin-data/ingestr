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

- **CSV** (`.csv`) - Comma-separated values files with headers
- **CSV Headless** - CSV files without headers (use `#csv_headless` suffix)
- **JSON** (`.json`, `.jsonl`) - JSON objects and JSON Lines format
- **Parquet** (`.parquet`) - Apache Parquet columnar format

The file format is automatically inferred from the URL extension. You can also explicitly specify the format using the `#format` suffix in the `--source-table` parameter.

## Usage

### Basic example

```bash
ingestr ingest \
    --source-uri "https://example.com/data.csv" \
    --source-table "data" \
    --dest-uri "duckdb:///local.duckdb" \
    --dest-table "my_table"
```

### Specifying file format explicitly

If the URL doesn't have a recognizable extension (e.g., an API endpoint), you can specify the format using the `#format` suffix in `--source-table`:

```bash
ingestr ingest \
    --source-uri "https://example.com/api/export" \
    --source-table "data#csv" \
    --dest-uri "duckdb:///local.duckdb" \
    --dest-table "my_table"
```

### CSV without headers

For CSV files that don't have a header row, use `#csv_headless`. You can optionally provide column names using the `--columns` flag:

```bash
# With custom column names
ingestr ingest \
    --source-uri "https://example.com/data.csv" \
    --source-table "data#csv_headless" \
    --columns "id:bigint,name:text,value:double" \
    --dest-uri "duckdb:///local.duckdb" \
    --dest-table "my_table"
```

If no column names are provided, columns will be automatically named `unknown_col_0`, `unknown_col_1`, etc.:

```bash
# Without column names (auto-generated)
ingestr ingest \
    --source-uri "https://example.com/data.csv" \
    --source-table "data#csv_headless" \
    --dest-uri "duckdb:///local.duckdb" \
    --dest-table "my_table"
```

### Example with JSON file

```bash
ingestr ingest \
    --source-uri "https://api.example.com/export/data.json" \
    --source-table "data" \
    --dest-uri "snowflake://user:pass@account/database/schema" \
    --dest-table "json_data"
```

### Example with Parquet file

```bash
ingestr ingest \
    --source-uri "https://storage.example.com/data.parquet" \
    --source-table "data" \
    --dest-uri "bigquery://project/dataset" \
    --dest-table "parquet_data"
```

## Supported format suffixes

You can use these suffixes with `--source-table` to explicitly specify the file format:

| Suffix | Format |
|--------|--------|
| `#csv` | CSV with headers |
| `#csv_headless` | CSV without headers |
| `#json` | JSON |
| `#jsonl` | JSON Lines |
| `#parquet` | Parquet |

## Notes

- The `--source-table` parameter is required; use it to specify the format suffix if needed (e.g., `data#csv_headless`)
- The HTTP source downloads the entire file before processing, so ensure you have sufficient memory for large files
- Authentication is not currently supported; only publicly accessible URLs can be used
- The file must be accessible without requiring cookies, headers, or other authentication mechanisms
- For very large files, consider using a dedicated file storage source (e.g., S3, GCS)
- Data is processed in chunks internally:
  - CSV: 10,000 rows per chunk
  - JSON: 1,000 objects per chunk
  - Parquet: 10,000 rows per chunk

## Limitations

- Only supports public URLs (no authentication)
- The entire file is downloaded into memory before processing
- No support for incremental loading
