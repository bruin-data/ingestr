# ClickHouse
ClickHouse is a fast, open-source, column-oriented database management system that allows for high performance data ingestion and querying.

ingestr supports ClickHouse as a source and destination.

## URI format
The URI format for ClickHouse as a source is as follows:

```plaintext
clickhouse://<username>:<password>@<host>:<port>?secure=<secure>
```

The URI format for ClickHouse as a destination is as follows:

```plaintext
clickhouse://<username>:<password>@<host>:<port>?http_port=<http_port>&secure=<secure>
```

## URI parameters:
- `username` (required): The username is required to authenticate with the ClickHouse server.
- `password` (required): The password is required to authenticate the provided username.
- `host` (required): The hostname or IP address of the ClickHouse server where the database is hosted.
- `port` (required): The TCP port number used by the ClickHouse server.
- `http_port` (optional): The port number to use when connecting to the ClickHouse server's HTTP interface. By default, it is set to port `8443`. It should only be used when ClickHouse is the destination. 
- `secure` (optional): Set to `1` for a secure HTTPS connection or `0` for a non-secure HTTP connection. By default, it is set to `1`.

ClickHouse requires a `username`, `password`, `host` and `port` to connect to the ClickHouse server. For more information, read [here](https://dlthub.com/docs/dlt-ecosystem/destinations/clickhouse#2-setup-clickhouse-database). Once you've completed the guide, you should have all the above-mentioned credentials.

```
ingestr ingest \
    --source-uri "stripe://?api_key=key123" \
    --source-table 'event' \
    --dest-uri "clickhouse://user_123:pass123@localhost:9000" \
    --dest-table 'stripe.event'
```

This is a sample command that will copy the data from the Stripe source into Athena.

<img alt="clickhouse_img" src="../media/clickhouse_img.png" />

<!-- 
    see https://github.com/dlt-hub/dlt/issues/2248
-->
> [!WARNING]
> Clickhouse currently doesn't support `delete+insert`, `merge` or `scd2` incremental strategies.
