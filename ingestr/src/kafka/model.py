from typing import List, Optional, Tuple, Union

import attrs
import toolz  # type: ignore[import-untyped]
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.utils import digest128


@attrs.define
class KafkaDecodingOptions:
    """
    Options that control decoding of the Kafka event.
    """

    # The data type of the Kafka event `key` field.
    # Possible values: `json`.
    key_type: Optional[str] = None

    # The data type of the Kafka event `value` field.
    # Possible values: `json`.
    value_type: Optional[str] = None

    # The output format/layout.
    # Possible values: `standard_v1`, `standard_v2`, `flexible`.
    format: Optional[str] = None

    # Which fields to include in the output, comma-separated.
    # Using this option will automatically select the `flexible` output format.
    include: List[str] = attrs.field(factory=list)

    # Which field to select (pick) into the output.
    # Using this option will automatically select the `flexible` output format.
    select: Optional[str] = None

    def __attrs_post_init__(self):
        if self.format is None:
            self.format = "standard_v1"

    @classmethod
    def from_params(
        cls,
        key_type: List[str],
        value_type: List[str],
        format: List[str],
        include: List[str],
        select: List[str],
    ):
        """
        Read options from CLI parameters.
        """
        output_format = None
        include_fields = []
        select_field = None
        if format:
            output_format = format[0]
        if include:
            output_format = "flexible"
            include_fields = list(map(str.strip, include[0].split(",")))
        if select:
            output_format = "flexible"
            select_field = select[0]
        return cls(
            key_type=key_type and key_type[0] or None,
            value_type=value_type and value_type[0] or None,
            format=output_format,
            include=include_fields,
            select=select_field,
        )


@attrs.define
class KafkaEvent:
    """
    Process and decode a typical Kafka event/message/record.

    https://kafka.apache.org/intro#intro_concepts_and_terms
    """

    ts: Tuple[int, int]
    topic: str
    partition: int
    offset: int
    key: Union[bytes, str]
    value: Union[bytes, str]

    def decode_text(self):
        """
        Assume `key` and `value` are bytes but decode from UTF-8 well.
        """
        if self.key is not None and isinstance(self.key, bytes):
            self.key = self.key.decode("utf-8")
        if self.value is not None and isinstance(self.value, bytes):
            self.value = self.value.decode("utf-8")

    def to_dict(self, options: KafkaDecodingOptions):
        """
        Convert Kafka event to designated output format/layout.
        """
        # TODO: Make decoding from text optional.
        self.decode_text()

        message_id = digest128(self.topic + str(self.partition) + str(self.key))

        # The standard message layout as defined per dlt and ingestr.
        standard_payload = {
            "partition": self.partition,
            "topic": self.topic,
            "key": self.key,
            "offset": self.offset,
            "ts": {
                "type": self.ts[0],
                "value": ensure_pendulum_datetime(self.ts[1] / 1e3),
            },
            "data": self.value,
        }

        # Basic Kafka message processors providing two formats.
        # Returns the message value and metadata.
        # The legacy format `standard_v1` uses the field `_kafka_msg_id`,
        # while the future `standard_v2` format uses `_kafka__msg_id`,
        # better aligned with all the other fields.
        #
        # Currently, as of July 2025, `standard_v1` is used as the
        # default to not cause any breaking changes.

        if options.format == "standard_v1":
            UserWarning(
                "Future versions of ingestr will use the `standard_v2` output format. "
                "To retain compatibility, make sure to start using `format=standard_v1` early."
            )
            return {
                "_kafka": standard_payload,
                "_kafka_msg_id": message_id,
            }

        if options.format == "standard_v2":
            standard_payload["msg_id"] = message_id
            return {
                "_kafka": standard_payload,
            }

        # Slightly advanced Kafka message processor providing basic means of projections.
        # include: A list of event attributes to include.
        # select: A single event attribute to select and drill down into.
        #         Use `select=value` to relay the message payload data only.
        if options.format == "flexible":
            if options.include:
                # TODO: Need to cache this variable?
                include_keys = [
                    key == "value" and "data" or key for key in options.include
                ]
                return toolz.keyfilter(lambda k: k in include_keys, standard_payload)
            if options.select:
                # TODO: Instead of a simple dictionary getter, `jsonpointer` or `jqlang`
                #       can provide easy access to deeper levels of nested data structures.
                key = options.select.replace("value", "data")
                return standard_payload.get(key)
            return standard_payload

        raise NotImplementedError(f"Unknown message processor format: {options.format}")
