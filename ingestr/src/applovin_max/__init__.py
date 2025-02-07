from typing import Iterator

import dlt
import pandas as pd  # type: ignore[import-untyped]
import pendulum
import requests
from dlt.sources import DltResource
from dlt.sources.helpers.requests import Client
from pendulum.date import Date


@dlt.source(max_table_nesting=0)
def applovin_max_source(
    start_date: str,
    application: str,
    api_key: str,
    end_date: str | None,
) -> DltResource:
    @dlt.resource(
        name="ad_revenue",
        write_disposition="merge",
        merge_key="_partition_date",
    )
    def fetch_ad_revenue_report(
        dateTime=(
            dlt.sources.incremental(
                "_partition_date",
                initial_value=start_date,
                end_value=end_date,
                range_start="closed",
                range_end="closed",
            )
        ),
    ) -> Iterator[dict]:
        url = "https://r.applovin.com/max/userAdRevenueReport"
        start_date = pendulum.from_format(dateTime.last_value, "YYYY-MM-DD").date()
        if dateTime.end_value is None:
            end_date = (pendulum.yesterday("UTC")).date()
        else:
            end_date = pendulum.from_format(dateTime.end_value, "YYYY-MM-DD").date()
        yield get_data(
            url=url,
            start_date=start_date,
            end_date=end_date,
            application=application,
            api_key=api_key,
        )

    return fetch_ad_revenue_report


def create_client() -> requests.Session:
    return Client(
        request_timeout=10.0,
        raise_for_status=False,
        retry_condition=retry_on_limit,
        request_max_attempts=12,
    ).session


def retry_on_limit(
    response: requests.Response | None, exception: BaseException | None
) -> bool:
    if response is None:
        return False
    return response.status_code == 429


def get_data(
    url: str, start_date: Date, end_date: Date, application: str, api_key: str
):
    client = create_client()
    platforms = ["ios", "android", "fireos"]
    current_date = start_date
    while current_date <= end_date:
        for platform in platforms:
            params = {
                "api_key": api_key,
                "date": current_date.strftime("%Y-%m-%d"),
                "platform": platform,
                "application": application,
                "aggregated": "false",
            }

            response = client.get(url=url, params=params)

            if response.status_code == 400:
                raise ValueError(response.text)

            if response.status_code != 200:
                continue

            response_url = response.json().get("ad_revenue_report_url")
            df = pd.read_csv(response_url)
            df["Date"] = pd.to_datetime(df["Date"])
            df["_partition_date"] = df["Date"].dt.strftime("%Y-%m-%d")
            yield df

        current_date = current_date.add(days=1)
