import requests
from dlt.sources.helpers.requests import Client

class AppsflyerAPI:
    def retry_on_limit(
            response: requests.Response, exception: BaseException
        ) -> bool:
            return response.status_code == 429
    
    request_client = Client(
            request_timeout=8.0,
            raise_for_status=False,
            retry_condition=retry_on_limit,
            request_max_attempts=12,
            request_backoff_factor=2,
        ).session 