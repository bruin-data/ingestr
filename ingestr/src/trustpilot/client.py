"""Simple Trustpilot API client."""

from typing import Any, Dict, Iterable

import pendulum
from dlt.sources.helpers import requests


class TrustpilotClient:
    """Client for the Trustpilot public API."""

    def __init__(self, api_key: str) -> None:
        self.api_key = api_key
        self.base_url = "https://api.trustpilot.com/v1"

    def _get(self, endpoint: str, params: Dict[str, Any]) -> Dict[str, Any]:
        params = dict(params)
        params["apikey"] = self.api_key
        response = requests.get(f"{self.base_url}{endpoint}", params=params)
        response.raise_for_status()
        return response.json()

    def paginated_reviews(
        self,
        business_unit_id: str,
        updated_since: str,
        end_date: str,
        per_page: int = 1000,
    ) -> Iterable[Dict[str, Any]]:
        page = 1
        while True:
            params: Dict[str, Any] = {"perPage": per_page, "page": page}
            if updated_since:
                params["updatedSince"] = updated_since
            data = self._get(f"/business-units/{business_unit_id}/reviews", params)
            reviews = data.get("reviews", data)
            if not reviews:
                break
            for review in reviews:
                end_date_dt = pendulum.parse(end_date)
                review["updated_at"] = review["updatedAt"]
                review_dt = pendulum.parse(review["updated_at"])
                if review_dt > end_date_dt:  # type: ignore
                    continue
                yield review
            if len(reviews) < per_page:
                break
            page += 1
