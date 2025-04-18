from typing import Iterable

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource

from ingestr.src.appsflyer.client import AppsflyerClient

DIMENSION_RESPONSE_MAPPING = {
    "c": "campaign",
    "af_adset_id": "adset_id",
    "af_adset": "adset",
    "af_ad_id": "ad_id",
}
HINTS = {
    "app_id": {
        "data_type": "text",
        "nullable": False,
    },
    "campaign": {
        "data_type": "text",
        "nullable": False,
    },
    "geo": {
        "data_type": "text",
        "nullable": False,
    },
    "cost": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": False,
    },
    "clicks": {
        "data_type": "bigint",
        "nullable": False,
    },
    "impressions": {
        "data_type": "bigint",
        "nullable": False,
    },
    "average_ecpi": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": False,
    },
    "installs": {
        "data_type": "bigint",
        "nullable": False,
    },
    "retention_day_7": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": False,
    },
    "retention_day_14": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": False,
    },
    "cohort_day_1_revenue_per_user": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "cohort_day_1_total_revenue_per_user": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "cohort_day_3_revenue_per_user": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "cohort_day_3_total_revenue_per_user": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "cohort_day_7_revenue_per_user": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "cohort_day_7_total_revenue_per_user": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "cohort_day_14_revenue_per_user": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "cohort_day_14_total_revenue_per_user": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "cohort_day_21_revenue_per_user": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "cohort_day_21_total_revenue_per_user": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "install_time": {
        "data_type": "date",
        "nullable": False,
    },
    "loyal_users": {
        "data_type": "bigint",
        "nullable": False,
    },
    "revenue": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "roi": {
        "data_type": "decimal",
        "precision": 30,
        "scale": 5,
        "nullable": True,
    },
    "uninstalls": {
        "data_type": "bigint",
        "nullable": True,
    },
}

CAMPAIGNS_DIMENSIONS = ["c", "geo", "app_id", "install_time"]
CAMPAIGNS_METRICS = [
    "average_ecpi",
    "clicks",
    "cohort_day_1_revenue_per_user",
    "cohort_day_1_total_revenue_per_user",
    "cohort_day_14_revenue_per_user",
    "cohort_day_14_total_revenue_per_user",
    "cohort_day_21_revenue_per_user",
    "cohort_day_21_total_revenue_per_user",
    "cohort_day_3_revenue_per_user",
    "cohort_day_3_total_revenue_per_user",
    "cohort_day_7_revenue_per_user",
    "cohort_day_7_total_revenue_per_user",
    "cost",
    "impressions",
    "installs",
    "loyal_users",
    "retention_day_7",
    "revenue",
    "roi",
    "uninstalls",
]

CREATIVES_DIMENSIONS = [
    "c",
    "geo",
    "app_id",
    "install_time",
    "af_adset_id",
    "af_adset",
    "af_ad_id",
]
CREATIVES_METRICS = [
    "impressions",
    "clicks",
    "installs",
    "cost",
    "revenue",
    "average_ecpi",
    "loyal_users",
    "uninstalls",
    "roi",
]


@dlt.source(max_table_nesting=0)
def appsflyer_source(
    api_key: str,
    start_date: str,
    end_date: str,
    dimensions: list[str],
    metrics: list[str],
) -> Iterable[DltResource]:
    client = AppsflyerClient(api_key)

    @dlt.resource(
        write_disposition="merge",
        merge_key="install_time",
        columns=make_hints(CAMPAIGNS_DIMENSIONS, CAMPAIGNS_METRICS),
    )
    def campaigns(
        datetime=dlt.sources.incremental(
            "install_time",
            initial_value=(
                start_date
                if start_date
                else pendulum.today().subtract(days=30).format("YYYY-MM-DD")
            ),
            end_value=end_date,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        end = (
            datetime.end_value
            if datetime.end_value
            else pendulum.now().format("YYYY-MM-DD")
        )

        yield from client._fetch_data(
            from_date=datetime.last_value,
            to_date=end,
            dimensions=CAMPAIGNS_DIMENSIONS,
            metrics=CAMPAIGNS_METRICS,
        )

    @dlt.resource(
        write_disposition="merge",
        merge_key="install_time",
        columns=make_hints(CREATIVES_DIMENSIONS, CREATIVES_METRICS),
    )
    def creatives(
        datetime=dlt.sources.incremental(
            "install_time",
            initial_value=(
                start_date
                if start_date
                else pendulum.today().subtract(days=30).format("YYYY-MM-DD")
            ),
            end_value=end_date,
            range_end="closed",
            range_start="closed",
        ),
    ) -> Iterable[TDataItem]:
        end = (
            datetime.end_value
            if datetime.end_value
            else pendulum.now().format("YYYY-MM-DD")
        )
        yield from client._fetch_data(
            datetime.last_value,
            end,
            dimensions=CREATIVES_DIMENSIONS,
            metrics=CREATIVES_METRICS,
        )

    primary_keys = []
    if "install_time" not in dimensions:
        dimensions.append("install_time")
        primary_keys.append("install_time")

    for dimension in dimensions:
        if dimension in DIMENSION_RESPONSE_MAPPING:
            primary_keys.append(DIMENSION_RESPONSE_MAPPING[dimension])
        else:
            primary_keys.append(dimension)

    @dlt.resource(
        write_disposition="merge",
        primary_key=primary_keys,
        columns=make_hints(dimensions, metrics),
    )
    def custom(
        datetime=dlt.sources.incremental(
            "install_time",
            initial_value=(
                start_date
                if start_date
                else pendulum.today().subtract(days=30).format("YYYY-MM-DD")
            ),
            end_value=end_date,
        ),
    ):
        end = (
            datetime.end_value
            if datetime.end_value
            else pendulum.now().format("YYYY-MM-DD")
        )
        res = client._fetch_data(
            from_date=datetime.last_value,
            to_date=end,
            dimensions=dimensions,
            metrics=metrics,
        )
        yield from res

    return campaigns, creatives, custom


def make_hints(dimensions: list[str], metrics: list[str]):
    campaign_hints = {}
    for dimension in dimensions:
        resp_key = dimension
        if dimension in DIMENSION_RESPONSE_MAPPING:
            resp_key = DIMENSION_RESPONSE_MAPPING[dimension]

        if resp_key in HINTS:
            campaign_hints[resp_key] = HINTS[resp_key]

    for metric in metrics:
        if metric in HINTS:
            campaign_hints[metric] = HINTS[metric]

    return campaign_hints
