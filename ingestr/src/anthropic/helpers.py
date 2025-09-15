"""Helper functions for the Anthropic source using common HTTP client."""

import logging
from typing import Any, Callable, Dict, Iterator, List, Optional

import requests

from ingestr.src.http_client import create_client

logger = logging.getLogger(__name__)


class AnthropicClient:
    """HTTP client for Anthropic Admin API."""

    def __init__(self, api_key: str):
        self.api_key = api_key
        self.base_url = "https://api.anthropic.com/v1"
        self.headers = {
            "anthropic-version": "2023-06-01",
            "x-api-key": api_key,
            "User-Agent": "ingestr/1.0.0 (https://github.com/bruin-data/ingestr)",
        }
        # Create client with retry logic for common error codes
        self.client = create_client(retry_status_codes=[429, 502, 503, 504])

    def get(
        self, path: str, params: Optional[Dict[str, Any]] = None
    ) -> requests.Response:
        """Make a GET request to the Anthropic API."""
        url = f"{self.base_url}/{path}"
        return self.client.get(url, headers=self.headers, params=params)

    def fetch_paginated(
        self,
        path: str,
        params: Optional[Dict[str, Any]] = None,
        flatten_func: Optional[Callable] = None,
        handle_404: bool = False,
    ) -> Iterator[Dict[str, Any]]:
        """
        Fetch paginated data from the Anthropic API.

        Args:
            path: API endpoint path
            params: Query parameters
            flatten_func: Optional function to flatten records
            handle_404: If True, treat 404 as empty result instead of error

        Yields:
            Flattened records
        """
        if params is None:
            params = {}

        # Make a copy to avoid modifying the original
        params = dict(params)

        has_more = True
        next_page = None

        while has_more:
            current_params = dict(params)

            if next_page:
                current_params["page"] = next_page
                # Remove limit from subsequent requests as page cursor includes it
                current_params.pop("limit", None)

            try:
                response = self.get(path, current_params)
                response.raise_for_status()

                data = response.json()

                # Process each record
                for record in data.get("data", []):
                    if flatten_func:
                        yield flatten_func(record)
                    else:
                        yield record

                # Check for more pages
                has_more = data.get("has_more", False)
                next_page = data.get("next_page")

            except requests.exceptions.HTTPError as e:
                if e.response.status_code == 401:
                    raise ValueError(
                        "Invalid API key. Please ensure you're using an Admin API key "
                        "(starts with sk-ant-admin...) and have the necessary permissions."
                    )
                elif e.response.status_code == 404:
                    if handle_404:
                        logger.info(f"No data available for {path}")
                        break
                    else:
                        logger.info(f"No data found at {path}")
                        break
                elif e.response.status_code == 400:
                    error_msg = e.response.text
                    raise ValueError(f"Bad request: {error_msg}")
                else:
                    raise Exception(f"API request failed: {e}")
            except Exception as e:
                raise Exception(f"Failed to fetch data: {e}")

    def fetch_single(self, path: str) -> Optional[Dict[str, Any]]:
        """
        Fetch a single resource from the API.

        Args:
            path: API endpoint path

        Returns:
            The resource data or None if not found
        """
        try:
            response = self.get(path)
            response.raise_for_status()
            return response.json()
        except requests.exceptions.HTTPError as e:
            if e.response.status_code == 401:
                raise ValueError("Invalid API key")
            elif e.response.status_code == 404:
                return None
            raise Exception(f"Failed to fetch resource: {e}")


def flatten_usage_record(record: Dict[str, Any]) -> Dict[str, Any]:
    """
    Flatten a nested Claude Code usage record.

    Args:
        record: Nested record from API

    Returns:
        Flattened record
    """

    # Extract actor information
    actor = record.get("actor", {})
    actor_type = actor.get("type", "")
    actor_id = ""
    if actor_type == "user_actor":
        actor_id = actor.get("email_address", "")
    elif actor_type == "api_actor":
        actor_id = actor.get("api_key_name", "")

    # Start with base fields
    flattened = {
        "date": record.get("date"),
        "actor_type": actor_type,
        "actor_id": actor_id,
        "organization_id": record.get("organization_id"),
        "customer_type": record.get("customer_type"),
        "terminal_type": record.get("terminal_type"),
    }

    # Extract core metrics
    core_metrics = record.get("core_metrics", {})
    flattened.update(
        {
            "num_sessions": core_metrics.get("num_sessions", 0),
            "lines_added": core_metrics.get("lines_of_code", {}).get("added", 0),
            "lines_removed": core_metrics.get("lines_of_code", {}).get("removed", 0),
            "commits_by_claude_code": core_metrics.get("commits_by_claude_code", 0),
            "pull_requests_by_claude_code": core_metrics.get(
                "pull_requests_by_claude_code", 0
            ),
        }
    )

    # Extract tool actions (flatten nested structure)
    tool_actions = record.get("tool_actions", {})
    for tool, actions in tool_actions.items():
        flattened[f"{tool}_accepted"] = actions.get("accepted", 0)
        flattened[f"{tool}_rejected"] = actions.get("rejected", 0)

    # Extract model breakdown and aggregate totals
    model_breakdown = record.get("model_breakdown", [])
    total_input_tokens = 0
    total_output_tokens = 0
    total_cache_read_tokens = 0
    total_cache_creation_tokens = 0
    total_estimated_cost_cents = 0
    models_used = []

    for model_info in model_breakdown:
        model_name = model_info.get("model", "")
        if model_name:
            models_used.append(model_name)

        tokens = model_info.get("tokens", {})
        total_input_tokens += tokens.get("input", 0)
        total_output_tokens += tokens.get("output", 0)
        total_cache_read_tokens += tokens.get("cache_read", 0)
        total_cache_creation_tokens += tokens.get("cache_creation", 0)

        cost = model_info.get("estimated_cost", {})
        if cost.get("currency") == "USD":
            total_estimated_cost_cents += cost.get("amount", 0)

    flattened.update(
        {
            "total_input_tokens": total_input_tokens,
            "total_output_tokens": total_output_tokens,
            "total_cache_read_tokens": total_cache_read_tokens,
            "total_cache_creation_tokens": total_cache_creation_tokens,
            "total_estimated_cost_cents": total_estimated_cost_cents,
            "models_used": ",".join(models_used) if models_used else None,
        }
    )

    return flattened


