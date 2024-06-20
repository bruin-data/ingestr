"""Fetches Gorgias data."""

from typing import Iterable

import dlt
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource

from .helpers import GorgiasApi


@dlt.source(name="gorgias", max_table_nesting=0)
def gorgias_source(
    domain: str = dlt.secrets.value,
    email: str = dlt.config.value,
    api_key: str = dlt.secrets.value,
    start_date: TAnyDateTime = "2024-06-15",
    end_date: TAnyDateTime = None,
) -> Iterable[DltResource]:
    """
    The source for the Gorgias pipeline. Available resources include tickets, users, and conversations.

    Args:
        domain: The domain of your Gorgias account.
        email: The email associated with your Gorgias account.
        api_key: The API key for accessing the Gorgias API.
        items_per_page: The max number of items to fetch per page. Defaults to 100.

    Returns:
        Iterable[DltResource]: A list of DltResource objects representing the data resources.
    """

    client = GorgiasApi(domain, email, api_key)

    start_date_obj = ensure_pendulum_datetime(start_date)
    end_date_obj = ensure_pendulum_datetime(end_date) if end_date else None

    @dlt.resource(primary_key="id", write_disposition="merge")
    def customers(
        updated_datetime=dlt.sources.incremental("updated_datetime", start_date_obj),
    ) -> Iterable[TDataItem]:
        """
        The resource for customers on your Gorgias domain, supports incremental loading and pagination.

        Args:
            updated_datetime: The saved state of the last 'updated_datetime' value.

        Returns:
            Iterable[TDataItem]: A generator of products.
        """
        yield from client.get_pages(
            "customers",
            params={},
            start_date=updated_datetime.start_value,
            end_date=end_date_obj,
        )

    @dlt.resource(
        primary_key="id",
        write_disposition="merge",
        columns={
            "id": {
                "data_type": "bigint",
                "nullable": False,
                "primary_key": True,
                "description": "Primary identifier for the ticket",
            },
            "uri": {
                "data_type": "text",
                "nullable": False,
                "description": "API endpoint for the ticket",
            },
            "external_id": {
                "data_type": "text",
                "nullable": True,
                "description": "External identifier for the ticket",
            },
            "language": {
                "data_type": "text",
                "nullable": True,
                "description": "Language of the ticket",
            },
            "status": {
                "data_type": "text",
                "nullable": False,
                "description": "Status of the ticket",
            },
            "priority": {
                "data_type": "text",
                "nullable": False,
                "description": "Priority level of the ticket",
            },
            "channel": {
                "data_type": "text",
                "precision": 20,
                "nullable": False,
                "description": "The channel used to initiate the conversation with the customer.",
            },
            "via": {
                "data_type": "text",
                "nullable": False,
                "description": "Method used to create the ticket",
            },
            "from_agent": {
                "data_type": "bool",
                "nullable": False,
                "description": "Indicates if the ticket was created by an agent",
            },
            "customer": {
                "data_type": "complex",
                "nullable": False,
                "description": "The customer linked to the ticket.",
            },
            "assignee_user": {
                "data_type": "complex",
                "nullable": True,
                "description": "User assigned to the ticket",
            },
            "assignee_team": {
                "data_type": "complex",
                "nullable": True,
                "description": "Team assigned to the ticket",
            },
            "subject": {
                "data_type": "text",
                "nullable": True,
                "description": "Subject of the ticket",
            },
            "excerpt": {
                "data_type": "text",
                "nullable": True,
                "description": "Excerpt of the ticket",
            },
            "integrations": {
                "data_type": "complex",
                "nullable": False,
                "description": "Integration information related to the ticket",
            },
            "meta": {
                "data_type": "complex",
                "nullable": True,
                "description": "Meta information related to the ticket",
            },
            "tags": {
                "data_type": "complex",
                "nullable": False,
                "description": "Tags associated with the ticket",
            },
            "messages_count": {
                "data_type": "bigint",
                "nullable": False,
                "description": "Count of messages in the ticket",
            },
            "is_unread": {
                "data_type": "bool",
                "nullable": False,
                "description": "Indicates if the ticket is unread",
            },
            "spam": {
                "data_type": "bool",
                "nullable": False,
                "description": "Indicates if the ticket is marked as spam",
            },
            "created_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": False,
                "description": "Creation timestamp of the ticket",
            },
            "opened_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "Opened timestamp of the ticket",
            },
            "last_received_message_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "Timestamp of the last received message",
            },
            "last_message_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "Timestamp of the last message",
            },
            "updated_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": False,
                "description": "Last updated timestamp of the ticket",
            },
            "closed_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "Closed timestamp of the ticket",
            },
            "snooze_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "Snooze timestamp of the ticket",
            },
            "trashed_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "Trashed timestamp of the ticket",
            },
        },
    )
    def tickets(
        updated_datetime=dlt.sources.incremental("updated_datetime", start_date_obj),
    ) -> Iterable[TDataItem]:
        """
        The resource for tickets on your Gorgias domain, supports incremental loading and pagination.

        Args:
            updated_datetime: The saved state of the last 'updated_datetime' value.

        Returns:
            Iterable[TDataItem]: A generator of products.
        """
        yield from client.get_pages(
            "tickets",
            params={},
            start_date=updated_datetime.start_value,
            end_date=end_date_obj,
        )

    @dlt.resource(primary_key="id", write_disposition="merge")
    def ticket_messages(
        updated_datetime=dlt.sources.incremental("updated_datetime", start_date_obj),
    ) -> Iterable[TDataItem]:
        """
        The resource for ticket messages on your Gorgias domain, supports incremental loading and pagination.

        Args:
            updated_datetime: The saved state of the last 'updated_datetime' value.

        Returns:
            Iterable[TDataItem]: A generator of products.
        """
        yield from client.get_pages(
            "messages",
            params={"order_by": "created_datetime:desc"},
            start_date=updated_datetime.start_value,
            end_date=end_date_obj,
        )

    @dlt.resource(primary_key="id", write_disposition="merge")
    def satisfaction_surveys(
        updated_datetime=dlt.sources.incremental("updated_datetime", start_date_obj),
    ) -> Iterable[TDataItem]:
        """
        The resource for satisfaction surveys on your Gorgias domain, supports incremental loading and pagination.

        Args:
            updated_datetime: The saved state of the last 'updated_datetime' value.

        Returns:
            Iterable[TDataItem]: A generator of products.
        """
        yield from client.get_pages(
            "satisfaction-surveys",
            params={"order_by": "created_datetime:desc"},
            start_date=updated_datetime.start_value,
            end_date=end_date_obj,
        )

    return (customers, tickets, ticket_messages, satisfaction_surveys)
