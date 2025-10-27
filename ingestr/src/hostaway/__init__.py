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
    listing_id: str | None = None,
) -> Iterable[DltResource]:
    """
    Hostaway API source for fetching listings and fee settings data.

    Args:
        api_key: Hostaway API key for Bearer token authentication
        start_date: Start date for incremental loading
        end_date: End date for incremental loading (defaults to current time)
        limit: Number of records to fetch per page (default: 100)
        listing_id: Optional listing ID for fetching specific listing's fee settings

    Returns:
        Iterable[DltResource]: DLT resources for listings and/or fee settings
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

    @dlt.resource(
        write_disposition="merge",
        name="listing_fee_settings",
        primary_key="id",
        table_name="listing_fee_settings",
    )
    def listing_fee_settings(
        datetime=dlt.sources.incremental(
            "updatedOn",
            initial_value=start_date,
            end_value=end_date,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        """
        Fetch listing fee settings from Hostaway API with incremental loading.
        Uses updatedOn field as the incremental cursor.

        If listing_id is provided, fetches fee settings for that specific listing.
        Otherwise, fetches fee settings for all listings.
        """
        if datetime.end_value is None:
            end_dt = pendulum.now(tz="UTC")
        else:
            end_dt = datetime.end_value

        start_dt = datetime.last_value

        if listing_id:
            # Fetch fee settings for specific listing
            yield from client.fetch_listing_fee_settings(
                listing_id, start_dt, end_dt, limit
            )
        else:
            # Fetch fee settings for all listings
            yield from client.fetch_all_listing_fee_settings(start_dt, end_dt, limit)

    @dlt.resource(
        write_disposition="replace",
        name="listing_agreements",
        table_name="listing_agreements",
        columns={
            "id": {"data_type": "text"},
            "text": {"data_type": "text"},
            "listingMapId": {"data_type": "bigint"},
        },
    )
    def listing_agreements() -> Iterable[TDataItem]:
        """
        Fetch listing agreements from Hostaway API.

        If listing_id is provided, fetches agreements for that specific listing.
        Otherwise, fetches agreements for all listings.

        Note: Uses replace mode, so no incremental loading.
        """
        if listing_id:
            # Fetch agreements for specific listing
            yield from client.fetch_listing_agreement(listing_id, limit)
        else:
            # Fetch agreements for all listings
            # Use a very wide date range to get all listings (no filtering)
            very_old_date = pendulum.datetime(1970, 1, 1, tz="UTC")
            now = pendulum.now(tz="UTC")

            yield from client.fetch_all_listing_agreements(very_old_date, now, limit)

    @dlt.resource(
        write_disposition="replace",
        name="listing_pricing_settings",
        table_name="listing_pricing_settings",
        columns={
            "id": {"data_type": "text"},
        },
    )
    def listing_pricing_settings() -> Iterable[TDataItem]:
        """
        Fetch listing pricing settings from Hostaway API.

        If listing_id is provided, fetches pricing settings for that specific listing.
        Otherwise, fetches pricing settings for all listings.

        Note: Uses replace mode, so no incremental loading.
        """
        if listing_id:
            # Fetch pricing settings for specific listing
            yield from client.fetch_listing_pricing_settings(listing_id, limit)
        else:
            # Fetch pricing settings for all listings
            # Use a very wide date range to get all listings (no filtering)
            very_old_date = pendulum.datetime(1970, 1, 1, tz="UTC")
            now = pendulum.now(tz="UTC")

            yield from client.fetch_all_listing_pricing_settings(very_old_date, now, limit)

    return listings, listing_fee_settings, listing_agreements, listing_pricing_settings
