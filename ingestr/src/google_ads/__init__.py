from typing import Iterator, List, Optional, Callable, Dict
from datetime import datetime

from flatten_json import flatten
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

class Report:
    resource: str
    dimensions: List[str]
    metrics: List[str]

    @classmethod
    def from_spec(cls, spec: str):
        """
        Parse a report specification string into a Report object.
        The expected format is:
        custom:{resource}:{dimensions}:{metrics}

        Example:
        custom:ad_group_ad_asset_view:ad_group.id,campaign.id:clicks,conversions
        """
        report = cls()
        if spec.count(":") != 3:
            raise ValueError("Invalid report specification format. Expected custom:{resource}:{dimensions}:{metrics}")

        _, resource, dimensions, metrics = spec.split(":")

        report.resource = resource
        report.dimensions = [
            d for d in map(cls._parse_dimension, dimensions.split(","))
        ]
        report.metrics = [
            m for m in map(cls._parse_metric, metrics.split(","))
        ]
        return report

    @classmethod
    def _parse_dimension(self, dim: str):
        dim = dim.strip()
        if dim.count(".") == 0:
            raise ValueError("Invalid dimension format. Expected {resource}.{field}")
        if dim.startswith("segments."):
            raise ValueError("Invalid dimension format. Segments are not allowed in dimensions.")
        return dim
    
    @classmethod
    def _parse_metric(self, metric: str):
        metric = metric.strip()
        if not metric.startswith("metrics."):
            metric = f"metrics.{metric.strip()}"
        return metric
    
@dlt.source
def google_ads(
    client: GoogleAdsClient,
    customer_id: str,
    report_spec: Optional[str] = None,
    start_date: Optional[datetime] = None,
    end_date: Optional[datetime] = None,
):
    report: Optional[Report] = None
    if report_spec is not None:
        report = Report().from_spec(report_spec)

    yield dlt.resource(
        daily_report,
        name="daily_report",
        write_disposition="merge",
        primary_key=report.dimensions,
    )(client, customer_id, report, start_date, end_date)

def daily_report(
    client: Resource,
    customer_id: str,
    report: Report,
    start_date: Optional[datetime] = None,
    end_date: Optional[datetime] = None,
):
    ga_service = client.get_service("GoogleAdsService")
    query = f"""
        SELECT
            {", ".join(report.dimensions + report.metrics)},
            segments.date
        FROM
            {report.resource}
        WHERE
            {date_predicate("segments.date", start_date, end_date)}
    """
    allowed_keys = set([
        k.replace(".", "_") 
        for k in report.dimensions + report.metrics + ["segments.date"]
    ])
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            data = flatten(to_dict(row))
            yield {
                k:v for k,v in data.items() if k in allowed_keys
            }