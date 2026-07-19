# NATS JetStream

[NATS](https://nats.io/) is a messaging system with a persistent streaming layer called JetStream. ingestr reads messages from JetStream streams in finite batch runs or continuously with streaming ingestion.

JetStream must be enabled on the NATS server. Core NATS subjects without a backing JetStream stream are not supported because they cannot replay messages or acknowledge them after a destination write.

## URI format

```text
nats://username:password@host:4222?subject=events.>&durable=ingestr_events
```

The required `source-table` is the JetStream stream name. Embedded callers that allow an empty table request can instead supply it with the `stream` URI parameter.

URI parameters:

- `stream`: JetStream stream-name fallback for embedded callers. The CLI still requires `source-table`.
- `subject`: Subject or wildcard filter within the stream. Defaults to `>`.
- `durable`: Durable consumer name used by streaming ingestion. If omitted, ingestr derives one from the stream name.
- `consumer` or `consumer_name`: Bind to an existing durable pull consumer.
- `bind` or `bind_consumer`: Bind to the consumer named by `durable` instead of creating or updating it.
- `token`: NATS authentication token.
- `credentials`, `creds`, or `credentials_file`: Path to a NATS credentials file.
- `batch_size`: Maximum messages fetched at once. Defaults to `3000`.
- `batch_timeout`: Maximum fetch wait in seconds. Defaults to `5` and accepts fractional seconds.

Username/password authentication can be supplied in the URI authority. For example:

```text
nats://ingestr:secret@nats.example.com:4222?subject=orders.*
```

## Output format

For finite batch ingestion, each message produces these columns:

| Column | Type | Description |
|---|---|---|
| `nats_msg_id` | VARCHAR | Stable ID derived from the JetStream stream name and stream sequence. |
| `data` | JSON | Decoded JSON payload, or a string when the payload is not JSON. |
| `nats` | JSON | Stream, consumer, subject, sequence, timestamp, delivery count, and headers. |

A finite run captures the stream's last sequence when the read starts and exits after reaching that cutoff. Messages published later are left for a subsequent run.

## Sample command

```sh
ingestr ingest \
    --source-uri 'nats://localhost:4222?subject=events.>' \
    --source-table EVENTS \
    --dest-uri duckdb:///nats.duckdb \
    --dest-table dest.events
```

## Streaming ingestion

Add `--stream` to consume continuously. Streaming mode uses a fixed envelope with a stable `msg_id`, a JSON `data` column containing the payload and NATS metadata, and an `_ingestr_order` sequence column. The default merge strategy de-duplicates redeliveries by `msg_id`.

Messages use explicit JetStream acknowledgements. ingestr acknowledges them only after the destination flush succeeds, so a failed or interrupted flush can be safely replayed.

```sh
ingestr ingest \
    --source-uri 'nats://localhost:4222?subject=events.>&durable=ingestr_events' \
    --source-table EVENTS \
    --dest-uri duckdb:///nats.duckdb \
    --dest-table dest.events \
    --stream
```

When binding an existing consumer, it must be a pull consumer with explicit acknowledgements. Its acknowledgement wait must also be long enough for the configured destination flush interval.
