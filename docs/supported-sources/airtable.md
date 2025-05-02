# Airtable

[Airtable](https://airtable.com/) is a cloud-based platform that combines spreadsheet and database functionalities, designed for data management and collaboration.

ingestr supports Airtable as a source.

## URI format

The URI format for Airtable is as follows:

```plaintext
 airtable://?access_token=<access_token>
```

URI parameters:

- `access_token`: A personal access token for authentication with the Airtable API.

The URI is used to connect to the Airtable API for extracting data. More details on setting up Airtable integrations can be found [here](https://airtable.com/developers/web/api).

## Setting up a Airtable Integration

Airtable requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/airtable#setup-guide).

Once you complete the guide, you should have an Access Token and a Base ID. The source table you'll use for ingestr will be `<base_id>/<table_name>`.

Let's say your access token is `patr123.abc` and the base ID is `appXYZ`, here's a sample command that will copy the data from Airtable into a DuckDB database:

```sh
ingestr ingest 
    --source-uri 'airtable://?access_token=patr123.abc' 
    --source-table 'appXYZ/employee' 
    --dest-uri 'duckdb:///airtable.duckdb' 
    --dest-table 'des.employee'
```

The result of this command will be an `employee` table containing data from the `employee` source in the `airtable.duckdb` database.

> [!CAUTION]
> Airtable does not support incremental loading, which means every time you run the command, the entire table will be copied from Airtable to the destination. This can be slow for large tables.
