from typing import Any, Dict, Iterator, Optional
import requests
import pendulum
import dlt

REVENUECAT_API_BASE = "https://api.revenuecat.com/v2"


def _make_request(
    api_key: str, 
    endpoint: str, 
    params: Optional[Dict[str, Any]] = None
) -> Dict[str, Any]:
    """Make a REST API request to RevenueCat API v2."""
    headers = {
        "Authorization": f"Bearer {api_key}",
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
        if "items" in data:
            for item in data["items"]:
                yield item
        
        # Check if there's a next page
        if "next_page" not in data:
            break
            
        # Extract starting_after parameter from next_page URL
        next_page_url = data["next_page"]
        if "starting_after=" in next_page_url:
            starting_after = next_page_url.split("starting_after=")[1].split("&")[0]
            current_params["starting_after"] = starting_after
        else:
            break


def _get_date_range(updated_at, start_date):
    """Extract current start and end dates from incremental state."""
    if updated_at.last_value:
        current_start_date = pendulum.parse(updated_at.last_value)
    else:
        current_start_date = start_date
    
    if updated_at.end_value:
        current_end_date = pendulum.parse(updated_at.end_value)
    else:
        current_end_date = pendulum.now(tz="UTC")
    
    return current_start_date, current_end_date


