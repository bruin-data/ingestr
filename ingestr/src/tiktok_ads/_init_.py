from datetime import timedelta
from typing import Optional, Sequence, Iterable

import dlt
import pendulum
from dlt.sources import DltResource
from tiktok_helpers import TikTokAPI
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.common.time import ensure_pendulum_datetime

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
def tiktok_source(start_date: str,end_date: str, access_token:str,advertiser_id:str)->Sequence[DltResource]:
    start_date_obj = ensure_pendulum_datetime(start_date)
    titkok_api = TikTokAPI(access_token)

    @dlt.resource(write_disposition="merge", primary_key="campaign_id")
    def campaigns(
        datetime=dlt.sources.incremental("modify_time", start_date_obj.isoformat()),
    ) -> Iterable[TDataItem]:
        start_time = datetime.end_value

        while start_time < end_date:
            interval_end = min(start_time + timedelta(days=180), end_date)

            for campaign in titkok_api.fetch_campaigns(start_time=start_time, end_time=end_date, advertiser_id=advertiser_id):
                yield campaign 

            start_time = interval_end + timedelta(seconds=1)

    return campaigns