from typing import Any, Dict, Iterator, Optional

from ingestr.src.http_client import create_client

from .settings import (
    ACCOUNT_QUERY,
    ACCOUNT_ROLES_QUERY,
    BOARD_COLUMNS_QUERY,
    BOARD_VIEWS_QUERY,
    BOARDS_QUERY,
    CUSTOM_ACTIVITIES_QUERY,
    MAX_PAGE_SIZE,
    TAGS_QUERY,
    TEAMS_QUERY,
    UPDATES_QUERY,
    USERS_QUERY,
    WEBHOOKS_QUERY,
    WORKSPACES_QUERY,
)


def _paginate(
    client: "MondayClient",
    query: str,
    field_name: str,
    limit: int = 100,
    extra_variables: Optional[Dict[str, Any]] = None,
) -> Iterator[Dict[str, Any]]:
    """
    Helper function to paginate through Monday.com API results.

    Args:
        client: MondayClient instance
        query: GraphQL query with $limit and $page variables
        field_name: Name of the field in the response to extract
        limit: Number of results per page
        extra_variables: Additional variables to pass to the query

    Yields:
        Normalized dictionaries from the API response
    """
    page = 1

    while True:
        variables = {
            "limit": min(limit, MAX_PAGE_SIZE),
            "page": page,
        }

        if extra_variables:
            variables.update(extra_variables)

        data = client._execute_query(query, variables)
        items = data.get(field_name, [])

        if not items:
            break

        for item in items:
            yield normalize_dict(item)

        if len(items) < limit:
            break

        page += 1


def _get_all_board_ids(client: "MondayClient") -> list[str]:
    """
    Collect all board IDs from the Monday.com API.

    Args:
        client: MondayClient instance

    Returns:
        List of board IDs as strings
    """
    board_ids = []
    for board in _paginate(client, BOARDS_QUERY, "boards", MAX_PAGE_SIZE):
        board_id = board.get("id")
        if board_id:
            board_ids.append(str(board_id))
    return board_ids


def _fetch_nested_board_data(
    client: "MondayClient", query: str, nested_field: str
) -> Iterator[Dict[str, Any]]:
    """
    Fetch nested data from boards (columns, views, etc).

    Args:
        client: MondayClient instance
        query: GraphQL query to execute
        nested_field: Name of the nested field to extract (e.g., "columns", "views")

    Yields:
        Dict containing nested data with board_id added
    """
    board_ids = _get_all_board_ids(client)

    if not board_ids:
        return

    for board_id in board_ids:
        variables = {"board_ids": [board_id]}
        data = client._execute_query(query, variables)
        boards = data.get("boards", [])

        for board in boards:
            nested_items = board.get(nested_field, [])

            if nested_items and isinstance(nested_items, list):
                for item in nested_items:
                    item_data = item.copy()
                    item_data["board_id"] = board.get("id")
                    yield normalize_dict(item_data)


def _fetch_simple_list(
    client: "MondayClient", query: str, field_name: str
) -> Iterator[Dict[str, Any]]:
    """
    Fetch a simple list of items from Monday.com API without pagination.

    Args:
        client: MondayClient instance
        query: GraphQL query to execute
        field_name: Name of the field in the response to extract

    Yields:
        Normalized dictionaries from the API response
    """
    data = client._execute_query(query)
    items = data.get(field_name, [])

    for item in items:
        yield normalize_dict(item)


def normalize_dict(data: Dict[str, Any]) -> Dict[str, Any]:
    """
    Normalize dictionary fields by detecting their structure:
    - Convert nested objects with 'id' field to {field_name}_id
    - Convert objects with other fields to flattened {field_name}_{subfield}
    - Convert arrays to JSON strings for storage
    - Preserve null values

    Args:
        data: The dictionary to normalize

    Returns:
        Normalized dictionary with flattened structure

    Example:
        >>> normalize_dict({"user": {"id": "123"}, "plan": {"tier": "pro"}})
        {"user_id": "123", "plan_tier": "pro"}
    """
    import json

    normalized: Dict[str, Any] = {}

    for key, value in data.items():
        if value is None:
            # Keep null values as-is
            normalized[key] = None
        elif isinstance(value, dict):
            # If the dict has only an 'id' field, replace with {key}_id
            if "id" in value and len(value) == 1:
                normalized[f"{key}_id"] = value["id"]
            # If dict has multiple fields, flatten them
            elif value:
                for subkey, subvalue in value.items():
                    normalized[f"{key}_{subkey}"] = subvalue
        elif isinstance(value, list):
            # If list contains dicts with only 'id' field, extract ids
            if value and isinstance(value[0], dict) and list(value[0].keys()) == ["id"]:
                normalized[key] = [item["id"] for item in value]
            else:
                # Convert other lists to JSON strings for storage
                normalized[key] = json.dumps(value)
        else:
            # Add scalar values directly
            normalized[key] = value

    return normalized