def fetch_claude_code_usage(
    api_key: str,
    date: str,
    limit: int = 100,
) -> Iterator[Dict[str, Any]]:
    """
    Fetch Claude Code usage data for a specific date.

    Args:
        api_key: Anthropic Admin API key
        date: Date in YYYY-MM-DD format
        limit: Number of records per page (max 1000)

    Yields:
        Flattened usage records
    """
    client = AnthropicClient(api_key)
    params = {"starting_at": date, "ending_at": date, "limit": min(limit, 1000)}

    for record in client.fetch_paginated(
        "organizations/usage_report/claude_code",
        params=params,
        flatten_func=flatten_usage_record,
        handle_404=True,
    ):
        yield record


def flatten_usage_report_record(record: Dict[str, Any]) -> Dict[str, Any]:
    """
    Flatten a usage report record.

    Args:
        record: Nested usage record from API

    Returns:
        Flattened record
    """

    # Start with base fields - ensure bucket is never None
    flattened = {
        "bucket": record.get("bucket", ""),
        "api_key_id": record.get("api_key_id", ""),
        "workspace_id": record.get("workspace_id", ""),
        "model": record.get("model", ""),
        "service_tier": record.get("service_tier", ""),
        "context_window": record.get("context_window", ""),
    }

    # Extract token counts
    tokens = record.get("tokens", {})
    flattened.update(
        {
            "uncached_input_tokens": tokens.get("uncached_input_tokens", 0),
            "cached_input_tokens": tokens.get("cached_input_tokens", 0),
            "cache_creation_tokens": tokens.get("cache_creation_tokens", 0),
            "output_tokens": tokens.get("output_tokens", 0),
        }
    )

    # Extract server tool usage
    server_tool_usage = record.get("server_tool_usage", {})
    flattened.update(
        {
            "web_search_usage": server_tool_usage.get("web_search_usage", 0),
            "code_execution_usage": server_tool_usage.get("code_execution_usage", 0),
        }
    )

    return flattened


def fetch_usage_report(
    api_key: str,
    starting_at: str,
    ending_at: str,
    bucket_width: str = "1d",
    limit: int = 100,
    group_by: Optional[List[str]] = None,
    models: Optional[List[str]] = None,
    service_tiers: Optional[List[str]] = None,
    context_window: Optional[List[str]] = None,
    api_key_ids: Optional[List[str]] = None,
    workspace_ids: Optional[List[str]] = None,
) -> Iterator[Dict[str, Any]]:
    """
    Fetch usage report from the messages endpoint.

    Args:
        api_key: Anthropic Admin API key
        starting_at: Start date in ISO 8601 format
        ending_at: End date in ISO 8601 format
        bucket_width: Bucket width (1m, 1h, 1d)
        limit: Number of results per page
        group_by: Fields to group by
        models: Filter by models
        service_tiers: Filter by service tiers
        context_window: Filter by context window
        api_key_ids: Filter by API key IDs
        workspace_ids: Filter by workspace IDs

    Yields:
        Flattened usage records
    """
    client = AnthropicClient(api_key)

    # Adjust limit based on bucket_width
    max_limit = 31 if bucket_width == "1d" else (168 if bucket_width == "1h" else 1440)

    params = {
        "starting_at": starting_at,
        "ending_at": ending_at,
        "bucket_width": bucket_width,
        "limit": min(limit, max_limit),
    }

    # Add optional filters
    if group_by:
        for i, field in enumerate(group_by):
            params[f"group_by[{i}]"] = field
    if models:
        for i, model in enumerate(models):
            params[f"models[{i}]"] = model
    if service_tiers:
        for i, tier in enumerate(service_tiers):
            params[f"service_tiers[{i}]"] = tier
    if context_window:
        for i, window in enumerate(context_window):
            params[f"context_window[{i}]"] = window
    if api_key_ids:
        for i, key_id in enumerate(api_key_ids):
            params[f"api_key_ids[{i}]"] = key_id
    if workspace_ids:
        for i, workspace_id in enumerate(workspace_ids):
            params[f"workspace_ids[{i}]"] = workspace_id

    for record in client.fetch_paginated(
        "organizations/usage_report/messages",
        params=params,
        flatten_func=flatten_usage_report_record,
        handle_404=True,
    ):
        yield record


