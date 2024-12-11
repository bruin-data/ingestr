"""
This module contains helpers that process data and make it ready for loading into the database
"""

from typing import Any, Iterator, List, Union
from dlt.common.pendulum import pendulum
from dlt.common.exceptions import MissingDependencyException
from dlt.common.typing import DictStrAny, TDataItem, TDataItems
import proto
import json

try:
    from google.analytics.data_v1beta import BetaAnalyticsDataClient
    from google.analytics.data_v1beta.types import (
        DateRange,
        Dimension,
        DimensionExpression,
        DimensionMetadata,
        GetMetadataRequest,
        Metadata,
        Metric,
        MetricMetadata,
        MetricType,
        RunReportRequest,
        RunReportResponse,
    )
except ImportError:
    raise MissingDependencyException(
        "Google Analytics API Client", ["google-analytics-data"]
    )
try:
    from apiclient.discovery import build, Resource
except ImportError:
    raise MissingDependencyException("Google API Client", ["google-api-python-client"])


def to_dict(item: Any) -> Iterator[TDataItem]:
    """
    Processes a batch result (page of results per dimension) accordingly
    :param batch:
    :return:
    """
    item = json.loads(
        proto.Message.to_json(
            item,
            preserving_proto_field_name=True,
            use_integers_for_enums=False,
            including_default_value_fields=False,
        )
    )
    print(item)
    yield item


def get_report(
    client: Resource,
    property_id: int,
    dimension_list: List[Dimension],
    metric_list: List[Metric],
    limit: int,
    start_date: str,
    end_date: str,
) -> Iterator[TDataItem]:
    """
    Gets all the possible pages of reports with the given query parameters.
    Processes every page and yields a dictionary for every row of the report.

    Args:
        client: The Google Analytics client used to make requests.
        property_id: A reference to the Google Analytics project.
            More info: https://developers.google.com/analytics/devguides/reporting/data/v1/property-id
        dimension_list: A list of all the dimensions requested in the query.
        metric_list: A list of all the metrics requested in the query.
        limit: Describes how many rows there should be per page.
        start_date: The starting date of the query.
        end_date: The ending date of the query.

    Yields:
        Generator of all rows of data in the report.
    """

    # loop through all the pages
    # the total number of rows is received after the first request, for the first request to be sent through, initializing the row_count to 1 would suffice
    offset = 0
    row_count = 1
    limit = limit
    while offset < row_count:
        # make request to get the particular page
        request = RunReportRequest(
            property=f"properties/{property_id}",
            dimensions=dimension_list,
            metrics=metric_list,
            offset=offset,
            limit=limit,
            date_ranges=[DateRange(start_date=start_date, end_date=end_date)],
        )
        # process request
        response = client.run_report(request)
        processed_response_generator = process_report(response=response)
        yield from processed_response_generator
        # update
        row_count = response.row_count
        offset += limit


def process_report(response: RunReportResponse) -> Iterator[TDataItems]:
    """
    Receives a single page for a report response, processes it, and returns a generator for every row of data in the report page.

    Args:
        response: The API response for a single page of the report.

    Yields:
        Generator of dictionaries for every row of the report page.
    """

    metrics_headers = [header.name for header in response.metric_headers]
    dimensions_headers = [header.name for header in response.dimension_headers]
    for row in response.rows:
        response_dict: DictStrAny = {
            dimension_header: _resolve_dimension_value(
                dimension_header, dimension_value.value
            )
            for dimension_header, dimension_value in zip(
                dimensions_headers, row.dimension_values
            )
        }
        for i in range(len(metrics_headers)):
            # get metric type and process the value depending on type. Save metric name including type as well for the columns
            metric_type = response.metric_headers[i].type_
            metric_value = process_metric_value(
                metric_type=metric_type, value=row.metric_values[i].value
            )
            response_dict[
                f"{metrics_headers[i]}_{metric_type.name.split('_')[1]}"
            ] = metric_value
        yield response_dict


def process_metric_value(metric_type: MetricType, value: str) -> Union[str, int, float]:
    """
    Processes the metric type, converts it from string to the correct type, and returns it.

    Args:
        metric_type: The type of the metric.
        value: The value of the metric as a string.

    Returns:
        The given value converted to the correct data type.
    """

    # So far according to GA4 documentation these are the correct types: https://developers.google.com/analytics/devguides/reporting/data/v1/rest/v1beta/MetricType
    # 0 for strings, 1 for ints and 2-12 are different types of floating points.
    if metric_type.value == 0:
        return value
    elif metric_type.value == 1:
        return int(value)
    else:
        return float(value)


def _resolve_dimension_value(dimension_name: str, dimension_value: str) -> Any:
    """
    Helper function that receives a dimension's name and value and converts it to a datetime object if needed.

    Args:
        dimension_name: Name of the dimension.
        dimension_value: Value of the dimension.

    Returns:
        The value of the dimension with the correct data type.
    """

    if dimension_name == "date":
        return pendulum.from_format(dimension_value, "YYYYMMDD", tz="UTC")
    elif dimension_name == "dateHour":
        return pendulum.from_format(dimension_value, "YYYYMMDDHH", tz="UTC")
    elif dimension_name == "dateHourMinute":
        return pendulum.from_format(dimension_value, "YYYYMMDDHHmm", tz="UTC")
    else:
        return dimension_value
