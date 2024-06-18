"""Fetches Shopify Orders and Products."""

from typing import Iterable

import dlt
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource

from .helpers import GorgiasApi


@dlt.source(name="gorgias", max_table_nesting=0)
def gorgias_source(
    domain: str = dlt.secrets.value,
    email: str = dlt.config.value,
    api_key: str = dlt.secrets.value,
    start_date: TAnyDateTime = "2024-06-15",
) -> Iterable[DltResource]:
    """
    The source for the Gorgias pipeline. Available resources include tickets, users, and conversations.

    Args:
        domain: The domain of your Gorgias account.
        email: The email associated with your Gorgias account.
        api_key: The API key for accessing the Gorgias API.
        items_per_page: The max number of items to fetch per page. Defaults to 100.

    Returns:
        Iterable[DltResource]: A list of DltResource objects representing the data resources.
    """

    client = GorgiasApi(domain, email, api_key)

    start_date_obj = ensure_pendulum_datetime(start_date)

    @dlt.resource(primary_key="id", write_disposition="merge")
    def customers(
        updated_datetime=dlt.sources.incremental("updated_datetime", start_date_obj),
    ) -> Iterable[TDataItem]:
        """
        The resource for customers on your Gorgias domain, supports incremental loading and pagination.

        Args:
            updated_at: The saved state of the last 'updated_at' value.

        Returns:
            Iterable[TDataItem]: A generator of products.
        """
        print("upd.initial_value", updated_datetime.initial_value)
        print("upd.start_value", updated_datetime.start_value)
        print("upd.last_value", updated_datetime.last_value)
        print("upd.cursor", updated_datetime.cursor_path)
        yield from client.get_pages(
            "customers", params={}, latest_updated_at=updated_datetime.start_value
        )

    return customers
