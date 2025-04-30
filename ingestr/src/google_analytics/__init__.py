"""
Defines all the sources and resources needed for Google Analytics V4
"""

from typing import Iterator, List, Optional, Union

import dlt
from dlt.common import pendulum
from dlt.common.typing import DictStrAny, TDataItem
from dlt.extract import DltResource
from dlt.sources.credentials import GcpOAuthCredentials, GcpServiceAccountCredentials
from google.analytics.data_v1beta import BetaAnalyticsDataClient
from google.analytics.data_v1beta.types import (
    Dimension,
    Metric,
    MinuteRange,
)

from .helpers import get_realtime_report, get_report


@dlt.source(max_table_nesting=0)
def google_analytics(
    datetime_dimension: str,
    credentials: Union[
        GcpOAuthCredentials, GcpServiceAccountCredentials
    ] = dlt.secrets.value,
    property_id: int = dlt.config.value,
    queries: List[DictStrAny] = dlt.config.value,
    start_date: Optional[pendulum.DateTime] = pendulum.datetime(2024, 1, 1),
    end_date: Optional[pendulum.DateTime] = None,
    rows_per_page: int = 10000,
    minute_range_objects: List[MinuteRange] | None = None,
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

    @dlt.resource(
        name="custom",
        merge_key=datetime_dimension,
        write_disposition="merge",
    )
    def basic_report(
        incremental=dlt.sources.incremental(
            datetime_dimension,
            initial_value=start_date,
            end_value=end_date,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterator[TDataItem]:
        start_date = incremental.last_value
        end_date = incremental.end_value
        if start_date is None:
            start_date = pendulum.datetime(2024, 1, 1)
        if end_date is None:
            end_date = pendulum.yesterday()
        yield from get_report(
            client=client,
            property_id=property_id,
            dimension_list=[Dimension(name=dimension) for dimension in dimensions],
            metric_list=[Metric(name=metric) for metric in query["metrics"]],
            per_page=rows_per_page,
            start_date=start_date,
            end_date=end_date,
        )

    # real time report
    @dlt.resource(
        name="realtime",
        merge_key="ingested_at",
        write_disposition="merge",
    )
    def real_time_report() -> Iterator[TDataItem]:
        yield from get_realtime_report(
            client=client,
            property_id=property_id,
            dimension_list=[Dimension(name=dimension) for dimension in dimensions],
            metric_list=[Metric(name=metric) for metric in query["metrics"]],
            per_page=rows_per_page,
            minute_range_objects=minute_range_objects,
        )

    # res = dlt.resource(
    #     basic_report, name="basic_report", merge_key=datetime_dimension, write_disposition="merge"
    # )(
    #     client=client,
    #     rows_per_page=rows_per_page,
    #     property_id=property_id,
    #     dimensions=dimensions,
    #     metrics=query["metrics"],
    #     resource_name=resource_name,
    #     last_date=dlt.sources.incremental(
    #         datetime_dimension,
    #         initial_value=start_date,
    #         end_value=end_date,
    #     ),
    # )

    return [basic_report, real_time_report]
