import requests
from dlt.sources.helpers.requests import Client

class KlaviyoAPI:
    def fetch_data_event(endpoint, datetime, api_key):
        base_url = "https://a.klaviyo.com/api/"
        headers = {
            "Authorization": f"Klaviyo-API-Key {api_key}",
            "accept": "application/json",
            "revision": "2024-07-15",
        }
        sort_filter = f"/?sort=-datetime&filter=greater-than(datetime,{datetime})"
        url = base_url + endpoint + sort_filter

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

        while True:
            response = request_client.get(url=url, headers=headers)
            result = response.json()
            events = result.get("data", [])

            for event in events:
                for attribute_key in event["attributes"]:
                    event[attribute_key] = event["attributes"][attribute_key]
                del event["attributes"]
            yield events

            url = result["links"]["next"]
            if url is None:
                break

    
    def fetch_data_profiles(endpoint, updated, api_key):
        base_url = "https://a.klaviyo.com/api/"
        headers = {
            "Authorization": f"Klaviyo-API-Key {api_key}",
            "accept": "application/json",
            "revision": "2024-07-15",
        }
        sort_filter = f"/?&sort=updated&filter=greater-than(updated,{updated})"
        url = base_url + endpoint + sort_filter
        print("url", url)

        def retry_on_limit(
            response: requests.Response, exception: BaseException
        ) -> bool:
            return response.status_code == 429

        request_client = Client(
            request_timeout=2.0,
            raise_for_status=False,
            retry_condition=retry_on_limit,
            request_max_attempts=12,
            request_backoff_factor=2,
        ).session

        while True:
            response = request_client.get(url=url, headers=headers)
            result = response.json()
            profiles = result.get("data", [])

            for profile in profiles:
                for attribute_key in profile["attributes"]:
                    profile[attribute_key] = profile["attributes"][attribute_key]
                del profile["attributes"]
            yield profiles

            url = result["links"]["next"]
            if url is None:
                break
            
