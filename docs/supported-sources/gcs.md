# Google Cloud Storage

[Google Cloud Storage](https://cloud.google.com/storage?hl=en) is an online file storage web service for storing and accessing data on Google Cloud Platform infrastructure. The service combines the performance and scalability of Google's cloud with advanced security and sharing capabilities. It is an Infrastructure as a Service (IaaS), comparable to Amazon S3.

`ingestr` supports Google Cloud Storage as both a data source and destination.

## URI format

The URI format for Google Cloud Storage is as follows:

```plaintext
gs://?credentials_path=/path/to/service-account.json
```

URI parameters:

- `credentials_path`: path to file containing your Google Cloud [Service Account](https://cloud.google.com/iam/docs/service-account-overview)
- `credentials_base64`: base64-encoded service account JSON (alternative to credentials_path)
- `layout`: Layout template (optional, destination only)

The `--source-table` must be in the format:
```
{bucket name}/{file glob}
```

## Setting up a GCS Integration

To use Google Cloud Storage source in `ingestr`, you will need:
* A Google Cloud Project.
* A Service Account with at least [roles/storage.objectUser](https://cloud.google.com/storage/docs/access-control/iam-roles) IAM permission for reading, or [roles/storage.objectAdmin](https://cloud.google.com/storage/docs/access-control/iam-roles) for writing to GCS.
* A Service Account key file for the corresponding service account.

For more information on how to create a Service Account or its keys, see [Create service accounts](https://cloud.google.com/iam/docs/service-accounts-create) and [Create or delete service account keys](https://cloud.google.com/iam/docs/keys-create-delete) on Google Cloud docs.

## Example: Loading data from GCS

Let's assume that:
* Service account key is available in the current directory, under the filename `service_account.json`. 
* The bucket you want to load data from is called `my-org-bucket`
* The source file is available at `data/latest/dump.csv`
* The data needs to be saved in a DuckDB database called `local.db`
* The destination table name will be `public.latest_dump`

You can run the following command line to achieve this:

```sh
ingestr ingest \
    --source-uri "gs://?credentials_path=$PWD/service_account.json" \
    --source-table "my-org-bucket/data/latest/dump.csv" \
    --dest-uri "duckdb:///local.db" \
    --dest-table "public.latest_dump"
```

## Example: Uploading data to GCS

For this example, we'll assume that:
* `records.db` is a DuckDB database.
* It has a table called `public.users`.
* The service account key is available in the current directory.

The following command demonstrates how to copy data from a local DuckDB database to GCS:
```sh
ingestr ingest \
    --source-uri 'duckdb:///records.db' \
    --source-table 'public.users' \
    --dest-uri "gs://?credentials_path=$PWD/service_account.json" \
    --dest-table 'my-org-bucket/records'
```

This will result in a file structure like the following:
```
my-org-bucket/
└── records
    ├── _dlt_loads
    ├── _dlt_pipeline_state
    ├── _dlt_version
    └── users
        └── <load_id>.<file_id>.parquet
```

The value of `load_id` and `file_id` is determined at runtime. The default layout creates a folder with the same table name as the source and places the data inside a parquet file. This layout is configurable using the `layout` parameter.

For example, if you would like to create a parquet file with the same name as the source table (as opposed to a folder) you can set `layout` to `{table_name}.{ext}` in the command line above:

```sh
ingestr ingest \
    --source-uri 'duckdb:///records.db' \
    --source-table 'public.users' \
    --dest-uri "gs://?layout={table_name}.{ext}&credentials_path=$PWD/service_account.json" \
    --dest-table 'my-org-bucket/records'
```

Result:
```
my-org-bucket/
└── records
    ├── _dlt_loads
    ├── _dlt_pipeline_state
    ├── _dlt_version
    └── users.parquet
```

List of available Layout variables is available [here](https://dlthub.com/docs/dlt-ecosystem/destinations/filesystem#available-layout-placeholders)

## Supported File Formats
`gs` source only supports loading files in the following formats:
* `csv`: Comma Separated Values 
* `parquet`: [Apache Parquet](https://parquet.apache.org/) storage format.
* `jsonl`: Line delimited JSON. see [https://jsonlines.org/](https://jsonlines.org/)

::: info NOTE
When writing to GCS, only `parquet` is supported.
:::
## File Pattern
`ingestr` supports [glob](https://en.wikipedia.org/wiki/Glob_(programming)) like pattern matching for `gs` source.
This allows for a powerful pattern matching mechanism that allows you to specify multiple files in a single `--source-table`.

Below are some examples of path patterns, each path pattern is glob you can specify after the bucket name:

- `**/*.csv`: Retrieves all the CSV files, regardless of how deep they are within the folder structure.
- `*.csv`: Retrieves all the CSV files from the first level of a folder.
- `myFolder/**/*.jsonl`: Retrieves all the JSONL files from anywhere under `myFolder`.
- `myFolder/mySubFolder/users.parquet`: Retrieves the `users.parquet` file from `mySubFolder`.
- `employees.jsonl`: Retrieves the `employees.jsonl` file from the root level of the bucket.

### Working with compressed files

`ingestr` automatically detects and handles gzipped files in your GCS bucket. You can load data from compressed files with the `.gz` extension without any additional configuration.

For example, to load data from a gzipped CSV file:

```sh
ingestr ingest \
    --source-uri "gs://?credentials_path=$PWD/service_account.json" \
    --source-table "my-org-bucket/logs/event-data.csv.gz" \
    --dest-uri "duckdb:///compressed_data.duckdb" \
    --dest-table "logs.events"
```

You can also use glob patterns to load multiple compressed files:

```sh
ingestr ingest \
    --source-uri "gs://?credentials_path=$PWD/service_account.json" \
    --source-table "my-org-bucket/logs/**/*.csv.gz" \
    --dest-uri "duckdb:///compressed_data.duckdb" \
    --dest-table "logs.events"
```

### File type hinting

If your files are properly encoded but lack the correct file extension (CSV, JSONL, or Parquet), you can provide a file type hint to inform `ingestr` about the format of the files. This is done by appending a fragment identifier (`#format`) to the end of the path in your `--source-table` parameter.

For example, if you have JSONL-formatted log files stored in GCS with a non-standard extension:

```
--source-table "my-org-bucket/logs/event-data#jsonl"
```

This tells `ingestr` to process the files as JSONL, regardless of their actual extension.

Supported format hints include:
- `#csv` - For comma-separated values files
- `#jsonl` - For line-delimited JSON files
- `#parquet` - For Parquet format files

::: tip
File type hinting works with `gzip` compressed files as well.
:::
