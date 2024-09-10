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
    adjust_api = AdjustAPI(start_date=start_date, end_date=end_date, api_key=api_key)

    @dlt.resource(write_disposition="replace")
    def campaigns():
        yield from adjust_api.fetch_report_data()

    return campaigns
