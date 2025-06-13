"""Simple Trustpilot API client."""

from typing import Any, Dict, Iterable, Optional

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
        per_page: int = 100,
        updated_since: Optional[str] = None,
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
                yield review
            if len(reviews) < per_page:
                break
            page += 1
