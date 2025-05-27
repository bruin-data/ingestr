from typing import Iterable, Iterator

import dlt
import pendulum
from dlt.sources import DltResource

from .helpers import SolidgateClient


@dlt.source(max_table_nesting=0)
def solidgate_source(
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None,
    public_key: str,
    secret_key: str,
) -> Iterable[DltResource]:
    solidgate_client = SolidgateClient(public_key, secret_key)

    @dlt.resource(
        name="subscriptions",
        write_disposition="merge",
        primary_key="id",
        columns={
            "created_at": {"data_type": "timestamp", "partition": True},
        },
    )
    def fetch_all_subscriptions(
        dateTime=dlt.sources.incremental(
            "updated_at",
            initial_value=start_date,
            end_value=end_date,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[dict]:
        path = "subscriptions"
        if dateTime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = dateTime.end_value

        start_dt = dateTime.last_value
        yield solidgate_client.fetch_data(path, date_from=start_dt, date_to=end_dt)

    @dlt.resource(
        name="apm-orders",
        write_disposition="merge",
        primary_key="order_id",
        columns={
            "created_at": {"data_type": "timestamp", "partition": True},
        },
    )
    def fetch_apm_orders(
        dateTime=dlt.sources.incremental(
            "updated_at",
            initial_value=start_date,
            end_value=end_date,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[dict]:
        path = "apm-orders"
        if dateTime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = dateTime.end_value

        start_dt = dateTime.last_value
        yield solidgate_client.fetch_data(path, date_from=start_dt, date_to=end_dt)

    @dlt.resource(
        name="card-orders",
        write_disposition="merge",
        primary_key="order_id",
        columns={
            "created_at": {"data_type": "timestamp", "partition": True},
        },
    )
    def fetch_card_orders(
        dateTime=dlt.sources.incremental(
            "updated_at",
            initial_value=start_date,
            end_value=end_date,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[dict]:
        path = "card-orders"
        if dateTime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = dateTime.end_value

        start_dt = dateTime.last_value
        yield solidgate_client.fetch_data(path, date_from=start_dt, date_to=end_dt)

    return fetch_all_subscriptions, fetch_apm_orders, fetch_card_orders
