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
        buckets,
        scopes,
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


@dlt.resource(write_disposition="replace", max_table_nesting=0)
def buckets(
    base_url: str = dlt.secrets.value,
    username: str = dlt.secrets.value,
    password: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches all buckets from Couchbase with detailed information.

    This includes:
    - Bucket name, type, and storage backend
    - Node locator and vBucket configuration
    - Bucket capabilities and manifest
    - VBucket server map and replication settings
    - Node information and statistics
    - Controllers and URIs
    - Quota and basic stats
    - Eviction policy and conflict resolution

    Args:
        base_url (str): Couchbase server URL (e.g., https://localhost:18091)
        username (str): Username for authentication
        password (str): Password for authentication

    Yields:
        dict: Detailed bucket information for each bucket.
    """
    client = get_client(base_url, username, password)
    yield from client.get_buckets()


@dlt.resource(write_disposition="replace", max_table_nesting=0)
def scopes(
    base_url: str = dlt.secrets.value,
    username: str = dlt.secrets.value,
    password: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches scopes and collections for all buckets from Couchbase.

    This includes:
    - Scope name and UID
    - Collections within each scope
    - Collection name, UID, maxTTL, and history settings

    Args:
        base_url (str): Couchbase server URL (e.g., https://localhost:18091)
        username (str): Username for authentication
        password (str): Password for authentication

    Yields:
        dict: Scopes and collections information for each bucket.
    """
    client = get_client(base_url, username, password)
    all_buckets = client.get_buckets()

    for bucket in all_buckets:
        bucket_name = bucket.get("name")
        if bucket_name:
            scopes_data = client.get_bucket_scopes(bucket_name)
            # Add bucket name to the response for context
            scopes_data["bucket_name"] = bucket_name
            yield scopes_data
