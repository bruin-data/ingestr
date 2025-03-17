# Load Personio Data to DuckDB

Welcome! 👋 This tutorial will guide you through loading data from `Personio` into `DuckDB` using `ingestr`, a command-line tool that enables data ingestion between any source and destination using simple flags, no coding required.

Personio is human resources management software that helps businesses streamline HR processes, including recruitment and employee data management. To analyze and report on this HR data effectively, you may often need to load it into an analytics database like DuckDB. This is where `ingestr` simplifies the process.

## Prerequisites
- Install ingestr if not installed ([guide](../getting-started/quickstart.md#Installation)), 
- Install DuckDB if not installed
- Get Personio Credentials (client_id, client_secret) from this [guide](https://dlthub.com/docs/dlt-ecosystem/verified-sources/personio#grab-credentials) if you don’t have them


## Configuration Steps 
### Source Configuration - Personio

#### `--source-uri`
This flag connects to your Personio account. The URI format is:

```bash
--source-uri 'personio://?client_id=id_123&client_secret=secret_123'
```

URI parameters:
- `client_id`: The client ID used for authentication with the Personio API
- `client_secret`: The client secret used for authentication with the Personio API

#### `--source-table`
This flag specifies which table to read from:
```bash
--source-table 'employees' # Employee information
```

### Destination Configuration - DuckDB
#### `--dest-uri`

This flag connects to DuckDB. The URI format is:
```bash
duckdb:///<database-file>
```
URI parameters:
- `database-file`: The path to the DuckDB database file

#### `--dest-table`
This flag specifies where to save the data:

```bash
--dest-table 'schema.table_name'
```

## Run the `ingest` command
Now that we've configured all our flags, we can run a single command to connect to Personio, read from our specified resources, and load the data into our DuckDB target table.

```bash
ingestr ingest --source-uri 'personio://?client_id=id_123&client_secret=secret_123' \
 --source-table 'employees' \
 --dest-uri duckdb:///personio.duckdb \
 --dest-table 'dest.employees'
```

After running this command, your `Personio` data will be loaded into `DuckDB`. Here's what the data looks like in the destination:

<img alt="personio_duckdb" src="../media/personio_duckdb.png" />

🎉 Congratulations!
You've successfully loaded data from Personio to your desired destination.
