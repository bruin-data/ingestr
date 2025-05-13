from typing import Iterable, Optional

import dlt
import pendulum
import requests
from dlt.common.typing import TAnyDateTime, TDataItem
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
def phantombuster_source(
    api_key: str, agent_id: str, start_date: TAnyDateTime, end_date: TAnyDateTime | None
) -> Iterable[DltResource]:
    client = PhantombusterClient(api_key)

    @dlt.resource(
        write_disposition="merge",
        primary_key="container_id",
        columns={
            "partition_dt": {"data_type": "date", "partition": True},
        },
    )
    def completed_phantoms(
        dateTime=(
            dlt.sources.incremental(
                "ended_at",
                initial_value=start_date,
                end_value=end_date,
                range_start="closed",
                range_end="closed",
            )
        ),
    ) -> Iterable[TDataItem]:
        if dateTime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = dateTime.end_value

        start_dt = dateTime.last_value

        yield client.fetch_containers_result(
            create_client(), agent_id, start_date=start_dt, end_date=end_dt
        )

    return completed_phantoms
