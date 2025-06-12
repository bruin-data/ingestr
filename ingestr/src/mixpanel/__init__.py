from typing import Iterable

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .client import MixpanelClient


@dlt.source(max_table_nesting=0)
def mixpanel_source(
    username: str,
    password: str,
    project_id: str,
    server: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None = None,
) -> Iterable[DltResource]:
    client = MixpanelClient(username, password, project_id, server)

    @dlt.resource(write_disposition="merge", name="events", primary_key="distinct_id")
    def events(
        date=dlt.sources.incremental(
            "time",
            initial_value=start_date.int_timestamp,
            end_value=end_date.int_timestamp if end_date else None,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        if date.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = pendulum.from_timestamp(date.end_value)

        start_dt = pendulum.from_timestamp(date.last_value)

        yield from client.fetch_events(
            start_dt,
            end_dt,
        )

    @dlt.resource(write_disposition="merge", primary_key="distinct_id", name="profiles")
    def profiles(
        last_seen=dlt.sources.incremental(
            "last_seen",
            initial_value=start_date,
            end_value=end_date,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        if last_seen.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = last_seen.end_value

        start_dt = last_seen.last_value
        yield from client.fetch_profiles(start_dt, end_dt)

    return events, profiles
