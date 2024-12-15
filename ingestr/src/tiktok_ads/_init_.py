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
    incremental_loading_param,
    advertiser_id,
    interval_days,
    dimensions: list[str],
    metrics: list[str],
    filters: list[str] | None,
) -> DltResource:
    tiktok_api = TikTokAPI(access_token)

    @dlt.resource(write_disposition="merge", primary_key="dimensions")
    def custom_reports(
        datetime=dlt.sources.incremental("stat_time_flat", start_date.isoformat())
        if incremental_loading_param in dimensions
        else None,
    ) -> Iterable[TDataItem]:
        current_date = start_date
       
        if datetime is not None:
            datetime_str = datetime.last_value
            current_date = ensure_pendulum_datetime(datetime_str)
            print("current_date:", current_date)
            print("end_date:", end_date)

        if "stat_time_hour" in dimensions:
            while current_date < end_date:
                interval_end = min(
                    current_date + timedelta(hours=23, minutes=59, seconds=59), end_date
                )
                print(
                        f"fetching from {current_date} to {interval_end}"
                    )
                try:
                    for report in tiktok_api.fetch_reports(
                        start_time=current_date,
                        end_time=interval_end,
                        advertiser_id=advertiser_id,
                        dimensions=dimensions,
                        metrics=metrics,
                        filters=filters or {},
                    ):
                        total_fetched += 1
                        print(f"Fetched dimenions: {report})")

                        yield report
                   
                except Exception as e:
                    print(f"Error fetching reports: {e}")

                current_date = interval_end + timedelta(seconds=1)
        else:
            while current_date < end_date:
                interval_end = min(
                    current_date + timedelta(days=interval_days), end_date
                )

                try:
                    for report in tiktok_api.fetch_reports(
                        start_time=current_date,
                        end_time=interval_end,
                        advertiser_id=advertiser_id,
                        dimensions=dimensions,
                        metrics=metrics,
                        filters=filters or {},
                    ):
                        yield report
                except Exception as e:
                    print(f"Error fetching reports: {e}")

                current_date = interval_end

    return custom_reports
