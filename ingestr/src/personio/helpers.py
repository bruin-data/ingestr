"""Personio source helpers"""

from typing import Any, Iterable, Optional
from urllib.parse import urljoin

from dlt.common.typing import Dict, TDataItems
from dlt.sources.helpers import requests


class PersonioAPI:
    """A Personio API client."""

    base_url = "https://api.personio.de/v1/"

    def __init__(self, client_id: str, client_secret: str) -> None:
        """
        Args:
            client_id: The client ID of your app.
            client_secret: The client secret of your app.
        """
        self.client_id = client_id
        self.client_secret = client_secret
        self.access_token = self.get_token()

    def get_token(self) -> str:
        """Get an access token from Personio.

        Returns:
            The access token.
        """
        headers = {"Content-Type": "application/json", "Accept": "application/json"}
        data = {"client_id": self.client_id, "client_secret": self.client_secret}
        url = urljoin(self.base_url, "auth")
        response = requests.request("POST", url, headers=headers, json=data)
        json_response = response.json()
        token: str = json_response["data"]["token"]
        return token

    def get_pages(
        self,
        resource: str,
        params: Optional[Dict[str, Any]] = None,
        offset_by_page: bool = False,
    ) -> Iterable[TDataItems]:
        """Get all pages from Personio using requests.

        Args:
            resource: The resource to get pages for (e.g. employees, absences, attendances).
            params: The parameters for the resource.
            offset_by_page (bool): If True, offset increases by 1 per page; else, increases by page_size.

        Yields:
            List of data items from the page
        """
        params = params or {}
        headers = {"Authorization": f"Bearer {self.access_token}"}
        params.update({"offset": int(offset_by_page), "page": int(offset_by_page)})
        url = urljoin(self.base_url, resource)
        starts_from_zero = False
        while True:
            response = requests.get(url, headers=headers, params=params)
            json_response = response.json()
            # Get an item list from the page
            yield json_response["data"]

            metadata = json_response.get("metadata")
            if not metadata:
                break

            total_pages = metadata.get("total_pages")
            current_page = metadata.get("current_page")
            if current_page == 0:
                starts_from_zero = True

            if (
                current_page >= (total_pages - int(starts_from_zero))
                or not json_response["data"]
            ):
                break

            if offset_by_page:
                params["offset"] += 1
                params["page"] += 1
            else:
                params["offset"] += params["limit"]
                params["page"] += 1
