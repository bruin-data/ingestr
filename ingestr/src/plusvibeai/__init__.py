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
        email_accounts,
        emails,
        blocklist,
        webhooks,
        tags,
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


@dlt.resource(
    write_disposition="merge",
    primary_key="_id",
    max_table_nesting=0,
)
def email_accounts(
    api_key: str = dlt.secrets.value,
    workspace_id: str = dlt.secrets.value,
    base_url: str = "https://api.plusvibe.ai",
    max_results: Optional[int] = None,
    updated: dlt.sources.incremental[str] = dlt.sources.incremental(
        "timestamp_updated",
        initial_value=DEFAULT_START_DATE,
        range_end="closed",
        range_start="closed",
    ),
) -> Iterable[TDataItem]:
    """
    Fetches email accounts from PlusVibeAI.

    Args:
        api_key (str): API key for authentication
        workspace_id (str): Workspace ID to access
        base_url (str): PlusVibeAI API base URL
        max_results (int): Maximum number of results to return
        updated (str): The date from which to fetch updated email accounts

    Yields:
        dict: The email account data.
    """
    client = get_client(api_key, workspace_id, base_url)

    for account in client.get_email_accounts(
        page_size=DEFAULT_PAGE_SIZE, max_results=max_results
    ):
        # Apply incremental filter if needed
        if updated.start_value:
            account_updated = account.get("timestamp_updated")
            if account_updated and account_updated < updated.start_value:
                continue

        yield account


@dlt.resource(
    write_disposition="merge",
    primary_key="id",
    max_table_nesting=0,
)
def emails(
    api_key: str = dlt.secrets.value,
    workspace_id: str = dlt.secrets.value,
    base_url: str = "https://api.plusvibe.ai",
    max_results: Optional[int] = None,
    updated: dlt.sources.incremental[str] = dlt.sources.incremental(
        "timestamp_created",
        initial_value=DEFAULT_START_DATE,
        range_end="closed",
        range_start="closed",
    ),
) -> Iterable[TDataItem]:
    """
    Fetches emails from PlusVibeAI.

    Args:
        api_key (str): API key for authentication
        workspace_id (str): Workspace ID to access
        base_url (str): PlusVibeAI API base URL
        max_results (int): Maximum number of results to return
        updated (str): The date from which to fetch emails

    Yields:
        dict: The email data.
    """
    client = get_client(api_key, workspace_id, base_url)

    for email in client.get_emails(max_results=max_results):
        # Apply incremental filter if needed
        if updated.start_value:
            email_created = email.get("timestamp_created")
            if email_created and email_created < updated.start_value:
                continue

        yield email


@dlt.resource(
    write_disposition="merge",
    primary_key="_id",
    max_table_nesting=0,
)
def blocklist(
    api_key: str = dlt.secrets.value,
    workspace_id: str = dlt.secrets.value,
    base_url: str = "https://api.plusvibe.ai",
    max_results: Optional[int] = None,
    updated: dlt.sources.incremental[str] = dlt.sources.incremental(
        "created_at",
        initial_value=DEFAULT_START_DATE,
        range_end="closed",
        range_start="closed",
    ),
) -> Iterable[TDataItem]:
    """
    Fetches blocklist entries from PlusVibeAI.

    Args:
        api_key (str): API key for authentication
        workspace_id (str): Workspace ID to access
        base_url (str): PlusVibeAI API base URL
        max_results (int): Maximum number of results to return
        updated (str): The date from which to fetch blocklist entries

    Yields:
        dict: The blocklist entry data.
    """
    client = get_client(api_key, workspace_id, base_url)

    for entry in client.get_blocklist(
        page_size=DEFAULT_PAGE_SIZE, max_results=max_results
    ):
        # Apply incremental filter if needed
        if updated.start_value:
            entry_created = entry.get("created_at")
            if entry_created and entry_created < updated.start_value:
                continue

        yield entry


@dlt.resource(
    write_disposition="merge",
    primary_key="_id",
    max_table_nesting=0,
)
def webhooks(
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
    Fetches webhooks from PlusVibeAI.

    Args:
        api_key (str): API key for authentication
        workspace_id (str): Workspace ID to access
        base_url (str): PlusVibeAI API base URL
        max_results (int): Maximum number of results to return
        updated (str): The date from which to fetch updated webhooks

    Yields:
        dict: The webhook data.
    """
    client = get_client(api_key, workspace_id, base_url)

    for webhook in client.get_webhooks(
        page_size=DEFAULT_PAGE_SIZE, max_results=max_results
    ):
        # Apply incremental filter if needed
        if updated.start_value:
            webhook_updated = webhook.get("modified_at")
            if webhook_updated and webhook_updated < updated.start_value:
                continue

        yield webhook


@dlt.resource(
    write_disposition="merge",
    primary_key="_id",
    max_table_nesting=0,
)
def tags(
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
    Fetches tags from PlusVibeAI.

    Args:
        api_key (str): API key for authentication
        workspace_id (str): Workspace ID to access
        base_url (str): PlusVibeAI API base URL
        max_results (int): Maximum number of results to return
        updated (str): The date from which to fetch updated tags

    Yields:
        dict: The tag data.
    """
    client = get_client(api_key, workspace_id, base_url)

    for tag in client.get_tags(page_size=DEFAULT_PAGE_SIZE, max_results=max_results):
        # Apply incremental filter if needed
        if updated.start_value:
            tag_updated = tag.get("modified_at")
            if tag_updated and tag_updated < updated.start_value:
                continue

        yield tag
