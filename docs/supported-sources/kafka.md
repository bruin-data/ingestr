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
- `security_protocol`: The protocol used to communicate with brokers, e.g. `SASL_SSL` for secure communication. `OAUTHBEARER` automatically enables TLS unless this is set to `SASL_PLAINTEXT`.
- `sasl_mechanisms`: The SASL mechanism to be used for authentication. Supported values are `PLAIN`, `SCRAM-SHA-256`, `SCRAM-SHA-512`, and `OAUTHBEARER`.
- `sasl_username`: The username for SASL authentication.
- `sasl_password`: The password for SASL authentication.
- `aws_region`: Required for `OAUTHBEARER`; the AWS region used to sign Amazon MSK IAM tokens.
- `aws_role_arn`: Optional for `OAUTHBEARER`; IAM role to assume before signing.
- `aws_role_session_name`: Optional for `OAUTHBEARER`; STS session name when `aws_role_arn` is supplied.
- `aws_profile`: Optional for `OAUTHBEARER`; AWS named profile used for signing.
- `aws_access_key_id`: Optional for `OAUTHBEARER`; static AWS access key ID.
- `aws_secret_access_key`: Optional for `OAUTHBEARER`; static AWS secret access key. Must be provided with `aws_access_key_id`.
- `aws_session_token`: Optional for `OAUTHBEARER`; static AWS session token.
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

### Amazon MSK IAM
Amazon MSK clusters that use IAM access control require `OAUTHBEARER` and usually listen on port `9098`:

```sh
ingestr ingest \
    --source-uri 'kafka://?bootstrap_servers=b-1.mycluster.kafka.us-east-1.amazonaws.com:9098&group_id=test_group&sasl_mechanisms=OAUTHBEARER&aws_region=us-east-1' \
    --source-table 'my-topic' \
    --dest-uri duckdb:///kafka.duckdb \
    --dest-table 'dest.my_topic'
```

When no AWS credential parameters are supplied, ingestr uses the default AWS credential chain, including environment variables, shared config, EC2/ECS/EKS roles, IRSA, and EKS Pod Identity.
