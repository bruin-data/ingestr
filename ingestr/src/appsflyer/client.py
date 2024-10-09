from typing import Optional

import requests
from dlt.sources.helpers.requests import Client
from requests.exceptions import HTTPError

DEFAULT_GROUPING = ["c", "geo", "app_id", "install_time"]
DEFAULT_KPIS = [
    "impressions",
    "clicks",
    "installs",
    "cost",
    "revenue",
    "average_ecpi",
    "loyal_users",
    "uninstalls",
    "roi",
]


class AppsflyerClient:
    def __init__(self, api_key: str):
        self.api_key = api_key
        self.uri = "https://hq1.appsflyer.com/api/master-agg-data/v4/app/all"

    def __get_headers(self):
        return {
            "Authorization": f"{self.api_key}",
            "accept": "text/json",
        }

    def _fetch_data(
        self,
        from_date: str,
        to_date: str,
        maximum_rows=1000000,
        dimensions=DEFAULT_GROUPING,
        metrics=DEFAULT_KPIS,
    ):
        params = {
            "from": from_date,
            "to": to_date,
            "groupings": ",".join(dimensions),
            "kpis": ",".join(metrics),
            "format": "json",
            "maximum_rows": maximum_rows,
        }

        def retry_on_limit(
            response: Optional[requests.Response], exception: Optional[BaseException]
        ) -> bool:
            return (
                isinstance(response, requests.Response) and response.status_code == 429
            )

        request_client = Client(
            request_timeout=10.0,
            raise_for_status=False,
            retry_condition=retry_on_limit,
            request_max_attempts=12,
            request_backoff_factor=2,
        ).session

        try:
            response = request_client.get(
                url=self.uri, headers=self.__get_headers(), params=params
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
        metrics = DEFAULT_KPIS + [
            "cohort_day_1_revenue_per_user",
            "cohort_day_1_total_revenue_per_user",
            "cohort_day_3_revenue_per_user",
            "cohort_day_3_total_revenue_per_user",
            "cohort_day_7_total_revenue_per_user",
            "cohort_day_7_revenue_per_user",
            "cohort_day_14_total_revenue_per_user",
            "cohort_day_14_revenue_per_user",
            "cohort_day_21_total_revenue_per_user",
            "cohort_day_21_revenue_per_user",
            "retention_day_7",
        ]
        return self._fetch_data(start_date, end_date, metrics=metrics)

    def fetch_creatives(
        self,
        start_date: str,
        end_date: str,
    ):
        dimensions = DEFAULT_GROUPING + ["af_adset_id", "af_adset", "af_ad_id"]
        return self._fetch_data(start_date, end_date, dimensions=dimensions)
