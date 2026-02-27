# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
This is a module that provides a DLT source to retrieve data from multiple endpoints of the HubSpot API using a specified API key. The retrieved data is returned as a tuple of Dlt resources, one for each endpoint.

The source retrieves data from the following endpoints:
- CRM Companies
- CRM Contacts
- CRM Deals
- CRM Tickets
- CRM Products
- CRM Quotes
- Web Analytics Events

For each endpoint, a resource and transformer function are defined to retrieve data and transform it to a common format.
The resource functions yield the raw data retrieved from the API, while the transformer functions are used to retrieve
additional information from the Web Analytics Events endpoint.

The source also supports enabling Web Analytics Events for each endpoint by setting the corresponding enable flag to True.

Example:
To retrieve data from all endpoints, use the following code:

python

>>> resources = hubspot(api_key="your_api_key")
"""

from typing import Any, Dict, Iterator, List, Literal, Optional, Sequence
from urllib.parse import parse_qs, quote, urlparse

import dlt
from dlt.common import pendulum
from dlt.common.typing import TDataItems
from dlt.sources import DltResource

from .helpers import (
    _get_property_names,
    fetch_data,
    fetch_data_raw,
    fetch_data_search,
    fetch_property_history,
)
from .settings import (
    ALL,
    CRM_OBJECT_ENDPOINTS,
    CRM_OWNERS_ENDPOINT,
    CRM_SCHEMAS_ENDPOINT,
    DEFAULT_CALL_PROPS,
    DEFAULT_CART_PROPS,
    DEFAULT_COMMERCE_PAYMENT_PROPS,
    DEFAULT_COMPANY_PROPS,
    DEFAULT_CONTACT_PROPS,
    DEFAULT_DEAL_PROPS,
    DEFAULT_DISCOUNT_PROPS,
    DEFAULT_EMAIL_PROPS,
    DEFAULT_FEE_PROPS,
    DEFAULT_FEEDBACK_SUBMISSION_PROPS,
    DEFAULT_INVOICE_PROPS,
    DEFAULT_LINE_ITEM_PROPS,
    DEFAULT_MEETING_PROPS,
    DEFAULT_NOTE_PROPS,
    DEFAULT_PRODUCT_PROPS,
    DEFAULT_QUOTE_PROPS,
    DEFAULT_TASK_PROPS,
    DEFAULT_TAX_PROPS,
    DEFAULT_TICKET_PROPS,
    OBJECT_TYPE_PLURAL,
    STARTDATE,
    WEB_ANALYTICS_EVENTS_ENDPOINT,
)

THubspotObjectType = Literal[
    "company",
    "contact",
    "deal",
    "ticket",
    "product",
    "quote",
    "call",
    "email",
    "feedback_submission",
    "line_item",
    "meeting",
    "note",
    "task",
    "cart",
    "discount",
    "fee",
    "invoice",
    "commerce_payment",
    "tax",
]


def _last_value_to_ms(last_value) -> Optional[str]:
    """Convert dlt incremental last_value (ISO string or datetime) to ms timestamp string."""
    if last_value is None:
        return None
    dt = (
        pendulum.parse(last_value)
        if isinstance(last_value, str)
        else pendulum.instance(last_value)
    )
    return str(int(dt.timestamp() * 1000))


@dlt.source(name="hubspot", max_table_nesting=0)
def hubspot(
    api_key: str = dlt.secrets.value,
    include_history: bool = False,
    include_custom_props: bool = True,
    custom_object: str = None,
) -> Sequence[DltResource]:
    """
    A DLT source that retrieves data from the HubSpot API using the
    specified API key.

    This function retrieves data for several HubSpot API endpoints,
    including companies, contacts, deals, tickets, products and web
    analytics events. It returns a tuple of Dlt resources, one for
    each endpoint.

    Args:
        api_key (Optional[str]):
            The API key used to authenticate with the HubSpot API. Defaults
            to dlt.secrets.value.
        include_history (Optional[bool]):
            Whether to load history of property changes along with entities.
            The history entries are loaded to separate tables.

    Returns:
        Sequence[DltResource]: Dlt resources, one for each HubSpot API endpoint.

    Notes:
        This function uses the `fetch_data` function to retrieve data from the
        HubSpot CRM API. The API key is passed to `fetch_data` as the
        `api_key` argument.
    """

    @dlt.resource(
        name="companies", write_disposition="merge", primary_key=["hs_object_id"]
    )
    def companies(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot companies resource"""
        yield from crm_objects(
            "company",
            api_key,
            include_history=include_history,
            props=DEFAULT_COMPANY_PROPS,
            include_custom_props=include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(
        name="contacts", write_disposition="merge", primary_key=["hs_object_id"]
    )
    def contacts(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot contacts resource"""
        yield from crm_objects(
            "contact",
            api_key,
            include_history,
            DEFAULT_CONTACT_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(name="deals", write_disposition="merge", primary_key=["hs_object_id"])
    def deals(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot deals resource"""
        yield from crm_objects(
            "deal",
            api_key,
            include_history,
            DEFAULT_DEAL_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(
        name="tickets", write_disposition="merge", primary_key=["hs_object_id"]
    )
    def tickets(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot tickets resource"""
        yield from crm_objects(
            "ticket",
            api_key,
            include_history,
            DEFAULT_TICKET_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(
        name="products", write_disposition="merge", primary_key=["hs_object_id"]
    )
    def products(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot products resource"""
        yield from crm_objects(
            "product",
            api_key,
            include_history,
            DEFAULT_PRODUCT_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(name="calls", write_disposition="merge", primary_key=["hs_object_id"])
    def calls(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot calls resource"""
        yield from crm_objects(
            "call",
            api_key,
            include_history,
            DEFAULT_CALL_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(
        name="emails", write_disposition="merge", primary_key=["hs_object_id"]
    )
    def emails(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot emails resource"""
        yield from crm_objects(
            "email",
            api_key,
            include_history,
            DEFAULT_EMAIL_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(
        name="feedback_submissions",
        write_disposition="merge",
        primary_key=["hs_object_id"],
    )
    def feedback_submissions(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot feedback submissions resource"""
        yield from crm_objects(
            "feedback_submission",
            api_key,
            include_history,
            DEFAULT_FEEDBACK_SUBMISSION_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(
        name="line_items", write_disposition="merge", primary_key=["hs_object_id"]
    )
    def line_items(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot line items resource"""
        yield from crm_objects(
            "line_item",
            api_key,
            include_history,
            DEFAULT_LINE_ITEM_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(
        name="meetings", write_disposition="merge", primary_key=["hs_object_id"]
    )
    def meetings(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot meetings resource"""
        yield from crm_objects(
            "meeting",
            api_key,
            include_history,
            DEFAULT_MEETING_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(name="notes", write_disposition="merge", primary_key=["hs_object_id"])
    def notes(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot notes resource"""
        yield from crm_objects(
            "note",
            api_key,
            include_history,
            DEFAULT_NOTE_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(name="tasks", write_disposition="merge", primary_key=["hs_object_id"])
    def tasks(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot tasks resource"""
        yield from crm_objects(
            "task",
            api_key,
            include_history,
            DEFAULT_TASK_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(name="carts", write_disposition="merge", primary_key=["hs_object_id"])
    def carts(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot carts resource"""
        yield from crm_objects(
            "cart",
            api_key,
            include_history,
            DEFAULT_CART_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(
        name="discounts", write_disposition="merge", primary_key=["hs_object_id"]
    )
    def discounts(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot discounts resource"""
        yield from crm_objects(
            "discount",
            api_key,
            include_history,
            DEFAULT_DISCOUNT_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(name="fees", write_disposition="merge", primary_key=["hs_object_id"])
    def fees(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot fees resource"""
        yield from crm_objects(
            "fee",
            api_key,
            include_history,
            DEFAULT_FEE_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(
        name="invoices", write_disposition="merge", primary_key=["hs_object_id"]
    )
    def invoices(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot invoices resource"""
        yield from crm_objects(
            "invoice",
            api_key,
            include_history,
            DEFAULT_INVOICE_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(
        name="commerce_payments",
        write_disposition="merge",
        primary_key=["hs_object_id"],
    )
    def commerce_payments(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot commerce payments resource"""
        yield from crm_objects(
            "commerce_payment",
            api_key,
            include_history,
            DEFAULT_COMMERCE_PAYMENT_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(name="taxes", write_disposition="merge", primary_key=["hs_object_id"])
    def taxes(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot taxes resource"""
        yield from crm_objects(
            "tax",
            api_key,
            include_history,
            DEFAULT_TAX_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(
        name="quotes", write_disposition="merge", primary_key=["hs_object_id"]
    )
    def quotes(
        api_key: str = api_key,
        include_history: bool = include_history,
        include_custom_props: bool = include_custom_props,
        updated_at: dlt.sources.incremental[str] = dlt.sources.incremental(
            "hs_lastmodifieddate", initial_value=None, row_order="asc"
        ),
    ) -> Iterator[TDataItems]:
        """Hubspot quotes resource"""
        yield from crm_objects(
            "quote",
            api_key,
            include_history,
            DEFAULT_QUOTE_PROPS,
            include_custom_props,
            start_date_ms=_last_value_to_ms(updated_at.last_value),
        )

    @dlt.resource(name="owners", write_disposition="merge", primary_key="id")
    def owners(
        api_key: str = api_key,
    ) -> Iterator[TDataItems]:
        """Hubspot owners resource"""
        yield from fetch_data(CRM_OWNERS_ENDPOINT, api_key, resource_name="owners")

    @dlt.resource(name="schemas", write_disposition="merge", primary_key="id")
    def schemas(
        api_key: str = api_key,
    ) -> Iterator[TDataItems]:
        """Hubspot schemas resource"""
        yield from fetch_data(CRM_SCHEMAS_ENDPOINT, api_key, resource_name="schemas")

    @dlt.resource(write_disposition="merge", primary_key="hs_object_id")
    def custom(
        api_key: str = api_key,
        custom_object_name: str = custom_object,
    ) -> Iterator[TDataItems]:
        custom_objects = fetch_data_raw(CRM_SCHEMAS_ENDPOINT, api_key)
        object_type_id = None
        associations = None
        if ":" in custom_object_name:
            fields = custom_object_name.split(":")
            if len(fields) == 2:
                custom_object_name = fields[0]
                associations = fields[1]

        custom_object_lowercase = custom_object_name.lower()

        for custom_object in custom_objects["results"]:
            if custom_object["name"].lower() == custom_object_lowercase:
                object_type_id = custom_object["objectTypeId"]
                break

            # sometimes people use the plural name of the object type by accident, we should try to match that if we can
            if "labels" in custom_object:
                if custom_object_lowercase == custom_object["labels"]["plural"].lower():
                    object_type_id = custom_object["objectTypeId"]
                    break

        if object_type_id is None:
            raise ValueError(f"There is no such custom object as {custom_object_name}")
        custom_object_properties = f"crm/v3/properties/{object_type_id}"

        props_pages = fetch_data(custom_object_properties, api_key)
        props = []
        for page in props_pages:
            props.extend([prop["name"] for prop in page])
        props = ",".join(sorted(list(set(props))))

        custom_object_endpoint = f"crm/v3/objects/{object_type_id}/?properties={props}"
        if associations:
            custom_object_endpoint += f"&associations={associations}"

        """Hubspot custom object details resource"""
        yield from fetch_data(custom_object_endpoint, api_key, resource_name="custom")

    return (
        companies,
        contacts,
        deals,
        tickets,
        products,
        quotes,
        calls,
        emails,
        feedback_submissions,
        line_items,
        meetings,
        notes,
        tasks,
        carts,
        discounts,
        fees,
        invoices,
        commerce_payments,
        taxes,
        owners,
        schemas,
        custom,
    )


def crm_objects(
    object_type: str,
    api_key: str = dlt.secrets.value,
    include_history: bool = False,
    props: Sequence[str] = None,
    include_custom_props: bool = True,
    start_date_ms: Optional[str] = None,
) -> Iterator[TDataItems]:
    """Building blocks for CRM resources."""
    if props == ALL:
        props = list(_get_property_names(api_key, object_type))

    if include_custom_props:
        all_props = _get_property_names(api_key, object_type)
        custom_props = [prop for prop in all_props if not prop.startswith("hs_")]
        props = props + custom_props  # type: ignore

    props = ",".join(sorted(list(set(props))))

    if start_date_ms is not None:
        _qs = parse_qs(urlparse(CRM_OBJECT_ENDPOINTS[object_type]).query)
        assoc_types = [t for t in _qs.get("associations", [""])[0].split(",") if t]
        yield from fetch_data_search(
            object_type, api_key, props, start_date_ms, association_types=assoc_types
        )
        return

    params = {"properties": props, "limit": 100}

    yield from fetch_data(CRM_OBJECT_ENDPOINTS[object_type], api_key, params=params)
    if include_history:
        # Get history separately, as requesting both all properties and history together
        # is likely to hit hubspot's URL length limit
        for history_entries in fetch_property_history(
            CRM_OBJECT_ENDPOINTS[object_type],
            api_key,
            props,
        ):
            yield dlt.mark.with_table_name(
                history_entries,
                OBJECT_TYPE_PLURAL[object_type] + "_property_history",
            )


@dlt.resource
def hubspot_events_for_objects(
    object_type: THubspotObjectType,
    object_ids: List[str],
    api_key: str = dlt.secrets.value,
    start_date: pendulum.DateTime = STARTDATE,
) -> DltResource:
    """
    A standalone DLT resources that retrieves web analytics events from the HubSpot API for a particular object type and list of object ids.

    Args:
        object_type(THubspotObjectType, required): One of the hubspot object types see definition of THubspotObjectType literal
        object_ids: (List[THubspotObjectType], required): List of object ids to track events
        api_key (str, optional): The API key used to authenticate with the HubSpot API. Defaults to dlt.secrets.value.
        start_date (datetime, optional): The initial date time from which start getting events, default to STARTDATE

    Returns:
        incremental dlt resource to track events for objects from the list
    """

    end_date = pendulum.now().isoformat()
    name = object_type + "_events"

    def get_web_analytics_events(
        occurred_at: dlt.sources.incremental[str],
    ) -> Iterator[List[Dict[str, Any]]]:
        """
        A helper function that retrieves web analytics events for a given object type from the HubSpot API.

        Args:
            object_type (str): The type of object for which to retrieve web analytics events.

        Yields:
            dict: A dictionary representing a web analytics event.
        """
        for object_id in object_ids:
            yield from fetch_data(
                WEB_ANALYTICS_EVENTS_ENDPOINT.format(
                    objectType=object_type,
                    objectId=object_id,
                    occurredAfter=quote(occurred_at.last_value),
                    occurredBefore=quote(end_date),
                ),
                api_key=api_key,
            )

    return dlt.resource(
        get_web_analytics_events,
        name=name,
        primary_key="id",
        write_disposition="append",
        selected=True,
        table_name=lambda e: name + "_" + str(e["eventType"]),
    )(
        dlt.sources.incremental(
            "occurredAt",
            initial_value=start_date.isoformat(),
            range_end="closed",
            range_start="closed",
        )
    )
