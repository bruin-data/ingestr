from typing import Iterable

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .tiktok_helpers import TikTokAPI

KNOWN_TYPE_HINTS = {
    "spend": {"data_type": "decimal"},
    "billed_cost": {"data_type": "decimal"},
    "cash_spend": {"data_type": "decimal"},
    "voucher_spend": {"data_type": "decimal"},
    "cpc": {"data_type": "decimal"},
    "cpm": {"data_type": "decimal"},
    "impressions": {"data_type": "bigint"},
    "gross_impressions": {"data_type": "bigint"},
    "clicks": {"data_type": "bigint"},
    "ctr": {"data_type": "decimal"},
    "reach": {"data_type": "bigint"},
    "cost_per_1000_reached": {"data_type": "decimal"},
    "frequency": {"data_type": "decimal"},
    "conversion": {"data_type": "bigint"},
    "cost_per_conversion": {"data_type": "decimal"},
    "conversion_rate": {"data_type": "decimal"},
    "conversion_rate_v2": {"data_type": "decimal"},
    "real_time_conversion": {"data_type": "bigint"},
    "real_time_cost_per_conversion": {"data_type": "decimal"},
    "real_time_conversion_rate": {"data_type": "decimal"},
    "real_time_conversion_rate_v2": {"data_type": "decimal"},
    "result": {"data_type": "bigint"},
    "cost_per_result": {"data_type": "decimal"},
    "result_rate": {"data_type": "decimal"},
    "real_time_result": {"data_type": "bigint"},
    "real_time_cost_per_result": {"data_type": "decimal"},
    "real_time_result_rate": {"data_type": "decimal"},
    "secondary_goal_result": {"data_type": "bigint"},
    "cost_per_secondary_goal_result": {"data_type": "decimal"},
    "secondary_goal_result_rate": {"data_type": "decimal"},
}


def find_intervals(
    current_date: pendulum.DateTime,
    end_date: pendulum.DateTime,
    interval_days: int,
):
    intervals = []
    while current_date <= end_date:
        interval_end = min(current_date.add(days=interval_days), end_date)
        intervals.append((current_date, interval_end))
        current_date = interval_end.add(days=1)

    return intervals


@dlt.source(max_table_nesting=0)
def tiktok_source(
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime,
    access_token: str,
    advertiser_ids: list[str],
    timezone: str,
    page_size: int,
    filtering_param: bool,
    filter_name: str,
    filter_value: list[int],
    dimensions: list[str],
    metrics: list[str],
) -> DltResource:
    tiktok_api = TikTokAPI(
        access_token=access_token,
        timezone=timezone,
        page_size=page_size,
        filtering_param=filtering_param,
        filter_name=filter_name,
        filter_value=filter_value,
    )
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

    type_hints = {
        "advertiser_id": {"data_type": "text"},
    }
    for dimension in dimensions:
        if dimension in KNOWN_TYPE_HINTS:
            type_hints[dimension] = KNOWN_TYPE_HINTS[dimension]
    for metric in metrics:
        if metric in KNOWN_TYPE_HINTS:
            type_hints[metric] = KNOWN_TYPE_HINTS[metric]

    @dlt.resource(
        write_disposition="merge",
        primary_key=dimensions + ["advertiser_id"],
        columns=type_hints,
        parallelized=True,
    )
    def custom_reports(
        datetime=(
            dlt.sources.incremental(
                incremental_loading_param,
                start_date,
                range_end="closed",
                range_start="closed",
            )
            if is_incremental
            else None
        ),
    ) -> Iterable[TDataItem]:
        current_date = start_date.in_tz(timezone)

        if datetime is not None:
            datetime_str = datetime.last_value
            current_date = ensure_pendulum_datetime(datetime_str).in_tz(timezone)

        list_of_interval = find_intervals(
            current_date=current_date,
            end_date=end_date,
            interval_days=interval_days,
        )

        for start, end in list_of_interval:
            yield tiktok_api.fetch_pages(
                advertiser_ids=advertiser_ids,
                start_time=start,
                end_time=end,
                dimensions=dimensions,
                metrics=metrics,
            )

    return custom_reports
