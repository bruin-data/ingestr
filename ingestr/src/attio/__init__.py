from typing import Iterable, Iterator

import dlt
import requests
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client

from .helpers import AttioClient


def create_client() -> requests.Session:
    return Client(
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
    ).session


def retry_on_limit(
    response: requests.Response | None, exception: BaseException | None
) -> bool:
    if response is None:
        return False
    return response.status_code == 502


@dlt.source(max_table_nesting=0)
def attio_source(
    api_key: str,
    object_id: str | None,
) -> Iterable[DltResource]:
    base_url = "https://api.attio.com/v2"

    @dlt.resource(
        name="objects",
        primary_key=["workspace_id", "object_id"],
        write_disposition="merge",
        columns={
            "partition_dt": {"data_type": "date", "partition": True},
        },
    )
    def fetch_objects() -> Iterator[dict]:
        url = f"{base_url}/objects"
        attio_client = AttioClient(api_key)
        yield attio_client.fetch_all_objects(url, create_client())

    @dlt.resource(
        name="records",
        primary_key=["workspace_id", "object_id", "record_id"],
        write_disposition="merge",
        columns={
            "partition_dt": {"data_type": "date", "partition": True},
        },
    )
    def fetch_records() -> Iterator[dict]:
        url = f"{base_url}/objects/{object_id}/records/query"
        attio_client = AttioClient(api_key)

        yield attio_client.fetch_all_records_of_object(url, create_client())

    return fetch_objects, fetch_records
