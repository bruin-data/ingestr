from typing import Iterable

import dlt
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .helpers import LinkedInAdsAPI, find_intervals


@dlt.source(max_table_nesting=0)
def linkedin_source(
    start_date,
    end_date,
    access_token,
    account_ids,
    dimension,
    metrics,
    time_granularity,
) -> DltResource:
    linkedin_api = LinkedInAdsAPI(
        access_token=access_token,
        account_ids=account_ids,
        dimension=dimension,
        metrics=metrics,
        time_granularity=time_granularity,
    )
    if time_granularity == "DAILY":
        primary_key = [dimension] + ["date"]
    else:
        primary_key = [dimension] + ["start_date"] + ["end_date"]

    @dlt.resource(write_disposition="merge", primary_key=primary_key)
    def custom_reports() -> Iterable[TDataItem]:
        list_of_interval = find_intervals(
            current_date=start_date,
            end_date=end_date,
            time_granularity=time_granularity,
        )
        print("list_of_interval", list_of_interval)

        for start, end in list_of_interval:
            yield linkedin_api.fetch_pages(start, end)

    return custom_reports
