"""
Defines all the sources and resources needed for Google Analytics V4
"""

from typing import List, Optional, Union

import dlt
from dlt.common.typing import DictStrAny
from dlt.sources import DltResource
from dlt.sources.credentials import GcpOAuthCredentials, GcpServiceAccountCredentials
from google.analytics.data_v1beta import BetaAnalyticsDataClient

from .helpers import basic_report


@dlt.source(max_table_nesting=0)
def google_analytics(
    datetime: str,
    credentials: Union[
        GcpOAuthCredentials, GcpServiceAccountCredentials
    ] = dlt.secrets.value,
    property_id: int = dlt.config.value,
    queries: List[DictStrAny] = dlt.config.value,
    start_date: Optional[str] = "2015-08-14",
    rows_per_page: int = 10000,
) -> List[DltResource]:
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
    if len(queries) > 1:
        raise ValueError(
            "Google Analytics supports a single query ingestion at a time, please give only one query"
        )
    query = queries[0]

    # always add "date" to dimensions so we are able to track the last day of a report
    dimensions = query["dimensions"]
    resource_name = query["resource_name"]

    res = dlt.resource(
        basic_report, name="basic_report", merge_key=datetime, write_disposition="merge"
    )(
        client=client,
        rows_per_page=rows_per_page,
        property_id=property_id,
        dimensions=dimensions,
        metrics=query["metrics"],
        resource_name=resource_name,
        start_date=start_date,
        last_date=dlt.sources.incremental(
            datetime
        ),  # pass empty primary key to avoid unique checks, a primary key defined by the resource will be used
    )

    return [res]
