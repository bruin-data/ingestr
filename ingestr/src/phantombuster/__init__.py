from typing import Iterable, Optional

import dlt
import pendulum
import requests
from dlt.common.typing import TDataItem, TAnyDateTime
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client
from dlt.common.time import ensure_pendulum_datetime


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
def phantombuster_source(api_key: str, agent_id: str, start_date: TAnyDateTime, end_date: TAnyDateTime | None) -> Iterable[DltResource]:
    client = PhantombusterClient(api_key)

    @dlt.resource(write_disposition="merge",
        primary_key="container_id"
    )

    def completed_phantoms(dateTime=(
            dlt.sources.incremental(
                "ended_at",
                initial_value=start_date,
                end_value=end_date,
                range_start="closed",
                range_end="closed",
            )
        ),) -> Iterable[TDataItem]:

        if end_date is not None:
            end_dt = dateTime.end_value
        else:
            end_dt = pendulum.now(tz="UTC")

        if dateTime.last_value:
            start_dt = dateTime.last_value
        else:
            start_dt = start_date


        yield client.fetch_containers_result(create_client(), agent_id, start_date=start_dt, end_date=end_dt)

    return completed_phantoms
