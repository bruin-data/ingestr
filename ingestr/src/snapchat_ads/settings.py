"""Snapchat Ads source settings and constants"""

from typing import Literal

# Valid granularities for stats
TStatsGranularity = Literal["TOTAL", "DAY", "HOUR", "LIFETIME"]

# Valid breakdowns for stats
TStatsBreakdown = Literal["ad", "adsquad", "campaign"]

# Stats primary key - includes all possible identifying fields
# campaign_id is always present
# adsquad_id and ad_id will be NULL when no breakdown is specified
# Time fields identify the time period for the stats
STATS_PRIMARY_KEY = (
    "campaign_id",
    "adsquad_id",
    "ad_id",
    "start_time",
    "end_time",
)

# Default stats fields
DEFAULT_STATS_FIELDS = "impressions,spend"
