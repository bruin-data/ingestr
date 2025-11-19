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


def build_stats_url(
    base_url: str,
    entity_type: str,
    entity_id: str,
) -> str:
    """
    Build the stats URL for a given entity type and ID.

    Args:
        base_url: Base API URL
        entity_type: Type of entity (campaign, adsquad, ad, adaccount)
        entity_id: ID of the entity

    Returns:
        Complete stats URL
    """
    entity_type_map = {
        "campaign": "campaigns",
        "adsquad": "adsquads",
        "ad": "ads",
        "adaccount": "adaccounts",
    }

    plural_entity = entity_type_map.get(entity_type)
    if not plural_entity:
        raise ValueError(
            f"Invalid entity_type: {entity_type}. Must be one of: {list(entity_type_map.keys())}"
        )

    return f"{base_url}/{plural_entity}/{entity_id}/stats"


def parse_stats_table_format(table: str) -> dict:
    """
    Parse the stats table format string.

    Format: snapchat_ads_stats:<entity_type>:<entity_id>:<granularity>[:<fields>][:<options>]

    Examples:
        snapchat_ads_stats:campaign:abc123:DAY
        snapchat_ads_stats:campaign:abc123:DAY:impressions,spend,swipes
        snapchat_ads_stats:ad:abc123:TOTAL:impressions,spend:breakdown=ad,swipe_up_attribution_window=28_DAY

    Returns:
        Dictionary with parsed parameters
    """
    parts = table.split(":")

    if len(parts) < 4:
        raise ValueError(
            f"Invalid stats table format: {table}. "
            "Expected: snapchat_ads_stats:<entity_type>:<entity_id>:<granularity>[:<fields>][:<options>]"
        )

    entity_type = parts[1]
    entity_id = parts[2]
    granularity = parts[3].upper()

    # Validate entity_type
    valid_entity_types = ["campaign", "adsquad", "ad", "adaccount"]
    if entity_type not in valid_entity_types:
        raise ValueError(
            f"Invalid entity_type: {entity_type}. Must be one of: {valid_entity_types}"
        )

    # Validate granularity
    valid_granularities = ["TOTAL", "DAY", "HOUR", "LIFETIME"]
    if granularity not in valid_granularities:
        raise ValueError(
            f"Invalid granularity: {granularity}. Must be one of: {valid_granularities}"
        )

    result = {
        "entity_type": entity_type,
        "entity_id": entity_id,
        "granularity": granularity,
    }

    # Parse fields if provided (part 4)
    if len(parts) > 4 and parts[4]:
        # Check if this looks like options (contains =) or fields
        if "=" in parts[4]:
            # This is options, not fields
            options_str = parts[4]
            for option in options_str.split(","):
                if "=" in option:
                    key, value = option.split("=", 1)
                    result[key.strip()] = value.strip()
        else:
            # This is fields
            result["fields"] = parts[4]

            # Parse options if provided (part 5)
            if len(parts) > 5:
                options_str = parts[5]
                for option in options_str.split(","):
                    if "=" in option:
                        key, value = option.split("=", 1)
                        result[key.strip()] = value.strip()

    return result


def fetch_stats_data(
    api: "SnapchatAdsAPI",
    url: str,
    params: dict,
    granularity: str,
) -> Iterator[dict]:
    """
    Fetch stats data from Snapchat API.

    Args:
        api: SnapchatAdsAPI instance
        url: Stats endpoint URL
        params: Query parameters
        granularity: Granularity of stats (TOTAL, DAY, HOUR, LIFETIME)

    Yields:
        Flattened stats records
    """
    client = create_client()
    headers = api.get_headers()

    response = client.get(url, headers=headers, params=params)
    response.raise_for_status()

    result = response.json()

    if result.get("request_status", "").upper() != "SUCCESS":
        raise ValueError(f"Request failed: {result.get('request_status')} - {result}")

    # Parse based on granularity
    if granularity in ["TOTAL", "LIFETIME"]:
        yield from parse_total_stats(result)
    else:  # DAY or HOUR
        yield from parse_timeseries_stats(result)


def parse_total_stats(result: dict) -> Iterator[dict]:
    """
    Parse TOTAL or LIFETIME granularity stats response.

    Args:
        result: API response JSON

    Yields:
        Flattened stats records
    """
    total_stats = result.get("total_stats", [])

    for stat_item in total_stats:
        if stat_item.get("sub_request_status", "").upper() == "SUCCESS":
            total_stat = stat_item.get("total_stat", {})
            if total_stat:
                # Flatten the stats object
                record = {
                    "id": total_stat.get("id"),
                    "type": total_stat.get("type"),
                    "granularity": total_stat.get("granularity"),
                    "start_time": total_stat.get("start_time"),
                    "end_time": total_stat.get("end_time"),
                    "finalized_data_end_time": total_stat.get(
                        "finalized_data_end_time"
                    ),
                    "conversion_data_processed_end_time": total_stat.get(
                        "conversion_data_processed_end_time"
                    ),
                    "swipe_up_attribution_window": total_stat.get(
                        "swipe_up_attribution_window"
                    ),
                    "view_attribution_window": total_stat.get(
                        "view_attribution_window"
                    ),
                }

                # Flatten nested stats
                stats = total_stat.get("stats", {})
                for key, value in stats.items():
                    record[key] = value

                # Handle breakdown_stats if present
                breakdown_stats = total_stat.get("breakdown_stats", {})
                if breakdown_stats:
                    for breakdown_type, breakdown_items in breakdown_stats.items():
                        for item in breakdown_items:
                            breakdown_record = record.copy()
                            breakdown_record["breakdown_type"] = breakdown_type
                            breakdown_record["breakdown_id"] = item.get("id")
                            breakdown_record["breakdown_entity_type"] = item.get("type")

                            item_stats = item.get("stats", {})
                            for key, value in item_stats.items():
                                breakdown_record[key] = value

                            yield breakdown_record
                else:
                    yield record


