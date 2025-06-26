# Zoom

[Zoom](https://www.zoom.com/) is a video conferencing and collaboration platform.

ingestr supports Zoom as a source.

## Prerequisites
- A [Zoom Server-to-Server OAuth App](https://developers.zoom.us/docs/internal-apps/s2s-oauth/)
- Appropriate permissions related to meetings and users must be added in the app's scopes
`user:read,user:write,user:read:admin,user:write:admin`
- Obtain the `client_id`, `client_secret` and `account_id`  credentials from the app

## URI format
```plaintext
zoom://?client_id=<client_id>&client_secret=<client_secret>&account_id=<account_id>
```

This command copies meetings data from the Zoom source to DuckDB.

```sh
ingestr ingest \
  --source-uri 'zoom://?client_id=abc&client_secret=xyz&account_id=123' \
  --source-table 'meetings' \
  --dest-uri duckdb:///zoom.duckdb \
  --dest-table 'dest.meetings'
```

<img alt="zoom" src="../media/zoom_ingestion.png"/>
## Tables

Zoom source allows ingesting the following tables:

- `users`: Retrieve a list your account's users.
- `meetings`: Retrieves all valid previous meetings, live meetings, and upcoming scheduled meetings for all users of the given Zoom account.


Use these as the `--source-table` parameter in the `ingestr ingest` command.
