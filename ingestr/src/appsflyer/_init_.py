from datetime import datetime
from typing import Iterable

import dlt
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
import pendulum

from ingestr.src.appsflyer.client import AppsflyerClient
from dlt.common.time import ensure_pendulum_datetime


@dlt.source(max_table_nesting=0)
def appsflyer_source(
    api_key: str, start_date: str, end_date: str
) -> Iterable[DltResource]:
    start_date_obj = ensure_pendulum_datetime(start_date)
    client = AppsflyerClient(api_key)

    @dlt.resource(write_disposition="merge", merge_key="Install Time")
    def campaigns(
      updated=dlt.sources.incremental('["Install Time"]', start_date_obj.isoformat())
    ) -> Iterable[TDataItem]:
        updated_start_time = datetime.fromisoformat(updated.start_value).date()
        yield from client.fetch_campaigns(start_date=updated_start_time, end_date=end_date)

    @dlt.resource(write_disposition="merge", merge_key="Install Time")
    def creatives(
       updated=dlt.sources.incremental('["Install Time"]', start_date_obj.isoformat())
    ) -> Iterable[TDataItem]:
        updated_start_time = datetime.fromisoformat(updated.start_value).date()
        print("updated_start_time",updated_start_time)
        print("start date",updated_start_time)
        print("end date",end_date)
        yield from client.fetch_creatives(start_date= updated_start_time, end_date=end_date)

    return campaigns, creatives
