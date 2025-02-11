from typing import Iterable

import dlt
from dlt.common.typing import TDataItem
from dlt.sources import DltResource, incremental
from simple_salesforce import Salesforce

from .helpers import get_records


@dlt.source(name="salesforce")
def salesforce_source(
    username: str,
    password: str,
    token: str,
) -> Iterable[DltResource]:
    """
    Retrieves data from Salesforce using the Salesforce API.

    Args:
        username (str): The username for authentication.
        password (str): The password for authentication.
        token (str): The security token for authentication.

    Yields:
        DltResource: Data resources from Salesforce.
    """

    client = Salesforce(username, password, token)

    # define resources
    @dlt.resource(write_disposition="replace")
    def user() -> Iterable[TDataItem]:
        yield get_records(client, "User")

    @dlt.resource(write_disposition="replace")
    def user_role() -> Iterable[TDataItem]:
        yield get_records(client, "UserRole")

    @dlt.resource(write_disposition="merge")
    def opportunity(
        last_timestamp: incremental[str] = dlt.sources.incremental(
            "SystemModstamp", initial_value=None
        ),
    ) -> Iterable[TDataItem]:
        yield get_records(
            client, "Opportunity", last_timestamp.last_value, "SystemModstamp"
        )

    @dlt.resource(write_disposition="merge")
    def opportunity_line_item(
        last_timestamp: incremental[str] = dlt.sources.incremental(
            "SystemModstamp", initial_value=None
        ),
    ) -> Iterable[TDataItem]:
        yield get_records(
            client, "OpportunityLineItem", last_timestamp.last_value, "SystemModstamp"
        )

    @dlt.resource(write_disposition="merge")
    def opportunity_contact_role(
        last_timestamp: incremental[str] = dlt.sources.incremental(
            "SystemModstamp", initial_value=None
        ),
    ) -> Iterable[TDataItem]:
        yield get_records(
            client,
            "OpportunityContactRole",
            last_timestamp.last_value,
            "SystemModstamp",
        )

    @dlt.resource(write_disposition="merge")
    def account(
        last_timestamp: incremental[str] = dlt.sources.incremental(
            "LastModifiedDate", initial_value=None
        ),
    ) -> Iterable[TDataItem]:
        yield get_records(
            client, "Account", last_timestamp.last_value, "LastModifiedDate"
        )

    @dlt.resource(write_disposition="replace")
    def contact() -> Iterable[TDataItem]:
        yield get_records(client, "Contact")

    @dlt.resource(write_disposition="replace")
    def lead() -> Iterable[TDataItem]:
        yield get_records(client, "Lead")

    @dlt.resource(write_disposition="replace")
    def campaign() -> Iterable[TDataItem]:
        yield get_records(client, "Campaign")

    @dlt.resource(write_disposition="merge")
    def campaign_member(
        last_timestamp: incremental[str] = dlt.sources.incremental(
            "SystemModstamp", initial_value=None
        ),
    ) -> Iterable[TDataItem]:
        yield get_records(
            client, "CampaignMember", last_timestamp.last_value, "SystemModstamp"
        )

    @dlt.resource(write_disposition="replace")
    def product() -> Iterable[TDataItem]:
        yield get_records(client, "Product2")

    @dlt.resource(write_disposition="replace")
    def pricebook() -> Iterable[TDataItem]:
        yield get_records(client, "Pricebook2")

    @dlt.resource(write_disposition="replace")
    def pricebook_entry() -> Iterable[TDataItem]:
        yield get_records(client, "PricebookEntry")

    @dlt.resource(write_disposition="merge")
    def task(
        last_timestamp: incremental[str] = dlt.sources.incremental(
            "SystemModstamp", initial_value=None
        ),
    ) -> Iterable[TDataItem]:
        yield get_records(client, "Task", last_timestamp.last_value, "SystemModstamp")

    @dlt.resource(write_disposition="merge")
    def event(
        last_timestamp: incremental[str] = dlt.sources.incremental(
            "SystemModstamp", initial_value=None
        ),
    ) -> Iterable[TDataItem]:
        yield get_records(client, "Event", last_timestamp.last_value, "SystemModstamp")

    return (
        user,
        user_role,
        opportunity,
        opportunity_line_item,
        opportunity_contact_role,
        account,
        contact,
        lead,
        campaign,
        campaign_member,
        product,
        pricebook,
        pricebook_entry,
        task,
        event,
    )
