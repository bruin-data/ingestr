import os
import dlt
import tempfile
import csv
import gzip
from copy import deepcopy
from typing import Optional
from datetime import datetime

import requests

from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from typing import List, Iterable, Sequence
from .client import AppStoreConnectClient
from .models import AnalyticsReportInstancesResponse

@dlt.source
def app_store(
    key_id: str,
    key_path: str,
    issuer_id: str,
    app_ids: List[str],
    start_date: Optional[datetime] = None,
    end_date: Optional[datetime] = None
) -> Sequence[DltResource]:
    key = None
    with open(key_path) as f: key = f.read()
    client = AppStoreConnectClient(
        key.encode(),
        key_id,
        issuer_id
    )

    return [
        app_downloads_detailed(client, app_ids, start_date, end_date),
        app_downloads_standard(client, app_ids, start_date, end_date)
    ]

def filter_instances_by_date(
        instances: AnalyticsReportInstancesResponse,
        start_date: Optional[datetime],
        end_date: Optional[datetime]
) -> AnalyticsReportInstancesResponse:
    instances = deepcopy(instances)
    if start_date is not None:
        instances.data = list(filter(lambda x: datetime.fromisoformat(x.attributes.processingDate) >= start_date, instances.data))
    if end_date is not None:
        instances.data = list(filter(lambda x: datetime.fromisoformat(x.attributes.processingDate) <= end_date, instances.data))

    return instances

def get_analytics_report(
        client: AppStoreConnectClient,
        app_id: str,
        report_name: str,
        start_date: Optional[datetime],
        end_date: Optional[datetime]
) -> Iterable[TDataItem]:
    report_requests = client.list_analytics_report_requests(app_id)
    ongoing_requests = list(filter(lambda x: x.attributes.accessType == "ONGOING" , report_requests.data))

    # todo: validate report is not stopped due to inactivity
    if len(ongoing_requests) == 0:
        raise Exception("No ONGOING report requests found")

    reports = client.list_analytics_reports(ongoing_requests[0].id, report_name)
    if len(reports.data) == 0:
        raise Exception(f"No such report found: {report_name}")

    for report in reports.data:
        instances = client.list_report_instances(report.id)

        instances = filter_instances_by_date(instances, start_date, end_date)

        if len(instances.data) == 0:
            raise Exception("No report instances found for the given date range")

        for instance in instances.data:
            segments = client.list_report_segments(instance.id)
            with tempfile.TemporaryDirectory() as temp_dir:
                files = []
                for segment in segments.data:
                    payload = requests.get(segment.attributes.url, stream=True)
                    payload.raise_for_status()

                    csv_path = os.path.join(temp_dir, f"{segment.attributes.checksum}.csv")
                    with open(csv_path, "wb") as f:
                        for chunk in payload.iter_content(chunk_size=8192):
                            f.write(chunk)
                    files.append(csv_path)
                for file in files:
                    with gzip.open(file, "rt") as f:
                        reader = csv.DictReader(f, delimiter="\t")
                        for row in reader:
                            yield {"processing_date": instance.attributes.processingDate, **row}

PRIMARY_KEY_APP_DOWNLOADS_DETAILED = [
    "date",
    "app_name",
    "app_apple_identifier",
    "download_type",
    "app_version",
    "device",
    "platform_version",
    "source_type",
    "source_info",
    "campaign",
    "page_type",
    "page_title",
    "pre_order",
    "territory",
    "processing_date",
]

PRIMARY_KEY_APP_DOWNLOADS_STANDARD = [
    "date",
    "app_name",
    "app_apple_identifier",
    "download_type",
    "app_version",
    "device",
    "platform_version",
    "source_type",
    "page_type",
    "pre_order",
    "territory",
]

COLUMN_HINTS_APP_DOWNLOADS = {
    "date": {
        "data_type": "date",
    },
    "app_apple_identifier": {
        "data_type": "bigint",
    },
    "counts": {
        "data_type": "bigint",
    }
}


@dlt.resource(
    name="app-downloads-detailed", 
    primary_key=PRIMARY_KEY_APP_DOWNLOADS_DETAILED,
    columns=COLUMN_HINTS_APP_DOWNLOADS,
)
def app_downloads_detailed(
    client: AppStoreConnectClient,
    app_ids: List[str],
    start_date: Optional[datetime],
    end_date: Optional[datetime]
) -> Iterable[TDataItem]:

    for app_id in app_ids:
        yield from get_analytics_report(client, app_id, "App Downloads Detailed", start_date, end_date)

@dlt.resource(
    name="app-downloads-standard",
    primary_key=PRIMARY_KEY_APP_DOWNLOADS_STANDARD,
    columns=COLUMN_HINTS_APP_DOWNLOADS,
)
def app_downloads_standard(
    client: AppStoreConnectClient,
    app_ids: List[str],
    start_date: Optional[datetime],
    end_date: Optional[datetime]
) -> Iterable[TDataItem]:
    for app_id in app_ids:
        yield from get_analytics_report(client, app_id, "App Downloads Standard", start_date, end_date)