"""Fundraiseup API Client for handling authentication and paginated requests."""

from typing import Any, Dict, Iterator, Optional

from ingestr.src.http_client import create_client


class FundraiseupClient:
    """Client for interacting with Fundraiseup API v1."""

    def __init__(self, api_key: str):
        """
        Initialize Fundraiseup API client.

        Args:
            api_key: API key for authentication
        """
        self.api_key = api_key
        self.base_url = "https://api.fundraiseup.com/v1"
        # Use shared HTTP client with retry logic for rate limiting
        self.client = create_client(retry_status_codes=[429, 500, 502, 503, 504])

    def get_paginated_data(
        self,
        endpoint: str,
        params: Optional[Dict[str, Any]] = None,
        page_size: int = 100,
    ) -> Iterator[list[Dict[str, Any]]]:
        """
        Fetch paginated data from a Fundraiseup API endpoint using cursor-based pagination.

        Args:
            endpoint: API endpoint path (e.g., "donations")
            params: Additional query parameters
            page_size: Number of items per page (default 100)

        Yields:
            Batches of items from the API
        """
        url = f"{self.base_url}/{endpoint}"
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

        if params is None:
            params = {}

        params["limit"] = page_size
        starting_after = None

        while True:
            # Add cursor for pagination if not first page
            if starting_after:
                params["starting_after"] = starting_after

            response = self.client.get(url=url, headers=headers, params=params)
            response.raise_for_status()

            data = response.json()

            # Handle both list response and object with data array
            if isinstance(data, list):
                items = data
                has_more = len(items) == page_size
            else:
                items = data.get("data", [])
                has_more = data.get("has_more", False)

            if not items:
                break

            yield items

            # Set cursor for next page
            if has_more and items:
                starting_after = items[-1].get("id")
                if not starting_after:
                    break
            else:
                break
