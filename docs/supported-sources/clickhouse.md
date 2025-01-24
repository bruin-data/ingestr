# ClickHouse
ClickHouse is a fast, open-source, column-oriented database management system that allows for high performance data ingestion and querying.

Ingestr supports ClickHouse as a destination.

## URI format
The URI format for ClickHouse as a destination is as follows:

```plaintext
clickhouse://<username>:<password>@<host>:<port>
```
## URI parameters:
- `username` (required): The username is required to authenticate with the ClickHouse server.
- `password` (required): The password is required to authenticate the provided username.
- `host` (required): The hostname or IP address of the ClickHouse server where the database is hosted.
- `port` (required): The TCP port number used by the ClickHouse server.


ClickHouse requires a `username`, `password`, `host`, `port`, and `database` to connect to the ClickHouse server. For more information, read [here](https://dlthub.com/docs/dlt-ecosystem/destinations/clickhouse#2-setup-clickhouse-database). Once you've completed the guide, you should have all the above-mentioned credentials.

```
ingestr ingest \
    --source-uri "stripe://?api_key=key123" \
    --source-table 'event' \
    --dest-uri "clickhouse://user_123:pass123@localhost:9000" \
    --dest-table 'stripe.event'
```

## Important Note for Local Development

Clickhouse's HTTP interface defaults to port 8123. If you're running Clickhouse in Docker, you must map the container's port 8123 to a port on your host (e.g., 8888) to access the HTTP interface.

In such cases, Ingestr requires the `http_port` query parameter in your connection string to specify the host port mapped to 8123.

For example, if you've mapped port 8123 to 8888 on your host, your connection string should be:

```
clickhouse://user:pass@localhost:9000?http_port=8888
```

This parameter is only needed when Clickhouse is running in Docker and its HTTP port has been mapped to a different host port. By default, Ingestr will attempt to connect to Clickhouse's HTTP interface on port 8123.

This is a sample command that will copy the data from the Stripe source into Athena.

<img alt="clickhouse_img" src="../media/clickhouse_img.png" />
