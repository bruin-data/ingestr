"""
Helper functions for Mailchimp source.
"""

from typing import Any, Iterator

import dlt


def fetch_paginated(
    session,
    url: str,
    auth: tuple,
    data_key: str | None = None,
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
        ),
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

    def fetch_data() -> Iterator[dict[str, Any]]:
        url = f"{base_url}/{path}"
        yield from fetch_paginated(session, url, auth, data_key=key)

    # Apply the resource decorator with conditional primary_key
    if pk is not None:
        return dlt.resource(
            fetch_data,
            name=name,
            write_disposition="replace",
            primary_key=pk,
        )
    else:
        return dlt.resource(
            fetch_data,
            name=name,
            write_disposition="replace",
        )


def create_nested_resource(
    base_url: str,
    session,
    auth: tuple,
    parent_resource_name: str,
    parent_path: str,
    parent_key: str,
    parent_id_field: str,
    nested_name: str,
    nested_path: str,
    nested_key: str | None,
    pk: str | None,
):
    """
    Create a nested DLT resource that depends on a parent resource.

    Args:
        base_url: Base API URL
        session: HTTP session
        auth: Authentication tuple
        parent_resource_name: Name of the parent resource
        parent_path: Parent API endpoint path
        parent_key: Data key in parent response
        parent_id_field: Field name for parent ID
        nested_name: Nested resource name
        nested_path: Nested API endpoint path (with {id} placeholder)
        nested_key: Data key in nested response (None to return whole response)
        pk: Primary key field (optional)

    Returns:
        DLT resource function
    """

    def fetch_nested_data() -> Iterator[dict[str, Any]]:
        # First, fetch parent items
        parent_url = f"{base_url}/{parent_path}"
        parent_items = fetch_paginated(session, parent_url, auth, data_key=parent_key)

        # For each parent item, fetch nested data
        for parent_item in parent_items:
            parent_id = parent_item.get(parent_id_field)
            if parent_id:
                # Build nested URL with parent ID
                nested_url = f"{base_url}/{nested_path.format(id=parent_id)}"

                # Fetch nested data
                response = session.get(nested_url, auth=auth)
                response.raise_for_status()
                data = response.json()

                # Extract nested items or return whole response
                if nested_key and nested_key in data:
                    items = data[nested_key]
                    if isinstance(items, list):
                        for item in items:
                            # Add parent reference
                            item[f"{parent_resource_name}_id"] = parent_id
                            yield item
                    else:
                        items[f"{parent_resource_name}_id"] = parent_id
                        yield items
                else:
                    # Return whole response with parent reference
                    data[f"{parent_resource_name}_id"] = parent_id
                    yield data

    # Apply the resource decorator with conditional primary_key
    if pk is not None:
        return dlt.resource(
            fetch_nested_data,
            name=nested_name,
            write_disposition="replace",
            primary_key=pk,
        )
    else:
        return dlt.resource(
            fetch_nested_data,
            name=nested_name,
            write_disposition="replace",
        )
