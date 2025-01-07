import os
import dlt
import tempfile
import csv
import gzip

import requests

from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from typing import List, Iterable, Sequence
from .client import AppStoreConnectClient

@dlt.source
def app_store(
    key_id: str,
    key_path: str,
    issuer_id: str,
    app_ids: List[str],
) -> Sequence[DltResource]:
    key = None
    with open(key_path) as f: key = f.read()
    client = AppStoreConnectClient(
        key.encode(),
        key_id,
        issuer_id
    )

    return [
        app_downloads_detailed(client, app_ids)
    ]


@dlt.resource(name="app-downloads-detailed", primary_key=[])
def app_downloads_detailed(client: AppStoreConnectClient, app_ids: List[str]) -> Iterable[TDataItem]:
    for app_id in app_ids:
        report_requests = client.list_analytics_report_requests(app_id)
        ongoing_requests = list(filter(lambda x: x.attributes.accessType == "ONGOING" , report_requests.data))
        
        # todo: validate report is not stopped due to inactivity
        if len(ongoing_requests) == 0:
            raise Exception("No ONGOING report requests found")

        reports = client.list_analytics_reports(ongoing_requests[0].id, "App Downloads Detailed")
        for report in reports.data:
            instances = client.list_report_instances(report.id)

            # use the last instance for now
            latest_report = instances.data[-1]

            # todo: handle pagination
            segments = client.list_report_segments(latest_report.id)

            # handle segments
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
                            yield row