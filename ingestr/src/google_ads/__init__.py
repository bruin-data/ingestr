"""
Preliminary implementation of Google Ads pipeline.
"""

from typing import Iterator, List, Union
import dlt
import tempfile
from dlt.common.exceptions import MissingDependencyException
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from dlt.sources.credentials import GcpOAuthCredentials, GcpServiceAccountCredentials
import json
from .helpers.data_processing import to_dict

from apiclient.discovery import Resource

try:
    from google.ads.googleads.client import GoogleAdsClient  # type: ignore
except ImportError:
    raise MissingDependencyException("Requests-OAuthlib", ["google-ads"])


DIMENSION_TABLES = [
    "accounts",
    "ad_group",
    "ad_group_ad",
    "ad_group_ad_label",
    "ad_group_label",
    "campaign_label",
    "click_view",
    "customer",
    "keyword_view",
    "geographic_view",
]


def get_client(
    credentials: Union[GcpOAuthCredentials, GcpServiceAccountCredentials],
    dev_token: str,
    impersonated_email: str,
) -> GoogleAdsClient:
    # generate access token for credentials if we are using OAuth2.0
    if isinstance(credentials, GcpOAuthCredentials):
        credentials.auth("https://www.googleapis.com/auth/adwords")
        conf = {
            "developer_token": dev_token,
            "use_proto_plus": True,
            **json.loads(credentials.to_native_representation()),
        }
        return GoogleAdsClient.load_from_dict(config_dict=conf)
    # use service account to authenticate if not using OAuth2.0
    else:
        # google ads client requires the key to be in a file on the disc..
        with tempfile.NamedTemporaryFile() as f:
            f.write(credentials.to_native_representation().encode())
            f.seek(0)
            return GoogleAdsClient.load_from_dict(
                config_dict={
                    "json_key_file_path": f.name,
                    "impersonated_email": impersonated_email,
                    "use_proto_plus": True,
                    "developer_token": dev_token,
                }
            )


@dlt.source(max_table_nesting=2)
def google_ads(
    credentials: Union[
        GcpOAuthCredentials, GcpServiceAccountCredentials
    ] = dlt.secrets.value,
    impersonated_email: str = dlt.secrets.value,
    dev_token: str = dlt.secrets.value,
) -> List[DltResource]:
    """
    Loads default tables for google ads in the database.
    :param credentials:
    :param dev_token:
    :return:
    """
    client = get_client(
        credentials=credentials,
        dev_token=dev_token,
        impersonated_email=impersonated_email,
    )
    return [
        customers(client=client),
        campaigns(client=client),
        change_events(client=client),
        customer_clients(client=client),
    ]


@dlt.resource(write_disposition="replace")
def customers(
    client: Resource, customer_id: str = dlt.secrets.value
) -> Iterator[TDataItem]:
    """
    Dlt resource which loads dimensions.
    :param client:
    :return:
    """
    # Issues a search request using streaming.
    ga_service = client.get_service("GoogleAdsService")
    query = "SELECT customer.id, customer.descriptive_name FROM customer"
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            yield to_dict(row.customer)


@dlt.resource(write_disposition="replace")
def campaigns(
    client: Resource, customer_id: str = dlt.secrets.value
) -> Iterator[TDataItem]:
    """
    Dlt resource which loads dimensions.
    :param client:
    :return:
    """
    # Issues a search request using streaming.
    ga_service = client.get_service("GoogleAdsService")
    query = "SELECT campaign.id, campaign.labels FROM campaign"
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            yield to_dict(row.campaign)


@dlt.resource(write_disposition="replace")
def change_events(
    client: Resource, customer_id: str = dlt.secrets.value
) -> Iterator[TDataItem]:
    """
    Dlt resource which loads dimensions.
    :param client:
    :return:
    """
    # Issues a search request using streaming.
    ga_service = client.get_service("GoogleAdsService")
    query = "SELECT change_event.change_date_time FROM change_event WHERE change_event.change_date_time during LAST_14_DAYS LIMIT 1000"
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            yield to_dict(row.change_event)


@dlt.resource(write_disposition="replace")
def customer_clients(
    client: Resource, customer_id: str = dlt.secrets.value
) -> Iterator[TDataItem]:
    """
    Dlt resource which loads dimensions.
    :param client:
    :return:
    """
    # Issues a search request using streaming.
    ga_service = client.get_service("GoogleAdsService")
    query = "SELECT customer_client.status FROM customer_client"
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            yield to_dict(row.customer_client)
