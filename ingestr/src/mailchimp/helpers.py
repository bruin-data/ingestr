"""
Helper functions for Mailchimp source.
"""

from typing import Any, Iterator

import dlt


def fetch_paginated(
    session,
    url: str,
    auth: tuple,
    data_key: str = None,
) -> Iterator[dict[str, Any]]:
    """
    Helper function to fetch paginated data from Mailchimp API.

    Args:
        session: HTTP session
        url: API endpoint URL
        auth: Authentication tuple
        data_key: Key in response containing the data array (if None, return whole response)

    Yields:
        Individual items from the paginated response
    """
    offset = 0
    count = 1000  # Maximum allowed by Mailchimp

    while True:
        params = {"count": count, "offset": offset}
        response = session.get(url, auth=auth, params=params)
        response.raise_for_status()
        data = response.json()

        # Extract items from response
        if data_key and data_key in data:
            items = data[data_key]
        elif isinstance(data, list):
            items = data
        else:
            # If no data_key specified and response is dict, yield the whole response
            yield data
            break

        if not items:
            break

        yield from items

        # Check if we've received fewer items than requested (last page)
        if len(items) < count:
            break

        offset += count


def create_merge_resource(
    base_url: str,
    session,
    auth: tuple,
    name: str,
    path: str,
    key: str,
    pk: str,
    ik: str,
):
    """
    Create a DLT resource with merge disposition for incremental loading.

    Args:
        base_url: Base API URL
        session: HTTP session
        auth: Authentication tuple
        name: Resource name
        path: API endpoint path
        key: Data key in response
        pk: Primary key field
        ik: Incremental key field

    Returns:
        DLT resource function
    """
    @dlt.resource(
        name=name,
        write_disposition="merge",
        primary_key=pk,
    )
    def fetch_data(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            ik, initial_value=None
        )
    ) -> Iterator[dict[str, Any]]:
        url = f"{base_url}/{path}"
        yield from fetch_paginated(session, url, auth, data_key=key)

    return fetch_data


def create_replace_resource(
    base_url: str,
    session,
    auth: tuple,
    name: str,
    path: str,
    key: str,
    pk: str | None,
):
    """
    Create a DLT resource with replace disposition.

    Args:
        base_url: Base API URL
        session: HTTP session
        auth: Authentication tuple
        name: Resource name
        path: API endpoint path
        key: Data key in response
        pk: Primary key field (optional)

    Returns:
        DLT resource function
    """
    @dlt.resource(
        name=name,
        write_disposition="replace",
        primary_key=pk,
    )
    def fetch_data() -> Iterator[dict[str, Any]]:
        url = f"{base_url}/{path}"
        yield from fetch_paginated(session, url, auth, data_key=key)

    return fetch_data
