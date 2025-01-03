"""
Preliminary implementation of Google Ads pipeline.
"""

from typing import Iterator, List

import dlt
from dlt.common.exceptions import MissingDependencyException
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from googleapiclient.discovery import Resource  # type: ignore

from .helpers.data_processing import to_dict

try:
    from google.ads.googleads.client import GoogleAdsClient  # type: ignore
except ImportError:
    raise MissingDependencyException("Requests-OAuthlib", ["google-ads"])


@dlt.source(max_table_nesting=2)
def google_ads(
    client: GoogleAdsClient,
    customer_id: str,
) -> List[DltResource]:
    return [
        customers(client=client, customer_id=customer_id),
        campaigns(client=client, customer_id=customer_id),
        change_events(client=client, customer_id=customer_id),
        customer_clients(client=client, customer_id=customer_id),
    ]


@dlt.resource(write_disposition="replace")
def customers(client: Resource, customer_id: str) -> Iterator[TDataItem]:
    # Issues a search request using streaming.
    ga_service = client.get_service("GoogleAdsService")
    query = "SELECT customer.id, customer.descriptive_name FROM customer"
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            yield to_dict(row.customer)


@dlt.resource(write_disposition="replace")
def campaigns(client: Resource, customer_id: str) -> Iterator[TDataItem]:
    # Issues a search request using streaming.
    ga_service = client.get_service("GoogleAdsService")
    query = "SELECT campaign.id, campaign.labels FROM campaign"
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            yield to_dict(row.campaign)


@dlt.resource(write_disposition="replace")
def change_events(client: Resource, customer_id: str) -> Iterator[TDataItem]:
    # Issues a search request using streaming.
    ga_service = client.get_service("GoogleAdsService")
    query = "SELECT change_event.change_date_time FROM change_event WHERE change_event.change_date_time during LAST_14_DAYS LIMIT 1000"
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            yield to_dict(row.change_event)


@dlt.resource(write_disposition="replace")
def customer_clients(client: Resource, customer_id: str) -> Iterator[TDataItem]:
    # Issues a search request using streaming.
    ga_service = client.get_service("GoogleAdsService")
    query = "SELECT customer_client.status FROM customer_client"
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            yield to_dict(row.customer_client)
