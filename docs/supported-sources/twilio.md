# Twilio

[Twilio](https://www.twilio.com/) is a cloud communications platform for messaging, voice, and phone numbers.

ingestr supports Twilio as a source.

## URI format

The URI format for Twilio is as follows:

```plaintext
twilio://?account_sid=<account-sid>&auth_token=<auth-token>
```

URI parameters:

- `account_sid` (required): Your Twilio Account SID (starts with `AC`).
- `auth_token`: Your Twilio Auth Token. Required unless `api_key`/`api_secret` are provided.
- `api_key` / `api_secret`: An API Key SID and Secret, used instead of the Auth Token. When `api_key` is set, `api_secret` is required.

Authentication uses HTTP Basic Auth. API Key + Secret is preferred; Account SID + Auth Token is the fallback. Either way the Account SID stays in the request path.

## Setting up a Twilio Integration

To get your credentials:

1. Log in to the [Twilio Console](https://console.twilio.com/).
2. Find your **Account SID** and **Auth Token** in the **Account Info** panel on the dashboard.
3. Optionally create an API Key under **Account > API keys & tokens** and use `api_key`/`api_secret` instead of the Auth Token.

Once you have your credentials, here's a sample command that will copy data from Twilio into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'twilio://?account_sid=ACxxxxxx&auth_token=xxxxxx' \
  --source-table 'messages' \
  --dest-uri duckdb:///twilio.duckdb \
  --dest-table 'twilio.messages'
```

The result of this command will be a table in the `twilio.duckdb` database.

## Tables

Twilio source allows ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `messages` | `sid` | - | replace | SMS and MMS messages. |
| `calls` | `sid` | `date_updated` | merge | Voice calls. |
| `recordings` | `sid` | `date_updated` | merge | Call recordings, including deleted ones. |
| `incoming_phone_numbers` | `sid` | - | replace | Phone numbers owned by the account. |
| `usage_records` | - | - | replace | Usage totals per category over the account lifetime. Accepts an optional granularity suffix (see below). |

Use one of these as the `--source-table` parameter in the `ingestr ingest` command.

### Usage record granularity

`usage_records` accepts an optional granularity suffix that switches to Twilio's `Daily`/`Monthly`/`Yearly` sub-resources:

| Source table | Grain | Inc Key | Inc Strategy |
| ------------ | ----- | ------- | ------------ |
| `usage_records` | lifetime total per category | - | replace |
| `usage_records:daily` | one row per category per day | `start_date` | merge |
| `usage_records:monthly` | one row per category per month | `start_date` | merge |
| `usage_records:yearly` | one row per category per year | `start_date` | merge |

The granular variants load incrementally by period: past periods are loaded once and the current period is refreshed on each run.

## Incremental loading

When `--interval-start`/`--interval-end` are not provided, all records are loaded. When an interval is provided:

- `calls`, `recordings`, and `usage_records:daily`/`:monthly`/`:yearly` load the records that fall within it (the incrementally-loaded tables).
- `messages`, `incoming_phone_numbers`, and `usage_records` ignore the interval and always load the full table. `messages` is loaded in full because redactions (which don't change `date_updated`) and deletions can't be detected incrementally via the Twilio API.

## Examples

Ingest daily usage records from a given start date:

```sh
ingestr ingest \
  --source-uri 'twilio://?account_sid=ACxxxxxx&auth_token=xxxxxx' \
  --source-table 'usage_records:daily' \
  --dest-uri duckdb:///twilio.duckdb \
  --dest-table 'twilio.usage_records_daily' \
  --interval-start 2024-01-01
```

Authenticate with an API Key + Secret instead of the Auth Token:

```sh
ingestr ingest \
  --source-uri 'twilio://?account_sid=ACxxxxxx&api_key=SKxxxxxx&api_secret=xxxxxx' \
  --source-table 'calls' \
  --dest-uri duckdb:///twilio.duckdb \
  --dest-table 'twilio.calls'
```
