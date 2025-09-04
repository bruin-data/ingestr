"""Docebo API Client for handling authentication and paginated requests."""

from datetime import datetime
from typing import Any, Dict, Iterator, Optional

import requests


class DoceboClient:
    """Client for interacting with Docebo LMS API."""
    
    def __init__(
        self,
        base_url: str,
        client_id: str,
        client_secret: str,
        username: Optional[str] = None,
        password: Optional[str] = None,
    ):
        """
        Initialize Docebo API client.
        
        Args:
            base_url: The base URL of your Docebo instance
            client_id: OAuth2 client ID
            client_secret: OAuth2 client secret
            username: Optional username for password grant type
            password: Optional password for password grant type
        """
        self.base_url = base_url.rstrip('/')
        self.client_id = client_id
        self.client_secret = client_secret
        self.username = username
        self.password = password
        self._access_token = None
    
    def get_access_token(self) -> str:
        """
        Get or refresh OAuth2 access token.
        
        Returns:
            Access token string
            
        Raises:
            Exception: If authentication fails
        """
        if self._access_token:
            return self._access_token
            
        auth_endpoint = f"{self.base_url}/oauth2/token"
        
        # Use client_credentials grant type if no username/password provided
        if not self.username or not self.password:
            data = {
                "client_id": self.client_id,
                "client_secret": self.client_secret,
                "grant_type": "client_credentials",
                "scope": "api",
            }
        else:
            data = {
                "client_id": self.client_id,
                "client_secret": self.client_secret,
                "username": self.username,
                "password": self.password,
                "grant_type": "password",
                "scope": "api",
            }
        
        response = requests.post(url=auth_endpoint, data=data)
        response.raise_for_status()
        token_data = response.json()
        self._access_token = token_data.get("access_token")
        
        if not self._access_token:
            raise Exception("Failed to obtain access token from Docebo")
            
        return self._access_token
    
    def get_paginated_data(
        self,
        endpoint: str,
        page_size: int = 200,
        params: Optional[Dict[str, Any]] = None,
    ) -> Iterator[Dict[str, Any]]:
        """
        Fetch paginated data from a Docebo API endpoint.
        
        Args:
            endpoint: API endpoint path (e.g., "manage/v1/user")
            page_size: Number of items per page
            params: Additional query parameters
            
        Yields:
            Batches of items from the API
        """
        url = f"{self.base_url}/{endpoint}"
        headers = {"authorization": f"Bearer {self.get_access_token()}"}
        
        page = 1
        has_more_data = True
        
        while has_more_data:
            request_params = {"page": page, "page_size": page_size}
            if params:
                request_params.update(params)
            
            response = requests.get(url=url, headers=headers, params=request_params)
            response.raise_for_status()
            data = response.json()
            
            # Handle paginated response structure
            if "data" in data:
                # Most Docebo endpoints return data in this structure
                if "items" in data["data"]:
                    items = data["data"]["items"]
                    if items:
                        yield items
                    
                    # Check for more pages
                    has_more_data = data["data"].get("has_more_data", False)
                    if has_more_data and "total_page_count" in data["data"]:
                        total_pages = data["data"]["total_page_count"]
                        if page >= total_pages:
                            has_more_data = False
                # Some endpoints might return data directly as a list
                elif isinstance(data["data"], list):
                    items = data["data"]
                    if items:
                        yield items
                    # For direct list responses, check if we got a full page
                    has_more_data = len(items) == page_size
                else:
                    has_more_data = False
            # Some endpoints might return items directly
            elif isinstance(data, list):
                if data:
                    yield data
                has_more_data = len(data) == page_size
            else:
                has_more_data = False
            
            page += 1
    
    def fetch_users(self) -> Iterator[Dict[str, Any]]:
        """
        Fetch all users from Docebo.
        
        Yields:
            Batches of user data
        """
        yield from self.get_paginated_data("manage/v1/user")
    
    def fetch_courses(self) -> Iterator[Dict[str, Any]]:
        """
        Fetch all courses from Docebo.
        
        Yields:
            Batches of course data
        """
        yield from self.get_paginated_data("learn/v1/courses")


def normalize_docebo_dates(item: Dict[str, Any]) -> Dict[str, Any]:
    """
    Normalize Docebo date fields that contain '0000-00-00' to Unix epoch (1970-01-01).
    
    Args:
        item: Dictionary containing data from Docebo API
        
    Returns:
        Dictionary with normalized date fields
    """
    # Unix epoch datetime (1970-01-01 00:00:00 UTC)
    epoch_datetime = datetime(1970, 1, 1)
    
    # Date fields that might contain '0000-00-00'
    # Add more fields as needed for different resources
    date_fields = [
        'last_access_date',
        'last_update', 
        'creation_date',
        'date_begin',  # Course field
        'date_end',    # Course field
        'date_publish', # Course field
        'date_unpublish', # Course field
    ]
    
    for field in date_fields:
        if field in item:
            # Handle string dates that are '0000-00-00' or '0000-00-00 00:00:00'
            if isinstance(item[field], str):
                if item[field].startswith('0000-00-00'):
                    item[field] = epoch_datetime
                # Handle other invalid date formats
                elif item[field] in ['', '0', 'null', 'NULL']:
                    item[field] = None
            # Handle cases where the field might be None or empty
            elif not item[field]:
                item[field] = None
    
    return item