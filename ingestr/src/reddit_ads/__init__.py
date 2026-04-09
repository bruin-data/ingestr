from typing import Iterable

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from pendulum import Date

from .helpers import (
    BASE_URL,
    LEVEL_ID_FIELDS,
    RedditAdsAPI,
    RedditAdsReportAPI,
)


@dlt.source(max_table_nesting=0)
def reddit_ads_source(
    access_token: str,
    account_ids: list[str],
) -> list[DltResource]:
    reddit_api = RedditAdsAPI(access_token=access_token)

    @dlt.resource(write_disposition="replace", primary_key="id")
    def accounts() -> Iterable[TDataItem]:
        url = f"{BASE_URL}/accounts"
        for page in reddit_api.fetch_pages(url):
            yield page

    @dlt.resource(write_disposition="replace", primary_key="id")
    def campaigns() -> Iterable[TDataItem]:
        for account_id in account_ids:
            url = f"{BASE_URL}/accounts/{account_id}/campaigns"
            for page in reddit_api.fetch_pages(url):
                for item in page:
                    item["account_id"] = account_id
                yield page

    @dlt.resource(write_disposition="replace", primary_key="id")
    def ad_groups() -> Iterable[TDataItem]:
        for account_id in account_ids:
            url = f"{BASE_URL}/accounts/{account_id}/ad_groups"
            for page in reddit_api.fetch_pages(url):
                for item in page:
                    item["account_id"] = account_id
                yield page

    @dlt.resource(write_disposition="replace", primary_key="id")
    def ads() -> Iterable[TDataItem]:
        for account_id in account_ids:
            url = f"{BASE_URL}/accounts/{account_id}/ads"
            for page in reddit_api.fetch_pages(url):
                for item in page:
                    item["account_id"] = account_id
                yield page

    @dlt.resource(write_disposition="replace", primary_key="id")
    def posts() -> Iterable[TDataItem]:
        for account_id in account_ids:
            url = f"{BASE_URL}/accounts/{account_id}/posts"
            for page in reddit_api.fetch_pages(url):
                for item in page:
                    item["account_id"] = account_id
                yield page

    @dlt.resource(write_disposition="replace", primary_key="id")
    def custom_audiences() -> Iterable[TDataItem]:
        for account_id in account_ids:
            url = f"{BASE_URL}/accounts/{account_id}/custom_audiences"
            for page in reddit_api.fetch_pages(url):
                for item in page:
                    item["account_id"] = account_id
                yield page

    @dlt.resource(write_disposition="replace", primary_key="id")
    def saved_audiences() -> Iterable[TDataItem]:
        for account_id in account_ids:
            url = f"{BASE_URL}/accounts/{account_id}/saved_audiences"
            for page in reddit_api.fetch_pages(url):
                for item in page:
                    item["account_id"] = account_id
                yield page

    @dlt.resource(write_disposition="replace", primary_key="id")
    def pixels() -> Iterable[TDataItem]:
        for account_id in account_ids:
            url = f"{BASE_URL}/accounts/{account_id}/pixels"
            for page in reddit_api.fetch_pages(url):
                for item in page:
                    item["account_id"] = account_id
                yield page

    @dlt.resource(write_disposition="replace", primary_key="id")
    def funding_instruments() -> Iterable[TDataItem]:
        for account_id in account_ids:
            url = f"{BASE_URL}/accounts/{account_id}/funding_instruments"
            for page in reddit_api.fetch_pages(url):
                for item in page:
                    item["account_id"] = account_id
                yield page

    return [
        accounts,
        campaigns,
        ad_groups,
        ads,
        posts,
        custom_audiences,
        saved_audiences,
        pixels,
        funding_instruments,
    ]


@dlt.source(max_table_nesting=0)
def reddit_ads_analytics_source(
    access_token: str,
    account_ids: list[str],
    level: str,
    breakdowns: list[str],
    metrics: list[str],
    start_date: Date,
    end_date: Date | None,
) -> DltResource:
    level_id_field = LEVEL_ID_FIELDS.get(level, "account_id")
    primary_key = [level_id_field] + breakdowns

    has_date_breakdown = "date" in breakdowns

    if has_date_breakdown:
        incremental_cursor = "date"
    else:
        incremental_cursor = None

    if incremental_cursor:

        @dlt.resource(write_disposition="merge", primary_key=primary_key)
        def custom_reports(
            dateTime=dlt.sources.incremental(
                incremental_cursor,
                initial_value=start_date,
                end_value=end_date,
                range_start="closed",
                range_end="closed",
            ),
        ) -> Iterable[TDataItem]:
            report_api = RedditAdsReportAPI(
                access_token=access_token,
                account_ids=account_ids,
                level=level,
                breakdowns=breakdowns,
                metrics=metrics,
            )

            actual_end = (
                dateTime.end_value
                if dateTime.end_value is not None
                else pendulum.now().date()
            )

            yield from report_api.fetch_report(
                start_date=dateTime.last_value,
                end_date=actual_end,
            )

    else:

        @dlt.resource(write_disposition="merge", primary_key=primary_key)
        def custom_reports() -> Iterable[TDataItem]:
            report_api = RedditAdsReportAPI(
                access_token=access_token,
                account_ids=account_ids,
                level=level,
                breakdowns=breakdowns,
                metrics=metrics,
            )

            actual_end = end_date if end_date is not None else pendulum.now().date()

            yield from report_api.fetch_report(
                start_date=start_date,
                end_date=actual_end,
            )

    return custom_reports
