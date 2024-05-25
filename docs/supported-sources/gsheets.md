# Google Sheets
[Google Sheets](https://www.google.com/sheets/about/) is a web-based spreadsheet program that is part of Google's free, web-based Google Docs Editors suite.

ingestr supports Google Sheets as a source.

## URI Format
The URI format for Google Sheets is as follows:

```
gsheets://?credentials_path=/path/to/service/account.json
```

Alternatively, you can use base64 encoded credentials:
```
gsheets://?credentials_base64=<base64_encoded_credentials>
```

URI parameters:
- `credentials_path`: the path to the service account JSON file

The URI is used to connect to the Google Sheets API for extracting data.

## Setting up a Google Sheets Integration

Google Sheets requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/google_sheets#setup-guide).

Once you complete the guide, you should have a service account JSON file, and the spreadsheet ID to connect to. Let's say:
- you store your JSON file in path `/path/to/file.json`
- the spreadsheet you'd like to connect to is `abcdxyz`, 
- the sheet inside the spreadsheet is `Sheet1`

Based on this assumption, here's a sample command that will copy the data from the Google Sheets spreadsheet into a duckdb database:

```sh
ingestr ingest --source-uri 'gsheets://?credentials_path=/path/to/file.json' --source-table 'abcdxyz.Sheet1' --dest-uri duckdb:///gsheets.duckdb --dest-table 'gsheets.output'
```

The result of this command will be a table in the \`gsheets.duckdb\` database.

> [!CAUTION]
> Google Sheets does not support incremental loading, which means every time you run the command, it will copy the entire spreadsheet from Google Sheets to the destination. This can be slow for large spreadsheets.
