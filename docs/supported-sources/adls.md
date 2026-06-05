# Azure Data Lake Storage Gen2

[Azure Data Lake Storage Gen2](https://learn.microsoft.com/en-us/azure/storage/blobs/data-lake-storage-introduction) is Azure Blob Storage with hierarchical namespace capabilities enabled for data lake workloads.

`ingestr` supports Azure Data Lake Storage Gen2 as both a source and destination.

## URI format

The URI for connecting to Azure Data Lake Storage Gen2 is structured as follows:

```plaintext
adls://?account_name=<storage-account>
```

The `adls`, `adlsgen2`, `azdatalake`, `abfs`, and `abfss` URI schemes are accepted.

URI parameters:

- `account_name`: Azure storage account name.
- `tenant_id`: Microsoft Entra tenant ID for service principal authentication.
- `client_id`: Service principal client ID.
- `client_secret`: Service principal client secret. URL-encode the value if it contains special characters.
- `account_key`: Azure storage account key.
- `sas_token`: Shared Access Signature token. URL-encode the token if it contains `&`.
- `layout`: Destination-only layout template.

For production, prefer Microsoft Entra service principal authentication. Grant the service principal an Azure RBAC role on the storage account or file system, such as `Storage Blob Data Reader` for source reads or `Storage Blob Data Contributor` for destination writes.

You can pass service principal credentials in the URI, or omit them from the URI and use Azure's default credential chain:

```sh
export AZURE_TENANT_ID='<tenant-id>'
export AZURE_CLIENT_ID='<client-id>'
export AZURE_CLIENT_SECRET='<client-secret>'
```

If you need to pass service principal credentials directly in the URI, use:

```plaintext
adls://?account_name=<storage-account>&tenant_id=<tenant-id>&client_id=<client-id>&client_secret=<client-secret>
```

When `tenant_id`, `client_id`, `client_secret`, `account_key`, and `sas_token` are omitted, `ingestr` uses `DefaultAzureCredential`. This supports environment variables, managed identity, Azure CLI login, and other credentials supported by the Azure SDK.

`account_key` and `sas_token` are supported for compatibility and demos, but they are not the recommended production authentication method.

The `--source-table` or `--dest-table` parameter specifies the ADLS Gen2 file system and path:

```plaintext
<file-system>/<path>
```

For sources, the path can be a single file or a glob pattern. Add a format hint when the file extension is not enough to detect the format:

```plaintext
<file-system>/<path-or-glob>#csv
<file-system>/<path-or-glob>#jsonl
<file-system>/<path-or-glob>#parquet
```

For destinations, the file system must already exist. `ingestr` creates any missing directories under the file system and writes parquet files to the selected path.

## Example: Reading data from ADLS Gen2

For this example, assume that:

- The ADLS Gen2 storage account is `myaccount`.
- The source file system is `lakehouse`.
- CSV files are stored under `exports/users/`.

```sh
ingestr ingest \
    --source-uri 'adls://?account_name=myaccount' \
    --source-table 'lakehouse/exports/users/*.csv' \
    --dest-uri 'duckdb:///records.db' \
    --dest-table 'public.users'
```

This reads matching CSV files from ADLS Gen2 and writes the rows into DuckDB.

## Example: Uploading data to ADLS Gen2

For this example, assume that:

- `records.db` is a DuckDB database.
- It has a table called `public.users`.
- The ADLS Gen2 storage account is `myaccount`.
- The destination file system is `lakehouse`.

```sh
ingestr ingest \
    --source-uri 'duckdb:///records.db' \
    --source-table 'public.users' \
    --dest-uri 'adls://?account_name=myaccount' \
    --dest-table 'lakehouse/records'
```

This writes parquet output under:

```plaintext
lakehouse/
`-- records
    `-- <load_id>.<file_id>.parquet
```

The default layout writes parquet files directly under the selected path. You can customize this with the `layout` parameter:

```sh
ingestr ingest \
    --source-uri 'duckdb:///records.db' \
    --source-table 'public.users' \
    --dest-uri 'adls://?account_name=myaccount&layout={table_name}/{load_id}.{file_id}.{ext}' \
    --dest-table 'lakehouse/records'
```

This writes:

```plaintext
lakehouse/
`-- records
    `-- records
        `-- <load_id>.<file_id>.parquet
```

## Supported strategies

Azure Data Lake Storage Gen2 supports `replace` and `append` strategies. It does not support `merge`, `delete+insert`, or `scd2`.

::: info NOTE
When reading from Azure Data Lake Storage Gen2, CSV, JSONL/NDJSON, parquet, and gzip-compressed variants are supported. When writing to Azure Data Lake Storage Gen2, only parquet output is supported.
:::
