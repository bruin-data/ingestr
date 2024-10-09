"""Fetches Shopify Orders and Products."""

from typing import Any, Dict, Iterable, Optional

import dlt
from dlt.common import jsonpath as jp
from dlt.common import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource

from .helpers import ShopifyApi, ShopifyGraphQLApi, TOrderStatus
from .settings import (
    DEFAULT_API_VERSION,
    DEFAULT_ITEMS_PER_PAGE,
    DEFAULT_PARTNER_API_VERSION,
    FIRST_DAY_OF_MILLENNIUM,
)


@dlt.source(name="shopify", max_table_nesting=0)
def shopify_source(
    private_app_password: str = dlt.secrets.value,
    api_version: str = DEFAULT_API_VERSION,
    shop_url: str = dlt.config.value,
    start_date: TAnyDateTime = FIRST_DAY_OF_MILLENNIUM,
    end_date: Optional[TAnyDateTime] = None,
    created_at_min: TAnyDateTime = FIRST_DAY_OF_MILLENNIUM,
    items_per_page: int = DEFAULT_ITEMS_PER_PAGE,
    order_status: TOrderStatus = "any",
) -> Iterable[DltResource]:
    """
    The source for the Shopify pipeline. Available resources are products, orders, and customers.

    `start_time` argument can be used on its own or together with `end_time`. When both are provided
    data is limited to items updated in that time range.
    The range is "half-open", meaning elements equal and newer than `start_time` and elements older than `end_time` are included.
    All resources opt-in to use Airflow scheduler if run as Airflow task

    Args:
        private_app_password: The app password to the app on your shop.
        api_version: The API version to use (e.g. 2023-01).
        shop_url: The URL of your shop (e.g. https://my-shop.myshopify.com).
        items_per_page: The max number of items to fetch per page. Defaults to 250.
        start_date: Items updated on or after this date are imported. Defaults to 2000-01-01.
            If end date is not provided, this is used as the initial value for incremental loading and after the initial run, only new data will be retrieved.
            Accepts any `date`/`datetime` object or a date/datetime string in ISO 8601 format.
        end_time: The end time of the range for which to load data.
            Should be used together with `start_date` to limit the data to items updated in that time range.
            If end time is not provided, the incremental loading will be enabled and after initial run, only new data will be retrieved
        created_at_min: The minimum creation date of items to import. Items created on or after this date are loaded. Defaults to 2000-01-01.
        order_status: The order status to filter by. Can be 'open', 'closed', 'cancelled', or 'any'. Defaults to 'any'.

    Returns:
        Iterable[DltResource]: A list of DltResource objects representing the data resources.
    """

    # build client
    client = ShopifyApi(shop_url, private_app_password, api_version)

    start_date_obj = ensure_pendulum_datetime(start_date)
    end_date_obj = ensure_pendulum_datetime(end_date) if end_date else None
    created_at_min_obj = ensure_pendulum_datetime(created_at_min)

    # define resources
    @dlt.resource(primary_key="id", write_disposition="merge")
    def products(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "updated_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            allow_external_schedulers=True,
        ),
        created_at_min: pendulum.DateTime = created_at_min_obj,
        items_per_page: int = items_per_page,
    ) -> Iterable[TDataItem]:
        """
        The resource for products on your shop, supports incremental loading and pagination.

        Args:
            updated_at: The saved state of the last 'updated_at' value.

        Returns:
            Iterable[TDataItem]: A generator of products.
        """
        params = dict(
            updated_at_min=updated_at.last_value.isoformat(),
            limit=items_per_page,
            order="updated_at asc",
            created_at_min=created_at_min.isoformat(),
        )
        if updated_at.end_value is not None:
            params["updated_at_max"] = updated_at.end_value.isoformat()
        yield from client.get_pages("products", params)

    @dlt.resource(primary_key="id", write_disposition="merge")
    def orders(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "updated_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            allow_external_schedulers=True,
        ),
        created_at_min: pendulum.DateTime = created_at_min_obj,
        items_per_page: int = items_per_page,
        status: TOrderStatus = order_status,
    ) -> Iterable[TDataItem]:
        """
        The resource for orders on your shop, supports incremental loading and pagination.

        Args:
            updated_at: The saved state of the last 'updated_at' value.

        Returns:
            Iterable[TDataItem]: A generator of orders.
        """
        params = dict(
            updated_at_min=updated_at.last_value.isoformat(),
            limit=items_per_page,
            status=status,
            order="updated_at asc",
            created_at_min=created_at_min.isoformat(),
        )
        if updated_at.end_value is not None:
            params["updated_at_max"] = updated_at.end_value.isoformat()
        yield from client.get_pages("orders", params)

    @dlt.resource(primary_key="id", write_disposition="merge")
    def customers(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "updated_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            allow_external_schedulers=True,
        ),
        created_at_min: pendulum.DateTime = created_at_min_obj,
        items_per_page: int = items_per_page,
    ) -> Iterable[TDataItem]:
        """
        The resource for customers on your shop, supports incremental loading and pagination.

        Args:
            updated_at: The saved state of the last 'updated_at' value.

        Returns:
            Iterable[TDataItem]: A generator of customers.
        """
        params = dict(
            updated_at_min=updated_at.last_value.isoformat(),
            limit=items_per_page,
            order="updated_at asc",
            created_at_min=created_at_min.isoformat(),
        )
        if updated_at.end_value is not None:
            params["updated_at_max"] = updated_at.end_value.isoformat()
        yield from client.get_pages("customers", params)

    @dlt.resource(primary_key="id", write_disposition="append")
    def events(
        created_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "created_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
        ),
        items_per_page: int = items_per_page,
    ) -> Iterable[TDataItem]:
        params = dict(
            created_at_min=created_at.last_value.isoformat(),
            limit=items_per_page,
            order="created_at asc",
        )
        yield from client.get_pages("events", params)

    @dlt.resource(primary_key="id", write_disposition="merge")
    def price_rules(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "updated_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
        ),
        items_per_page: int = items_per_page,
    ) -> Iterable[TDataItem]:
        params = dict(
            updated_at_min=updated_at.last_value.isoformat(),
            limit=items_per_page,
            order="updated_at asc",
        )
        yield from client.get_pages("price_rules", params)

    @dlt.resource(primary_key="id", write_disposition="merge")
    def transactions(
        since_id: dlt.sources.incremental[int] = dlt.sources.incremental(
            "id",
            initial_value=None,
        ),
        items_per_page: int = items_per_page,
    ) -> Iterable[TDataItem]:
        params = dict(
            limit=items_per_page,
        )
        if since_id.start_value is not None:
            params["since_id"] = since_id.start_value
        yield from client.get_pages("shopify_payments/balance/transactions", params)

    @dlt.resource(primary_key="currency", write_disposition={"disposition": "merge", "strategy": "scd2"})
    def balance() -> Iterable[TDataItem]:
        yield from client.get_pages("shopify_payments/balance", {})

    @dlt.resource(primary_key="id", write_disposition="merge")
    def inventory_items(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            allow_external_schedulers=True,
        ),
        items_per_page: int = items_per_page,
    ) -> Iterable[TDataItem]:
        client = ShopifyGraphQLApi(
            base_url=shop_url,
            access_token=private_app_password,
            api_version="2024-07",
        )

        query = """
            query inventoryItems($after: String, $query: String, $first: Int) {
            inventoryItems(after: $after, first: $first, query: $query) {
                edges {
                node {
                    id
                    countryCodeOfOrigin
                    createdAt
                    duplicateSkuCount
                    harmonizedSystemCode
                    inventoryHistoryUrl
                    legacyResourceId
                    measurement {
                    id
                    weight {
                        unit
                        value
                    }
                    }

                    provinceCodeOfOrigin
                    requiresShipping
                    sku
                    tracked
                    trackedEditable {
                    locked
                    reason
                    }
                    unitCost {
                    amount
                    currencyCode
                    }
                    updatedAt
                    variant {
                    id
                    availableForSale
                    barcode

                    compareAtPrice
                    createdAt
                    inventoryPolicy
                    inventoryQuantity
                    legacyResourceId

                    position
                    price
                    product {
                        id
                    }
                    requiresComponents

                    selectedOptions {
                        name
                        value
                    }
                    sellableOnlineQuantity

                    sellingPlanGroupsCount {
                        count
                        precision
                    }
                    sku

                    taxCode
                    taxable
                    title
                    updatedAt
                    }
                }
                }
                pageInfo {
                    endCursor
                }
            }
        }"""

        yield from client.get_graphql_pages(
            query,
            data_items_path="data.inventoryItems.edges[*].node",
            pagination_cursor_path="data.inventoryItems.pageInfo.endCursor",
            pagination_variable_name="after",
            variables={
                "query": f"updated_at:>'{updated_at.last_value.isoformat()}'",
                "first": items_per_page,
            },
        )

    return (products, orders, customers, inventory_items, transactions, balance, events, price_rules)
