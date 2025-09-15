"""Anthropic source for loading Claude Code usage analytics and other Anthropic API data."""

from typing import Any, Dict, Iterator, Optional, Sequence

import dlt
import pendulum
from dlt.sources import DltResource

from .helpers import (
    fetch_api_keys,
    fetch_claude_code_usage,
    fetch_cost_report,
    fetch_invites,
    fetch_organization_info,
    fetch_usage_report,
    fetch_users,
    fetch_workspace_members,
    fetch_workspaces,
)


@dlt.source(max_table_nesting=0)
def anthropic_source(
    api_key: str,
    initial_start_date: Optional[pendulum.DateTime] = None,
    end_date: Optional[pendulum.DateTime] = None,
) -> Sequence[DltResource]:
    """
    Load data from Anthropic APIs.

    Currently supports:
    - Claude Code Usage Analytics

    Args:
        api_key: Anthropic Admin API key (starts with sk-ant-admin...)
        initial_start_date: Start date for data retrieval (defaults to 2023-01-01)
        end_date: Optional end date for data retrieval

    Returns:
        Sequence of DLT resources with Anthropic data
    """

    # Default start date to 2023-01-01 if not provided
    start_date: pendulum.DateTime = (
        initial_start_date
        if initial_start_date is not None
        else pendulum.datetime(2023, 1, 1)
    )

    # Prepare end_value for incremental
    end_value_str = None
    if end_date is not None:
        end_value_str = end_date.to_date_string()

    @dlt.resource(
        name="claude_code_usage",
        write_disposition="merge",
        primary_key=["date", "actor_type", "actor_id", "terminal_type"],
    )
    def claude_code_usage(
        date: dlt.sources.incremental[str] = dlt.sources.incremental(
            "date",
            initial_value=start_date.to_date_string(),
            end_value=end_value_str,
        ),
    ) -> Iterator[Dict[str, Any]]:
        """
        Load Claude Code usage analytics data incrementally by date.

        Yields flattened records with:
        - date: The date of the usage data
        - actor_type: Type of actor (user_actor or api_actor)
        - actor_id: Email address or API key name
        - organization_id: Organization UUID
        - customer_type: api or subscription
        - terminal_type: Terminal/environment type
        - Core metrics (sessions, lines of code, commits, PRs)
        - Tool actions (accepted/rejected counts by tool)
        - Model usage and costs
        """

        # Get the date range from the incremental state
        start_value = date.last_value if date.last_value else date.initial_value
        start_date_parsed = (
            pendulum.parse(start_value) if start_value else pendulum.now()
        )

        # Ensure we have a DateTime object
        if isinstance(start_date_parsed, pendulum.DateTime):
            start_date = start_date_parsed
        elif isinstance(start_date_parsed, pendulum.Date):
            start_date = pendulum.datetime(
                start_date_parsed.year, start_date_parsed.month, start_date_parsed.day
            )
        else:
            start_date = pendulum.now()

        end_filter = pendulum.now()
        if date.end_value:
            end_filter_parsed = pendulum.parse(date.end_value)
            # Ensure we have a DateTime object
            if isinstance(end_filter_parsed, pendulum.DateTime):
                end_filter = end_filter_parsed
            elif isinstance(end_filter_parsed, pendulum.Date):
                end_filter = pendulum.datetime(
                    end_filter_parsed.year,
                    end_filter_parsed.month,
                    end_filter_parsed.day,
                )

        # Iterate through each day in the range
        current_date = start_date
        while current_date.date() <= end_filter.date():
            # Fetch data for the current date
            for record in fetch_claude_code_usage(
                api_key, current_date.to_date_string()
            ):
                yield record

            # Move to the next day
            current_date = current_date.add(days=1)

    @dlt.resource(
        name="usage_report",
        write_disposition="merge",
        primary_key=["bucket", "api_key_id", "workspace_id", "model", "service_tier"],
    )
    def usage_report() -> Iterator[Dict[str, Any]]:
        """
        Load usage report data from the messages endpoint.

        Yields records with token usage and server tool usage metrics.
        """

        # Convert dates to ISO format with timezone
        start_iso = start_date.to_iso8601_string()
        end_iso = (
            end_date.to_iso8601_string()
            if end_date
            else pendulum.now().to_iso8601_string()
        )

        for record in fetch_usage_report(
            api_key,
            starting_at=start_iso,
            ending_at=end_iso,
            bucket_width="1h",  # Hourly buckets by default
        ):
            yield record

    @dlt.resource(
        name="cost_report",
        write_disposition="merge",
        primary_key=["bucket", "workspace_id", "description"],
    )
    def cost_report() -> Iterator[Dict[str, Any]]:
        """
        Load cost report data.

        Yields records with cost breakdowns by workspace and description.
        """

        # Convert dates to ISO format with timezone
        start_iso = start_date.to_iso8601_string()
        end_iso = (
            end_date.to_iso8601_string()
            if end_date
            else pendulum.now().to_iso8601_string()
        )

        for record in fetch_cost_report(
            api_key,
            starting_at=start_iso,
            ending_at=end_iso,
        ):
            yield record

    @dlt.resource(
        name="organization",
        write_disposition="replace",
    )
    def organization() -> Iterator[Dict[str, Any]]:
        """
        Load organization information.

        Yields a single record with organization details.
        """
        org_info = fetch_organization_info(api_key)
        if org_info:
            yield org_info

    @dlt.resource(
        name="workspaces",
        write_disposition="replace",
        primary_key=["id"],
    )
    def workspaces() -> Iterator[Dict[str, Any]]:
        """
        Load all workspaces in the organization.

        Yields records with workspace details including name, type, and creation date.
        """
        for workspace in fetch_workspaces(api_key):
            yield workspace

    @dlt.resource(
        name="api_keys",
        write_disposition="replace",
        primary_key=["id"],
    )
    def api_keys() -> Iterator[Dict[str, Any]]:
        """
        Load all API keys in the organization.

        Yields records with API key details including name, status, and creation date.
        """
        for api_key_record in fetch_api_keys(api_key):
            yield api_key_record

    @dlt.resource(
        name="invites",
        write_disposition="replace",
        primary_key=["id"],
    )
    def invites() -> Iterator[Dict[str, Any]]:
        """
        Load all pending invites in the organization.

        Yields records with invite details including email, role, and expiration.
        """
        for invite in fetch_invites(api_key):
            yield invite

    @dlt.resource(
        name="users",
        write_disposition="replace",
        primary_key=["id"],
    )
    def users() -> Iterator[Dict[str, Any]]:
        """
        Load all users in the organization.

        Yields records with user details including email, name, and role.
        """
        for user in fetch_users(api_key):
            yield user

    @dlt.resource(
        name="workspace_members",
        write_disposition="replace",
        primary_key=["workspace_id", "user_id"],
    )
    def workspace_members() -> Iterator[Dict[str, Any]]:
        """
        Load workspace members for all workspaces.

        Yields records with workspace membership details.
        """
        # First get all workspaces
        for workspace in fetch_workspaces(api_key):
            workspace_id = workspace.get("id")
            if workspace_id:
                # Get members for each workspace
                for member in fetch_workspace_members(api_key, workspace_id):
                    yield member

    return [
        claude_code_usage,
        usage_report,
        cost_report,
        organization,
        workspaces,
        api_keys,
        invites,
        users,
        workspace_members,
    ]
