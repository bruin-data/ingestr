"""Snapchat Ads source settings and constants"""

from typing import Literal

# Valid granularities for stats
TStatsGranularity = Literal["TOTAL", "DAY", "HOUR", "LIFETIME"]

# Valid breakdowns for stats
TStatsBreakdown = Literal["ad", "adsquad", "campaign"]

# Valid dimensions for stats
TStatsDimension = Literal["GEO", "DEMO", "INTEREST", "DEVICE"]

# Valid pivots for stats
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

# Stats primary key
STATS_PRIMARY_KEY = ("id", "start_time")

# Default stats fields
DEFAULT_STATS_FIELDS = "impressions,spend"
