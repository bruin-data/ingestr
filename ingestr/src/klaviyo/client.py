from urllib.parse import urlencode

import pendulum
import requests

BASE_URL = "https://a.klaviyo.com/api"


class KlaviyoClient:
    def __init__(self, api_key: str):
        self.api_key = api_key

    def __get_headers(self):
        return {
            "Authorization": f"Klaviyo-API-Key {self.api_key}",
            "accept": "application/json",
            "revision": "2024-07-15",
        }

    def _flatten_attributes(self, items: list):
        for event in items:
            if "attributes" not in event:
                continue

            for attribute_key in event["attributes"]:
                event[attribute_key] = event["attributes"][attribute_key]

            del event["attributes"]
        return items

    def _fetch_pages(self, session: requests.Session, url: str) -> list:
        all_items = []

        while True:
            response = session.get(url=url, headers=self.__get_headers())
            result = response.json()
            items = result.get("data", [])
            items = self._flatten_attributes(items)
            all_items.extend(items)

            url = result["links"]["next"]
            if url is None:
                break

        return all_items

    def fetch_events(
        self,
        session: requests.Session,
        start_date: str,
        end_date: str,
    ):
        print(f"Fetching events for {start_date} to {end_date}")
        url = f"{BASE_URL}/events/?sort=-datetime&filter=and(greater-or-equal(datetime,{start_date}),less-than(datetime,{end_date}))"
        return self._fetch_pages(session, url)

    def fetch_metrics(
        self,
        session: requests.Session,
        last_updated: str,
    ):
        print(f"Fetching metrics since {last_updated}")
        url = f"{BASE_URL}/metrics"
        items = self._fetch_pages(session, url)

        last_updated_obj = pendulum.parse(last_updated)
        for item in items:
            updated_at = pendulum.parse(item["updated"])
            if updated_at > last_updated_obj:
                yield item

    def fetch_profiles(
        self,
        session: requests.Session,
        start_date: str,
        end_date: str,
    ):
        print(f"Fetching profiles for {start_date} to {end_date}")
        pendulum_start_date = pendulum.parse(start_date)
        pendulum_start_date = pendulum_start_date.subtract(seconds=1)
        url = f"{BASE_URL}/profiles/?sort=updated&filter=and(greater-than(updated,{pendulum_start_date.isoformat()}),less-than(updated,{end_date}))"
        return self._fetch_pages(session, url)

    def fetch_campaigns(
        self,
        session: requests.Session,
        start_date: str,
        end_date: str,
        campaign_type: str,
    ):
        print(f"Fetching {campaign_type} campaigns for {start_date} to {end_date}")

        base_url = f"{BASE_URL}/campaigns/"
        params = {
            "sort": "updated_at",
            "filter": f"and(equals(messages.channel,'{campaign_type}'),greater-or-equal(updated_at,{start_date}),less-than(updated_at,{end_date}))",
        }
        url = f"{base_url}?{urlencode(params)}"
        pages = self._fetch_pages(session, url)
        for page in pages:
            page["campaign_type"] = campaign_type

        return pages
