from typing import Any, Dict, List, Optional

from confluent_kafka import Consumer, Message, TopicPartition  # type: ignore
from confluent_kafka.admin import TopicMetadata  # type: ignore
from dlt import config
from dlt.common import pendulum
from dlt.common.configuration import configspec
from dlt.common.configuration.specs import CredentialsConfiguration
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import DictStrAny, TSecretValue
from dlt.common.utils import digest128


def default_msg_processor(msg: Message) -> Dict[str, Any]:
    """Basic Kafka message processor.

    Returns the message value and metadata. Timestamp consists of two values:
    (type of the timestamp, timestamp). Type represents one of the Python
    Kafka constants:
        TIMESTAMP_NOT_AVAILABLE - Timestamps not supported by broker.
        TIMESTAMP_CREATE_TIME - Message creation time (or source / producer time).
        TIMESTAMP_LOG_APPEND_TIME - Broker receive time.

    Args:
        msg (confluent_kafka.Message): A single Kafka message.

    Returns:
        dict: Processed Kafka message.
    """
    ts = msg.timestamp()
    topic = msg.topic()
    partition = msg.partition()
    key = msg.key()
    if key is not None:
        key = key.decode("utf-8")

    return {
        "_kafka": {
            "partition": partition,
            "topic": topic,
            "key": key,
            "offset": msg.offset(),
            "ts": {
                "type": ts[0],
                "value": ensure_pendulum_datetime(ts[1] / 1e3),
            },
            "data": msg.value().decode("utf-8"),
        },
        "_kafka_msg_id": digest128(topic + str(partition) + str(key)),
    }


class OffsetTracker(dict):  # type: ignore
    """Object to control offsets of the given topics.

    Tracks all the partitions of the given topics with two params:
    current offset and maximum offset (partition length).

    Args:
        consumer (confluent_kafka.Consumer): Kafka consumer.
        topic_names (List): Names of topics to track.
        pl_state (DictStrAny): Pipeline current state.
        start_from (Optional[pendulum.DateTime]): A timestamp, after which messages
            are read. Older messages are ignored.
    """

    def __init__(
        self,
        consumer: Consumer,
        topic_names: List[str],
        pl_state: DictStrAny,
        start_from: pendulum.DateTime = None,  # type: ignore
    ):
        super().__init__()

        self._consumer = consumer
        self._topics = self._read_topics(topic_names)

        # read/init current offsets
        self._cur_offsets = pl_state.setdefault(
            "offsets", {t_name: {} for t_name in topic_names}
        )

        self._init_partition_offsets(start_from)

    def _read_topics(self, topic_names: List[str]) -> Dict[str, TopicMetadata]:
        """Read the given topics metadata from Kafka.

        Reads all the topics at once, instead of requesting
        each in a separate call. Returns only those needed.

        Args:
            topic_names (list): Names of topics to be read.

        Returns:
            dict: Metadata of the given topics.
        """
        tracked_topics = {}
        topics = self._consumer.list_topics().topics

        for t_name in topic_names:
            tracked_topics[t_name] = topics[t_name]

        return tracked_topics

    def _init_partition_offsets(self, start_from: pendulum.DateTime) -> None:
        """Designate current and maximum offsets for every partition.

        Current offsets are read from the state, if present. Set equal
        to the partition beginning otherwise.

        Args:
            start_from (pendulum.DateTime): A timestamp, at which to start
                reading. Older messages are ignored.
        """
        all_parts = []
        for t_name, topic in self._topics.items():
            self[t_name] = {}

            # init all the topic partitions from the partitions' metadata
            parts = [
                TopicPartition(
                    t_name,
                    part,
                    start_from.int_timestamp * 1000 if start_from is not None else 0,
                )
                for part in topic.partitions
            ]

            # get offsets for the timestamp, if given
            if start_from is not None:
                ts_offsets = self._consumer.offsets_for_times(parts)

            # designate current and maximum offsets for every partition
            for i, part in enumerate(parts):
                max_offset = self._consumer.get_watermark_offsets(part)[1]

                if start_from is not None:
                    if ts_offsets[i].offset != -1:
                        cur_offset = ts_offsets[i].offset
                    else:
                        cur_offset = max_offset - 1
                else:
                    cur_offset = (
                        self._cur_offsets[t_name].get(str(part.partition), -1) + 1
                    )

                self[t_name][str(part.partition)] = {
                    "cur": cur_offset,
                    "max": max_offset,
                }

                parts[i].offset = cur_offset

            all_parts += parts

        # assign the current offsets to the consumer
        self._consumer.assign(all_parts)

    @property
    def has_unread(self) -> bool:
        """Check if there are unread messages in the tracked topics.

        Returns:
            bool: True, if there are messages to read, False if all
                the current offsets are equal to their maximums.
        """
        for parts in self.values():
            for part in parts.values():
                if part["cur"] + 1 < part["max"]:
                    return True

        return False

    def renew(self, msg: Message) -> None:
        """Update partition offset from the given message.

        Args:
            msg (confluent_kafka.Message): A read Kafka message.
        """
        topic = msg.topic()
        partition = str(msg.partition())

        offset = self[topic][partition]
        offset["cur"] = msg.offset()

        self._cur_offsets[topic][partition] = msg.offset()


@configspec
class KafkaCredentials(CredentialsConfiguration):
    """Kafka source credentials.

    NOTE: original Kafka credentials are written with a period, e.g.
    bootstrap.servers. However, KafkaCredentials expect them to
    use underscore symbols instead, e.g. bootstrap_servers.
    """

    bootstrap_servers: str = config.value
    group_id: str = config.value
    security_protocol: Optional[str] = None
    sasl_mechanisms: Optional[str] = None
    sasl_username: Optional[str] = None
    sasl_password: Optional[TSecretValue] = None

    def init_consumer(self) -> Consumer:
        """Init a Kafka consumer from this credentials.

        Returns:
            confluent_kafka.Consumer: an initiated consumer.
        """
        config = {
            "bootstrap.servers": self.bootstrap_servers,
            "group.id": self.group_id,
            "auto.offset.reset": "earliest",
        }

        if self.security_protocol:
            config["security.protocol"] = self.security_protocol
        if self.sasl_mechanisms:
            config["sasl.mechanisms"] = self.sasl_mechanisms
        if self.sasl_username:
            config["sasl.username"] = self.sasl_username
        if self.sasl_password:
            config["sasl.password"] = self.sasl_password

        return Consumer(config)
