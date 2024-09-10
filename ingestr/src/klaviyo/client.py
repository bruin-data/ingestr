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

    def _fetch_pages(
        self, session: requests.Session, url: str, flat: bool = True
    ) -> list:
        all_items = []
        while True:
            response = session.get(url=url, headers=self.__get_headers())
            result = response.json()
            items = result.get("data", [])

            if flat:
                items = self._flatten_attributes(items)

            all_items.extend(items)
            nextURL = result.get("links", {}).get("next")
            if nextURL is None:
                break

            url = nextURL

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

    def fetch_tag(self, session: requests.Session):
        url = f"{BASE_URL}/tags"
        return self._fetch_pages(session, url, False)

    def fetch_catalog_variant(
        self,
        session: requests.Session,
        last_updated: str,
    ):
        url = f"{BASE_URL}/catalog-variants"
        items = self._fetch_pages(session, url)
        last_updated_obj = pendulum.parse(last_updated)

        for item in items:
            updated_at = pendulum.parse(item["updated"])
            if updated_at > last_updated_obj:
                yield item

    def fetch_coupons(self, session: requests.Session):
        url = f"{BASE_URL}/coupons"
        return self._fetch_pages(session, url, False)

    def fetch_catalog_categories(
        self,
        session: requests.Session,
        last_updated: str,
    ):
        url = f"{BASE_URL}/catalog-categories"
        items = self._fetch_pages(session, url)
        last_updated_obj = pendulum.parse(last_updated)

        for item in items:
            updated_at = pendulum.parse(item["updated"])
            if updated_at > last_updated_obj:
                yield item

    def fetch_catalog_item(
        self,
        session: requests.Session,
        last_updated: str,
    ):
        url = f"{BASE_URL}/catalog-items"
        items = self._fetch_pages(session, url)
        last_updated_obj = pendulum.parse(last_updated)

        for item in items:
            updated_at = pendulum.parse(item["updated"])
            if updated_at > last_updated_obj:
                yield item

    def fetch_forms(
        self,
        session: requests.Session,
        start_date: str,
        end_date: str,
    ):
        print(f"Fetching forms for {start_date} to {end_date}")
        url = f"{BASE_URL}/forms/?sort=-updated_at&filter=and(greater-or-equal(updated_at,{start_date}),less-than(updated_at,{end_date}))"
        return self._fetch_pages(session, url)

    def fetch_lists(
        self,
        session: requests.Session,
        updated_date: str,
    ):
        # https://a.klaviyo.com/api/lists/?sort=-updated&filter=greater-than(updated,2024-02-01 00:00:00+00:00)
        url = f"{BASE_URL}/lists/?sort=-updated&filter=greater-than(updated,{updated_date})"
        return self._fetch_pages(session, url)

    def fetch_images(self, session: requests.Session, start_date: str, end_date: str):
        # https://a.klaviyo.com/api/images/?sort=-updated_at&filter=greater-or-equal(updated_at,2024-06-01 00:00:00+00:00),less-than(updated_at,2024-09-01 00:00:00+00:00)
        url = f"{BASE_URL}/images/?sort=-updated_at&filter=and(greater-or-equal(updated_at,{start_date}),less-than(updated_at,{end_date}))"
        return self._fetch_pages(session, url)

    def fetch_segments(
        self,
        session: requests.Session,
        updated_date: str,
    ):
        # https://a.klaviyo.com/api/segments/?sort=-updated&filter=greater-than(updated,2024-04-01 00:00:00+00:00)
        url = f"{BASE_URL}/segments/?sort=-updated&filter=greater-than(updated,{updated_date})"
        print("url", url)
        return self._fetch_pages(session, url)

    def fetch_flows(
        self,
        session: requests.Session,
        start_date: str,
        end_date: str,
    ):
        print(f"Fetching events for {start_date} to {end_date}")
        # https://a.klaviyo.com/api/flows/?sort=-updated&filter=and(greater-or-equal(updated,2024-06-01 00:00:00+00:00),less-than(updated,2024-09-01 00:00:00+00:00))
        url = f"{BASE_URL}/flows/?sort=-updated&filter=and(greater-or-equal(updated,{start_date}),less-than(updated,{end_date}))"
        return self._fetch_pages(session, url)

    def fetch_templates(
        self,
        session: requests.Session,
        start_date: str,
        end_date: str,
    ):
        # https://a.klaviyo.com/api/templates/?sort=-updated&filter=and(greater-or-equal(updated,2024-06-01 00:00:00+00:00),less-than(updated,2024-09-01 00:00:00+00:00))
        url = f"{BASE_URL}/templates/?sort=-updated&filter=and(greater-or-equal(updated,{start_date}),less-than(updated,{end_date}))"
        return self._fetch_pages(session, url)
