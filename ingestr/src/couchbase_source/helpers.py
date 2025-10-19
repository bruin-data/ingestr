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
    ) -> Dict[str, Any]:
        """
        Make HTTP request to Couchbase API.

        Args:
            endpoint: API endpoint path
            method: HTTP method (GET, POST, etc.)
            params: Query parameters
            data: Request body data

        Returns:
            JSON response data
        """
        url = urljoin(self.base_url + "/", endpoint.lstrip("/"))

        response = requests.request(
            method=method,
            url=url,
            headers=self.headers,
            params=params,
            json=data,
            auth=(self.username, self.password),
            timeout=self.timeout,
            verify=False,
        )

        response.raise_for_status()
        return response.json()

    def get_pools_default(self) -> Dict[str, Any]:
        """
        Get default pool information including rebalance status, tasks, and bucket info.

        Returns:
            Default pool information
        """
        logger.info("Fetching pools/default information")
        return self._make_request("/pools/default")

    def get_buckets(self) -> List[Dict[str, Any]]:
        """
        Get all buckets defined on the cluster.

        Returns:
            List of all buckets
        """
        logger.info("Fetching all buckets")
        response = self._make_request("/pools/default/buckets")
        if isinstance(response, list):
            return response
        return [response]

    def get_bucket(self, bucket_name: str) -> Dict[str, Any]:
        """
        Get detailed information for a specific bucket including streaming URI.

        Args:
            bucket_name: Name of the bucket

        Returns:
            Bucket information with streaming URI
        """
        logger.info(f"Fetching bucket details: {bucket_name}")
        return self._make_request(f"/pools/default/buckets/{bucket_name}")

    def get_bucket_scopes(self, bucket_name: str) -> Dict[str, Any]:
        """
        Get scopes and collections for a specific bucket.

        Args:
            bucket_name: Name of the bucket

        Returns:
            Scopes and collections information
        """
        logger.info(f"Fetching scopes for bucket: {bucket_name}")
        return self._make_request(f"/pools/default/buckets/{bucket_name}/scopes/")


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
