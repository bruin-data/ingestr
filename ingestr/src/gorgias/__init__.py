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
    start_date: TAnyDateTime = "2000-01-01",
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

    @dlt.resource(
        primary_key="id",
        write_disposition="merge",
        columns={
            "id": {
                "data_type": "bigint",
                "nullable": False,
                "primary_key": True,
                "description": "ID of the user.",
            },
            "external_id": {
                "data_type": "text",
                "nullable": True,
                "description": "External ID of the user in a foreign system.",
            },
            "active": {
                "data_type": "bool",
                "nullable": False,
                "description": "Indicates if the user is active.",
            },
            "email": {
                "data_type": "text",
                "nullable": True,
                "description": "Email address of the user.",
            },
            "name": {
                "data_type": "text",
                "nullable": True,
                "description": "Full name of the user.",
            },
            "firstname": {
                "data_type": "text",
                "nullable": False,
                "description": "First name of the user.",
            },
            "lastname": {
                "data_type": "text",
                "nullable": False,
                "description": "Last name of the user.",
            },
            "language": {
                "data_type": "text",
                "nullable": True,
                "description": "Preferred language of the user.",
            },
            "timezone": {
                "data_type": "text",
                "nullable": True,
                "description": "Time zone of the user.",
            },
            "created_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": False,
                "description": "When the user was created.",
            },
            "updated_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": False,
                "description": "When the user was last updated.",
            },
            "meta": {
                "data_type": "json",
                "nullable": True,
                "description": "Meta information associated with the user.",
            },
            "data": {
                "data_type": "json",
                "nullable": True,
                "description": "Additional data associated with the user.",
            },
            "note": {
                "data_type": "text",
                "nullable": True,
                "description": "Notes about the user.",
            },
        },
    )
    def customers(
        updated_datetime=dlt.sources.incremental(
            "updated_datetime", start_date_obj, range_end="closed", range_start="closed"
        ),
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
                "nullable": True,
                "description": "Indicates if the ticket was created by an agent",
            },
            "customer": {
                "data_type": "json",
                "nullable": False,
                "description": "The customer linked to the ticket.",
            },
            "assignee_user": {
                "data_type": "json",
                "nullable": True,
                "description": "User assigned to the ticket",
            },
            "assignee_team": {
                "data_type": "json",
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
                "data_type": "json",
                "nullable": False,
                "description": "Integration information related to the ticket",
            },
            "meta": {
                "data_type": "json",
                "nullable": True,
                "description": "Meta information related to the ticket",
            },
            "tags": {
                "data_type": "json",
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
        updated_datetime=dlt.sources.incremental(
            "updated_datetime", start_date_obj, range_end="closed", range_start="closed"
        ),
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
            "message_id": {
                "data_type": "text",
                "nullable": True,
                "description": "ID of the message on the service that send the message.It can be the ID of an email, a Messenger message, a Facebook comment, etc...",
            },
            "ticket_id": {
                "data_type": "bigint",
                "nullable": True,
                "description": "The ID of the ticket the message is associated with.",
            },
            "external_id": {
                "data_type": "text",
                "nullable": True,
                "description": "External identifier for the ticket",
            },
            "public": {
                "data_type": "bool",
                "nullable": False,
                "description": "Public flag",
            },
            "channel": {
                "data_type": "text",
                "nullable": False,
                "description": "The channel used to initiate the conversation with the customer.",
            },
            "via": {
                "data_type": "text",
                "nullable": False,
                "description": "How the message has been received, or sent from Gorgias.",
            },
            "sender": {
                "data_type": "json",
                "nullable": False,
                "description": "The person who sent the message. It can be a user or a customer.",
            },
            "integration_id": {
                "data_type": "bigint",
                "nullable": True,
                "description": "ID of the integration that either received or sent the message.",
            },
            "intents": {
                "data_type": "json",
                "nullable": True,
                "description": "",
            },
            "rule_id": {
                "data_type": "bigint",
                "nullable": True,
                "description": "ID of the rule which sent the message, if any.",
            },
            "from_agent": {
                "data_type": "bool",
                "nullable": True,
                "description": "Whether the message was sent by your company to a customer, or the opposite.",
            },
            "receiver": {
                "data_type": "json",
                "nullable": True,
                "description": "The primary receiver of the message. It can be a user or a customer. Optional when the source type is 'internal-note'.",
            },
            "subject": {
                "data_type": "text",
                "nullable": True,
                "description": "The subject of the message.",
            },
            "body_text": {
                "data_type": "text",
                "nullable": True,
                "description": "The full text version of the body of the message, if any.",
            },
            "body_html": {
                "data_type": "text",
                "nullable": True,
                "description": "The full HTML version of the body of the message, if any.",
            },
            "stripped_text": {
                "data_type": "text",
                "nullable": True,
                "description": "The text version of the body of the message without email signatures and previous replies.",
            },
            "stripped_html": {
                "data_type": "text",
                "nullable": True,
                "description": "The HTML version of the body of the message without email signatures and previous replies.",
            },
            "stripped_signature": {
                "data_type": "text",
                "nullable": True,
                "description": "",
            },
            "headers": {
                "data_type": "json",
                "nullable": True,
                "description": "Headers of the message",
            },
            "attachments": {
                "data_type": "json",
                "nullable": True,
                "description": "A list of files attached to the message.",
            },
            "actions": {
                "data_type": "json",
                "nullable": True,
                "description": "A list of actions performed on the message.",
            },
            "macros": {
                "data_type": "json",
                "nullable": True,
                "description": "A list of macros",
            },
            "meta": {
                "data_type": "json",
                "nullable": True,
                "description": "Message metadata",
            },
            "created_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": False,
                "description": "When the message was created.",
            },
            "sent_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "When the message was sent. If ommited, the message will be sent by Gorgias.",
            },
            "failed_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "When the message failed to be sent. Messages that couldn't be sent can be resend.",
            },
            "deleted_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "When the message was deleted.",
            },
            "opened_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "When the message was opened by the receiver.",
            },
            "last_sending_error": {
                "data_type": "text",
                "nullable": True,
                "description": "Details of the last error encountered when Gorgias attempted to send the message.",
            },
            "is_retriable": {
                "data_type": "bool",
                "nullable": True,
                "description": "Can be retried",
            },
        },
    )
    def ticket_messages(
        updated_datetime=dlt.sources.incremental(
            "updated_datetime", start_date_obj, range_end="closed", range_start="closed"
        ),
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

    @dlt.resource(
        primary_key="id",
        write_disposition="merge",
        columns={
            "id": {
                "data_type": "bigint",
                "nullable": False,
                "primary_key": True,
                "description": "ID of the satisfaction survey.",
            },
            "body_text": {
                "data_type": "text",
                "nullable": True,
                "description": "Text body of the satisfaction survey.",
            },
            "created_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": False,
                "description": "When the satisfaction survey was created.",
            },
            "customer_id": {
                "data_type": "bigint",
                "nullable": False,
                "description": "ID of the customer linked to the survey.",
            },
            "meta": {
                "data_type": "json",
                "nullable": True,
                "description": "Meta information associated with the survey.",
            },
            "score": {
                "data_type": "double",
                "nullable": True,
                "description": "Score given in the satisfaction survey.",
            },
            "scored_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "When the score was given.",
            },
            "sent_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "When the satisfaction survey was sent.",
            },
            "should_send_datetime": {
                "data_type": "timestamp",
                "precision": 6,
                "nullable": True,
                "description": "When the survey should be sent.",
            },
            "ticket_id": {
                "data_type": "bigint",
                "nullable": False,
                "description": "ID of the ticket linked to the survey.",
            },
            "uri": {
                "data_type": "text",
                "nullable": False,
                "description": "URI of the satisfaction survey.",
            },
        },
    )
    def satisfaction_surveys(
        updated_datetime=dlt.sources.incremental(
            "updated_datetime", start_date_obj, range_end="closed", range_start="closed"
        ),
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
