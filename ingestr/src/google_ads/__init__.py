# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

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
    customer_ids: list[str],
    report_spec: Optional[str] = None,
    gaql_query: Optional[str] = None,
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
        custom_report, _ = Report.from_spec(report_spec)
        yield dlt.resource(
            daily_report,
            name="daily_report",
            write_disposition="merge",
            primary_key=custom_report.primary_keys() + ["customer_id"],
            columns=dlt_metrics_schema(custom_report.metrics),
        )(client, customer_ids, custom_report, date_range)

    if gaql_query is not None:
        yield dlt.resource(
            run_gaql_query,
            name="gaql_query",
            write_disposition="append",
            max_table_nesting=0,
        )(client, customer_ids, gaql_query, start_date, end_date)

    for report_name, report in BUILTIN_REPORTS.items():
        yield dlt.resource(
            daily_report,
            name=report_name,
            write_disposition="merge",
            primary_key=report.primary_keys() + ["customer_id"],
            columns=dlt_metrics_schema(report.metrics),
        )(client, customer_ids, report, date_range)


def daily_report(
    client: Resource,
    customer_ids: list[str],
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
    for customer_id in customer_ids:
        stream = ga_service.search_stream(customer_id=customer_id, query=query)
        for batch in stream:
            for row in batch.results:
                data = flatten(merge_lists(to_dict(row)))
                if "segments_date" in data:
                    data["segments_date"] = datetime.strptime(
                        data["segments_date"], "%Y-%m-%d"
                    ).date()
                row_data = {k: v for k, v in data.items() if k in allowed_keys}
                for pk in report.primary_keys():
                    if pk not in row_data or row_data[pk] is None or row_data[pk] == "":
                        row_data[pk] = "-"
                row_data["customer_id"] = customer_id
                yield row_data


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


def extract_fields(data: dict, field_paths: list[str]) -> dict:
    result = {}
    for path in field_paths:
        parts = path.split(".")
        value: Any = data
        for part in parts:
            if isinstance(value, dict):
                value = value.get(part)
            else:
                value = None
                break
        column_name = path.replace(".", "_")
        result[column_name] = value
    return result


def run_gaql_query(
    client: GoogleAdsClient,
    customer_ids: list[str],
    query: str,
    start_date: Optional[datetime] = None,
    end_date: Optional[datetime] = None,
) -> Iterator[TDataItem]:
    """
    Execute a raw Google Ads Query Language (GAQL) query.
    Supports :interval_start and :interval_end placeholders for date filtering.
    """
    ga_service = client.get_service("GoogleAdsService")

    if ":interval_start" in query:
        start_str = start_date.strftime("%Y-%m-%d") if start_date else "1970-01-01"
        query = query.replace(":interval_start", f"'{start_str}'")

    if ":interval_end" in query:
        end_str = (
            end_date.strftime("%Y-%m-%d")
            if end_date
            else date.today().strftime("%Y-%m-%d")
        )
        query = query.replace(":interval_end", f"'{end_str}'")

    field_paths = None
    for customer_id in customer_ids:
        stream = ga_service.search_stream(customer_id=customer_id, query=query)
        for batch in stream:
            if field_paths is None:
                field_paths = list(batch.field_mask.paths)
            for row in batch.results:
                data = extract_fields(to_dict(row), field_paths)
                data["customer_id"] = customer_id
                yield data
