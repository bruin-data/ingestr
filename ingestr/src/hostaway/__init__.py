from typing import Iterable

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .client import HostawayClient


@dlt.source(max_table_nesting=0)
def hostaway_source(
    api_key: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None = None,
    limit: int = 100,
) -> Iterable[DltResource]:
    """
    Hostaway API source for fetching listings data.

    Args:
        api_key: Hostaway API key for Bearer token authentication
        start_date: Start date for incremental loading
        end_date: End date for incremental loading (defaults to current time)
        limit: Number of records to fetch per page (default: 100)

    Returns:
        Iterable[DltResource]: DLT resources for listings
    """
    client = HostawayClient(api_key)

    @dlt.resource(
        write_disposition="merge",
        name="listings",
        primary_key="id",
    )
    def listings(
        datetime=dlt.sources.incremental(
            "latestActivityOn",
            initial_value=start_date,
            end_value=end_date,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        """
        Fetch listings from Hostaway API with incremental loading.
        Uses latestActivityOn field as the incremental cursor.
        """
        if datetime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = datetime.end_value

        start_dt = datetime.last_value

        yield from client.fetch_listings(start_dt, end_dt, limit)

    return (listings,)
