"""Helper functions for the Anthropic source."""

import logging
from typing import Any, Dict, Iterator, List, Optional

import requests

logger = logging.getLogger(__name__)


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

    base_url = "https://api.anthropic.com/v1/organizations/usage_report/claude_code"
    headers = {
        "anthropic-version": "2023-06-01",
        "x-api-key": api_key,
        "User-Agent": "ingestr/1.0.0 (https://github.com/bruin-data/ingestr)",
    }

    params: Dict[str, Any] = {
        "starting_at": date,
        "limit": min(limit, 1000),  # API max is 1000
    }

    has_more = True
    next_page = None

    while has_more:
        # Update params with pagination cursor if available
        if next_page:
            params["page"] = next_page
            # Remove limit from subsequent requests as page cursor includes it
            params.pop("limit", None)

        try:
            response = requests.get(base_url, headers=headers, params=params)
            response.raise_for_status()

            data = response.json()

            # Process each record and flatten the structure
            for record in data.get("data", []):
                yield flatten_usage_record(record)

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
                # No data for this date, return empty
                logger.info(f"No data available for date {date}")
                return
            else:
                raise Exception(f"API request failed: {e}")
        except requests.exceptions.RequestException as e:
            # Handle specific request exceptions
            if hasattr(e, "response") and e.response is not None:
                if e.response.status_code == 401:
                    raise ValueError(
                        "Invalid API key. Please ensure you're using an Admin API key "
                        "(starts with sk-ant-admin...) and have the necessary permissions."
                    )
                elif e.response.status_code == 404:
                    # No data for this date, return empty
                    logger.info(f"No data available for date {date}")
                    return
            raise Exception(f"API request failed: {e}")
        except Exception as e:
            raise Exception(f"Failed to fetch Claude Code usage data: {e}")


