"""
Helper functions and API client for Intercom integration.
"""
import time
from dataclasses import dataclass
from enum import Enum
from typing import Any, Dict, Iterator, List, Optional, Union

from dlt.common.typing import TDataItem, TDataItems, TSecretValue

from ingestr.src.http_client import create_client
from .settings import (
    API_VERSION,
    DEFAULT_PAGE_SIZE,
    RATE_LIMIT_PER_10_SECONDS,
    RATE_LIMIT_RETRY_AFTER_DEFAULT,
    REGIONAL_ENDPOINTS,
)


class PaginationType(Enum):
    """Types of pagination supported by Intercom API."""
    CURSOR = "cursor"
    SCROLL = "scroll"
    SIMPLE = "simple"  # No pagination, single page
    SEARCH = "search"  # Search API pagination


class IntercomCredentials:
    """Base class for Intercom credentials."""
    
    def __init__(self, region: str = "us"):
        self.region = region
        if self.region not in REGIONAL_ENDPOINTS:
            raise ValueError(f"Invalid region: {self.region}. Must be one of {list(REGIONAL_ENDPOINTS.keys())}")

    @property
    def base_url(self) -> str:
        """Get the base URL for the specified region."""
        return REGIONAL_ENDPOINTS[self.region]


@dataclass
class IntercomCredentialsAccessToken(IntercomCredentials):
    """Credentials for Intercom API using Access Token authentication."""
    access_token: TSecretValue
    region: str = "us"  # us, eu, or au
    
    def __post_init__(self):
        super().__init__(self.region)


@dataclass
class IntercomCredentialsOAuth(IntercomCredentials):
    """Credentials for Intercom API using OAuth authentication."""
    oauth_token: TSecretValue
    region: str = "us"  # us, eu, or au
    
    def __post_init__(self):
        super().__init__(self.region)


TIntercomCredentials = Union[IntercomCredentialsAccessToken, IntercomCredentialsOAuth]


