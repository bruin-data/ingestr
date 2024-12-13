from datetime import timedelta
from typing import Iterable

import dlt
import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .tiktok_helpers import TikTokAPI


@dlt.source(max_table_nesting=0)
def tiktok_source(
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime,
    access_token,
    advertiser_id,
    dimensions,
    metrics,
) -> DltResource:
    titkok_api = TikTokAPI(access_token)

    @dlt.resource(write_disposition="merge", primary_key="dimensions")
    def reports(
        datetime=dlt.sources.incremental("stat_time_day", start_date.isoformat())
        if "stat_time_day" in dimensions
        else None,
    ) -> Iterable[TDataItem]:
        days = 365

        if datetime is not None:
            datetime_str = datetime.last_value
            start_time = ensure_pendulum_datetime(datetime_str)
            days = 30
        else:
            start_time = start_date

        while start_time < end_date:
            interval_end = min(start_time + timedelta(days=days), end_date)

            for report in titkok_api.fetch_reports(
                start_time=start_time,
                end_time=end_date,
                advertiser_id=advertiser_id,
                dimensions=dimensions,
                metrics=metrics,
            ):
                yield report
            start_time = interval_end

    return reports
