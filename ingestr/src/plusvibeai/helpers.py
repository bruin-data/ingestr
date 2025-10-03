"""PlusVibeAI source helpers"""

import logging
import time
from typing import Any, Dict, Iterator, Optional
from urllib.parse import urljoin

import requests

from .settings import API_BASE_PATH, DEFAULT_PAGE_SIZE, REQUEST_TIMEOUT

logger = logging.getLogger(__name__)


class PlusVibeAIAPIError(Exception):
    """Custom exception for PlusVibeAI API errors."""

    def __init__(
        self,
        message: str,
        status_code: Optional[int] = None,
        response_text: Optional[str] = None,
    ):
        super().__init__(message)
        self.status_code = status_code
        self.response_text = response_text


class PlusVibeAIAuthenticationError(PlusVibeAIAPIError):
    """Exception raised for authentication failures."""

    pass


class PlusVibeAIRateLimitError(PlusVibeAIAPIError):
    """Exception raised when rate limit is exceeded."""

    pass


class PlusVibeAIClient:
    """PlusVibeAI REST API client with API key authentication and pagination support."""

    def __init__(
        self,
        api_key: str,
        workspace_id: str,
        base_url: str = "https://api.plusvibe.ai",
        timeout: int = REQUEST_TIMEOUT,
    ):
        """
        Initialize PlusVibeAI client with API key authentication.

        Args:
            api_key: API key for authentication
            workspace_id: Workspace ID to access
            base_url: PlusVibeAI API base URL
            timeout: Request timeout in seconds
        """
        self.base_url = base_url.rstrip("/")
        self.api_url = urljoin(self.base_url, API_BASE_PATH)
        self.workspace_id = workspace_id
        self.timeout = timeout

        self.headers = {
            "x-api-key": api_key,
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
        Make HTTP request to PlusVibeAI API with retry logic.

        Args:
            endpoint: API endpoint path
            params: Query parameters
            method: HTTP method
            max_retries: Maximum number of retry attempts
            backoff_factor: Factor for exponential backoff

        Returns:
            JSON response data

        Raises:
            PlusVibeAIAPIError: If request fails after retries
            PlusVibeAIAuthenticationError: If authentication fails
            PlusVibeAIRateLimitError: If rate limit is exceeded (5 requests per second)
        """
        url = urljoin(self.api_url + "/", endpoint.lstrip("/"))

        # Add workspace_id to params
        if params is None:
            params = {}
        params["workspace_id"] = self.workspace_id

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
                    raise PlusVibeAIAuthenticationError(
                        "Authentication failed. Please check your API key.",
                        status_code=response.status_code,
                        response_text=response.text,
                    )
                elif response.status_code == 403:
                    raise PlusVibeAIAuthenticationError(
                        "Access forbidden. Please check your permissions and workspace_id.",
                        status_code=response.status_code,
                        response_text=response.text,
                    )
                elif response.status_code == 429:
                    # Rate limit exceeded (5 requests per second)
                    retry_after = int(response.headers.get("Retry-After", 1))
                    if attempt < max_retries:
                        logger.warning(
                            f"Rate limit exceeded (5 requests/second). Waiting {retry_after} seconds before retry."
                        )
                        time.sleep(retry_after)
                        continue
                    else:
                        raise PlusVibeAIRateLimitError(
                            f"Rate limit exceeded after {max_retries} retries. PlusVibeAI allows 5 requests per second.",
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
                        raise PlusVibeAIAPIError(
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
                    logger.error(
                        f"Invalid JSON response. Status: {response.status_code}, URL: {url}, Response text: {response.text[:500]}"
                    )
                    raise PlusVibeAIAPIError(
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
                    time.sleep(wait_time)
                    continue
                else:
                    raise PlusVibeAIAPIError(
                        f"Request failed after {max_retries} retries: {str(e)}"
                    )

        raise PlusVibeAIAPIError(f"Request failed after {max_retries} retries")

    def get_paginated(
        self,
        endpoint: str,
        params: Optional[Dict[str, Any]] = None,
        page_size: int = DEFAULT_PAGE_SIZE,
        max_results: Optional[int] = None,
        use_page_param: bool = False,
    ) -> Iterator[Dict[str, Any]]:
        """
        Get paginated results from PlusVibeAI API with error handling.

        Args:
            endpoint: API endpoint path
            params: Query parameters
            page_size: Number of items per page
            max_results: Maximum total results to return
            use_page_param: If True, use 'page' parameter (1-based) instead of 'skip' (0-based)

        Yields:
            Individual items from paginated response

        Raises:
            PlusVibeAIAPIError: If pagination fails
        """
        if params is None:
            params = {}

        params["limit"] = page_size

        if use_page_param:
            params["page"] = 1
        else:
            params["skip"] = 0

        total_returned = 0
        consecutive_empty_pages = 0
        max_empty_pages = 3

        while True:
            try:
                response = self._make_request(endpoint, params)

                # Handle different response structures
                if "data" in response:
                    items = response["data"]
                    total = response.get("total", len(items))
                elif "results" in response:
                    items = response["results"]
                    total = response.get("total", len(items))
                elif isinstance(response, list):
                    # Some endpoints return arrays directly
                    items = response
                    total = len(items)
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
                if len(items) < page_size:
                    break

                # Check if we've got all available items
                if total and total_returned >= total:
                    break

                # Move to next page
                if use_page_param:
                    params["page"] += 1
                    # Safety check
                    if params["page"] > 10000:
                        logger.warning(
                            f"Pagination safety limit reached for {endpoint}, stopping"
                        )
                        break
                else:
                    params["skip"] += page_size
                    # Safety check
                    if params["skip"] > 100000:
                        logger.warning(
                            f"Pagination safety limit reached for {endpoint}, stopping"
                        )
                        break

            except PlusVibeAIAPIError as e:
                logger.error(f"API error during pagination of {endpoint}: {str(e)}")
                raise
            except Exception as e:
                logger.error(
                    f"Unexpected error during pagination of {endpoint}: {str(e)}"
                )
                raise PlusVibeAIAPIError(f"Pagination failed: {str(e)}")

    def get_campaigns(
        self,
        page_size: int = DEFAULT_PAGE_SIZE,
        max_results: Optional[int] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Get all campaigns from PlusVibeAI.

        Args:
            page_size: Number of items per page
            max_results: Maximum total results to return

        Yields:
            Campaign data
        """
        yield from self.get_paginated(
            "campaign/list-all", page_size=page_size, max_results=max_results
        )

    def get_leads(
        self,
        page_size: int = DEFAULT_PAGE_SIZE,
        max_results: Optional[int] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Get workspace leads from PlusVibeAI.

        Args:
            page_size: Number of items per page
            max_results: Maximum total results to return

        Yields:
            Lead data
        """
        yield from self.get_paginated(
            "lead/workspace-leads",
            page_size=page_size,
            max_results=max_results,
            use_page_param=True,  # Leads endpoint uses 'page' parameter instead of 'skip'
        )

    def get_email_accounts(
        self,
        page_size: int = DEFAULT_PAGE_SIZE,
        max_results: Optional[int] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Get email accounts from PlusVibeAI.

        Args:
            page_size: Number of items per page
            max_results: Maximum total results to return

        Yields:
            Email account data
        """
        # Email accounts endpoint returns data in 'accounts' key
        for response in self.get_paginated(
            "account/list", page_size=page_size, max_results=max_results
        ):
            # Response structure: {"accounts": [...]}
            if isinstance(response, dict) and "accounts" in response:
                for account in response["accounts"]:
                    yield account
            else:
                yield response

    def get_emails(
        self,
        max_results: Optional[int] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Get emails from PlusVibeAI (uses cursor-based pagination with page_trail).

        Args:
            max_results: Maximum total results to return

        Yields:
            Email data
        """
        params: Dict[str, Any] = {}
        total_returned = 0

        while True:
            response = self._make_request("unibox/emails", params)

            if isinstance(response, dict):
                items = response.get("data", [])
                page_trail = response.get("page_trail")

                if not items:
                    break

                for item in items:
                    if max_results and total_returned >= max_results:
                        return
                    yield item
                    total_returned += 1

                # page_trail can be empty string when there are no more pages
                if page_trail and page_trail.strip():
                    params["page_trail"] = page_trail
                else:
                    break
            else:
                break

    def get_blocklist(
        self,
        page_size: int = DEFAULT_PAGE_SIZE,
        max_results: Optional[int] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Get blocklist entries from PlusVibeAI.

        Note: Blocklist API returns data in format {"0": {...}, "1": {...}}
        instead of standard array format.

        Args:
            page_size: Number of items per page
            max_results: Maximum total results to return

        Yields:
            Blocklist entry data
        """
        if max_results is None:
            max_results_limit = float("inf")
        else:
            max_results_limit = max_results

        params = {"limit": page_size, "skip": 0}
        total_returned = 0

        while total_returned < max_results_limit:
            response = self._make_request("blocklist/list", params)

            # Blocklist API returns {"0": {...}, "1": {...}} format
            if isinstance(response, dict):
                # Extract items from numbered keys
                items = []
                for key in sorted(
                    response.keys(),
                    key=lambda x: int(x) if x.isdigit() else float("inf"),
                ):
                    if key.isdigit():
                        items.append(response[key])

                if not items:
                    break

                for item in items:
                    if max_results and total_returned >= max_results:
                        return
                    yield item
                    total_returned += 1

                # If we got fewer items than page_size, we're done
                if len(items) < page_size:
                    break

                # Move to next page
                params["skip"] += page_size
            else:
                break

    def get_webhooks(
        self,
        page_size: int = DEFAULT_PAGE_SIZE,
        max_results: Optional[int] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Get webhooks from PlusVibeAI.

        Args:
            page_size: Number of items per page
            max_results: Maximum total results to return

        Yields:
            Webhook data
        """
        # Webhooks endpoint returns data in 'hooks' key
        response = self._make_request("hook/list")

        if isinstance(response, dict) and "hooks" in response:
            hooks = response["hooks"]
            if isinstance(hooks, list):
                count = 0
                for hook in hooks:
                    if max_results and count >= max_results:
                        break
                    yield hook
                    count += 1
            else:
                # Single hook response
                yield hooks
        elif isinstance(response, list):
            # Direct array response
            count = 0
            for hook in response:
                if max_results and count >= max_results:
                    break
                yield hook
                count += 1
        else:
            # Single item response
            yield response

    def get_tags(
        self,
        page_size: int = DEFAULT_PAGE_SIZE,
        max_results: Optional[int] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Get tags from PlusVibeAI.

        Args:
            page_size: Number of items per page
            max_results: Maximum total results to return

        Yields:
            Tag data
        """
        yield from self.get_paginated(
            "tags/list", page_size=page_size, max_results=max_results
        )


def get_client(
    api_key: str,
    workspace_id: str,
    base_url: str = "https://api.plusvibe.ai",
    timeout: int = REQUEST_TIMEOUT,
) -> PlusVibeAIClient:
    """
    Create and return a PlusVibeAI API client.

    Args:
        api_key: API key for authentication
        workspace_id: Workspace ID to access
        base_url: PlusVibeAI API base URL
        timeout: Request timeout in seconds

    Returns:
        PlusVibeAIClient instance
    """
    return PlusVibeAIClient(api_key, workspace_id, base_url, timeout)
