"""Daisycon source for loading transactions incrementally.

Supports one or multiple advertiser ids. Incremental ranges use timestamps in
the ``YYYY-MM-DD HH:mm:ss`` format and are normalised to UTC."""

from typing import Iterable, Iterator

import dlt
from dlt.common import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .client import DaisyconClient


@dlt.source(max_table_nesting=0)
def daisycon_source(
    client_id: str,
    client_secret: str,
    refresh_token: str,
    advertiser_ids: list[str],
    currency_code: str = "EUR",
    start_date: pendulum.DateTime = pendulum.datetime(2024, 1, 1),
    end_date: pendulum.DateTime | None = None,
) -> Iterable[DltResource]:

    client = DaisyconClient(client_id, client_secret, refresh_token, advertiser_ids)

    @dlt.resource(
        name="transactions",
        write_disposition="merge",
        primary_key="id",
    )
    def transactions(
        last_modified=dlt.sources.incremental(
            "last_modified",
            initial_value=start_date,
            end_value=end_date,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[TDataItem]:
        if last_modified.end_value is None:
            end_dt = pendulum.now("UTC").format("YYYY-MM-DD HH:mm:ss")
        else:
            end_dt = ensure_pendulum_datetime(last_modified.end_value).format(
                "YYYY-MM-DD HH:mm:ss"
            )
        start_dt = ensure_pendulum_datetime(last_modified.last_value).format(
            "YYYY-MM-DD HH:mm:ss"
        )
        yield from client.paginated_transactions(start_dt, end_dt, currency_code)

    yield transactions