def flatten_usage_record(record: Dict[str, Any]) -> Dict[str, Any]:
    """
    Flatten a nested usage record into a flat structure suitable for database loading.

    Args:
        record: Nested usage record from API

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

    # Extract core metrics
    core_metrics = record.get("core_metrics", {})
    lines_of_code = core_metrics.get("lines_of_code", {})

    # Extract tool actions
    tool_actions = record.get("tool_actions", {})

    # Start building the flattened record
    flattened = {
        "date": record.get("date"),
        "actor_type": actor_type,
        "actor_id": actor_id,
        "organization_id": record.get("organization_id"),
        "customer_type": record.get("customer_type"),
        "terminal_type": record.get("terminal_type"),
        # Core metrics
        "num_sessions": core_metrics.get("num_sessions", 0),
        "lines_added": lines_of_code.get("added", 0),
        "lines_removed": lines_of_code.get("removed", 0),
        "commits_by_claude_code": core_metrics.get("commits_by_claude_code", 0),
        "pull_requests_by_claude_code": core_metrics.get(
            "pull_requests_by_claude_code", 0
        ),
        # Tool actions - Edit tool
        "edit_tool_accepted": tool_actions.get("edit_tool", {}).get("accepted", 0),
        "edit_tool_rejected": tool_actions.get("edit_tool", {}).get("rejected", 0),
        # Tool actions - MultiEdit tool
        "multi_edit_tool_accepted": tool_actions.get("multi_edit_tool", {}).get(
            "accepted", 0
        ),
        "multi_edit_tool_rejected": tool_actions.get("multi_edit_tool", {}).get(
            "rejected", 0
        ),
        # Tool actions - Write tool
        "write_tool_accepted": tool_actions.get("write_tool", {}).get("accepted", 0),
        "write_tool_rejected": tool_actions.get("write_tool", {}).get("rejected", 0),
        # Tool actions - NotebookEdit tool
        "notebook_edit_tool_accepted": tool_actions.get("notebook_edit_tool", {}).get(
            "accepted", 0
        ),
        "notebook_edit_tool_rejected": tool_actions.get("notebook_edit_tool", {}).get(
            "rejected", 0
        ),
    }

    # Process model breakdown - aggregate totals across all models
    model_breakdown = record.get("model_breakdown", [])

    total_input_tokens = 0
    total_output_tokens = 0
    total_cache_read_tokens = 0
    total_cache_creation_tokens = 0
    total_estimated_cost_cents = 0

    # Also track per-model usage
    models_used = []

    for model_data in model_breakdown:
        model_name = model_data.get("model", "")
        tokens = model_data.get("tokens", {})
        cost = model_data.get("estimated_cost", {})

        input_tokens = tokens.get("input", 0)
        output_tokens = tokens.get("output", 0)
        cache_read = tokens.get("cache_read", 0)
        cache_creation = tokens.get("cache_creation", 0)
        cost_cents = cost.get("amount", 0)

        # Add to totals
        total_input_tokens += input_tokens
        total_output_tokens += output_tokens
        total_cache_read_tokens += cache_read
        total_cache_creation_tokens += cache_creation
        total_estimated_cost_cents += cost_cents

        # Track which models were used
        if model_name:
            models_used.append(model_name)

    # Add token and cost totals to flattened record
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


def fetch_usage_report(
    api_key: str,
    starting_at: str,
    ending_at: str,
    bucket_width: str = "1d",
    group_by: Optional[List[str]] = None,
    models: Optional[List[str]] = None,
    service_tiers: Optional[List[str]] = None,
    context_window: Optional[List[str]] = None,
    api_key_ids: Optional[List[str]] = None,
    workspace_ids: Optional[List[str]] = None,
    limit: int = 100,
) -> Iterator[Dict[str, Any]]:
    """
    Fetch usage report data from the messages endpoint.

    Args:
        api_key: Anthropic Admin API key
        starting_at: Start datetime in ISO format
        ending_at: End datetime in ISO format
        bucket_width: Time bucket width (1m, 1h, or 1d)
        group_by: List of fields to group by
        models: Filter by specific models
        service_tiers: Filter by service tiers
        context_window: Filter by context window size
        api_key_ids: Filter by API key IDs
        workspace_ids: Filter by workspace IDs
        limit: Number of records per page

    Yields:
        Flattened usage records
    """

    base_url = "https://api.anthropic.com/v1/organizations/usage_report/messages"
    headers = {
        "anthropic-version": "2023-06-01",
        "x-api-key": api_key,
        "User-Agent": "ingestr/1.0.0 (https://github.com/bruin-data/ingestr)",
    }

    # Adjust limit based on bucket_width
    max_limit = 31 if bucket_width == "1d" else (168 if bucket_width == "1h" else 1440)

    params: Dict[str, Any] = {
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

    has_more = True
    next_page = None

    while has_more:
        if next_page:
            params["page"] = next_page
            # Remove limit from subsequent requests as page cursor includes it
            params.pop("limit", None)

        try:
            response = requests.get(base_url, headers=headers, params=params)
            response.raise_for_status()

            data = response.json()

            # Process each record and flatten the structure
            for record in data.get("data", []):
                yield flatten_usage_report_record(record)

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
                logger.info(
                    f"No data available for period {starting_at} to {ending_at}"
                )
                return
            elif e.response.status_code == 400:
                logger.error(f"Bad request: {e.response.text}")
                raise Exception(f"API request failed with 400: {e.response.text}")
            else:
                raise Exception(f"API request failed: {e}")
        except Exception as e:
            raise Exception(f"Failed to fetch usage data: {e}")


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


def fetch_cost_report(
    api_key: str,
    starting_at: str,
    ending_at: str,
    group_by: Optional[List[str]] = None,
    workspace_ids: Optional[List[str]] = None,
    limit: int = 100,
) -> Iterator[Dict[str, Any]]:
    """
    Fetch cost report data.

    Args:
        api_key: Anthropic Admin API key
        starting_at: Start datetime in ISO format
        ending_at: End datetime in ISO format
        group_by: List of fields to group by (workspace_id, description)
        workspace_ids: Filter by workspace IDs
        limit: Number of records per page

    Yields:
        Flattened cost records
    """

    base_url = "https://api.anthropic.com/v1/organizations/cost_report"
    headers = {
        "anthropic-version": "2023-06-01",
        "x-api-key": api_key,
        "User-Agent": "ingestr/1.0.0 (https://github.com/bruin-data/ingestr)",
    }

    # Cost report only supports daily granularity with max limit of 31
    params: Dict[str, Any] = {
        "starting_at": starting_at,
        "ending_at": ending_at,
        "limit": min(limit, 31),
    }

    # Add optional filters
    if group_by:
        for i, field in enumerate(group_by):
            params[f"group_by[{i}]"] = field
    if workspace_ids:
        for i, workspace_id in enumerate(workspace_ids):
            params[f"workspace_ids[{i}]"] = workspace_id

    has_more = True
    next_page = None

    while has_more:
        if next_page:
            params["page"] = next_page
            params.pop("limit", None)

        try:
            response = requests.get(base_url, headers=headers, params=params)
            response.raise_for_status()

            data = response.json()

            # Process each record
            for record in data.get("data", []):
                yield flatten_cost_record(record)

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
                logger.info(
                    f"No cost data available for period {starting_at} to {ending_at}"
                )
                return
            elif e.response.status_code == 400:
                logger.error(f"Bad request for cost report: {e.response.text}")
                raise Exception(f"API request failed with 400: {e.response.text}")
            else:
                raise Exception(f"API request failed: {e}")
        except Exception as e:
            raise Exception(f"Failed to fetch cost data: {e}")


def flatten_cost_record(record: Dict[str, Any]) -> Dict[str, Any]:
    """
    Flatten a cost report record.

    Args:
        record: Cost record from API

    Returns:
        Flattened record
    """

    # Extract cost in cents (convert from decimal string)
    cost_str = record.get("cost", "0")
    try:
        cost_cents = int(float(cost_str))
    except (ValueError, TypeError):
        cost_cents = 0

    return {
        "bucket": record.get("bucket", ""),
        "workspace_id": record.get("workspace_id", ""),
        "description": record.get("description", ""),
        "cost_cents": cost_cents,
        "currency": "USD",  # API always returns USD
    }


def fetch_organization_info(api_key: str) -> Dict[str, Any]:
    """
    Fetch organization information.

    Args:
        api_key: Anthropic Admin API key

    Returns:
        Organization information
    """

    base_url = "https://api.anthropic.com/v1/organizations/me"
    headers = {
        "anthropic-version": "2023-06-01",
        "x-api-key": api_key,
        "User-Agent": "ingestr/1.0.0 (https://github.com/bruin-data/ingestr)",
    }

    try:
        response = requests.get(base_url, headers=headers)
        response.raise_for_status()
        return response.json()
    except requests.exceptions.HTTPError as e:
        if e.response.status_code == 401:
            raise ValueError("Invalid API key")
        raise Exception(f"Failed to fetch organization info: {e}")


def fetch_workspaces(api_key: str, limit: int = 100) -> Iterator[Dict[str, Any]]:
    """
    Fetch all workspaces in the organization.

    Args:
        api_key: Anthropic Admin API key
        limit: Number of records per page

    Yields:
        Workspace records
    """

    base_url = "https://api.anthropic.com/v1/workspaces"
    headers = {
        "anthropic-version": "2023-06-01",
        "x-api-key": api_key,
        "User-Agent": "ingestr/1.0.0 (https://github.com/bruin-data/ingestr)",
    }

    params: Dict[str, Any] = {"limit": min(limit, 100)}
    has_more = True
    next_page = None

    while has_more:
        if next_page:
            params["page"] = next_page
            params.pop("limit", None)

        try:
            response = requests.get(base_url, headers=headers, params=params)
            response.raise_for_status()

            data = response.json()

            for workspace in data.get("data", []):
                yield workspace

            has_more = data.get("has_more", False)
            next_page = data.get("next_page")

        except requests.exceptions.HTTPError as e:
            if e.response.status_code == 401:
                raise ValueError("Invalid API key")
            elif e.response.status_code == 404:
                logger.info("No workspaces found")
                return
            raise Exception(f"Failed to fetch workspaces: {e}")


def fetch_api_keys(api_key: str, limit: int = 100) -> Iterator[Dict[str, Any]]:
    """
    Fetch all API keys in the organization.

    Args:
        api_key: Anthropic Admin API key
        limit: Number of records per page

    Yields:
        API key records
    """

    base_url = "https://api.anthropic.com/v1/api_keys"
    headers = {
        "anthropic-version": "2023-06-01",
        "x-api-key": api_key,
        "User-Agent": "ingestr/1.0.0 (https://github.com/bruin-data/ingestr)",
    }

    params: Dict[str, Any] = {"limit": min(limit, 100)}
    has_more = True
    next_page = None

    while has_more:
        if next_page:
            params["page"] = next_page
            params.pop("limit", None)

        try:
            response = requests.get(base_url, headers=headers, params=params)
            response.raise_for_status()

            data = response.json()

            for api_key_record in data.get("data", []):
                # Mask the actual key value for security
                if "value" in api_key_record:
                    api_key_record["value"] = "REDACTED"
                yield api_key_record

            has_more = data.get("has_more", False)
            next_page = data.get("next_page")

        except requests.exceptions.HTTPError as e:
            if e.response.status_code == 401:
                raise ValueError("Invalid API key")
            elif e.response.status_code == 404:
                logger.info("No API keys found")
                return
            raise Exception(f"Failed to fetch API keys: {e}")


def fetch_invites(api_key: str, limit: int = 100) -> Iterator[Dict[str, Any]]:
    """
    Fetch all invites in the organization.

    Args:
        api_key: Anthropic Admin API key
        limit: Number of records per page

    Yields:
        Invite records
    """

    base_url = "https://api.anthropic.com/v1/invites"
    headers = {
        "anthropic-version": "2023-06-01",
        "x-api-key": api_key,
        "User-Agent": "ingestr/1.0.0 (https://github.com/bruin-data/ingestr)",
    }

    params: Dict[str, Any] = {"limit": min(limit, 100)}
    has_more = True
    next_page = None

    while has_more:
        if next_page:
            params["page"] = next_page
            params.pop("limit", None)

        try:
            response = requests.get(base_url, headers=headers, params=params)
            response.raise_for_status()

            data = response.json()

            for invite in data.get("data", []):
                yield invite

            has_more = data.get("has_more", False)
            next_page = data.get("next_page")

        except requests.exceptions.HTTPError as e:
            if e.response.status_code == 401:
                raise ValueError("Invalid API key")
            elif e.response.status_code == 404:
                logger.info("No invites found")
                return
            raise Exception(f"Failed to fetch invites: {e}")


def fetch_users(api_key: str, limit: int = 100) -> Iterator[Dict[str, Any]]:
    """
    Fetch all users in the organization.

    Args:
        api_key: Anthropic Admin API key
        limit: Number of records per page

    Yields:
        User records
    """

    base_url = "https://api.anthropic.com/v1/users"
    headers = {
        "anthropic-version": "2023-06-01",
        "x-api-key": api_key,
        "User-Agent": "ingestr/1.0.0 (https://github.com/bruin-data/ingestr)",
    }

    params: Dict[str, Any] = {"limit": min(limit, 100)}
    has_more = True
    next_page = None

    while has_more:
        if next_page:
            params["page"] = next_page
            params.pop("limit", None)

        try:
            response = requests.get(base_url, headers=headers, params=params)
            response.raise_for_status()

            data = response.json()

            for user in data.get("data", []):
                yield user

            has_more = data.get("has_more", False)
            next_page = data.get("next_page")

        except requests.exceptions.HTTPError as e:
            if e.response.status_code == 401:
                raise ValueError("Invalid API key")
            elif e.response.status_code == 404:
                logger.info("No users found")
                return
            raise Exception(f"Failed to fetch users: {e}")


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

    base_url = "https://api.anthropic.com/v1/workspace_members"
    headers = {
        "anthropic-version": "2023-06-01",
        "x-api-key": api_key,
        "User-Agent": "ingestr/1.0.0 (https://github.com/bruin-data/ingestr)",
    }

    params: Dict[str, Any] = {"limit": min(limit, 100)}
    if workspace_id:
        params["workspace_id"] = workspace_id

    has_more = True
    next_page = None

    while has_more:
        if next_page:
            params["page"] = next_page
            params.pop("limit", None)

        try:
            response = requests.get(base_url, headers=headers, params=params)
            response.raise_for_status()

            data = response.json()

            for member in data.get("data", []):
                yield member

            has_more = data.get("has_more", False)
            next_page = data.get("next_page")

        except requests.exceptions.HTTPError as e:
            if e.response.status_code == 401:
                raise ValueError("Invalid API key")
            elif e.response.status_code == 404:
                logger.info("No workspace members found")
                return
            raise Exception(f"Failed to fetch workspace members: {e}")
