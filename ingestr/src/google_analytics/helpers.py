"""
This module contains helpers that process data and make it ready for loading into the database
"""

import base64
import json
from typing import Any, Iterator, List, Union
from urllib.parse import parse_qs, urlparse

import proto
from dlt.common.exceptions import MissingDependencyException
from dlt.common.pendulum import pendulum
from dlt.common.typing import DictStrAny, TDataItem, TDataItems

try:
    from google.analytics.data_v1beta import BetaAnalyticsDataClient  # noqa: F401
    from google.analytics.data_v1beta.types import (
        DateRange,
        Dimension,
        DimensionExpression,  # noqa: F401
        DimensionMetadata,  # noqa: F401
        GetMetadataRequest,  # noqa: F401
        Metadata,  # noqa: F401
        Metric,
        MetricMetadata,  # noqa: F401
        MetricType,
        MinuteRange,
        RunRealtimeReportRequest,
        RunReportRequest,
        RunReportResponse,
    )
except ImportError:
    raise MissingDependencyException(
        "Google Analytics API Client", ["google-analytics-data"]
    )
try:
    from apiclient.discovery import Resource, build  # type: ignore # noqa: F401
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
    yield item


def get_realtime_report(
    client: Resource,
    property_id: int,
    dimension_list: List[Dimension],
    metric_list: List[Metric],
    per_page: int,
    minute_range_objects: List[MinuteRange] | None = None,
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

    Yields:
        Generator of all rows of data in the report.
    """
    offset = 0
    ingest_at = pendulum.now().to_date_string()

    while True:
        request = RunRealtimeReportRequest(
            property=f"properties/{property_id}",
            dimensions=dimension_list,
            metrics=metric_list,
            limit=per_page,
            minute_ranges=minute_range_objects if minute_range_objects else None,
        )
        response = client.run_realtime_report(request)

        # process request
        processed_response_generator = process_report(
            response=response, ingest_at=ingest_at
        )
        # import pdb; pdb.set_trace()
        yield from processed_response_generator
        offset += per_page
        if len(response.rows) < per_page or offset > 1000000:
            break


def get_report(
    client: Resource,
    property_id: int,
    dimension_list: List[Dimension],
    metric_list: List[Metric],
    per_page: int,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime,
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

    offset = 0
    while True:
        request = RunReportRequest(
            property=f"properties/{property_id}",
            dimensions=dimension_list,
            metrics=metric_list,
            limit=per_page,
            offset=offset,
            date_ranges=[
                DateRange(
                    start_date=start_date.to_date_string(),
                    end_date=end_date.to_date_string(),
                )
            ],
        )
        response = client.run_report(request)

        # process request
        processed_response_generator = process_report(response=response)

        # import pdb; pdb.set_trace()
        yield from processed_response_generator
        offset += per_page
        if len(response.rows) < per_page or offset > 1000000:
            break


def process_report(
    response: RunReportResponse, ingest_at: str | None = None
) -> Iterator[TDataItems]:
    metrics_headers = [header.name for header in response.metric_headers]
    dimensions_headers = [header.name for header in response.dimension_headers]

    distinct_key_combinations = {}

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
            response_dict[metrics_headers[i]] = metric_value
        if ingest_at is not None:
            response_dict["ingested_at"] = ingest_at

        unique_key = "-".join(list(response_dict.keys()))
        if unique_key not in distinct_key_combinations:
            distinct_key_combinations[unique_key] = True

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
    if dimension_name == "date":
        return pendulum.from_format(dimension_value, "YYYYMMDD", tz="UTC")
    elif dimension_name == "dateHour":
        return pendulum.from_format(dimension_value, "YYYYMMDDHH", tz="UTC")
    elif dimension_name == "dateHourMinute":
        return pendulum.from_format(dimension_value, "YYYYMMDDHHmm", tz="UTC")
    else:
        return dimension_value


def convert_minutes_ranges_to_minute_range_objects(
    minutes_ranges: str,
) -> List[MinuteRange]:
    minutes_ranges = minutes_ranges.strip()
    minutes = minutes_ranges.replace(" ", "").split(",")
    if minutes == "":
        raise ValueError(
            "Invalid input. Minutes range should be startminute-endminute format. For example: 1-2,5-6"
        )

    minute_range_objects = []
    for min_range in minutes:
        if "-" not in min_range:
            raise ValueError(
                "Invalid input. Minutes range should be startminute-endminute format. For example: 1-2,5-6"
            )
        parts = min_range.split("-")

        if not parts[0].isdigit() or not parts[1].isdigit():
            raise ValueError(
                f"Invalid input '{min_range}'. Both start and end minutes must be digits. For example: 1-2,5-6"
            )

        end_minutes_ago = int(parts[0])
        start_minutes_ago = int(parts[1])
        minute_range_objects.append(
            MinuteRange(
                name=f"{end_minutes_ago}-{start_minutes_ago} minutes ago",
                start_minutes_ago=start_minutes_ago,
                end_minutes_ago=end_minutes_ago,
            )
        )

    return minute_range_objects


def parse_google_analytics_uri(uri: str):
    parse_uri = urlparse(uri)
    source_fields = parse_qs(parse_uri.query)
    cred_path = source_fields.get("credentials_path")
    cred_base64 = source_fields.get("credentials_base64")

    if not cred_path and not cred_base64:
        raise ValueError(
            "credentials_path or credentials_base64 is required to connect Google Analytics"
        )
    credentials = {}
    if cred_path:
        with open(cred_path[0], "r") as f:
            credentials = json.load(f)
    elif cred_base64:
        credentials = json.loads(base64.b64decode(cred_base64[0]).decode("utf-8"))

    property_id = source_fields.get("property_id")
    if not property_id:
        raise ValueError("property_id is required to connect to Google Analytics")

    if (not cred_path and not cred_base64) or (not property_id):
        raise ValueError(
            "credentials_path or credentials_base64 and property_id are required to connect Google Analytics"
        )

    return {"credentials": credentials, "property_id": property_id[0]}
