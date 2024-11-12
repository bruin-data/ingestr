from typing import Sequence

import dlt
from dlt.sources import DltResource

from .helpers import DEFAULT_DIMENSIONS, AdjustAPI


@dlt.source(max_table_nesting=0)
def adjust_source(
    start_date: str,
    end_date: str,
    api_key: str,
    utc_offset: str,
) -> Sequence[DltResource]:
    @dlt.resource(write_disposition="merge", merge_key="day")
    def campaigns():
        adjust_api = AdjustAPI(api_key=api_key)
        yield from adjust_api.fetch_report_data(
            start_date=start_date,
            end_date=end_date,
            utc_offset=utc_offset,
        )

    @dlt.resource(write_disposition="merge", merge_key="day")
    def creatives():
        dimensions = DEFAULT_DIMENSIONS + ["adgroup", "creative"]
        adjust_api = AdjustAPI(api_key=api_key)
        yield from adjust_api.fetch_report_data(
            start_date=start_date,
            end_date=end_date,
            dimensions=dimensions,
            utc_offset=utc_offset,
        )

    return campaigns, creatives
