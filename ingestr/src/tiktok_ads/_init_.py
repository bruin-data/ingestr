from datetime import timedelta
from typing import Iterable, Optional

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .tiktok_helpers import TikTokAPI

def find_intervals(
    current_date: pendulum.DateTime,
    end_date: pendulum.DateTime,
    interval_days: int,
    by_hour=False,
):
    intervals = []
    print("start_day",current_date)
    print("end_date",end_date)
    while current_date <= end_date:
        if by_hour:
            interval_end = current_date.end_of("day")
            if interval_end > end_date:
                interval_end = end_date
            intervals.append((current_date.start_of("day"), interval_end))
            current_date = current_date.add(days=1)
        else:
            interval_end = min(current_date.add(days=interval_days), end_date)
            intervals.append((current_date, interval_end))
            current_date = interval_end.add(days=1)

    return intervals

def fetch_tiktok_reports(
    tiktok_api: TikTokAPI,
    current_date: pendulum.DateTime,
    interval_end: pendulum.DateTime,
    advertiser_id: str,
    dimensions: list[str],
    metrics: list[str],
    filters: Optional[dict] | None,
) -> Iterable[TDataItem]:
    try:
        yield from tiktok_api.fetch_reports(
            start_time=current_date,
            end_time=interval_end,
            advertiser_id=advertiser_id,
            dimensions=dimensions,
            metrics=metrics,
            filters=filters,
        )
    except Exception as e:
        raise RuntimeError(f"Error fetching TikTok report: {e}")


@dlt.source(max_table_nesting=0)
def tiktok_source(
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime,
    access_token: str,
    advertiser_id: str,
    time_zone:str,
    dimensions: list[str],
    metrics: list[str],
    filters=None,
) -> DltResource:
    tiktok_api = TikTokAPI(access_token=access_token,time_zone=time_zone)
    incremental_loading_param = ""
    is_incremental = False
    interval_days = 365
    if "stat_time_day" in dimensions:
        incremental_loading_param = "stat_time_day"
        is_incremental = True
        interval_days = 30

    if "stat_time_hour" in dimensions:
        incremental_loading_param = "stat_time_hour"
        is_incremental = True
        interval_days = 0

    @dlt.resource(write_disposition="merge", primary_key=dimensions)
    def custom_reports(
        datetime=dlt.sources.incremental(
            incremental_loading_param, start_date
        )
        if is_incremental
        else None,
    ) -> Iterable[TDataItem]:
        current_date = start_date.in_tz(time_zone)

        if datetime is not None:
            datetime_str = datetime.last_value
            current_date = ensure_pendulum_datetime(datetime_str).in_tz(time_zone)
        
        list_of_interval = find_intervals(current_date=current_date,end_date=end_date,interval_days=interval_days)
        
        for start, end in list_of_interval:
            print(f"Start: {start}, End: {end}")
    
        for start, end in list_of_interval:
            yield from fetch_tiktok_reports(
                tiktok_api=tiktok_api,
                current_date=start,
                interval_end=end,
                advertiser_id=advertiser_id,
                dimensions=dimensions,
                metrics=metrics,
                filters=None,
            )         

    return custom_reports
