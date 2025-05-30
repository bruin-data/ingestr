from typing import Optional, Sequence

import dlt
import pendulum
from dlt.sources import DltResource

from .criteo_helpers import (
    DEFAULT_DIMENSIONS,
    DEFAULT_METRICS,
    CriteoAPI,
)

REQUIRED_CUSTOM_DIMENSIONS = [
    "Hour",
    "Day",
    "Week",
    "Month",
    "Year",
]

KNOWN_TYPE_HINTS = {
    # Time dimensions
    "Hour": {"data_type": "timestamp"},
    "Day": {"data_type": "date"},
    "Week": {"data_type": "text"},
    "Month": {"data_type": "text"},
    "Year": {"data_type": "text"},
    # ID dimensions
    "AdsetId": {"data_type": "text"},
    "CampaignId": {"data_type": "text"},
    "AdvertiserId": {"data_type": "text"},
    "CategoryId": {"data_type": "text"},
    "ProductId": {"data_type": "text"},
    # Geo dimensions
    "Country": {"data_type": "text"},
    "Region": {"data_type": "text"},
    "City": {"data_type": "text"},
    # Tech dimensions
    "Device": {"data_type": "text"},
    "Os": {"data_type": "text"},
    "Browser": {"data_type": "text"},
    "Environment": {"data_type": "text"},
    # Metrics - integers
    "Displays": {"data_type": "bigint"},
    "Clicks": {"data_type": "bigint"},
    "PostViewConversions": {"data_type": "bigint"},
    "PostClickConversions": {"data_type": "bigint"},
    # Metrics - decimals
    "AdvertiserCost": {"data_type": "decimal"},
    "Ctr": {"data_type": "decimal"},
    "Cpc": {"data_type": "decimal"},
    "Cpm": {"data_type": "decimal"},
    "SalesPostView": {"data_type": "decimal"},
    "SalesPostClick": {"data_type": "decimal"},
    "Revenue": {"data_type": "decimal"},
    "RevenuePostView": {"data_type": "decimal"},
    "RevenuePostClick": {"data_type": "decimal"},
}


@dlt.source(max_table_nesting=0)
def criteo_source(
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime,
    client_id: str,
    client_secret: str,
    access_token: Optional[str] = None,
    dimensions: Optional[list[str]] = None,
    metrics: Optional[list[str]] = None,
    currency: str = "USD",
    advertiser_ids: Optional[list[str]] = None,
    merge_key: Optional[str] = None,
) -> Sequence[DltResource]:
    """
    Criteo Marketing Solutions API source for campaign statistics

    Args:
        start_date: Start date for the report
        end_date: End date for the report
        client_id: Criteo API client ID
        client_secret: Criteo API client secret
        access_token: Optional access token (if already obtained)
        dimensions: List of dimensions to include in the report
        metrics: List of metrics to include in the report
        currency: Currency for the report (default: USD)
        advertiser_ids: Optional list of advertiser IDs to filter
        merge_key: Optional merge key for custom reports
    """

    # Use default dimensions and metrics if not provided
    final_dimensions = dimensions or DEFAULT_DIMENSIONS
    final_metrics = metrics or DEFAULT_METRICS

    # Validate custom dimensions and metrics
    criteo_api = CriteoAPI(
        client_id=client_id, client_secret=client_secret, access_token=access_token
    )
    criteo_api.validate_dimensions_and_metrics(final_dimensions, final_metrics)

    # Determine merge key from dimensions
    final_merge_key = merge_key
    if not final_merge_key:
        for dimension in REQUIRED_CUSTOM_DIMENSIONS:
            if dimension in final_dimensions:
                final_merge_key = dimension
                break
        if not final_merge_key:
            final_merge_key = "Day"  # Default fallback

    # Build type hints for the custom resource
    type_hints = {}
    for dimension in final_dimensions:
        if dimension in KNOWN_TYPE_HINTS:
            type_hints[dimension] = KNOWN_TYPE_HINTS[dimension]
    for metric in final_metrics:
        if metric in KNOWN_TYPE_HINTS:
            type_hints[metric] = KNOWN_TYPE_HINTS[metric]

    @dlt.resource(  # type: ignore
        name="custom",
        write_disposition={"disposition": "merge", "strategy": "delete-insert"},
        merge_key=final_merge_key,
        primary_key=final_dimensions,
        columns=type_hints,
    )
    def custom():
        """Custom campaign statistics report with user-specified dimensions and metrics"""
        yield from criteo_api.fetch_campaign_statistics(
            start_date=start_date,
            end_date=end_date,
            dimensions=final_dimensions,
            metrics=final_metrics,
            currency=currency,
            advertiser_ids=advertiser_ids,
            timezone="UTC",  # Always use UTC as requested
        )

    return (custom,)
