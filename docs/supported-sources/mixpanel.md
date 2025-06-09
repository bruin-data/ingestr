# Mixpanel

[Mixpanel](https://mixpanel.com/) is an analytics service for tracking user interactions in web and mobile applications.

ingestr supports Mixpanel as a source.

## URI format

```plaintext
mixpanel://?api_secret=<api_secret>&project_id=<project_id>
```

URI parameters:

- `api_secret`: The API secret for your Mixpanel project.
- `project_id`: The numeric project ID.

The URI is used to connect to the Mixpanel API for extracting data.

## Example

Copy events from Mixpanel into a DuckDB database:

```sh
ingestr ingest \
    --source-uri 'mixpanel://?api_secret=secret&project_id=12345' \
    --source-table 'events' \
    --dest-uri duckdb:///mixpanel.duckdb \
    --dest-table 'mixpanel.events' \
    --interval-start 2024-01-01
```

## Tables

Mixpanel source allows ingesting the following tables:

- `events`: Raw event data returned from the export API. Loaded incrementally.
- `profiles`: User profiles from the Engage API. Loaded incrementally based on `$last_seen`.

Use these as `--source-table` values in the `ingestr ingest` command.
