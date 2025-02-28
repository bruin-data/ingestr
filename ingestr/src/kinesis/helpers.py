from typing import Any, Sequence, Tuple

import dlt
from dlt.common import pendulum
from dlt.common.typing import DictStrAny, StrAny, StrStr


def get_shard_iterator(
    kinesis_client: Any,
    stream_name: str,
    shard_id: str,
    last_msg: dlt.sources.incremental[StrStr],
    initial_at_timestamp: pendulum.DateTime | None,
) -> Tuple[str, StrAny]:
    """Gets shard `shard_id` of `stream_name` iterator. If `last_msg` incremental is present it may
    contain last message sequence for shard_id. in that case AFTER_SEQUENCE_NUMBER is created.
    If no message sequence is present, `initial_at_timestamp` is used for AT_TIMESTAMP or LATEST.
    The final fallback is TRIM_HORIZON
    """
    sequence_state = (
        {} if last_msg is None else last_msg.last_value or last_msg.initial_value or {}
    )
    iterator_params: DictStrAny
    msg_sequence = sequence_state.get(shard_id, None)
    if msg_sequence:
        iterator_params = dict(
            ShardIteratorType="AFTER_SEQUENCE_NUMBER",
            StartingSequenceNumber=msg_sequence,
        )
    elif initial_at_timestamp is None:
        # Fetch all records from the beginning
        iterator_params = dict(ShardIteratorType="TRIM_HORIZON")

    elif initial_at_timestamp.timestamp() == 0.0:
        # will sets to latest i.e only the messages at the tip of the stream are read
        iterator_params = dict(ShardIteratorType="LATEST")
    else:
        iterator_params = dict(
            ShardIteratorType="AT_TIMESTAMP", Timestamp=initial_at_timestamp.timestamp()
        )

    shard_iterator: StrStr = kinesis_client.get_shard_iterator(
        StreamName=stream_name, ShardId=shard_id, **iterator_params
    )
    return shard_iterator["ShardIterator"], iterator_params


def max_sequence_by_shard(values: Sequence[StrStr]) -> StrStr:
    """A last_value_function that operates on mapping of shard_id:msg_sequence defining the max"""
    last_value = None
    # if tuple/list contains only one element then return it
    if len(values) == 1:
        item = values[0]
    else:
        # item is kinesis metadata, last_value is previous state of the shards
        item, last_value = values

    if last_value is None:
        last_value = {}
    else:
        last_value = dict(last_value)  # always make a copy
    shard_id = item["shard_id"]
    # we compare message sequence at shard_id
    last_value[shard_id] = max(item["seq_no"], last_value.get(shard_id, ""))
    return last_value
