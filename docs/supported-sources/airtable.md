# Airtable

[Airtable](https://airtable.com/) is a cloud-based platform that combines spreadsheet and database functionalities, designed for data management and collaboration.

ingestr supports Airtable as a source.

## URI Format

The URI format for Airtable is as follows:

```plaintext
 airtable://?access_token=<access_token>&base_id=<base_id>
```

URI parameters:

- `base_id`: A unique identifier for an Airtable base.
- `access_token`: A personal access token for authentication with the Airtable API.

The URI is used to connect to the Airtable API for extracting data. More details on setting up Airtable integrations can be found [here](https://airtable.com/developers/web/api).

## Setting up a Airtable Integration

Airtable requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/airtable#setup-guide).

Once you complete the guide, you should have an Access Token and Base Id. Let's say your Access Token is `patr123.abc` and Base Id is `appXYZ`, here's a sample command that will copy the data from Airtable into a duckdb database:

```sh
ingestr ingest --source-uri 'airtable://?base_id=appXYc&access_token=patr123.abc' --source-table 'employee' --dest-uri 'duckdb:///airtable.duckdb' --dest-table 'des.employee'
```

The result of this command will be an `employee` table containing data from the `employee` source in the `Airtable.duckdb` database.

The `source-table` can include multiple table names that share the `same base_id` (e.g.--source-table 'employee,users') but this will merge all the data from the specified tables into a single destination table.

> [!CAUTION]
> Airtable does not support incremental loading, which means every time you run the command, the entire table will be copied from Airtable to the destination. This can be slow for large tables.
