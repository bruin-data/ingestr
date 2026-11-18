# Azure Blob Storage

[Azure Blob Storage](https://learn.microsoft.com/en-us/azure/storage/blobs/storage-blobs-introduction) is Microsoft's object storage for unstructured data.

`ingestr` supports Azure Blob Storage as a source.

## URI format

The URI for connecting to Azure Blob Storage is structured as follows:

```plaintext
az://?account_name=<storage-account>
```

The `az` and `azure` URI schemes are accepted.

URI parameters:

- `account_name`: Azure storage account name. Required unless `connection_string` is set.
- `tenant_id`: Microsoft Entra tenant ID for service principal authentication.
- `client_id`: Service principal client ID.
- `client_secret`: Service principal client secret. URL-encode the value if it contains special characters.
- `connection_string`: Azure Storage connection string. URL-encode the whole value.
- `account_key`: Azure storage account key.
- `sas_token`: Shared Access Signature token, with or without a leading `?`. URL-encode the token if it contains `&`.

Each sign-in method needs these parameters:

| Sign-in method | Parameters |
|---|---|
| Service principal | `account_name`, `tenant_id`, `client_id`, `client_secret` |
| Connection string | `connection_string` only |
| Account key | `account_name`, `account_key` |
| SAS token | `account_name`, `sas_token` |
| Default Azure login | `account_name` only |

When several are given, `ingestr` uses the first one in this order: connection string, account key, SAS token, service principal, then default Azure login. A connection string sets the account itself, so `account_name` is ignored when one is given. If you set some but not all of `tenant_id`, `client_id`, and `client_secret`, `ingestr` reports the missing ones instead of falling back to the default login.

The default Azure login uses `DefaultAzureCredential`, which supports environment variables, managed identity, Azure CLI login, and other credentials supported by the Azure SDK.

For production, prefer Microsoft Entra service principal authentication. Grant the service principal the `Storage Blob Data Reader` role on the storage account or on a single container. A role scoped to one container is enough: `ingestr` only lists and reads blobs in the container named in `--source-table`.

```plaintext
az://?account_name=<storage-account>&tenant_id=<tenant-id>&client_id=<client-id>&client_secret=<client-secret>
```

You can also omit the credentials from the URI and set them as environment variables:

```sh
export AZURE_TENANT_ID='<tenant-id>'
export AZURE_CLIENT_ID='<client-id>'
export AZURE_CLIENT_SECRET='<client-secret>'
```

A connection string also works for sovereign clouds and the Azurite emulator, because its `EndpointSuffix` or `BlobEndpoint` sets the service endpoint.

The `--source-table` parameter specifies the container and a file path or glob pattern:

```plaintext
<container>/<path-or-glob>
```

Add a format hint when the file extension is not enough to detect the format:

```plaintext
<container>/<path-or-glob>#csv
<container>/<path-or-glob>#jsonl
<container>/<path-or-glob>#parquet
```

CSV, JSONL/NDJSON, parquet, gzip-compressed variants, and ZIP archive members are supported. Select ZIP members with `<container>/<archive.zip>!<member-glob>`. Files without a recognized extension or format hint are skipped.

## Example: Reading data from Azure Blob Storage

For this example, assume that:

- The storage account is `myaccount`.
- The container is `exports`.
- CSV files are stored under `users/`.

```sh
ingestr ingest \
    --source-uri 'az://?account_name=myaccount&tenant_id=<tenant-id>&client_id=<client-id>&client_secret=<client-secret>' \
    --source-table 'exports/users/*.csv' \
    --dest-uri 'duckdb:///records.db' \
    --dest-table 'public.users'
```

## Example: Reading Cloudflare Logpush files

Cloudflare Logpush writes gzip-compressed JSON lines with a `.log.gz` extension. Because `.log` does not identify the format, add the `#jsonl` hint:

```sh
ingestr ingest \
    --source-uri 'az://?account_name=myaccount&tenant_id=<tenant-id>&client_id=<client-id>&client_secret=<client-secret>' \
    --source-table 'cloudflare-logs/http_requests/**/*.log.gz#jsonl' \
    --dest-uri 'duckdb:///logs.db' \
    --dest-table 'cloudflare.http_requests'
```

## Incremental loading by source file timestamps

`ingestr` can use the blob `Last-Modified` or `Creation-Time` timestamp as the incremental key. This filters matching blobs before they are downloaded, so only files inside the requested timestamp interval are read.

When enabled with `--incremental-key _ingestr_source_file_modified_at` or `--incremental-key _ingestr_source_file_created_at`, the source adds these columns to every emitted row:

- `_ingestr_source_file_modified_at`: the blob `Last-Modified` timestamp in UTC.
- `_ingestr_source_file_created_at`: the blob `Creation-Time` timestamp in UTC.
- `_ingestr_source_file_path`: the full `az://container/path` of the source blob.

Only the selected timestamp column is added. Other `--incremental-key` values are treated as regular columns from the file data.

Use `--interval-start` and optionally `--interval-end` to load only blobs whose selected timestamp is in that window. The interval is half-open: `--interval-start` is inclusive, and `--interval-end` is exclusive.

```sh
ingestr ingest \
    --source-uri 'az://?account_name=myaccount' \
    --source-table 'cloudflare-logs/http_requests/**/*.log.gz#jsonl' \
    --dest-uri 'duckdb:///logs.db' \
    --dest-table 'cloudflare.http_requests' \
    --incremental-strategy append \
    --incremental-key _ingestr_source_file_modified_at \
    --interval-start '2026-01-01T00:00:00Z'
```

The `_ingestr_source_file_modified_at`, `_ingestr_source_file_created_at`, and `_ingestr_source_file_path` column names must not already exist in the files.

## Notes

- Storage accounts with a hierarchical namespace (Azure Data Lake Storage Gen2) can be read with `az://` as well. Directory entries are skipped. To use the Data Lake endpoint instead, see [Azure Data Lake Storage Gen2](/supported-sources/adls.md).
- Blobs in the archive tier are skipped, because Azure does not allow reading them until they are rehydrated to an online tier. `ingestr` prints one warning with the number of archived blobs it skipped; run with `--debug` to list their names. With an incremental interval, only archived blobs inside the interval are counted.
- Anonymous access to public containers is not supported. Use a SAS token or one of the credential options above.
