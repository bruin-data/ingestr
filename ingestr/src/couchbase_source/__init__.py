"""Source that loads data from Couchbase Server.

This source provides data extraction from Couchbase Server using the REST API.
"""

from typing import Any, Dict, Iterable

import dlt
import requests
from dlt.sources import DltResource
from requests.auth import HTTPBasicAuth


@dlt.source(max_table_nesting=0)
def couchbase_source(
    host: str = dlt.config.value,
    username: str = dlt.secrets.value,
    password: str = dlt.secrets.value,
    port: int = 8091,
) -> Iterable[DltResource]:
    """
    Loads data from Couchbase Server pools/default endpoint.

    Args:
        host (str): The Couchbase server hostname or IP address (e.g., 'localhost')
        username (str): The username for authentication (e.g., 'Administrator')
        password (str): The password for authentication
        port (int): The REST API port (default: 8091)

    Yields:
        DltResource: Resource containing pools/default data
    """
    yield dlt.resource(
        get_pools_default(host, port, username, password),
        name="pools_default",
        write_disposition="replace",
    )


def get_pools_default(
    host: str, port: int, username: str, password: str
) -> Iterable[Dict[str, Any]]:
    """
    Gets pools/default information from Couchbase Server.

    Args:
        host (str): The Couchbase server hostname
        port (int): The REST API port
        username (str): Username for authentication
        password (str): Password for authentication

    Yields:
        Dict[str, Any]: Pools default information
    """
    url = f"http://{host}:{port}/pools/default"
    response = requests.get(url, auth=HTTPBasicAuth(username, password))
    response.raise_for_status()
    data = response.json()

    # Yield the complete pools/default data
    yield data
