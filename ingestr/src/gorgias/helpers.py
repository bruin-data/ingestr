"""Gorgias source helpers"""

import time
from typing import Any, Iterable, Optional, Tuple

from dlt.common.pendulum import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import Dict, TDataItems
from dlt.sources.helpers import requests
from pyrate_limiter import Duration, Limiter, Rate
from requests.auth import HTTPBasicAuth

RETRY_COUNT = 10


def get_max_datetime_from_datetime_fields(
    item: Dict[str, Any],
) -> Tuple[str, Optional[pendulum.DateTime]]:
    """Get the maximum datetime from any field that ends with _datetime"""

    max_field_name = None
    max_field_value = None
    for field in item:
        if field.endswith("_datetime") and item[field] is not None:
            dt = ensure_pendulum_datetime(item[field])
            if not max_field_name or dt > max_field_value:
                max_field_name = field
                max_field_value = dt

    return max_field_name, max_field_value


def convert_datetime_fields(item: Dict[str, Any]) -> Dict[str, Any]:
    for field in item:
        if field.endswith("_datetime") and item[field] is not None:
            item[field] = ensure_pendulum_datetime(item[field])

    if "updated_datetime" not in item:
        _, max_datetime = get_max_datetime_from_datetime_fields(item)
        item["updated_datetime"] = max_datetime

    return item


def find_latest_timestamp_from_page(
    items: list[Dict[str, Any]],
) -> Optional[Dict[str, Any]]:
    latest_time = None
    for item in items:
        _, max_field_value = get_max_datetime_from_datetime_fields(item)
        if not latest_time or ensure_pendulum_datetime(max_field_value) > latest_time:
            latest_time = max_field_value

    return latest_time


class GorgiasApi:
    """
    A Gorgias API client that can be used to get pages of data from Gorgias.
    """

    def __init__(
        self,
        domain: str,
        email: str,
        api_key: str,
    ) -> None:
        """
        Args:
            domain: The domain of your Gorgias account.
            email: The email associated with your Gorgias account.
            api_key: The API key for accessing the Gorgias API.
        """
        self.domain = domain
        self.email = email
        self.api_key = api_key

    def get_pages(
        self,
        resource: str,
        params: Optional[Dict[str, Any]] = None,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
    ) -> Iterable[TDataItems]:
        """Get all pages from Gorgias using requests.
        Iterates through all pages and yield each page items.

        Args:
            resource: The resource to get pages for (e.g. products, orders, customers).
            params: Query params to include in the request.

        Yields:
            List of data items from the page
        """
        url = f"https://{self.domain}.gorgias.com/api/{resource}"
        rate = Rate(2, Duration.SECOND)
        limiter = Limiter(rate, raise_when_fail=False)

        start_date_obj = ensure_pendulum_datetime(start_date) if start_date else None

        if not params:
            params = {}

        params["limit"] = 100
        if "order_by" not in params:
            params["order_by"] = "updated_datetime:desc"

        while True:
            limiter.try_acquire(f"gorgias-{self.domain}")

            # this is to retry a back-off if we get a 429
            for i in range(RETRY_COUNT):
                try:
                    response = requests.get(
                        url, params=params, auth=HTTPBasicAuth(self.email, self.api_key)
                    )
                except Exception as e:
                    retry_after = int(response.headers.get("Retry-After", 10))
                    print(
                        f"Got an error from Gorgias API, retrying after {retry_after} seconds",
                        e,
                    )
                    time.sleep(retry_after)
                    continue

                break

            if len(response.json()["data"]) == 0:
                break

            json = response.json()

            items = self.__filter_items_in_range(json["data"], start_date, end_date)
            if len(items) > 0:
                yield items

            # if there is no cursor, yield the items first and then break the loop
            cursor = json.get("meta", {}).get("next_cursor")
            params["cursor"] = cursor
            if not cursor:
                break

            if start_date_obj:
                max_datetime = find_latest_timestamp_from_page(json["data"])
                if start_date_obj > ensure_pendulum_datetime(max_datetime):
                    break

    def __filter_items_in_range(
        self,
        items: list[Dict[str, Any]],
        start_date: Optional[str],
        end_date: Optional[str],
    ) -> list[Dict[str, Any]]:
        start_date_obj = ensure_pendulum_datetime(start_date) if start_date else None
        end_date_obj = ensure_pendulum_datetime(end_date) if end_date else None

        filtered = []
        for item in items:
            converted_item = convert_datetime_fields(item)
            if start_date_obj and item["updated_datetime"] < start_date_obj:
                continue
            if end_date_obj and item["updated_datetime"] > end_date_obj:
                continue
            filtered.append(converted_item)

        return filtered
