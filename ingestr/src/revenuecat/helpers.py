from typing import Any, Dict, Iterator, List, Optional
import requests
import pendulum

REVENUECAT_API_BASE = "https://api.revenuecat.com/v2"


def _make_request(
    api_key: str, 
    endpoint: str, 
    params: Optional[Dict[str, Any]] = None
) -> Dict[str, Any]:
    """Make a REST API request to RevenueCat API v2."""
    # Check if api_key already starts with "Bearer "
    auth_header = api_key if api_key.startswith("Bearer ") else f"Bearer {api_key}"
    
    headers = {
        "Authorization": auth_header,
        "Content-Type": "application/json"
    }
    
    url = f"{REVENUECAT_API_BASE}{endpoint}"
    response = requests.get(url, headers=headers, params=params or {})
    response.raise_for_status()
    
    return response.json()


def _paginate(
    api_key: str, 
    endpoint: str, 
    params: Optional[Dict[str, Any]] = None
) -> Iterator[Dict[str, Any]]:
    """Paginate through RevenueCat API results."""
    current_params = params.copy() if params is not None else {}
    current_params["limit"] = 1000
    
    while True:
        data = _make_request(api_key, endpoint, current_params)
        
        # Yield items from the current page
        if "items" in data and data["items"] is not None:
            for item in data["items"]:
                yield item
        
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


def convert_timestamps_to_iso(record: Dict[str, Any], timestamp_fields: List[str]) -> Dict[str, Any]:
    """Convert timestamp fields from milliseconds to ISO format."""
    for field in timestamp_fields:
        if field in record and record[field] is not None:
            # Convert from milliseconds timestamp to ISO datetime string
            timestamp_ms = record[field]
            dt = pendulum.from_timestamp(timestamp_ms / 1000)
            record[field] = dt.to_iso8601_string()
    
    return record


def fetch_and_process_nested_resource(
    api_key: str,
    project_id: str,
    customer_id: str,
    customer: Dict[str, Any],
    resource_name: str,
    timestamp_fields: Optional[List[str]] = None
) -> None:
    """
    Fetch and process any nested resource for a customer.
    
    Args:
        api_key: RevenueCat API key
        project_id: Project ID
        customer_id: Customer ID
        customer: Customer data dictionary to modify
        resource_name: Name of the nested resource (e.g., 'purchases', 'subscriptions', 'events')
        timestamp_fields: List of timestamp fields to convert to ISO format
    """
    # If resource not included in customer data, fetch separately
    if resource_name not in customer or customer[resource_name] is None:
        endpoint = f"/projects/{project_id}/customers/{customer_id}/{resource_name}"
        customer[resource_name] = []
        for item in _paginate(api_key, endpoint):
            customer[resource_name].append(item)
    
    # Convert timestamps if fields specified
    if timestamp_fields and resource_name in customer and customer[resource_name] is not None:
        for item in customer[resource_name]:
            convert_timestamps_to_iso(item, timestamp_fields)


