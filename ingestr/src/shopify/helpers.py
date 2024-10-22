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


def convert_datetime_fields(item: Dict[str, Any]) -> Dict[str, Any]:
    """Convert timestamp fields in the item to pendulum datetime objects

    The item is modified in place, including nested items.

    Args:
        item: The item to convert

    Returns:
        The same data item (for convenience)
    """
    fields = ["created_at", "updated_at", "createdAt", "updatedAt"]

    def convert_nested(obj: Any) -> Any:
        if isinstance(obj, dict):
            for key, value in obj.items():
                if key in fields and isinstance(value, str):
                    obj[key] = ensure_pendulum_datetime(value)
                else:
                    obj[key] = convert_nested(value)
        elif isinstance(obj, list):
            return [convert_nested(elem) for elem in obj]
        return obj

    return convert_nested(item)


def remove_nodes_key(item: Any) -> Any:
    """
    Recursively remove the 'nodes' key from dictionaries if it's the only key and its value is an array.

    Args:
        item: The item to process (can be a dict, list, or any other type)

    Returns:
        The processed item
    """
    if isinstance(item, dict):
        if len(item) == 1 and "nodes" in item and isinstance(item["nodes"], list):
            return [remove_nodes_key(node) for node in item["nodes"]]
        return {k: remove_nodes_key(v) for k, v in item.items()}
    elif isinstance(item, list):
        return [remove_nodes_key(element) for element in item]
    else:
        return item


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

        resource_last = resource.split("/")[-1]

        headers = {"X-Shopify-Access-Token": self.private_app_password}
        while url:
            response = requests.get(url, params=params, headers=headers)
            response.raise_for_status()
            json = response.json()
            yield [convert_datetime_fields(item) for item in json[resource_last]]
            url = response.links.get("next", {}).get("url")
            # Query params are included in subsequent page URLs
            params = None


class ShopifyGraphQLApi:
    """Client for Shopify GraphQL API"""

    def __init__(
        self,
        access_token: str,
        api_version: str = DEFAULT_PARTNER_API_VERSION,
        base_url: str = "partners.shopify.com",
    ) -> None:
        self.access_token = access_token
        self.api_version = api_version
        self.base_url = base_url

    @property
    def graphql_url(self) -> str:
        if self.base_url.startswith("https://"):
            return f"{self.base_url}/admin/api/{self.api_version}/graphql.json"

        return f"https://{self.base_url}/admin/api/{self.api_version}/graphql.json"

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
        pagination_cursor_has_next_page_path: Optional[jsonpath.TJsonPath] = None,
        variables: Optional[DictStrAny] = None,
    ) -> Iterable[TDataItems]:
        variables = dict(variables or {})
        while True:
            data = self.run_graphql_query(query, variables)
            data_items = jsonpath.find_values(data_items_path, data)

            if not data_items:
                break

            yield [
                remove_nodes_key(convert_datetime_fields(item)) for item in data_items
            ]

            cursors = jsonpath.find_values(pagination_cursor_path, data)
            if not cursors:
                break

            if pagination_cursor_has_next_page_path:
                has_next_page = jsonpath.find_values(
                    pagination_cursor_has_next_page_path, data
                )
                if not has_next_page or not has_next_page[0]:
                    break

            variables[pagination_variable_name] = cursors[-1]
