"""Facebook ads source settings and constants"""

from typing import Any, Callable, Dict, Iterator, Literal

from dlt.common.schema.typing import TTableSchemaColumns
from facebook_business.adobjects.abstractobject import AbstractObject

TFbMethod = Callable[..., Iterator[AbstractObject]]


DEFAULT_FIELDS = (
    "id",
    "updated_time",
    "created_time",
    "name",
    "status",
    "effective_status",
)

DEFAULT_CAMPAIGN_FIELDS = DEFAULT_FIELDS + (
    "objective",
    "start_time",
    "stop_time",
    "daily_budget",
    "lifetime_budget",
)

DEFAULT_AD_FIELDS = DEFAULT_FIELDS + (
    "adset_id",
    "campaign_id",
    "creative",
    "targeting",
    "tracking_specs",
    "conversion_specs",
)

DEFAULT_ADSET_FIELDS = DEFAULT_FIELDS + (
    "campaign_id",
    "start_time",
    "end_time",
    "daily_budget",
    "lifetime_budget",
    "optimization_goal",
    "promoted_object",
    "billing_event",
    "bid_amount",
    "bid_strategy",
    "targeting",
)

DEFAULT_ADCREATIVE_FIELDS = (
    "id",
    "name",
    "status",
    "thumbnail_url",
    "object_story_spec",
    "effective_object_story_id",
    "call_to_action_type",
    "object_type",
    "template_url",
    "url_tags",
    "instagram_actor_id",
    "product_set_id",
)

DEFAULT_LEAD_FIELDS = (
    "id",
    "created_time",
    "ad_id",
    "ad_name",
    "adset_id",
    "adset_name",
    "campaign_id",
    "campaign_name",
    "form_id",
    "field_data",
)

DEFAULT_INSIGHT_FIELDS = (
    "campaign_id",
    "adset_id",
    "ad_id",
    "date_start",
    "date_stop",
    "reach",
    "impressions",
    "frequency",
    "clicks",
    "unique_clicks",
    "ctr",
    "unique_ctr",
    "cpc",
    "cpm",
    "cpp",
    "spend",
    "actions",
    "action_values",
    "cost_per_action_type",
    "website_ctr",
    "account_currency",
    "ad_click_actions",
    "ad_name",
    "adset_name",
    "campaign_name",
    "country",
    "dma",
    "full_view_impressions",
    "full_view_reach",
    "inline_link_click_ctr",
    "outbound_clicks",
    "reach",
    "social_spend",
    "spend",
    "website_ctr",
)

TInsightsLevels = Literal["account", "campaign", "adset", "ad"]

INSIGHTS_PRIMARY_KEY = ("campaign_id", "adset_id", "ad_id", "date_start")

ALL_STATES = {
    "effective_status": [
        "ACTIVE",
        "PAUSED",
        "DELETED",
        "PENDING_REVIEW",
        "DISAPPROVED",
        "PREAPPROVED",
        "PENDING_BILLING_INFO",
        "CAMPAIGN_PAUSED",
        "ARCHIVED",
        "ADSET_PAUSED",
    ]
}

TInsightsBreakdownOptions = Literal[
    "ads_insights",
    "ads_insights_age_and_gender",
    "ads_insights_country",
    "ads_insights_platform_and_device",
    "ads_insights_region",
    "ads_insights_dma",
    "ads_insights_hourly_advertiser",
]

ALL_ACTION_ATTRIBUTION_WINDOWS = (
    "1d_click",
    "7d_click",
    "28d_click",
    "1d_view",
    "7d_view",
    "28d_view",
)

ALL_ACTION_BREAKDOWNS = ("action_type", "action_target_id", "action_destination")

INSIGHTS_BREAKDOWNS_OPTIONS: Dict[TInsightsBreakdownOptions, Any] = {
    "ads_insights": {"breakdowns": (), "fields": ()},
    "ads_insights_age_and_gender": {
        "breakdowns": ("age", "gender"),
        "fields": ("age", "gender"),
    },
    "ads_insights_country": {"breakdowns": ("country",), "fields": ("country",)},
    "ads_insights_platform_and_device": {
        "breakdowns": ("publisher_platform", "platform_position", "impression_device"),
        "fields": ("publisher_platform", "platform_position", "impression_device"),
    },
    "ads_insights_region": {"breakdowns": ("region",), "fields": ("region",)},
    "ads_insights_dma": {"breakdowns": ("dma",), "fields": ("dma",)},
    "ads_insights_hourly_advertiser": {
        "breakdowns": ("hourly_stats_aggregated_by_advertiser_time_zone",),
        "fields": ("hourly_stats_aggregated_by_advertiser_time_zone",),
    },
}

INSIGHT_FIELDS_TYPES: TTableSchemaColumns = {
    "campaign_id": {"data_type": "bigint"},
    "adset_id": {"data_type": "bigint"},
    "ad_id": {"data_type": "bigint"},
    "date_start": {"data_type": "timestamp"},
    "date_stop": {"data_type": "timestamp"},
    "reach": {"data_type": "bigint"},
    "impressions": {"data_type": "bigint"},
    "frequency": {"data_type": "decimal"},
    "clicks": {"data_type": "bigint"},
    "unique_clicks": {"data_type": "bigint"},
    "ctr": {"data_type": "decimal"},
    "unique_ctr": {"data_type": "decimal"},
    "cpc": {"data_type": "decimal"},
    "cpm": {"data_type": "decimal"},
    "cpp": {"data_type": "decimal"},
    "spend": {"data_type": "decimal"},
}

INVALID_INSIGHTS_FIELDS = [
    "impression_device",
    "publisher_platform",
    "platform_position",
    "age",
    "gender",
    "country",
    "placement",
    "region",
    "dma",
    "hourly_stats_aggregated_by_advertiser_time_zone",
]

FACEBOOK_INSIGHTS_RETENTION_PERIOD = 37  # months
