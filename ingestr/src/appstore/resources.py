
from dataclasses import dataclass
from typing import List

@dataclass
class ResourceConfig:
    name: str
    primary_key: List[str]
    columns: dict
    report_name: str

RESOURCES: List[ResourceConfig] = [
    ResourceConfig(
        name="app-downloads-detailed",
        primary_key=[
            "app_apple_identifier",
            "app_name",
            "app_version",
            "campaign",
            "date",
            "device",
            "download_type",
            "page_title",
            "page_type",
            "platform_version",
            "pre_order",
            "source_info",
            "source_type",
            "territory",
        ],
        columns={
            "date": {"data_type": "date"},
            "app_apple_identifier": {"data_type": "bigint"},
            "counts": {"data_type": "bigint"},
            "processing_date": {"data_type": "date"},
        },
        report_name="App Downloads Detailed"
    ),
    ResourceConfig(
        name="app-store-discovery-and-engagement-detailed",
        primary_key=[
            "app_apple_identifier",
            "app_name",
            "campaign",
            "date",
            "device",
            "engagement_type",
            "event",
            "page_title",
            "page_type",
            "platform_version",
            "source_info",
            "source_type",
            "territory",
        ],
        columns={
            "date": {"data_type": "date"},
            "app_apple_identifier": {"data_type": "bigint"},
            "counts": {"data_type": "bigint"},
            "unique_counts": {"data_type": "bigint"},
            "processing_date": {"data_type": "date"},
        },
        report_name="App Store Discovery and Engagement Detailed"
    ),
    ResourceConfig(
        name="app-sessions-detailed",
        primary_key=[
            "date",
            "app_name",
            "app_apple_identifier",
            "app_version",
            "device",
            "platform_version",
            "source_type",
            "source_info",
            "campaign",
            "page_type",
            "page_title",
            "app_download_date",
            "territory",
        ],
        columns={
            "date": {"data_type": "date"},
            "app_apple_identifier": {"data_type": "bigint"},
            "sessions": {"data_type": "bigint"},
            "total_session_duration": {"data_type": "bigint"},
            "unique_devices": {"data_type": "bigint"},
            "processing_date": {"data_type": "date"},
        },
        report_name="App Sessions Detailed"
    ),
    ResourceConfig(
        name="app-store-installation-and-deletion-detailed",
        primary_key=[
            "app_apple_identifier",
            "app_download_date",
            "app_name",
            "app_version",
            "campaign",
            "counts",
            "date",
            "device",
            "download_type",
            "event",
            "page_title",
            "page_type",
            "platform_version",
            "source_info",
            "source_type",
            "territory",
            "unique_devices",
        ],
        columns={
            "date": {"data_type": "date"},
            "app_apple_identifier": {"data_type": "bigint"},
            "counts": {"data_type": "bigint"},
            "unique_devices": {"data_type": "bigint"},
            "app_download_date": {"data_type": "date"},
            "processing_date": {"data_type": "date"},
        },
        report_name="App Store Installation and Deletion Detailed"
    ),
    ResourceConfig(
        name="app-store-purchases-detailed",
        primary_key=[
            "app_apple_identifier",
            "app_download_date",
            "app_name",
            "campaign",
            "content_apple_identifier",
            "content_name",
            "date",
            "device",
            "page_title",
            "page_type",
            "payment_method",
            "platform_version",
            "pre_order",
            "purchase_type",
            "source_info",
            "source_type",
            "territory",
        ],
        columns={
            "date": {"data_type": "date"},
            "app_apple_identifier": {"data_type": "bigint"},
            "app_download_date": {"data_type": "date"},
            "content_apple_identifier": {"data_type": "bigint"},
            "purchases": {"data_type": "bigint"},
            "proceeds_in_usd": {"data_type": "float"},
            "sales_in_usd": {"data_type": "float"},
            "paying_users": {"data_type": "bigint"},
            "processing_date": {"data_type": "date"},
        },
        report_name="App Store Purchases Detailed"
    ),
    ResourceConfig(
        name="app-crashes-expanded",
        primary_key=[
            "app_name",
            "app_version",
            "build",
            "date",
            "device",
            "platform",
            "release_type",
            "territory",
        ],
        columns={
            "date": {"data_type": "date"},
            "processing_date": {"data_type": "date"},
            "app_apple_identifier": {"data_type": "bigint"},
            "count": {"data_type": "bigint"},
            "unique_devices": {"data_type": "bigint"},
        },
        report_name="App Crashes Expanded"
    )
]