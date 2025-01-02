from urllib.parse import quote

import requests
from dateutil.relativedelta import relativedelta
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


def find_intervals(current_date, end_date, time_granularity):
    intervals = []
    print("current_date", current_date)
    print("end_date", end_date)
    print("time_granularity", time_granularity)
    while current_date <= end_date:
        if time_granularity == "DAILY":
            next_date = min(current_date + relativedelta(months=6), end_date)
        else:  # MONTHLY
            # For monthly data, move forward 2 years
            next_date = min(current_date + relativedelta(years=2), end_date)

        intervals.append((current_date, next_date))

        # Start next interval from the next day
        current_date = next_date + relativedelta(days=1)

    return intervals


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
        self.metrics: str = metrics
        self.headers = {
            "Authorization": f"Bearer {access_token}",
            "Linkedin-Version": "202411",
            "X-Restli-Protocol-Version": "2.0.0",
        }

    def construct_url(self, start, end):
        date_range = f"(start:(year:{start.year},month:{start.month},day:{start.day})"
        date_range += f",end:(year:{end.year},month:{end.month},day:{end.day})"
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

    def fetch_pages(self, start, end):
        client = create_client()
        url = self.construct_url(start, end)
        # base_url = "https://api.linkedin.com"
        # while url:
        response = client.get(url=url, headers=self.headers)
        result = response.json()
        items = result.get("elements", [])

        if not items:
            raise ValueError("No items found")

        items = flat_structure(
            items=items,
            pivot=self.dimension,
            time_granularity=self.time_granularity,
        )
        print("items::", items)
        yield items

        # next_link = None
        # for link in result.get("paging", {}).get("links", []):
        #     if link.get("rel") == "next":
        #         next_link = link.get("href")
        #         break
        # print(next_link)
        # if not next_link:
        #     break
        # url = base_url + next_link
