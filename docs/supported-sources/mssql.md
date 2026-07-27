# Microsoft SQL Server
Microsoft SQL Server is a relational database management system developed by Microsoft.

ingestr supports Microsoft SQL Server as both a source and destination.

## Installation

To use Microsoft SQL Server with ingestr, you need to install the `pyodbc` add-on as well. You can do this by running:

```bash
pip install ingestr[odbc]
```

## URI format
The URI format for Microsoft SQL Server is as follows:

```plaintext
mssql://user:password@host:port/dbname?driver=ODBC+Driver+18+for+SQL+Server&TrustServerCertificate=yes
```

URI parameters:
- `user`: the username to connect to the SQL Server instance
- `password`: the password to connect to the SQL Server instance
- `host`: the hostname of the SQL Server instance
- `port`: the port of the SQL Server instance
- `dbname`: the name of the database to connect to
- `driver`: the ODBC driver to use to connect to the SQL Server instance
- `TrustServerCertificate`: whether to trust the server certificate

The same URI structure can be used both for sources and destinations. You can read more about SQLAlchemy's SQL Server dialect [here](https://docs.sqlalchemy.org/en/20/core/engines.html#microsoft-sql-server).

## Change Tracking

ingestr can read SQL Server Change Tracking tables with the `mssql+ct://`, `sqlserver+ct://`, `azuresql+ct://`, and `azure-sql+ct://` URI schemes.

```sh
ingestr ingest \
    --source-uri "mssql+ct://user:password@host:1433/dbname?encrypt=disable" \
    --source-table "dbo.users" \
    --dest-uri "duckdb:///warehouse.duckdb" \
    --dest-table "dbo.users"
```

The destination table stores the Change Tracking cursor in `_cdc_lsn`, so later runs resume automatically from the latest loaded version. Change Tracking loads use the `merge` strategy by default and require a primary key on the source table.

SQL Server setup example:

```sql
ALTER DATABASE your_database
SET CHANGE_TRACKING = ON
(CHANGE_RETENTION = 2 DAYS, AUTO_CLEANUP = ON);

ALTER TABLE dbo.users
ENABLE CHANGE_TRACKING
WITH (TRACK_COLUMNS_UPDATED = OFF);
```

Change Tracking returns net row changes since the last loaded version. For inserts and updates, ingestr joins the changed primary keys back to the source table and loads the current row. For deletes, SQL Server only returns the primary key, so ingestr marks the destination row as deleted with `_cdc_deleted = true` while preserving existing destination values for other columns. If a row is updated and then deleted between two ingestr runs, Change Tracking cannot reconstruct the intermediate updated values.

## Change Data Capture

For full row-level change history — not just which rows changed — ingestr can read SQL Server's log-based **Change Data Capture** with the `mssql+cdc://`, `sqlserver+cdc://`, `azuresql+cdc://`, and `azure-sql+cdc://` URI schemes.

```sh
ingestr ingest \
    --source-uri "mssql+cdc://user:password@host:1433/shop?encrypt=disable" \
    --source-table "dbo.customers" \
    --dest-uri "duckdb:///warehouse.duckdb" \
    --dest-table "dbo.customers"
```

This path reads a consistent snapshot first, then reads the CDC change tables SQL Server's capture job populates from the transaction log. It produces the `_cdc_lsn`, `_cdc_deleted`, and `_cdc_synced_at` metadata columns and resumes from the destination table's maximum `_cdc_lsn` on subsequent runs. Incremental runs use the `merge` strategy so updates and deletes are applied by primary key; deletes are soft (`_cdc_deleted = true`). Run with `--full-refresh` to rebuild from a fresh snapshot, or `--stream` to ingest continuously instead of once per invocation.

Requirements:
- The **SQL Server Agent** must be running — it drives the capture job that copies changes from the log into the CDC tables.
- CDC must be enabled on the database (`sys.sp_cdc_enable_db`) and on each source table (`sys.sp_cdc_enable_table`), which creates a capture instance.
- Source tables must have a primary key.
- The connecting user needs `SELECT` on the table and its capture instance.

CDC URI parameters:
- `capture_instance`: optional capture-instance name; defaults to the single instance registered for the table.
- `dest_schema`: optional destination schema for multi-table CDC runs. Ignored when `--source-table` is set; the destination is then `--dest-table`.

For a full walkthrough — enabling CDC and replicating a table into DuckDB — see [Replicate SQL Server to DuckDB with CDC](/tutorials/cdc-sqlserver-duckdb.md).

## Tips & Tricks

If you're using Azure SQL Server, you can use `az cli` to generate access tokens to connect to SQL server. 

Set the password to your token and the `Authentication` parameter to `ActiveDirectoryAccessToken`
::: code-group

```sh [token-auth-example.sh]
USER=$(az account show --query user.name -o tsv)
TOKEN=$(az account get-access-token --resource https://database.windows.net/ --query accessToken -o tsv)
ingestr ingest \
    --source-uri "mssql://$USER:$TOKEN@<server>.database.windows.net/<database>?Authentication=ActiveDirectoryAccessToken" \
    --source-table "dbo.example" \
    --dest-uri "duckdb:///example.db" \
    --dest-table "dbo.example" \
```
:::
