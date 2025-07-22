# InfluxDB
[InfluxDB](https://www.influxdata.com/) is a time series database optimized for storing high throughput metrics.

ingestr supports InfluxDB as a source.

## URI format

```plaintext
influxdb://<host>:<port>?token=<token>&org=<org>&bucket=<bucket>&secure=<secure>
```

URI parameters:
- `host`: The host address of the database server.
- `port`: The port number the database server is listening on. If you do not specify a port, the default is 8086 for self-hosted InfluxDB and 443 for InfluxDB Cloud.
- `token`: Authentication token.
- `org`: Name of the organization.
- `bucket`: Bucket that stores the measurements.
- `secure`: Optional. Use HTTPS when `true` (default) or HTTP when `false`.

The `<measurement>` name should be provided as the value for `--source-table`.

## Example

Copy cpu metrics from InfluxDB into DuckDB:

```sh
ingestr ingest \
    --source-uri 'influxdb://eu-central-1-0.aws.cloud3.influxdata.com?token=my-token&org=my-org&bucket=metrics&secure=false' \
    --source-table 'cpu' \
    --dest-uri duckdb:///metrics.duckdb \
    --dest-table 'dest.cpu'
```

## Tables

The InfluxDB source accepts any measurement name. Specify the measurement as the `--source-table` option when running `ingestr`.

> [!NOTE]
> Primary Key Required for Merge: When using `--incremental-strategy merge`, you must also specify `--primary-key` Otherwise, the strategy defaults to append.