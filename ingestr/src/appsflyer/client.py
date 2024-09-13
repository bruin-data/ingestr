import requests
from dlt.sources.helpers.requests import Client

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

    def __get_headers(self):
        return {
            "Authorization": f"{self.api_key}",
            "accept": "text/json",
        }

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

        def retry_on_limit(
            response: requests.Response, exception: BaseException
        ) -> bool:
            return response.status_code == 429

        request_client = Client(
            request_timeout=10.0,
            raise_for_status=False,
            retry_condition=retry_on_limit,
            request_max_attempts=12,
            request_backoff_factor=2,
        ).session

        response = request_client.get(
            url=url, headers=self.__get_headers(), params=params
        )

        if response.status_code == 200:
            result = response.json()
            yield result

    def fetch_campaigns(
        self,
        start_date: str,
        end_date: str,
    ):
        url = f"{BASE_URL}/master-agg-data/v4/app/all"
        return self._fetch_data(url, start_date, end_date)
