from typing import Iterable

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from ingestr.src.klaviyo.helpers import split_date_range

from .client import MixpanelClient


@dlt.source(max_table_nesting=0)
def mixpanel_source(
    username: str,
    password: str,
    project_id: str,
    start_date: str,
    end_date: str | None = None,
) -> Iterable[DltResource]:
    client = MixpanelClient(username, password, project_id)
    start = pendulum.parse(start_date) if start_date else pendulum.datetime(2020, 1, 1)

    @dlt.resource(write_disposition="merge", name="events", primary_key="distinct_id")
    def events(
        date=dlt.sources.incremental(
            "time",
            initial_value=start.int_timestamp,
            end_value=pendulum.parse(end_date).int_timestamp if end_date else None,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        if date.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = pendulum.from_timestamp(date.end_value)

        start_dt = pendulum.from_timestamp(date.last_value)

        intervals = split_date_range(start_dt, end_dt)
        for s, e in intervals:
            yield from client.fetch_events(
                pendulum.parse(s).format("YYYY-MM-DD"),
                pendulum.parse(e).format("YYYY-MM-DD"),
            )

    @dlt.resource(write_disposition="merge", primary_key="distinct_id", name="profiles")
    def profiles(
        last_seen=dlt.sources.incremental(
            "last_seen",
            start.format("YYYY-MM-DD"),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        yield from client.fetch_profiles(pendulum.parse(last_seen.start_value))

    return events, profiles
