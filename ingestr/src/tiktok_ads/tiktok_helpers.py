from typing import Optional

import pendulum
import requests
from dlt.sources.helpers.requests import Client
from requests.exceptions import HTTPError

# Suggestion filetring: A time range within 6 months is suggested when applying a creation time filter, 
# to ensure that the success and speed of the task won't be affected.


BASE_URL = "https://business-api.tiktok.com/open_api/v1.3"

def retry_on_limit(response: requests.Response, exception: BaseException) -> bool:
    return response.status_code == 429

def create_client() -> requests.Session:
    return Client(
        request_timeout=10.0,
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
        request_backoff_factor=2,
    ).session

class TikTokAPI:
    def __init__(self, access_token):
         self.headers= {
                "Access-Token": access_token,
                 }

    def _fetch_pages(
        self, url: str, advertiser_id: str, start_time: str, end_time:str
    ) -> list:
        all_items = []
        current_page = 1
        
        self.params = {
            "advertiser_id": advertiser_id,
            "creation_filter_start_time":start_time,
            "creation_filter_end_time":end_time,
            "page_size":100,
        }
        
        while True:
            self.params["page"] = current_page
            response = create_client().get(url=url, headers=self.headers, params=self.params)
            result = response.json()
            items = result.get("data", [])

            all_items.extend(items)
            page_info = result.get("page_info", {})
            total_pages = page_info.get("total_page", 1)
            if current_page >= total_pages:
                break

            current_page += 1

        return all_items

    def fetch_campaigns(
        self,
        start_time: str,
        end_time: str,
        advertiser_id: str,
    ):
        url = f"{BASE_URL}/campaign/get/"
        return self._fetch_pages(url,start_time,end_time,advertiser_id)
    