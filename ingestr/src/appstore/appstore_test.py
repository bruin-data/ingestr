import gzip
import io
from datetime import datetime
from unittest.mock import MagicMock, patch

import dlt
import duckdb
import pytest
import requests

from appstore import app_store  # type: ignore

from .errors import (
    NoOngoingReportRequestsFoundError,
    NoReportsFoundError,
    NoSuchReportError,
)
from .models import (
    AnalyticsReportInstancesResponse,
    AnalyticsReportRequestsResponse,
    AnalyticsReportResponse,
    AnalyticsReportSegmentsResponse,
    Report,
    ReportAttributes,
    ReportInstance,
    ReportInstanceAttributes,
    ReportRequest,
    ReportRequestAttributes,
    ReportSegment,
    ReportSegmentAttributes,
)


def has_exception(exception, exc_type):
    if isinstance(exception, pytest.ExceptionInfo):
        exception = exception.value

    while exception:
        if isinstance(exception, exc_type):
            return True
        exception = exception.__cause__
    return False


@pytest.fixture
def app_download_testdata():
    return """\
Date\tApp Apple Identifier\tCounts\tProcessing Date\tApp Name\tDownload Type\tApp Version\tDevice\tPlatform Version\tSource Type\tSource Info\tCampaign\tPage Type\tPage Title\tPre-Order\tTerritory
2025-01-07\t1\t590\t2025-01-08\tAcme Inc\tAuto-update\t4.2.40\tiPhone\tiOS 18.1\tApp Store search\t""\t""\tNo page\tNo page\t""\tFR
2025-01-07\t1\t16\t2025-01-08\tAcme Inc\tAuto-update\t4.2.40\tiPhone\tiOS 18.1\tApp referrer\tcom.burbn.instagram\t""\tStore sheet\tDefault custom product page\t""\tSG
2025-01-07\t1\t11\t2025-01-08\tAcme Inc\tAuto-update\t4.2.40\tiPhone\tiOS 18.3\tApp Store search\t""\t""\tNo page\tNo page\t""\tMX
"""


def test_no_report_instances_found():
    """
    When there are no report instances for the given date range,
    NoReportsError should be raised.
    """
    client = MagicMock()
    client.list_analytics_report_requests = MagicMock(
        return_value=AnalyticsReportRequestsResponse(
            [
                ReportRequest(
                    type="analyticsReportRequests",
                    id="123",
                    attributes=ReportRequestAttributes(
                        accessType="ONGOING", stoppedDueToInactivity=False
                    ),
                )
            ],
            None,
            None,
        )
    )
    client.list_analytics_reports = MagicMock(
        return_value=AnalyticsReportResponse(
            [
                Report(
                    type="analyticsReports",
                    id="123",
                    attributes=ReportAttributes(
                        name="app-downloads-detailed", category="USER"
                    ),
                )
            ],
            None,
            None,
        )
    )
    client.list_report_instances = MagicMock(
        return_value=AnalyticsReportInstancesResponse(
            [
                ReportInstance(
                    type="analyticsReportInstances",
                    id="123",
                    attributes=ReportInstanceAttributes(
                        granularity="DAILY", processingDate="2024-01-03"
                    ),
                )
            ],
            None,
            None,
        )
    )
    src = app_store(
        client,
        ["one"],
        start_date=datetime.fromisoformat("2024-01-01"),
        end_date=datetime.fromisoformat("2024-01-02"),
    ).with_resources("app-downloads-detailed")

    with pytest.raises(Exception) as exc:
        dlt.pipeline(destination="duckdb").run(src)

    assert has_exception(exc, NoReportsFoundError)


