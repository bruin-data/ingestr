from typing import Iterable

import dlt
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .helpers import LinkedInAdsAPI


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
        interval_start=start_date,
        interval_end=end_date,
    )
    if time_granularity == "DAILY":
        primary_key = [dimension] + ["date"]
    else:
        primary_key = [dimension] + ["start_date"] + ["end_date"]

    @dlt.resource(write_disposition="merge", primary_key=primary_key)
    def custom_reports() -> Iterable[TDataItem]:
        yield linkedin_api.fetch_pages()

    return custom_reports
