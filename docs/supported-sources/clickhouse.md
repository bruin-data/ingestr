# ClickHouse
ClickHouse is a fast, open-source, column-oriented database management system that allows for high performance data ingestion and querying.

Ingestr supports ClickHouse as a destination.

## URI format
The URI format for ClickHouse as a destination is as follows:

```plaintext
clickhouse://<username>:<password>@<host>:<http_port>/<database>?secure=<secure>
```
## URI parameters:
- `host` (required): The host of Clickhouse server.
- `http_port` (optional): The HTTP port of Clickhouse.
- `database` (required): The name of the ClickHouse database to connect to.
- `username` (required): The username of Clickhouse server.
- `password` (required): The password of Clickhouse server.
- `secure` (optional): The secure connection of Clickhouse server.

ClickHouse requires an `database`, `username` `password` `host` to connect to the ClickHouse server. Please follow the guide to obtain the [credentials](https://dlthub.com/docs/dlt-ecosystem/destinations/clickhouse#2-setup-clickhouse-database). Once you've completed the guide, you should have all the above-mentioned credentials.

This is a sample command that will copy the data from the Stripe source into ClickHouse.

```sh
ingestr ingest \
    --source-uri "stripe://?api_key=key123" \
    --source-table 'event' \
    --dest-uri "clickhouse://localhost:8123/db_123?username=user_123&password=password_123&secure=0" \
    --dest-table 'dest.stripe_event'
```

This command will retrieve data for the specified date range and save it to the `dest.stripe_event` table in the ClickHouse database.


