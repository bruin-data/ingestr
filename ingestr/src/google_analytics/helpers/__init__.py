"""Google analytics source helpers"""

from typing import Iterator, List

import dlt
from apiclient.discovery import Resource  # type: ignore
from dlt.common import logger, pendulum
from dlt.common.typing import TDataItem
from google.analytics.data_v1beta.types import (
    Dimension,
    Metric,
)
from pendulum.datetime import DateTime

from .data_processing import get_report


def basic_report(
    client: Resource,
    rows_per_page: int,
    dimensions: List[str],
    metrics: List[str],
    property_id: int,
    resource_name: str,
    start_date: str,
    last_date: dlt.sources.incremental[DateTime],
) -> Iterator[TDataItem]:
    """
    Retrieves the data for a report given dimensions, metrics, and filters required for the report.

    Args:
        client: The Google Analytics client used to make requests.
        dimensions: Dimensions for the report. See metadata for the full list of dimensions.
        metrics: Metrics for the report. See metadata for the full list of metrics.
        property_id: A reference to the Google Analytics project.
            More info: https://developers.google.com/analytics/devguides/reporting/data/v1/property-id
        rows_per_page: Controls how many rows are retrieved per page in the reports.
            Default is 10000, maximum possible is 100000.
        resource_name: The resource name used to save incremental into dlt state.
        start_date: Incremental load start_date.
            Default is taken from dlt state if it exists.
        last_date: Incremental load end date.
            Default is taken from dlt state if it exists.

    Returns:
        Generator of all rows of data in the report.
    """

    # grab the start time from last dlt load if not filled, if that is also empty then use the first day of the millennium as the start time instead
    if last_date.last_value:
        if start_date != "2015-08-14":
            logger.warning(
                f"Using the starting date: {last_date.last_value} for incremental report: {resource_name} and ignoring start date passed as argument {start_date}"
            )
        start_date = last_date.last_value.to_date_string()
    else:
        start_date = start_date or "2015-08-14"

    processed_response = get_report(
        client=client,
        property_id=property_id,
        # fill dimensions and metrics with the proper api client objects
        dimension_list=[Dimension(name=dimension) for dimension in dimensions],
        metric_list=[Metric(name=metric) for metric in metrics],
        limit=rows_per_page,
        start_date=start_date,
        # configure end_date to yesterday as a date string
        end_date=pendulum.now().to_date_string(),
    )
    yield from processed_response
