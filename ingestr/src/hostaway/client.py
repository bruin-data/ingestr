from typing import Callable, Iterable, Optional

import pendulum
from dlt.sources.helpers.requests import Client


class HostawayClient:
    BASE_URL = "https://api.hostaway.com"

    def __init__(self, api_key: str) -> None:
        self.session = Client(raise_for_status=False).session
        self.session.headers.update({"Authorization": f"Bearer {api_key}"})

    def _fetch_single(self, url: str, params: Optional[dict] = None) -> Iterable[dict]:
        response = self.session.get(url, params=params, timeout=30)
        response.raise_for_status()
        response_data = response.json()

        if isinstance(response_data, dict) and "result" in response_data:
            items = response_data["result"]
        elif isinstance(response_data, list):
            items = response_data
        else:
            items = []

        if isinstance(items, list):
            for item in items:
                yield item
        elif isinstance(items, dict):
            yield items

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
            response = self.session.get(url, params=page_params, timeout=30)
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
    ) -> Iterable[dict]:
        def process_listing(listing: dict) -> dict:
            if "latestActivityOn" in listing and listing["latestActivityOn"]:
                try:
                    listing["latestActivityOn"] = pendulum.parse(
                        listing["latestActivityOn"]
                    )
                except Exception:
                    listing["latestActivityOn"] = pendulum.datetime(
                        1970, 1, 1, tz="UTC"
                    )
            else:
                listing["latestActivityOn"] = pendulum.datetime(1970, 1, 1, tz="UTC")
            return listing

        url = f"{self.BASE_URL}/v1/listings"
        for listing in self._paginate(url, process_item=process_listing):
            if start_time <= listing["latestActivityOn"] <= end_time:
                yield listing

    def fetch_listing_fee_settings(
        self,
        listing_id,
        start_time: pendulum.DateTime,
        end_time: pendulum.DateTime,
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
        for fee in self._paginate(url, process_item=process_fee):
            if start_time <= fee["updatedOn"] <= end_time:
                yield fee

    def fetch_all_listing_fee_settings(
        self,
        start_time: pendulum.DateTime,
        end_time: pendulum.DateTime,
    ) -> Iterable[dict]:
        for listing in self.fetch_listings(start_time, end_time):
            listing_id = listing.get("id")
            if listing_id:
                try:
                    yield from self.fetch_listing_fee_settings(
                        listing_id, start_time, end_time
                    )
                except Exception:
                    continue

    def fetch_listing_agreement(
        self,
        listing_id,
    ) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/listingAgreement/{str(listing_id)}"
        yield from self._paginate(url)

    def fetch_listing_pricing_settings(
        self,
        listing_id,
    ) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/listing/pricingSettings/{str(listing_id)}"
        yield from self._paginate(url)

    def fetch_all_listing_pricing_settings(
        self,
        start_time: pendulum.DateTime,
        end_time: pendulum.DateTime,
    ) -> Iterable[dict]:
        for listing in self.fetch_listings(start_time, end_time):
            listing_id = listing.get("id")
            if listing_id:
                try:
                    yield from self.fetch_listing_pricing_settings(listing_id)
                except Exception:
                    continue

    def fetch_all_listing_agreements(
        self,
        start_time: pendulum.DateTime,
        end_time: pendulum.DateTime,
    ) -> Iterable[dict]:
        for listing in self.fetch_listings(start_time, end_time):
            listing_id = listing.get("id")
            if listing_id:
                try:
                    yield from self.fetch_listing_agreement(listing_id)
                except Exception:
                    continue

    def fetch_cancellation_policies(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/cancellationPolicies"
        yield from self._fetch_single(url)

    def fetch_cancellation_policies_airbnb(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/cancellationPolicies/airbnb"
        yield from self._fetch_single(url)

    def fetch_cancellation_policies_marriott(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/cancellationPolicies/marriott"
        yield from self._fetch_single(url)

    def fetch_cancellation_policies_vrbo(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/cancellationPolicies/vrbo"
        yield from self._fetch_single(url)

    def fetch_reservations(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/reservations"
        yield from self._paginate(url)

    def fetch_finance_field(self, reservation_id) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/financeField/{str(reservation_id)}"
        yield from self._fetch_single(url)

    def fetch_all_finance_fields(self) -> Iterable[dict]:
        for reservation in self.fetch_reservations():
            reservation_id = reservation.get("id")
            if reservation_id:
                try:
                    yield from self.fetch_finance_field(reservation_id)
                except Exception:
                    continue

    def fetch_reservation_payment_methods(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/reservations/paymentMethods"
        yield from self._fetch_single(url)

    def fetch_reservation_rental_agreement(self, reservation_id) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/reservations/{str(reservation_id)}/rentalAgreement"
        try:
            yield from self._fetch_single(url)
        except Exception:
            return

    def fetch_all_reservation_rental_agreements(self) -> Iterable[dict]:
        for reservation in self.fetch_reservations():
            reservation_id = reservation.get("id")
            if reservation_id:
                try:
                    yield from self.fetch_reservation_rental_agreement(reservation_id)
                except Exception:
                    continue

    def fetch_listing_calendar(self, listing_id) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/listings/{str(listing_id)}/calendar"
        yield from self._fetch_single(url)

    def fetch_all_listing_calendars(
        self,
        start_time: pendulum.DateTime,
        end_time: pendulum.DateTime,
    ) -> Iterable[dict]:
        for listing in self.fetch_listings(start_time, end_time):
            listing_id = listing.get("id")
            if listing_id:
                try:
                    yield from self.fetch_listing_calendar(listing_id)
                except Exception:
                    continue

    def fetch_conversations(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/conversations"
        yield from self._paginate(url)

    def fetch_message_templates(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/messageTemplates"
        yield from self._fetch_single(url)

    def fetch_bed_types(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/bedTypes"
        yield from self._fetch_single(url)

    def fetch_property_types(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/propertyTypes"
        yield from self._fetch_single(url)

    def fetch_countries(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/countries"
        yield from self._fetch_single(url)

    def fetch_account_tax_settings(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/accountTaxSettings"
        yield from self._fetch_single(url)

    def fetch_user_groups(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/userGroups"
        yield from self._fetch_single(url)

    def fetch_guest_payment_charges(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/guestPayments/charges"
        yield from self._paginate(url)

    def fetch_coupons(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/coupons"
        yield from self._fetch_single(url)

    def fetch_webhook_reservations(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/webhooks/reservations"
        yield from self._fetch_single(url)

    def fetch_tasks(self) -> Iterable[dict]:
        url = f"{self.BASE_URL}/v1/tasks"
        yield from self._fetch_single(url)