class MondayClient:
    """Monday.com GraphQL API client."""

    def __init__(self, api_token: str) -> None:
        self.api_token = api_token
        self.base_url = "https://api.monday.com/v2"
        self.session = create_client()

    def _headers(self) -> Dict[str, str]:
        return {
            "Authorization": self.api_token,
            "Content-Type": "application/json",
        }

    def _execute_query(
        self, query: str, variables: Optional[Dict[str, Any]] = None
    ) -> Dict[str, Any]:
        """Execute a GraphQL query against Monday.com API."""
        payload: Dict[str, Any] = {"query": query}
        if variables:
            payload["variables"] = variables

        response = self.session.post(
            self.base_url,
            headers=self._headers(),
            json=payload,
        )
        response.raise_for_status()
        data = response.json()

        if "errors" in data:
            raise Exception(f"GraphQL errors: {data['errors']}")

        return data.get("data", {})

    def get_account(self) -> Dict[str, Any]:
        """
        Fetch account information from Monday.com API.

        Returns:
            Dict containing account data
        """
        data = self._execute_query(ACCOUNT_QUERY)
        account = data.get("account", {})

        if not account:
            raise Exception("No account data returned from Monday.com API")

        return normalize_dict(account)

    def get_account_roles(self) -> Iterator[Dict[str, Any]]:
        """
        Fetch account roles from Monday.com API.

        Yields:
            Dict containing account role data
        """
        yield from _fetch_simple_list(self, ACCOUNT_ROLES_QUERY, "account_roles")

    def get_users(self, limit: int = MAX_PAGE_SIZE) -> Iterator[Dict[str, Any]]:
        """
        Fetch users from Monday.com API with pagination.

        Args:
            limit: Number of results per page (max 100)

        Yields:
            Dict containing user data
        """
        yield from _paginate(self, USERS_QUERY, "users", limit)

    def get_boards(self, limit: int = MAX_PAGE_SIZE) -> Iterator[Dict[str, Any]]:
        """
        Fetch boards from Monday.com API with pagination.

        Args:
            limit: Number of results per page (max 100)

        Yields:
            Dict containing board data
        """
        yield from _paginate(self, BOARDS_QUERY, "boards", limit)

    def get_workspaces(self) -> Iterator[Dict[str, Any]]:
        """
        Fetch workspaces from Monday.com API.
        First gets all boards to extract unique workspace IDs,
        then fetches workspace details.

        Yields:
            Dict containing workspace data
        """
        # Collect unique workspace IDs from boards
        workspace_ids = set()
        for board in _paginate(self, BOARDS_QUERY, "boards", MAX_PAGE_SIZE):
            workspace_id = board.get("workspace_id")
            if workspace_id:
                workspace_ids.add(str(workspace_id))

        if not workspace_ids:
            return

        # Fetch workspace details
        variables = {"ids": list(workspace_ids)}
        data = self._execute_query(WORKSPACES_QUERY, variables)
        workspaces = data.get("workspaces", [])

        for workspace in workspaces:
            yield normalize_dict(workspace)

    def get_webhooks(self) -> Iterator[Dict[str, Any]]:
        """
        Fetch webhooks from Monday.com API.
        First gets all board IDs, then fetches webhooks for each board.

        Yields:
            Dict containing webhook data
        """
        board_ids = _get_all_board_ids(self)

        for board_id in board_ids:
            variables = {"board_id": board_id}
            data = self._execute_query(WEBHOOKS_QUERY, variables)
            webhooks = data.get("webhooks", [])

            for webhook in webhooks:
                yield normalize_dict(webhook)

    def get_updates(
        self,
        limit: int = MAX_PAGE_SIZE,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Fetch updates from Monday.com API.

        Args:
            limit: Number of results (max 100)
            start_date: Start date in YYYY-MM-DD format
            end_date: End date in YYYY-MM-DD format

        Yields:
            Dict containing update data
        """
        variables: Dict[str, Any] = {"limit": min(limit, MAX_PAGE_SIZE)}

        if start_date:
            variables["from_date"] = start_date
        if end_date:
            variables["to_date"] = end_date

        data = self._execute_query(UPDATES_QUERY, variables)
        updates = data.get("updates", [])

        for update in updates:
            yield normalize_dict(update)

    def get_teams(self) -> Iterator[Dict[str, Any]]:
        """
        Fetch teams from Monday.com API.

        Yields:
            Dict containing team data
        """
        yield from _fetch_simple_list(self, TEAMS_QUERY, "teams")

    def get_tags(self) -> Iterator[Dict[str, Any]]:
        """
        Fetch tags from Monday.com API.

        Yields:
            Dict containing tag data
        """
        yield from _fetch_simple_list(self, TAGS_QUERY, "tags")

    def get_custom_activities(self) -> Iterator[Dict[str, Any]]:
        """
        Fetch custom activities from Monday.com API.

        Yields:
            Dict containing custom activity data
        """
        yield from _fetch_simple_list(self, CUSTOM_ACTIVITIES_QUERY, "custom_activity")

    def get_board_columns(self) -> Iterator[Dict[str, Any]]:
        """
        Fetch board columns from Monday.com API.
        First gets all board IDs, then fetches columns for each board.

        Yields:
            Dict containing board column data with board_id
        """
        yield from _fetch_nested_board_data(self, BOARD_COLUMNS_QUERY, "columns")

    def get_board_views(self) -> Iterator[Dict[str, Any]]:
        """
        Fetch board views from Monday.com API.
        First gets all board IDs, then fetches views for each board.

        Yields:
            Dict containing board view data with board_id
        """
        yield from _fetch_nested_board_data(self, BOARD_VIEWS_QUERY, "views")
