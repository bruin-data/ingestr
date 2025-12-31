from typing import Iterable
from urllib.parse import quote

import dlt
import pendulum
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from pendulum import Date

from .dimension_time_enum import Dimension, TimeGranularity
from .helpers import LinkedInAdsAnalyticsAPI, LinkedInAdsAPI, find_intervals


@dlt.source(max_table_nesting=0)
def linked_in_ads_analytics_source(
    start_date: Date,
    end_date: Date | None,
    access_token: str,
    account_ids: list[str],
    dimension: Dimension,
    metrics: list[str],
    time_granularity: TimeGranularity,
) -> DltResource:
    if time_granularity == TimeGranularity.daily:
        primary_key = [dimension.value, "date"]
        incremental_loading_param = "date"
    else:
        primary_key = [dimension.value, "start_date", "end_date"]
        incremental_loading_param = "start_date"

    @dlt.resource(write_disposition="merge", primary_key=primary_key)
    def custom_reports(
        dateTime=(
            dlt.sources.incremental(
                incremental_loading_param,
                initial_value=start_date,
                end_value=end_date,
                range_start="closed",
                range_end="closed",
            )
        ),
    ) -> Iterable[TDataItem]:
        linkedin_api = LinkedInAdsAnalyticsAPI(
            access_token=access_token,
            account_ids=account_ids,
            dimension=dimension,
            metrics=metrics,
            time_granularity=time_granularity,
        )

        if dateTime.end_value is None:
            end_date = pendulum.now().date()
        else:
            end_date = dateTime.end_value

        list_of_interval = find_intervals(
            start_date=dateTime.last_value,
            end_date=end_date,
            time_granularity=time_granularity,
        )
        for start, end in list_of_interval:
            yield linkedin_api.fetch_pages(start, end)

    return custom_reports


@dlt.source(max_table_nesting=0)
def linked_in_ads_source(access_token: str) -> list[DltResource]:
    linkedin_api = LinkedInAdsAPI(
        access_token=access_token,
    )

    @dlt.resource(write_disposition="replace", primary_key="id")
    def ad_accounts() -> Iterable[TDataItem]:
        yield from linkedin_api.fetch_pages(
            url="https://api.linkedin.com/rest/adAccounts?q=search"
        )

    @dlt.transformer(
        write_disposition="replace",
        primary_key=["user", "account"],
        data_from=ad_accounts,
    )
    def ad_account_users(ad_accounts) -> Iterable[TDataItem]:
        for ad_account in ad_accounts:
            account_id = ad_account["id"]
            encoded_id = quote(f"urn:li:sponsoredAccount:{account_id}")
            url = f"https://api.linkedin.com/rest/adAccountUsers?q=accounts&accounts=List({encoded_id})"
            for page in linkedin_api.fetch_full(url):
                for item in page:
                    item["account_id"] = account_id

                yield page

    @dlt.transformer(
        write_disposition="replace",
        primary_key="id",
        data_from=ad_accounts,
    )
    def campaign_groups(ad_accounts) -> Iterable[TDataItem]:
        for ad_account in ad_accounts:
            account_id = ad_account["id"]
            url = f"https://api.linkedin.com/rest/adAccounts/{account_id}/adCampaignGroups?q=search"
            for page in linkedin_api.fetch_pages(url):
                for item in page:
                    item["account_id"] = account_id

                yield page

    @dlt.transformer(
        write_disposition="replace",
        primary_key="id",
        data_from=ad_accounts,
    )
    def campaigns(ad_accounts) -> Iterable[TDataItem]:
        for ad_account in ad_accounts:
            account_id = ad_account["id"]
            url = f"https://api.linkedin.com/rest/adAccounts/{account_id}/adCampaigns?q=search"
            for page in linkedin_api.fetch_pages(url):
                for item in page:
                    item["account_id"] = account_id

                yield page

    @dlt.transformer(
        write_disposition="replace",
        primary_key="id",
        data_from=ad_accounts,
    )
    def creatives(ad_accounts) -> Iterable[TDataItem]:
        for ad_account in ad_accounts:
            account_id = ad_account["id"]
            url = f"https://api.linkedin.com/rest/adAccounts/{account_id}/creatives?q=criteria"
            for page in linkedin_api.fetch_pages(url):
                for item in page:
                    item["account_id"] = account_id

                yield page

    
    @dlt.transformer(
        write_disposition="replace",
        primary_key="id",
        data_from=ad_accounts,
    )
    def conversions(ad_accounts) -> Iterable[TDataItem]:
        for ad_account in ad_accounts:
            account_id = ad_account["id"]
            encoded_id = quote(f"urn:li:sponsoredAccount:{account_id}")
            url = f"https://api.linkedin.com/rest/conversions?q=account&account={encoded_id}"
            for page in linkedin_api.fetch_full(url):
                for item in page:
                    item["account_id"] = account_id

                yield page

    return [ad_accounts, ad_account_users, campaign_groups, campaigns, creatives, conversions]
