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
- `email_activity_query` (optional): An additional SendGrid Email Activity query expression that is combined with the interval filter for the `messages` table.
- `stats_aggregated_by` (optional): Aggregation for the `global_stats` table. One of `day` (default), `week`, or `month`.

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
| `messages` | `msg_id` | `last_event_time` | merge | Email Activity. Server-side query on `last_event_time`. The endpoint has no pagination, so a single page of up to 1000 messages is returned. |
| `global_stats` | `date` | `date` | merge | Global email statistics. Server-side `start_date`/`end_date` filter. `--interval-start` is required. |
| `bounces` | `email`, `created` | `created` | merge | Bounced addresses. Server-side inclusive `start_time`/`end_time` Unix timestamp filter. |
| `lists` | `id` | - | replace | Marketing contact lists. No time filter; the table is fully replaced on each run. |
| `single_sends` | `id` | `updated_at` | merge | Marketing single sends. Filtered client-side on `updated_at`. |

Use one of these as the `--source-table` parameter in the `ingestr ingest` command.

## Incremental loading

SendGrid handles incrementality differently per table:

- `messages`: With both bounds, ingestr filters `last_event_time BETWEEN TIMESTAMP "<start>" AND TIMESTAMP "<end>"`; with a single bound it uses `>=` or `<=`. The endpoint has no documented pagination, so ingestr requests one page with `limit=1000` and warns if a full page is returned.
- `global_stats`: Requires `--interval-start` because SendGrid requires `start_date`. If `--interval-end` is omitted, the current UTC date is used. Dates are sent as `YYYY-MM-DD` with `aggregated_by` from the URI.
- `bounces`: Uses SendGrid's inclusive Unix timestamp filters `start_time`/`end_time` when intervals are provided; without them, all bounces are paged through.
- `single_sends`: SendGrid documents no server-side time filter for the list endpoint, so ingestr pages through all results and applies the interval filter on `updated_at` client-side.
- `lists`: Replace-only; SendGrid does not expose an update timestamp in the list response.

When no interval is provided, `bounces`, `lists`, and `single_sends` fetch all available records, `messages` returns the first page of up to 1000 records, and `global_stats` returns an error because `start_date` is required.

## Notes

- The `messages` table requires the SendGrid Email Activity feature and an API key with Email Activity access.
- SendGrid documents a hard limit of 6 requests per minute for the Email Activity API; ingestr applies a dedicated rate limiter for `messages` and a conservative limiter for the other v3 endpoints, relying on retries for 429 responses.
- The `contacts` endpoint is intentionally not included because SendGrid's current `GET /v3/marketing/contacts` endpoint returns only up to 50 recent contacts and documents contact pagination as deprecated.
- Nested objects and arrays are preserved as JSON strings in the destination columns.
