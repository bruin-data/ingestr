# Load Data From Personio to DuckDB

Welcome! 👋  
This tutorial will guide you through loading data from `Personio` into `DuckDB` using `ingestr`, a command-line tool that enables data ingestion between any source and destination using simple flags, no coding required.

Personio is human resources management software that helps businesses streamline HR processes, including recruitment and employee data management. To analyze and report on this HR data effectively, you may often need to load it into an analytics database like DuckDB. This is where `ingestr` simplifies the process.

## Prerequisites
Before you begin, ensure you have the following installed and configured:
- [ingestr](../getting-started/quickstart.md#Installation)
- [Personio API credentials](https://dlthub.com/docs/dlt-ecosystem/verified-sources/personio#grab-credentials)
- DuckDB

## Ingest Data from Personio to DuckDB
Run the following command to connect to Personio, read from the specified table, and load the data into DuckDB:
```bash
ingestr ingest \
    --source-uri 'personio://?client_id=<YOUR_CLIENT_ID>&client_secret=<YOUR_CLIENT_SECRET>' \
    --source-table 'employees' \
    --dest-uri 'duckdb:///personio.duckdb' \
    --dest-table 'dest.employees'
```
- `--source-uri`: Connects to your source, specifying the data source and authentication details.

  - `personio://?client_id=<ID>&client_secret=<SECRET>`
 Specifies the data source, which in this case is Personio, and uses API credentials (client_id and client_secret) to authenticate with the Personio account
- `--source-table`: Specifies the table from which to read data
  - In the above example, the employees table in Personio is selected, which retrieves employee information
- `--dest-uri`: Connects to your destination, specifying where the data will be stored.
  - `duckdb:///<database-file>`, where <database-file> is the path to the DuckDB database file.
- `--dest-table`: Defines the table in DuckDB where the data will be stored.
  - Example: 'dest.employees' (Stores the data in the employees table within the dest schema in DuckDB).

## Verify the Data in DuckDB
After running the above command with valid credentials, your `Personio` data will be successfully loaded into `DuckDB`. Here's what the data looks like in the destination:

<img alt="personio_duckdb" src="../media/personio_duckdb.png" />

🎉 Congratulations!   
You've successfully loaded data from Personio to your desired destination.
