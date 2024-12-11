import json
from typing import Optional

import pendulum
import requests
from dlt.sources.helpers.requests import Client
from requests.exceptions import HTTPError
from dlt.common.time import ensure_pendulum_datetime

# Suggestion filetring: A time range within 6 months is suggested when applying a creation time filter, 
# to ensure that the success and speed of the task won't be affected.


BASE_URL = "https://business-api.tiktok.com/open_api/v1.3/report/integrated/get/"

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
        self, advertiser_id: str, start_time, end_time
    ) -> list:
        all_items = []
        current_page = 1
        start_time = ensure_pendulum_datetime(start_time).to_date_string()
        end_time = ensure_pendulum_datetime(end_time).to_date_string()
        dimensions = ["advertiser_id","stat_time_day"]
        metrics = ["impressions", "clicks", "ctr", "cpc", "cpm"]
        self.params = {
            "advertiser_id": advertiser_id,
            "report_type": "BASIC",
            "data_level": "AUCTION_ADVERTISER",
            "start_date":start_time,
            "end_date":end_time,
            "page_size":100,
            "dimensions":json.dumps(dimensions),
            "metrics":  json.dumps(metrics),
        }
       
        while True:
            self.params["page"] = current_page
            response = create_client().get(url=BASE_URL, headers=self.headers, params=self.params)
            print("response",response.json())
            
            if response.status_code != 200:
                raise HTTPError(f"Error fetching data: {response.status_code} {response.text}")
            
            try:
                result = response.json()
            except ValueError:
                raise HTTPError(f"aomething went wrong")
            
            items = result.get("data", {}).get("list", [])
            for item in items:
                if "dimensions" in item:
                    item["stat_time_day"] = item["dimensions"]["stat_time_day"]

            all_items.extend(items)
            page_info = result.get("page_info", {})
            total_pages = page_info.get("total_page", 1)
            if current_page >= total_pages:
                break

            current_page += 1
     
        return all_items

    def fetch_advertisers_reports_daily(
        self,
        start_time,
        end_time,
        advertiser_id: str,
    ):
        
        return self._fetch_pages(advertiser_id=advertiser_id, start_time=start_time,end_time= end_time)
    