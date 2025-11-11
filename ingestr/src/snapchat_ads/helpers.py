from typing import Iterator

import requests

from .client import SnapchatAdsAPI, create_client


def client_side_date_filter(data: dict, start_date, end_date) -> bool:
    """
    Check if data item falls within the specified date range based on updated_at.

    """
    if not start_date and not end_date:
        return True

    from dlt.common.time import ensure_pendulum_datetime

    updated_at_str = data.get("updated_at")
    if not updated_at_str:
        return True

    updated_at = ensure_pendulum_datetime(updated_at_str)

    if start_date and updated_at < ensure_pendulum_datetime(start_date):
        return False

    if end_date and updated_at > ensure_pendulum_datetime(end_date):
        return False

    return True


def paginate(client: requests.Session, headers: dict, url: str, page_size: int = 1000):
    """
    Helper to paginate through Snapchat API responses.
    """
    from urllib.parse import parse_qs, urlparse

    params: dict[str, int | str] = {"limit": page_size}

    while url:
        response = client.get(url, headers=headers, params=params)
        response.raise_for_status()

        result = response.json()

        if result.get("request_status", "").upper() != "SUCCESS":
            raise ValueError(
                f"Request failed: {result.get('request_status')} - {result}"
            )

        yield result

        # Check for next page
        paging = result.get("paging", {})
        next_link = paging.get("next_link")

        if next_link:
            # Extract cursor from next_link
            parsed = urlparse(next_link)
            query_params = parse_qs(parsed.query)
            cursor_list = query_params.get("cursor", [None])
            cursor = cursor_list[0] if cursor_list else None

            if cursor:
                params["cursor"] = cursor
            else:
                break
        else:
            break


def get_account_ids(
    api: "SnapchatAdsAPI",
    ad_account_id: str | None,
    organization_id: str | None,
    base_url: str,
    resource_name: str,
    start_date=None,
    end_date=None,
) -> list[str]:
    """
    Get list of account IDs to fetch data for.

    If ad_account_id is provided, returns a list with that single account.
    Otherwise, fetches all ad accounts for the organization.
    """
    if ad_account_id:
        return [ad_account_id]

    if not organization_id:
        raise ValueError(
            f"organization_id is required to fetch {resource_name} for all ad accounts"
        )

    accounts_url = f"{base_url}/organizations/{organization_id}/adaccounts"
    accounts_data = list(
        fetch_snapchat_data(
            api, accounts_url, "adaccounts", "adaccount", start_date, end_date
        )
    )
    return [
        account_id
        for account in accounts_data
        if (account_id := account.get("id")) is not None
    ]


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
    """
    client = create_client()
    headers = api.get_headers()

    response = client.get(url, headers=headers)
    response.raise_for_status()

    result = response.json()

    if result.get("request_status", "").upper() != "SUCCESS":
        raise ValueError(f"Request failed: {result.get('request_status')} - {result}")

    items_data = result.get(resource_key, [])

    for item in items_data:
        if item.get("sub_request_status", "").upper() == "SUCCESS":
            data = item.get(item_key, {})
            if data:
                # Client-side filtering by updated_at
                if client_side_date_filter(data, start_date, end_date):
                    yield data


def fetch_snapchat_data_with_params(
    api: "SnapchatAdsAPI",
    url: str,
    resource_key: str,
    item_key: str,
    params: dict | None = None,
) -> Iterator[dict]:
    """
    Generic helper to fetch data from Snapchat API with query parameters.
    """
    client = create_client()
    headers = api.get_headers()

    response = client.get(url, headers=headers, params=params or {})
    response.raise_for_status()

    result = response.json()

    if result.get("request_status", "").upper() != "SUCCESS":
        raise ValueError(f"Request failed: {result.get('request_status')} - {result}")

    items_data = result.get(resource_key, [])

    for item in items_data:
        if item.get("sub_request_status", "").upper() == "SUCCESS":
            data = item.get(item_key, {})
            if data:
                yield data


def fetch_account_id_resource(
    api: "SnapchatAdsAPI",
    ad_account_id: str | None,
    organization_id: str | None,
    base_url: str,
    resource_name: str,
    item_key: str,
    start_date=None,
    end_date=None,
) -> Iterator[dict]:
    """
    Fetch resource data for ad accounts without pagination.

    If ad_account_id is provided, fetches data for that specific account.
    Otherwise, fetches all ad accounts and then fetches data for each account.
    """
    account_ids = get_account_ids(
        api,
        ad_account_id,
        organization_id,
        base_url,
        resource_name,
        start_date,
        end_date,
    )

    for account_id in account_ids:
        url = f"{base_url}/adaccounts/{account_id}/{resource_name}"
        yield from fetch_snapchat_data(
            api, url, resource_name, item_key, start_date, end_date
        )


def fetch_with_paginate_account_id(
    api: "SnapchatAdsAPI",
    ad_account_id: str | None,
    organization_id: str | None,
    base_url: str,
    resource_name: str,
    item_key: str,
    start_date=None,
    end_date=None,
) -> Iterator[dict]:
    """
    Fetch paginated resource data for ad accounts.

    If ad_account_id is provided, fetches data for that specific account.
    Otherwise, fetches all ad accounts and then fetches data for each account.
    """
    account_ids = get_account_ids(
        api,
        ad_account_id,
        organization_id,
        base_url,
        resource_name,
        start_date,
        end_date,
    )

    client = create_client()
    headers = api.get_headers()

    for account_id in account_ids:
        url = f"{base_url}/adaccounts/{account_id}/{resource_name}"

        for result in paginate(client, headers, url, page_size=1000):
            items_data = result.get(resource_name, [])

            for item in items_data:
                if item.get("sub_request_status", "").upper() == "SUCCESS":
                    data = item.get(item_key, {})
                    if data:
                        if client_side_date_filter(data, start_date, end_date):
                            yield data
