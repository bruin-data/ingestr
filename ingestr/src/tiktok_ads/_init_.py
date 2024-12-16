from datetime import timedelta
from typing import Iterable, Optional

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .tiktok_helpers import TikTokAPI

def calculate_interval_end(current_date, end_date, interval=365, diff_hour=False):
    if diff_hour:
        return min(current_date + timedelta(hours=23, minutes=59, seconds=59), end_date)
    return min(current_date + timedelta(days=interval), end_date)

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
        for report in tiktok_api.fetch_reports(
            start_time=current_date,
            end_time=interval_end,
            advertiser_id=advertiser_id,
            dimensions=dimensions,
            metrics=metrics,
            filters=filters,
        ):
            yield report
    except Exception as e:
        print(f"Got error while fetching tiktok basic report: {e}")

@dlt.source(max_table_nesting=0)
def tiktok_source(
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime,
    access_token: str,
    incremental_loading_param: str,
    advertiser_id: str,
    interval_days: int,
    dimensions: list[str],
    metrics: list[str],
    filters: Optional[list[str]] = None,
) -> DltResource:
    tiktok_api = TikTokAPI(access_token)

    @dlt.resource(write_disposition="merge", primary_key=dimensions)
    def custom_reports(
        datetime=dlt.sources.incremental(incremental_loading_param, start_date.isoformat())
        if incremental_loading_param in dimensions
        else None,
    ) -> Iterable[TDataItem]:
        current_date = start_date
        diff_by_hour = False

        if datetime is not None:
            datetime_str = datetime.last_value
            current_date = ensure_pendulum_datetime(datetime_str)

        if "stat_time_hour" in dimensions:
            diff_by_hour = True

        while current_date < end_date:
            interval_end = calculate_interval_end(current_date, end_date, interval_days, diff_by_hour)
            print(f"{current_date} - {interval_end}")

            for report in fetch_tiktok_reports(
                tiktok_api = tiktok_api,
                current_date=current_date,
                interval_end=interval_end,
                advertiser_id=advertiser_id,
                dimensions= dimensions,
                metrics=metrics,
                filters=filters,
            ):
                yield report

            current_date = interval_end
    return custom_reports
