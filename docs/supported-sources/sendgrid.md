# SendGrid

[SendGrid](https://sendgrid.com/) is Twilio's email delivery and marketing platform.

ingestr supports SendGrid as a source.

## URI format

The URI format for SendGrid is as follows:

```plaintext
sendgrid://?api_key=<api-key>
```

URI parameters:

- `api_key` (required): Your Twilio SendGrid API key. Basic authentication is not supported by SendGrid's v3 API.
- `on_behalf_of` (optional): Value for SendGrid's `on-behalf-of` header, used when a parent account queries a subuser or customer account.

## Setting up a SendGrid Integration

To get your API key:

1. Log in to your [SendGrid account](https://app.sendgrid.com/).
2. Go to **Settings** > **API Keys**.
3. Click **Create API Key**, give it a name, and grant at least **Read Access** to the resources you want to ingest (Email Activity, Stats, Suppressions, and Marketing).
4. Copy the generated key — it is shown only once.

Once you have your API key, here's a sample command that will copy data from SendGrid into a DuckDB database:

```sh
ingestr ingest \
  --source-uri 'sendgrid://?api_key=SG.xxxxxx' \
  --source-table 'bounces' \
  --dest-uri duckdb:///sendgrid.duckdb \
  --dest-table 'sendgrid.bounces'
```

The result of this command will be a table in the `sendgrid.duckdb` database.

## Tables

SendGrid source allows ingesting the following sources into separate tables:

| Table | PK | Inc Key | Inc Strategy | Details |
| ----- | -- | ------- | ------------ | ------- |
| `messages` | `msg_id` | `last_event_time` | merge | Email Activity. Server-side query on `last_event_time`. The endpoint caps each response at 1000 records with no pagination, so ingestr recursively splits the time range until every window fits under the cap. |
| `global_stats` | `date` | `date` | merge | Global email statistics. Server-side `start_date`/`end_date` filter. `--interval-start` is required. Aggregation defaults to daily; use a `global_stats:week` or `global_stats:month` suffix for weekly/monthly grain. |
| `bounces` | `email`, `created` | `created` | merge | Bounced addresses. Server-side inclusive `start_time`/`end_time` Unix timestamp filter. |
| `lists` | `id` | - | replace | Marketing contact lists. No time filter; the table is fully replaced on each run. |
| `single_sends` | `id` | `updated_at` | merge | Marketing single sends. Filtered client-side on `updated_at`. |

Use one of these as the `--source-table` parameter in the `ingestr ingest` command.

The `global_stats` table accepts an optional granularity suffix — `global_stats`, `global_stats:week`, or `global_stats:month` (defaults to daily).

## Examples

Ingest weekly aggregated statistics from a given start date (`global_stats` requires `--interval-start`):

```sh
ingestr ingest \
  --source-uri 'sendgrid://?api_key=SG.xxxxxx' \
  --source-table 'global_stats:week' \
  --dest-uri duckdb:///sendgrid.duckdb \
  --dest-table 'sendgrid.global_stats' \
  --interval-start 2024-01-01
```

Ingest messages on behalf of a subuser using the `on-behalf-of` header:

```sh
ingestr ingest \
  --source-uri 'sendgrid://?api_key=SG.xxxxxx&on_behalf_of=my-subuser' \
  --source-table 'messages' \
  --dest-uri duckdb:///sendgrid.duckdb \
  --dest-table 'sendgrid.messages'
```

## Notes

- Intervals are **half-open `[start, end)`** — `--interval-start` is inclusive and `--interval-end` is exclusive across all tables. For example, `global_stats` with `--interval-start 2024-01-01 --interval-end 2024-01-09` returns Jan 1–8.
- The `messages` table requires the SendGrid Email Activity feature and an API key with Email Activity access.
- The Email Activity endpoint returns at most 1000 records per query and offers no pagination. ingestr works around this by recursively bisecting the `last_event_time` range — any window that comes back full is split in half until each piece is under the cap. Because the 6 req/min limit applies, very dense ranges can take a while; and if more than 1000 events share the same one-second instant, the surplus cannot be separated and a warning is emitted.
- SendGrid documents a hard limit of 6 requests per minute for the Email Activity API; ingestr applies a dedicated rate limiter for `messages` and a conservative limiter for the other v3 endpoints, relying on retries for 429 responses.
- The `contacts` endpoint is intentionally not included because SendGrid's current `GET /v3/marketing/contacts` endpoint returns only up to 50 recent contacts and documents contact pagination as deprecated.
- Nested objects and arrays are preserved as JSON strings in the destination columns.
