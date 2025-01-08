import pytest
import dlt
from datetime import datetime
from appstore import app_store
from .client import AppStoreConnectClientInterface
from unittest.mock import Mock, MagicMock
from .models import *


def test_no_reports_found():
    """
    When the date range is valid and there are no reports found,
    an exception should be raised.
    """
    client = MagicMock()
    client.list_analytics_report_requests = MagicMock(
        return_value=AnalyticsReportRequestsResponse(
            [
                ReportRequest(
                    type="analyticsReportRequests",
                    id="123",
                    attributes=ReportRequestAttributes(
                        accessType="ONGOING",
                        stoppedDueToInactivity=False
                    )
                )
            ],
            None,
            None,
    ))
    client.list_analytics_reports = MagicMock(
        return_value=AnalyticsReportResponse(
            [
                Report(
                    type="analyticsReports",
                    id="123",
                    attributes=ReportAttributes(
                        name="app-downloads-detailed",
                        category="USER"
                    )
                )
            ],
            None,
            None,
    ))
    client.list_report_instances = MagicMock(
        return_value=AnalyticsReportInstancesResponse(
            [
                ReportInstance(
                    type="analyticsReportInstances",
                    id="123",
                    attributes=ReportInstanceAttributes(
                        granularity="DAILY",
                        processingDate="2024-01-03"
                    )
                )
            ],
            None,
            None,
    ))
    src = app_store(
        client,
        ["one"],
        start_date=datetime.fromisoformat("2024-01-01"),
        end_date=datetime.fromisoformat("2024-01-02"),
    ).with_resources("app-downloads-detailed")

    with pytest.raises(Exception):
        dlt.pipeline(destination="duckdb").run(src)
    
