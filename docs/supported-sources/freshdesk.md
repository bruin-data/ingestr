# Freshdesk

[Freshdesk](https://www.freshworks.com/freshdesk/) is a cloud-based customer service platform that helps businesses manages customer support via multiple channels including email,phone,websites, and social media.

ingestr supports Freshdesk as a source.

## URI format

The URI format for Freshdesk is:

```
freshdesk://<domain>?api_key=<api_key>
```

URI parameters:
- `api_key` :  The API token used for authentication with Freshdesk.
- `domain`: The domain of your Freshdesk account, found in your account URL. For example, if your account URL is https://my_company.freshdesk.com/, then `my_company` is your domain.

The URI is used to connect to the Freshdesk API for extracting data.

## Setting up a Freshdesk integration

Freshdesk requires a few steps to set up an integration, please follow dltHub [setup guide](https://dlthub.com/docs/dlt-ecosystem/verified-sources/freshdesk#setup-guide).

After completing the guide,, you will have your  `api_key` and `domain`. For example, if your api_key is
`test_key` and domain `my_company`, you can use the following command to copy data from Freshdesk into a DuckDB database:

```sh
ingestr ingest --source-uri 'freshdesk://$DOMAIN?api_key=$API_KEY' --source-table 'contacts' --dest-uri duckdb:///freshdesk.duckdb --dest-table 'dest.contacts'
```
This command copies data from the Freshdesk source to DuckDB.

<img alt="freshdesk_img" src="../media/freshdesk_ingestion.png" />

## Available Tables
The Freshdesk source allows you to ingest the following tables:

| S.No. | Name      | Description                                                                               |
| ----- | --------- | ----------------------------------------------------------------------------------------- |
| 1.    | agents    |  Users responsible for managing and resolving customer inquiries and support tickets.     |
| 2.    | companies |  Customer organizations or groups that agents support.                                    |
| 3.    | contacts  |  Individuals or customers who reach out for support.                                      |
| 4.    | groups    |  Agents organized based on specific criteria.                                             |
| 5.    | roles     |  Predefined sets of permissions that determine what actions an agent can perform.         |
| 6.    | tickets   |  Customer inquiries or issues submitted via various channels like email, chat, phone, etc. |

Use these as the `--source-table` parameter in the `ingestr ingest` command.