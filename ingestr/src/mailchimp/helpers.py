"""
Helper functions for Mailchimp source.
"""

from typing import Any, Iterator


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
