from typing import Iterable
from .helpers import LinkedInAdsAPI
import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

@dlt.source(max_table_nesting=0)
def linkedin_source(start_date: pendulum.DateTime, end_date: pendulum.DateTime, access_token: str, account_ids: list[str], dimension: str, metrics: list[str],
                 time_granularity: str = "DAILY"
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

    @dlt.resource(
        write_disposition="merge",
        primary_key= dimension
    )
    def custom_reports() -> Iterable[TDataItem]:
       yield from linkedin_api.fetch_pages()  

    return custom_reports
    
  


