"""Snapchat Ads source settings and constants"""

from typing import Literal

from dlt.common.schema.typing import TColumnSchema

# Valid granularities for stats (required)
TStatsGranularity = Literal["TOTAL", "DAY", "HOUR", "LIFETIME"]

# Valid breakdowns for stats (object-level breakdown)
TStatsBreakdown = Literal["ad", "adsquad", "campaign"]

# Valid dimensions for stats (insight-level breakdown)
TStatsDimension = Literal["GEO", "DEMO", "INTEREST", "DEVICE"]

# Valid pivots for stats (pivot for insights breakdown)
TStatsPivot = Literal[
    "country",
    "region",
    "dma",
    "gender",
    "age_bucket",
    "interest_category_id",
    "interest_category_name",
    "operating_system",
    "make",
    "model",
]

# Sets for efficient lookup
VALID_GRANULARITIES = {"TOTAL", "DAY", "HOUR", "LIFETIME"}
VALID_BREAKDOWNS = {"ad", "adsquad", "campaign"}
VALID_DIMENSIONS = {"GEO", "DEMO", "INTEREST", "DEVICE"}
VALID_PIVOTS = {
    "country",
    "region",
    "dma",
    "gender",
    "age_bucket",
    "interest_category_id",
    "interest_category_name",
    "operating_system",
    "make",
    "model",
}

# Stats primary key - includes all possible identifying fields
# campaign_id is always present
# adsquad_id and ad_id will be NULL when no breakdown is specified
# Time fields identify the time period for the stats
STATS_PRIMARY_KEY = [
    "campaign_id",
    "adsquad_id",
    "ad_id",
    "start_time",
    "end_time",
]

# Default stats fields
DEFAULT_STATS_FIELDS = "impressions,spend"

# Core metrics column hints for stats resources
# All monetary values are in micro-currency (1.00 = 1,000,000 micro-currency)
# Metrics are finalized 48 hours after the end of the day in the Ad Account's timezone
STATS_METRICS_COLUMNS: dict[str, TColumnSchema] = {
    # Core metrics (available for all Snap Ads)
    "impressions": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Impression Count",
    },
    "swipes": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Swipe-Up Count",
    },
    "view_time_millis": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Deprecated: Use screen_time_millis instead. Total Time Spent on top Snap Ad (milliseconds)",
    },
    "screen_time_millis": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Total Time Spent on top Snap Ad (milliseconds)",
    },
    "quartile_1": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Video Views to 25%",
    },
    "quartile_2": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Video Views to 50%",
    },
    "quartile_3": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Video Views to 75%",
    },
    "view_completion": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Video Views to completion",
    },
    "spend": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Amount Spent (micro-currency: 1.00 = 1,000,000)",
    },
    "coupon_used_local": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Amount Spent via Coupon in the assigned currency of Ad Account (micro-currency)",
    },
    "coupon_used_usd": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Amount Spent via Coupon in USD (micro-currency)",
    },
    "video_views": {
        "data_type": "bigint",
        "nullable": True,
        "description": "Total impressions with at least 2 seconds of consecutive watch time or a swipe up action",
    },
}

# Set of valid metric field names for validation
VALID_METRICS = set(STATS_METRICS_COLUMNS.keys())
