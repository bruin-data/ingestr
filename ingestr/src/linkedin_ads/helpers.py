from urllib.parse import quote

import pendulum
import requests
from dateutil.relativedelta import relativedelta
from dlt.sources.helpers.requests import Client
from pendulum import Date


def retry_on_limit(
    response: requests.Response | None, exception: BaseException | None
) -> bool:
    if response is None:
        return False
    return response.status_code == 429


def create_client() -> requests.Session:
    return Client(
        request_timeout=10.0,
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
    ).session


def flat_structure(items, pivot, time_granularity):
    pivot = pivot.lower()
    for item in items:
        if "pivotValues" in item and item["pivotValues"]:
            item[pivot] = ", ".join(item["pivotValues"])

        if "dateRange" in item:
            start_date = item["dateRange"]["start"]
            start_dt = pendulum.date(
                year=start_date["year"],
                month=start_date["month"],
                day=start_date["day"],
            )

            if time_granularity == "DAILY":
                item["date"] = start_dt
            elif time_granularity == "MONTHLY":
                end_date = item["dateRange"]["end"]
                formatted_end_date = (
                    f"{end_date['year']}-{end_date['month']:02d}-{end_date['day']:02d}"
                )
                item["start_date"] = start_dt
                item["end_date"] = formatted_end_date

        del item["dateRange"]
        del item["pivotValues"]

    return items


def find_intervals(start_date: Date, end_date: Date, time_granularity: str):
    intervals = []

    while start_date <= end_date:
        if time_granularity == "DAILY":
            next_date = min(start_date + relativedelta(months=6), end_date)
        else:
            next_date = min(start_date + relativedelta(years=2), end_date)

        intervals.append((start_date, next_date))

        start_date = next_date + relativedelta(days=1)

    return intervals


def construct_url(
    start: Date,
    end: Date,
    account_ids: list[str],
    metrics: list[str],
    dimension: str,
    time_granularity: str,
):
    date_range = f"(start:(year:{start.year},month:{start.month},day:{start.day})"
    date_range += f",end:(year:{end.year},month:{end.month},day:{end.day})"
    date_range += ")"

    accounts = ",".join(
        [quote(f"urn:li:sponsoredAccount:{account_id}") for account_id in account_ids]
    )
    encoded_accounts = f"List({accounts})"
    metrics_str = ",".join(metrics)

    url = (
        f"https://api.linkedin.com/rest/adAnalytics?"
        f"q=analytics&timeGranularity={time_granularity}&"
        f"dateRange={date_range}&accounts={encoded_accounts}&"
        f"pivot={dimension}&fields={metrics_str}"
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
        self.time_granularity: str = time_granularity
        self.account_ids: list[str] = account_ids
        self.dimension: str = dimension.upper()
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
        result = response.json()
        items = result.get("elements", [])
        print("items::", items)

        items = flat_structure(
            items=items,
            pivot=self.dimension,
            time_granularity=self.time_granularity,
        )
        yield items
