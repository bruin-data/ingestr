"""A source to extract Kafka messages.

When extraction starts, partition length is checked -
data is read only up to it, overriding the default Kafka's
behavior of waiting for new messages in endless loop.
"""

from contextlib import closing
from typing import Any, Callable, Dict, Iterable, List, Optional, Union

import dlt
from confluent_kafka import Consumer, Message  # type: ignore
from dlt.common import logger
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem

from .helpers import (
    KafkaCredentials,
    KafkaEventProcessor,
    OffsetTracker,
)


@dlt.resource(
    name="kafka_messages",
    table_name=lambda msg: msg["_kafka"]["topic"],
    standalone=True,
)
def kafka_consumer(
    topics: Union[str, List[str]],
    credentials: Union[KafkaCredentials, Consumer] = dlt.secrets.value,
    msg_processor: Optional[Callable[[Message], Dict[str, Any]]] = None,
    batch_size: Optional[int] = 3000,
    batch_timeout: Optional[int] = 3,
    start_from: Optional[TAnyDateTime] = None,
) -> Iterable[TDataItem]:
    """Extract recent messages from the given Kafka topics.

    The resource tracks offsets for all the topics and partitions,
    and so reads data incrementally.

    Messages from different topics are saved in different tables.

    Args:
        topics (Union[str, List[str]]): Names of topics to extract.
        credentials (Optional[Union[KafkaCredentials, Consumer]]):
            Auth credentials or an initiated Kafka consumer. By default,
            is taken from secrets.
        msg_processor(Optional[Callable]): A function-converter,
            which will process every Kafka message after it is read and
            before it is transferred to the destination.
        batch_size (Optional[int]): Messages batch size to read at once.
        batch_timeout (Optional[int]): Maximum time to wait for a batch
            to be consumed in seconds.
        start_from (Optional[TAnyDateTime]): A timestamp, at which to start
            reading. Older messages are ignored.

    Yields:
        Iterable[TDataItem]: Kafka messages.
    """
    msg_processor = msg_processor or KafkaEventProcessor().process

    if not isinstance(topics, list):
        topics = [topics]

    if isinstance(credentials, Consumer):
        consumer = credentials
    elif isinstance(credentials, KafkaCredentials):
        consumer = credentials.init_consumer()
    else:
        raise TypeError(
            (
                "Wrong credentials type provided. Need to be of type: "
                "KafkaCredentials or confluent_kafka.Consumer"
            )
        )

    if start_from is not None:
        start_from = ensure_pendulum_datetime(start_from)

    tracker = OffsetTracker(consumer, topics, dlt.current.resource_state(), start_from)  # type: ignore

    # read messages up to the maximum offsets,
    # not waiting for new messages
    with closing(consumer):
        while True:
            messages = consumer.consume(batch_size, timeout=batch_timeout)
            if not messages:
                break

            batch = []
            for msg in messages:
                if msg.error():
                    err = msg.error()
                    if err.retriable() or not err.fatal():
                        logger.warning(f"ERROR: {err} - RETRYING")
                    else:
                        raise err
                else:
                    batch.append(msg_processor(msg))  # type: ignore
                    tracker.renew(msg)

            yield batch

            if tracker.has_unread is False:
                return
