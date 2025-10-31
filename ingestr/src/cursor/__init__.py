"""
This source provides data extraction from Cursor via the REST API.

It fetches team member information from the Cursor API.
"""

from typing import Any, Iterable, Optional

import dlt
from dlt.common.typing import TDataItem

from .helpers import CursorClient


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
    client = CursorClient(api_key=api_key)

    members = client.get_team_members()
    yield from members


@dlt.resource(
    write_disposition="replace",
    max_table_nesting=0,
)
def daily_usage_data(
    api_key: str = dlt.secrets.value,
    start_date: Optional[int] = dlt.config.value,
    end_date: Optional[int] = dlt.config.value,
) -> Iterable[TDataItem]:
    client = CursorClient(api_key=api_key)

    yield from client.get_daily_usage_data(start_date=start_date, end_date=end_date)


@dlt.resource(
    write_disposition="replace",
    max_table_nesting=0,
)
def team_spend(
    api_key: str = dlt.secrets.value,
) -> Iterable[TDataItem]:
    client = CursorClient(api_key=api_key)

    yield from client.get_team_spend()


@dlt.resource(
    write_disposition="replace",
    max_table_nesting=0,
)
def filtered_usage_events(
    api_key: str = dlt.secrets.value,
    start_date: Optional[int] = dlt.config.value,
    end_date: Optional[int] = dlt.config.value,
) -> Iterable[TDataItem]:
    client = CursorClient(api_key=api_key)

    yield from client.get_filtered_usage_events(
        start_date=start_date, end_date=end_date
    )
