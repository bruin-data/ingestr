from dlt.sources.helpers.requests import Client
import requests

import requests

class AdjustAPI:
    def __init__(self, start_date, end_date, api_key):
        self.start_date = start_date
        self.end_date = end_date
        self.api_key = api_key
        self.default_dimensions = "day,app,store_type,channel,country"
        self.default_metrics = (
            "network_cost,all_revenue_total_d0,ad_revenue_total_d0,revenue_total_d0,"
            "all_revenue_total_d1,ad_revenue_total_d1,revenue_total_d1,all_revenue_total_d3,"
            "ad_revenue_total_d3,revenue_total_d3,all_revenue_total_d7,ad_revenue_total_d7,"
            "revenue_total_d7,all_revenue_total_d14,ad_revenue_total_d14,revenue_total_d14,"
            "all_revenue_total_d21"
        )
        self.uri = "https://automate.adjust.com/reports-service/report"

    def fetch_report_data(self):
        headers = {"Authorization": f"Bearer {self.api_key}"}
        params = {
            "date_period": f"{self.start_date}:{self.end_date}",
            "dimensions": self.default_dimensions,
            "metrics": self.default_metrics,
            "utc_offset": "+00:00",
            "ad_spend_mode": "network",
            "attribution_source": "first",
            "attribution_type": "all",
            "cohort_maturity": "immature",
            "reattributed": "all",
            "sandbox": "false"
        }

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

        response = request_client.get(self.uri, headers=headers, params=params)

        if response.status_code == 200:
            result = response.json()
            res = result.get("rows", [])
            yield res
        else:
            return 
