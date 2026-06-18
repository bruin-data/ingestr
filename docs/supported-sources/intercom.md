# Intercom

[Intercom](https://www.intercom.com/) is a customer messaging platform that helps businesses connect with customers through targeted, behavior-driven messages.

ingestr supports Intercom as a source.

## URI format

The URI format for Intercom is as follows:

```plaintext
intercom://?access_token=<access_token>&region=<region>
```

URI parameters:

- `access_token`: The access token is used for authentication with the Intercom API.
- `region`: The data region where your Intercom workspace is hosted. Valid values are `us`, `eu`, or `au`. Defaults to `us`.

The URI is used to connect to the Intercom API for extracting data.

## Setting up an Intercom Integration

To connect to Intercom, you need to create an app and obtain an access token.

### Step 1: Create a Private App

1. Log in to your [Intercom Developer Hub](https://developers.intercom.com/)
2. Click **Your apps** in the top right
3. Click **New app**
4. Enter an app name and select your workspace
5. Choose **Internal integration** as the app type
6. Click **Create app**

### Step 2: Configure Permissions

1. In your app settings, go to **Authentication**
2. Under **Access Token**, click **Edit** next to the workspace
3. Enable the permissions you need:
   - **Read users and companies** - For contacts and companies
   - **Read conversations** - For conversations
   - **Read content data** - For articles and help center content
   - **Read and list admins** - For admin users
4. Click **Save**

### Step 3: Get the Access Token

1. In **Authentication**, find the **Access Token** section
2. Copy the access token (it starts with `dG9r` when base64 encoded)
3. Store this token securely

### Determine Your Region

Intercom has different data regions. Check your Intercom URL:
- `app.intercom.com` → `us` region
- `app.eu.intercom.com` → `eu` region  
- `app.au.intercom.com` → `au` region

Once you have your access token, let's say your access token is `dG9rOjE...` and your workspace is in the US region, here's a sample command that will copy the data from Intercom into a DuckDB database:

```sh
ingestr ingest \
    --source-uri 'intercom://?access_token=dG9rOjE...&region=us' \
    --source-table 'contacts' \
    --dest-uri duckdb:///intercom.duckdb \
    --dest-table 'intercom.contacts'
```

The result of this command will be a table in the `intercom.duckdb` database.

## Tables

Intercom source allows ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
|-------|----|---------|--------------|---------|
| [contacts](https://developers.intercom.com/docs/references/rest-api/api.intercom.io/Contacts/contact/) | id | updated_at | merge | Retrieves information about contacts (visitors, users, and leads) |
| [companies](https://developers.intercom.com/docs/references/rest-api/api.intercom.io/Companies/company/) | id | updated_at | merge | Retrieves information about companies |
| [conversations](https://developers.intercom.com/docs/references/rest-api/api.intercom.io/Conversations/conversation/) | id | updated_at | merge | Retrieves conversation data |
| [articles](https://developers.intercom.com/docs/references/rest-api/api.intercom.io/Articles/article/) | id | updated_at | merge | Retrieves help center articles |
| [tags](https://developers.intercom.com/docs/references/rest-api/api.intercom.io/Tags/tag/) | id | - | replace | Retrieves tags used to organize contacts and companies |
| [segments](https://developers.intercom.com/docs/references/rest-api/api.intercom.io/Segments/segment/) | id | - | replace | Retrieves segments for filtering contacts and companies |
| [admins](https://developers.intercom.com/docs/references/rest-api/api.intercom.io/Admins/admin/) | id | - | replace | Retrieves admin user information |
| [teams](https://developers.intercom.com/docs/references/rest-api/api.intercom.io/Teams/team/) | id | - | replace | Retrieves team information |
| [data_attributes](https://developers.intercom.com/docs/references/rest-api/api.intercom.io/Data%20Attributes/dataattribute/) | name | - | replace | Retrieves data attributes (both built-in and custom attributes) |

Use these as `--source-table` parameter in the `ingestr ingest` command.

> [!TIP]
> Resources marked with "merge" Inc Strategy support incremental loading based on the `updated_at` timestamp, which means subsequent runs will only fetch new or updated records.