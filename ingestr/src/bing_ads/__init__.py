import csv
from typing import Iterable

import dlt
import pendulum
from bingads.authorization import (  # type: ignore
    AuthorizationData,
    OAuthWebAuthCodeGrant,
)
from bingads.v13.reporting import (  # type: ignore
    ReportingDownloadParameters,
    ReportingServiceManager,
)
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TDataItem
from dlt.sources import DltResource


def _create_manager(
    client_id: str,
    client_secret: str,
    refresh_token: str,
    developer_token: str,
    customer_id: str,
    account_id: str,
    environment: str,
) -> ReportingServiceManager:
    auth = OAuthWebAuthCodeGrant(
        client_id, client_secret, "https://login.live.com/oauth20_desktop.srf"
    )
    auth.request_oauth_tokens_by_refresh_token(refresh_token)
    auth_data = AuthorizationData(
        account_id=account_id,
        customer_id=customer_id,
        developer_token=developer_token,
        authentication=auth,
    )
    return ReportingServiceManager(auth_data, environment=environment)


@dlt.source(max_table_nesting=0)
def bing_ads_source(
    client_id: str,
    client_secret: str,
    refresh_token: str,
    developer_token: str,
    customer_id: str,
    account_id: str,
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime | None = None,
    environment: str = "production",
) -> Iterable[DltResource]:
    manager = _create_manager(
        client_id,
        client_secret,
        refresh_token,
        developer_token,
        customer_id,
        account_id,
        environment,
    )

    @dlt.resource(
        write_disposition="merge",
        name="campaign_performance",
        primary_key=["CampaignId", "Date"],
    )
    def campaign_performance(
        date=dlt.sources.incremental(
            "Date",
            initial_value=start_date,
            end_value=end_date,
            range_start="closed",
            range_end="closed",
        ),
    ) -> Iterable[TDataItem]:
        request = manager.factory.create("CampaignPerformanceReportRequest")
        request.Aggregation = "Daily"
        request.Format = "Csv"
        request.ReportName = "ingestr campaign report"
        request.ReturnOnlyCompleteData = False
        request.Language = "English"
        request.Scope = manager.factory.create("AccountThroughCampaignReportScope")
        request.Scope.AccountIds = {"long": [int(account_id)]}
        request.Scope.Campaigns = None
        request.Time = manager.factory.create("ReportTime")
        start_dt = ensure_pendulum_datetime(date.last_value)
        request.Time.CustomDateRangeStart = {
            "Year": start_dt.year,
            "Month": start_dt.month,
            "Day": start_dt.day,
        }
        end_dt = (
            ensure_pendulum_datetime(date.end_value)
            if date.end_value
            else pendulum.now()
        )
        request.Time.CustomDateRangeEnd = {
            "Year": end_dt.year,
            "Month": end_dt.month,
            "Day": end_dt.day,
        }
        request.Columns = manager.factory.create(
            "ArrayOfCampaignPerformanceReportColumn"
        )
        request.Columns.CampaignPerformanceReportColumn.append("TimePeriod")
        request.Columns.CampaignPerformanceReportColumn.append("CampaignId")
        request.Columns.CampaignPerformanceReportColumn.append("Impressions")
        request.Columns.CampaignPerformanceReportColumn.append("Clicks")

        params = ReportingDownloadParameters(
            report_request=request,
            result_file_name="campaign_report.csv",
        )
        file_path = manager.download_file(params)
        with open(file_path, newline="", encoding="utf-8") as f:
            reader = csv.DictReader(f)
            for row in reader:
                yield {
                    "Date": pendulum.parse(row["TimePeriod"]),
                    "CampaignId": int(row["CampaignId"]),
                    "Impressions": int(row.get("Impressions", 0)),
                    "Clicks": int(row.get("Clicks", 0)),
                }

    return campaign_performance
