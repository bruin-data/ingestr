from datetime import datetime, timedelta
from io import StringIO

import pandas as pd
import requests
from dlt.sources.helpers.requests import Client


class AppsflyerAPI:
    def __init__(self):
        self.request_client = Client(
            request_timeout=8.0,
            raise_for_status=False,
            retry_condition=self.retry_on_limit,
            request_max_attempts=5,
            request_backoff_factor=10,
        ).session
      
    @staticmethod
    def retry_on_limit(response: requests.Response, exception: BaseException) -> bool:
        return response.status_code == 429

    def fetch_data(
        self,
        app_id,
        api_key,
        from_date= "2024-09-10 17:00:00",
        to_date= "2024-09-10 18:00:00",
        maximum_rows= 1000000
    ):
        url = f"https://hq1.appsflyer.com/api/raw-data/export/app/{app_id}/installs_report/v5"
        headers = {"accept": "text/csv", "Authorization": f"Bearer {api_key}"}
        all_data = pd.DataFrame()
        current_to = datetime.strptime(to_date, "%Y-%m-%d %H:%M:%S")
        final_from = datetime.strptime(from_date, "%Y-%m-%d %H:%M:%S")

        while current_to > final_from:
            current_from = final_from
            while True:
                params = {
                    "from": current_from.strftime("%Y-%m-%d %H:%M"),
                    "to": current_to.strftime("%Y-%m-%d %H:%M"),
                    "maximum_rows": maximum_rows,
                }

                response = self.request_client.get(
                    url=url, headers=headers, params=params
                )

                if response.status_code == 200:
                    csv_data = StringIO(response.text)
                    df = pd.read_csv(csv_data)

                    if df.empty:
                        break

                    all_data = pd.concat([all_data, df], ignore_index=True)
                    if len(df) >= maximum_rows:
                        min_event_time = df["Event Time"].min()
                        print("Minimum event time found", min_event_time)
                        current_to = datetime.strptime(
                            min_event_time, "%Y-%m-%d %H:%M:%S"
                        )
                    else:
                        break
                else:
                    print("Failed to fetch data", response.status_code)
                    break

        
        yield all_data
