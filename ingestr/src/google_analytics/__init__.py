"""
Defines all the sources and resources needed for Google Analytics V4
"""

from typing import Iterator, List, Optional, Union

import dlt
from apiclient.discovery import Resource
from dlt.common.typing import DictStrAny, TDataItem
from dlt.sources import DltResource
from dlt.sources.credentials import GcpOAuthCredentials, GcpServiceAccountCredentials
from google.analytics.data_v1beta import BetaAnalyticsDataClient
from google.analytics.data_v1beta.types import GetMetadataRequest, Metadata

from .helpers import basic_report
from .helpers.data_processing import to_dict
from .settings import START_DATE


@dlt.source(max_table_nesting=0)
def google_analytics(
    credentials: Union[
        GcpOAuthCredentials, GcpServiceAccountCredentials
    ] = dlt.secrets.value,
    property_id: int = dlt.config.value,
    queries: List[DictStrAny] = dlt.config.value,
    start_date: Optional[str] = START_DATE,
    rows_per_page: int = 1000,
) -> List[DltResource]:
    """
    The DLT source for Google Analytics. Loads basic Analytics info to the pipeline.

    Args:
        credentials: Credentials to the Google Analytics Account.
        property_id: A numeric Google Analytics property id.
            More info: https://developers.google.com/analytics/devguides/reporting/data/v1/property-id.
        queries: List containing info on all the reports being requested with all the dimensions and metrics per report.
        start_date: The string version of the date in the format yyyy-mm-dd and some other values.
            More info: https://developers.google.com/analytics/devguides/reporting/data/v1/rest/v1beta/DateRange.
            Can be left empty for default incremental load behavior.
        rows_per_page: Controls how many rows are retrieved per page in the reports.
            Default is 10000, maximum possible is 100000.

    Returns:
        resource_list: List containing all the resources in the Google Analytics Pipeline.
    """
    # validate input params for the most common mistakes
    try:
        property_id = int(property_id)
    except ValueError:
        raise ValueError(
            f"{property_id} is an invalid google property id. Please use a numeric id, and not your Measurement ID like G-7F1AE12JLR"
        )
    if property_id == 0:
        raise ValueError(
            "Google Analytics property id is 0. Did you forget to configure it?"
        )
    if not rows_per_page:
        raise ValueError("Rows per page cannot be 0")
    # generate access token for credentials if we are using OAuth2.0
    if isinstance(credentials, GcpOAuthCredentials):
        credentials.auth("https://www.googleapis.com/auth/analytics.readonly")

    # Build the service object for Google Analytics api.
    client = BetaAnalyticsDataClient(credentials=credentials.to_native_credentials())
    # get metadata needed for some resources
    metadata = get_metadata(client=client, property_id=property_id)
    resource_list = [metadata | metrics_table, metadata | dimensions_table]
    for query in queries:
        # always add "date" to dimensions so we are able to track the last day of a report
        dimensions = query["dimensions"]
        if "date" not in dimensions:
            # make a copy of dimensions
            dimensions = dimensions + ["date"]
        resource_name = query["resource_name"]
        resource_list.append(
            dlt.resource(basic_report, name=resource_name, write_disposition="append")(
                client=client,
                rows_per_page=rows_per_page,
                property_id=property_id,
                dimensions=dimensions,
                metrics=query["metrics"],
                resource_name=resource_name,
                start_date=start_date,
                last_date=dlt.sources.incremental(
                    "date", primary_key=()
                ),  # pass empty primary key to avoid unique checks, a primary key defined by the resource will be used
            )
        )
    return resource_list


@dlt.resource(selected=False)
def get_metadata(client: Resource, property_id: int) -> Iterator[Metadata]:
    """
    Get all the metrics and dimensions for a report.

    Args:
        client: The Google Analytics client used to make requests.
        property_id: A reference to the Google Analytics project.
            More info: https://developers.google.com/analytics/devguides/reporting/data/v1/property-id

    Yields:
        Metadata objects. Only 1 is expected but yield is used as dlt resources require yield to be used.
    """
    request = GetMetadataRequest(name=f"properties/{property_id}/metadata")
    metadata: Metadata = client.get_metadata(request)
    yield metadata


@dlt.transformer(data_from=get_metadata, write_disposition="replace", name="metrics")
def metrics_table(metadata: Metadata) -> Iterator[TDataItem]:
    """
    Loads data for metrics.

    Args:
        metadata: Metadata class object which contains all the information stored in the GA4 metadata.

    Yields:
        Generator of dicts, 1 metric at a time.
    """
    for metric in metadata.metrics:
        yield to_dict(metric)


@dlt.transformer(data_from=get_metadata, write_disposition="replace", name="dimensions")
def dimensions_table(metadata: Metadata) -> Iterator[TDataItem]:
    """
    Loads data for dimensions.

    Args:
        metadata: Metadata class object which contains all the information stored in the GA4 metadata.

    Yields:
        Generator of dicts, 1 dimension at a time.
    """
    for dimension in metadata.dimensions:
        yield to_dict(dimension)
