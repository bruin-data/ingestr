from datetime import timedelta
from typing import Iterable, Optional
from .tiktok_helpers import TikTokAPI
from sys import getsizeof

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

@dlt.source(max_table_nesting=0)
def tiktok_source(
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime,
    access_token,
    incremental_loading_param,
    advertiser_id,
    days,
    dimensions: list[str],
    metrics: list[str],
    filters: Optional[dict] = None
) -> DltResource:
    
    tiktok_api = TikTokAPI(access_token)

    @dlt.resource(write_disposition="merge", primary_key="dimensions")
    def custom_reports(
        datetime = dlt.sources.incremental(incremental_loading_param, start_date.isoformat())
        if incremental_loading_param in dimensions
        else None
    )-> Iterable[TDataItem]:
        current_date = start_date
        if datetime is not None:
            datetime_str = datetime.last_value
            current_date = ensure_pendulum_datetime(datetime_str)
        
        while current_date < end_date:
            interval_end = min(current_date + timedelta(days=days), end_date)
            
            try:
                for report in tiktok_api.fetch_reports(
                    start_time=current_date,
                    end_time=interval_end,
                    advertiser_id=advertiser_id,
                    dimensions=dimensions,
                    metrics=metrics,
                    filters=filters or {}
                ):
                    yield report
            except Exception as e:
                print(f"Error fetching reports: {e}")
            
            current_date = interval_end
                                    
    return custom_reports
