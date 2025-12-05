# Copyright 2022-2025 ScaleVector
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Jira source helpers"""

import base64
import logging
import time
from typing import Any, Dict, Iterator, Optional
from urllib.parse import urljoin

import requests

from .settings import API_BASE_PATH, DEFAULT_PAGE_SIZE, MAX_PAGE_SIZE, REQUEST_TIMEOUT

logger = logging.getLogger(__name__)


class JiraAPIError(Exception):
    """Custom exception for Jira API errors."""

    def __init__(
        self,
        message: str,
        status_code: Optional[int] = None,
        response_text: Optional[str] = None,
    ):
        super().__init__(message)
        self.status_code = status_code
        self.response_text = response_text


class JiraAuthenticationError(JiraAPIError):
    """Exception raised for authentication failures."""

    pass


class JiraRateLimitError(JiraAPIError):
    """Exception raised when rate limit is exceeded."""

    pass


class JiraClient:
    """Jira REST API client with authentication and pagination support."""

    def __init__(
        self, base_url: str, email: str, api_token: str, timeout: int = REQUEST_TIMEOUT
    ):
        """
        Initialize Jira client with basic auth.

        Args:
            base_url: Jira instance URL (e.g., https://your-domain.atlassian.net)
            email: User email for authentication
            api_token: API token for authentication
            timeout: Request timeout in seconds
        """
        self.base_url = base_url.rstrip("/")
        self.api_url = urljoin(self.base_url, API_BASE_PATH)
        self.timeout = timeout

        # Create basic auth header
        credentials = f"{email}:{api_token}"
        encoded_credentials = base64.b64encode(credentials.encode()).decode()

        self.headers = {
            "Authorization": f"Basic {encoded_credentials}",
            "Accept": "application/json",
            "Content-Type": "application/json",
        }

    def _make_request(
        self,
        endpoint: str,
        params: Optional[Dict[str, Any]] = None,
        method: str = "GET",
        max_retries: int = 3,
        backoff_factor: float = 1.0,
    ) -> Dict[str, Any]:
        """
        Make HTTP request to Jira API with retry logic.

        Args:
            endpoint: API endpoint path
            params: Query parameters
            method: HTTP method
            max_retries: Maximum number of retry attempts
            backoff_factor: Factor for exponential backoff

        Returns:
            JSON response data

        Raises:
            JiraAPIError: If request fails after retries
            JiraAuthenticationError: If authentication fails
            JiraRateLimitError: If rate limit is exceeded
        """
        url = urljoin(self.api_url + "/", endpoint.lstrip("/"))

        for attempt in range(max_retries + 1):
            try:
                response = requests.request(
                    method=method,
                    url=url,
                    headers=self.headers,
                    params=params,
                    timeout=self.timeout,
                )

                # Handle different error status codes
                if response.status_code == 401:
                    raise JiraAuthenticationError(
                        "Authentication failed. Please check your email and API token.",
                        status_code=response.status_code,
                        response_text=response.text,
                    )
                elif response.status_code == 403:
                    raise JiraAuthenticationError(
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
                        time.sleep(retry_after)  # type: ignore
                        continue
                    else:
                        raise JiraRateLimitError(
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
                        time.sleep(wait_time)  # type: ignore
                        continue
                    else:
                        raise JiraAPIError(
                            f"Server error after {max_retries} retries.",
                            status_code=response.status_code,
                            response_text=response.text,
                        )

                # Raise for other HTTP errors
                response.raise_for_status()

                # Try to parse JSON response
                try:
                    return response.json()
                except ValueError as e:
                    raise JiraAPIError(
                        f"Invalid JSON response: {str(e)}",
                        status_code=response.status_code,
                        response_text=response.text,
                    )

            except requests.RequestException as e:
                if attempt < max_retries:
                    wait_time = backoff_factor * (2**attempt)
                    logger.warning(
                        f"Request failed: {str(e)}. Retrying in {wait_time} seconds."
                    )
                    time.sleep(wait_time)  # type: ignore
                    continue
                else:
                    raise JiraAPIError(
                        f"Request failed after {max_retries} retries: {str(e)}"
                    )

        raise JiraAPIError(f"Request failed after {max_retries} retries")

    def get_paginated(
        self,
        endpoint: str,
        params: Optional[Dict[str, Any]] = None,
        page_size: int = DEFAULT_PAGE_SIZE,
        max_results: Optional[int] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Get paginated results from Jira API with error handling.

        Args:
            endpoint: API endpoint path
            params: Query parameters
            page_size: Number of items per page
            max_results: Maximum total results to return

        Yields:
            Individual items from paginated response

        Raises:
            JiraAPIError: If pagination fails
        """
        if params is None:
            params = {}

        # Validate page size
        page_size = min(max(1, page_size), MAX_PAGE_SIZE)
        params["maxResults"] = page_size
        params["startAt"] = 0

        total_returned = 0
        consecutive_empty_pages = 0
        max_empty_pages = 3

        while True:
            try:
                response = self._make_request(endpoint, params)

                # Handle different response structures
                if "values" in response:
                    items = response["values"]
                    total = response.get("total", len(items))
                    is_last = response.get("isLast", False)
                elif "issues" in response:
                    items = response["issues"]
                    total = response.get("total", len(items))
                    is_last = len(items) < page_size
                elif isinstance(response, list):
                    # Some endpoints return arrays directly
                    items = response
                    total = len(items)
                    is_last = True
                else:
                    # Single item response
                    yield response
                    break

                # Check for empty pages
                if not items:
                    consecutive_empty_pages += 1
                    if consecutive_empty_pages >= max_empty_pages:
                        logger.warning(
                            f"Received {consecutive_empty_pages} consecutive empty pages, stopping pagination"
                        )
                        break
                else:
                    consecutive_empty_pages = 0

                for item in items:
                    if max_results and total_returned >= max_results:
                        return
                    yield item
                    total_returned += 1

                # Check if we've reached the end
                if is_last or len(items) < page_size:
                    break

                # Check if we've got all available items
                if total and total_returned >= total:
                    break

                # Move to next page
                params["startAt"] += page_size

                # Safety check to prevent infinite loops
                if params["startAt"] > 100000:  # Arbitrary large number
                    logger.warning(
                        f"Pagination safety limit reached for {endpoint}, stopping"
                    )
                    break

            except JiraAPIError as e:
                logger.error(f"API error during pagination of {endpoint}: {str(e)}")
                raise
            except Exception as e:
                logger.error(
                    f"Unexpected error during pagination of {endpoint}: {str(e)}"
                )
                raise JiraAPIError(f"Pagination failed: {str(e)}")

    def search_issues(
        self,
        jql: str,
        fields: Optional[str] = None,
        expand: Optional[str] = None,
        page_size: int = DEFAULT_PAGE_SIZE,
        max_results: Optional[int] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Search for issues using JQL.

        Args:
            jql: JQL query string
            fields: Comma-separated list of fields to return
            expand: Comma-separated list of fields to expand
            page_size: Number of items per page
            max_results: Maximum total results to return

        Yields:
            Issue data
        """
        params = {"jql": jql}
        if fields:
            params["fields"] = fields
        if expand:
            params["expand"] = expand

        yield from self.get_paginated(
            "search/jql", params=params, page_size=page_size, max_results=max_results
        )

    def get_projects(
        self, expand: Optional[str] = None, recent: Optional[int] = None
    ) -> Iterator[Dict[str, Any]]:
        """
        Get all projects.

        Args:
            expand: Comma-separated list of fields to expand
            recent: Number of recent projects to return

        Yields:
            Project data
        """
        params = {}
        if expand:
            params["expand"] = expand
        if recent:
            params["recent"] = str(recent)

        yield from self.get_paginated("project", params=params)

    def get_users(
        self,
        username: Optional[str] = None,
        account_id: Optional[str] = None,
        start_at: int = 0,
        max_results: int = DEFAULT_PAGE_SIZE,
    ) -> Iterator[Dict[str, Any]]:
        """
        Get users.

        Args:
            username: Username to search for
            account_id: Account ID to search for
            start_at: Starting index
            max_results: Maximum results per page

        Yields:
            User data
        """
        params = {
            "startAt": str(start_at),
            "maxResults": str(min(max_results, MAX_PAGE_SIZE)),
        }
        if username:
            params["username"] = username
        if account_id:
            params["accountId"] = account_id

        yield from self.get_paginated("users/search", params=params)

    def get_issue_types(self) -> Iterator[Dict[str, Any]]:
        """Get all issue types."""
        response = self._make_request("issuetype")
        if isinstance(response, list):
            for issue_type in response:
                yield issue_type

    def get_statuses(self) -> Iterator[Dict[str, Any]]:
        """Get all statuses."""
        response = self._make_request("status")
        if isinstance(response, list):
            for status in response:
                yield status

    def get_priorities(self) -> Iterator[Dict[str, Any]]:
        """Get all priorities."""
        response = self._make_request("priority")
        if isinstance(response, list):
            for priority in response:
                yield priority

    def get_resolutions(self) -> Iterator[Dict[str, Any]]:
        """Get all resolutions."""
        response = self._make_request("resolution")
        if isinstance(response, list):
            for resolution in response:
                yield resolution

    def get_project_versions(self, project_key: str) -> Iterator[Dict[str, Any]]:
        """
        Get versions for a specific project.

        Args:
            project_key: Project key

        Yields:
            Version data
        """
        yield from self.get_paginated(f"project/{project_key}/version")

    def get_project_components(self, project_key: str) -> Iterator[Dict[str, Any]]:
        """
        Get components for a specific project.

        Args:
            project_key: Project key

        Yields:
            Component data
        """
        yield from self.get_paginated(f"project/{project_key}/component")

    def get_events(self) -> Iterator[Dict[str, Any]]:
        """Get all events (issue events like created, updated, etc.)."""
        response = self._make_request("events")
        if isinstance(response, list):
            for event in response:
                yield event


def get_client(
    base_url: str, email: str, api_token: str, timeout: int = REQUEST_TIMEOUT
) -> JiraClient:
    """
    Create and return a Jira API client.

    Args:
        base_url: Jira instance URL
        email: User email for authentication
        api_token: API token for authentication
        timeout: Request timeout in seconds

    Returns:
        JiraClient instance
    """
    return JiraClient(base_url, email, api_token, timeout)
