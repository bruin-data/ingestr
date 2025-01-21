import csv
import gzip
import os
import tempfile
from copy import deepcopy
from datetime import datetime
from typing import Iterable, List, Optional

import dlt
import requests
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from .client import AppStoreConnectClientInterface
from .errors import (
    NoOngoingReportRequestsFoundError,
    NoReportsFoundError,
    NoSuchReportError,
)
from .models import AnalyticsReportInstancesResponse
from .resources import RESOURCES


@dlt.source
def app_store(
    client: AppStoreConnectClientInterface,
    app_ids: List[str],
    start_date: Optional[datetime] = None,
    end_date: Optional[datetime] = None,
) -> Iterable[DltResource]:
    if start_date and start_date.tzinfo is not None:
        start_date = start_date.replace(tzinfo=None)
    if end_date and end_date.tzinfo is not None:
        end_date = end_date.replace(tzinfo=None)
    for resource in RESOURCES:
        yield dlt.resource(
            get_analytics_reports,
            name=resource.name,
            primary_key=resource.primary_key,
            columns=resource.columns,
        )(client, app_ids, resource.report_name, start_date, end_date)


def filter_instances_by_date(
    instances: AnalyticsReportInstancesResponse,
    start_date: Optional[datetime],
    end_date: Optional[datetime],
) -> AnalyticsReportInstancesResponse:
    instances = deepcopy(instances)
    if start_date is not None:
        instances.data = list(
            filter(
                lambda x: datetime.fromisoformat(x.attributes.processingDate)
                >= start_date,
                instances.data,
            )
        )
    if end_date is not None:
        instances.data = list(
            filter(
                lambda x: datetime.fromisoformat(x.attributes.processingDate)
                <= end_date,
                instances.data,
            )
        )

    return instances


def get_analytics_reports(
    client: AppStoreConnectClientInterface,
    app_ids: List[str],
    report_name: str,
    start_date: Optional[datetime],
    end_date: Optional[datetime],
    last_processing_date=dlt.sources.incremental("processing_date"),
) -> Iterable[TDataItem]:
    if last_processing_date.last_value:
        start_date = datetime.fromisoformat(last_processing_date.last_value)
    for app_id in app_ids:
        yield from get_report(client, app_id, report_name, start_date, end_date)


def get_report(
    client: AppStoreConnectClientInterface,
    app_id: str,
    report_name: str,
    start_date: Optional[datetime],
    end_date: Optional[datetime],
) -> Iterable[TDataItem]:
    report_requests = client.list_analytics_report_requests(app_id)
    ongoing_requests = list(
        filter(
            lambda x: x.attributes.accessType == "ONGOING"
            and not x.attributes.stoppedDueToInactivity,
            report_requests.data,
        )
    )

    if len(ongoing_requests) == 0:
        raise NoOngoingReportRequestsFoundError()

    reports = client.list_analytics_reports(ongoing_requests[0].id, report_name)
    if len(reports.data) == 0:
        raise NoSuchReportError(report_name)

    for report in reports.data:
        instances = client.list_report_instances(report.id)

        instances = filter_instances_by_date(instances, start_date, end_date)

        if len(instances.data) == 0:
            raise NoReportsFoundError()

        for instance in instances.data:
            segments = client.list_report_segments(instance.id)
            with tempfile.TemporaryDirectory() as temp_dir:
                files = []
                for segment in segments.data:
                    payload = requests.get(segment.attributes.url, stream=True)
                    payload.raise_for_status()

                    csv_path = os.path.join(
                        temp_dir, f"{segment.attributes.checksum}.csv"
                    )
                    with open(csv_path, "wb") as f:
                        for chunk in payload.iter_content(chunk_size=8192):
                            f.write(chunk)
                    files.append(csv_path)
                for file in files:
                    with gzip.open(file, "rt") as f:
                        # TODO: infer delimiter from the file itself
                        delimiter = (
                            "," if report_name == "App Crashes Expanded" else "\t"
                        )
                        reader = csv.DictReader(f, delimiter=delimiter)
                        for row in reader:
                            yield {
                                "processing_date": instance.attributes.processingDate,
                                **row,
                            }
