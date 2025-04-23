from urllib.parse import quote

import pendulum
import requests
from dlt.sources.helpers.requests import Client
from pendulum import Date

from .dimension_time_enum import Dimension, TimeGranularity


def retry_on_limit(
    response: requests.Response | None, exception: BaseException | None
) -> bool:
    if response is None:
        return False
    return response.status_code == 429


def create_client() -> requests.Session:
    return Client(
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
    ).session


def flat_structure(items, pivot: Dimension, time_granularity: TimeGranularity):
    for item in items:
        if "pivotValues" in item:
            if len(item["pivotValues"]) > 1:
                item[pivot.value.lower()] = item["pivotValues"]
            else:
                item[pivot.value.lower()] = item["pivotValues"][0]
        if "dateRange" in item:
            start_date = item["dateRange"]["start"]
            start_dt = pendulum.date(
                year=start_date["year"],
                month=start_date["month"],
                day=start_date["day"],
            )
            if time_granularity == TimeGranularity.daily:
                item["date"] = start_dt
            else:
                end_date = item["dateRange"]["end"]
                end_dt = pendulum.date(
                    year=end_date["year"],
                    month=end_date["month"],
                    day=end_date["day"],
                )
                item["start_date"] = start_dt
                item["end_date"] = end_dt

        del item["dateRange"]
        del item["pivotValues"]

    return items


def find_intervals(start_date: Date, end_date: Date, time_granularity: TimeGranularity):
    intervals = []

    if start_date > end_date:
        raise ValueError("Start date must be less than end date")

    while start_date <= end_date:
        if time_granularity == TimeGranularity.daily:
            next_date = min(start_date.add(months=6), end_date)
        else:
            next_date = min(start_date.add(years=2), end_date)

        intervals.append((start_date, next_date))

        start_date = next_date.add(days=1)

    return intervals


def construct_url(
    start: Date,
    end: Date,
    account_ids: list[str],
    metrics: list[str],
    dimension: Dimension,
    time_granularity: TimeGranularity,
):
    date_range = f"(start:(year:{start.year},month:{start.month},day:{start.day})"
    date_range += f",end:(year:{end.year},month:{end.month},day:{end.day}))"
    accounts = ",".join(
        [quote(f"urn:li:sponsoredAccount:{account_id}") for account_id in account_ids]
    )
    encoded_accounts = f"List({accounts})"
    dimension_str = dimension.value.upper()
    time_granularity_str = time_granularity.value
    metrics_str = ",".join([metric for metric in metrics])

    url = (
        f"https://api.linkedin.com/rest/adAnalytics?"
        f"q=analytics&timeGranularity={time_granularity_str}&"
        f"dateRange={date_range}&accounts={encoded_accounts}&"
        f"pivot={dimension_str}&fields={metrics_str}"
    )

    return url


class LinkedInAdsAPI:
    def __init__(
        self,
        access_token,
        time_granularity,
        account_ids,
        dimension,
        metrics,
    ):
        self.time_granularity: TimeGranularity = time_granularity
        self.account_ids: list[str] = account_ids
        self.dimension: Dimension = dimension
        self.metrics: list[str] = metrics
        self.headers = {
            "Authorization": f"Bearer {access_token}",
            "Linkedin-Version": "202411",
            "X-Restli-Protocol-Version": "2.0.0",
        }

    def fetch_pages(self, start: Date, end: Date):
        client = create_client()
        url = construct_url(
            start=start,
            end=end,
            account_ids=self.account_ids,
            metrics=self.metrics,
            dimension=self.dimension,
            time_granularity=self.time_granularity,
        )
        response = client.get(url=url, headers=self.headers)

        if response.status_code != 200:
            error_data = response.json()
            raise ValueError(f"LinkedIn API Error: {error_data.get('message')}")

        result = response.json()
        items = result.get("elements", [])
        yield flat_structure(
            items=items,
            pivot=self.dimension,
            time_granularity=self.time_granularity,
        )
