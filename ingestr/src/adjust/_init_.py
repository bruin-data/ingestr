from typing import Sequence

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.sources import DltResource

from .helpers import AdjustAPI


@dlt.source(max_table_nesting=0)
def adjust_source(
    start_date: str,
    end_date: str,
    api_key: str,
) -> Sequence[DltResource]:
    start_date_obj = ensure_pendulum_datetime(start_date)

    @dlt.resource(write_disposition="merge", merge_key="day")
    def campaigns(
        start_date=dlt.sources.incremental("day", start_date_obj.isoformat()),
    ):
        print("start_date_incremental", start_date.start_value)
        formatted_start_date = pendulum.parse(start_date.start_value).format(
            "YYYY-MM-DD"
        )

        adjust_api = AdjustAPI(
            start_date=formatted_start_date, end_date=end_date, api_key=api_key
        )
        yield from adjust_api.fetch_report_data()

    return campaigns
