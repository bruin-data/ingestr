from typing import Optional, Sequence

import dlt
import pendulum
from dlt.sources import DltResource

from .adjust_helpers import DEFAULT_DIMENSIONS, DEFAULT_METRICS, AdjustAPI

REQUIRED_CUSTOM_DIMENSIONS = [
    "hour",
    "day",
    "week",
    "month",
    "quarter",
    "year",
]
KNOWN_TYPE_HINTS = {
    "hour": {"data_type": "timestamp"},
    "day": {"data_type": "date"},
    "week": {"data_type": "text"},
    "month": {"data_type": "text"},
    "quarter": {"data_type": "text"},
    "year": {"data_type": "text"},
    "campaign": {"data_type": "text"},
    "adgroup": {"data_type": "text"},
    "creative": {"data_type": "text"},
    # metrics
    "installs": {"data_type": "bigint"},
    "clicks": {"data_type": "bigint"},
    "cost": {"data_type": "decimal"},
    "network_cost": {"data_type": "decimal"},
    "impressions": {"data_type": "bigint"},
    "ad_revenue": {"data_type": "decimal"},
    "all_revenue": {"data_type": "decimal"},
}


@dlt.source(max_table_nesting=0)
def adjust_source(
    start_date: pendulum.DateTime,
    end_date: pendulum.DateTime,
    api_key: str,
    dimensions: Optional[list[str]] = None,
    metrics: Optional[list[str]] = None,
    merge_key: Optional[str] = None,
    filters: Optional[dict] = None,
) -> Sequence[DltResource]:
    @dlt.resource(write_disposition="merge", merge_key="day")
    def campaigns():
        adjust_api = AdjustAPI(api_key=api_key)
        yield from adjust_api.fetch_report_data(
            start_date=start_date,
            end_date=end_date,
            dimensions=DEFAULT_DIMENSIONS,
            metrics=DEFAULT_METRICS,
            filters=filters,
        )

    @dlt.resource(write_disposition="replace", primary_key="id")
    def events():
        adjust_api = AdjustAPI(api_key=api_key)
        yield adjust_api.fetch_events()

    @dlt.resource(write_disposition="merge", merge_key="day")
    def creatives():
        adjust_api = AdjustAPI(api_key=api_key)
        yield from adjust_api.fetch_report_data(
            start_date=start_date,
            end_date=end_date,
            dimensions=DEFAULT_DIMENSIONS + ["adgroup", "creative"],
            metrics=DEFAULT_METRICS,
            filters=filters,
        )

    if not dimensions:
        return campaigns, creatives, events

    merge_key = merge_key
    type_hints = {}
    for dimension in REQUIRED_CUSTOM_DIMENSIONS:
        if dimension in dimensions:
            merge_key = dimension
            break

    for dimension in dimensions:
        if dimension in KNOWN_TYPE_HINTS:
            type_hints[dimension] = KNOWN_TYPE_HINTS[dimension]
    for metric in metrics:
        if metric in KNOWN_TYPE_HINTS:
            type_hints[metric] = KNOWN_TYPE_HINTS[metric]

    @dlt.resource(
        write_disposition={"disposition": "merge", "strategy": "delete-insert"},
        merge_key=merge_key,
        primary_key=dimensions,
        columns=type_hints,
    )
    def custom():
        adjust_api = AdjustAPI(api_key=api_key)
        yield from adjust_api.fetch_report_data(
            start_date=start_date,
            end_date=end_date,
            dimensions=dimensions,
            metrics=metrics,
            filters=filters,
        )

    return campaigns, creatives, custom, events
