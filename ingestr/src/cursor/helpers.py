"""Cursor source helpers"""

import logging
from typing import Any, Dict, List, Optional

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
            kwargs = {
                "method": method,
                "url": url,
                "auth": (self.api_key, ""),
                "timeout": self.timeout,
                "headers": {"Content-Type": "application/json"},
            }

            if json_data is not None:
                kwargs["json"] = json_data
            else:
                kwargs["json"] = {}

            response = requests.request(**kwargs)

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
                except:
                    pass

                # Add specific hint for 400 errors on date-related endpoints
                if response.status_code == 400 and endpoint in ["teams/daily-usage-data", "teams/filtered-usage-events"]:
                    error_message += "\nNote: Date range cannot exceed 30 days for this endpoint."

                raise CursorAPIError(
                    error_message,
                    status_code=response.status_code,
                    response_text=response.text,
                )

            return response.json()

        except requests.RequestException as e:
            logger.error(f"Request failed: {str(e)}")
            raise CursorAPIError(f"Request failed: {str(e)}")

    def get_team_members(self) -> List[Dict[str, Any]]:
        """
        Fetch team members from Cursor API.

        Returns:
            List of team member dictionaries

        Example response:
            {
                "teamMembers": [
                    {
                        "name": "Alex",
                        "email": "developer@company.com",
                        "role": "member"
                    },
                    {
                        "name": "Sam",
                        "email": "admin@company.com",
                        "role": "owner"
                    }
                ]
            }
        """
        response = self._make_request("teams/members", method="GET")
        return response.get("teamMembers", [])

    def get_daily_usage_data(
        self,
        start_date: Optional[int] = None,
        end_date: Optional[int] = None,
        page_size: Optional[int] = 100,
    ) -> List[Dict[str, Any]]:
        """
        Fetch daily usage data from Cursor API with pagination support.

        Args:
            start_date: Start date in epoch milliseconds (optional)
            end_date: End date in epoch milliseconds (optional)
            page_size: Number of results per page (default: 100)

        Yields:
            Daily usage data dictionaries

        Note:
            Date range cannot exceed 30 days when specified.
        """
        page = 1

        while True:
            payload = {}

            if start_date is not None:
                payload["startDate"] = start_date
            if end_date is not None:
                payload["endDate"] = end_date

            if page_size:
                payload["pageSize"] = page_size
                payload["page"] = page

            result = self._make_request("teams/daily-usage-data", json_data=payload)
            data = result.get("data", [])

            if not data:
                break

            for record in data:
                yield record

            # If page_size is not set, we get all data in one request
            if not page_size:
                break

            # If we got less data than page_size, we've reached the end
            if len(data) < page_size:
                break

            page += 1

    def get_team_spend(
        self,
        page_size: Optional[int] = 100,
    ) -> List[Dict[str, Any]]:
        """
        Fetch team spending data from Cursor API with pagination support.

        Args:
            page_size: Number of results per page (default: 100)

        Yields:
            Team spending data dictionaries
        """
        page = 1

        while True:
            payload = {}

            if page_size:
                payload["pageSize"] = page_size
                payload["page"] = page

            result = self._make_request("teams/spend", json_data=payload)
            data = result.get("teamMemberSpend", [])
            total_pages = result.get("totalPages", 1)

            if not data:
                break

            for record in data:
                yield record

            # If we've reached the last page, stop
            if page >= total_pages:
                break

            page += 1

    def get_filtered_usage_events(
        self,
        start_date: Optional[int] = None,
        end_date: Optional[int] = None,
        page_size: Optional[int] = 100,
    ) -> List[Dict[str, Any]]:
        """
        Fetch filtered usage events from Cursor API with pagination support.

        Args:
            start_date: Start date in epoch milliseconds (optional)
            end_date: End date in epoch milliseconds (optional)
            page_size: Number of results per page (default: 100)

        Yields:
            Usage event dictionaries
        """
        page = 1

        while True:
            payload = {}

            if start_date is not None:
                payload["startDate"] = start_date
            if end_date is not None:
                payload["endDate"] = end_date

            if page_size:
                payload["pageSize"] = page_size
                payload["page"] = page

            result = self._make_request("teams/filtered-usage-events", json_data=payload)
            data = result.get("usageEvents", [])
            pagination = result.get("pagination", {})
            has_next_page = pagination.get("hasNextPage", False)

            if not data:
                break

            for record in data:
                yield record

            # If there's no next page, stop
            if not has_next_page:
                break

            page += 1


def get_client(api_key: str) -> CursorClient:
    """
    Get a CursorClient instance.

    Args:
        api_key: API key for authentication

    Returns:
        CursorClient instance
    """
    return CursorClient(api_key=api_key)
