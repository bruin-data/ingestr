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


def flat_structure(items,time_zone="UTC"):
    for item in items:
        if "dimensions" in item:
            for key, value in item["dimensions"].items():
                if key == "stat_time_day":
                    item["stat_time_day"] = ensure_pendulum_datetime(value).in_tz(time_zone)
                elif key == "stat_time_hour":
                    item["stat_time_hour"] = ensure_pendulum_datetime(value).in_tz(time_zone)
                else:
                    item[key] = value
            del item["dimensions"]
           
            for key, value in item["metrics"].items():
                item[key] = value
            del item["metrics"]

    return items


class TikTokAPI:
    def __init__(self, access_token, time_zone):
        self.headers = {
            "Access-Token": access_token, 
        }
        self.time_zone = time_zone

    def fetch_pages(
        self, advertiser_id: str, start_time, end_time, dimensions, metrics, filters
    ) -> list:
        data_level_mapping = {
            "advertiser_id": "AUCTION_ADVERTISER",
            "campaign_id": "AUCTION_CAMPAIGN",
            "adgroup_id": "AUCTION_ADGROUP",
        }
        
        data_level = "AUCTION_AD"
        for id_dimension in dimensions:
            if id_dimension in data_level_mapping:
                data_level = data_level_mapping[id_dimension]
                break

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
            "page_size": 1000,
            "dimensions": json.dumps(dimensions),
            "metrics": json.dumps(metrics),
        }

        while True:
            self.params["page"] = current_page
            response = create_client().get(
                url=BASE_URL, headers=self.headers, params=self.params
            )

            result = response.json()
            if result.get("message") != "OK":
                raise ValueError(result.get("message", ""))

            result_data = result.get("data", {})
            items = result_data.get("list", [])
            
            flat_structure(items=items,time_zone= self.time_zone)

            all_items.extend(items)
            page_info = result_data.get("page_info", {})
            total_pages = page_info.get("total_page", 1)

            if current_page >= total_pages:
                break

            current_page += 1
        return all_items

    def fetch_reports(
        self, start_time, end_time, advertiser_id, dimensions, metrics, filters
    ):
        for item in self.fetch_pages(
            advertiser_id = advertiser_id,
            start_time = start_time,
            end_time = end_time,
            dimensions = dimensions,
            metrics = metrics,
            filters = None,
        ):
            yield item
