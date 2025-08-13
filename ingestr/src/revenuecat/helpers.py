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
    
    while True:
        data = _make_request(api_key, endpoint, current_params)
        
        # Yield items from the current page
        if "items" in data:
            for item in data["items"]:
                yield item
            return


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


