from typing import Iterable

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .client import WiseClient


@dlt.source(max_table_nesting=0)
def wise_source(
    api_key: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None = None,
) -> Iterable[DltResource]:
    client = WiseClient(api_key)

    # List of all profiles belonging to user.
    @dlt.resource(write_disposition="merge", name="profiles", primary_key="id")
    def profiles() -> Iterable[TDataItem]:
        yield from client.fetch_profiles()

    # List transfers for a profile.
    @dlt.resource(write_disposition="merge", name="transfers", primary_key="id")
    def transfers(
        profiles=profiles,
        datetime=dlt.sources.incremental(
            "created",
            initial_value=start_date,
            end_value=end_date,
            range_end="closed",
            range_start="closed",
        ),
    ):
        if datetime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = datetime.end_value

        start_dt = datetime.last_value

        for profile in profiles:
            yield from client.fetch_transfers(profile["id"], start_dt, end_dt)

    # Retrieve the user's multi-currency account balance accounts. It returns all balance accounts the profile has.
    @dlt.resource(write_disposition="merge", name="balances", primary_key="id")
    def balances(
        profiles=profiles,
        datetime=dlt.sources.incremental(
            "modificationTime",
            initial_value=start_date,
            end_value=end_date,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        if datetime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = datetime.end_value

        start_dt = datetime.last_value

        for profile in profiles:
            yield from client.fetch_balances(profile["id"], start_dt, end_dt)

    return profiles, transfers, balances
