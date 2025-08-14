from typing import Any, Dict, Iterator, Optional

import dlt
import pendulum
import requests

LINEAR_GRAPHQL_ENDPOINT = "https://api.linear.app/graphql"


def _graphql(
    api_key: str, query: str, variables: Optional[Dict[str, Any]] = None
) -> Dict[str, Any]:
    headers = {"Authorization": api_key, "Content-Type": "application/json"}
    response = requests.post(
        LINEAR_GRAPHQL_ENDPOINT,
        json={"query": query, "variables": variables or {}},
        headers=headers,
    )
    response.raise_for_status()
    payload = response.json()
    if "errors" in payload:
        raise ValueError(str(payload["errors"]))
    return payload["data"]


def _paginate(api_key: str, query: str, root: str) -> Iterator[Dict[str, Any]]:
    cursor: Optional[str] = None
    while True:
        data = _graphql(api_key, query, {"cursor": cursor})[root]
        for item in data["nodes"]:
            yield item
        if not data["pageInfo"]["hasNextPage"]:
            break
        cursor = data["pageInfo"]["endCursor"]


def _get_date_range(updated_at, start_date):
    """Extract current start and end dates from incremental state."""
    if updated_at.last_value:
        current_start_date = pendulum.parse(updated_at.last_value)
    else:
        current_start_date = pendulum.parse(start_date)

    if updated_at.end_value:
        current_end_date = pendulum.parse(updated_at.end_value)
    else:
        current_end_date = pendulum.now(tz="UTC")

    return current_start_date, current_end_date


def _paginated_resource(
    api_key: str, query: str, query_field: str, updated_at, start_date
) -> Iterator[Dict[str, Any]]:
    """Helper function for paginated resources with date filtering."""
    current_start_date, current_end_date = _get_date_range(updated_at, start_date)

    for item in _paginate(api_key, query, query_field):
        if pendulum.parse(item["updatedAt"]) >= current_start_date:
            if pendulum.parse(item["updatedAt"]) <= current_end_date:
                yield normalize_dictionaries(item)


def _create_paginated_resource(
    resource_name: str,
    query: str,
    query_field: str,
    api_key: str,
    start_date,
    end_date=None,
):
    """Factory function to create paginated resources dynamically."""

    @dlt.resource(name=resource_name, primary_key="id", write_disposition="merge")
    def paginated_resource(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date.isoformat(),
            end_value=end_date.isoformat() if end_date else None,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterator[Dict[str, Any]]:
        for item in _paginated_resource(
            api_key, query, query_field, updated_at, start_date
        ):
            yield normalize_dictionaries(item)

    return paginated_resource


def normalize_dictionaries(item: Dict[str, Any]) -> Dict[str, Any]:
    """
    Automatically normalize dictionary fields by detecting their structure:
    - Convert nested objects with 'id' field to {field_name}_id
    - Convert objects with 'nodes' field to arrays

    """
    normalized_item = item.copy()

    for key, value in list(normalized_item.items()):
        if isinstance(value, dict):
            # If the dict has an 'id' field, replace with {key}_id
            if "id" in value:
                normalized_item[f"{key}_id"] = value["id"]
                del normalized_item[key]
            # If the dict has 'nodes' field, extract the nodes array
            elif "nodes" in value:
                normalized_item[key] = value["nodes"]

    return normalized_item
