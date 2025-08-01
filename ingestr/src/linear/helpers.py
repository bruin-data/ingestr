from typing import Any, Dict, Iterator, Optional

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
            if 'id' in value:
                normalized_item[f"{key}_id"] = value['id']
                del normalized_item[key]
            # If the dict has 'nodes' field, extract the nodes array
            elif 'nodes' in value:
                normalized_item[key] = value['nodes']
    
    return normalized_item