def test_no_ongoing_reports_found():
    client = MagicMock()
    client.list_analytics_report_requests = MagicMock(
        return_value=AnalyticsReportRequestsResponse(
            [
                ReportRequest(
                    type="analyticsReportRequests",
                    id="123",
                    attributes=ReportRequestAttributes(
                        accessType="ONE_TIME_SNAPSHOT", stoppedDueToInactivity=False
                    ),
                ),
                ReportRequest(
                    type="analyticsReportRequests",
                    id="124",
                    attributes=ReportRequestAttributes(
                        accessType="ONGOING", stoppedDueToInactivity=True
                    ),
                ),
            ],
            None,
            None,
        )
    )
    src = app_store(
        client,
        ["one"],
    ).with_resources("app-downloads-detailed")

    with pytest.raises(Exception) as exc:
        dlt.pipeline(destination="duckdb").run(src)

    assert has_exception(exc, NoOngoingReportRequestsFoundError)


def test_no_such_report():
    client = MagicMock()
    client.list_analytics_report_requests = MagicMock(
        return_value=AnalyticsReportRequestsResponse(
            [
                ReportRequest(
                    type="analyticsReportRequests",
                    id="123",
                    attributes=ReportRequestAttributes(
                        accessType="ONGOING", stoppedDueToInactivity=False
                    ),
                )
            ],
            None,
            None,
        )
    )
    client.list_analytics_reports = MagicMock(
        return_value=AnalyticsReportResponse(
            [],
            None,
            None,
        )
    )
    src = app_store(
        client,
        ["one"],
    ).with_resources("app-downloads-detailed")

    with pytest.raises(Exception) as exc:
        dlt.pipeline(destination="duckdb").run(src)

    assert has_exception(exc, NoSuchReportError)


def test_successful_ingestion(app_download_testdata):
    client = MagicMock()
    client.list_analytics_report_requests = MagicMock(
        return_value=AnalyticsReportRequestsResponse(
            [
                ReportRequest(
                    type="analyticsReportRequests",
                    id="123",
                    attributes=ReportRequestAttributes(
                        accessType="ONGOING", stoppedDueToInactivity=False
                    ),
                )
            ],
            None,
            None,
        )
    )
    client.list_analytics_reports = MagicMock(
        return_value=AnalyticsReportResponse(
            [
                Report(
                    type="analyticsReports",
                    id="123",
                    attributes=ReportAttributes(
                        name="app-downloads-detailed", category="USER"
                    ),
                )
            ],
            None,
            None,
        )
    )

    client.list_report_instances = MagicMock(
        return_value=AnalyticsReportInstancesResponse(
            [
                ReportInstance(
                    type="analyticsReportInstances",
                    id="123",
                    attributes=ReportInstanceAttributes(
                        granularity="DAILY", processingDate="2025-01-01"
                    ),
                )
            ],
            None,
            None,
        )
    )

    client.list_report_segments = MagicMock(
        return_value=AnalyticsReportSegmentsResponse(
            [
                ReportSegment(
                    type="analyticsReportSegments",
                    id="123",
                    attributes=ReportSegmentAttributes(
                        checksum="checksum-0",
                        url="http://example.com/report.csv",  # we'll monkey patch requests.get to return this file
                        sizeInBytes=123,
                    ),
                )
            ],
            None,
            None,
        )
    )

    src = app_store(
        client,
        ["1"],
    ).with_resources("app-downloads-detailed")

    conn = duckdb.connect()
    dest = dlt.destinations.duckdb(
        credentials=conn,
    )

    with patch("requests.get") as mock_get:
        res = requests.Response()
        buffer = io.BytesIO()
        buffer.mode = "rw"
        archive = gzip.GzipFile(fileobj=buffer, mode="w")
        archive.write(app_download_testdata.encode())
        archive.close()
        buffer.seek(0)
        res.status_code = 200
        res.raw = buffer

        mock_get.return_value = res

        dlt.pipeline(destination=dest, dataset_name="public").run(src)

    conn.sql("select count(*) from public.app_downloads_detailed").fetchone()[0] == 3

def test_incremental_ingestion():
    pass