from typing import Iterable

import dlt
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource

from ingestr.src.klaviyo.helpers import KlaviyoAPI

@dlt.source(max_table_nesting=0)
def klaviyo_source(api_key: str, start_date: TAnyDateTime) -> Iterable[DltResource]:
    start_date_obj = ensure_pendulum_datetime(start_date)

    @dlt.resource(write_disposition="append", primary_key="id")
    def events(
        datetime=dlt.sources.incremental("datetime", start_date_obj.isoformat()),
    ) -> Iterable[TDataItem]:
        yield from KlaviyoAPI.fetch_data_event(
            endpoint="events", datetime=datetime.start_value, api_key=api_key
        )

    @dlt.resource(write_disposition="merge", primary_key="id")
    def profiles(
        updated=dlt.sources.incremental("updated", start_date_obj.isoformat()),
    ) -> Iterable[TDataItem]:
        yield from KlaviyoAPI.fetch_data_profiles(
            endpoint="profiles", updated=updated.start_value, api_key=api_key
        )

    return events, profiles

