"""This source uses Freshdesk API and dlt to load data such as Agents, Companies, Tickets
etc. to the database"""

from typing import Any, Dict, Generator, Iterable, List, Optional

import dlt
from dlt.sources import DltResource

from .freshdesk_client import FreshdeskClient
from .settings import DEFAULT_ENDPOINTS


@dlt.source()
def freshdesk_source(
    endpoints: Optional[List[str]] = None,
    per_page: int = 100,
    domain: str = dlt.secrets.value,
    api_secret_key: str = dlt.secrets.value,
) -> Iterable[DltResource]:
    """
    Retrieves data from specified Freshdesk API endpoints.

    This source supports pagination and incremental data loading. It fetches data from a list of
    specified endpoints, or defaults to predefined endpoints in 'settings.py'.

    Args:
        endpoints: A list of Freshdesk API endpoints to fetch. Deafults to 'settings.py'.
        per_page: The number of items to fetch per page, with a maximum of 100.
        domain: The Freshdesk domain from which to fetch the data. Defaults to 'config.toml'.
        api_secret_key: Freshdesk API key. Defaults to 'secrets.toml'.

    Yields:
        Iterable[DltResource]: Resources with data updated after the last 'updated_at'
        timestamp for each endpoint.
    """
    # Instantiate FreshdeskClient with the provided domain and API key
    freshdesk = FreshdeskClient(api_key=api_secret_key, domain=domain)

    def incremental_resource(
        endpoint: str,
        updated_at: Optional[Any] = dlt.sources.incremental(
            "updated_at", initial_value="2022-01-01T00:00:00Z"
        ),
    ) -> Generator[Dict[Any, Any], Any, None]:
        """
        Fetches and yields paginated data from a specified API endpoint.
        Each page of data is fetched based on the `updated_at` timestamp
        to ensure incremental loading.
        """

        # Retrieve the last updated timestamp to fetch only new or updated records.
        if updated_at is not None:
            updated_at = updated_at.last_value

        # Use the FreshdeskClient instance to fetch paginated responses
        yield from freshdesk.paginated_response(
            endpoint=endpoint,
            per_page=per_page,
            updated_at=updated_at,
        )

    # Set default endpoints if not provided
    endpoints = endpoints or DEFAULT_ENDPOINTS

    # For each endpoint, create and yield a DLT resource
    for endpoint in endpoints:
        yield dlt.resource(
            incremental_resource,
            name=endpoint,
            write_disposition="merge",
            primary_key="id",
        )(endpoint=endpoint)
