"""Couchbase source helpers"""

import logging
import time
from typing import Any, Dict, List, Optional
from urllib.parse import urljoin

import requests

from .settings import MAX_PAGE_SIZE, REBALANCE_RETRY_ENDPOINT, REQUEST_TIMEOUT

logger = logging.getLogger(__name__)


class CouchbaseAPIError(Exception):
    """Custom exception for Couchbase API errors."""

    def __init__(
        self,
        message: str,
        status_code: Optional[int] = None,
        response_text: Optional[str] = None,
    ):
        super().__init__(message)
        self.status_code = status_code
        self.response_text = response_text


class CouchbaseAuthenticationError(CouchbaseAPIError):
    """Exception raised for authentication failures."""

    pass


class CouchbaseRateLimitError(CouchbaseAPIError):
    """Exception raised when rate limit is exceeded."""

    pass


class CouchbaseClient:
    """Couchbase REST API client with authentication support."""

    def __init__(
        self,
        base_url: str,
        username: str,
        password: str,
        timeout: int = REQUEST_TIMEOUT,
    ):
        """
        Initialize Couchbase client with basic auth.

        Args:
            base_url: Couchbase server URL (e.g., http://localhost:8091)
            username: Username for authentication
            password: Password for authentication
            timeout: Request timeout in seconds
        """
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.username = username
        self.password = password

        self.headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
        }

    def _make_request(
        self,
        endpoint: str,
        method: str = "GET",
        params: Optional[Dict[str, Any]] = None,
        data: Optional[Dict[str, Any]] = None,
        max_retries: int = 3,
        backoff_factor: float = 1.0,
    ) -> Dict[str, Any]:
        """
        Make HTTP request to Couchbase API with retry logic.

        Args:
            endpoint: API endpoint path
            method: HTTP method (GET, POST, etc.)
            params: Query parameters
            data: Request body data
            max_retries: Maximum number of retry attempts
            backoff_factor: Factor for exponential backoff

        Returns:
            JSON response data

        Raises:
            CouchbaseAPIError: If request fails after retries
            CouchbaseAuthenticationError: If authentication fails
            CouchbaseRateLimitError: If rate limit is exceeded
        """
        url = urljoin(self.base_url + "/", endpoint.lstrip("/"))

        for attempt in range(max_retries + 1):
            try:
                response = requests.request(
                    method=method,
                    url=url,
                    headers=self.headers,
                    params=params,
                    json=data,
                    auth=(self.username, self.password),
                    timeout=self.timeout,
                    verify=False,  # Disable SSL verification for self-signed certificates
                )

                # Handle different error status codes
                if response.status_code == 401:
                    raise CouchbaseAuthenticationError(
                        "Authentication failed. Please check your username and password.",
                        status_code=response.status_code,
                        response_text=response.text,
                    )
                elif response.status_code == 403:
                    raise CouchbaseAuthenticationError(
                        "Access forbidden. Please check your permissions.",
                        status_code=response.status_code,
                        response_text=response.text,
                    )
                elif response.status_code == 429:
                    # Rate limit exceeded
                    retry_after = int(response.headers.get("Retry-After", 60))
                    if attempt < max_retries:
                        logger.warning(
                            f"Rate limit exceeded. Waiting {retry_after} seconds before retry."
                        )
                        time.sleep(retry_after)
                        continue
                    else:
                        raise CouchbaseRateLimitError(
                            f"Rate limit exceeded after {max_retries} retries.",
                            status_code=response.status_code,
                            response_text=response.text,
                        )
                elif response.status_code >= 500:
                    # Server error - retry with backoff
                    if attempt < max_retries:
                        wait_time = backoff_factor * (2**attempt)
                        logger.warning(
                            f"Server error {response.status_code}. Retrying in {wait_time} seconds."
                        )
                        time.sleep(wait_time)
                        continue
                    else:
                        raise CouchbaseAPIError(
                            f"Server error after {max_retries} retries.",
                            status_code=response.status_code,
                            response_text=response.text,
                        )

                # Raise for other HTTP errors
                response.raise_for_status()

                # Try to parse JSON response
                try:
                    return response.json()
                except ValueError:
                    # If response is not JSON, return empty dict
                    return {"status": "success", "raw_response": response.text}

            except requests.RequestException as e:
                if attempt < max_retries:
                    wait_time = backoff_factor * (2**attempt)
                    logger.warning(
                        f"Request failed: {str(e)}. Retrying in {wait_time} seconds."
                    )
                    time.sleep(wait_time)
                    continue
                else:
                    raise CouchbaseAPIError(
                        f"Request failed after {max_retries} retries: {str(e)}"
                    )

        raise CouchbaseAPIError(f"Request failed after {max_retries} retries")

    def get_pools_default(self) -> Dict[str, Any]:
        """
        Get default pool information including rebalance status, tasks, and bucket info.

        Returns:
            Default pool information
        """
        logger.info("Fetching pools/default information")
        return self._make_request("/pools/default")


def get_client(
    base_url: str, username: str, password: str, timeout: int = REQUEST_TIMEOUT
) -> CouchbaseClient:
    """
    Create and return a Couchbase API client.

    Args:
        base_url: Couchbase server URL
        username: Username for authentication
        password: Password for authentication
        timeout: Request timeout in seconds

    Returns:
        CouchbaseClient instance
    """
    return CouchbaseClient(base_url, username, password, timeout)
