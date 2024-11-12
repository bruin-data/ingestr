# Slack

[Slack](https://www.Slack.com/) is a messaging platform for teams and organizations where they can collaborate, share ideas and information.

ingestr supports Slack as a source.

## URI format

The URI format for Slack is as follows:

```plaintext
slack://?api_key=<api-key-here>
```

URI parameters:

- `api_key`: The API key used for authentication with the Slack API.

The URI is used to connect to the Slack API for extracting data.

## Setting up a Slack integration

Slack requires a few steps to set up an integration, please follow the guide dltHub [has built here](https://dlthub.com/docs/dlt-ecosystem/verified-sources/Slack#setup-guide).

Once you complete the guide, you should have an API key with the necessary permissions as mentioned in the guide. Let's say your API key is axb-test-564. Here's a sample command that will copy the data from Slack into a DuckDB database:

```sh
ingestr ingest --source-uri 'slack://?api_key=axb-test-564' --source-table 'channels' --dest-uri duckdb:///slack.duckdb --dest-table 'dest.channels'
```

The result of this command will be a table in the `slack.duckdb` database.

## Tables

Slack source allows ingesting the following sources into separate tables:

- `channels`: Retrieves information about all the channels.
- `users`: Retrieves information about all the users.
- `messages:chan1,chan2`: Retrieves messages from specified channels, where chan1 and chan2 represent user-defined channels, e.g: general, memes. Multiple channels can be listed.
- `access_logs`: Retrieves all the access logs.

Use these as `--source-table` parameter in the `ingestr ingest` command.
