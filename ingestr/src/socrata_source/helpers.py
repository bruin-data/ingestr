"""Socrata API helpers"""

from typing import Any, Dict, Iterator, Optional

from dlt.sources.helpers import requests

from .settings import DEFAULT_PAGE_SIZE, REQUEST_TIMEOUT


def fetch_data(
    domain: str,
    dataset_id: str,
    app_token: Optional[str] = None,
    username: Optional[str] = None,
    password: Optional[str] = None,
    incremental_key: Optional[str] = None,
    start_value: Optional[str] = None,
    end_value: Optional[str] = None,
) -> Iterator[Dict[str, Any]]:
    """
    Fetch records from Socrata dataset with pagination and optional filtering.

    Uses offset-based pagination to get all records, not just first 50000.
    Supports incremental loading via SoQL WHERE clause for server-side filtering.

    Args:
        domain: Socrata domain (e.g., "data.seattle.gov")
        dataset_id: Dataset identifier (e.g., "6udu-fhnu")
        app_token: Socrata app token for higher rate limits
        username: Username for authentication
        password: Password for authentication
        start_value: Minimum value for incremental_key (inclusive)
        end_value: Maximum value for incremental_key (exclusive)

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

    limit = DEFAULT_PAGE_SIZE
    offset = 0

    while True:
        params: Dict[str, Any] = {"$limit": limit, "$offset": offset}

        if incremental_key and start_value:
            start_value_iso = str(start_value).replace(" ", "T")
            where_conditions = [f"{incremental_key} >= '{start_value_iso}'"]

            if end_value:
                end_value_iso = str(end_value).replace(" ", "T")
                where_conditions.append(f"{incremental_key} < '{end_value_iso}'")

            params["$where"] = " AND ".join(where_conditions)
            params["$order"] = f"{incremental_key} ASC"

        response = requests.get(
            url,
            headers=headers,
            auth=auth,
            params=params,
            timeout=REQUEST_TIMEOUT,
        )
        response.raise_for_status()

        data = response.json()

        if not data:
            break

        yield data

        if len(data) < limit:
            break

        offset += limit
