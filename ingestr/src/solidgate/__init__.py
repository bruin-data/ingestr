from typing import Iterable, Iterator

import dlt
import pendulum
from dlt.sources import DltResource

from .helpers import SolidgateClient


@dlt.source(max_table_nesting=0)
def solidgate_source(
    public_key: str,
    secret_key: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None,
) -> Iterable[DltResource]:
    solidgate_client = SolidgateClient(public_key, secret_key)

    @dlt.resource(
        name="subscriptions",
        write_disposition="merge",
        primary_key="id",
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

    return fetch_all_subscriptions
