# /// script
# requires-python = ">=3.12"
# dependencies = ["confluent-kafka==2.11.1", "duckdb==1.3.2"]
# ///
"""Seed a dedicated Kafka benchmark topic with JSON objects from DuckDB."""

import argparse
import time

import duckdb
from confluent_kafka import Consumer, Producer, TopicPartition
from confluent_kafka.admin import AdminClient, NewTopic


def seed(brokers, database, rows, suffix, partitions):
    with duckdb.connect(database, read_only=True) as connection:
        actual_rows = connection.execute(f'SELECT count(*) FROM "bench_data_{suffix}"').fetchone()[0]
        if actual_rows != rows:
            raise ValueError(f"DuckDB fixture has {actual_rows} rows, expected {rows}")
    topic = f"bench_data_json_{suffix}"
    admin = AdminClient({"bootstrap.servers": brokers})
    metadata = admin.list_topics(timeout=30)
    if topic in metadata.topics:
        consumer = Consumer({"bootstrap.servers": brokers, "group.id": "bench-seed-check"})
        try:
            offsets = [consumer.get_watermark_offsets(TopicPartition(topic, p), timeout=30)
                       for p in metadata.topics[topic].partitions]
        finally:
            consumer.close()
        if (len(offsets) == partitions and all(low == 0 for low, _ in offsets)
                and sum(high for _, high in offsets) == rows):
            print(f"Kafka: {topic} already seeded ({rows} messages)")
            return
        admin.delete_topics([topic], operation_timeout=30)[topic].result(timeout=60)
        deadline = time.monotonic() + 60
        while topic in admin.list_topics(timeout=10).topics:
            if time.monotonic() >= deadline:
                raise TimeoutError(f"Topic deletion did not finish: {topic}")
            time.sleep(1)

    admin.create_topics([NewTopic(topic, num_partitions=partitions, replication_factor=1,
                                 config={"retention.ms": "-1", "retention.bytes": "-1"})],
                        operation_timeout=30)[topic].result(timeout=60)
    producer = Producer({"bootstrap.servers": brokers, "linger.ms": 20,
                         "batch.size": 1048576, "compression.type": "lz4",
                         "queue.buffering.max.kbytes": 16384,
                         "enable.idempotence": True})
    errors = []

    def delivered(error, message):
        if error is not None:
            errors.append(str(error))

    count = 0
    with duckdb.connect(database, read_only=True) as connection:
        cursor = connection.execute(
            f'SELECT id, to_json(t) FROM '
            f'(SELECT * REPLACE (json_val::JSON AS json_val) FROM "bench_data_{suffix}") t'
        )
        while batch := cursor.fetchmany(1000):
            for row_id, payload in batch:
                while True:
                    if errors:
                        raise RuntimeError(errors[0])
                    try:
                        producer.produce(topic, key=str(row_id), value=payload, on_delivery=delivered)
                        break
                    except BufferError:
                        producer.poll(0.1)
                producer.poll(0)
                count += 1
    remaining = producer.flush(120)
    if remaining or errors or count != rows:
        raise RuntimeError(f"Kafka seed failed: {count}/{rows} rows, {remaining} pending, errors={errors[:1]}")
    print(f"Kafka: seeded {topic} ({count} JSON messages, {partitions} partitions)")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--brokers", default="localhost:9094")
    parser.add_argument("--database", required=True)
    parser.add_argument("--rows", type=int, required=True)
    parser.add_argument("--suffix", required=True)
    parser.add_argument("--partitions", type=int, default=5)
    args = parser.parse_args()
    if args.rows <= 0 or args.partitions <= 0 or not args.suffix.isalnum():
        parser.error("rows and partitions must be positive; suffix must be alphanumeric")
    seed(args.brokers, args.database, args.rows, args.suffix, args.partitions)
