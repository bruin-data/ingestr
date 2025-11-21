from typing import Iterator

import requests

from .client import SnapchatAdsAPI, create_client

# Module-level constant for entity type mapping
ENTITY_TYPE_MAP = {
    "campaign": "campaigns",
    "adsquad": "adsquads",
    "ad": "ads",
    "adaccount": "adaccounts",
}


def build_metadata_fields(source: dict, **overrides) -> dict:
    metadata_keys = [
        "start_time",
        "end_time",
        "finalized_data_end_time",
    ]
    result = {key: source.get(key) for key in metadata_keys}
    result.update(overrides)
    return result


def add_semantic_entity_fields(
    record: dict,
    entity_type: str,
    entity_id: str,
    breakdown_type: str | None = None,
    breakdown_id: str | None = None,
) -> None:
    """Add semantic entity ID fields to a record in-place."""
    parent_field_name = f"{entity_type.lower()}_id"
    record[parent_field_name] = entity_id

    if breakdown_type and breakdown_id is not None:
        breakdown_field_name = f"{breakdown_type}_id"
        record[breakdown_field_name] = breakdown_id


def normalize_stats_record(record: dict) -> dict:
    """Normalize stats record by ensuring required primary key fields exist.

    Only campaign_id is required and will be filled with 'no_campaign_id' if missing.
    Other fields (adsquad_id, ad_id) will be set to None if not present (no breakdown).
    Time fields (start_time, end_time) are always expected to exist.
    """
    # Ensure campaign_id exists (required field)
    if "campaign_id" not in record or record["campaign_id"] is None:
        record["campaign_id"] = "no_campaign_id"

    # For optional breakdown fields, set to None if not present
    for field in ["adsquad_id", "ad_id"]:
        if field not in record:
            record[field] = None

    # Time fields should always exist, but add fallback
    for field in ["start_time", "end_time"]:
        if field not in record or record[field] is None:
            record[field] = f"no_{field}"

    return record


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
    # Don't filter accounts by date - we want all accounts, then filter stats by date
    accounts_data = list(
        fetch_snapchat_data(api, accounts_url, "adaccounts", "adaccount", None, None)
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
    plural_entity = ENTITY_TYPE_MAP.get(entity_type)
    if not plural_entity:
        raise ValueError(
            f"Invalid entity_type: {entity_type}. Must be one of: {list(ENTITY_TYPE_MAP.keys())}"
        )

    return f"{base_url}/{plural_entity}/{entity_id}/stats"


def fetch_stats_data(
    api: "SnapchatAdsAPI",
    url: str,
    params: dict,
    granularity: str,
) -> Iterator[dict]:
    client = create_client()
    headers = api.get_headers()

    response = client.get(url, headers=headers, params=params)
    if not response.ok:
        raise ValueError(
            f"Stats request failed: {response.status_code} - {response.text}"
        )
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
    # Handle both total_stats and lifetime_stats response formats
    total_stats = result.get("total_stats", []) or result.get("lifetime_stats", [])

    for stat_item in total_stats:
        if stat_item.get("sub_request_status", "").upper() == "SUCCESS":
            # Handle both total_stat and lifetime_stat keys
            total_stat = stat_item.get("total_stat", {}) or stat_item.get(
                "lifetime_stat", {}
            )
            if total_stat:
                # Flatten the stats object
                record = {
                    "id": total_stat.get("id"),
                    "type": total_stat.get("type"),
                    **build_metadata_fields(total_stat),
                }

                # Flatten nested stats
                stats = total_stat.get("stats", {})
                for key, value in stats.items():
                    record[key] = value

                # Handle breakdown_stats if present
                breakdown_stats = total_stat.get("breakdown_stats", {})

                if breakdown_stats:
                    # Yield breakdown data when no dimension
                    for breakdown_type, breakdown_items in breakdown_stats.items():
                        for item in breakdown_items:
                            breakdown_record: dict = {}

                            # Add semantic entity fields (parent + breakdown)
                            add_semantic_entity_fields(
                                breakdown_record,
                                record["type"],
                                record["id"],
                                breakdown_type,
                                item.get("id"),
                            )

                            # Add metadata fields
                            metadata = build_metadata_fields(record)
                            breakdown_record.update(metadata)

                            # Add stats
                            item_stats = item.get("stats", {})
                            for key, value in item_stats.items():
                                breakdown_record[key] = value

                            yield normalize_stats_record(breakdown_record)
                else:
                    # No breakdown or dimension - yield parent record
                    # Convert generic 'id' to semantic name for consistency
                    parent_field_name = f"{record['type'].lower()}_id"
                    record[parent_field_name] = record.pop("id")
                    record.pop("type", None)  # Remove type field as it's redundant now
                    yield normalize_stats_record(record)


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

                # Handle breakdown_stats if present in timeseries
                breakdown_stats = timeseries_stat.get("breakdown_stats", {})

                if breakdown_stats:
                    # Yield only breakdown data when breakdown is present
                    for breakdown_type, breakdown_items in breakdown_stats.items():
                        for item in breakdown_items:
                            item_timeseries = item.get("timeseries", [])
                            for period in item_timeseries:
                                breakdown_record: dict = {}

                                # Add semantic entity fields (parent + breakdown)
                                add_semantic_entity_fields(
                                    breakdown_record,
                                    entity_type,
                                    entity_id,
                                    breakdown_type,
                                    item.get("id"),
                                )

                                # Add metadata fields
                                metadata = build_metadata_fields(
                                    timeseries_stat,
                                    start_time=period.get("start_time"),
                                    end_time=period.get("end_time"),
                                )
                                breakdown_record.update(metadata)

                                # Add stats
                                item_stats = period.get("stats", {})
                                for key, value in item_stats.items():
                                    breakdown_record[key] = value

                                yield normalize_stats_record(breakdown_record)
                else:
                    # Yield parent entity data when no breakdown or dimension
                    timeseries = timeseries_stat.get("timeseries", [])
                    for period in timeseries:
                        record: dict = {}

                        # Add semantic entity field (parent only)
                        add_semantic_entity_fields(record, entity_type, entity_id)

                        # Add metadata fields
                        metadata = build_metadata_fields(
                            timeseries_stat,
                            start_time=period.get("start_time"),
                            end_time=period.get("end_time"),
                        )
                        record.update(metadata)

                        # Flatten nested stats
                        stats = period.get("stats", {})
                        for key, value in stats.items():
                            record[key] = value

                        yield normalize_stats_record(record)


def fetch_entity_stats(
    api: "SnapchatAdsAPI",
    entity_type: str,
    ad_account_id: str | None,
    organization_id: str | None,
    base_url: str,
    params: dict,
    granularity: str,
    start_date=None,
    end_date=None,
) -> Iterator[dict]:
    # Get account IDs
    account_ids = get_account_ids(
        api, ad_account_id, organization_id, base_url, "stats", start_date, end_date
    )

    if not account_ids:
        return

    if entity_type == "adaccount":
        # For ad accounts, fetch stats directly for each account
        for account_id in account_ids:
            url = f"{base_url}/adaccounts/{account_id}/stats"
            yield from fetch_stats_data(api, url, params, granularity)
    else:
        # For campaign, adsquad, ad - first fetch entities, then stats
        # Build resource_name from ENTITY_TYPE_MAP and item_key from entity_type
        resource_name = ENTITY_TYPE_MAP.get(entity_type)
        if not resource_name:
            raise ValueError(f"Invalid entity_type: {entity_type}")

        item_key = entity_type
        client = create_client()
        headers = api.get_headers()

        for account_id in account_ids:
            url = f"{base_url}/adaccounts/{account_id}/{resource_name}"

            for result in paginate(client, headers, url, page_size=1000):
                items_data = result.get(resource_name, [])

                for item in items_data:
                    if item.get("sub_request_status", "").upper() == "SUCCESS":
                        data = item.get(item_key, {})
                        if data and data.get("id"):
                            entity_id = data["id"]
                            stats_url = build_stats_url(
                                base_url, entity_type, entity_id
                            )
                            yield from fetch_stats_data(
                                api, stats_url, params, granularity
                            )


def parse_stats_table(table: str) -> dict:
    import typing

    from ingestr.src.snapchat_ads.settings import (
        DEFAULT_STATS_FIELDS,
        TStatsBreakdown,
        TStatsGranularity,
    )

    parts = table.split(":")
    resource_name = parts[0]
    stats_config = {}

    if len(parts) == 1:
        raise ValueError(
            f"Parameters required for stats table. Format: {resource_name}:<granularity>[,<fields>]"
        )

    valid_granularities = list(typing.get_args(TStatsGranularity))
    valid_breakdowns = list(typing.get_args(TStatsBreakdown))

    # Parse all parameters from parts[1] (comma-separated)
    params = parts[1].split(",")

    # Find granularity (required)
    granularity_found = False
    fields_parts = []

    for i, param in enumerate(params):
        param_clean = param.strip()

        if param_clean.lower() in valid_breakdowns:
            stats_config["breakdown"] = param_clean.lower()
        elif param_clean.upper() in valid_granularities:
            stats_config["granularity"] = param_clean.upper()
            granularity_found = True
            # Everything after granularity is fields
            if i + 1 < len(params):
                fields_parts = params[i + 1 :]
            break

    if not granularity_found:
        raise ValueError(
            f"Granularity is required. Format: {resource_name}:<breakdown>,<granularity>[,<fields>]"
        )

    # Join remaining parts as fields
    if fields_parts:
        stats_config["fields"] = ",".join(p.strip() for p in fields_parts)
    else:
        stats_config["fields"] = DEFAULT_STATS_FIELDS

    return {
        "resource_name": resource_name,
        "stats_config": stats_config,
    }
