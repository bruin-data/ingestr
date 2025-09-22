"""
Intercom source implementation for data ingestion.

This module provides DLT sources for retrieving data from Intercom API endpoints
including contacts, companies, conversations, tickets, and more.
"""
from typing import Any, Dict, Iterator, List, Optional, Sequence

import dlt
from dlt.common import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem, TDataItems
from dlt.sources import DltResource

from .helpers import (
    IntercomAPIClient,
    IntercomCredentialsAccessToken,
    IntercomCredentialsOAuth,
    PaginationType,
    TIntercomCredentials,
    build_incremental_query,
    transform_company,
    transform_contact,
    transform_conversation,
)
from .settings import (
    CORE_ENDPOINTS,
    DEFAULT_START_DATE,
)


@dlt.source(name="intercom", max_table_nesting=0)
def intercom_source(
    credentials: TIntercomCredentials = dlt.secrets.value,
    start_date: Optional[TAnyDateTime] = DEFAULT_START_DATE,
    end_date: Optional[TAnyDateTime] = None,
) -> Sequence[DltResource]:
    """
    A DLT source that retrieves data from Intercom API.

    This source provides access to various Intercom resources including contacts,
    companies, conversations, tickets, and more. It supports incremental loading
    for resources that track updated timestamps.

    Args:
        credentials: Intercom API credentials (AccessToken or OAuth).
            Defaults to dlt.secrets.value.
        start_date: The start date for incremental loading.
            Defaults to January 1, 2020.
        end_date: Optional end date for incremental loading.
            If not provided, loads all data from start_date to present.

    Returns:
        Sequence of DLT resources for different Intercom endpoints.

    Example:
        >>> source = intercom_source(
        ...     credentials=IntercomCredentialsAccessToken(
        ...         access_token="your_token",
        ...         region="us"
        ...     ),
        ...     start_date=datetime(2024, 1, 1)
        ... )
    """
    # Initialize API client
    api_client = IntercomAPIClient(credentials)
    
    # Convert dates to pendulum
    start_date_obj = ensure_pendulum_datetime(start_date)
    end_date_obj = ensure_pendulum_datetime(end_date) if end_date else None
    
    # Contacts resource (incremental)
    @dlt.resource(
        name="contacts",
        primary_key="id",
        write_disposition="merge",
        columns={
            "custom_attributes": {"data_type": "json"},
            "tags": {"data_type": "json"},
        }
    )
    def contacts_resource(
        updated_at: dlt.sources.incremental[pendulum.DateTime] = dlt.sources.incremental(
            "updated_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            allow_external_schedulers=True,
        )
    ) -> Iterator[TDataItems]:
        """
        Load contacts (users and leads) from Intercom.
        
        Yields:
            Transformed contact records.
        """
        # Build search query for incremental loading
        query = build_incremental_query(
            "updated_at",
            updated_at.last_value.int_timestamp,
            updated_at.end_value.int_timestamp if updated_at.end_value else None,
        )
        
        # Use search API for incremental loading
        for page in api_client.search("contacts", query):
            transformed_contacts = [transform_contact(c) for c in page]
            yield transformed_contacts
            
            # Stop if we've reached the end value
            if updated_at.end_out_of_range:
                return
    
    # Companies resource (incremental)
    @dlt.resource(
        name="companies",
        primary_key="id",
        write_disposition="merge",
        columns={
            "custom_attributes": {"data_type": "json"},
            "tags": {"data_type": "json"},
        }
    )
    def companies_resource(
        updated_at: dlt.sources.incremental[pendulum.DateTime] = dlt.sources.incremental(
            "updated_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            allow_external_schedulers=True,
        )
    ) -> Iterator[TDataItems]:
        """
        Load companies from Intercom.
        
        Yields:
            Transformed company records.
        """
        # Build search query for incremental loading
        query = build_incremental_query(
            "updated_at",
            updated_at.last_value.int_timestamp,
            updated_at.end_value.int_timestamp if updated_at.end_value else None,
        )
        
        # Use search API for incremental loading
        for page in api_client.search("companies", query):
            transformed_companies = [transform_company(c) for c in page]
            yield transformed_companies
            
            if updated_at.end_out_of_range:
                return
    
    # Conversations resource (incremental)
    @dlt.resource(
        name="conversations",
        primary_key="id",
        write_disposition="merge",
        columns={
            "custom_attributes": {"data_type": "json"},
            "tags": {"data_type": "json"},
        }
    )
    def conversations_resource(
        updated_at: dlt.sources.incremental[pendulum.DateTime] = dlt.sources.incremental(
            "updated_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            allow_external_schedulers=True,
        )
    ) -> Iterator[TDataItems]:
        """
        Load conversations from Intercom.
        
        Yields:
            Transformed conversation records.
        """
        # Build search query for incremental loading
        query = build_incremental_query(
            "updated_at",
            updated_at.last_value.int_timestamp,
            updated_at.end_value.int_timestamp if updated_at.end_value else None,
        )
        
        # Use search API for incremental loading
        for page in api_client.search("conversations", query):
            transformed_conversations = [transform_conversation(c) for c in page]
            yield transformed_conversations
            
            if updated_at.end_out_of_range:
                return
    
    # Tickets resource (incremental)
    @dlt.resource(
        name="tickets",
        primary_key="id",
        write_disposition="merge",
        columns={
            "ticket_attributes": {"data_type": "json"},
        }
    )
    def tickets_resource(
        updated_at: dlt.sources.incremental[pendulum.DateTime] = dlt.sources.incremental(
            "updated_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            allow_external_schedulers=True,
        )
    ) -> Iterator[TDataItems]:
        """
        Load tickets from Intercom.
        
        Yields:
            Ticket records.
        """
        # Tickets use cursor pagination, not search
        params = {
            "updated_since": updated_at.last_value.int_timestamp
        }
        
        if updated_at.end_value:
            # Note: Tickets API doesn't support updated_until, so we filter client-side
            end_timestamp = updated_at.end_value.int_timestamp
        else:
            end_timestamp = None
        
        for page in api_client.get_pages(
            "/tickets",
            "tickets",
            PaginationType.CURSOR,
            params=params
        ):
            if end_timestamp:
                # Filter tickets that are beyond end_value
                filtered_tickets = [
                    t for t in page
                    if t.get("updated_at", 0) <= end_timestamp
                ]
                if filtered_tickets:
                    yield filtered_tickets
                
                # Check if any ticket was beyond end_value
                if any(t.get("updated_at", 0) > end_timestamp for t in page):
                    return
            else:
                yield page
    
    # Tags resource (replace mode, not incremental)
    @dlt.resource(
        name="tags",
        primary_key="id",
        write_disposition="replace",
    )
    def tags_resource() -> Iterator[TDataItems]:
        """
        Load all tags from Intercom.
        
        Yields:
            Tag records.
        """
        yield from api_client.get_pages(
            "/tags",
            "data",
            PaginationType.SIMPLE,
        )
    
    # Segments resource (replace mode, not incremental)
    @dlt.resource(
        name="segments",
        primary_key="id",
        write_disposition="replace",
    )
    def segments_resource() -> Iterator[TDataItems]:
        """
        Load all segments from Intercom.
        
        Yields:
            Segment records.
        """
        yield from api_client.get_pages(
            "/segments",
            "segments",
            PaginationType.CURSOR,
        )
    
    # Teams resource (replace mode, not incremental)
    @dlt.resource(
        name="teams",
        primary_key="id",
        write_disposition="replace",
    )
    def teams_resource() -> Iterator[TDataItems]:
        """
        Load all teams from Intercom.
        
        Yields:
            Team records.
        """
        yield from api_client.get_pages(
            "/teams",
            "teams",
            PaginationType.SIMPLE,
        )
    
    # Admins resource (replace mode, not incremental)
    @dlt.resource(
        name="admins",
        primary_key="id",
        write_disposition="replace",
    )
    def admins_resource() -> Iterator[TDataItems]:
        """
        Load all admins from Intercom.
        
        Yields:
            Admin records.
        """
        yield from api_client.get_pages(
            "/admins",
            "admins",
            PaginationType.SIMPLE,
        )
    
    # Articles resource (incremental)
    @dlt.resource(
        name="articles",
        primary_key="id",
        write_disposition="merge",
    )
    def articles_resource(
        updated_at: dlt.sources.incremental[pendulum.DateTime] = dlt.sources.incremental(
            "updated_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            allow_external_schedulers=True,
        )
    ) -> Iterator[TDataItems]:
        """
        Load help center articles from Intercom.
        
        Yields:
            Article records.
        """
        # Articles use cursor pagination
        # Note: Articles API doesn't have direct date filtering, 
        # so we need to fetch all and filter client-side
        for page in api_client.get_pages(
            "/articles",
            "data",
            PaginationType.CURSOR,
        ):
            # Filter by updated_at
            filtered_articles = []
            for article in page:
                article_updated = article.get("updated_at", 0)
                if article_updated >= updated_at.last_value.int_timestamp:
                    if updated_at.end_value and article_updated > updated_at.end_value.int_timestamp:
                        continue
                    filtered_articles.append(article)
            
            if filtered_articles:
                yield filtered_articles
            
            # Check if we should stop
            if updated_at.end_out_of_range:
                return
    
    # Data attributes resource (for custom field definitions)
    @dlt.resource(
        name="data_attributes",
        primary_key="id",
        write_disposition="replace",
    )
    def data_attributes_resource() -> Iterator[TDataItems]:
        """
        Load custom data attribute definitions from Intercom.
        
        Yields:
            Data attribute records.
        """
        yield from api_client.get_pages(
            "/data_attributes",
            "data",
            PaginationType.CURSOR,
        )
    
    # Return all resources
    return [
        contacts_resource(),
        companies_resource(),
        conversations_resource(),
        tickets_resource(),
        tags_resource(),
        segments_resource(),
        teams_resource(),
        admins_resource(),
        articles_resource(),
        data_attributes_resource(),
    ]


def intercom(
    api_key: str,
    region: str = "us",
    start_date: Optional[TAnyDateTime] = DEFAULT_START_DATE,
    end_date: Optional[TAnyDateTime] = None,
) -> Sequence[DltResource]:
    """
    Convenience function to create Intercom source with access token.

    Args:
        api_key: Intercom API access token.
        region: Data region (us, eu, or au). Defaults to "us".
        start_date: Start date for incremental loading.
        end_date: Optional end date for incremental loading.

    Returns:
        Sequence of DLT resources.

    Example:
        >>> source = intercom(
        ...     api_key="your_access_token",
        ...     region="us",
        ...     start_date=datetime(2024, 1, 1)
        ... )
    """
    credentials = IntercomCredentialsAccessToken(
        access_token=api_key,
        region=region
    )
    
    return intercom_source(
        credentials=credentials,
        start_date=start_date,
        end_date=end_date,
    )