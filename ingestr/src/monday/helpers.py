from typing import Any, Dict, Iterator, Optional

from ingestr.src.http_client import create_client

from .settings import ACCOUNT_QUERY, ACCOUNT_ROLES_QUERY, APP_INSTALLS_QUERY, BOARDS_QUERY, MAX_PAGE_SIZE, UPDATES_QUERY, USERS_QUERY, WEBHOOKS_QUERY, WORKSPACES_QUERY


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

    normalized = {}

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

    def _execute_query(self, query: str, variables: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        """Execute a GraphQL query against Monday.com API."""
        payload = {"query": query}
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
        data = self._execute_query(ACCOUNT_ROLES_QUERY)
        roles = data.get("account_roles", [])

        for role in roles:
            yield normalize_dict(role)

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
        First gets all boards to extract board IDs,
        then fetches webhooks for each board.

        Yields:
            Dict containing webhook data
        """
        # Collect board IDs from boards
        for board in _paginate(self, BOARDS_QUERY, "boards", MAX_PAGE_SIZE):
            board_id = board.get("id")
            if not board_id:
                continue

            # Fetch webhooks for this board
            variables = {"board_id": str(board_id)}
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
        variables = {"limit": min(limit, MAX_PAGE_SIZE)}

        if start_date:
            variables["from_date"] = start_date
        if end_date:
            variables["to_date"] = end_date

        data = self._execute_query(UPDATES_QUERY, variables)
        updates = data.get("updates", [])

        for update in updates:
            yield normalize_dict(update)
