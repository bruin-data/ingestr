from datetime import datetime
import io
from typing import Iterable
import pandas as pd
import pendulum
import requests

import dlt
from dlt.common.typing import TDataItem
from dlt.sources import DltResource
from pendulum import Date
from dlt.sources.helpers.requests import Client
from typing import Iterator
from dlt.common.time import ensure_pendulum_datetime


@dlt.source(max_table_nesting=0)
def applovin_max_source(
) -> Iterator[DltResource]:
    start_date = "2025-01-09"

    @dlt.resource(name="ad_revenue_report",
                  write_disposition="merge",
                  merge_key="_partition_date",
                  )  
    def fetch_applovin_report(
        dateTime=(
            dlt.sources.incremental(
                "_partition_date",
                initial_value=start_date,
            )
        ),
    ) -> Iterator[dict]:
        url = "https://r.applovin.com/max/userAdRevenueReport"
        date = dateTime.last_value
        if date is None:
            date = start_date
        
        platforms = ["ios","fireos", "android"]

        for platform in platforms:
            params = {
                "api_key": "",
                    "date": date,
                    "platform": platform,
                    "application": "",
                    "aggregated": "false",
            }
        
            print("creating client", pendulum.now())
            client = create_client()
            print("getting response", pendulum.now())
            response = client.get(url=url, params=params)
            print("repsone url", pendulum.now())
            if response.status_code == 200:
                response_url = response.json().get("ad_revenue_report_url")
                print("read csv", pendulum.now())
                df = pd.read_csv(response_url)
                print("columnst", pendulum.now())
                df.columns = df.columns.str.replace(" ", "_").str.lower()
                print("to date", pendulum.now())
                df["date"] = pd.to_datetime(df["date"])
                print("partititon date", pendulum.now())
                df["_partition_date"] = df["date"].dt.strftime("%Y-%m-%d")
                print("above yield", pendulum.now())
            else:
                print("error", response.status_code)

        yield df
    
    return fetch_applovin_report
    
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