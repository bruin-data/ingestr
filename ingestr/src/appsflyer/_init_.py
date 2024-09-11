from typing import Iterable
import requests

import dlt
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client
from ingestr.src.appsflyer.client import AppsflyerClient
from dlt.common.typing import TDataItem
from dlt.common.time import ensure_pendulum_datetime

def retry_on_limit(response: requests.Response, exception: BaseException) -> bool:
    return response.status_code == 429

def create_client() -> requests.Session:
    return Client(
        request_timeout=10.0,
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
        request_backoff_factor=2,
    ).session


@dlt.source(max_table_nesting=0)
def appsflyer_source( api_key:str,
            app_id:str,
            start_date:str,
            end_date:str) -> Iterable[DltResource]:
    client = AppsflyerClient(api_key)
    
    start_date_obj = ensure_pendulum_datetime(start_date)
    
    @dlt.resource(write_disposition="merge", merge_key="event_time")
    def installs(
        event_date=dlt.sources.incremental("event_time", start_date_obj.isoformat()),
    ) -> Iterable[TDataItem]:

        yield from client.fetch_installs(create_client(),event_date.start_value,end_date,app_id)
   
    return installs