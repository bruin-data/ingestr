from datetime import timedelta
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
    start_date: Date,
    applications: list[str],
    api_key: str,
    end_date: Date | None,
) -> DltResource:
    @dlt.resource(
        name="user_ad_revenue",
        write_disposition="merge",
        merge_key="partition_date",
        columns={
            "partition_date": {"data_type": "date", "partition": True},
        },
    )
    def fetch_ad_revenue_report(
        dateTime=(
            dlt.sources.incremental(
                "partition_date",
                initial_value=start_date,
                end_value=end_date,
                range_start="closed",
                range_end="closed",
            )
        ),
    ) -> Iterator[dict]:
        url = "https://r.applovin.com/max/userAdRevenueReport"
        start_date = dateTime.last_value

        if dateTime.end_value is None:
            end_date = (pendulum.yesterday("UTC")).date()
        else:
            end_date = dateTime.end_value

        client = create_client()
        platforms = ["ios", "android", "fireos"]

        for app in applications:
            current_date = start_date
            while current_date <= end_date:
                for platform in platforms:
                    df = get_data(
                        url=url,
                        current_date=current_date,
                        application=app,
                        api_key=api_key,
                        client=client,
                        platform=platform,
                    )
                    if df is not None:
                        yield df
                current_date = current_date + timedelta(days=1)

    return fetch_ad_revenue_report


def create_client() -> requests.Session:
    return Client(
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
    url: str,
    current_date: Date,
    application: str,
    api_key: str,
    platform: str,
    client: requests.Session,
):
    params = {
        "api_key": api_key,
        "date": current_date.isoformat(),
        "platform": platform,
        "application": application,
        "aggregated": "false",
    }

    response = client.get(url=url, params=params)

    if response.status_code != 200:
        if response.status_code == 404:
            if "No Mediation App Id found for platform" in response.text:
                return None
        error_message = (
            f"AppLovin MAX API error (status {response.status_code}): {response.text}"
        )
        raise requests.HTTPError(error_message)

    response_url = response.json().get("ad_revenue_report_url")
    df = pd.read_csv(response_url)
    df["Date"] = pd.to_datetime(df["Date"])
    df["partition_date"] = df["Date"].dt.date
    return df
