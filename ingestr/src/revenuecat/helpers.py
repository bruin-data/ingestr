import asyncio
import time
from typing import Any, Dict, Iterator, List, Optional

import aiohttp
import pendulum
import requests

REVENUECAT_API_BASE = "https://api.revenuecat.com/v2"


def _make_request(
    api_key: str,
    endpoint: str,
    params: Optional[Dict[str, Any]] = None,
    max_retries: int = 3,
) -> Dict[str, Any]:
    """Make a REST API request to RevenueCat API v2 with rate limiting."""
    auth_header = f"Bearer {api_key}"

    headers = {"Authorization": auth_header, "Content-Type": "application/json"}

    url = f"{REVENUECAT_API_BASE}{endpoint}"

    for attempt in range(max_retries + 1):
        try:
            response = requests.get(url, headers=headers, params=params or {})

            # Handle rate limiting (429 Too Many Requests)
            if response.status_code == 429:
                if attempt < max_retries:
                    # Wait based on Retry-After header or exponential backoff
                    retry_after = response.headers.get("Retry-After")
                    if retry_after:
                        wait_time = int(retry_after)
                    else:
                        wait_time = (2**attempt) * 5  # 5, 10, 20 seconds

                    time.sleep(wait_time)
                    continue

            response.raise_for_status()
            return response.json()

        except requests.exceptions.RequestException:
            if attempt < max_retries:
                wait_time = (2**attempt) * 2  # 2, 4, 8 seconds
                time.sleep(wait_time)
                continue
            raise

    # If we get here, all retries failed
    response.raise_for_status()
    return response.json()


def _paginate(
    api_key: str, endpoint: str, params: Optional[Dict[str, Any]] = None
) -> Iterator[Dict[str, Any]]:
    """Paginate through RevenueCat API results."""
    current_params = params.copy() if params is not None else {}
    current_params["limit"] = 1000

    while True:
        data = _make_request(api_key, endpoint, current_params)

        if "items" in data and data["items"] is not None:
            yield data["items"]

        if "next_page" not in data:
            break

        # Extract starting_after parameter from next_page URL
        next_page_url = data["next_page"]
        if next_page_url and "starting_after=" in next_page_url:
            starting_after = next_page_url.split("starting_after=")[1].split("&")[0]
            current_params["starting_after"] = starting_after
        else:
            break


def convert_timestamps_to_iso(
    record: Dict[str, Any], timestamp_fields: List[str]
) -> Dict[str, Any]:
    """Convert timestamp fields from milliseconds to ISO format."""
    for field in timestamp_fields:
        if field in record and record[field] is not None:
            timestamp_ms = record[field]
            dt = pendulum.from_timestamp(timestamp_ms / 1000)
            record[field] = dt.to_iso8601_string()

    return record


async def _make_request_async(
    session: aiohttp.ClientSession,
    api_key: str,
    endpoint: str,
    params: Optional[Dict[str, Any]] = None,
    max_retries: int = 3,
) -> Dict[str, Any]:
    """Make an async REST API request to RevenueCat API v2 with rate limiting."""
    auth_header = f"Bearer {api_key}"

    headers = {"Authorization": auth_header, "Content-Type": "application/json"}

    url = f"{REVENUECAT_API_BASE}{endpoint}"

    for attempt in range(max_retries + 1):
        try:
            async with session.get(
                url, headers=headers, params=params or {}
            ) as response:
                # Handle rate limiting (429 Too Many Requests)
                if response.status == 429:
                    if attempt < max_retries:
                        # Wait based on Retry-After header or exponential backoff
                        retry_after = response.headers.get("Retry-After")
                        if retry_after:
                            wait_time = int(retry_after)
                        else:
                            wait_time = (2**attempt) * 5  # 5, 10, 20 seconds

                        await asyncio.sleep(wait_time)
                        continue

                response.raise_for_status()
                return await response.json()

        except aiohttp.ClientError:
            if attempt < max_retries:
                wait_time = (2**attempt) * 2  # 2, 4, 8 seconds
                await asyncio.sleep(wait_time)
                continue
            raise

    # If we get here, all retries failed
    async with session.get(url, headers=headers, params=params or {}) as response:
        response.raise_for_status()
        return await response.json()


async def _paginate_async(
    session: aiohttp.ClientSession,
    api_key: str,
    endpoint: str,
    params: Optional[Dict[str, Any]] = None,
) -> List[Dict[str, Any]]:
    """Paginate through RevenueCat API results asynchronously."""
    items = []
    current_params = params.copy() if params is not None else {}
    current_params["limit"] = 1000

    while True:
        data = await _make_request_async(session, api_key, endpoint, current_params)

        # Collect items from the current page
        if "items" in data and data["items"] is not None:
            items.extend(data["items"])

        # Check if there's a next page
        if "next_page" not in data:
            break

        # Extract starting_after parameter from next_page URL
        next_page_url = data["next_page"]
        if next_page_url and "starting_after=" in next_page_url:
            starting_after = next_page_url.split("starting_after=")[1].split("&")[0]
            current_params["starting_after"] = starting_after
        else:
            break

    return items


async def process_customer_with_nested_resources_async(
    session: aiohttp.ClientSession,
    api_key: str,
    project_id: str,
    customer: Dict[str, Any],
) -> Dict[str, Any]:
    customer_id = customer["id"]
    customer = convert_timestamps_to_iso(customer, ["first_seen_at", "last_seen_at"])
    nested_resources = [
        ("subscriptions", ["purchased_at", "expires_at", "grace_period_expires_at"]),
        ("purchases", ["purchased_at", "expires_at"]),
    ]

    async def fetch_and_convert(resource_name, timestamp_fields):
        if resource_name not in customer or customer[resource_name] is None:
            endpoint = f"/projects/{project_id}/customers/{customer_id}/{resource_name}"
            customer[resource_name] = await _paginate_async(session, api_key, endpoint)
        if (
            timestamp_fields
            and resource_name in customer
            and customer[resource_name] is not None
        ):
            for item in customer[resource_name]:
                convert_timestamps_to_iso(item, timestamp_fields)

    await asyncio.gather(
        *[
            fetch_and_convert(resource_name, timestamp_fields)
            for resource_name, timestamp_fields in nested_resources
        ]
    )

    return customer


def create_project_resource(
    resource_name: str,
    api_key: str,
    project_id: str = None,
    timestamp_fields: List[str] = None,
) -> Iterator[Dict[str, Any]]:
    """
    Helper function to create DLT resources for project-dependent endpoints.

    Args:
        resource_name: Name of the resource (e.g., 'products', 'entitlements', 'offerings')
        api_key: RevenueCat API key
        project_id: RevenueCat project ID
        timestamp_fields: List of timestamp fields to convert to ISO format

    Returns:
        Iterator of resource data
    """
    if project_id is None:
        raise ValueError(f"project_id is required for {resource_name} resource")

    endpoint = f"/projects/{project_id}/{resource_name}"
    default_timestamp_fields = timestamp_fields or ["created_at", "updated_at"]

    for item in _paginate(api_key, endpoint):
        item = convert_timestamps_to_iso(item, default_timestamp_fields)
        yield item
