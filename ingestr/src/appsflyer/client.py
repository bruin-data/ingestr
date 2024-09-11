from urllib.parse import urlencode

import pendulum
import requests

from datetime import datetime, timedelta
from io import StringIO

import pandas as pd

BASE_URL = "https://hq1.appsflyer.com/api/raw-data/export/app/"

class AppsflyerClient:
    def __init__(self, api_key: str):
        self.api_key = api_key

    def __get_headers(self):
        return {
            "Authorization": f"{self.api_key}",
            "accept": "text/csv",
        }

    def _fetch_pages(
        self, 
        url:str,
        session:requests.Session,
        from_date="2024-09-10 17:00:00",
        to_date="2024-09-10 18:00:00",
        maximum_rows=1000000,
    ):
        all_data = pd.DataFrame()
        end = datetime.strptime(to_date, "%Y-%m-%d %H:%M:%S")
        start = datetime.strptime(from_date, "%Y-%m-%d %H:%M:%S")

        while end > start:
            while True:
                params = {
                    "from":start.strftime("%Y-%m-%d %H:%M"),
                    "to": end.strftime("%Y-%m-%d %H:%M"),
                    "maximum_rows": maximum_rows,
                }

                response = session.get(url= url, headers=self.__get_headers(),params=params)

                if response.status_code == 200:
                    csv_data = StringIO(response.text)
                    df = pd.read_csv(csv_data)

                    if df.empty:
                        break

                    all_data = pd.concat([all_data, df], ignore_index=True)

                    if len(df) >= maximum_rows:
                        min_event_time = df["Event Time"].min()
                        print("Minimum event time found", min_event_time)
                        end = datetime.strptime(
                            min_event_time, "%Y-%m-%d %H:%M:%S"
                        )
                    else:
                        break
                else:
                    print("Failed to fetch data", response.status_code)
                    break

       
        yield all_data

    def fetch_installs(
        self,
        session: requests.Session,
        start_date: str,
        end_date: str,
        app_id:str
    ):
        print(f"Fetching installs for {start_date} to {end_date}")
        url = f"{BASE_URL}/{app_id}/installs_report/v5"
        return self._fetch_pages( url,session)