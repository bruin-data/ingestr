from typing import Optional

import requests
from dlt.sources.helpers.requests import Client
from requests.exceptions import HTTPError

BASE_URL = "https://hq1.appsflyer.com/api"
DEFAULT_GROUPING = ["af_c_id", "geo", "af_adset", "af_channel", "install_time"]
DEFAULT_KPIS = [
    "impressions",
    "clicks",
    "installs",
    "cost",
    "revenue",
    "roi",
    "average_ecpi",
    "loyal_users",
    "uninstalls",
    "retention_day_7",
    "cr",
    "sessions",
    "arpu_ltv",
    "retention_day_7",
    "retention_rate_day_7",
]


class AppsflyerClient:
    def __init__(self, api_key: str):
        self.api_key = api_key
        self.client = self._create_client()

    def __get_headers(self):
        return {
            "Authorization": f"{self.api_key}",
            "accept": "text/json",
        }

    def _create_client(self) -> Client:
        def retry_on_limit(
            response: Optional[requests.Response], exception: Optional[BaseException]
        ) -> bool:
            return (
                isinstance(response, requests.Response) and response.status_code == 429
            )

        return Client(
            request_timeout=10.0,
            raise_for_status=False,
            retry_condition=retry_on_limit,
            request_max_attempts=12,
            request_backoff_factor=2,
        ).session

    def _fetch_data(
        self,
        url: str,
        from_date: str,
        to_date: str,
        maximum_rows=1000000,
    ):
        params = {
            "from": from_date,
            "to": to_date,
            "groupings": ",".join(DEFAULT_GROUPING),
            "kpis": ",".join(DEFAULT_KPIS),
            "format": "json",
            "maximum_rows": maximum_rows,
        }

        try:
            response = self.client.get(
                url=url, headers=self.__get_headers(), params=params
            )

            if response.status_code == 200:
                result = response.json()
                yield result
            else:
                raise HTTPError(
                    f"Request failed with status code: {response.status_code}"
                )

        except requests.RequestException as e:
            raise HTTPError(f"Request failed: {e}")

    def fetch_campaigns(
        self,
        start_date: str,
        end_date: str,
    ):
        url = f"{BASE_URL}/master-agg-data/v4/app/all"
        return self._fetch_data(url, start_date, end_date)
