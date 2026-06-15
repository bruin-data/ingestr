# RabbitMQ
[RabbitMQ](https://www.rabbitmq.com/) is an open-source message broker that implements the Advanced Message Queuing Protocol (AMQP). It is widely used for building distributed systems, microservices communication, and asynchronous task processing.

ingestr supports RabbitMQ as a source.

## URI format
The URI format for RabbitMQ is as follows:

```plaintext
amqp://username:password@host:port/vhost
```

URI parameters:
- `username`: Required, the username for authentication, e.g. `guest`.
- `password`: Required, the password for authentication, e.g. `guest`.
- `host`: Required, the RabbitMQ server hostname, e.g. `localhost`.
- `port`: The AMQP port, defaults to `5672`. For TLS connections (`amqps://`), the default is `5671`.
- `vhost`: The virtual host to connect to, defaults to `/`.

The `source-table` parameter specifies the queue name to consume messages from.

## TLS
For TLS-encrypted connections, use the `amqps://` scheme:

```plaintext
amqps://username:password@host:5671/vhost
```

## Output format
Each message is stored as a row with three columns:

| Column | Type | Description |
|--------|------|-------------|
| `data` | JSON | The message payload. JSON messages are stored as structured JSON; non-JSON messages are stored as strings. |
| `metadata` | JSON | AMQP metadata including `exchange`, `routing_key`, `content_type`, `delivery_tag`, `message_id`, `timestamp`, and `headers`. |
| `msg_id` | VARCHAR | The message ID if set by the producer, otherwise a SHA256-based hash of the message content and delivery tag. |

## Sample command
Once you have your RabbitMQ server running, here's a sample command to ingest messages from a queue into a DuckDB database:

```sh
ingestr ingest \
    --source-uri 'amqp://guest:guest@localhost:5672/' \
    --source-table 'my_queue' \
    --dest-uri 'duckdb://./rabbitmq.duckdb' \
    --dest-table 'dest.my_queue'
```

The result of this command will be a table in the `rabbitmq.duckdb` database with JSON columns containing the message data and metadata.

## Streaming ingestion

Add `--stream` to consume the queue continuously instead of reading what is currently available and exiting. In streaming mode each message is projected into a fixed envelope schema — a `msg_id` primary key plus a JSON `data` column holding the decoded body and metadata — and changes are merged on `msg_id` (latest record per key wins within a flush window, ordered by an `_ingestr_order` column) (so at-least-once redeliveries are idempotent). Deliveries are acknowledged only after a flush succeeds. See [`ingest --stream`](../commands/ingest.md#streaming-ingestion).

```sh
ingestr ingest \
    --source-uri 'amqp://guest:guest@localhost:5672/' \
    --source-table 'my_queue' \
    --dest-uri 'duckdb://./rabbitmq.duckdb' \
    --dest-table 'dest.my_queue' \
    --stream
```
