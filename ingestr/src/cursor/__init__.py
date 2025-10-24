"""
This source provides data extraction from Cursor via the REST API.

It fetches team member information from the Cursor API.
"""

from typing import Any, Iterable, Optional

import dlt
from dlt.common.typing import TDataItem

from .helpers import get_client


@dlt.source
def cursor_source() -> Any:
    """
    The main function that fetches data from Cursor API.

    Returns:
        Sequence[DltResource]: A sequence of DltResource objects containing the fetched data.
    """
    return [
        team_members,
        daily_usage_data,
        team_spend,
        filtered_usage_events,
    ]


@dlt.resource(
    write_disposition="replace",
    max_table_nesting=0,
)
def team_members(
    api_key: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches team members from Cursor API.

    Args:
        api_key (str): API key for authentication

    Yields:
        dict: The team member data.
    """
    client = get_client(api_key)

    members = client.get_team_members()

    for member in members:
        yield member


@dlt.resource(
    write_disposition="replace",
    max_table_nesting=0,
)
def daily_usage_data(
    api_key: str = dlt.secrets.value,
    start_date: Optional[int] = dlt.config.value,
    end_date: Optional[int] = dlt.config.value,
) -> Iterable[TDataItem]:
    """
    Fetches daily usage data from Cursor API.

    Args:
        api_key (str): API key for authentication
        start_date (int): Start date in epoch milliseconds (from interval_start, optional)
        end_date (int): End date in epoch milliseconds (from interval_end, optional)

    Yields:
        dict: The daily usage data.

    Note:
        Date range cannot exceed 30 days when specified.
    """
    client = get_client(api_key)

    for record in client.get_daily_usage_data(start_date=start_date, end_date=end_date):
        yield record


@dlt.resource(
    write_disposition="replace",
    max_table_nesting=0,
)
def team_spend(
    api_key: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    """
    Fetches team spending data from Cursor API.

    Args:
        api_key (str): API key for authentication

    Yields:
        dict: The team spending data.
    """
    client = get_client(api_key)

    for record in client.get_team_spend():
        yield record


@dlt.resource(
    write_disposition="replace",
    max_table_nesting=0,
)
def filtered_usage_events(
    api_key: str = dlt.secrets.value,
    start_date: Optional[int] = dlt.config.value,
    end_date: Optional[int] = dlt.config.value,
) -> Iterable[TDataItem]:
    """
    Fetches filtered usage events from Cursor API.

    Args:
        api_key (str): API key for authentication
        start_date (int): Start date in epoch milliseconds (from interval_start, optional)
        end_date (int): End date in epoch milliseconds (from interval_end, optional)

    Yields:
        dict: The usage event data.
    """
    client = get_client(api_key)

    for record in client.get_filtered_usage_events(start_date=start_date, end_date=end_date):
        yield record
