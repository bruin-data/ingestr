"""
Monday.com source for data extraction via GraphQL API.

This source provides access to Monday.com app installation data.
"""

from typing import Any, Iterator, Optional

import dlt
from dlt.sources import DltResource

from .helpers import MondayClient, normalize_dict


@dlt.source(max_table_nesting=0, name="monday_source")
def monday_source(
    api_token: str,
    params: list[str],
) -> Iterator[DltResource]:
    """
    Monday.com data source.

    Args:
        api_token: Monday.com API token for authentication
        params: Table-specific parameters in format [table_type, ...params]

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
            raise ValueError("Account roles table must be in the format `account_roles`")

        yield from monday_client.get_account_roles()

    return (fetch_account, fetch_account_roles)
