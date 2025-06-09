from typing import Iterable

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from ingestr.src.klaviyo.helpers import split_date_range

from .client import MixpanelClient


@dlt.source(max_table_nesting=0)
def mixpanel_source(api_secret: str, project_id: str, start_date: str | None = None, end_date: str | None = None) -> Iterable[DltResource]:
    client = MixpanelClient(api_secret, project_id)
    start = pendulum.parse(start_date) if start_date else pendulum.datetime(2000, 1, 1)

    @dlt.resource(write_disposition="append", name="events")
    def events(
        date=dlt.sources.incremental(
            "date",
            start.format("YYYY-MM-DD"),
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        end = pendulum.parse(end_date) if end_date else pendulum.now()
        intervals = split_date_range(pendulum.parse(date.start_value), end)
        for s, e in intervals:
            yield from client.fetch_events(pendulum.parse(s), pendulum.parse(e))

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
