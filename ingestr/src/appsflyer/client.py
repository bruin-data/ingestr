from datetime import datetime
from io import StringIO

import pandas as pd
import requests

BASE_URL = "https://hq1.appsflyer.com/api/raw-data/export/app"


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
        url: str,
        session: requests.Session,
        from_date: str,
        to_date: str,
        maximum_rows=1000000,
    ):
        all_data = pd.DataFrame()
        end = datetime.strptime(to_date, "%Y-%m-%d %H:%M:%S")
        start = datetime.strptime(from_date, "%Y-%m-%d %H:%M:%S")

        while end > start:
            while True:
                params = {
                    "from": start.strftime("%Y-%m-%d %H:%M"),
                    "to": end.strftime("%Y-%m-%d %H:%M"),
                    "timezone": "Europe/Berlin",
                    "maximum_rows": maximum_rows,
                }

                response = session.get(
                    url=url, headers=self.__get_headers(), params=params
                )
                print("response", response)
                if response.status_code == 200:
                    csv_data = StringIO(response.text)
                    df = pd.read_csv(csv_data)

                    if df.empty:
                        break

                    all_data = pd.concat([all_data, df], ignore_index=True)

                    if len(df) >= maximum_rows:
                        min_event_time = df["Event Time"].min()
                        end = datetime.strptime(min_event_time, "%Y-%m-%d %H:%M:%S")
                    else:
                        break
                else:
                    print("Failed to fetch data", response.status_code)
                    break

        all_data["event_date"] = pd.to_datetime(df["Event Time"])
        yield all_data

    def fetch_installs(
        self, session: requests.Session, start_date: str, end_date: str, app_id: str
    ):
        print(f"Fetching installs for {start_date} to {end_date}")
        url = f"{BASE_URL}/{app_id}/installs_report/v5"
        print("url", url)
        return self._fetch_pages(url, session, start_date, end_date)
