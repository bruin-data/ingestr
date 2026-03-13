# Apache Kafka
[Apache Kafka](https://kafka.apache.org/) is a distributed event streaming platform used by thousands of companies for high-performance data pipelines, streaming analytics, data integration, and mission-critical applications.

ingestr supports Apache Kafka as a source.

## URI format
The URI format for Apache Kafka is as follows:

```plaintext
kafka://?bootstrap_servers=localhost:9092&group_id=test_group&security_protocol=SASL_SSL&sasl_mechanisms=PLAIN&sasl_username=example_username&sasl_password=example_secret&batch_size=1000&batch_timeout=3
```

### URI parameters

Connectivity options:
- `bootstrap_servers`: Required, the Kafka server or servers to connect to, typically in the form of a host and port, e.g. `localhost:9092`
- `group_id`: Required, the consumer group ID used for identifying the client when consuming messages.
- `security_protocol`: The protocol used to communicate with brokers, e.g. `SASL_SSL` for secure communication.
- `sasl_mechanisms`: The SASL mechanism to be used for authentication, e.g. `PLAIN`.
- `sasl_username`: The username for SASL authentication.
- `sasl_password`: The password for SASL authentication.

Transfer options:
- `batch_size`: The number of messages to fetch in a single batch, defaults to 3000.
- `batch_timeout`: The maximum time to wait for messages, defaults to 3 seconds.

Decoding options:
- `key_type`: The data type of the Kafka event `key` field. Possible values: `json`.
- `value_type`: The data type of the Kafka event `value_type` field. Possible values: `json`.
- `include`: A list of event attributes to include in the output, comma-separated.
- `select`: A single event attribute to select and drill down into.
  Use `select=value` to relay the Kafka event **payload data** only.
- `format`: The output format/layout. Possible values: `standard_v1` (default),
  `standard_v2`, `flexible`. When using the `include` or `select` option, the
  decoder will automatically select the `flexible` output format.

The URI is used to connect to the Kafka brokers for ingesting messages.

### Group ID
The group ID is used to identify the consumer group that reads messages from a topic. Kafka uses the group ID to manage consumer offsets and assign partitions to consumers, which means that the group ID is the key to reading messages from the correct partition and position in the topic.

## Examples

### Kafka to DuckDB

Once you have your Kafka server, credentials, and group ID set up,
here are a few sample commands to ingest messages from a Kafka topic into a destination database:

Transfer data using the traditional `standard_v1` output format into DuckDB.
The result of this command will be a table in the `kafka.duckdb` database with JSON columns.
```sh
ingestr ingest \
    --source-uri 'kafka://?bootstrap_servers=localhost:9092&group_id=test' \
    --source-table 'my-topic' \
    --dest-uri 'duckdb:///kafka.duckdb' \
    --dest-table 'dest.my_topic'
```

### Kafka to PostgreSQL

Transfer data converging the Kafka event `value` into a PostgreSQL destination
table, after decoding from JSON, using the `flexible` output format.
```sh
echo '{"sensor_id":1,"ts":"2025-06-01 10:00","reading":42.42}' | kcat -P -b localhost -t demo
```
```sh
ingestr ingest \
  --source-uri 'kafka://?bootstrap_servers=localhost:9092&group_id=test&value_type=json&select=value' \
  --source-table 'demo' \
  --dest-uri 'postgres://postgres:postgres@localhost:5432/?sslmode=disable' \
  --dest-table 'public.kafka_demo'
```
The result of this command will be the `public.kafka_demo` table using
the Kafka event `value`'s top-level JSON keys as table columns.
```sh
psql "postgresql://postgres:postgres@localhost:5432/" \
  -c '\d+ public.kafka_demo' \
  -c 'select * from public.kafka_demo;'
```
```text
       Table "public.kafka_demo"

    Column    |           Type           |
--------------+--------------------------+
 sensor_id    | bigint                   |
 ts           | timestamp with time zone |
 reading      | double precision         |
 _dlt_load_id | character varying        |
 _dlt_id      | character varying        |
```
