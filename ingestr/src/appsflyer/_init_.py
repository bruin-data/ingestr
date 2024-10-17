from datetime import datetime, timedelta
from typing import Iterable

import dlt
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from ingestr.src.appsflyer.client import AppsflyerClient


@dlt.source(max_table_nesting=0)
def appsflyer_source(
    api_key: str, start_date: str, end_date: str
) -> Iterable[DltResource]:
    start_date_obj = ensure_pendulum_datetime(start_date)
    client = AppsflyerClient(api_key)

    @dlt.resource(write_disposition="merge", merge_key="Install Time")
    def campaigns(
        updated=dlt.sources.incremental('["Install Time"]', start_date_obj.isoformat()),
    ) -> Iterable[TDataItem]:
        current_start_time = datetime.fromisoformat(updated.start_value).date()
        end_date_time = datetime.fromisoformat(end_date).date()
        
        while current_start_time < end_date_time:
            current_end_time = current_start_time + timedelta(days=30)
            next_end_date = min(current_end_time, end_date_time)
            yield from client.fetch_campaigns(
                start_date=current_start_time.isoformat(), end_date=next_end_date.isoformat()
            )
            print(current_start_time, next_end_date)
            current_start_time = next_end_date

    @dlt.resource(write_disposition="merge", merge_key="Install Time")
    def creatives(
        updated=dlt.sources.incremental('["Install Time"]', start_date_obj.isoformat()),
    ) -> Iterable[TDataItem]:
        current_start_time = datetime.fromisoformat(updated.start_value).date()
        end_date_time = datetime.fromisoformat(end_date).date()

        while current_start_time < end_date_time:
            current_end_time = current_start_time + timedelta(days=30)
            next_end_date = min(current_end_time, end_date_time)
            yield from client.fetch_creatives(
                start_date=current_start_time.isoformat(), end_date=next_end_date.isoformat()
            )
            current_start_time = next_end_date
            
    return campaigns, creatives
