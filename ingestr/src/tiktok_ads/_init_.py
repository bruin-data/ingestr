from datetime import timedelta
from typing import Iterable, Optional

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .tiktok_helpers import TikTokAPI


def fetch_data_by_interval(
    tiktok_api,
    advertiser_id: str,
    dimensions: list[str],
    metrics: list[str],
    current_date: pendulum.DateTime,
    end_date: pendulum.DateTime,
    interval_days: int,
    filters=None,
) -> Iterable[TDataItem]:
    # The API allows fetching data for the same date (e.g., 2024-01-01 to 2024-01-01),
    # and its side effect is that it will fetch data atleast one time by default
    # if the incremental load date matches the start date
    while current_date <= end_date:
        interval_end = min(current_date + timedelta(days=interval_days), end_date)

        print(f"Fetching data for interval: {current_date} - {interval_end}")

        for report in fetch_tiktok_reports(
            tiktok_api=tiktok_api,
            current_date=current_date,
            interval_end=interval_end,
            advertiser_id=advertiser_id,
            dimensions=dimensions,
            metrics=metrics,
            filters=filters,
        ):
            yield report

        current_date = interval_end + timedelta(seconds=1)


# The API allows fetching data only if date is less than 24 hours and also
# it should be of same day,
def fetch_data_hourly(
    dimensions,
    end_date: pendulum.DateTime,
    start_date: pendulum.DateTime,
    tiktok_api,
    advertiser_id,
    metrics,
):
    end_date = end_date.end_of("day")
    current_date = start_date
    while current_date <= end_date:
        day_start = current_date.start_of("day")
        day_end = current_date.end_of("day")

        interval_end = min(day_end, end_date)
        print(f"Fetching data for interval: {day_start} - {interval_end}")
        for report in fetch_tiktok_reports(
            tiktok_api=tiktok_api,
            current_date=day_start,
            interval_end=interval_end,
            advertiser_id=advertiser_id,
            dimensions=dimensions,
            metrics=metrics,
            filters=None,
        ):
            yield report

        current_date = current_date.add(days=1)


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
    advertiser_id: str,
    dimensions: list[str],
    metrics: list[str],
    filters=None,
) -> DltResource:
    tiktok_api = TikTokAPI(access_token)
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

    @dlt.resource(write_disposition="merge", primary_key=dimensions)
    def custom_reports(
        datetime=dlt.sources.incremental(
            incremental_loading_param, start_date.isoformat()
        )
        if is_incremental
        else None,
    ) -> Iterable[TDataItem]:
        current_date = start_date

        if datetime is not None:
            datetime_str = datetime.last_value
            current_date = ensure_pendulum_datetime(datetime_str)

        if "stat_time_hour" in dimensions:
            yield from fetch_data_hourly(
                dimensions=dimensions,
                end_date=end_date,
                start_date=current_date,
                tiktok_api=tiktok_api,
                advertiser_id=advertiser_id,
                metrics=metrics,
            )
        else:
            yield from fetch_data_by_interval(
                dimensions=dimensions,
                tiktok_api=tiktok_api,
                metrics=metrics,
                current_date=current_date,
                interval_days=interval_days,
                end_date=end_date,
                advertiser_id=advertiser_id,
                filters=filters,
            )

    return custom_reports
