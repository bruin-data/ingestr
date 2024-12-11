# Apache Kafka
[Apache Kafka](https://kafka.apache.org/) is a distributed event streaming platform used by thousands of companies for high-performance data pipelines, streaming analytics, data integration, and mission-critical applications.

ingestr supports Apache Kafka as a source.

## URI format
The URI format for Apache Kafka is as follows:

```plaintext
kafka://?bootstrap_servers=localhost:9092&group_id=test_group&security_protocol=SASL_SSL&sasl_mechanisms=PLAIN&sasl_username=example_username&sasl_password=example_secret&batch_size=1000&batch_timeout=3
```

URI parameters:
- `bootstrap_servers`: Required, the Kafka server or servers to connect to, typically in the form of a host and port, e.g. `localhost:9092`
- `group_id`: Required, the consumer group ID used for identifying the client when consuming messages.
- `security_protocol`: The protocol used to communicate with brokers, e.g. `SASL_SSL` for secure communication.
- `sasl_mechanisms`: The SASL mechanism to be used for authentication, e.g. `PLAIN`.
- `sasl_username`: The username for SASL authentication.
- `sasl_password`: The password for SASL authentication.
- `batch_size`: The number of messages to fetch in a single batch, defaults to 3000.
- `batch_timeout`: The maximum time to wait for messages, defaults to 3 seconds.

The URI is used to connect to the Kafka brokers for ingesting messages.

### Group ID
The group ID is used to identify the consumer group that reads messages from a topic. Kafka uses the group ID to manage consumer offsets and assign partitions to consumers, which means that the group ID is the key to reading messages from the correct partition and position in the topic.

Once you have your Kafka server, credentials, and group ID set up, here's a sample command to ingest messages from a Kafka topic into a DuckDB database:

```sh
ingestr ingest \
    --source-uri 'kafka://?bootstrap_servers=localhost:9092&group_id=test_group' \
    --source-table 'my-topic' \
    --dest-uri duckdb:///kafka.duckdb \
    --dest-table 'dest.my_topic'
```

The result of this command will be a table in the `kafka.duckdb` database with JSON columns.
