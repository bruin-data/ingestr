"""Socrata API helpers"""

from typing import Any, Dict, Iterator, Optional

from dlt.sources.helpers import requests

REQUEST_TIMEOUT = 30  # seconds


def fetch_data(
    domain: str,
    dataset_id: str,
    app_token: Optional[str] = None,
    username: Optional[str] = None,
    password: Optional[str] = None,
) -> Iterator[Dict[str, Any]]:
    """
    Fetch all records from Socrata dataset with pagination.

    Uses offset-based pagination to get all records, not just first 50000.

    Args:
        domain: Socrata domain (e.g., "data.seattle.gov")
        dataset_id: Dataset identifier (e.g., "6udu-fhnu")
        app_token: Socrata app token for higher rate limits
        username: Username for authentication
        password: Password for authentication

    Yields:
        Lists of records (one list per page)

    Raises:
        requests.HTTPError: If API request fails
    """
    url = f"https://{domain}/resource/{dataset_id}.json"

    headers = {"Accept": "application/json"}
    if app_token:
        headers["X-App-Token"] = app_token

    auth = (username, password) if username and password else None

    # Pagination settings
    limit = 50000  # Max per page
    offset = 0

    while True:
        params = {"$limit": limit, "$offset": offset}

        response = requests.get(
            url,
            headers=headers,
            auth=auth,
            params=params,
            timeout=REQUEST_TIMEOUT,
        )
        response.raise_for_status()

        data = response.json()

        # If no data, we're done
        if not data:
            break

        # Yield this page of data
        yield data

        # If we got less than limit, this was the last page
        if len(data) < limit:
            break

        # Move to next page
        offset += limit
