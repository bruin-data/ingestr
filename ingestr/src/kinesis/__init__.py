"""Reads messages from Kinesis queue."""

from typing import Iterable, List, Optional

import dlt
from dlt.common import json, pendulum
from dlt.common.configuration.specs import AwsCredentials
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import StrStr, TAnyDateTime, TDataItem
from dlt.common.utils import digest128

from .helpers import get_shard_iterator, max_sequence_by_shard


@dlt.resource(
    name=lambda args: args["stream_name"],
    primary_key="kinesis_msg_id",
    standalone=True,
    max_table_nesting=0,
)
def kinesis_stream(
    stream_name: str,
    initial_at_timestamp: TAnyDateTime,
    credentials: AwsCredentials,
    last_msg: Optional[dlt.sources.incremental[StrStr]] = dlt.sources.incremental(
        "kinesis", last_value_func=max_sequence_by_shard
    ),
    max_number_of_messages: int = None,  # type: ignore
    milliseconds_behind_latest: int = 1000,
    parse_json: bool = True,
    chunk_size: int = 1000,
) -> Iterable[TDataItem]:
    """Reads a kinesis stream and yields messages. Supports incremental loading. Parses messages as json by default.

    Args:
        stream_name (str): The name of the stream to read from. If not provided, the
            value must be present in config/secrets
        credentials (AwsCredentials): The credentials to use to connect to kinesis. If not provided,
            the value from secrets or credentials present on the device will be used.
        last_msg (Optional[dlt.sources.incremental]): An incremental over a mapping from shard_id to message sequence
            that will be used to create shard iterators of type AFTER_SEQUENCE_NUMBER when loading incrementally.
        initial_at_timestamp (TAnyDateTime): An initial timestamp used to generate AT_TIMESTAMP or LATEST iterator when timestamp value is 0
        max_number_of_messages (int): Maximum number of messages to read in one run. Actual read may exceed that number by up to chunk_size. Defaults to None (no limit).
        milliseconds_behind_latest (int): The number of milliseconds behind the top of the shard to stop reading messages, defaults to 1000.
        parse_json (bool): If True, assumes that messages are json strings, parses them and returns instead of `data` (otherwise). Defaults to False.
        chunk_size (int): The number of records to fetch at once. Defaults to 1000.
    Yields:
            Iterable[TDataItem]: Messages. Contain Kinesis envelope in `kinesis` and bytes data in `data` (if `parse_json` disabled)

    """
    session = credentials._to_botocore_session()
    # the default timeouts are (60, 60) which is fine
    kinesis_client = session.create_client("kinesis")
    # normalize at_timestamp to pendulum
    initial_at_datetime = (
        None
        if initial_at_timestamp is None
        else ensure_pendulum_datetime(initial_at_timestamp)
    )
    # set it in state
    resource_state = dlt.current.resource_state()
    initial_at_datetime = resource_state.get(
        "initial_at_timestamp", initial_at_datetime
    )
    # so next time we request shards at AT_TIMESTAMP that is now
    resource_state["initial_at_timestamp"] = pendulum.now("UTC").subtract(seconds=1)

    shards_list = kinesis_client.list_shards(StreamName=stream_name)
    shards: List[StrStr] = shards_list["Shards"]
    while next_token := shards_list.get("NextToken"):
        shards_list = kinesis_client.list_shards(NextToken=next_token)
        shards.extend(shards_list)

    shard_ids = [shard["ShardId"] for shard in shards]

    # get next shard to fetch messages from
    while shard_id := shard_ids.pop(0) if shard_ids else None:
        shard_iterator, _ = get_shard_iterator(
            kinesis_client,
            stream_name,
            shard_id,
            last_msg,  # type: ignore
            initial_at_datetime,  # type: ignore
        )

        while shard_iterator:
            records = []
            records_response = kinesis_client.get_records(
                ShardIterator=shard_iterator,
                Limit=chunk_size,  # The size of data can be up to 1 MB, it must be controlled by the user
            )

            for record in records_response["Records"]:
                sequence_number = record["SequenceNumber"]
                content = record["Data"]

                arrival_time = record["ApproximateArrivalTimestamp"]
                arrival_timestamp = arrival_time.astimezone(pendulum.UTC)

                message = {
                    "kinesis": {
                        "shard_id": shard_id,
                        "seq_no": sequence_number,
                        "ts": ensure_pendulum_datetime(arrival_timestamp),
                        "partition": record["PartitionKey"],
                        "stream_name": stream_name,
                    },
                    "kinesis_msg_id": digest128(shard_id + sequence_number),
                }

                if parse_json:
                    message.update(json.loadb(content))
                else:
                    message["data"] = content
                records.append(message)
            yield records

            # do not load more  max_number_of_messages
            if max_number_of_messages is not None:
                max_number_of_messages -= len(records)
                if max_number_of_messages <= 0:
                    return

            # add child shards so we can request messages from them
            child_shards = records_response.get("ChildShards", None)
            if child_shards:
                for child_shard in child_shards:
                    child_shard_id = child_shard["ShardId"]
                    if child_shard_id not in shards:
                        shard_ids.append(child_shard_id)

            # gets 0 when no messages so we cutoff empty shards
            records_ms_behind_latest = records_response.get("MillisBehindLatest", 0)
            if records_ms_behind_latest < milliseconds_behind_latest:
                # stop taking messages from shard
                shard_iterator = None  # type: ignore
            else:
                # continue taking messages
                shard_iterator = records_response["NextShardIterator"]
