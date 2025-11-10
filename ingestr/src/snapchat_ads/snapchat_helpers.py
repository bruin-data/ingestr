from typing import Iterator

import requests
from dlt.sources.helpers.requests import Client


def retry_on_limit(
    response: requests.Response | None, exception: BaseException | None
) -> bool:
    if response is None:
        return False
    return response.status_code == 429


def create_client() -> requests.Session:
    return Client(
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
        request_backoff_factor=2,
    ).session


def fetch_snapchat_data(
    api: "SnapchatAdsAPI",
    url: str,
    resource_key: str,
    item_key: str,
    start_date=None,
    end_date=None,
) -> Iterator[dict]:
    """
    Generic helper to fetch data from Snapchat API.

    Args:
        api: SnapchatAdsAPI instance
        url: API endpoint URL
        resource_key: Key in response JSON for the list of items (e.g., "organizations")
        item_key: Key in each item for the actual data (e.g., "organization")
        start_date: Optional start date for filtering by updated_at (client-side)
        end_date: Optional end date for filtering by updated_at (client-side)

    Yields:
        dict: Individual items from the API response
    """
    from dlt.common.time import ensure_pendulum_datetime

    client = create_client()
    headers = api.get_headers()

    response = client.get(url, headers=headers)

    if response.status_code != 200:
        raise ValueError(
            f"Failed to fetch {resource_key}: {response.status_code} - {response.text}"
        )

    result = response.json()

    if result.get("request_status", "").upper() != "SUCCESS":
        raise ValueError(f"Request failed: {result.get('request_status')} - {result}")

    items_data = result.get(resource_key, [])

    for item in items_data:
        if item.get("sub_request_status", "").upper() == "SUCCESS":
            data = item.get(item_key, {})
            if data:
                # Client-side filtering by updated_at
                if start_date or end_date:
                    updated_at_str = data.get("updated_at")
                    if updated_at_str:
                        updated_at = ensure_pendulum_datetime(updated_at_str)

                        if start_date and updated_at < ensure_pendulum_datetime(
                            start_date
                        ):
                            continue

                        if end_date and updated_at > ensure_pendulum_datetime(end_date):
                            continue

                yield data


def fetch_snapchat_data_with_params(
    api: "SnapchatAdsAPI",
    url: str,
    resource_key: str,
    item_key: str,
    params: dict = None,
) -> Iterator[dict]:
    """
    Generic helper to fetch data from Snapchat API with query parameters.

    Args:
        api: SnapchatAdsAPI instance
        url: API endpoint URL
        resource_key: Key in response JSON for the list of items (e.g., "transactions")
        item_key: Key in each item for the actual data (e.g., "transaction")
        params: Optional query parameters to pass to the API

    Yields:
        dict: Individual items from the API response
    """
    client = create_client()
    headers = api.get_headers()

    response = client.get(url, headers=headers, params=params or {})

    if response.status_code != 200:
        raise ValueError(
            f"Failed to fetch {resource_key}: {response.status_code} - {response.text}"
        )

    result = response.json()

    if result.get("request_status", "").upper() != "SUCCESS":
        raise ValueError(f"Request failed: {result.get('request_status')} - {result}")

    items_data = result.get(resource_key, [])

    for item in items_data:
        if item.get("sub_request_status", "").upper() == "SUCCESS":
            data = item.get(item_key, {})
            if data:
                yield data


class SnapchatAdsAPI:
    """Helper class for Snapchat Ads API authentication and requests."""

    TOKEN_URL = "https://accounts.snapchat.com/login/oauth2/access_token"

    def __init__(self, refresh_token: str, client_id: str, client_secret: str):
        self.refresh_token = refresh_token
        self.client_id = client_id
        self.client_secret = client_secret
        self._access_token = None

    def get_access_token(self) -> str:
        """
        Refresh the access token using the refresh token.

        Returns:
            str: The access token
        """
        if self._access_token:
            return self._access_token

        client = create_client()
        response = client.post(
            self.TOKEN_URL,
            data={
                "refresh_token": self.refresh_token,
                "client_id": self.client_id,
                "client_secret": self.client_secret,
                "grant_type": "refresh_token",
            },
        )

        if response.status_code != 200:
            raise ValueError(
                f"Failed to refresh access token: {response.status_code} - {response.text}"
            )

        result = response.json()
        self._access_token = result.get("access_token")

        if not self._access_token:
            raise ValueError(f"No access token in response: {result}")

        return self._access_token

    def get_headers(self) -> dict:
        """
        Get the headers with the access token for API requests.

        Returns:
            dict: Headers with Authorization Bearer token
        """
        access_token = self.get_access_token()
        return {
            "Authorization": f"Bearer {access_token}",
            "Content-Type": "application/json",
        }
