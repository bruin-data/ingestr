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

To connect to Airtable, you need to create a Personal Access Token.

### Step 1: Create a Personal Access Token

1. Log in to [Airtable](https://airtable.com/)
2. Go to [Account Settings](https://airtable.com/account) → click your profile icon → **Account**
3. Click on **Developer hub** in the left sidebar
4. Click **Personal access tokens**
5. Click **Create new token**

### Step 2: Configure the Token

1. Enter a name for your token (e.g., "Data Integration")
2. Under **Scopes**, select:
   - `data.records:read` - Read records from bases
   - `schema.bases:read` - Read base schemas
3. Under **Access**, choose:
   - **All current and future bases in all current and future workspaces** (for full access)
   - Or select specific bases you want to access
4. Click **Create token**

### Step 3: Copy the Token

1. Copy the token immediately (starts with `pat`)
2. Store it securely - it won't be shown again

Once you have the Access Token: 

The source table you'll use for ingestr will be `<base_id>/<table_id>`.

### Getting your Base ID and Table ID

To find your Base ID and Table ID:

1. Log into Airtable and navigate to your base or table
2. Look at the URL in your browser's address bar when viewing your base
3. The Base ID always starts with "app" and appears before the next `/`
4. The Table IDs start with "tbl" and appears before the next `/`.

For example, in this URL:
```plaintext
https://airtable.com/appve10kl227BIT4GV/tblOUnZVLFWbemTP1/viw3qtF76bRQC3wKx/rec9khXgeTotgCQ62?blocks=hide 
```
In this case base_id is `appve10kl227BIT4GV` and table_id is `tblOUnZVLFWbemTP1`

Let's say your access token is `patr123.abc` and the base ID is `appXYZ`, here's a sample command that will copy the data from Airtable into a DuckDB database:

```sh
ingestr ingest \
    --source-uri 'airtable://?access_token=patr123.abc' \
    --source-table 'appXYZ/employee' \
    --dest-uri 'duckdb:///airtable.duckdb' \
    --dest-table 'des.employee'
```

The result of this command will be an `employee` table containing data from the `employee` source in the `airtable.duckdb` database.

> [!CAUTION]
> Airtable does not support incremental loading, which means every time you run the command, the entire table will be copied from Airtable to the destination. This can be slow for large tables.
