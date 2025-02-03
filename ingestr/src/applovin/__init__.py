import dlt
from dlt.sources.rest_api import RESTAPIConfig, rest_api_resources
from enum import Enum
from typing import List, Dict, Set

class ReportType(Enum):
    PUBLISHER = "publisher"
    ADVERTISER = "advertiser"

class InvalidCustomReportError(Exception):
    def __init__(self):
        super().__init__("Custom report should be in the format 'custom:{endpoint}:{report_type}:{dimensions}")

class InvalidDimensionError(Exception):
    def __init__(self, dim: str, report_type: str):
        super().__init__(f"Unknown dimension {dim} for report type {report_type}")

TYPE_HINTS = {
    "ad_type": {"data_type": "text"},
    "application": {"data_type": "text"},
    "application_is_hidden": {"data_type": "bool"},
    "bidding_integration": {"data_type": "text"},
    "clicks": {"data_type": "bigint"},
    "country": {"data_type": "text"},
    "ctr": {"data_type": "double"},
    "day": {"data_type": "date"},
    "device_type": {"data_type": "text"},
    "ecpm": {"data_type": "double"},
    "impressions": {"data_type": "bigint"},
    "package_name": {"data_type": "text"},
    "placement_type": {"data_type": "text"},
    "platform": {"data_type": "text"},
    "revenue": {"data_type": "double"},
    "size": {"data_type": "text"},
    "store_id": {"data_type": "text"},
    "zone": {"data_type": "text"},
    "zone_id": {"data_type": "text"},
}

REPORT_SCHEMA: Dict[ReportType, Dict] = {
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
        "traffic_source"
    ],
}

# NOTE(turtledev): These values are valid columns,
# but often don't produce a value. Find a way to either add
# a default value, or use an alternative strategy to de-duplicate
# OR make them nullable
SKA_REPORT_EXCLUDE = [
    "ad",
    "ad_id",
    "ad_type",
    "average_cpc",
    "campaign_ad_type",
    "clicks",
    "conversions",
    "conversion_rate",
    "creative_set",
    "creative_set_id",
    "ctr",
    "custom_page_id",
    "device_type",
    "first_purchase",
    "impressions",
    "placement_type",
    "sales",
    "size",
    "traffic_source"
]

PROBABILISTIC_REPORT_EXCLUDE = [
    "installs",
    "redownloads",
]

@dlt.source
def applovin_source(
    api_key: str,
    start_date: str,
    end_date: str,
    custom: str,
):
    ska_report_columns = exclude(
        REPORT_SCHEMA[ReportType.ADVERTISER],
        SKA_REPORT_EXCLUDE,
    )

    probabilistic_report_columns = exclude(
        REPORT_SCHEMA[ReportType.ADVERTISER],
        PROBABILISTIC_REPORT_EXCLUDE,
    )

    # validate that start_date & end_date are within the last 45 days
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
                    "end_param": "end",
                    "initial_value": start_date,
                    "end_value": end_date,
                },
                "params": {
                    "format": "json",
                },
                "paginator": "single_page",
            },
        },
        "resources": [
            {
                "name": "publisher_report",
                "primary_key": REPORT_SCHEMA[ReportType.PUBLISHER],
                "columns": build_schema(REPORT_SCHEMA[ReportType.PUBLISHER]),
                "endpoint": {
                    "path": "report",
                    "params": {
                        "report_type": ReportType.PUBLISHER.value,
                        "columns": ",".join(REPORT_SCHEMA[ReportType.PUBLISHER])
                    },
                },
            },
            {
                "name": "advertiser_report",
                "primary_key": REPORT_SCHEMA[ReportType.ADVERTISER],
                "endpoint": {
                    "path": "report",
                    "params": {
                        "report_type": ReportType.ADVERTISER.value,
                        "columns": ",".join(REPORT_SCHEMA[ReportType.ADVERTISER])
                    },
                },
            },
            {
                "name": "advertiser_probabilistic_report",
                "primary_key": probabilistic_report_columns,
                "endpoint": {
                    "path": "probabilisticReport",
                    "params": {
                        "report_type": ReportType.ADVERTISER.value,
                        "columns": ",".join(probabilistic_report_columns)
                    },
                },
            },
            {
                "name": "advertiser_ska_report",
                "primary_key": ska_report_columns,
                "endpoint": {
                    "path": "skaReport",
                    "params": {
                        "report_type": ReportType.ADVERTISER.value,
                        "columns": ",".join(ska_report_columns)
                    },
                },
            },
        ]
    }

    if custom:
        custom_report = custom_report_from_spec(custom)
        config["resources"].append(custom_report)


    yield from rest_api_resources(config)



def custom_report_from_spec(spec: str) -> dict:
    parts = spec.split(":")
    if len(parts) != 4:
        raise InvalidCustomReportError()

    _, endpoint, report_type, dimensions = parts
    report_type = ReportType(report_type.strip())
    dimensions = validate_dimensions(report_type, dimensions)
    endpoint = endpoint.strip()

    return {
        "name": "custom_report",
        "primary_key": dimensions,
        "endpoint": {
            "path": endpoint,
            "params": {
                "report_type": report_type.value,
                "columns": ",".join(dimensions)
            },
        },
    }

def validate_dimensions(report_type: ReportType, dimensions: str) -> List[str]:
    dims = [
        dim.strip() for dim in dimensions.split(",")
    ]

    schema = set(REPORT_SCHEMA[report_type])
    for dim in dims:
        if dim not in schema:
            raise InvalidDimensionError(dim, report_type.value)

    if "day" not in dims:
        dims.append("day")
    
    return dims

def exclude(source: List[str], excludes: List[str]) -> List[str]:
    excludes = set(excludes)
    return [
        col for col in source
        if col not in excludes
    ]

def build_schema(cols: List[str]) -> dict:
    return {
        col: TYPE_HINTS[col] for col in cols
        if col in TYPE_HINTS
    }
    