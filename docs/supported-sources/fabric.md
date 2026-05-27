# Microsoft Fabric

[Microsoft Fabric](https://learn.microsoft.com/en-us/fabric/data-warehouse/) Data Warehouse is a lake-centric, SQL-based data warehouse that speaks the SQL Server (TDS) protocol.

ingestr supports Microsoft Fabric Warehouse as a destination.

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

## Authentication

Fabric Warehouse only supports **Microsoft Entra ID** authentication — there is no SQL username/password login. The connection is encrypted (TLS) by default.

By default, ingestr authenticates with a **service principal**: supply the client ID, secret and `tenant_id`, and ingestr uses the `ActiveDirectoryServicePrincipal` workflow. The service principal must be granted access to the workspace (Contributor role or item-level permissions on the warehouse), and your Fabric admin must allow service principals to use the APIs.

If you omit the credentials, ingestr falls back to `ActiveDirectoryDefault`, which uses [`DefaultAzureCredential`](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication) — picking up environment variables, a managed identity, or your Azure CLI login.

You can select any workflow explicitly with the `fedauth` query parameter, for example:
- `fedauth=ActiveDirectoryServicePrincipalAccessToken` — pass a pre-fetched access token as the password
- `fedauth=ActiveDirectoryManagedIdentity` — authenticate with a managed identity

## Example

Copy a table from a local SQLite database into a Fabric Warehouse:

```bash
ingestr ingest \
    --source-uri "sqlite:///source.db" \
    --source-table "main.users" \
    --dest-uri "fabric://$CLIENT_ID:$CLIENT_SECRET@myworkspace.datawarehouse.fabric.microsoft.com/MyWarehouse?tenant_id=$TENANT_ID" \
    --dest-table "dbo.users"
```

## Notes & limitations

- **Type mapping**: Fabric does not support a number of SQL Server types. Strings are written as `VARCHAR` (UTF-8) and timestamps as `DATETIME2(6)`; timezone-aware timestamps are stored as their UTC instant (Fabric has no `DATETIMEOFFSET`).
- **Primary keys** are created as `NONCLUSTERED ... NOT ENFORCED`, as required by Fabric.
- **Replace strategy** writes directly to the target table (drop and recreate) rather than performing an atomic staging-table swap, since the warehouse stages data in a separate schema.
- **Schema evolution** can add new (nullable) columns; changing an existing column's type is not performed.
- The default warehouse collation is case-sensitive (`Latin1_General_100_BIN2_UTF8`), so string-keyed merges and joins are case-sensitive.
