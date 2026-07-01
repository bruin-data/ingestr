# Google Sheets

[Google Sheets](https://www.google.com/sheets/about/) is a web-based spreadsheet program that is part of Google's free, web-based Google Docs Editors suite.

ingestr supports Google Sheets as a source.

## URI format

The URI format for Google Sheets is as follows:

```
gsheets://?credentials_path=/path/to/service/account.json
```

Alternatively, you can use base64 encoded credentials:

```
gsheets://?credentials_base64=<base64_encoded_credentials>
```

URI parameters:

- `credentials_path`: **Optional**. The path to the service account JSON file. If omitted, the source uses [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials) (the `GOOGLE_APPLICATION_CREDENTIALS` env var, or the `gcloud auth application-default login` token on your machine).
- `credentials_base64`: **Optional**. The base64-encoded service account JSON (alternative to `credentials_path`).

The URI is used to connect to the Google Sheets API for extracting data.

To authenticate with your own Google account instead of a service account key, run `gcloud auth application-default login` and omit both credential parameters:

```
gsheets://
```

## Setting up a Google Sheets integration

To connect to Google Sheets, you need to create a Google Cloud service account and share your spreadsheet with it.

### Step 1: Create a Google Cloud Project

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select an existing one
3. Note your project ID

### Step 2: Enable the Google Sheets API

1. In the Cloud Console, go to **APIs & Services** → **Library**
2. Search for "Google Sheets API"
3. Click on it and then click **Enable**

### Step 3: Create a Service Account

1. Go to **APIs & Services** → **Credentials**
2. Click **Create Credentials** → **Service Account**
3. Enter a name (e.g., "sheets-integration") and click **Create**
4. Skip the optional steps and click **Done**

### Step 4: Generate a JSON Key

1. Click on the service account you just created
2. Go to the **Keys** tab
3. Click **Add Key** → **Create new key**
4. Select **JSON** and click **Create**
5. The JSON key file will be downloaded automatically - save it securely

### Step 5: Share Your Spreadsheet

1. Open the JSON key file and find the `client_email` field
2. Open your Google Spreadsheet
3. Click **Share** and add the service account email with **Viewer** access

The JSON file path is your `credentials_path` for the ingestr URI.

Once you have the service account JSON file and the spreadsheet ID, let's say:

- you store your JSON file at the path `/path/to/file.json`.
- the spreadsheet you'd like to connect to has the ID `fkdUQ2bjdNfUq2CA`. For example, if your spreadsheet URL is `https://docs.google.com/spreadsheets/d/fkdUQ2bjdNfUq2CA/edit?pli=1&gid=0#gid=0`, then the spreadsheet ID is `fkdUQ2bjdNfUq2CA`.
- the sheet inside the spreadsheet is `Sheet1`.

Based on this assumption, here's a sample command that will copy the data from the Google Sheets spreadsheet into a DuckDB database:

```sh
ingestr ingest --source-uri 'gsheets://?credentials_path=/path/to/file.json' --source-table 'fkdUQ2bjdNfUq2CA.Sheet1' --dest-uri duckdb:///gsheets.duckdb --dest-table 'dest.output'
```

The result of this command will be a table in the `gsheets.duckdb` database.

> [!CAUTION]
> Google Sheets does not support incremental loading, which means every time you run the command, it will copy the entire spreadsheet from Google Sheets to the destination. This can be slow for large spreadsheets.
