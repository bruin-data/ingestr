import json
from typing import Any, Dict, Iterator, List, Optional

import dlt
import pendulum
import requests

FLUXX_API_BASE = "https://{instance}.fluxxlabs.com"
FLUXX_OAUTH_TOKEN_PATH = "/oauth/token"
FLUXX_API_V2_PATH = "/api/rest/v2"


def get_access_token(instance: str, client_id: str, client_secret: str) -> str:
    """Obtain OAuth access token using client credentials flow."""
    token_url = f"{FLUXX_API_BASE.format(instance=instance)}{FLUXX_OAUTH_TOKEN_PATH}"

    response = requests.post(
        token_url,
        data={
            "grant_type": "client_credentials",
            "client_id": client_id,
            "client_secret": client_secret,
        },
    )
    response.raise_for_status()

    token_data = response.json()
    return token_data["access_token"]


def fluxx_api_request(
    instance: str,
    access_token: str,
    endpoint: str,
    method: str = "GET",
    params: Optional[Dict[str, Any]] = None,
    data: Optional[Dict[str, Any]] = None,
) -> Dict[str, Any]:
    """Make an authenticated request to the Fluxx API."""
    url = f"{FLUXX_API_BASE.format(instance=instance)}{FLUXX_API_V2_PATH}/{endpoint}"

    headers = {
        "Authorization": f"Bearer {access_token}",
        "Content-Type": "application/json",
    }

    response = requests.request(
        method=method,
        url=url,
        headers=headers,
        params=params,
        json=data,
    )
    response.raise_for_status()

    if response.text:
        return response.json()
    return {}


def paginate_fluxx_resource(
    instance: str,
    access_token: str,
    endpoint: str,
    params: Optional[Dict[str, Any]] = None,
    page_size: int = 100,
) -> Iterator[List[Dict[str, Any]]]:
    """Paginate through a Fluxx API resource."""
    if params is None:
        params = {}

    page = 1
    params["per_page"] = page_size

    while True:
        params["page"] = page

        response = fluxx_api_request(
            instance=instance,
            access_token=access_token,
            endpoint=endpoint,
            params=params,
        )

        if not response:
            break

        # Get the first available key from records instead of assuming endpoint name
        records = response["records"]
        if records:
            # Pick the first key available in records
            first_key = next(iter(records))
            items = records[first_key]
        else:
            items = []

        yield items

        if response["per_page"] is None or len(items) < response["per_page"]:
            break

        page += 1


def get_date_range(updated_at, start_date):
    """Extract current start and end dates from incremental state."""
    if updated_at.last_value:
        current_start_date = pendulum.parse(updated_at.last_value)
    else:
        current_start_date = (
            pendulum.parse(start_date)
            if start_date
            else pendulum.now().subtract(days=30)
        )

    if updated_at.end_value:
        current_end_date = pendulum.parse(updated_at.end_value)
    else:
        current_end_date = pendulum.now(tz="UTC")

    return current_start_date, current_end_date


def create_dynamic_resource(
    resource_name: str,
    endpoint: str,
    instance: str,
    access_token: str,
    start_date: Optional[pendulum.DateTime] = None,
    end_date: Optional[pendulum.DateTime] = None,
    fields_to_extract: Optional[Dict[str, Any]] = None,
):
    """Factory function to create dynamic Fluxx resources."""

    # Extract column definitions for DLT resource
    columns = {}
    if fields_to_extract:
        for field_name, field_config in fields_to_extract.items():
            data_type = field_config.get("data_type")
            if data_type:
                columns[field_name] = {"data_type": data_type}

    @dlt.resource(name=resource_name, write_disposition="replace", columns=columns)  # type: ignore
    def fluxx_resource() -> Iterator[Dict[str, Any]]:
        params = {}
        if fields_to_extract:
            field_names = list(fields_to_extract.keys())
            params["cols"] = json.dumps(field_names)

        for page in paginate_fluxx_resource(
            instance=instance,
            access_token=access_token,
            endpoint=endpoint,
            params=params,
            page_size=100,
        ):
            yield [normalize_fluxx_item(item, fields_to_extract) for item in page]  # type: ignore

    return fluxx_resource


def normalize_fluxx_item(
    item: Dict[str, Any], fields_to_extract: Optional[Dict[str, Any]] = None
) -> Dict[str, Any]:
    """
    Normalize a Fluxx API response item.
    Handles nested structures and field extraction based on field types.
    Rounds all decimal/float values to 4 decimal places regardless of field type.
    """
    normalized: Dict[str, Any] = {}

    # If no field mapping provided, just return the item as-is
    if not fields_to_extract:
        return item

    for field_name, field_config in fields_to_extract.items():
        if field_name in item:
            value = item[field_name]
            field_type = field_config.get("data_type")

            if isinstance(value, float):
                # Round any numeric value with decimal places
                normalized[field_name] = round(value, 4)
            elif field_type == "json":
                # Handle json fields (arrays/relations)
                if value is None:
                    normalized[field_name] = None
                elif value == "":
                    normalized[field_name] = None
                elif isinstance(value, (list, dict)):
                    normalized[field_name] = value
                else:
                    # Single value - wrap in array for json fields
                    normalized[field_name] = [value]
            elif field_type in ("date", "timestamp", "datetime", "text"):
                # Handle text/date fields - convert empty strings to None
                if value == "":
                    normalized[field_name] = None
                else:
                    normalized[field_name] = value
            else:
                # All other field types - pass through as-is
                normalized[field_name] = value

    # Always include id if present
    if "id" in item:
        normalized["id"] = item["id"]

    return normalized
