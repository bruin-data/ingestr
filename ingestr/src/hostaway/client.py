from typing import Iterable, Callable, Optional

import pendulum
from dlt.sources.helpers.requests import Client


class HostawayClient:
    BASE_URL = "https://api.hostaway.com"

    def __init__(self, api_key: str) -> None:
        self.session = Client(raise_for_status=False).session
        self.session.headers.update({"Authorization": f"Bearer {api_key}"})

    def _paginate(
        self,
        url: str,
        params: Optional[dict] = None,
        limit: int = 100,
        process_item: Optional[Callable[[dict], dict]] = None,
    ) -> Iterable[dict]:
        offset = 0
        if params is None:
            params = {}

        while True:
            page_params = {**params, "limit": limit, "offset": offset}
            response = self.session.get(url, params=page_params)

            if response.status_code == 403:
                break

            response.raise_for_status()
            response_data = response.json()

            if isinstance(response_data, dict) and "result" in response_data:
                items = response_data["result"]
            elif isinstance(response_data, list):
                items = response_data
            else:
                items = []

            if not items or (isinstance(items, list) and len(items) == 0):
                break

            if isinstance(items, list):
                for item in items:
                    if process_item:
                        item = process_item(item)
                    yield item
            elif isinstance(items, dict):
                if process_item:
                    items = process_item(items)
                yield items

            if isinstance(items, list) and len(items) < limit:
                break
            elif isinstance(items, dict):
                break

            offset += limit

    def fetch_listings(
        self,
        start_time: pendulum.DateTime,
        end_time: pendulum.DateTime,
        limit: int = 100,
    ) -> Iterable[dict]:
        def process_listing(listing: dict) -> dict:
            if "latestActivityOn" in listing and listing["latestActivityOn"]:
                try:
                    listing["latestActivityOn"] = pendulum.parse(listing["latestActivityOn"])
                except Exception:
                    listing["latestActivityOn"] = pendulum.datetime(1970, 1, 1, tz="UTC")
            else:
                listing["latestActivityOn"] = pendulum.datetime(1970, 1, 1, tz="UTC")
            return listing

        url = f"{self.BASE_URL}/v1/listings"
        for listing in self._paginate(url, limit=limit, process_item=process_listing):
            if start_time <= listing["latestActivityOn"] <= end_time:
                yield listing

    def fetch_listing_fee_settings(
        self,
        listing_id,
        start_time: pendulum.DateTime,
        end_time: pendulum.DateTime,
        limit: int = 100,
    ) -> Iterable[dict]:
        def process_fee(fee: dict) -> dict:
            if "updatedOn" in fee and fee["updatedOn"]:
                try:
                    fee["updatedOn"] = pendulum.parse(fee["updatedOn"])
                except Exception:
                    fee["updatedOn"] = pendulum.datetime(1970, 1, 1, tz="UTC")
            else:
                fee["updatedOn"] = pendulum.datetime(1970, 1, 1, tz="UTC")
            return fee

        url = f"{self.BASE_URL}/v1/listingFeeSettings/{str(listing_id)}"
        for fee in self._paginate(url, limit=limit, process_item=process_fee):
            if start_time <= fee["updatedOn"] <= end_time:
                yield fee

    def fetch_all_listing_fee_settings(
        self,
        start_time: pendulum.DateTime,
        end_time: pendulum.DateTime,
        limit: int = 100,
    ) -> Iterable[dict]:
        for listing in self.fetch_listings(start_time, end_time, limit):
            listing_id = listing.get("id")
            if listing_id:
                try:
                    yield from self.fetch_listing_fee_settings(listing_id, start_time, end_time, limit)
                except Exception:
                    continue

    def fetch_listing_agreement(
        self,
        listing_id,
        limit: int = 100,
    ) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/listingAgreement/{str(listing_id)}"
        yield from self._paginate(url, limit=limit)

    def fetch_listing_pricing_settings(
        self,
        listing_id,
        limit: int = 100,
    ) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/listing/pricingSettings/{str(listing_id)}"
        yield from self._paginate(url, limit=limit)

    def fetch_all_listing_pricing_settings(
        self,
        start_time: pendulum.DateTime,
        end_time: pendulum.DateTime,
        limit: int = 100,
    ) -> Iterable[dict]:
        for listing in self.fetch_listings(start_time, end_time, limit):
            listing_id = listing.get("id")
            if listing_id:
                try:
                    yield from self.fetch_listing_pricing_settings(listing_id, limit)
                except Exception:
                    continue

    def fetch_all_listing_agreements(
        self,
        start_time: pendulum.DateTime,
        end_time: pendulum.DateTime,
        limit: int = 100,
    ) -> Iterable[dict]:
        for listing in self.fetch_listings(start_time, end_time, limit):
            listing_id = listing.get("id")
            if listing_id:
                try:
                    yield from self.fetch_listing_agreement(listing_id, limit)
                except Exception:
                    continue
