"""
Preliminary implementation of Google Ads pipeline.
"""

from typing import Iterator, List, Optional
from datetime import datetime

import dlt
from dlt.common.exceptions import MissingDependencyException
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from googleapiclient.discovery import Resource  # type: ignore

from .helpers.data_processing import to_dict

from .predicates import date_predicate

try:
    from google.ads.googleads.client import GoogleAdsClient  # type: ignore
except ImportError:
    raise MissingDependencyException("Requests-OAuthlib", ["google-ads"])


@dlt.source(max_table_nesting=2)
def google_ads(
    client: GoogleAdsClient,
    customer_id: str,
    start_date: Optional[datetime] = None,
    end_date: Optional[datetime] = None,
) -> List[DltResource]:
    return [
        customers(client=client, customer_id=customer_id),
        campaigns(client=client, customer_id=customer_id),
        change_events(client=client, customer_id=customer_id),
        customer_clients(client=client, customer_id=customer_id),
        asset_report_daily(client=client, customer_id=customer_id, start_date=start_date, end_date=end_date),
        ad_report_daily(client=client, customer_id=customer_id, start_date=start_date, end_date=end_date),
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

@dlt.resource
def asset_report_daily(
    client: Resource,
    customer_id: str,
    start_date: Optional[datetime] = None,
    end_date: Optional[datetime] = None,
) -> Iterator[TDataItem]:
    # Issues a search request using streaming.
    ga_service = client.get_service("GoogleAdsService")
    query = f"""
        SELECT 
            metrics.clicks, 
            metrics.conversions, 
            metrics.conversions_value, 
            metrics.cost_micros, 
            metrics.impressions, 
            campaign.id, 
            campaign.name, 
            customer.id, 
            ad_group.id, 
            ad_group.name, 
            asset.id,
            segments.date
        FROM 
            ad_group_ad_asset_view 
        WHERE 
            {date_predicate("segments.date", start_date, end_date)}
    """
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            yield to_dict(row)

@dlt.resource
def ad_report_daily(
    client: Resource,
    customer_id: str,
    start_date: Optional[datetime] = None,
    end_date: Optional[datetime] = None,
) -> Iterator[TDataItem]:
    # Issues a search request using streaming.
    ga_service = client.get_service("GoogleAdsService")
    query = f"""
        SELECT
            metrics.clicks,
            metrics.conversions,
            metrics.conversions_value,
            metrics.impressions,
            metrics.cost_micros,
            metrics.video_quartile_p25_rate,
            metrics.video_quartile_p50_rate,
            metrics.video_quartile_p75_rate,
            metrics.video_quartile_p100_rate,
            customer.id,
            campaign.id,
            campaign.name,
            ad_group.id,
            ad_group.name,
            ad_group.status,
            ad_group_ad.ad.id,
            segments.date
        FROM
            ad_group_ad
        WHERE
            {date_predicate("segments.date", start_date, end_date)}
    """
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            yield to_dict(row)