class IntercomAPIClient:
    """
    API client for making requests to Intercom API.
    Handles authentication, pagination, and rate limiting.
    """

    def __init__(self, credentials: TIntercomCredentials):
        """
        Initialize the Intercom API client.

        Args:
            credentials: Intercom API credentials
        """
        self.credentials = credentials
        self.base_url = credentials.base_url
        
        # Set up authentication headers
        self.headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
            "Intercom-Version": API_VERSION,  # REQUIRED header
        }
        
        if isinstance(credentials, IntercomCredentialsAccessToken):
            self.headers["Authorization"] = f"Bearer {credentials.access_token}"
        elif isinstance(credentials, IntercomCredentialsOAuth):
            self.headers["Authorization"] = f"Bearer {credentials.oauth_token}"
        else:
            raise TypeError("Invalid credentials type. Must be IntercomCredentialsAccessToken or IntercomCredentialsOAuth")
        
        # Create HTTP client with rate limit retry for 429 status codes
        self.client = create_client(retry_status_codes=[429, 502, 503])

    def _make_request(
        self,
        method: str,
        endpoint: str,
        params: Optional[Dict[str, Any]] = None,
        json_data: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        """
        Make a request to the Intercom API.

        Args:
            method: HTTP method (GET, POST, etc.)
            endpoint: API endpoint path
            params: Query parameters
            json_data: JSON body data

        Returns:
            Response JSON data
        """
        url = f"{self.base_url}/api{endpoint}" if not endpoint.startswith("/api") else f"{self.base_url}{endpoint}"
        
        if method.upper() == "GET":
            response = self.client.get(url, headers=self.headers, params=params)
        elif method.upper() == "POST":
            response = self.client.post(url, headers=self.headers, json=json_data, params=params)
        else:
            response = self.client.request(method, url, headers=self.headers, json=json_data, params=params)
        
        # The create_client already handles rate limiting (429) with retries
        # Just check for other errors
        if response.status_code >= 400:
            error_msg = f"Intercom API error {response.status_code}: {response.text}"
            raise Exception(error_msg)
        
        return response.json()

    def get_pages(
        self,
        endpoint: str,
        data_key: str,
        pagination_type: PaginationType,
        params: Optional[Dict[str, Any]] = None,
        search_query: Optional[Dict[str, Any]] = None,
    ) -> Iterator[TDataItems]:
        """
        Get paginated data from an Intercom endpoint.

        Args:
            endpoint: API endpoint path
            data_key: Key in response containing the data items
            pagination_type: Type of pagination to use
            params: Query parameters
            search_query: Search query for search endpoints

        Yields:
            Lists of data items from each page
        """
        params = params or {}
        
        if pagination_type == PaginationType.SIMPLE:
            # Single page, no pagination
            response = self._make_request("GET", endpoint, params)
            if data_key in response:
                yield response[data_key]
            return
        
        elif pagination_type == PaginationType.CURSOR:
            # Cursor-based pagination
            params["per_page"] = params.get("per_page", DEFAULT_PAGE_SIZE)
            next_cursor = None
            
            while True:
                if next_cursor:
                    params["starting_after"] = next_cursor
                
                response = self._make_request("GET", endpoint, params)
                
                # Yield the data
                if data_key in response and response[data_key]:
                    yield response[data_key]
                
                # Check for next page
                pages_info = response.get("pages", {})
                if not pages_info.get("next"):
                    break
                    
                next_cursor = pages_info.get("next", {}).get("starting_after")
                if not next_cursor:
                    break
        
        elif pagination_type == PaginationType.SCROLL:
            # Scroll API pagination (for large exports)
            scroll_param = None
            
            while True:
                scroll_endpoint = endpoint
                if scroll_param:
                    scroll_endpoint = f"{endpoint}/scroll"
                    params = {"scroll_param": scroll_param}
                
                response = self._make_request("GET", scroll_endpoint, params)
                
                # Yield the data
                if data_key in response and response[data_key]:
                    yield response[data_key]
                
                # Get next scroll parameter
                scroll_param = response.get("scroll_param")
                if not scroll_param:
                    break
        
        elif pagination_type == PaginationType.SEARCH:
            # Search API pagination
            if not search_query:
                raise ValueError("Search query required for search pagination")
            
            pagination_info = search_query.get("pagination", {})
            pagination_info["per_page"] = pagination_info.get("per_page", DEFAULT_PAGE_SIZE)
            
            while True:
                # Build search request
                request_data = {
                    "query": search_query.get("query", {}),
                    "pagination": pagination_info,
                }
                
                if "sort" in search_query:
                    request_data["sort"] = search_query["sort"]
                
                response = self._make_request("POST", endpoint, json_data=request_data)
                
                # Yield the data
                if data_key in response and response[data_key]:
                    yield response[data_key]
                
                # Check for next page
                pages_info = response.get("pages", {})
                if not pages_info.get("next"):
                    break
                
                next_cursor = pages_info.get("next", {}).get("starting_after")
                if not next_cursor:
                    break
                    
                pagination_info["starting_after"] = next_cursor

    def get_single_resource(self, endpoint: str, resource_id: str) -> TDataItem:
        """
        Get a single resource by ID.

        Args:
            endpoint: Base endpoint path
            resource_id: Resource ID

        Returns:
            Resource data
        """
        return self._make_request("GET", f"{endpoint}/{resource_id}")

    def search(
        self,
        resource_type: str,
        query: Dict[str, Any],
        sort: Optional[Dict[str, str]] = None,
    ) -> Iterator[TDataItems]:
        """
        Search for resources using the Search API.

        Args:
            resource_type: Type of resource to search (contacts, companies, conversations)
            query: Search query following Intercom's query format
            sort: Optional sort configuration

        Yields:
            Lists of matching resources
        """
        endpoint = f"/{resource_type}/search"
        search_query = {"query": query}
        
        if sort:
            search_query["sort"] = sort
        
        yield from self.get_pages(
            endpoint=endpoint,
            data_key="data",
            pagination_type=PaginationType.SEARCH,
            search_query=search_query,
        )


def transform_contact(contact: Dict[str, Any]) -> Dict[str, Any]:
    """
    Transform a contact record to ensure consistent format.

    Args:
        contact: Raw contact data from API

    Returns:
        Transformed contact data
    """
    # Ensure consistent field names and types
    transformed = contact.copy()
    
    # Flatten location data if present
    if "location" in transformed and isinstance(transformed["location"], dict):
        location = transformed.pop("location")
        transformed["location_country"] = location.get("country")
        transformed["location_region"] = location.get("region")
        transformed["location_city"] = location.get("city")
    
    # Flatten companies relationship
    if "companies" in transformed and isinstance(transformed["companies"], dict):
        companies_data = transformed["companies"].get("data", [])
        transformed["company_ids"] = [c.get("id") for c in companies_data if c.get("id")]
        transformed["companies_count"] = len(companies_data)
    
    # Ensure custom_attributes is always a dict
    if "custom_attributes" not in transformed:
        transformed["custom_attributes"] = {}
    
    return transformed


def transform_company(company: Dict[str, Any]) -> Dict[str, Any]:
    """
    Transform a company record to ensure consistent format.

    Args:
        company: Raw company data from API

    Returns:
        Transformed company data
    """
    transformed = company.copy()
    
    # Ensure custom_attributes is always a dict
    if "custom_attributes" not in transformed:
        transformed["custom_attributes"] = {}
    
    # Flatten plan information if it's an object
    if "plan" in transformed and isinstance(transformed["plan"], dict):
        plan = transformed.pop("plan")
        transformed["plan_id"] = plan.get("id")
        transformed["plan_name"] = plan.get("name")
    
    return transformed


def transform_conversation(conversation: Dict[str, Any]) -> Dict[str, Any]:
    """
    Transform a conversation record to ensure consistent format.

    Args:
        conversation: Raw conversation data from API

    Returns:
        Transformed conversation data
    """
    transformed = conversation.copy()
    
    # Extract statistics if present
    if "statistics" in transformed and isinstance(transformed["statistics"], dict):
        stats = transformed.pop("statistics")
        transformed["first_contact_reply_at"] = stats.get("first_contact_reply_at")
        transformed["first_admin_reply_at"] = stats.get("first_admin_reply_at")
        transformed["last_contact_reply_at"] = stats.get("last_contact_reply_at")
        transformed["last_admin_reply_at"] = stats.get("last_admin_reply_at")
        transformed["median_admin_reply_time"] = stats.get("median_admin_reply_time")
        transformed["mean_admin_reply_time"] = stats.get("mean_admin_reply_time")
    
    # Flatten conversation parts count
    if "conversation_parts" in transformed and isinstance(transformed["conversation_parts"], dict):
        parts = transformed["conversation_parts"]
        transformed["conversation_parts_count"] = parts.get("total_count", 0)
    
    return transformed


def build_incremental_query(
    field: str,
    start_value: Any,
    end_value: Optional[Any] = None,
) -> Dict[str, Any]:
    """
    Build a search query for incremental loading.

    Args:
        field: Field to filter on
        start_value: Start value (inclusive)
        end_value: Optional end value (inclusive)

    Returns:
        Query dict for Intercom Search API
    """
    conditions = [
        {
            "field": field,
            "operator": ">=",
            "value": start_value,
        }
    ]
    
    if end_value is not None:
        conditions.append({
            "field": field,
            "operator": "<=",
            "value": end_value,
        })
    
    if len(conditions) == 1:
        return conditions[0]
    else:
        return {
            "operator": "AND",
            "value": conditions,
        }