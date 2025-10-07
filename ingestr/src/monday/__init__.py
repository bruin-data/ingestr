"""
Monday.com source for data extraction via GraphQL API.

This source provides access to Monday.com app installation data.
"""

from typing import Any, Iterable, Iterator, Optional

import dlt
from dlt.sources import DltResource

from .helpers import MondayClient, normalize_dict


@dlt.source(max_table_nesting=0, name="monday_source")
def monday_source(
    api_token: str,
    params: list[str],
    start_date: Optional[str] = None,
    end_date: Optional[str] = None,
) -> Iterable[DltResource]:
    """
    Monday.com data source.

    Args:
        api_token: Monday.com API token for authentication
        params: Table-specific parameters in format [table_type, ...params]
        start_date: Optional start date for date-filtered queries (YYYY-MM-DD)
        end_date: Optional end date for date-filtered queries (YYYY-MM-DD)

    Yields:
        DltResource: Data resource for the requested table
    """
    monday_client = MondayClient(api_token)

    @dlt.resource(
        name="account",
        write_disposition="replace",
    )
    def fetch_account() -> Iterator[dict[str, Any]]:
        """
        Fetch account information from Monday.com.

        Table format: account (no parameters needed)
        """
        if len(params) != 0:
            raise ValueError("Account table must be in the format `account`")

        yield normalize_dict(monday_client.get_account())

    @dlt.resource(
        name="account_roles",
        write_disposition="replace",
    )
    def fetch_account_roles() -> Iterator[dict[str, Any]]:
        """
        Fetch account roles from Monday.com.

        Table format: account_roles (no parameters needed)
        """
        if len(params) != 0:
            raise ValueError(
                "Account roles table must be in the format `account_roles`"
            )

        yield from monday_client.get_account_roles()

    @dlt.resource(
        name="users",
        write_disposition="replace",
    )
    def fetch_users() -> Iterator[dict[str, Any]]:
        """
        Fetch users from Monday.com.

        Table format: users (no parameters needed)
        """
        if len(params) != 0:
            raise ValueError("Users table must be in the format `users`")

        yield from monday_client.get_users()

    @dlt.resource(
        name="boards",
        write_disposition="merge",
        primary_key="id",
    )
    def fetch_boards(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updated_at", initial_value=start_date
        ),
    ) -> Iterator[dict[str, Any]]:
        """
        Fetch boards from Monday.com.

        Table format: boards (no parameters needed)
        """
        if len(params) != 0:
            raise ValueError("Boards table must be in the format `boards`")

        yield from monday_client.get_boards()

    @dlt.resource(
        name="workspaces",
        write_disposition="replace",
    )
    def fetch_workspaces() -> Iterator[dict[str, Any]]:
        """
        Fetch workspaces from Monday.com.

        Table format: workspaces (no parameters needed)
        """
        if len(params) != 0:
            raise ValueError("Workspaces table must be in the format `workspaces`")

        yield from monday_client.get_workspaces()

    @dlt.resource(
        name="webhooks",
        write_disposition="replace",
    )
    def fetch_webhooks() -> Iterator[dict[str, Any]]:
        """
        Fetch webhooks from Monday.com.

        Table format: webhooks (no parameters needed)
        """
        if len(params) != 0:
            raise ValueError("Webhooks table must be in the format `webhooks`")

        yield from monday_client.get_webhooks()

    @dlt.resource(
        name="updates",
        write_disposition="merge",
        primary_key="id",
    )
    def fetch_updates(
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "updated_at", initial_value=start_date
        ),
    ) -> Iterator[dict[str, Any]]:
        """
        Fetch updates from Monday.com.

        Table format: updates (no parameters needed)
        Requires start_date and end_date parameters
        """
        if len(params) != 0:
            raise ValueError("Updates table must be in the format `updates`")

        yield from monday_client.get_updates(start_date=start_date, end_date=end_date)

    @dlt.resource(
        name="teams",
        write_disposition="replace",
    )
    def fetch_teams() -> Iterator[dict[str, Any]]:
        """
        Fetch teams from Monday.com.

        Table format: teams (no parameters needed)
        """
        if len(params) != 0:
            raise ValueError("Teams table must be in the format `teams`")

        yield from monday_client.get_teams()

    @dlt.resource(
        name="tags",
        write_disposition="replace",
    )
    def fetch_tags() -> Iterator[dict[str, Any]]:
        """
        Fetch tags from Monday.com.

        Table format: tags (no parameters needed)
        """
        if len(params) != 0:
            raise ValueError("Tags table must be in the format `tags`")

        yield from monday_client.get_tags()

    @dlt.resource(
        name="custom_activities",
        write_disposition="replace",
    )
    def fetch_custom_activities() -> Iterator[dict[str, Any]]:
        """
        Fetch custom activities from Monday.com.

        Table format: custom_activities (no parameters needed)
        """
        if len(params) != 0:
            raise ValueError(
                "Custom activities table must be in the format `custom_activities`"
            )

        yield from monday_client.get_custom_activities()

    @dlt.resource(
        name="board_columns",
        write_disposition="replace",
    )
    def fetch_board_columns() -> Iterator[dict[str, Any]]:
        """
        Fetch board columns from Monday.com.

        Table format: board_columns (no parameters needed)
        """
        if len(params) != 0:
            raise ValueError(
                "Board columns table must be in the format `board_columns`"
            )

        yield from monday_client.get_board_columns()

    @dlt.resource(
        name="board_views",
        write_disposition="replace",
    )
    def fetch_board_views() -> Iterator[dict[str, Any]]:
        """
        Fetch board views from Monday.com.

        Table format: board_views (no parameters needed)
        """
        if len(params) != 0:
            raise ValueError("Board views table must be in the format `board_views`")

        yield from monday_client.get_board_views()

    return (
        fetch_account,
        fetch_account_roles,
        fetch_users,
        fetch_boards,
        fetch_workspaces,
        fetch_webhooks,
        fetch_updates,
        fetch_teams,
        fetch_tags,
        fetch_custom_activities,
        fetch_board_columns,
        fetch_board_views,
    )
