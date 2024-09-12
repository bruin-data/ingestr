import requests
from dlt.sources.helpers.requests import Client
from requests.exceptions import HTTPError

DEFAULT_DIMENSIONS = ["campaign", "day", "app", "store_type", "channel", "country"]

DEFAULT_METRICS = [
    "network_cost",
    "all_revenue_total_d0",
    "ad_revenue_total_d0",
    "revenue_total_d0",
    "all_revenue_total_d1",
    "ad_revenue_total_d1",
    "revenue_total_d1",
    "all_revenue_total_d3",
    "ad_revenue_total_d3",
    "revenue_total_d3",
    "all_revenue_total_d7",
    "ad_revenue_total_d7",
    "revenue_total_d7",
    "all_revenue_total_d14",
    "ad_revenue_total_d14",
    "revenue_total_d14",
    "all_revenue_total_d21",
]


class AdjustAPI:
    def __init__(self, api_key):
        self.api_key = api_key
        self.uri = "https://automate.adjust.com/reports-service/report"

    def fetch_report_data(
        self,
        start_date,
        end_date,
        dimensions=DEFAULT_DIMENSIONS,
        metrics=DEFAULT_METRICS,
        utc_offset="+00:00",
        ad_spend_mode="network",
        attribution_source="first",
        attribution_type="all",
        cohort_maturity="immature",
        reattributed="all",
        sandbox="false",
    ):
        headers = {"Authorization": f"Bearer {self.api_key}"}
        comma_separated_dimensions = ",".join(dimensions)
        comma_separated_metrics = ",".join(metrics)
        params = {
            "date_period": f"{start_date}:{end_date}",
            "dimensions": comma_separated_dimensions,
            "metrics": comma_separated_metrics,
            "utc_offset": utc_offset,
            "ad_spend_mode": ad_spend_mode,
            "attribution_source": attribution_source,
            "attribution_type": attribution_type,
            "cohort_maturity": cohort_maturity,
            "reattributed": reattributed,
            "sandbox": sandbox,
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
            items = result.get("rows", [])
            yield items
        else:
            raise HTTPError(f"Request failed with status code: {response.status_code}")
