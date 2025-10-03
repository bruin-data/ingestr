"""
This source provides data extraction from PlusVibeAI via the REST API.

It defines functions to fetch data from different parts of PlusVibeAI including
campaigns and other marketing analytics data.
"""

from typing import Any, Iterable, Optional

import dlt
from dlt.common.typing import TDataItem

from .helpers import get_client
from .settings import DEFAULT_PAGE_SIZE, DEFAULT_START_DATE


@dlt.source
def plusvibeai_source() -> Any:
    """
    The main function that runs all the other functions to fetch data from PlusVibeAI.

    Returns:
        Sequence[DltResource]: A sequence of DltResource objects containing the fetched data.
    """
    return [
        campaigns,
        leads,
    ]


@dlt.resource(
    write_disposition="merge",
    primary_key="id",
    max_table_nesting=0,  # Keep nested objects (schedule, sequences) as JSON columns
)
def campaigns(
    api_key: str = dlt.secrets.value,
    workspace_id: str = dlt.secrets.value,
    base_url: str = "https://api.plusvibe.ai",
    max_results: Optional[int] = None,
    updated: dlt.sources.incremental[str] = dlt.sources.incremental(
        "modified_at",  # PlusVibeAI uses modified_at for updates
        initial_value=DEFAULT_START_DATE,
        range_end="closed",
        range_start="closed",
    ),
) -> Iterable[TDataItem]:
    """
    Fetches campaigns from PlusVibeAI.

    Args:
        api_key (str): API key for authentication (get from https://app.plusvibe.ai/v2/settings/api-access/)
        workspace_id (str): Workspace ID to access
        base_url (str): PlusVibeAI API base URL
        max_results (int): Maximum number of results to return
        updated (str): The date from which to fetch updated campaigns

    Yields:
        dict: The campaign data with nested objects (schedule, sequences, etc.) as JSON.
    """
    client = get_client(api_key, workspace_id, base_url)

    for campaign in client.get_campaigns(
        page_size=DEFAULT_PAGE_SIZE, max_results=max_results
    ):
        # Apply incremental filter if needed
        if updated.start_value:
            campaign_updated = campaign.get("modified_at")
            if campaign_updated and campaign_updated < updated.start_value:
                continue

        yield campaign


@dlt.resource(
    write_disposition="merge",
    primary_key="_id",
    max_table_nesting=0,
)
def leads(
    api_key: str = dlt.secrets.value,
    workspace_id: str = dlt.secrets.value,
    base_url: str = "https://api.plusvibe.ai",
    max_results: Optional[int] = None,
    updated: dlt.sources.incremental[str] = dlt.sources.incremental(
        "modified_at",
        initial_value=DEFAULT_START_DATE,
        range_end="closed",
        range_start="closed",
    ),
) -> Iterable[TDataItem]:
    """
    Fetches leads from PlusVibeAI.

    Args:
        api_key (str): API key for authentication
        workspace_id (str): Workspace ID to access
        base_url (str): PlusVibeAI API base URL
        max_results (int): Maximum number of results to return
        updated (str): The date from which to fetch updated leads

    Yields:
        dict: The lead data.
    """
    client = get_client(api_key, workspace_id, base_url)

    for lead in client.get_leads(page_size=DEFAULT_PAGE_SIZE, max_results=max_results):
        # Apply incremental filter if needed
        if updated.start_value:
            lead_updated = lead.get("modified_at")
            if lead_updated and lead_updated < updated.start_value:
                continue

        yield lead
