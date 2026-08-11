# Amplitude

[Amplitude](https://amplitude.com/) is a product analytics platform for tracking user behavior across web and mobile applications.

ingestr supports Amplitude as a source.

## URI format

```plaintext
amplitude://?api_key=<api_key>&secret_key=<secret_key>&region=<region>
```

URI parameters:

- `api_key`: The project's API key.
- `secret_key`: The project's secret key.
- `region`: (Optional) The data region of your Amplitude project. Can be `us` or `eu`. Defaults to `us`.

Both `api_key` and `secret_key` are found in your Amplitude project under Settings → Projects → (your project). They are used together for HTTP Basic authentication.

## Example

Copy events from Amplitude into a DuckDB database:

```sh
ingestr ingest \
    --source-uri 'amplitude://?api_key=my-api-key&secret_key=my-secret-key' \
    --source-table 'events' \
    --dest-uri duckdb:///amplitude.duckdb \
    --dest-table 'amplitude.events'
```

## Tables

Amplitude source allows ingesting the following tables:

| Table              | PK             | Inc Key      | Inc Strategy | Details                                                        |
| ------------------ | -------------- | ------------ | ------------ | -------------------------------------------------------------- |
| `events`           | uuid           | server_upload_time | merge  | Raw events from the [Export API](https://amplitude.com/docs/apis/analytics/export). |
| `cohorts`          | id             |              | replace      | Behavioral cohorts defined in the project.                     |
| `annotations`      | id             |              | replace      | Chart annotations.                                             |
| `event_types`      | event_type     |              | replace      | Event taxonomy — event types.                                  |
| `event_categories` | id             |              | replace      | Event taxonomy — event categories.                             |
| `event_properties` | event_property |              | replace      | Event taxonomy — event properties.                             |
| `user_properties`  | user_property  |              | replace      | User taxonomy — user properties.                               |

Use these as `--source-table` values in the `ingestr ingest` command.

The `events` table is loaded incrementally. The Export API windows events by `server_upload_time` (when Amplitude received them, not when they occurred), so ingestion tracks that field to reliably capture late-arriving events. When no interval is given, ingestr loads the last 30 days. The Export API only makes data available roughly two hours after it is received, so the most recent events may not yet be present.
