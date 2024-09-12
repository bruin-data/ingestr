from typing import Sequence

import dlt
from dlt.sources import DltResource

from .helpers import AdjustAPI


@dlt.source(max_table_nesting=0)
def adjust_source(
    start_date: str,
    end_date: str,
    api_key: str,
) -> Sequence[DltResource]:
    @dlt.resource(write_disposition="merge", merge_key="day")
    def campaigns():
        adjust_api = AdjustAPI(api_key=api_key)
        yield from adjust_api.fetch_report_data(
            start_date=start_date,
            end_date=end_date,
        )

    return campaigns
