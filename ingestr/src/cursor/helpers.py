"""Cursor source helpers"""

import logging
from typing import Any, Callable, Dict, Iterator, List, Optional

import requests

logger = logging.getLogger(__name__)

REQUEST_TIMEOUT = 30


class CursorAPIError(Exception):
    """Custom exception for Cursor API errors."""

    def __init__(
        self,
        message: str,
        status_code: Optional[int] = None,
        response_text: Optional[str] = None,
    ):
        super().__init__(message)
        self.status_code = status_code
        self.response_text = response_text


class CursorAuthenticationError(CursorAPIError):
    """Exception raised for authentication failures."""

    pass


class CursorClient:
    """Cursor REST API client with API key authentication."""

    def __init__(
        self,
        api_key: str,
        base_url: str = "https://api.cursor.com",
        timeout: int = REQUEST_TIMEOUT,
    ):
        """
        Initialize Cursor client with API key authentication.

        Args:
            api_key: API key for authentication
            base_url: Cursor API base URL
            timeout: Request timeout in seconds
        """
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.api_key = api_key

    def _make_request(
        self,
        endpoint: str,
        method: str = "POST",
        json_data: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        """
        Make HTTP request to Cursor API.

        Args:
            endpoint: API endpoint path
            method: HTTP method (default: POST)
            json_data: JSON data for request body

        Returns:
            JSON response data

        Raises:
            CursorAPIError: If request fails
            CursorAuthenticationError: If authentication fails
        """
        url = f"{self.base_url}/{endpoint.lstrip('/')}"

        try:
            if json_data is not None:
                response = requests.request(
                    method=method,
                    url=url,
                    auth=(self.api_key, ""),
                    timeout=self.timeout,
                    headers={"Content-Type": "application/json"},
                    json=json_data,
                )
            else:
                response = requests.request(
                    method=method,
                    url=url,
                    auth=(self.api_key, ""),
                    timeout=self.timeout,
                    headers={"Content-Type": "application/json"},
                    json={},
                )

            if response.status_code == 401:
                raise CursorAuthenticationError(
                    "Authentication failed. Please check your API key.",
                    status_code=401,
                    response_text=response.text,
                )

            if response.status_code != 200:
                error_message = f"API request failed with status {response.status_code}"

                # Try to parse error response for more details
                try:
                    error_data = response.json()
                    if "message" in error_data:
                        error_message = f"{error_message}: {error_data['message']}"
                except Exception:
                    pass

                # Add specific hint for 400 errors on date-related endpoints
                if response.status_code == 400 and endpoint in [
                    "teams/daily-usage-data",
                    "teams/filtered-usage-events",
                ]:
                    error_message += (
                        "\nNote: Date range cannot exceed 30 days for this endpoint."
                    )

                raise CursorAPIError(
                    error_message,
                    status_code=response.status_code,
                    response_text=response.text,
                )

            return response.json()

        except requests.RequestException as e:
            logger.error(f"Request failed: {str(e)}")
            raise CursorAPIError(f"Request failed: {str(e)}")

    def _paginate(
        self,
        endpoint: str,
        data_key: str,
        base_payload: Optional[Dict[str, Any]] = None,
        page_size: Optional[int] = 100,
        has_next_page_check: Optional[Callable[[Dict[str, Any]], bool]] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Generic pagination helper for API endpoints.

        Args:
            endpoint: API endpoint to call
            data_key: Key in response containing the data array
            base_payload: Base payload to include in each request
            page_size: Number of results per page (default: 100)
            has_next_page_check: Optional function to check if there's a next page from response

        Yields:
            Individual records from the paginated response
        """
        page = 1
        base_payload = base_payload or {}

        while True:
            payload = base_payload.copy()

            if page_size:
                payload["pageSize"] = page_size
                payload["page"] = page

            result = self._make_request(endpoint, json_data=payload)
            data = result.get(data_key, [])

            if not data:
                break

            for record in data:
                yield record

            # If page_size is not set, we get all data in one request
            if not page_size:
                break

            # Custom check for next page if provided
            if has_next_page_check:
                if not has_next_page_check(result):
                    break
            # Default: if we got less data than page_size, we've reached the end
            elif len(data) < page_size:
                break

            page += 1

    def get_team_members(self) -> List[Dict[str, Any]]:
        response = self._make_request("teams/members", method="GET")
        return response.get("teamMembers", [])

    def get_daily_usage_data(
        self,
        start_date: Optional[int] = None,
        end_date: Optional[int] = None,
        page_size: Optional[int] = 100,
    ) -> Iterator[Dict[str, Any]]:
        base_payload = {}
        if start_date is not None:
            base_payload["startDate"] = start_date
        if end_date is not None:
            base_payload["endDate"] = end_date

        yield from self._paginate(
            endpoint="teams/daily-usage-data",
            data_key="data",
            base_payload=base_payload,
            page_size=page_size,
        )

    def get_team_spend(
        self,
        page_size: Optional[int] = 100,
    ) -> Iterator[Dict[str, Any]]:
        def check_has_next_page(response: Dict[str, Any]) -> bool:
            current_page = response.get("currentPage", 1)
            total_pages = response.get("totalPages", 1)
            return current_page < total_pages

        yield from self._paginate(
            endpoint="teams/spend",
            data_key="teamMemberSpend",
            page_size=page_size,
            has_next_page_check=check_has_next_page,
        )

    def get_filtered_usage_events(
        self,
        start_date: Optional[int] = None,
        end_date: Optional[int] = None,
        page_size: Optional[int] = 100,
    ) -> Iterator[Dict[str, Any]]:
        base_payload = {}
        if start_date is not None:
            base_payload["startDate"] = start_date
        if end_date is not None:
            base_payload["endDate"] = end_date

        # Custom check for hasNextPage
        def check_has_next_page(response: Dict[str, Any]) -> bool:
            pagination = response.get("pagination", {})
            return pagination.get("hasNextPage", False)

        yield from self._paginate(
            endpoint="teams/filtered-usage-events",
            data_key="usageEvents",
            base_payload=base_payload,
            page_size=page_size,
            has_next_page_check=check_has_next_page,
        )


def get_client(api_key: str) -> CursorClient:
    return CursorClient(api_key=api_key)
