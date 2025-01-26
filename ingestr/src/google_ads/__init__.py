import json
from datetime import date, datetime
from typing import Any, Iterator, Optional

import dlt
import proto  # type: ignore
from dlt.common.exceptions import MissingDependencyException
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from flatten_json import flatten  # type: ignore
from googleapiclient.discovery import Resource  # type: ignore

from . import field
from .metrics import dlt_metrics_schema
from .predicates import date_predicate
from .reports import BUILTIN_REPORTS, Report

try:
    from google.ads.googleads.client import GoogleAdsClient  # type: ignore
except ImportError:
    raise MissingDependencyException("Requests-OAuthlib", ["google-ads"])


@dlt.source
def google_ads(
    client: GoogleAdsClient,
    customer_id: str,
    report_spec: Optional[str] = None,
    start_date: Optional[datetime] = None,
    end_date: Optional[datetime] = None,
) -> Iterator[DltResource]:
    date_range = dlt.sources.incremental(
        "segments_date",
        initial_value=start_date.date(),  # type: ignore
        end_value=end_date.date() if end_date is not None else None,  # type: ignore
        range_start="closed",
        range_end="closed",
    )
    if report_spec is not None:
        custom_report = Report().from_spec(report_spec)
        yield dlt.resource(
            daily_report,
            name="daily_report",
            write_disposition="merge",
            primary_key=custom_report.primary_keys(),
            columns=dlt_metrics_schema(custom_report.metrics),
        )(client, customer_id, custom_report, date_range)

    for report_name, report in BUILTIN_REPORTS.items():
        yield dlt.resource(
            daily_report,
            name=report_name,
            write_disposition="merge",
            primary_key=report.primary_keys(),
            columns=dlt_metrics_schema(report.metrics),
        )(client, customer_id, report, date_range)


def daily_report(
    client: Resource,
    customer_id: str,
    report: Report,
    date: dlt.sources.incremental[date],
) -> Iterator[TDataItem]:
    ga_service = client.get_service("GoogleAdsService")
    fields = report.dimensions + report.metrics + report.segments
    criteria = date_predicate("segments.date", date.last_value, date.end_value)  # type:ignore
    query = f"""
        SELECT
            {", ".join(fields)}
        FROM
            {report.resource}
        WHERE
            {criteria}
    """
    if report.unfilterable is True:
        i = query.index("WHERE", 0)
        query = query[:i]

    allowed_keys = set([field.to_column(k) for k in fields])
    stream = ga_service.search_stream(customer_id=customer_id, query=query)
    for batch in stream:
        for row in batch.results:
            data = flatten(merge_lists(to_dict(row)))
            if "segments_date" in data:
                data["segments_date"] = datetime.strptime(
                    data["segments_date"], "%Y-%m-%d"
                ).date()
            yield {k: v for k, v in data.items() if k in allowed_keys}


def to_dict(item: Any) -> TDataItem:
    """
    Processes a batch result (page of results per dimension) accordingly
    :param batch:
    :return:
    """
    return json.loads(
        proto.Message.to_json(
            item,
            preserving_proto_field_name=True,
            use_integers_for_enums=False,
            including_default_value_fields=False,
        )
    )


def merge_lists(item: dict) -> dict:
    replacements = {}
    for k, v in item.get("metrics", {}).items():
        if isinstance(v, list):
            replacements[k] = ",".join(v)
    if len(replacements) == 0:
        return item
    item["metrics"].update(replacements)
    return item
