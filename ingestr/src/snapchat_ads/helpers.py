from dataclasses import dataclass
from typing import Iterator

import requests

from .client import SnapchatAdsAPI, create_client


@dataclass
class ParsedStatsTable:
    """Parsed stats table configuration.

    Table format: <resource-name>:<dimension-like-values>:<metrics>

    Dimension-like values (order-independent, comma-separated):
        - granularity (required): TOTAL, DAY, HOUR, LIFETIME
        - breakdown (optional): ad, adsquad, campaign
        - dimension (optional): GEO, DEMO, INTEREST, DEVICE
        - pivot (optional): country, region, dma, gender, age_bucket, etc.

    Metrics: comma-separated field names (default: impressions,spend)
    """

    resource_name: str
    granularity: str
    fields: str
    breakdown: str | None = None
    dimension: str | None = None
    pivot: str | None = None


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
    ad_account_id: list[str] | None,
    organization_id: str | None,
    base_url: str,
    resource_name: str,
    start_date=None,
    end_date=None,
) -> list[str]:
    """
    Get list of account IDs to fetch data for.

    If ad_account_id is provided, returns that list of accounts.
    Otherwise, fetches all ad accounts for the organization.
    """
    if ad_account_id:
        return ad_account_id

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
    ad_account_id: list[str] | None,
    organization_id: str | None,
    base_url: str,
    resource_name: str,
    item_key: str,
    start_date=None,
    end_date=None,
) -> Iterator[dict]:
    """
    Fetch resource data for ad accounts without pagination.

    If ad_account_id is provided, fetches data for those specific accounts.
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
    ad_account_id: list[str] | None,
    organization_id: str | None,
    base_url: str,
    resource_name: str,
    item_key: str,
    start_date=None,
    end_date=None,
) -> Iterator[dict]:
    """
    Fetch paginated resource data for ad accounts.

    If ad_account_id is provided, fetches data for those specific accounts.
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
    ad_account_id: list[str] | None,
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


def parse_stats_table(table: str) -> ParsedStatsTable:
    """Parse stats table string into ParsedStatsTable.

    Format: <resource-name>:<dimension-like-values>:<metrics>

    Examples:
        campaigns_stats:DAY:impressions,spend
        campaigns_stats:campaign,DAY:impressions,spend
        campaigns_stats:campaign,DAY,GEO,country:impressions,spend

    Args:
        table: Table string in the format above

    Returns:
        ParsedStatsTable with categorized parameters

    Raises:
        ValueError: If granularity is missing or format is invalid
    """
    from ingestr.src.snapchat_ads.settings import (
        DEFAULT_STATS_FIELDS,
        VALID_BREAKDOWNS,
        VALID_DIMENSIONS,
        VALID_GRANULARITIES,
        VALID_PIVOTS,
    )

    parts = table.split(":")
    resource_name = parts[0]

    if len(parts) < 2:
        raise ValueError(
            f"Parameters required for stats table. "
            f"Format: {resource_name}:<dimension-like-values>:<metrics>"
        )

    # Parse dimension-like values (part 1)
    dimension_params = [p.strip() for p in parts[1].split(",")]

    # Categorize each parameter without depending on order
    granularity: str | None = None
    breakdown: str | None = None
    dimension: str | None = None
    pivot: str | None = None

    for param in dimension_params:
        param_upper = param.upper()
        param_lower = param.lower()

        if param_upper in VALID_GRANULARITIES:
            granularity = param_upper
        elif param_lower in VALID_BREAKDOWNS:
            breakdown = param_lower
        elif param_upper in VALID_DIMENSIONS:
            dimension = param_upper
        elif param_lower in VALID_PIVOTS:
            pivot = param_lower
        else:
            raise ValueError(
                f"Unknown parameter '{param}'. Must be a granularity "
                f"({', '.join(VALID_GRANULARITIES)}), breakdown "
                f"({', '.join(VALID_BREAKDOWNS)}), dimension "
                f"({', '.join(VALID_DIMENSIONS)}), or pivot "
                f"({', '.join(VALID_PIVOTS)})"
            )

    if not granularity:
        raise ValueError(
            f"Granularity is required. "
            f"Format: {resource_name}:<dimension-like-values>:<metrics>"
        )

    # Parse metrics (part 2) or use defaults
    if len(parts) >= 3 and parts[2].strip():
        fields = parts[2].strip()
    else:
        fields = DEFAULT_STATS_FIELDS

    return ParsedStatsTable(
        resource_name=resource_name,
        granularity=granularity,
        fields=fields,
        breakdown=breakdown,
        dimension=dimension,
        pivot=pivot,
    )
