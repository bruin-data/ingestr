from typing import Optional, Dict, Any
import json
import pendulum
import requests
from dlt.sources.helpers.requests import Client
from requests.exceptions import HTTPError

# Default dimensions based on Criteo API documentation
DEFAULT_DIMENSIONS = ["AdsetId", "Day"]

# Default metrics for campaign statistics
DEFAULT_METRICS = [
    "Displays",
    "Clicks", 
    "AdvertiserCost",
    "Ctr",
    "Cpc",
    "Cpm"
]

# Available dimensions from Criteo API
AVAILABLE_DIMENSIONS = [
    "AdsetId", "CampaignId", "AdvertiserId", "Day", "Week", "Month", "Year",
    "Hour", "CategoryId", "ProductId", "Country", "Region", "City", "Device",
    "Os", "Browser", "Environment"
]

# Available metrics from Criteo API  
AVAILABLE_METRICS = [
    "Displays", "Clicks", "AdvertiserCost", "Ctr", "Cpc", "Cpm",
    "PostViewConversions", "PostClickConversions", "SalesPostView", 
    "SalesPostClick", "Revenue", "RevenuePostView", "RevenuePostClick"
]

# Supported currencies
SUPPORTED_CURRENCIES = [
    "USD", "EUR", "GBP", "JPY", "AUD", "CAD", "CHF", "CNY", "SEK", "NZD",
    "MXN", "SGD", "HKD", "NOK", "TRY", "ZAR", "RUB", "INR", "BRL", "KRW"
]


def retry_on_limit(response: requests.Response, exception: BaseException) -> bool:
    return response.status_code == 429


class CriteoAPI:
    def __init__(self, client_id: str, client_secret: str, access_token: Optional[str] = None):
        """
        Initialize Criteo API client
        
        Args:
            client_id: Criteo API client ID
            client_secret: Criteo API client secret  
            access_token: Optional access token (if already obtained)
        """
        self.client_id = client_id
        self.client_secret = client_secret
        self.access_token = access_token
        self.base_url = "https://api.criteo.com/2025-04"
        
        self.request_client = Client(
            request_timeout=300,  # 5 minute timeout
            raise_for_status=False,
            retry_condition=retry_on_limit,
            request_max_attempts=5,
            request_backoff_factor=2,
        ).session

    def _get_access_token(self) -> str:
        """Get access token using client credentials flow"""
        if self.access_token:
            return self.access_token
            
        auth_url = f"{self.base_url}/oauth2/token"
        
        headers = {
            "Content-Type": "application/x-www-form-urlencoded"
        }
        
        data = {
            "grant_type": "client_credentials",
            "client_id": self.client_id,
            "client_secret": self.client_secret
        }
        
        response = self.request_client.post(auth_url, headers=headers, data=data)
        
        if response.status_code == 200:
            token_data = response.json()
            self.access_token = token_data["access_token"]
            return self.access_token
        else:
            raise HTTPError(
                f"Authentication failed with status code: {response.status_code}, {response.text}"
            )

    def fetch_campaign_statistics(
        self,
        start_date: pendulum.DateTime,
        end_date: pendulum.DateTime,
        dimensions: Optional[list[str]] = None,
        metrics: Optional[list[str]] = None,
        currency: str = "USD",
        advertiser_ids: Optional[list[str]] = None,
        timezone: str = "UTC"
    ) -> Any:
        """
        Fetch campaign statistics from Criteo API
        
        Args:
            start_date: Start date for the report
            end_date: End date for the report  
            dimensions: List of dimensions to include in the report
            metrics: List of metrics to include in the report
            currency: Currency for the report (default: USD)
            advertiser_ids: Optional list of advertiser IDs to filter
            timezone: Timezone for the report (default: UTC)
        """
        if not dimensions:
            dimensions = DEFAULT_DIMENSIONS
        if not metrics:
            metrics = DEFAULT_METRICS
            
        # Validate inputs
        if start_date > end_date:
            raise ValueError(
                f"Invalid date range: Start date ({start_date}) must be earlier than end date ({end_date})"
            )
            
        if currency not in SUPPORTED_CURRENCIES:
            raise ValueError(f"Unsupported currency: {currency}. Supported currencies: {SUPPORTED_CURRENCIES}")
            
        # Get access token
        access_token = self._get_access_token()
        
        headers = {
            "Authorization": f"Bearer {access_token}",
            "Content-Type": "application/json"
        }
        
        # Prepare request body
        request_body = {
            "startDate": start_date.strftime("%Y-%m-%dT%H:%M:%S.000Z"),
            "endDate": end_date.strftime("%Y-%m-%dT%H:%M:%S.000Z"),
            "format": "json",
            "dimensions": dimensions,
            "metrics": metrics,
            "currency": currency,
            "timezone": timezone
        }
        
        # Add advertiser IDs if provided
        if advertiser_ids:
            request_body["advertiserIds"] = ",".join(advertiser_ids)
            
        url = f"{self.base_url}/statistics/report"
        
        response = self.request_client.post(
            url,
            headers=headers,
            data=json.dumps(request_body)
        )
        
        if response.status_code == 200:
            result = response.json()
            
            # Handle different response formats
            if isinstance(result, dict) and "rows" in result:
                # If response has rows structure
                yield from result["rows"]
            elif isinstance(result, list):
                # If response is directly a list of records
                yield from result
            else:
                # If response is a single record
                yield result
        else:
            raise HTTPError(
                f"Request failed with status code: {response.status_code}, {response.text}"
            )

    def validate_dimensions_and_metrics(self, dimensions: list[str], metrics: list[str]) -> bool:
        """
        Validate that provided dimensions and metrics are supported
        
        Args:
            dimensions: List of dimensions to validate
            metrics: List of metrics to validate
            
        Returns:
            True if all dimensions and metrics are valid
            
        Raises:
            ValueError: If any dimension or metric is not supported
        """
        invalid_dimensions = [d for d in dimensions if d not in AVAILABLE_DIMENSIONS]
        if invalid_dimensions:
            raise ValueError(f"Invalid dimensions: {invalid_dimensions}. Available dimensions: {AVAILABLE_DIMENSIONS}")
            
        invalid_metrics = [m for m in metrics if m not in AVAILABLE_METRICS]
        if invalid_metrics:
            raise ValueError(f"Invalid metrics: {invalid_metrics}. Available metrics: {AVAILABLE_METRICS}")
            
        return True 