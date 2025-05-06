from typing import Iterable, Optional

import dlt
import requests
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client

from ingestr.src.phantombuster.client import PhantombusterClient


def retry_on_limit(
    response: Optional[requests.Response], exception: Optional[BaseException]
) -> bool:
    if response is not None and response.status_code == 429:
        return True
    return False


def create_client() -> requests.Session:
    return Client(
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
        request_backoff_factor=2,
    ).session


@dlt.source(max_table_nesting=0)
def phantombuster_source(api_key: str, agent_id: str) -> Iterable[DltResource]:
    client = PhantombusterClient(api_key)

    @dlt.resource(name="containers_result_object")
    def fetch_containers_result_object() -> Iterable[TDataItem]:
        url = "https://api.phantombuster.com/api/v2/containers/fetch-all/"

        yield client.fetch_containers_result(create_client(), url, agent_id)

    return fetch_containers_result_object
