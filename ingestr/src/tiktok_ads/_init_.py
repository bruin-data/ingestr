from datetime import timedelta
from typing import Optional, Sequence, Iterable

import dlt
import pendulum
from dlt.sources import DltResource
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.common.time import ensure_pendulum_datetime

from .tiktok_helpers import TikTokAPI

#endpoint https://business-api.tiktok.com/open_api/v1.3/campaign/get/
#filter {gap <= 6 months
#   "creation_filter_start_time": "2023-01-01 00:00:00",
#   "creation_filter_end_time": "2023-06-30 23:59:59"
#Access-Token: header
#advertiser_id
# }
# pagination - page
#modify_time 

@dlt.source(max_table_nesting=0)
def tiktok_source(start_date,end_date, access_token:str,advertiser_id:str)->Sequence[DltResource]:
    start_date_obj = ensure_pendulum_datetime(start_date)
    end_date = ensure_pendulum_datetime(end_date)
    titkok_api = TikTokAPI(access_token)

    @dlt.resource(write_disposition="merge", primary_key="stat_time_day")
    def advertisersreportsdaily(
        datetime=dlt.sources.incremental("stat_time_day", start_date_obj.isoformat()),
    ) -> Iterable[TDataItem]:
        datetime_str = datetime.last_value
        start_time = ensure_pendulum_datetime(datetime_str)
        end_date = ensure_pendulum_datetime("2024-12-06")

        while start_time < end_date:
            interval_end = min(start_time + timedelta(days=30), end_date)

            for report in titkok_api.fetch_advertisers_reports_daily(start_time=start_time, end_time=end_date, advertiser_id=advertiser_id):
                yield report 

            start_time = interval_end

    return advertisersreportsdaily