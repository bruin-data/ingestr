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

ALTER DATABASE your_database
SET ALLOW_SNAPSHOT_ISOLATION ON;

ALTER TABLE dbo.users
ENABLE CHANGE_TRACKING
WITH (TRACK_COLUMNS_UPDATED = OFF);
```

Enabling snapshot isolation is recommended: ingestr then reads each change window inside one SNAPSHOT transaction, which is how SQL Server guarantees a consistent change set while retention cleanup runs. Without it, the initial snapshot is taken under a `HOLDLOCK` table lock that blocks writers to the table until the snapshot finishes, and incremental reads run under READ COMMITTED with the cursor re-validated after the read; if cleanup (or a `TRUNCATE TABLE`, which resets a table's tracking) invalidated the cursor mid-read, the run fails and asks for `--full-refresh` instead of loading an incomplete change set.

Change Tracking returns net row changes since the last loaded version. For inserts and updates, ingestr joins the changed primary keys back to the source table and loads the current row. For deletes, SQL Server only returns the primary key, so ingestr marks the destination row as deleted with `_cdc_deleted = true` while preserving existing destination values for other columns. If a row is updated and then deleted between two ingestr runs, Change Tracking cannot reconstruct the intermediate updated values.

Pass `--stream` to keep polling instead of exiting once caught up:

```sh
ingestr ingest \
    --source-uri "mssql+ct://user:password@host:1433/dbname?encrypt=disable&poll_interval=2s" \
    --source-table "dbo.users" \
    --dest-uri "duckdb:///warehouse.duckdb" \
    --dest-table "dbo.users" \
    --stream
```

A shorter poll interval narrows the window in which several updates to the same row collapse into one, so streaming Change Tracking loses less intermediate detail than a scheduled run — but it still reports the row's current state rather than every individual change. Use [Change Data Capture](#change-data-capture) when you need the full history.

Change Tracking URI parameters:

- `poll_interval`: how long to wait between polls in streaming mode. Any Go duration (`500ms`, `2s`, `1m`); defaults to `1s`. Ignored outside `--stream`.

While a stream sits idle, ingestr periodically restamps the resume cursor so the recorded version stays inside the database's `CHANGE_RETENTION` window and a restart can resume instead of re-snapshotting. The interval is derived from that retention setting — a quarter of it, capped at 5 minutes and floored at 5 seconds — so a database that expires versions in minutes is restamped more often than one retaining them for days. Streaming is single-table: name one table with `--source-table`.

Into BigQuery or Snowflake, run this connector serially: neither destination enforces primary-key uniqueness, so two overlapping runs can leave permanent duplicate rows. See [Overlapping runs on BigQuery and Snowflake](/getting-started/cdc.md#overlapping-runs-on-bigquery-and-snowflake).

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
- `dest_schema`: optional destination schema for multi-table CDC runs. Ignored when `--source-table` names a single table; the destination is then `--dest-table`. A comma-separated `--source-table` is still a multi-table run, so `dest_schema` applies.

For a full walkthrough — enabling CDC and replicating a table into DuckDB — see [Replicate SQL Server to DuckDB with CDC](/tutorials/cdc-sqlserver-duckdb.md).

Into BigQuery or Snowflake, run this connector serially: neither destination enforces primary-key uniqueness, so two overlapping runs can leave permanent duplicate rows. See [Overlapping runs on BigQuery and Snowflake](/getting-started/cdc.md#overlapping-runs-on-bigquery-and-snowflake).

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
