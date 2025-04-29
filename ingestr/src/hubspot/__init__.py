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

from typing import Any, Dict, Iterator, List, Literal, Sequence
from urllib.parse import quote

import dlt
from dlt.common import pendulum
from dlt.common.typing import TDataItems
from dlt.sources import DltResource

from .helpers import (
    _get_property_names,
    fetch_data,
    fetch_data_raw,
    fetch_property_history,
)
from .settings import (
    ALL,
    CRM_OBJECT_ENDPOINTS,
    CRM_SCHEMAS_ENDPOINT,
    DEFAULT_COMPANY_PROPS,
    DEFAULT_CONTACT_PROPS,
    DEFAULT_DEAL_PROPS,
    DEFAULT_PRODUCT_PROPS,
    DEFAULT_QUOTE_PROPS,
    DEFAULT_TICKET_PROPS,
    OBJECT_TYPE_PLURAL,
    STARTDATE,
    WEB_ANALYTICS_EVENTS_ENDPOINT,
)

THubspotObjectType = Literal["company", "contact", "deal", "ticket", "product", "quote"]


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

    @dlt.resource(name="companies", write_disposition="replace")
    def companies(
        api_key: str = api_key,
        include_history: bool = include_history,
        props: Sequence[str] = DEFAULT_COMPANY_PROPS,
        include_custom_props: bool = include_custom_props,
    ) -> Iterator[TDataItems]:
        """Hubspot companies resource"""
        yield from crm_objects(
            "company",
            api_key,
            include_history=include_history,
            props=props,
            include_custom_props=include_custom_props,
        )

    @dlt.resource(name="contacts", write_disposition="replace")
    def contacts(
        api_key: str = api_key,
        include_history: bool = include_history,
        props: Sequence[str] = DEFAULT_CONTACT_PROPS,
        include_custom_props: bool = include_custom_props,
    ) -> Iterator[TDataItems]:
        """Hubspot contacts resource"""
        yield from crm_objects(
            "contact",
            api_key,
            include_history,
            props,
            include_custom_props,
        )

    @dlt.resource(name="deals", write_disposition="replace")
    def deals(
        api_key: str = api_key,
        include_history: bool = include_history,
        props: Sequence[str] = DEFAULT_DEAL_PROPS,
        include_custom_props: bool = include_custom_props,
    ) -> Iterator[TDataItems]:
        """Hubspot deals resource"""
        yield from crm_objects(
            "deal",
            api_key,
            include_history,
            props,
            include_custom_props,
        )

    @dlt.resource(name="tickets", write_disposition="replace")
    def tickets(
        api_key: str = api_key,
        include_history: bool = include_history,
        props: Sequence[str] = DEFAULT_TICKET_PROPS,
        include_custom_props: bool = include_custom_props,
    ) -> Iterator[TDataItems]:
        """Hubspot tickets resource"""
        yield from crm_objects(
            "ticket",
            api_key,
            include_history,
            props,
            include_custom_props,
        )

    @dlt.resource(name="products", write_disposition="replace")
    def products(
        api_key: str = api_key,
        include_history: bool = include_history,
        props: Sequence[str] = DEFAULT_PRODUCT_PROPS,
        include_custom_props: bool = include_custom_props,
    ) -> Iterator[TDataItems]:
        """Hubspot products resource"""
        yield from crm_objects(
            "product",
            api_key,
            include_history,
            props,
            include_custom_props,
        )

    @dlt.resource(name="schemas", write_disposition="merge", primary_key="id")
    def schemas(
        api_key: str = api_key,
    ) -> Iterator[TDataItems]:
        """Hubspot schemas resource"""
        yield from fetch_data(CRM_SCHEMAS_ENDPOINT, api_key, resource_name="schemas")

    @dlt.resource(name="quotes", write_disposition="replace")
    def quotes(
        api_key: str = api_key,
        include_history: bool = include_history,
        props: Sequence[str] = DEFAULT_QUOTE_PROPS,
        include_custom_props: bool = include_custom_props,
    ) -> Iterator[TDataItems]:
        """Hubspot quotes resource"""
        yield from crm_objects(
            "quote",
            api_key,
            include_history,
            props,
            include_custom_props,
        )

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

    return companies, contacts, deals, tickets, products, quotes, schemas, custom


def crm_objects(
    object_type: str,
    api_key: str = dlt.secrets.value,
    include_history: bool = False,
    props: Sequence[str] = None,
    include_custom_props: bool = True,
) -> Iterator[TDataItems]:
    """Building blocks for CRM resources."""
    if props == ALL:
        props = list(_get_property_names(api_key, object_type))

    if include_custom_props:
        all_props = _get_property_names(api_key, object_type)
        custom_props = [prop for prop in all_props if not prop.startswith("hs_")]
        props = props + custom_props  # type: ignore

    props = ",".join(sorted(list(set(props))))

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
