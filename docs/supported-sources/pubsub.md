# Google Cloud Pub/Sub

[Google Cloud Pub/Sub](https://cloud.google.com/pubsub/docs) is a managed messaging service for event-driven systems and real-time data integration.

ingestr supports Pub/Sub pull subscriptions as a source.

## URI format

```plaintext
pubsub://<project-id>
```

URI parameters:
- `credentials_file` / `credentials_path`: Optional path to a Google service account credentials file.
- `credentials_base64`: Optional base64-encoded Google service account JSON credentials.
- `endpoint`: Optional custom endpoint, useful for the Pub/Sub emulator.
- `ack_deadline_seconds`: Optional ack deadline while ingestr buffers records. Defaults to 300.
- `pull_timeout_seconds`: Optional timeout for each pull request. Defaults to 2.

The `--source-table` value is the subscription ID. A full subscription name like `projects/my-project/subscriptions/my-subscription` is also accepted.

## Authentication

When no credentials are provided in the URI, ingestr uses Google Application Default Credentials. That supports `GOOGLE_APPLICATION_CREDENTIALS`, `gcloud auth application-default login`, and service account credentials on Google Cloud runtimes.

Explicit service account credentials can be passed with either a file path:

```plaintext
pubsub://my-project?credentials_path=/path/to/service-account.json
```

or base64-encoded JSON:

```plaintext
pubsub://my-project?credentials_base64=<base64-service-account-json>
```

## Sample command

```sh
ingestr ingest \
    --source-uri 'pubsub://my-project' \
    --source-table 'orders-subscription' \
    --dest-uri duckdb:///pubsub.duckdb \
    --dest-table 'dest.orders'
```

## Streaming ingestion

Add `--stream` to consume the subscription continuously. In streaming mode each message is projected into a fixed envelope schema: `msg_id`, JSON `data`, and `_ingestr_order`. Pub/Sub messages are acknowledged only after a destination flush succeeds.

```sh
ingestr ingest \
    --source-uri 'pubsub://my-project' \
    --source-table 'orders-subscription' \
    --dest-uri duckdb:///pubsub.duckdb \
    --dest-table 'dest.orders' \
    --stream
```
