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

To set up a Slack integration, you need to create a Slack App and obtain an API token with the necessary permissions.

### Step 1: Create a Slack App

1. Go to [Slack API Apps page](https://api.slack.com/apps)
2. Click **Create New App**
3. Choose **From scratch**
4. Enter an App Name (e.g., "Data Integration") and select your workspace
5. Click **Create App**

### Step 2: Configure OAuth Scopes

1. In the left sidebar, click **OAuth & Permissions**
2. Scroll down to **Scopes** → **Bot Token Scopes**
3. Add the following scopes based on the data you want to access:
   - `channels:read` - View basic information about public channels
   - `channels:history` - View messages in public channels
   - `users:read` - View users in the workspace
   - `team:read` - View team information
   - For access logs, you need Enterprise Grid and `admin.teams:read` scope

### Step 3: Install the App and Get Token

1. Scroll to the top of **OAuth & Permissions** page
2. Click **Install to Workspace**
3. Review and allow the permissions
4. Copy the **Bot User OAuth Token** (starts with `xoxb-`)

This token is your `api_key` for the ingestr URI.

Once you have the API key, let's say it is `axb-test-564`, here's a sample command that will copy the data from Slack into a DuckDB database:

```sh
ingestr ingest --source-uri 'slack://?api_key=axb-test-564' --source-table 'channels' --dest-uri duckdb:///slack.duckdb --dest-table 'dest.channels'
```

The result of this command will be a table in the `slack.duckdb` database.

## Tables

Slack source allows ingesting the following sources into separate tables:


| Table | PK | Inc Key | Inc Strategy | Details |
|-------|----|---------|--------------|---------|
| [channels](https://api.slack.com/methods/conversations.list) | id | - | replace | Retrieves information about all the channels |
| [users](https://api.slack.com/methods/users.list) | id | - | replace | Retrieves all the users|
| [messages:chan1,chan2](https://api.slack.com/methods/conversations.history) | ts | ts | append/merge | Retrieves messages from specified channels (e.g., general, memes). Multiple channels can be listed separated by commas |
| [access_logs](https://api.slack.com/methods/team.accessLogs) | user_id | - | append | Retrieves access logs|


Use these as `--source-table` parameter in the `ingestr ingest` command.
