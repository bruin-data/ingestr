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


def _normalize_issue(item: Dict[str, Any]) -> Dict[str, Any]:
    field_mapping = {
        "assignee": "assignee_id",
        "creator": "creator_id",
        "state": "state_id",
        "cycle": "cycle_id",
        "project": "project_id",
    }
    for key, value in field_mapping.items():
        if item.get(key):
            item[value] = item[key]["id"]
            del item[key]
        else:
            item[value] = None
            del item[key]
    json_fields = [
        "comments",
        "subscribers",
        "attachments",
        "labels",
        "subtasks",
        "projects",
        "memberships",
        "members",
    ]
    for field in json_fields:
        if item.get(field):
            item[f"{field}"] = item[field].get("nodes", [])

    return item


def _normalize_team(item: Dict[str, Any]) -> Dict[str, Any]:
    json_fields = ["memberships", "members", "projects"]
    for field in json_fields:
        if item.get(field):
            item[f"{field}"] = item[field].get("nodes", [])
    return item
