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

To connect to Notion, you need to create an internal integration and share your database with it.

### Step 1: Create an Integration

1. Go to [Notion Integrations](https://www.notion.so/my-integrations)
2. Click **+ New integration**
3. Select the workspace where your database is located
4. Give your integration a name (e.g., "Data Integration")
5. Under **Capabilities**, ensure at least **Read content** is enabled
6. Click **Submit**

### Step 2: Get the API Key

1. After creating the integration, you'll see an **Internal Integration Secret**
2. Click **Show** and then **Copy** to get your API key (starts with `secret_`)
3. Store this key securely

### Step 3: Share Your Database with the Integration

1. Open the Notion database you want to access
2. Click the **...** menu in the top-right corner
3. Click **Add connections** (or **Connect to**)
4. Search for and select your integration
5. Click **Confirm**

### Step 4: Get Your Database ID

1. Open your database in Notion
2. Look at the URL in your browser
3. The database ID is the 32-character string after your workspace name and before the `?`

For example, in this URL:
```
https://www.notion.so/myworkspace/bfeaafc0c25f40a9asdasd672a9456f3?v=...
```
The database ID is `bfeaafc0c25f40a9asdasd672a9456f3`.

Once you have your API key and database ID, let's say your API token is `secret_12345` and the database you'd like to connect to is `bfeaafc0c25f40a9asdasd672a9456f3`, here's a sample command that will copy the data from the Notion table into a DuckDB database:

```sh
ingestr ingest --source-uri 'notion://?api_key=secret_12345' --source-table 'bfeaafc0c25f40a9asdasd672a9456f3' --dest-uri duckdb:///notion.duckdb --dest-table 'dest.output'
```

The result of this command will be a table in the `notion.duckdb` database with JSON columns. 

> [!CAUTION]
> Notion does not support incremental loading, which means every time you run the command, it will copy the entire table from Notion to the destination. This can be slow for large tables.

