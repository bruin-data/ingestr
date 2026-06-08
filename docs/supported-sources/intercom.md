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

To obtain an Intercom access token, create a private app in your workspace:

1. Sign in to Intercom with the workspace you want to ingest data from, then open the [Intercom Developer Hub](https://developers.intercom.com/) — you can also reach it from within your workspace via **Settings → Integrations → Developer Hub**.
2. Go to **Your apps** (or open [https://app.intercom.com/developers/_/apps](https://app.intercom.com/developers/_/apps) directly) and click **New app**. Give it a name, select the workspace you want to ingest from, and choose **Internal integration** as the app type.
3. Open the new app and go to **Authentication** in the left sidebar.
4. Under **Access token**, click **Generate token** (or copy the existing one). This token is the value you will pass as `access_token`.
5. Go to **Authentication → Permissions** and enable read access for the resources you intend to ingest:
   - **Read users and companies** — for `contacts` and `companies`.
   - **Read conversations** — for `conversations`.
   - **Read content data** — for `articles` and other help-center content.
   - **Read and list admins** — for `admins` and `teams`.
6. Note the region your workspace is hosted in (`us`, `eu`, or `au`). You can confirm it by checking the URL you sign in from: `app.intercom.com` for US, `app.eu.intercom.com` for EU, `app.au.intercom.com` for AU.

Once you have an access token, let's say your access token is `dG9rOjE...` and your workspace is in the US region, here's a sample command that will copy the data from Intercom into a DuckDB database:

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