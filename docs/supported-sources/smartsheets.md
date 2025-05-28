# Smartsheet

[Smartsheet](https://www.smartsheet.com/) is a software as a service (SaaS) offering for collaboration and work management.

ingestr supports Smartsheet as a source.

## URI format

The URI format for Smartsheet is as follows:

```plaintext
smartsheet://?access_token=<access_token>
```

URI parameters:

- `access_token`: Your Smartsheet API access token.

The URI is used to connect to the Smartsheet API for extracting data. You can generate an access token in Smartsheet by navigating to Account > Personal Settings > API Access.

## Setting up a Smartsheet Integration

To set up a Smartsheet integration, you'll need an API Access Token.

1. Log in to Smartsheet.
2. Click on "Account" in the bottom left corner, then "Personal Settings".
3. Go to the "API Access" tab.
4. Click "Generate new access token".
5. Give your token a name and click "OK".
6. Copy the generated token. This will be your `access_token`.

The source table you'll use for ingestr will be the `sheet_id` of the Smartsheet you want to ingest. You can find the `sheet_id` by opening the sheet in Smartsheet and going to File > Properties. The Sheet ID will be listed there.

Let's say your access token is `llk2k3j4l5k6j7h8g9f0` and the sheet ID is `1234567890123456`, here's a sample command that will copy the data from Smartsheet into a DuckDB database:

```sh
ingestr ingest \
    --source-uri 'smartsheet://?access_token=llk2k3j4l5k6j7h8g9f0' \
    --source-table '1234567890123456' \
    --dest-uri 'duckdb:///smartsheet_data.duckdb' \
    --dest-table 'des.my_sheet_data'
```

The result of this command will be a `my_sheet_data` table containing data from your Smartsheet in the `smartsheet_data.duckdb` database.

> [!CAUTION]
> Smartsheet integration does not currently support incremental loading. Every time you run the command, the entire sheet will be copied from Smartsheet to the destination. This can be slow for large sheets. 