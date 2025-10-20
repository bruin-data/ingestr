"""Helper functions for Couchbase source."""

from typing import Any, Dict, Iterable

import requests
from requests.auth import HTTPBasicAuth


def get_couchbase_data(
    host: str,
    port: int,
    username: str,
    password: str,
    endpoint: str,
    use_ssl: bool = False,
) -> Iterable[Dict[str, Any]]:
    """
    Gets data from a Couchbase Server REST API endpoint.

    Args:
        host (str): The Couchbase server hostname
        port (int): The REST API port
        username (str): Username for authentication
        password (str): Password for authentication
        endpoint (str): The API endpoint path (e.g., 'pools/default')
        use_ssl (bool): Whether to use HTTPS (for Capella/Cloud). Default: False

    Yields:
        Dict[str, Any]: Data from the endpoint
    """
    protocol = "https" if use_ssl else "http"
    url = f"{protocol}://{host}:{port}/{endpoint}"
    response = requests.get(url, auth=HTTPBasicAuth(username, password), verify=True)
    response.raise_for_status()
    data = response.json()

    # Yield the complete data
    yield data


def get_capella_cloud_data(
    api_token: str,
    endpoint: str,
) -> Iterable[Dict[str, Any]]:
    """
    Gets data from Couchbase Capella Cloud API endpoint.

    Args:
        api_token (str): Capella Cloud API Bearer token (Base64 encoded credentials)
        endpoint (str): The API endpoint path (e.g., 'v4/organizations/{org_id}/users')

    Yields:
        Dict[str, Any]: Data from the endpoint
    """
    url = f"https://cloudapi.cloud.couchbase.com/{endpoint}"
    headers = {"Authorization": f"Bearer {api_token}"}
    response = requests.get(url, headers=headers, verify=True)
    response.raise_for_status()
    data = response.json()

    # Yield the complete data
    yield data
