"""Primer API client helpers"""

from typing import Any, Dict, List, Optional

from dlt.common.pendulum import pendulum
from dlt.sources.helpers import requests


def build_date_params(
    start_date: Optional[pendulum.DateTime] = None,
    end_date: Optional[pendulum.DateTime] = None,
) -> Dict[str, str]:
    params: Dict[str, str] = {}
    if start_date:
        params["from_date"] = start_date.start_of("day").to_iso8601_string()
    if end_date:
        params["to_date"] = end_date.add(days=1).start_of("day").to_iso8601_string()
    return params


class PrimerApi:
    """
    A Primer API client that handles pagination and data fetching.
    """

    BASE_URL = "https://api.primer.io"

    def __init__(
        self,
        api_key: str,
        api_version: str = "2.4",
    ) -> None:
        self.api_key = api_key
        self.api_version = api_version

    def _get_headers(self) -> Dict[str, str]:
        return {
            "X-API-KEY": self.api_key,
            "X-API-VERSION": self.api_version,
        }

    def list_payment_ids(
        self,
        start_date: Optional[pendulum.DateTime] = None,
        end_date: Optional[pendulum.DateTime] = None,
    ) -> List[str]:
        """List all payment IDs from the payments endpoint."""
        url = f"{self.BASE_URL}/payments"
        params: Dict[str, Any] = build_date_params(start_date, end_date)

        payment_ids: List[str] = []

        while True:
            response = requests.get(url, params=params, headers=self._get_headers())
            response.raise_for_status()

            json_data = response.json()
            data = json_data.get("data", [])

            if len(data) == 0:
                break

            for item in data:
                payment_ids.append(item["id"])

            next_cursor = json_data.get("nextCursor")
            if not next_cursor:
                break

            params["cursor"] = next_cursor

        return payment_ids

    def get_payment(self, payment_id: str) -> Dict[str, Any]:
        """Get payment by ID."""
        url = f"{self.BASE_URL}/payments/{payment_id}"
        response = requests.get(url, headers=self._get_headers())
        response.raise_for_status()
        return response.json()
