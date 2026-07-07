# Microsoft OneLake

[OneLake](https://learn.microsoft.com/en-us/fabric/onelake/onelake-overview) is the unified, lake-centric storage layer of Microsoft Fabric. It exposes an ADLS Gen2-compatible endpoint at `onelake.dfs.fabric.microsoft.com`, where each workspace acts as a container and items (Lakehouses, Warehouses, …) live underneath it.

ingestr supports OneLake as a **destination**. It can write to either area of a Lakehouse:

- **Tables** — written as a [Delta Lake](https://docs.delta.io/) table (Parquet data files plus a `_delta_log` transaction log) so the table is immediately queryable in Fabric, the SQL analytics endpoint, and Spark.
- **Files** — written as raw Parquet files into the Lakehouse `Files` area.

## URI format

```plaintext
onelake://<workspace>/<lakehouse>?tenant_id=<tenant_id>&client_id=<client_id>&client_secret=<client_secret>
```

URI parameters:
- `workspace`: the Fabric workspace name or GUID (the URI host)
- `lakehouse`: the Lakehouse name or GUID (the URI path). A `.Lakehouse` item suffix is added automatically; pass an explicit suffix (e.g. `mywh.Warehouse`) to target a different item type.
- `tenant_id`, `client_id`, `client_secret` (optional): Microsoft Entra service principal credentials
- `sas_token` (optional): a SAS token issued for OneLake
- `layout` (optional): file-name template for **Files** mode (default `{load_id}.{file_id}.{ext}`); supports `{table_name}`, `{load_id}`, `{file_id}` and `{ext}`

The **mode and table** come from `--dest-table`, mirroring OneLake's path layout:
- `Tables/<name>` (or `Tables/<schema>/<name>`) → a Delta table
- `Files/<path>` → raw Parquet files
- a bare name with no prefix defaults to `Tables/`

For Delta tables, `.` and `/` are interchangeable separators and the leading `Tables` segment is optional, so `users`, `schema.name`, `Tables.schema.name` and `Tables/schema/name` are all valid (the last two are equivalent). Period-separated names are convenient because Fabric table and schema names cannot contain a period. Files targets must use the explicit `Files/<path>` form — periods there are left untouched so file extensions are preserved.

The final object path is:
`https://onelake.dfs.fabric.microsoft.com/<workspace>/<lakehouse>.Lakehouse/<Tables|Files>/<rest>`

## Authentication

OneLake only supports **Microsoft Entra ID** authentication — shared account keys are not accepted. ingestr resolves credentials in this order:

1. **SAS token** — if `sas_token` is provided.
2. **Service principal** — if `tenant_id`, `client_id` and `client_secret` are all provided (via [`ClientSecretCredential`](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication)). The service principal needs Contributor (or item-level) access to the workspace, and your Fabric admin must allow service principals to use the APIs.
3. **DefaultAzureCredential** — otherwise, ingestr falls back to [`DefaultAzureCredential`](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication), picking up environment variables, a managed identity, or your Azure CLI login.

## Examples

Load a table into a Lakehouse as a queryable **Delta table**:

```bash
ingestr ingest \
    --source-uri "postgres://user:pass@host:5432/db" \
    --source-table "public.users" \
    --dest-uri "onelake://myworkspace/mylakehouse?tenant_id=$TENANT_ID&client_id=$CLIENT_ID&client_secret=$CLIENT_SECRET" \
    --dest-table "Tables/users"
```

Write raw Parquet **files** into the Lakehouse `Files` area:

```bash
ingestr ingest \
    --source-uri "postgres://user:pass@host:5432/db" \
    --source-table "public.users" \
    --dest-uri "onelake://myworkspace/mylakehouse?sas_token=$SAS_TOKEN" \
    --dest-table "Files/exports/users"
```

Append new rows to an existing Delta table (adds a new Delta commit):

```bash
ingestr ingest \
    --source-uri "postgres://user:pass@host:5432/db" \
    --source-table "public.events" \
    --dest-uri "onelake://myworkspace/mylakehouse?tenant_id=$TENANT_ID&client_id=$CLIENT_ID&client_secret=$CLIENT_SECRET" \
    --dest-table "Tables/events" \
    --incremental-strategy append
```

## Incremental strategies

For **Tables** (Delta) mode, ingestr supports `replace`, `append`, `merge`, `delete+insert` and `scd2`:

| Strategy | Behaviour |
|----------|-----------|
| `replace` | Clears the table directory and writes a fresh Delta commit (version 0). |
| `append` | Reads the current Delta version and writes the next commit with the new rows. |
| `merge` | Upsert by `primary_key`: existing rows with a matching key are replaced by the incoming rows. |
| `delete+insert` | Deletes target rows whose `incremental_key` falls in the loaded interval, then inserts the new rows. |
| `scd2` | Slowly-changing-dimension type 2: maintains `_scd_valid_from`/`_scd_valid_to`/`_scd_is_current`, closing changed rows and inserting new versions. |

The **Files** mode only supports `replace` and `append`.

`merge`, `delete+insert` and `scd2` are **copy-on-write**: because a Delta table has no SQL engine here, ingestr reads the current table back into memory, applies the operation, and rewrites it as a new Delta version. This means each run reads and rewrites the full table — suitable for small-to-medium tables.

Example (merge):

```bash
ingestr ingest \
    --source-uri "postgres://user:pass@host:5432/db" \
    --source-table "public.users" \
    --dest-uri "onelake://myworkspace/mylakehouse?tenant_id=$TENANT_ID&client_id=$CLIENT_ID&client_secret=$CLIENT_SECRET" \
    --dest-table "Tables/users" \
    --incremental-strategy merge \
    --primary-key id
```

## Notes & limitations

- **Replace** is not atomic — there is a brief window where the table is empty.
- **Copy-on-write** strategies load the entire target table into memory and rewrite it on every run.
- **Partitioning**: Delta tables are written non-partitioned; `partition_by` is ignored in Tables mode for now.
- **Type mapping**: timestamps are stored as Delta `timestamp` (microseconds, UTC); JSON and UUID columns are stored as `string`; `TIME` columns are carried as microsecond `long` values (Delta has no time type).
- **CDC-aware merge** (soft-deletes via `_cdc_deleted`) is not implemented; CDC delete markers are merged as regular rows.
