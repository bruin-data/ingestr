from typing import Iterable

import dlt
import requests
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client

from ingestr.src.appsflyer.client import AppsflyerClient


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
def appsflyer_source(
    api_key: str, app_id: str, start_date: str, end_date: str
) -> Iterable[DltResource]:
    client = AppsflyerClient(api_key)

    @dlt.resource(write_disposition="merge", merge_key="touch_time")
    def installs() -> Iterable[TDataItem]:
        yield from client.fetch_installs(create_client(), start_date, end_date, app_id)

    @dlt.resource(write_disposition="merge", merge_key="touch_time")
    def organic_installs() -> Iterable[TDataItem]:
        yield from client.fetch_organic_installs(
            create_client(), start_date, end_date, app_id
        )

    return installs, organic_installs
