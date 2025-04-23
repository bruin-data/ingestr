from typing import Optional

import pendulum
import requests
from dlt.sources.helpers.requests import Client
from requests.exceptions import HTTPError

DEFAULT_DIMENSIONS = ["campaign", "day", "app", "store_type", "channel", "country"]

DEFAULT_METRICS = [
    "network_cost",
    "all_revenue_total_d0",
    "ad_revenue_total_d0",
    "revenue_total_d0",
    "all_revenue_total_d1",
    "ad_revenue_total_d1",
    "revenue_total_d1",
    "all_revenue_total_d3",
    "ad_revenue_total_d3",
    "revenue_total_d3",
    "all_revenue_total_d7",
    "ad_revenue_total_d7",
    "revenue_total_d7",
    "all_revenue_total_d14",
    "ad_revenue_total_d14",
    "revenue_total_d14",
    "all_revenue_total_d21",
]


def retry_on_limit(response: requests.Response, exception: BaseException) -> bool:
    return response.status_code == 429


class AdjustAPI:
    def __init__(self, api_key):
        self.api_key = api_key
        self.request_client = Client(
            request_timeout=1000,  # Adjust support recommends 1000 seconds of read timeout.
            raise_for_status=False,
            retry_condition=retry_on_limit,
            request_max_attempts=12,
            request_backoff_factor=2,
        ).session

    def fetch_report_data(
        self,
        start_date: pendulum.DateTime,
        end_date: pendulum.DateTime,
        dimensions=DEFAULT_DIMENSIONS,
        metrics=DEFAULT_METRICS,
        filters: Optional[dict] = None,
    ):
        headers = {"Authorization": f"Bearer {self.api_key}"}
        params = {}

        if filters:
            for key, value in filters.items():
                if isinstance(value, list):
                    params[key] = ",".join(value)
                else:
                    params[key] = value

        params["date_period"] = (
            f"{start_date.format('YYYY-MM-DD')}:{end_date.format('YYYY-MM-DD')}"
        )
        params["dimensions"] = ",".join(dimensions)
        params["metrics"] = ",".join(metrics)

        if start_date > end_date:
            raise ValueError(
                f"Invalid date range: Start date ({start_date}) must be earlier than end date ({end_date})."
            )

        response = self.request_client.get(
            "https://automate.adjust.com/reports-service/report",
            headers=headers,
            params=params,
        )
        if response.status_code == 200:
            result = response.json()
            items = result.get("rows", [])
            yield items
        else:
            raise HTTPError(
                f"Request failed with status code: {response.status_code}, {response.text}."
            )

    def fetch_events(self):
        headers = {"Authorization": f"Bearer {self.api_key}"}
        response = self.request_client.get(
            "https://automate.adjust.com/reports-service/events", headers=headers
        )
        if response.status_code == 200:
            result = response.json()
            yield result
        else:
            raise HTTPError(
                f"Request failed with status code: {response.status_code}, {response.text}."
            )


def parse_filters(filters_raw: str) -> dict:
    # Parse filter string like "key1=value1,key2=value2,value3,value4"
    filters = {}
    current_key = None

    for item in filters_raw.split(","):
        if "=" in item:
            # Start of a new key-value pair
            key, value = item.split("=")
            filters[key] = [value]  # Always start with a list
            current_key = key
        elif current_key is not None:
            # Additional value for the current key
            filters[current_key].append(item)

    # Convert single-item lists to simple values
    filters = {k: v[0] if len(v) == 1 else v for k, v in filters.items()}

    return filters
