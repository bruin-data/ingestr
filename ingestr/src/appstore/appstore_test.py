import pytest
import dlt
from dlt.pipeline.exceptions import PipelineStepFailed
from dlt.extract.exceptions import ResourceExtractionError
from datetime import datetime
from appstore import app_store
from .client import AppStoreConnectClientInterface
from unittest.mock import Mock, MagicMock, patch
from .models import *
from .errors import (
    NoReportsFoundError,
    NoOngoingReportRequestsFoundError,
    NoSuchReportError,
)

def has_exception(exception, exc_type):
    if isinstance(exception, pytest.ExceptionInfo):
        exception = exception.value
    
    while exception:
        if isinstance(exception, exc_type):
            return True
        exception = exception.__cause__
    return False

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
                        accessType="ONE_TIME_SNAPSHOT",
                        stoppedDueToInactivity=False
                    )
                ),
                ReportRequest(
                    type="analyticsReportRequests",
                    id="124",
                    attributes=ReportRequestAttributes(
                        accessType="ONGOING",
                        stoppedDueToInactivity=True
                    )
                )
            ],
            None,
            None,
    ))
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
            [],
            None,
            None,
    ))
    src = app_store(
        client,
        ["one"],
    ).with_resources("app-downloads-detailed")

    with pytest.raises(Exception) as exc:
        dlt.pipeline(destination="duckdb").run(src)

    assert has_exception(exc, NoSuchReportError)