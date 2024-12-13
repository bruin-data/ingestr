import json

import requests
from dlt.common.time import ensure_pendulum_datetime
from dlt.sources.helpers.requests import Client

BASE_URL = "https://business-api.tiktok.com/open_api/v1.3/report/integrated/get/"


def retry_on_limit(
    response: requests.Response | None, exception: BaseException | None
) -> bool:
    if response is None:
        return False
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
        self.headers = {
            "Access-Token": access_token,
        }

    def fetch_pages(
        self, advertiser_id: str, start_time, end_time, dimensions, metrics
    ) -> list:
        if advertiser_id in dimensions:
            data_level = "AUCTION_ADVERTISER"

        if "campaign_id" in dimensions:
            data_level = "AUCTION_CAMPAIGN"

        if "adgroup_id" in dimensions:
            data_level = "AUCTION_ADGROUP"

        if "ad_id" in dimensions:
            data_level = "AUCTION_AD"

        all_items = []
        current_page = 1
        start_time = ensure_pendulum_datetime(start_time).to_date_string()
        end_time = ensure_pendulum_datetime(end_time).to_date_string()
        self.params = {
            "advertiser_id": advertiser_id,
            "report_type": "BASIC",
            "data_level": data_level,
            "start_date": start_time,
            "end_date": end_time,
            "page_size": 100,
            "dimensions": json.dumps(dimensions),
            "metrics": json.dumps(metrics),
        }

        while True:
            self.params["page"] = current_page
            response = create_client().get(
                url=BASE_URL, headers=self.headers, params=self.params
            )
            print("response", response.json())
            result = response.json()
            items = result.get("data", {}).get("list", [])

            if "stat_time_day" in dimensions:
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

    def fetch_reports(
        self,
        start_time,
        end_time,
        advertiser_id,
        dimensions,
        metrics,
    ):
        return self.fetch_pages(
            advertiser_id=advertiser_id,
            start_time=start_time,
            end_time=end_time,
            dimensions=dimensions,
            metrics=metrics,
        )
