from datetime import datetime, timedelta, timezone
from enum import Enum
from typing import Dict, List, Optional

import dlt
from dlt.sources.rest_api import EndpointResource, RESTAPIConfig, rest_api_resources
from requests import Response


class InvalidCustomReportError(Exception):
    def __init__(self):
        super().__init__(
            "Custom report should be in the format 'custom:{endpoint}:{report_type}:{dimensions}"
        )


class ClientError(Exception):
    pass


TYPE_HINTS = {
    "application_is_hidden": {"data_type": "bool"},
    "average_cpa": {"data_type": "double"},
    "average_cpc": {"data_type": "double"},
    "campaign_bid_goal": {"data_type": "double"},
    "campaign_roas_goal": {"data_type": "double"},
    "clicks": {"data_type": "bigint"},
    "conversions": {"data_type": "bigint"},
    "conversion_rate": {"data_type": "double"},
    "cost": {"data_type": "double"},  # assuming float.
    "ctr": {"data_type": "double"},
    "day": {"data_type": "date"},
    "first_purchase": {"data_type": "bigint"},
    "ecpm": {"data_type": "double"},
    "impressions": {"data_type": "bigint"},
    "installs": {"data_type": "bigint"},
    "revenue": {"data_type": "double"},
    "redownloads": {"data_type": "bigint"},
    "sales": {"data_type": "double"},  # assuming float.
}


class ReportType(Enum):
    PUBLISHER = "publisher"
    ADVERTISER = "advertiser"


REPORT_SCHEMA: Dict[ReportType, List[str]] = {
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
        "impressions",
        "installs",
        "optimization_day_target",
        "placement_type",
        "platform",
        "redownloads",
        "sales",
        "size",
        "target_event",
        "traffic_source",
    ],
}

PROBABILISTIC_REPORT_EXCLUDE = [
    "installs",
    "redownloads",
]


@dlt.source
def applovin_source(
    api_key: str,
    start_date: str,
    end_date: Optional[str],
    custom: Optional[str],
):
    backfill = False
    if end_date is None:
        backfill = True

        # use the greatest of yesterday and start_date
        end_date = max(
            datetime.now(timezone.utc) - timedelta(days=1),
            datetime.fromisoformat(start_date).replace(tzinfo=timezone.utc),
        ).strftime("%Y-%m-%d")

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
            "write_disposition": "merge",
            "endpoint": {
                "incremental": {
                    "cursor_path": "day",
                    "start_param": "start",
                    "initial_value": start_date,
                    "range_start": "closed",
                    "range_end": "closed",
                },
                "params": {
                    "format": "json",
                    "end": end_date,
                },
                "paginator": "single_page",
                "response_actions": [
                    http_error_handler,
                ],
            },
        },
        "resources": [
            resource(
                "publisher-report",
                "report",
                REPORT_SCHEMA[ReportType.PUBLISHER],
                ReportType.PUBLISHER,
            ),
            resource(
                "advertiser-report",
                "report",
                REPORT_SCHEMA[ReportType.ADVERTISER],
                ReportType.ADVERTISER,
            ),
            resource(
                "advertiser-probabilistic-report",
                "probabilisticReport",
                exclude(
                    REPORT_SCHEMA[ReportType.ADVERTISER], PROBABILISTIC_REPORT_EXCLUDE
                ),
                ReportType.ADVERTISER,
            ),
            resource(
                "advertiser-ska-report",
                "skaReport",
                REPORT_SCHEMA[ReportType.ADVERTISER],
                ReportType.ADVERTISER,
            ),
        ],
    }

    if custom:
        custom_report = custom_report_from_spec(custom)
        config["resources"].append(custom_report)

    if backfill:
        config["resource_defaults"]["endpoint"]["incremental"]["end_value"] = end_date  # type: ignore

    yield from rest_api_resources(config)


def resource(
    name: str,
    endpoint: str,
    dimensions: List[str],
    report_type: ReportType,
) -> EndpointResource:
    return {
        "name": name,
        "columns": build_type_hints(dimensions),
        "merge_key": "day",
        "endpoint": {
            "path": endpoint,
            "params": {
                "report_type": report_type.value,
                "columns": ",".join(dimensions),
            },
        },
    }


def custom_report_from_spec(spec: str) -> EndpointResource:
    parts = spec.split(":")
    if len(parts) != 4:
        raise InvalidCustomReportError()

    _, endpoint, report, dims = parts
    report_type = ReportType(report.strip())
    dimensions = validate_dimensions(dims)
    endpoint = endpoint.strip()

    return resource(
        name="custom_report",
        endpoint=endpoint,
        dimensions=dimensions,
        report_type=report_type,
    )


def validate_dimensions(dimensions: str) -> List[str]:
    dims = [dim.strip() for dim in dimensions.split(",")]

    if "day" not in dims:
        dims.append("day")

    return dims


def exclude(source: List[str], exclude_list: List[str]) -> List[str]:
    excludes = set(exclude_list)
    return [col for col in source if col not in excludes]


def build_type_hints(cols: List[str]) -> dict:
    return {col: TYPE_HINTS[col] for col in cols if col in TYPE_HINTS}


def http_error_handler(resp: Response):
    if not resp.ok:
        raise ClientError(f"HTTP Status {resp.status_code}: {resp.text}")
