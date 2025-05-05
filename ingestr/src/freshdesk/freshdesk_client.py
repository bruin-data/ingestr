"""Freshdesk Client for making authenticated requests"""

import logging
import time
from typing import Any, Dict, Iterable, Optional

from dlt.common.typing import TDataItem
from dlt.sources.helpers import requests


class FreshdeskClient:
    """
    Client for making authenticated requests to the Freshdesk API. It incorporates API requests with
    rate limit and pagination.

    Attributes:
        api_key (str): The API key used for authenticating requests to the Freshdesk API.
        domain (str): The Freshdesk domain specific to the user, used in constructing the base URL.
        base_url (str): The base URL constructed from the domain, targeting the Freshdesk API v2.
    """

    def __init__(self, api_key: str, domain: str):
        # Initialize the FreshdeskClient instance with API key and domain.
        # The API key is used for authentication with the Freshdesk API.
        # The domain specifies the unique Freshdesk domain of the user.

        # Store the API key provided during initialization.
        self.api_key = api_key
        # Store the Freshdesk domain provided during initialization.
        self.domain = domain

        # Construct the base URL for the API requests.
        # This URL is formed by appending the domain to the standard Freshdesk API base URL format.
        # All API requests will use this base URL as their starting point.
        self.base_url = f"https://{domain}.freshdesk.com/api/v2"

    def _request_with_rate_limit(self, url: str, **kwargs: Any) -> requests.Response:
        """
        Handles rate limits in HTTP requests and ensures
        that the client doesn't exceed the limit set by the server.
        """

        while True:
            try:
                response = requests.get(url, **kwargs, auth=(self.api_key, "X"))
                response.raise_for_status()

                return response
            except requests.HTTPError as e:
                if e.response.status_code == 429:
                    # Get the 'Retry-After' header to know how long to wait
                    # Fallback to 60 seconds if header is missing
                    seconds_to_wait = int(e.response.headers.get("Retry-After", 60))
                    # Log a warning message
                    logging.warning(
                        "Rate limited. Waiting to retry after: %s secs", seconds_to_wait
                    )

                    # Wait for the specified number of seconds before retrying
                    time.sleep(seconds_to_wait)
                else:
                    # If the error is not a rate limit (429), raise the exception to be
                    # handled elsewhere or stop execution
                    raise

    def paginated_response(
        self,
        endpoint: str,
        per_page: int,
        updated_at: Optional[str] = None,
    ) -> Iterable[TDataItem]:
        """
        Fetches a paginated response from a specified endpoint.

        This method will continuously fetch data from the given endpoint,
        page by page, until no more data is available or until it reaches data
        updated at the specified timestamp.
        """
        page = 1
        while True:
            # Construct the URL for the specific endpoint
            url = f"{self.base_url}/{endpoint}"

            params: Dict[str, Any] = {"per_page": per_page, "page": page}

            # Implement date range splitting logic here, if applicable
            if endpoint in ["tickets", "contacts"]:
                param_key = (
                    "updated_since" if endpoint == "tickets" else "_updated_since"
                )
                if updated_at:
                    params[param_key] = updated_at

            # Handle requests with rate-limiting
            # A maximum of 300 pages (30000 tickets) will be returned.
            response = self._request_with_rate_limit(url, params=params)
            data = response.json()

            if not data:
                break  # Stop if no data or max page limit reached
            yield data
            page += 1
