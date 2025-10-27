from typing import Iterable

import pendulum
from dlt.sources.helpers.requests import Client


class HostawayClient:
    BASE_URL = "https://api.hostaway.com"

    def __init__(self, api_key: str) -> None:
        self.session = Client(raise_for_status=False).session
        self.session.headers.update({"Authorization": f"Bearer {api_key}"})

    def fetch_listings(
        self,
        start_time: pendulum.DateTime,
        end_time: pendulum.DateTime,
        limit: int = 100,
    ) -> Iterable[dict]:
        """
        Fetch all listings from Hostaway API with pagination.
        Client-side filtering by latestActivityOn field.

        Args:
            start_time: Start date for filtering
            end_time: End date for filtering
            limit: Number of records per page (default: 100)

        Yields:
            dict: Listing data with parsed latestActivityOn
        """
        offset = 0

        while True:
            params = {
                "limit": limit,
                "offset": offset,
            }

            response = self.session.get(
                f"{self.BASE_URL}/v1/listings",
                params=params,
            )
            response.raise_for_status()
            response_data = response.json()

            # Handle different response structures
            if isinstance(response_data, dict):
                if "result" in response_data:
                    listings = response_data["result"]
                elif "data" in response_data:
                    listings = response_data["data"]
                else:
                    listings = []
            elif isinstance(response_data, list):
                listings = response_data
            else:
                listings = []

            if not listings or len(listings) == 0:
                break

            for listing in listings:
                # Parse latestActivityOn if present
                if "latestActivityOn" in listing and listing["latestActivityOn"]:
                    try:
                        listing["latestActivityOn"] = pendulum.parse(
                            listing["latestActivityOn"]
                        )
                    except Exception:
                        listing["latestActivityOn"] = pendulum.datetime(1970, 1, 1, tz="UTC")
                else:
                    listing["latestActivityOn"] = pendulum.datetime(1970, 1, 1, tz="UTC")

                # Client-side filtering based on latestActivityOn
                if start_time <= listing["latestActivityOn"] <= end_time:
                    yield listing

            # If we got fewer results than limit, we're done
            if len(listings) < limit:
                break

            offset += limit
