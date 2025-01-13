from typing import Iterable

import dlt
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from pendulum import Date

from .helpers import LinkedInAdsAPI, find_intervals
from .metrics_dimenison_enum import Dimension, Metric, TimeGranularity


@dlt.source(max_table_nesting=0)
def linked_in_ads_source(
    start_date: Date,
    end_date: Date,
    access_token: str,
    account_ids: list[str],
    dimension: Dimension,
    metrics: list[Metric],
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
        dateTime=(dlt.sources.incremental(incremental_loading_param, start_date)),
    ) -> Iterable[TDataItem]:
        datetime_value = dateTime.last_value
        start_date = datetime_value
        print(f"start_date: {start_date}, end_date: {end_date}")

        list_of_interval = find_intervals(
            start_date=start_date,
            end_date=end_date,
            time_granularity=time_granularity,
        )

        for start, end in list_of_interval:
            yield linkedin_api.fetch_pages(start, end)

    return custom_reports
