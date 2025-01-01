from datetime import datetime
from urllib.parse import quote

import requests
from dlt.sources.helpers.requests import Client


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
        # Process pivotValues
        if "pivotValues" in item and item["pivotValues"]:
            item[pivot] = ", ".join(item["pivotValues"])

        # Process dateRange based on time granularity
        if "dateRange" in item:
            start_date = item["dateRange"]["start"]
            formatted_start_date = f"{start_date['year']}-{start_date['month']:02d}-{start_date['day']:02d}"

            if time_granularity == "DAILY":
                item["date"] = formatted_start_date
            elif time_granularity == "MONTHLY":
                end_date = item["dateRange"]["end"]
                formatted_end_date = (
                    f"{end_date['year']}-{end_date['month']:02d}-{end_date['day']:02d}"
                )
                item["start_date"] = formatted_start_date
                item["end_date"] = formatted_end_date
            else:
                raise ValueError(f"Invalid time_granularity: {time_granularity}")

        del item["dateRange"]
        del item["pivotValues"]

    return items


class LinkedInAdsAPI:
    def __init__(
        self,
        access_token,
        time_granularity,
        account_ids,
        dimension,
        metrics,
        interval_start,
        interval_end=None,
    ):
        self.time_granularity: str = time_granularity
        self.account_ids: list[str] = account_ids
        self.dimension: str = dimension.upper()
        self.metrics: str = metrics
        self.interval_start: datetime = interval_start
        self.interval_end: datetime = interval_end
        self.headers = {
            "Authorization": f"Bearer {access_token}",
            "Linkedin-Version": "202411",
            "X-Restli-Protocol-Version": "2.0.0",
        }
        # interval start is compulsory but end is optional
        self.start = self.interval_start
        self.end = self.interval_end

    def construct_url(self):
        date_range = f"(start:(year:{self.start.year},month:{self.start.month},day:{self.start.day})"
        if self.end is not None:
            date_range += (
                f",end:(year:{self.end.year},month:{self.end.month},day:{self.end.day})"
            )
        date_range += ")"

        accounts = ",".join(
            [
                quote(f"urn:li:sponsoredAccount:{account_id}")
                for account_id in self.account_ids
            ]
        )
        encoded_accounts = f"List({accounts})"

        metrics_str = ",".join(self.metrics)

        url = (
            f"https://api.linkedin.com/rest/adAnalytics?"
            f"q=analytics&timeGranularity={self.time_granularity}&"
            f"dateRange={date_range}&accounts={encoded_accounts}&"
            f"pivot={self.dimension}&fields={metrics_str}"
        )
        return url

    def fetch_pages(self):
        client = create_client()
        url = self.construct_url()
        base_url = "https://api.linkedin.com"
        while url:
            response = client.get(url=url, headers=self.headers)
            print(f"Request URL: {response.url}")
            print(f"Response Status Code: {response.status_code}")
            print(f"Response Content: {response.text}")

            result = response.json()
            print("result", result)
            items = result.get("elements", [])

            if not items:
                raise ValueError("No items found")

            flat_structure(
                items=items,
                pivot=self.dimension,
                time_granularity=self.time_granularity,
            )

            yield items

            next_link = None
            for link in result.get("paging", {}).get("links", []):
                if link.get("rel") == "next":
                    next_link = link.get("href")
                    break
            print(next_link)
            if not next_link:
                break
            url = base_url + next_link
