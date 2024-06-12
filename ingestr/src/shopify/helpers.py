"""Shopify source helpers"""

from typing import Any, Iterable, Literal, Optional
from urllib.parse import urljoin

from dlt.common import jsonpath
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import Dict, DictStrAny, TDataItems
from dlt.sources.helpers import requests

from .exceptions import ShopifyPartnerApiError
from .settings import DEFAULT_API_VERSION, DEFAULT_PARTNER_API_VERSION

TOrderStatus = Literal["open", "closed", "cancelled", "any"]


class ShopifyApi:
    """
    A Shopify API client that can be used to get pages of data from Shopify.
    """

    def __init__(
        self,
        shop_url: str,
        private_app_password: str,
        api_version: str = DEFAULT_API_VERSION,
    ) -> None:
        """
        Args:
            shop_url: The URL of your shop (e.g. https://my-shop.myshopify.com).
            private_app_password: The private app password to the app on your shop.
            api_version: The API version to use (e.g. 2023-01)
        """
        self.shop_url = shop_url
        self.private_app_password = private_app_password
        self.api_version = api_version

    def get_pages(
        self, resource: str, params: Optional[Dict[str, Any]] = None
    ) -> Iterable[TDataItems]:
        """Get all pages from shopify using requests.
        Iterates through all pages and yield each page items.

        Args:
            resource: The resource to get pages for (e.g. products, orders, customers).
            params: Query params to include in the request.

        Yields:
            List of data items from the page
        """
        url = urljoin(self.shop_url, f"/admin/api/{self.api_version}/{resource}.json")

        headers = {"X-Shopify-Access-Token": self.private_app_password}
        while url:
            response = requests.get(url, params=params, headers=headers)
            response.raise_for_status()
            json = response.json()
            # Get item list from the page
            yield [self._convert_datetime_fields(item) for item in json[resource]]
            url = response.links.get("next", {}).get("url")
            # Query params are included in subsequent page URLs
            params = None

    def _convert_datetime_fields(self, item: Dict[str, Any]) -> Dict[str, Any]:
        """Convert timestamp fields in the item to pendulum datetime objects

        The item is modified in place.

        Args:
            item: The item to convert

        Returns:
            The same data item (for convenience)
        """
        fields = ["created_at", "updated_at"]
        for field in fields:
            if field in item:
                item[field] = ensure_pendulum_datetime(item[field])
        return item


class ShopifyPartnerApi:
    """Client for Shopify Partner grapql API"""

    def __init__(
        self,
        access_token: str,
        organization_id: str,
        api_version: str = DEFAULT_PARTNER_API_VERSION,
    ) -> None:
        """
        Args:
            access_token: The access token to use
            organization_id: The organization id to query
            api_version: The API version to use (e.g. 2023-01)
        """
        self.access_token = access_token
        self.organization_id = organization_id
        self.api_version = api_version

    @property
    def graphql_url(self) -> str:
        return f"https://partners.shopify.com/{self.organization_id}/api/{self.api_version}/graphql.json"

    def run_graphql_query(
        self, query: str, variables: Optional[DictStrAny] = None
    ) -> DictStrAny:
        """Run a graphql query against the Shopify Partner API

        Args:
            query: The query to run
            variables: The variables to include in the query

        Returns:
            The response JSON
        """
        headers = {"X-Shopify-Access-Token": self.access_token}
        response = requests.post(
            self.graphql_url,
            json={"query": query, "variables": variables},
            headers=headers,
        )
        data = response.json()
        if data.get("errors"):
            raise ShopifyPartnerApiError(response.text)
        return data  # type: ignore[no-any-return]

    def get_graphql_pages(
        self,
        query: str,
        data_items_path: jsonpath.TJsonPath,
        pagination_cursor_path: jsonpath.TJsonPath,
        pagination_variable_name: str,
        variables: Optional[DictStrAny] = None,
    ) -> Iterable[TDataItems]:
        variables = dict(variables or {})
        while True:
            data = self.run_graphql_query(query, variables)
            print(data)
            data_items = jsonpath.find_values(data_items_path, data)
            if not data_items:
                break
            yield data_items
            cursors = jsonpath.find_values(pagination_cursor_path, data)
            if not cursors:
                break
            variables[pagination_variable_name] = cursors[-1]