def fetch_cost_report(
    api_key: str,
    starting_at: str,
    ending_at: str,
    group_by: Optional[List[str]] = None,
    workspace_ids: Optional[List[str]] = None,
    limit: int = 31,
) -> Iterator[Dict[str, Any]]:
    """
    Fetch cost report data.

    Args:
        api_key: Anthropic Admin API key
        starting_at: Start date in ISO 8601 format
        ending_at: End date in ISO 8601 format
        group_by: Fields to group by
        workspace_ids: Filter by workspace IDs
        limit: Number of results per page (max 31)

    Yields:
        Cost records
    """
    client = AnthropicClient(api_key)

    params = {
        "starting_at": starting_at,
        "ending_at": ending_at,
        "limit": min(limit, 31),  # Max 31 for cost reports
    }

    # Add optional filters
    if group_by:
        for i, field in enumerate(group_by):
            params[f"group_by[{i}]"] = field
    if workspace_ids:
        for i, workspace_id in enumerate(workspace_ids):
            params[f"workspace_ids[{i}]"] = workspace_id

    def flatten_cost_record(record: Dict[str, Any]) -> Dict[str, Any]:
        """Flatten cost record with defaults for nullable fields."""
        return {
            "bucket": record.get("bucket", ""),
            "workspace_id": record.get("workspace_id", ""),
            "description": record.get("description", ""),
            "amount_cents": record.get("amount_cents", 0),
        }

    for record in client.fetch_paginated(
        "organizations/cost_report",
        params=params,
        flatten_func=flatten_cost_record,
        handle_404=True,
    ):
        yield record


def fetch_organization_info(api_key: str) -> Optional[Dict[str, Any]]:
    """
    Fetch organization information.

    Args:
        api_key: Anthropic Admin API key

    Returns:
        Organization information
    """
    client = AnthropicClient(api_key)
    return client.fetch_single("organizations/me")


def fetch_workspaces(api_key: str, limit: int = 100) -> Iterator[Dict[str, Any]]:
    """
    Fetch all workspaces in the organization.

    Args:
        api_key: Anthropic Admin API key
        limit: Number of records per page

    Yields:
        Workspace records
    """
    client = AnthropicClient(api_key)
    params = {"limit": min(limit, 100)}

    for record in client.fetch_paginated("workspaces", params=params):
        yield record


def fetch_api_keys(api_key: str, limit: int = 100) -> Iterator[Dict[str, Any]]:
    """
    Fetch all API keys in the organization.

    Args:
        api_key: Anthropic Admin API key
        limit: Number of records per page

    Yields:
        API key records
    """
    client = AnthropicClient(api_key)
    params = {"limit": min(limit, 100)}

    for record in client.fetch_paginated("api_keys", params=params):
        yield record


def fetch_invites(api_key: str, limit: int = 100) -> Iterator[Dict[str, Any]]:
    """
    Fetch all pending invites.

    Args:
        api_key: Anthropic Admin API key
        limit: Number of records per page

    Yields:
        Invite records
    """
    client = AnthropicClient(api_key)
    params = {"limit": min(limit, 100)}

    for record in client.fetch_paginated("invites", params=params):
        yield record


def fetch_users(api_key: str, limit: int = 100) -> Iterator[Dict[str, Any]]:
    """
    Fetch all users in the organization.

    Args:
        api_key: Anthropic Admin API key
        limit: Number of records per page

    Yields:
        User records
    """
    client = AnthropicClient(api_key)
    params = {"limit": min(limit, 100)}

    for record in client.fetch_paginated("users", params=params):
        yield record


def fetch_workspace_members(
    api_key: str, workspace_id: Optional[str] = None, limit: int = 100
) -> Iterator[Dict[str, Any]]:
    """
    Fetch workspace members.

    Args:
        api_key: Anthropic Admin API key
        workspace_id: Optional workspace ID to filter by
        limit: Number of records per page

    Yields:
        Workspace member records
    """
    client = AnthropicClient(api_key)
    params: Dict[str, Any] = {"limit": min(limit, 100)}
    if workspace_id:
        params["workspace_id"] = workspace_id

    for record in client.fetch_paginated("workspace_members", params=params):
        yield record
