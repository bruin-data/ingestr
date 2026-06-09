# SendGrid

[SendGrid](https://sendgrid.com/) is Twilio's email delivery and marketing platform.

ingestr supports SendGrid as a source.

## URI format

The URI format for SendGrid is as follows:

```plaintext
sendgrid://?api_key=<api-key>
```

URI parameters:

- `api_key` (required): Twilio SendGrid API key. Basic authentication is not supported by SendGrid's v3 API.
- `on_behalf_of` (optional): Value for SendGrid's `on-behalf-of` header when using a parent account to query a subuser or customer account.
- `email_activity_query` (optional): Additional SendGrid Email Activity query expression to combine with the interval filter for the `messages` table.
- `stats_aggregated_by` (optional): Aggregation for the `global_stats` table. Supported values: `day`, `week`, `month`. Defaults to `day`.

Example:

```sh
ingestr ingest \
  --source-uri 'sendgrid://?api_key=SG.xxxxxx&stats_aggregated_by=day' \
  --source-table 'bounces' \
  --dest-uri duckdb:///sendgrid.duckdb \
  --dest-table 'sendgrid.bounces'
```

## Tables

| Table | PK | Incremental key | Strategy | Filtering |
|-------|----|-----------------|----------|-----------|
| `messages` | `msg_id` | `last_event_time` | merge | Server-side Email Activity query on `last_event_time`; one page only, max 1000 messages |
| `global_stats` | `date` | `date` | merge | Server-side `start_date` and `end_date`; `--interval-start` is required |
| `bounces` | `email`, `created` | `created` | merge | Server-side inclusive `start_time` and `end_time` Unix timestamps |
| `lists` | `id` | - | replace | No time filter; full list replacement |
| `single_sends` | `id` | `updated_at` | merge | Client-side filter on `updated_at`; SendGrid documents no list endpoint time filter |

Use these as the `--source-table` parameter in the `ingestr ingest` command.

## Incremental loading

SendGrid supports incrementality differently by table:

- `messages`: When `--interval-start` and/or `--interval-end` is provided, ingestr adds a SendGrid Email Activity query on `last_event_time`. With both values, it uses `last_event_time BETWEEN TIMESTAMP "<start>" AND TIMESTAMP "<end>"`. With only one bound, it uses `>=` or `<=` respectively. SendGrid does not document cursor or offset pagination for this endpoint, so ingestr requests one page with `limit=1000`.
- `global_stats`: Requires `--interval-start` because SendGrid requires `start_date`. If `--interval-end` is omitted, ingestr uses the current UTC date. Dates are sent as `YYYY-MM-DD` with `aggregated_by` from the URI.
- `bounces`: Uses SendGrid's inclusive Unix timestamp filters `start_time` and `end_time` when intervals are provided. Without intervals, ingestr pages through all bounces.
- `single_sends`: SendGrid returns `updated_at` but does not document a server-side time filter for the list endpoint. ingestr pages through results and applies the interval filter client-side.
- `lists`: Replace-only; SendGrid does not expose an update timestamp in the list response.

When no interval is provided:

- `messages` defaults to a broad `last_event_time>=TIMESTAMP "1970-01-01T00:00:00Z"` query and still returns only the first page of up to 1000 messages because the endpoint has no documented pagination.
- `global_stats` returns an error because `start_date` is required by SendGrid.
- `bounces`, `lists`, and `single_sends` fetch all available pages.

## Rate limits

SendGrid documents a specific Email Activity API limit of 6 requests per minute. ingestr uses a separate Email Activity client capped at 80% of that limit.

For the rest of the Web API v3, SendGrid documents endpoint-specific limits returned in `X-RateLimit-*` headers rather than a global numeric limit. ingestr applies a conservative local limiter and still relies on normal retry handling for 429 responses.

## Notes

- The `messages` table requires the SendGrid Email Activity feature and API key permissions for Email Activity access.
- The `contacts` endpoint is intentionally not included because SendGrid's current `GET /v3/marketing/contacts` endpoint returns only up to 50 recent contacts and states that contact pagination is deprecated. Use SendGrid's contact export workflows outside ingestr for full contact backups.
- Nested objects and arrays are preserved as JSON strings in destination columns.
