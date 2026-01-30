from datetime import datetime
from typing import Iterable

import dlt
import pendulum
import requests
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client

from ingestr.src.customer_io.helpers import CustomerIoClient


def retry_on_limit(response: requests.Response, exception: BaseException) -> bool:
    return response.status_code == 429


def create_client() -> requests.Session:
    return Client(
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
        request_backoff_factor=2,
    ).session


@dlt.source(max_table_nesting=0)
def customer_io_source(
    api_key: str,
    start_date: TAnyDateTime | None = None,
    end_date: TAnyDateTime | None = None,
) -> Iterable[DltResource]:
    client = CustomerIoClient(api_key)

    @dlt.resource(write_disposition="replace", primary_key="id")
    def activities() -> Iterable[TDataItem]:
        yield from client.fetch_activities(create_client())

    @dlt.resource(write_disposition="merge", primary_key="id")
    def broadcasts(
        updated=dlt.sources.incremental(
            "updated",
            initial_value=start_date or pendulum.datetime(1970, 1, 1),
            end_value=end_date,
        ),
    ) -> Iterable[TDataItem]:
        for item in client.fetch_broadcasts(create_client()):
            item_updated = pendulum.from_timestamp(item.get("updated", 0))
            if item_updated >= updated.last_value:
                if updated.end_value is None or item_updated <= updated.end_value:
                    item["updated"] = item_updated
                    yield item

    @dlt.transformer(data_from=broadcasts, write_disposition="merge", primary_key="id")
    def broadcast_actions(broadcast: TDataItem) -> Iterable[TDataItem]:
        broadcast_id = broadcast.get("id")
        for item in client.fetch_broadcast_actions(create_client(), broadcast_id):
            item_updated = pendulum.from_timestamp(item.get("updated", 0))
            item["updated"] = item_updated
            yield item

    @dlt.transformer(data_from=broadcasts, write_disposition="merge", primary_key="id")
    def broadcast_messages(broadcast: TDataItem) -> Iterable[TDataItem]:
        broadcast_id = broadcast.get("id")
        start_ts = int(start_date.timestamp()) if start_date else None
        end_ts = int(end_date.timestamp()) if end_date else None
        for item in client.fetch_broadcast_messages(
            create_client(), broadcast_id, start_ts, end_ts
        ):
            yield item

    return (activities, broadcasts, broadcast_actions, broadcast_messages)


@dlt.source(max_table_nesting=0)
def customer_io_broadcast_metrics_source(
    api_key: str,
    period: str = "days",
    metric_type: str | None = None,
) -> Iterable[DltResource]:
    client = CustomerIoClient(api_key)

    @dlt.resource(
        write_disposition="replace"
    )
    def broadcast_metrics() -> Iterable[TDataItem]:
        yield from client.fetch_broadcast_metrics(create_client(), period, metric_type)

    return (broadcast_metrics,)


@dlt.source(max_table_nesting=0)
def customer_io_broadcast_action_metrics_source(
    api_key: str,
    period: str = "days",
) -> Iterable[DltResource]:
    client = CustomerIoClient(api_key)

    @dlt.resource(write_disposition="replace", primary_key="id", selected=False)
    def broadcasts() -> Iterable[TDataItem]:
        for item in client.fetch_broadcasts(create_client()):
            yield item

    @dlt.transformer(data_from=broadcasts, write_disposition="replace", selected=False)
    def broadcast_actions(broadcast: TDataItem) -> Iterable[TDataItem]:
        broadcast_id = broadcast.get("id")
        for item in client.fetch_broadcast_actions(create_client(), broadcast_id):
            yield item

    @dlt.transformer(
        data_from=broadcast_actions,
        write_disposition="replace",
        primary_key=["broadcast_id", "action_id", "period", "step_index"],
    )
    def broadcast_action_metrics(action: TDataItem) -> Iterable[TDataItem]:
        broadcast_id = action.get("broadcast_id")
        action_id = action.get("id")
        for item in client.fetch_broadcast_action_metrics(
            create_client(), broadcast_id, action_id, period
        ):
            yield item

    return (broadcasts | broadcast_actions | broadcast_action_metrics,)
