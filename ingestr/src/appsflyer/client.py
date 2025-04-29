from typing import Optional

import requests
from dlt.sources.helpers.requests import Client
from requests.exceptions import HTTPError


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
        dimensions: list[str],
        metrics: list[str],
        maximum_rows=1000000,
    ):
        excluded_metrics = exclude_metrics_for_date_range(metrics, from_date, to_date)
        included_metrics = [
            metric for metric in metrics if metric not in excluded_metrics
        ]

        params = {
            "from": from_date,
            "to": to_date,
            "groupings": ",".join(dimensions),
            "kpis": ",".join(included_metrics),
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
                yield standardize_keys(result, excluded_metrics)
            else:
                raise HTTPError(
                    f"Request failed with status code: {response.status_code}: {response.text}"
                )

        except requests.RequestException as e:
            raise HTTPError(f"Request failed: {e}")


def standardize_keys(data: list[dict], excluded_metrics: list[str]) -> list[dict]:
    def fix_key(key: str) -> str:
        return key.lower().replace("-", "").replace("  ", "_").replace(" ", "_")

    standardized = []
    for item in data:
        standardized_item = {}
        for key, value in item.items():
            standardized_item[fix_key(key)] = value

        for metric in excluded_metrics:
            if metric not in standardized_item:
                standardized_item[fix_key(metric)] = None

        standardized.append(standardized_item)

    return standardized


def exclude_metrics_for_date_range(
    metrics: list[str], from_date: str, to_date: str
) -> list[str]:
    """
    Some of the cohort metrics are not available if there hasn't been enough time to have data for that cohort.
    This means if you request data for yesterday with cohort day 7 metrics, you will get an error because 7 days hasn't passed yet.
    One would expect the API to handle this gracefully, but it doesn't.

    This function will exclude the metrics that are not available for the given date range.
    """
    import pendulum

    excluded_metrics = []
    days_between_today_and_end = (pendulum.now() - pendulum.parse(to_date)).days  # type: ignore
    for metric in metrics:
        if "cohort_day_" in metric:
            day_count = int(metric.split("_")[2])
            if days_between_today_and_end <= day_count:
                excluded_metrics.append(metric)
    return excluded_metrics
