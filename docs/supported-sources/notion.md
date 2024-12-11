# Notion
[Notion](https://www.notion.so/) is an all-in-one workspace for note-taking, project management, and database management.

ingestr supports Notion as a source.

## URI format
The URI format for Notion is as follows:

```plaintext
notion://?api_key=token
```

URI parameters:
- `api_key`: the integration token used for authentication with the Notion API

The URI is used to connect to the Notion API for extracting data. More details on setting up Notion integrations can be found [here](https://developers.notion.com/docs/getting-started).

## Setting up a Notion Integration

Notion requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/notion#setup-guide).

Once you complete the guide, you should have an API key, and the table ID to connect to. Let's say your API token is `secret_12345` and the database you'd like to connect to is `bfeaafc0c25f40a9asdasd672a9456f3`, here's a sample command that will copy the data from the Notion table into a DuckDB database:

```sh
ingestr ingest --source-uri 'notion://?api_key=secret_12345' --source-table 'bfeaafc0c25f40a9asdasd672a9456f3' --dest-uri duckdb:///notion.duckdb --dest-table 'dest.output'
```

The result of this command will be a table in the `notion.duckdb` database with JSON columns. 

> [!CAUTION]
> Notion does not support incremental loading, which means every time you run the command, it will copy the entire table from Notion to the destination. This can be slow for large tables.

