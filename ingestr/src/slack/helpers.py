"""Slack source helpers."""

from typing import Any, Generator, Iterable, List, Optional
from urllib.parse import urljoin

import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import Dict, TAnyDateTime, TDataItem
from dlt.sources.helpers import requests
from jsonpath_ng.ext import parse  # type: ignore

from .settings import MAX_PAGE_SIZE, SLACK_API_URL


class SlackApiException(Exception):
    """Slack api exception."""


class PaidOnlyException(SlackApiException):
    """Slack api exception."""


def extract_jsonpath(
    expression: str,
    json_data: TDataItem,
) -> Generator[Any, None, None]:
    """Extract records from an input based on a JSONPath expression."""
    if not expression:
        yield json_data
        return

    jsonpath = parse(expression)

    for match in jsonpath.find(json_data):
        yield match.value


def update_jsonpath(expression: str, json_data: TDataItem, value: Any) -> Any:
    """Update a record in an input based on a JSONPath expression."""
    jsonpath = parse(expression)
    return jsonpath.update_or_create(json_data, value)


def ensure_dt_type(dt: TAnyDateTime, to_ts: bool = False) -> Any:
    """Converts a datetime to a pendulum datetime or timestamp.
    Args:
        dt: The datetime to convert.
        to_ts: Whether to convert to a timestamp or not.
    Returns:
        Any: The converted datetime or timestamp.
    """
    if dt is None:
        return None
    out_dt = ensure_pendulum_datetime(dt)
    if to_ts:
        return out_dt.timestamp()
    return out_dt


class SlackAPI:
    """
    A Slack API client that can be used to get pages of data from Slack.
    """

    def __init__(
        self,
        access_token: str,
        page_size: int = MAX_PAGE_SIZE,
    ) -> None:
        """
        Args:
            access_token: The private app password to the app on your shop.
            page_size: The max number of items to fetch per page. Defaults to 1000.
        """
        self.access_token = access_token
        self.page_size = page_size

    @property
    def headers(self) -> Dict[str, str]:
        """Generate the headers to use for the request."""
        return {"Authorization": f"Bearer {self.access_token}"}

    def parameters(
        self, params: Optional[Dict[str, Any]] = None, next_cursor: str = None
    ) -> Dict[str, str]:
        """
        Generate the query parameters to use for the request.

        Args:
            params: The query parameters to include in the request.
            next_cursor: The cursor to use to get the next page of results.

        Returns:
            The query parameters to use for the request.
        """
        params = params or {}
        params["limit"] = self.page_size
        if next_cursor:
            params["cursor"] = next_cursor
        return params

    def url(self, resource: str) -> str:
        """
        Generate the URL to use for the request.

        Args:
            resource: The resource to get pages for (e.g. conversations.list).
        """
        return urljoin(SLACK_API_URL, resource)

    def _get_next_cursor(self, response: Dict[str, Any]) -> Any:
        """
        Get the next cursor from the response.

        Args:
            response: The response from the Slack API.
        """
        cursor_jsonpath = "$.response_metadata.next_cursor"
        return next(extract_jsonpath(cursor_jsonpath, response), None)

    def _convert_datetime_fields(
        self, item: Dict[str, Any], datetime_fields: List[str]
    ) -> Dict[str, Any]:
        """Convert timestamp fields in the item to pendulum datetime objects.

        The item is modified in place.

        Args:
            item: The item to convert
            datetime_fields: List of fields to convert to pendulum datetime objects.

        Returns:
            The same data item (for convenience)
        """
        if not datetime_fields:
            return item

        for field in datetime_fields:
            if timestamp := next(extract_jsonpath(field, item), None):
                if isinstance(timestamp, str):
                    timestamp = float(timestamp)
                if timestamp > 1e10:
                    timestamp = timestamp / 1000
                pendulum_dt = pendulum.from_timestamp(timestamp)
                item = update_jsonpath(field, item, pendulum_dt)
        return item

    def get_pages(
        self,
        resource: str,
        response_path: str = None,
        params: Dict[str, Any] = None,
        datetime_fields: List[str] = None,
        context: Dict[str, Any] = None,
    ) -> Iterable[TDataItem]:
        """Get all pages from slack using requests.
        Iterates through all pages and yield each page items.\

        Args:
            resource: The resource to get pages for (e.g. conversations.list).
            response_path: The path to the list of items in the response JSON.
            params: Query params to include in the request.
            datetime_fields: List of fields to convert to pendulum datetime objects.
            context: Additional context to add to each item.

        Yields:
            List of data items from the page
        """
        has_next_page = True
        next_cursor = None

        # Iterate through all pages
        while has_next_page:
            # Make the request
            response = requests.get(
                url=self.url(resource),
                headers=self.headers,
                params=self.parameters(params or {}, next_cursor),
            )
            json_response = response.json()

            # Stop if there was an error
            if not json_response.get("ok"):
                has_next_page = False
                error = json_response.get("error")
                if error == "paid_only":
                    raise PaidOnlyException(
                        "This resource is just available on paid accounts."
                    )
                else:
                    raise SlackApiException(error)

            # Yield the page converting datetime fields
            output = []
            for item in extract_jsonpath(response_path, json_response):
                item = self._convert_datetime_fields(item, datetime_fields)
                item.update(context or {})
                output.append(item)
            yield output

            # Get the next cursor
            next_cursor = self._get_next_cursor(json_response)
            if not next_cursor:
                has_next_page = False
