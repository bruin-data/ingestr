"""Shopify source helpers"""

from typing import Any, Iterable, Optional

from dlt.common import jsonpath
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import Dict, TDataItems
from dlt.sources.helpers import requests
from pyrate_limiter import Duration, Limiter, Rate
from requests.auth import HTTPBasicAuth


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
        latest_updated_at: Optional[str] = None,
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

        latest_updated_at_object = (
            ensure_pendulum_datetime(latest_updated_at) if latest_updated_at else None
        )

        print("Received latest update", latest_updated_at_object)

        if not params:
            params = {}

        params["limit"] = 100
        params["order_by"] = "updated_datetime:desc"

        while True:
            limiter.try_acquire(f"gorgias-{self.domain}")
            response = requests.get(
                url, params=params, auth=HTTPBasicAuth(self.email, self.api_key)
            )
            response.raise_for_status()

            json = response.json()
            yield [self._convert_datetime_fields(item) for item in json["data"]]
            cursor = json.get("meta", {}).get("next_cursor")
            if not cursor:
                break

            print(f"Fetching next page for {resource} with cursor: {cursor}")
            if latest_updated_at_object:
                last_updated_at = jsonpath.find_values(
                    "data[-1].updated_datetime", json
                )[0]
                if latest_updated_at_object >= ensure_pendulum_datetime(
                    last_updated_at
                ):
                    break

            params["cursor"] = cursor

    def _convert_datetime_fields(self, item: Dict[str, Any]) -> Dict[str, Any]:
        """Convert timestamp fields in the item to pendulum datetime objects

        The item is modified in place.

        Args:
            item: The item to convert

        Returns:
            The same data item (for convenience)
        """
        fields = ["created_datetime", "updated_datetime"]
        for field in fields:
            if field in item:
                item[field] = ensure_pendulum_datetime(item[field])
        return item
