# Azure SQL

[Azure SQL Database](https://learn.microsoft.com/en-us/azure/azure-sql/database/) is Microsoft's managed SQL Server database service.

ingestr supports Azure SQL Database as a source.

## URI format

The URI format for Azure SQL is:

```plaintext
azuresql://user:password@server.database.windows.net:1433/database
```

URI parameters:
- `user`: SQL username, Microsoft Entra username, managed identity client ID, or service principal client ID
- `password`: SQL password, Microsoft Entra password, access token, or service principal secret
- `host`: Azure SQL server host, usually `<server>.database.windows.net`
- `port`: optional, defaults to `1433`
- `database`: the database to read from
- `encrypt`: optional, defaults to `true`
- `fedauth`: optional Microsoft Entra authentication workflow
- `tenant_id`: optional tenant ID; with service principal auth, ingestr appends it to the client ID as `client_id@tenant_id`

## Authentication

SQL authentication works without extra query parameters:

```bash
ingestr ingest \
    --source-uri "azuresql://$SQL_USER:$SQL_PASSWORD@myserver.database.windows.net/mydb" \
    --source-table "dbo.users" \
    --dest-uri "duckdb:///local.db" \
    --dest-table "main.users"
```

For Microsoft Entra service principal authentication:

```bash
ingestr ingest \
    --source-uri "azuresql://$CLIENT_ID:$CLIENT_SECRET@myserver.database.windows.net/mydb?tenant_id=$TENANT_ID&fedauth=ActiveDirectoryServicePrincipal" \
    --source-table "dbo.users" \
    --dest-uri "duckdb:///local.db" \
    --dest-table "main.users"
```

For local Azure CLI, managed identity, or default environment credentials, omit username and password and choose the `fedauth` workflow:

```bash
ingestr ingest \
    --source-uri "azuresql://myserver.database.windows.net/mydb?fedauth=ActiveDirectoryDefault" \
    --source-table "dbo.users" \
    --dest-uri "duckdb:///local.db" \
    --dest-table "main.users"
```

Common `fedauth` values:
- `ActiveDirectoryDefault`
- `ActiveDirectoryAzCli`
- `ActiveDirectoryManagedIdentity`
- `ActiveDirectoryServicePrincipal`
- `ActiveDirectoryServicePrincipalAccessToken`
- `ActiveDirectoryPassword`

The `azure-sql://` scheme is also accepted.

## Local testing

You can test the Azure SQL source path locally with the integration test suite:

```bash
go test -tags integration -run TestAzureSQLSourceScheme_SQLServerContainerToSQLite -count=1 ./tests/integration
```

This starts a SQL Server 2022 container, reads it with an `azuresql://` source URI using SQL authentication, and writes the result to SQLite. It validates the Azure SQL scheme, driver selection, schema discovery, and read path without requiring a real Azure SQL server. Microsoft Entra-only `fedauth` workflows still require Azure-hosted credentials to test end to end.
