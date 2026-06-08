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
- `sasl_mechanisms`: The SASL mechanism to be used for authentication. Supported values are `PLAIN`, `SCRAM-SHA-256`, `SCRAM-SHA-512`, and `OAUTHBEARER` (for AWS MSK IAM authentication).
- `sasl_username`: The username for SASL authentication (not used with `OAUTHBEARER`).
- `sasl_password`: The password for SASL authentication (not used with `OAUTHBEARER`).
- `batch_size`: The number of messages to fetch in a single batch, defaults to 3000.
- `batch_timeout`: The maximum time to wait for messages, defaults to 3 seconds.

The URI is used to connect to the Kafka brokers for ingesting messages.

### AWS MSK IAM authentication
For [Amazon MSK](https://aws.amazon.com/msk/) clusters that use IAM access control, set `sasl_mechanisms=OAUTHBEARER`. ingestr generates a short-lived IAM auth token for each connection using the AWS MSK IAM signer. MSK IAM is served over TLS (typically port `9098`), and TLS is enabled automatically for this mechanism.

Additional URI parameters for MSK IAM:
- `aws_region`: Required, the AWS region of the MSK cluster, e.g. `us-east-1`.
- `aws_role_arn`: Optional, an IAM role ARN to assume for generating the token.
- `aws_role_session_name`: Optional, the STS session name used when assuming `aws_role_arn`.
- `aws_profile`: Optional, an AWS named profile to load credentials from.
- `aws_access_key_id` / `aws_secret_access_key`: Optional static credentials. Both are required together.
- `aws_session_token`: Optional session token to use with static credentials.

When none of the credential parameters are provided, the default AWS credential chain is used (environment variables, shared config, and EC2/ECS/EKS instance roles). This is the common setup when ingestr runs inside AWS with an attached IAM role.

> On EKS, the default chain also covers [IRSA](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html) (IAM Roles for Service Accounts) and EKS Pod Identity automatically — just set `aws_region` and let the service account's IAM role provide credentials.

```sh
ingestr ingest \
    --source-uri 'kafka://?bootstrap_servers=b-1.mycluster.kafka.us-east-1.amazonaws.com:9098&group_id=test_group&sasl_mechanisms=OAUTHBEARER&aws_region=us-east-1' \
    --source-table 'my-topic' \
    --dest-uri duckdb:///kafka.duckdb \
    --dest-table 'dest.my_topic'
```

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
