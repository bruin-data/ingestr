# Amazon SQS

[Amazon Simple Queue Service](https://docs.aws.amazon.com/sqs/) is a managed message queue for decoupling applications and moving messages between services.

ingestr supports Amazon SQS as a source.

## URI format

```plaintext
sqs://?region=<region>
```

URI parameters:
- `access_key_id` / `secret_access_key`: Optional AWS static credentials. You can also use `aws_access_key_id` / `aws_secret_access_key`.
- `session_token`: Optional AWS session token. You can also use `aws_session_token`.
- `region`: AWS region for the queue. You can also use `region_name` or `aws_region`.
- `endpoint_url`: Optional custom endpoint, useful for LocalStack.
- `visibility_timeout`: Optional message visibility timeout in seconds while ingestr buffers records. Defaults to 300.
- `wait_time_seconds`: Optional SQS long-poll wait time. Defaults to 2.

The `--source-table` value is the queue name. A full queue URL is also accepted.

## Authentication

When no static credentials are provided in the URI, ingestr uses the AWS SDK default credential chain. That supports environment variables, shared AWS config and credentials files, web identity credentials, and IAM role credentials on EC2, ECS, EKS, and similar AWS runtimes.

Static credentials can still be provided explicitly:

```plaintext
sqs://?access_key_id=<aws-access-key-id>&secret_access_key=<aws-secret-access-key>&region=<region>
```

## Sample command

```sh
ingestr ingest \
    --source-uri 'sqs://?region=us-east-1' \
    --source-table 'orders' \
    --dest-uri duckdb:///sqs.duckdb \
    --dest-table 'dest.orders'
```

## Streaming ingestion

Add `--stream` to consume the queue continuously. In streaming mode each message is projected into a fixed envelope schema: `msg_id`, JSON `data`, and `_ingestr_order`. Messages are deleted from SQS only after a destination flush succeeds.

```sh
ingestr ingest \
    --source-uri 'sqs://?region=us-east-1' \
    --source-table 'orders' \
    --dest-uri duckdb:///sqs.duckdb \
    --dest-table 'dest.orders' \
    --stream
```
