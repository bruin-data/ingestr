# Kafka benchmarks

The local Kafka source uses a five-partition topic named `bench_data_json_{size}`
on `localhost:9094`. Each message contains a JSON object with the standard DuckDB
benchmark fields; `json_val` contains a nested JSON object. Dates and timestamps
are encoded as strings; numeric fields remain JSON numbers.

Run the Kafka scenarios against PostgreSQL and DuckDB:

```bash
bash benchmarks/scripts/run.sh --scenarios '*kafka*' --tools gong dlt airbyte spark --validate
bash benchmarks/scripts/run.sh --scenarios '*kafka*' --tools gong dlt spark --rows 1000000
bash benchmarks/scripts/run.sh --scenarios '*kafka*' --tools gong dlt --validate --rows 100000
```

The standard setup starts the broker along with the other benchmark services and
seeds the selected size. Seeding reuses a complete topic; an incomplete topic is
deleted and rebuilt. These `bench_data_json_*` topics are dedicated benchmark
fixtures. Retention is disabled so repeated runs read the same records.

To seed only Kafka after creating the DuckDB fixture and starting the broker:

```bash
docker compose -f benchmarks/docker-compose.yml up -d --wait bench-kafka-source
uv run --no-project --python 3.13 --script benchmarks/scripts/seed_kafka.py \
  --database benchmarks/duckdb_files/bench_source.duckdb --rows 1000 --suffix 1k
bash benchmarks/scripts/run.sh --skip-setup --scenarios '*kafka*' --tools gong --validate
```

gong's Kafka batch ingestion preserves the message in `_kafka.data` with metadata in
`_kafka` and a unique `_kafka_msg_id`. Validation checks the row count, envelope
columns, payload fields, and the sum of IDs extracted from the JSON payload. Batch reads consume
the full retained backlog each time; these benchmarks do not use `--stream` or
consumer-group checkpoints.

| Tool | Kafka read path | Destinations |
| --- | --- | --- |
| gong | Existing Kafka source, `_kafka` envelope | PostgreSQL, DuckDB |
| dlt | Verified Kafka source, `_kafka` envelope | PostgreSQL, DuckDB |
| Spark | Native Kafka batch source, `data` column | PostgreSQL, DuckDB |
| Airbyte | Official Kafka Docker connector, `value` column | PostgreSQL |

Old Python ingestr remains skipped. Sling is excluded from Kafka scenarios because
it has no native Kafka connector. Historical Sling Kafka results used a custom
JSONL adapter and are not native connector comparisons.
dlt uses a fresh pipeline state per invocation, Spark explicitly reads earliest to
latest, and Airbyte uses a fresh consumer group. Airbyte's existing DuckDB and 10m
skip rules still apply, as do Spark's existing 1m/10m DuckDB skips.

Airbyte connects through the Docker listener on port 9095. On Linux, set
`BENCH_KAFKA_DOCKER_HOST` to the Docker bridge gateway address before starting
Compose; macOS defaults to `host.docker.internal`. Custom Kafka sources must supply
broker addresses reachable from the Airbyte connector container.
