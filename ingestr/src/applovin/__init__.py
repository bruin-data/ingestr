import dlt
from datetime import datetime
from dlt.sources.rest_api import RESTAPIConfig, rest_api_resources
from enum import Enum

class ReportType(Enum):
    PUBLISHER = "publisher"
    ADVERTISER = "advertiser"

REPORT_SCHEMA = {
    ReportType.PUBLISHER: [
        "ad_type",
        "application",
        "application_is_hidden",
        "bidding_integration",
        "clicks",
        "country",
        "ctr",
        "day",
        "device_type",
        "ecpm",
        "impressions",
        "package_name",
        "placement_type",
        "platform",
        "revenue",
        "size",
        "store_id",
        "zone",
        "zone_id",
    ],
    ReportType.ADVERTISER: [
        "ad",
        "ad_creative_type",
        "ad_id",
        "ad_type",
        "app_id_external",
        "application",
        "average_cpa",
        "average_cpc",
        "campaign",
        "campaign_ad_type",
        "campaign_bid_goal",
        "campaign_id_external",
        "campaign_package_name",
        "campaign_roas_goal",
        "campaign_store_id",
        "campaign_type",
        "clicks",
        "conversions",
        "conversion_rate",
        "cost",
        "country",
        "creative_set",
        "creative_set_id",
        "ctr",
        "custom_page_id",
        "day",
        "device_type",
        "external_placement_id",
        "first_purchase",
        "hour",
        "impressions",
        "installs",
        "optimization_day_target",
        "placement_type",
        "platform",
        "redownloads",
        "sales",
        "size",
        "target_event",
        "traffic_source"
    ],
}


@dlt.source
def applovin_source(
    api_key: str,
    report_type: ReportType,
    start_date: str,
    end_date: str,
):
    # validate that start_date & end_date are within the last 45 days
    # validate that ReportType.ADVERTISER is only used with probabilisticReport & skaReport
    config: RESTAPIConfig = {
        "client": {
            "base_url": "https://r.applovin.com/",
            "auth": {
                "type": "api_key",
                "name": "api_key",
                "location": "query",
                "api_key": api_key,
            },
        },
        "resource_defaults": {
            "primary_key": REPORT_SCHEMA[report_type],
            "write_disposition": "merge",
            "endpoint": {
                "incremental": {
                    "cursor_path": "day",
                    "start_param": "start",
                    "end_param": "end",
                    "initial_value": start_date,
                    "end_value": end_date,
                },
                "params": {
                    "format": "json",
                    "report_type": report_type.value, 
                    "columns": ",".join(REPORT_SCHEMA[report_type]),
                },
            },
        },
        "resources": [
            "report",
            "probabilisticReport",
            "skaReport",
        ]
    }
    yield from rest_api_resources(config)
