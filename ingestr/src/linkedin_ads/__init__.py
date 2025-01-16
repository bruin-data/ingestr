from typing import Iterable

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from pendulum import Date

from .dimension_time_enum import Dimension, TimeGranularity
from .helpers import LinkedInAdsAPI, find_intervals


@dlt.source(max_table_nesting=0)
def linked_in_ads_source(
    start_date: Date,
    end_date: Date | None,
    access_token: str,
    account_ids: list[str],
    dimension: Dimension,
    metrics: list[str],
    time_granularity: TimeGranularity,
) -> DltResource:
    linkedin_api = LinkedInAdsAPI(
        access_token=access_token,
        account_ids=account_ids,
        dimension=dimension,
        metrics=metrics,
        time_granularity=time_granularity,
    )

    if time_granularity == TimeGranularity.daily:
        primary_key = [dimension.value, "date"]
        incremental_loading_param = "date"
    else:
        primary_key = [dimension.value, "start_date", "end_date"]
        incremental_loading_param = "start_date"

    @dlt.resource(write_disposition="merge", primary_key=primary_key)
    def custom_reports(
        dateTime=(
            dlt.sources.incremental(
                incremental_loading_param,
                initial_value=start_date,
                end_value=end_date,
                range_start="closed",
                range_end="closed",
            )
        ),
    ) -> Iterable[TDataItem]:
        if dateTime.end_value is None:
            end_date = pendulum.now().date()
        else:
            end_date = dateTime.end_value

        list_of_interval = find_intervals(
            start_date=dateTime.last_value,
            end_date=end_date,
            time_granularity=time_granularity,
        )
        for start, end in list_of_interval:
            yield linkedin_api.fetch_pages(start, end)

    return custom_reports
