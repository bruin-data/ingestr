"""
Intercom source implementation for data ingestion.

This module provides DLT sources for retrieving data from Intercom API endpoints
including contacts, companies, conversations, tickets, and more.
"""

from typing import Optional, Sequence

import dlt
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime
from dlt.sources import DltResource, DltSource

from .helpers import (
    IntercomAPIClient,
    IntercomCredentialsAccessToken,
    TIntercomCredentials,
    convert_datetime_to_timestamp,
    create_resource_from_config,
    transform_company,
    transform_contact,
    transform_conversation,
)
from .helpers import (
    IntercomCredentialsOAuth as IntercomCredentialsOAuth,
)
from .settings import (
    DEFAULT_START_DATE,
    RESOURCE_CONFIGS,
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

    # Convert dates to pendulum and then to unix timestamps for Intercom API
    start_date_obj = ensure_pendulum_datetime(start_date) if start_date else None
    end_date_obj = ensure_pendulum_datetime(end_date) if end_date else None

    # Convert to unix timestamps for API compatibility
    # Use default start date if none provided
    if not start_date_obj:
        from .settings import DEFAULT_START_DATE

        start_date_obj = ensure_pendulum_datetime(DEFAULT_START_DATE)

    start_timestamp = convert_datetime_to_timestamp(start_date_obj)
    end_timestamp = (
        convert_datetime_to_timestamp(end_date_obj) if end_date_obj else None
    )

    # Transform function mapping
    transform_functions = {
        "transform_contact": transform_contact,
        "transform_company": transform_company,
        "transform_conversation": transform_conversation,
    }

    # Generate all resources from configuration
    resources = []
    for resource_name, config in RESOURCE_CONFIGS.items():
        resource_func = create_resource_from_config(
            resource_name,
            config,
            api_client,
            start_timestamp,
            end_timestamp,
            transform_functions,
        )

        # Call the resource function to get the actual resource
        resources.append(resource_func())

    return resources


def intercom(
    api_key: str,
    region: str = "us",
    start_date: Optional[TAnyDateTime] = DEFAULT_START_DATE,
    end_date: Optional[TAnyDateTime] = None,
) -> DltSource:
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
    credentials = IntercomCredentialsAccessToken(access_token=api_key, region=region)

    return intercom_source(
        credentials=credentials,
        start_date=start_date,
        end_date=end_date,
    )
