from datetime import datetime
from typing import Sequence

import dlt
from dlt.sources import DltResource
from dlt.common.time import ensure_pendulum_datetime

from .helpers import DEFAULT_DIMENSIONS, AdjustAPI


@dlt.source(max_table_nesting=0)
def adjust_source(
    start_date: str,
    end_date: str,
    api_key: str,
) -> Sequence[DltResource]:
    start_date_obj = ensure_pendulum_datetime(start_date)
   
    @dlt.resource(write_disposition="merge", merge_key="day")
    def campaigns(updated=dlt.sources.incremental('day', start_date_obj.isoformat())):
        adjust_api = AdjustAPI(api_key=api_key)
        yield from adjust_api.fetch_report_data(
            start_date=updated.last_value,
            end_date=end_date,
        )

    @dlt.resource(write_disposition="merge", merge_key="day")
    def creatives(updated=dlt.sources.incremental('day', start_date_obj.isoformat())):
        dimensions = DEFAULT_DIMENSIONS + ["adgroup", "creative"]
        adjust_api = AdjustAPI(api_key=api_key)
    
        
        yield from adjust_api.fetch_report_data(
            start_date=updated.last_value, end_date=end_date, dimensions=dimensions
        )

    return campaigns, creatives
