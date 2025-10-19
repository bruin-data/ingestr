"""
This source provides data extraction from Couchbase via the REST API.

The source only performs GET requests to extract data.
"""

from typing import Any, Iterable

import dlt
from dlt.common.typing import TDataItem

from .helpers import get_client


@dlt.source
def couchbase_source() -> Any:
    """
    The main function that runs all the other functions to fetch data from Couchbase.

    Returns:
        Sequence[DltResource]: A sequence of DltResource objects containing the fetched data.
    """
    return [
        pools_default,
    ]


@dlt.resource(write_disposition="replace", max_table_nesting=0)
def pools_default(
    base_url: str = dlt.secrets.value,
    username: str = dlt.secrets.value,
    password: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches default pool information from Couchbase.

    This includes:
    - Rebalance status and progress
    - Node status and services
    - Bucket and collection counts
    - Services and buckets that need rebalance
    - Server groups information

    Args:
        base_url (str): Couchbase server URL (e.g., https://localhost:18091)
        username (str): Username for authentication
        password (str): Password for authentication

    Yields:
        dict: The default pool information data.
    """
    client = get_client(base_url, username, password)
    yield client.get_pools_default()