def parse_timeseries_stats(result: dict) -> Iterator[dict]:
    """
    Parse DAY or HOUR granularity stats response.

    Args:
        result: API response JSON

    Yields:
        Flattened stats records for each time period
    """
    timeseries_stats = result.get("timeseries_stats", [])

    for stat_item in timeseries_stats:
        if stat_item.get("sub_request_status", "").upper() == "SUCCESS":
            timeseries_stat = stat_item.get("timeseries_stat", {})
            if timeseries_stat:
                entity_id = timeseries_stat.get("id")
                entity_type = timeseries_stat.get("type")
                granularity = timeseries_stat.get("granularity")
                finalized_data_end_time = timeseries_stat.get("finalized_data_end_time")
                conversion_data_processed_end_time = timeseries_stat.get(
                    "conversion_data_processed_end_time"
                )
                swipe_up_attribution_window = timeseries_stat.get(
                    "swipe_up_attribution_window"
                )
                view_attribution_window = timeseries_stat.get("view_attribution_window")

                # Iterate through each time period
                timeseries = timeseries_stat.get("timeseries", [])
                for period in timeseries:
                    record = {
                        "id": entity_id,
                        "type": entity_type,
                        "granularity": granularity,
                        "start_time": period.get("start_time"),
                        "end_time": period.get("end_time"),
                        "finalized_data_end_time": finalized_data_end_time,
                        "conversion_data_processed_end_time": conversion_data_processed_end_time,
                        "swipe_up_attribution_window": swipe_up_attribution_window,
                        "view_attribution_window": view_attribution_window,
                    }

                    # Flatten nested stats
                    stats = period.get("stats", {})
                    for key, value in stats.items():
                        record[key] = value

                    yield record

                # Handle breakdown_stats if present in timeseries
                breakdown_stats = timeseries_stat.get("breakdown_stats", {})
                if breakdown_stats:
                    for breakdown_type, breakdown_items in breakdown_stats.items():
                        for item in breakdown_items:
                            item_timeseries = item.get("timeseries", [])
                            for period in item_timeseries:
                                breakdown_record = {
                                    "id": entity_id,
                                    "type": entity_type,
                                    "granularity": granularity,
                                    "start_time": period.get("start_time"),
                                    "end_time": period.get("end_time"),
                                    "finalized_data_end_time": finalized_data_end_time,
                                    "conversion_data_processed_end_time": conversion_data_processed_end_time,
                                    "swipe_up_attribution_window": swipe_up_attribution_window,
                                    "view_attribution_window": view_attribution_window,
                                    "breakdown_type": breakdown_type,
                                    "breakdown_id": item.get("id"),
                                    "breakdown_entity_type": item.get("type"),
                                }

                                item_stats = period.get("stats", {})
                                for key, value in item_stats.items():
                                    breakdown_record[key] = value

                                yield breakdown_record


def get_entity_ids_for_stats(
    api: "SnapchatAdsAPI",
    entity_type: str,
    organization_id: str | None,
    base_url: str,
    entity_id: str | None = None,
) -> list[str]:
    """
    Get list of entity IDs to fetch stats for.

    Args:
        api: SnapchatAdsAPI instance
        entity_type: Type of entity (campaign, adsquad, ad, adaccount)
        organization_id: Organization ID (required if entity_id not provided)
        base_url: Base API URL
        entity_id: Specific entity ID to fetch stats for (optional)

    Returns:
        List of entity IDs
    """
    # If specific entity_id is provided, use it directly
    if entity_id:
        return [entity_id]

    if entity_type == "adaccount":
        # For ad account stats, return the account IDs
        return get_account_ids(
            api, None, organization_id, base_url, "stats", None, None
        )

    # For campaign, adsquad, ad - need to fetch from ad accounts
    account_ids = get_account_ids(
        api, None, organization_id, base_url, "stats", None, None
    )

    entity_ids = []
    client = create_client()
    headers = api.get_headers()

    entity_type_map = {
        "campaign": ("campaigns", "campaign"),
        "adsquad": ("adsquads", "adsquad"),
        "ad": ("ads", "ad"),
    }

    resource_name, item_key = entity_type_map[entity_type]

    for account_id in account_ids:
        url = f"{base_url}/adaccounts/{account_id}/{resource_name}"

        for result in paginate(client, headers, url, page_size=1000):
            items_data = result.get(resource_name, [])

            for item in items_data:
                if item.get("sub_request_status", "").upper() == "SUCCESS":
                    data = item.get(item_key, {})
                    if data and data.get("id"):
                        entity_ids.append(data["id"])

    return entity_ids
