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
    start_date: str,
    application: str,
    api_key: str,
    end_date: str,
) -> Iterator[DltResource]:
    
    @dlt.resource(name="ad_revenue_report",
                  write_disposition="merge",
                  merge_key="_partition_date",
                  )  
    def fetch_applovin_report(
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
        start_date = dateTime.last_value
        end_date = dateTime.end_value
        
        yield get_data(url=url, start_date=start_date, end_date=end_date, application=application, api_key=api_key)
    
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

client = create_client()
def get_data(url: str, start_date:str, end_date:str, application: str, api_key: str):
    platforms = ["android"]
    current_date_obj = pendulum.parse(start_date)
    end_date_obj = pendulum.parse(end_date)
    while current_date_obj <= end_date_obj:
        for platform in platforms:
            params = {
                "api_key": api_key,
                "date": current_date_obj.format("YYYY-MM-DD"),
                "platform": platform,
                "application": application,
                "aggregated": "false",
            }
        
        response = client.get(url=url, params=params)
       
        if response.status_code == 200:
            response_url = response.json().get("ad_revenue_report_url")
            df = pd.read_csv(response_url)
            df.columns = df.columns.str.replace(" ", "_").str.lower()
            df["date"] = pd.to_datetime(df["date"])
            df["_partition_date"] = df["date"].dt.strftime("%Y-%m-%d")
            yield df
        else:
            print("platform:", platform, " status code", response.status_code)
        current_date_obj = current_date_obj.add(days=1)
        print("current_date", current_date_obj)
