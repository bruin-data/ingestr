# Microsoft Fabric

[Microsoft Fabric](https://learn.microsoft.com/en-us/fabric/data-warehouse/) Data Warehouse is a lake-centric, SQL-based data warehouse that speaks the SQL Server (TDS) protocol.

ingestr supports Microsoft Fabric Warehouse as both a source and a destination.

## URI format

The URI format for Microsoft Fabric is as follows:

```plaintext
fabric://<client_id>:<client_secret>@<workspace>.datawarehouse.fabric.microsoft.com/<warehouse>?tenant_id=<tenant_id>
```

URI parameters:
- `client_id`: the application (client) ID of the Microsoft Entra service principal
- `client_secret`: the client secret of the service principal
- `host`: the warehouse's SQL connection string, e.g. `<workspace>.datawarehouse.fabric.microsoft.com`
- `warehouse`: the name of the warehouse to connect to
- `tenant_id`: the Microsoft Entra tenant ID the service principal belongs to
- `fedauth` (optional): the Microsoft Entra authentication workflow to use (see below)
- `write_strategy` (optional, destination only): how rows are written to Fabric — `copy` (default) or `insert` (see [Write strategies](#write-strategies))

## Authentication

Fabric Warehouse only supports **Microsoft Entra ID** authentication — there is no SQL username/password login. The connection is encrypted (TLS) by default.

By default, ingestr authenticates with a **service principal**: supply the client ID, secret and `tenant_id`, and ingestr uses the `ActiveDirectoryServicePrincipal` workflow. The service principal must be granted access to the workspace (Contributor role or item-level permissions on the warehouse), and your Fabric admin must allow service principals to use the APIs.

If you omit the credentials, ingestr falls back to `ActiveDirectoryDefault`, which uses [`DefaultAzureCredential`](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication) — picking up environment variables, a managed identity, or your Azure CLI login.

You can select any workflow explicitly with the `fedauth` query parameter, for example:
- `fedauth=ActiveDirectoryServicePrincipalAccessToken` — pass a pre-fetched access token as the password
- `fedauth=ActiveDirectoryManagedIdentity` — authenticate with a managed identity

## Examples

Load a table **into** a Fabric Warehouse (Fabric as destination):

```bash
ingestr ingest \
    --source-uri "sqlite:///source.db" \
    --source-table "main.users" \
    --dest-uri "fabric://$CLIENT_ID:$CLIENT_SECRET@myworkspace.datawarehouse.fabric.microsoft.com/MyWarehouse?tenant_id=$TENANT_ID" \
    --dest-table "dbo.users"
```

Read a table **from** a Fabric Warehouse (Fabric as source):

```bash
ingestr ingest \
    --source-uri "fabric://$CLIENT_ID:$CLIENT_SECRET@myworkspace.datawarehouse.fabric.microsoft.com/MyWarehouse?tenant_id=$TENANT_ID" \
    --source-table "dbo.users" \
    --dest-uri "duckdb:///local.db" \
    --dest-table "main.users"
```

## Write strategies

When Fabric is used as a **destination**, the `write_strategy` query parameter controls how rows are sent to the warehouse:

- `copy` (**default**): rows are streamed through the TDS **bulk-copy** path (the same mechanism as SQL Server's `BULK INSERT`). It is the fastest option, especially for **wide tables** (many columns) or **high-row-count** loads where the parameterised insert path is limited by the ~2100 parameter cap per statement.
- `insert`: rows are written with batched, parameterised `INSERT ... VALUES` statements. Use it if you need to fall back from the bulk-copy path.

Both strategies write directly over the connection — `copy` does **not** stage files in OneLake or blob storage; it uses the bulk-copy path built into the TDS protocol.

Override the default on the destination URI:

```bash
ingestr ingest \
    --source-uri "sqlite:///source.db" \
    --source-table "main.events" \
    --dest-uri "fabric://$CLIENT_ID:$CLIENT_SECRET@myworkspace.datawarehouse.fabric.microsoft.com/MyWarehouse?tenant_id=$TENANT_ID&write_strategy=insert" \
    --dest-table "dbo.events"
```

The `write_strategy` value only affects how data is loaded; it is independent of `--incremental-strategy`, which controls the merge/replace/append semantics of the ingestion.

## Notes & limitations

- **Type mapping (destination)**: Fabric does not support a number of SQL Server types. Strings are written as `VARCHAR` (UTF-8) and timestamps as `DATETIME2(6)`; timezone-aware timestamps are stored as their UTC instant (Fabric has no `DATETIMEOFFSET`).
- **Primary keys** are created as `NONCLUSTERED ... NOT ENFORCED`, as required by Fabric.
- **Replace strategy** writes directly to the target table (drop and recreate) rather than performing an atomic staging-table swap, since the warehouse stages data in a separate schema.
- **Schema evolution** can add new (nullable) columns; changing an existing column's type is not performed.
- The default warehouse collation is case-sensitive (`Latin1_General_100_BIN2_UTF8`), so string-keyed merges and joins are case-sensitive.
