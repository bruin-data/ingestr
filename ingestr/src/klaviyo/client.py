import requests

BASE_URL = "https://a.klaviyo.com/api/"


class KlaviyoClient:
    def __init__(self, api_key: str):
        self.api_key = api_key

    def __get_headers(self):
        return {
            "Authorization": f"Klaviyo-API-Key {self.api_key}",
            "accept": "application/json",
            "revision": "2024-07-15",
        }

    def fetch_events(
        self,
        session: requests.Session,
        start_date: str,
        end_date: str,
    ):
        print("Fetching data from Klaviyo", start_date, end_date, flush=True)
        sort_filter = f"/?sort=-datetime&filter=and(greater-or-equal(datetime,{start_date}),less-than(datetime,{end_date}))"
        url = BASE_URL + "events" + sort_filter

        all_events = []
        while True:
            response = session.get(url=url, headers=self.__get_headers())
            result = response.json()
            events = result.get("data", [])

            for event in events:
                for attribute_key in event["attributes"]:
                    event[attribute_key] = event["attributes"][attribute_key]
                del event["attributes"]

            all_events.extend(events)

            url = result["links"]["next"]
            if url is None:
                break

        return all_events
