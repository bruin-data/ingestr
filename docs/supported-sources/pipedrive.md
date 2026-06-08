# Pipedrive
[Pipedrive](https://www.pipedrive.com/) is a cloud-based sales Customer Relationship Management (CRM) tool designed to help businesses manage leads and deals, track communication, and automate sales processes.

ingestr supports pipedrive as a source.

## URI format

The URI format for pipedrive is as follows:

```plaintext
pipedrive://?api_token=<api_token>
```

URI parameters:
- api_token: token used for authentication with the Pipedrive API

## Setting up a Pipedrive Integration

Pipedrive uses a personal API token for authentication. Each user has their own token, and the token inherits that user's permissions:

1. Sign in to your Pipedrive account with the user whose access you want ingestr to use. Make sure that user has visibility into the data you want to copy (deals, persons, organizations, etc.).
2. Click your profile picture in the top-right corner and choose **Personal preferences**.
3. Open the **API** tab. If you do not see this tab, ask an admin to enable API access for your user under **Settings → User management**.
4. Copy the value shown under **Your personal API token**. This is your `api_token`. You can also click **Generate new token** if you want a dedicated token for this integration.

Once you have the token, let's say your `api_token` is token_123, here's a sample command that will copy the data from Pipedrive into a DuckDB database:

```bash
ingestr ingest \
--source-uri 'pipedrive://?api_token=token' \
--source-table 'users' \
--dest-uri duckdb:///pipedrive.duckdb \
--dest-table 'dest.users'
```

<img alt="pipedrive_img" src="../media/pipedrive.png"/>

pipedrive source allows ingesting the following resources into separate tables:

| Table           | PK | Inc Key | Inc Strategy | Details                                                                                                                                        |
| --------------- | ----------- | --------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |  
| `activities` | id | update_time | merge | Refers to scheduled events or tasks associated with deals, contacts, or organizations |
| `deals` | id | update_time | merge | Refers to potential sale or transaction that you can track through various stages |
| `persons` | id | update_time | merge | Refers individual contacts or leads that can be linked to sales deals |
| `organizations` | id | update_time | merge | Refers to company or entity with which you have potential or existing business dealings. |
| `products` | id | update_time | merge | Refers to items or services offered for sale that can be associated with deals |
| `users` | id | update_time | merge | Refers to Individual with a unique login credential who can access and use the platform |


Use these as `--source-table` parameter in the `ingestr ingest` command.
