"""Fetches Shopify Orders and Products."""

from typing import Any, Dict, Iterable, Optional  # noqa: F401

import dlt
from dlt.common import jsonpath as jp  # noqa: F401
from dlt.common import pendulum
from dlt.common.time import ensure_pendulum_datetime
from dlt.common.typing import TAnyDateTime, TDataItem
from dlt.sources import DltResource

from .helpers import ShopifyApi, ShopifyGraphQLApi, TOrderStatus
from .settings import (
    DEFAULT_API_VERSION,
    DEFAULT_ITEMS_PER_PAGE,
    DEFAULT_PARTNER_API_VERSION,  # noqa: F401
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
    @dlt.resource(
        primary_key="id",
        write_disposition="merge",
        columns={
            "body_html": {
                "data_type": "text",
                "nullable": True,
                "description": "A description of the product. Supports HTML formatting.",
            },
            "created_at": {
                "data_type": "timestamp",
                "nullable": False,
                "description": "The date and time (ISO 8601 format) when the product was created.",
            },
            "handle": {
                "data_type": "text",
                "nullable": False,
                "description": "A unique human-friendly string for the product. Automatically generated from the product's title. Used by the Liquid templating language to refer to objects.",
            },
            "id": {
                "data_type": "bigint",
                "nullable": False,
                "primary_key": True,
                "description": "An unsigned 64-bit integer that's used as a unique identifier for the product.",
            },
            "images": {
                "data_type": "json",
                "nullable": True,
                "description": "A list of product image objects, each one representing an image associated with the product.",
            },
            "options": {
                "data_type": "json",
                "nullable": True,
                "description": "The custom product properties. For example, Size, Color, and Material.",
            },
            "product_type": {
                "data_type": "text",
                "nullable": True,
                "description": "A categorization for the product used for filtering and searching products.",
            },
            "published_at": {
                "data_type": "timestamp",
                "nullable": True,
                "description": "The date and time (ISO 8601 format) when the product was published.",
            },
            "published_scope": {
                "data_type": "text",
                "nullable": True,
                "description": "Whether the product is published to the Point of Sale channel.",
            },
            "status": {
                "data_type": "text",
                "nullable": False,
                "description": "The status of the product.",
            },
            "tags": {
                "data_type": "text",
                "nullable": True,
                "description": "A string of comma-separated tags used for filtering and search.",
            },
            "template_suffix": {
                "data_type": "text",
                "nullable": True,
                "description": "The suffix of the Liquid template used for the product page.",
            },
            "title": {
                "data_type": "text",
                "nullable": False,
                "description": "The name of the product.",
            },
            "updated_at": {
                "data_type": "timestamp",
                "nullable": True,
                "description": "The date and time (ISO 8601 format) when the product was last modified.",
            },
            "variants": {
                "data_type": "json",
                "nullable": True,
                "description": "An array of product variants, each representing a different version of the product.",
            },
            "vendor": {
                "data_type": "text",
                "nullable": True,
                "description": "The name of the product's vendor.",
            },
        },
    )
    def products_legacy(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "updated_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            allow_external_schedulers=True,
            range_end="closed",
            range_start="closed",
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

    @dlt.resource(
        primary_key="id",
        write_disposition="merge",
        columns={
            "app_id": {
                "data_type": "bigint",
                "nullable": True,
                "description": "The ID of the app that created the order.",
            },
            "billing_address": {
                "data_type": "json",
                "nullable": True,
                "description": "The mailing address associated with the payment method.",
            },
            "browser_ip": {
                "data_type": "text",
                "nullable": True,
                "description": "The IP address of the browser used by the customer when they placed the order.",
            },
            "buyer_accepts_marketing": {
                "data_type": "bool",
                "nullable": True,
                "description": "Whether the customer consented to receive email updates from the shop.",
            },
            "cancel_reason": {
                "data_type": "text",
                "nullable": True,
                "description": "The reason why the order was canceled.",
            },
            "cancelled_at": {
                "data_type": "timestamp",
                "nullable": True,
                "description": "The date and time when the order was canceled.",
            },
            "cart_token": {
                "data_type": "text",
                "nullable": True,
                "description": "A unique value referencing the cart associated with the order.",
            },
            "checkout_token": {
                "data_type": "text",
                "nullable": True,
                "description": "A unique value referencing the checkout associated with the order.",
            },
            "client_details": {
                "data_type": "json",
                "nullable": True,
                "description": "Information about the browser the customer used when placing the order.",
            },
            "closed_at": {
                "data_type": "timestamp",
                "nullable": True,
                "description": "The date and time when the order was closed.",
            },
            "company": {
                "data_type": "json",
                "nullable": True,
                "description": "Information about the purchasing company for the order.",
            },
            "confirmation_number": {
                "data_type": "text",
                "nullable": True,
                "description": "A randomly generated identifier for the order.",
            },
            "confirmed": {
                "data_type": "bool",
                "nullable": True,
                "description": "Whether inventory has been reserved for the order.",
            },
            "created_at": {
                "data_type": "timestamp",
                "nullable": False,
                "description": "The autogenerated date and time when the order was created.",
            },
            "currency": {
                "data_type": "text",
                "nullable": False,
                "description": "The three-letter code (ISO 4217 format) for the shop currency.",
            },
            "current_total_additional_fees_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The current total additional fees on the order in shop and presentment currencies.",
            },
            "current_total_discounts": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The current total discounts on the order in the shop currency.",
            },
            "current_total_discounts_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The current total discounts on the order in shop and presentment currencies.",
            },
            "current_total_duties_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The current total duties charged on the order in shop and presentment currencies.",
            },
            "current_total_price": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The current total price of the order in the shop currency.",
            },
            "current_total_price_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The current total price of the order in shop and presentment currencies.",
            },
            "current_subtotal_price": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The sum of prices for all line items after discounts and returns in the shop currency.",
            },
            "current_subtotal_price_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The sum of the prices for all line items after discounts and returns in shop and presentment currencies.",
            },
            "current_total_tax": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The sum of the prices for all tax lines applied to the order in the shop currency.",
            },
            "current_total_tax_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The sum of the prices for all tax lines applied to the order in shop and presentment currencies.",
            },
            "customer": {
                "data_type": "json",
                "nullable": True,
                "description": "Information about the customer.",
            },
            "customer_locale": {
                "data_type": "text",
                "nullable": True,
                "description": "The two or three-letter language code, optionally followed by a region modifier.",
            },
            "discount_applications": {
                "data_type": "json",
                "nullable": True,
                "description": "An ordered list of stacked discount applications.",
            },
            "discount_codes": {
                "data_type": "json",
                "nullable": True,
                "description": "A list of discounts applied to the order.",
            },
            "email": {
                "data_type": "text",
                "nullable": True,
                "description": "The customer's email address.",
            },
            "estimated_taxes": {
                "data_type": "bool",
                "nullable": True,
                "description": "Whether taxes on the order are estimated.",
            },
            "financial_status": {
                "data_type": "text",
                "nullable": True,
                "description": "The status of payments associated with the order.",
            },
            "fulfillments": {
                "data_type": "json",
                "nullable": True,
                "description": "An array of fulfillments associated with the order.",
            },
            "fulfillment_status": {
                "data_type": "text",
                "nullable": True,
                "description": "The order's status in terms of fulfilled line items.",
            },
            "gateway": {
                "data_type": "text",
                "nullable": True,
                "description": "The payment gateway used.",
            },
            "id": {
                "data_type": "bigint",
                "nullable": False,
                "primary_key": True,
                "description": "The ID of the order, used for API purposes.",
            },
            "landing_site": {
                "data_type": "text",
                "nullable": True,
                "description": "The URL for the page where the buyer landed when they entered the shop.",
            },
            "line_items": {
                "data_type": "json",
                "nullable": True,
                "description": "A list of line item objects containing information about an item in the order.",
            },
            "location_id": {
                "data_type": "bigint",
                "nullable": True,
                "description": "The ID of one of the locations assigned to fulfill the order.",
            },
            "merchant_of_record_app_id": {
                "data_type": "bigint",
                "nullable": True,
                "description": "The application acting as Merchant of Record for the order.",
            },
            "name": {
                "data_type": "text",
                "nullable": True,
                "description": "The order name, generated by combining the order_number with the order prefix and suffix.",
            },
            "note": {
                "data_type": "text",
                "nullable": True,
                "description": "An optional note that a shop owner can attach to the order.",
            },
            "note_attributes": {
                "data_type": "json",
                "nullable": True,
                "description": "Extra information added to the order as key-value pairs.",
            },
            "number": {
                "data_type": "bigint",
                "nullable": True,
                "description": "The order's position in the shop's count of orders.",
            },
            "order_number": {
                "data_type": "bigint",
                "nullable": True,
                "description": "The order's position in the shop's count of orders, starting at 1001.",
            },
            "original_total_additional_fees_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The original total additional fees on the order in shop and presentment currencies.",
            },
            "original_total_duties_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The original total duties charged on the order in shop and presentment currencies.",
            },
            "payment_terms": {
                "data_type": "json",
                "nullable": True,
                "description": "The terms and conditions under which a payment should be processed.",
            },
            "payment_gateway_names": {
                "data_type": "json",
                "nullable": True,
                "description": "The list of payment gateways used for the order.",
            },
            "phone": {
                "data_type": "text",
                "nullable": True,
                "description": "The customer's phone number for receiving SMS notifications.",
            },
            "po_number": {
                "data_type": "text",
                "nullable": True,
                "description": "The purchase order number associated with the order.",
            },
            "presentment_currency": {
                "data_type": "text",
                "nullable": True,
                "description": "The presentment currency used to display prices to the customer.",
            },
            "processed_at": {
                "data_type": "timestamp",
                "nullable": True,
                "description": "The date and time when an order was processed.",
            },
            "referring_site": {
                "data_type": "text",
                "nullable": True,
                "description": "The website where the customer clicked a link to the shop.",
            },
            "refunds": {
                "data_type": "json",
                "nullable": True,
                "description": "A list of refunds applied to the order.",
            },
            "shipping_address": {
                "data_type": "json",
                "nullable": True,
                "description": "The mailing address where the order will be shipped.",
            },
            "shipping_lines": {
                "data_type": "json",
                "nullable": True,
                "description": "An array detailing the shipping methods used.",
            },
            "source_name": {
                "data_type": "text",
                "nullable": True,
                "description": "The source of the checkout.",
            },
            "source_identifier": {
                "data_type": "text",
                "nullable": True,
                "description": "The ID of the order placed on the originating platform.",
            },
            "source_url": {
                "data_type": "text",
                "nullable": True,
                "description": "A valid URL to the original order on the originating surface.",
            },
            "subtotal_price": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The price of the order in the shop currency after discounts but before shipping, duties, taxes, and tips.",
            },
            "subtotal_price_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The subtotal of the order in shop and presentment currencies after discounts but before shipping, duties, taxes, and tips.",
            },
            "tags": {
                "data_type": "text",
                "nullable": True,
                "description": "Tags attached to the order, formatted as a string of comma-separated values.",
            },
            "tax_lines": {
                "data_type": "json",
                "nullable": True,
                "description": "An array of tax line objects detailing taxes applied to the order.",
            },
            "taxes_included": {
                "data_type": "bool",
                "nullable": True,
                "description": "Whether taxes are included in the order subtotal.",
            },
            "test": {
                "data_type": "bool",
                "nullable": True,
                "description": "Whether this is a test order.",
            },
            "token": {
                "data_type": "text",
                "nullable": True,
                "description": "A unique value referencing the order.",
            },
            "total_discounts": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The total discounts applied to the price of the order in the shop currency.",
            },
            "total_discounts_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The total discounts applied to the price of the order in shop and presentment currencies.",
            },
            "total_line_items_price": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The sum of all line item prices in the shop currency.",
            },
            "total_line_items_price_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The total of all line item prices in shop and presentment currencies.",
            },
            "total_outstanding": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The total outstanding amount of the order in the shop currency.",
            },
            "total_price": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The sum of all line item prices, discounts, shipping, taxes, and tips in the shop currency.",
            },
            "total_price_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The total price of the order in shop and presentment currencies.",
            },
            "total_shipping_price_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The total shipping price of the order in shop and presentment currencies.",
            },
            "total_tax": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The sum of the prices for all tax lines applied to the order in the shop currency.",
            },
            "total_tax_set": {
                "data_type": "json",
                "nullable": True,
                "description": "The sum of the prices for all tax lines applied to the order in shop and presentment currencies.",
            },
            "total_tip_received": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The sum of all the tips in the order in the shop currency.",
            },
            "total_weight": {
                "data_type": "decimal",
                "nullable": True,
                "description": "The sum of all line item weights in grams.",
            },
            "updated_at": {
                "data_type": "timestamp",
                "nullable": True,
                "description": "The date and time when the order was last modified.",
            },
            "user_id": {
                "data_type": "bigint",
                "nullable": True,
                "description": "The ID of the user logged into Shopify POS who processed the order.",
            },
            "order_status_url": {
                "data_type": "text",
                "nullable": True,
                "description": "The URL pointing to the order status web page, if applicable.",
            },
        },
    )
    def orders(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "updated_at",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            allow_external_schedulers=True,
            range_end="closed",
            range_start="closed",
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
            range_end="closed",
            range_start="closed",
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
            range_end="closed",
            range_start="closed",
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
            range_end="closed",
            range_start="closed",
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

    @dlt.resource(
        primary_key="currency",
        write_disposition={"disposition": "merge", "strategy": "scd2"},
    )
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
            range_end="closed",
            range_start="closed",
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

    @dlt.resource(primary_key="id", write_disposition="merge")
    def discounts(items_per_page: int = items_per_page) -> Iterable[TDataItem]:
        client = ShopifyGraphQLApi(
            base_url=shop_url,
            access_token=private_app_password,
            api_version="2024-07",
        )

        query = """
query discountNodes($after: String, $query: String, $first: Int)  {
  discountNodes(after: $after, first: $first, query: $query) {
    nodes {
      id
      discount {
        __typename
        ... on DiscountCodeApp {
          appDiscountType {
            app {
              id
            }
            functionId
            targetType
          }
          appliesOncePerCustomer
          asyncUsageCount
          combinesWith {
            orderDiscounts
            productDiscounts
            shippingDiscounts
          }
          codesCount {
            count
            precision
          }
          createdAt
          customerSelection {
            __typename
            ... on DiscountCustomerAll {
              allCustomers
            }
            ... on DiscountCustomerSegments {
              segments {
                creationDate
                id
                lastEditDate
                name
                query
              }
            }
            ... on DiscountCustomers {
              customers {
                id
              }
            }
          }
          discountClass
          discountId
          endsAt
          errorHistory {
            errorsFirstOccurredAt
            firstOccurredAt
            hasBeenSharedSinceLastError
            hasSharedRecentErrors
          }
          hasTimelineComment
          recurringCycleLimit
          shareableUrls {
            targetItemImage {
              id
              url
            }
            targetType
            title
            url
          }
          startsAt
          status
          title
          totalSales {
            amount
            currencyCode
          }
          updatedAt
          usageLimit
        }
        ... on DiscountCodeBasic {
          appliesOncePerCustomer
          asyncUsageCount
          codes: codes(first: 250) {
            nodes {
              id
              code
            }
          }
          codesCount {
            count
            precision
          }
          combinesWith {
            orderDiscounts
            productDiscounts
            shippingDiscounts
          }
          createdAt
          customerGets {
            appliesOnOneTimePurchase
            appliesOnSubscription
            items {
              __typename
              ... on AllDiscountItems {
                allItems
              }
              ... on DiscountCollections {
                collectionsFirst250: collections(first: 250) {
                  nodes {
                    id
                  }
                }
              }
              ... on DiscountProducts {
                productsFirst250: products(first: 250) {
                  nodes {
                    id
                  }
                }
                productVariantsFirst250: productVariants(first: 250) {
                  nodes {
                    id
                  }
                }
              }
            }
            value {
              __typename
              ... on DiscountAmount {
                amount {
                  amount
                  currencyCode
                }
                appliesOnEachItem
              }
              ... on DiscountOnQuantity {
                effect {
                  ... on DiscountAmount {
                    amount {
                      amount
                      currencyCode
                    }
                    appliesOnEachItem
                  }
                  ... on DiscountPercentage {
                    percentage
                  }
                }
                quantity {
                  quantity
                }
              }
              ... on DiscountPercentage {
                percentage
              }
            }
          }
          customerSelection {
            __typename
            ... on DiscountCustomerAll {
              allCustomers
            }
            ... on DiscountCustomerSegments {
              segments {
                creationDate
                id
                lastEditDate
                name
                query
              }
            }
            ... on DiscountCustomers {
              customers {
                id
              }
            }
          }
          discountClass
          endsAt
          hasTimelineComment
          minimumRequirement {
            __typename
            ... on DiscountMinimumQuantity {
              greaterThanOrEqualToQuantity
            }
            ... on DiscountMinimumSubtotal {
              greaterThanOrEqualToSubtotal {
                amount
                currencyCode
              }
            }
          }
          recurringCycleLimit
          shareableUrls {
            url
            title
          }
          shortSummary
          startsAt
          status
          summary
          title
          totalSales {
            amount
            currencyCode
          }
          updatedAt
          usageLimit
        }
        ... on DiscountCodeBxgy {
          appliesOncePerCustomer
          asyncUsageCount
          codesFirst50: codes(first: 50) {
            nodes {
              id
              code
            }
          }
          codesCount {
            count
            precision
          }
          combinesWith {
            orderDiscounts
            productDiscounts
            shippingDiscounts
          }
          createdAt
          customerBuys {
            items {
              __typename
              ... on AllDiscountItems {
                allItems
              }
              ... on DiscountCollections {
                collectionsFirst250: collections(first: 250) {
                  nodes {
                    id
                  }
                }
              }
              ... on DiscountProducts {
                productsFirst250: products(first: 250) {
                  nodes {
                    id
                  }
                }
                productVariantsFirst250: productVariants(first: 250) {
                  nodes {
                    id
                  }
                }
              }
            }
            value {
              __typename
              ... on DiscountPurchaseAmount {
                amount
              }
              ... on DiscountQuantity {
                quantity
              }
            }
          }
          customerGets {
            appliesOnOneTimePurchase
            appliesOnSubscription
            items {
              __typename
              ... on AllDiscountItems {
                allItems
              }
              ... on DiscountCollections {
                collectionsFirst250: collections(first: 250) {
                  nodes {
                    id
                  }
                }
              }
              ... on DiscountProducts {
                productsFirst250: products(first: 250) {
                  nodes {
                    id
                  }
                }
                productVariantsFirst250: productVariants(first: 250) {
                  nodes {
                    id
                  }
                }
              }
            }
            value {
              __typename
              ... on DiscountAmount {
                amount {
                  amount
                  currencyCode
                }
                appliesOnEachItem
              }
              ... on DiscountOnQuantity {
                effect {
                  __typename
                  ... on DiscountAmount {
                    amount {
                      amount
                      currencyCode
                    }
                    appliesOnEachItem
                  }
                  ... on DiscountPercentage {
                    percentage
                  }
                }
                quantity {
                  quantity
                }
              }
              ... on DiscountPercentage {
                percentage
              }
            }
          }
          customerSelection {
            __typename
            ... on DiscountCustomerAll {
              allCustomers
            }
            ... on DiscountCustomerSegments {
              segments {
                creationDate
                id
                lastEditDate
                name
                query
              }
            }
            ... on DiscountCustomers {
              customers {
                id
              }
            }
          }
          discountClass
          endsAt
          hasTimelineComment
          shareableUrls {
            url
            title
          }
          startsAt
          status
          summary
          title
          totalSales {
            amount
            currencyCode
          }
          updatedAt
          usageLimit
          usesPerOrderLimit
        }
        ... on DiscountCodeFreeShipping {
          appliesOncePerCustomer
          appliesOnOneTimePurchase
          appliesOnSubscription
          asyncUsageCount
          codesFirst250: codes(first: 250) {
            nodes {
              id
              code
            }
          }
          codesCount {
            count
            precision
          }
          combinesWith {
            orderDiscounts
            productDiscounts
            shippingDiscounts
          }
          createdAt
          customerSelection {
            __typename
            ... on DiscountCustomerAll {
              allCustomers
            }
            ... on DiscountCustomerSegments {
              segments {
                creationDate
                id
                lastEditDate
                name
                query
              }
            }
            ... on DiscountCustomers {
              customers {
                id
              }
            }
          }
          destinationSelection {
            __typename
            ... on DiscountCountries {
              countries
              includeRestOfWorld
            }
            ... on DiscountCountryAll {
              allCountries
            }
          }
          discountClass
          endsAt
          hasTimelineComment
          maximumShippingPrice {
            amount
            currencyCode
          }
          minimumRequirement {
            __typename
            ... on DiscountMinimumQuantity {
              greaterThanOrEqualToQuantity
            }
            ... on DiscountMinimumSubtotal {
              greaterThanOrEqualToSubtotal {
                amount
                currencyCode
              }
            }
          }
          recurringCycleLimit
          shareableUrls {
            targetItemImage {
              id
              url
            }
            targetType
            title
            url
          }
          shortSummary
          startsAt
          status
          summary
          title
          totalSales {
            amount
            currencyCode
          }
          updatedAt
          usageLimit
        }
        ... on DiscountAutomaticApp {
          appDiscountType {
            app {
              apiKey
            }
            appBridge {
              createPath
              detailsPath
            }
            appKey
            description
            discountClass
            functionId
            targetType
            title
          }
          asyncUsageCount
          combinesWith {
            orderDiscounts
            productDiscounts
            shippingDiscounts
          }
          createdAt
          discountClass
          discountId
          startsAt
          endsAt
          errorHistory {
            errorsFirstOccurredAt
            firstOccurredAt
            hasBeenSharedSinceLastError
            hasSharedRecentErrors
          }
          status
          title
          updatedAt
        }
        ... on DiscountAutomaticBasic {
          asyncUsageCount
          combinesWith {
            orderDiscounts
            productDiscounts
            shippingDiscounts
          }
          createdAt
          customerGets {
            appliesOnOneTimePurchase
            appliesOnSubscription
            items {
              __typename
              ... on AllDiscountItems {
                allItems
              }
              ... on DiscountCollections {
                collectionsFirst250: collections(first: 250) {
                  nodes {
                    id
                  }
                }
              }
              ... on DiscountProducts {
                productsFirst250: products(first: 250) {
                  nodes {
                    id
                  }
                }
                productVariantsFirst250: productVariants(first: 250) {
                  nodes {
                    id
                  }
                }
              }
            }
            value {
              __typename
              ... on DiscountAmount {
                amount {
                  amount
                  currencyCode
                }
                appliesOnEachItem
              }
              ... on DiscountOnQuantity {
                effect {
                  __typename
                  ... on DiscountAmount {
                    amount {
                      amount
                      currencyCode
                    }
                    appliesOnEachItem
                  }
                  ... on DiscountPercentage {
                    percentage
                  }
                }
                quantity {
                  quantity
                }
              }
              ... on DiscountPercentage {
                percentage
              }
            }
          }
          discountClass
          endsAt
          minimumRequirement {
            __typename
            ... on DiscountMinimumQuantity {
              greaterThanOrEqualToQuantity
            }
            ... on DiscountMinimumSubtotal {
              greaterThanOrEqualToSubtotal {
                amount
                currencyCode
              }
            }
          }
          recurringCycleLimit
          shortSummary
          startsAt
          status
          summary
          title
          updatedAt
        }
        ... on DiscountAutomaticBxgy {
          asyncUsageCount
          combinesWith {
            orderDiscounts
            productDiscounts
            shippingDiscounts
          }
          createdAt
          customerBuys {
            items {
              __typename
              ... on AllDiscountItems {
                allItems
              }
              ... on DiscountCollections {
                collectionsFirst250: collections(first: 250) {
                  nodes {
                    id
                  }
                }
              }
              ... on DiscountProducts {
                productsFirst250: products(first: 250) {
                  nodes {
                    id
                  }
                }
                productVariantsFirst250: productVariants(first: 250) {
                  nodes {
                    id
                  }
                }
              }
            }
            value {
              __typename
              ... on DiscountPurchaseAmount {
                amount
              }
              ... on DiscountQuantity {
                quantity
              }
            }
          }
          customerGets {
            appliesOnOneTimePurchase
            appliesOnSubscription
            items {
              __typename
              ... on AllDiscountItems {
                allItems
              }
              ... on DiscountCollections {
                collectionsFirst250: collections(first: 250) {
                  nodes {
                    id
                  }
                }
              }
              ... on DiscountProducts {
                productsFirst250: products(first: 250) {
                  nodes {
                    id
                  }
                }
                productVariantsFirst250: productVariants(first: 250) {
                  nodes {
                    id
                  }
                }
              }
            }
            value {
              __typename
              ... on DiscountAmount {
                amount {
                  amount
                  currencyCode
                }
                appliesOnEachItem
              }
              ... on DiscountOnQuantity {
                effect {
                  __typename
                  ... on DiscountAmount {
                    amount {
                      amount
                      currencyCode
                    }
                    appliesOnEachItem
                  }
                  ... on DiscountPercentage {
                    percentage
                  }
                }
                quantity {
                  quantity
                }
              }
              ... on DiscountPercentage {
                percentage
              }
            }
          }
          discountClass
          endsAt
          startsAt
          status
          summary
          title
          updatedAt
          usesPerOrderLimit
        }
        ... on DiscountAutomaticFreeShipping {
          appliesOnOneTimePurchase
          appliesOnSubscription
          asyncUsageCount
          combinesWith {
            orderDiscounts
            productDiscounts
            shippingDiscounts
          }
          createdAt
          destinationSelection
          discountClass
          endsAt
          hasTimelineComment
          maximumShippingPrice {
            amount
            currencyCode
          }
          minimumRequirement {
            __typename
            ... on DiscountMinimumQuantity {
              greaterThanOrEqualToQuantity
            }
            ... on DiscountMinimumSubtotal {
              greaterThanOrEqualToSubtotal {
                amount
                currencyCode
              }
            }
          }
          recurringCycleLimit
          shortSummary
          startsAt
          status
          summary
          title
          totalSales {
            amount
            currencyCode
          }
          updatedAt
        }
      }
      metafieldsFirst250: metafields(first: 250) {
        nodes {
          id
          key
          value
        }
      }
    }
    pageInfo {
        endCursor
        hasNextPage
        hasPreviousPage
        startCursor
    }
  }
}
"""

        yield from client.get_graphql_pages(
            query,
            data_items_path="data.discountNodes.nodes[*]",
            pagination_cursor_path="data.discountNodes.pageInfo.endCursor",
            pagination_variable_name="after",
            variables={
                "first": items_per_page,
            },
        )

    @dlt.resource(primary_key="id", write_disposition="merge")
    def taxonomy(items_per_page: int = items_per_page) -> Iterable[TDataItem]:
        client = ShopifyGraphQLApi(
            base_url=shop_url,
            access_token=private_app_password,
            api_version="2024-07",
        )

        query = """
{
  taxonomy {
    categories(first: 250) {
      nodes {
        id
        isArchived
        isLeaf
        isRoot
        level
        name
        parentId
        fullName
        ancestorIds
        attributes(first: 250) {
          nodes {
            ... on TaxonomyAttribute {
              id
            }
            ... on TaxonomyChoiceListAttribute {
              id
              name
            }
            ... on TaxonomyMeasurementAttribute {
              id
              name
            }
          }
        }
      }
      pageInfo {
        endCursor
        hasNextPage
        hasPreviousPage
        startCursor
      }
    }
  }
}
"""

        yield from client.get_graphql_pages(
            query,
            data_items_path="data.taxonomy.categories.nodes[*]",
            pagination_cursor_path="data.taxonomy.categories.pageInfo.endCursor",
            pagination_cursor_has_next_page_path="data.taxonomy.categories.pageInfo.hasNextPage",
            pagination_variable_name="after",
            variables={
                "first": items_per_page,
            },
        )

    @dlt.resource(
        primary_key="id",
        write_disposition="merge",
        columns={
            "id": {
                "data_type": "text",
                "nullable": False,
                "primary_key": True,
                "description": "A globally unique ID for the product.",
            },
            "availablePublicationsCount": {
                "data_type": "json",
                "nullable": False,
                "description": "The number of publications that a resource is published to",
            },
            "category": {
                "data_type": "json",
                "nullable": True,
                "description": "The category of the product from Shopify's Standard Product Taxonomy.",
            },
            "combinedListing": {
                "data_type": "json",
                "nullable": True,
                "description": "A special product type that combines separate products into a single product listing.",
            },
            "combinedListingRole": {
                "data_type": "json",
                "nullable": True,
                "description": "The role of the product in a combined listing.",
            },
            "compareAtPriceRange": {
                "data_type": "json",
                "nullable": True,
                "description": "The compare-at price range of the product in the shop's default currency.",
            },
            "createdAt": {
                "data_type": "timestamp",
                "nullable": False,
                "description": "The date and time when the product was created.",
            },
            "defaultCursor": {
                "data_type": "text",
                "nullable": False,
                "description": "A default cursor that returns the next record sorted by ID.",
            },
            "description": {
                "data_type": "text",
                "nullable": False,
                "description": "A single-line description of the product, with HTML tags removed.",
            },
            "descriptionHtml": {
                "data_type": "text",
                "nullable": False,
                "description": "The description of the product, with HTML tags.",
            },
            "handle": {
                "data_type": "text",
                "nullable": False,
                "description": "A unique, human-readable string of the product's title.",
            },
            "metafields": {
                "data_type": "json",
                "nullable": True,
                "description": "A list of custom fields associated with the product.",
            },
            "options": {
                "data_type": "json",
                "nullable": True,
                "description": "A list of product options, e.g., size, color.",
            },
            "priceRangeV2": {
                "data_type": "json",
                "nullable": False,
                "description": "The minimum and maximum prices of a product.",
            },
            "productType": {
                "data_type": "text",
                "nullable": False,
                "description": "The product type defined by the merchant.",
            },
            "publishedAt": {
                "data_type": "timestamp",
                "nullable": True,
                "description": "The date and time when the product was published.",
            },
            "requiresSellingPlan": {
                "data_type": "bool",
                "nullable": True,
                "description": "Whether the product can only be purchased with a selling plan.",
            },
            "status": {
                "data_type": "text",
                "nullable": False,
                "description": "The product status, which controls visibility across all sales channels.",
            },
            "tags": {
                "data_type": "text",
                "nullable": True,
                "description": "A comma-separated list of searchable keywords associated with the product.",
            },
            "templateSuffix": {
                "data_type": "text",
                "nullable": True,
                "description": "The theme template used when customers view the product in a store.",
            },
            "title": {
                "data_type": "text",
                "nullable": False,
                "description": "The name for the product that displays to customers.",
            },
            "totalInventory": {
                "data_type": "bigint",
                "nullable": False,
                "description": "The quantity of inventory that's in stock.",
            },
            "tracksInventory": {
                "data_type": "bool",
                "nullable": False,
                "description": "Whether inventory tracking is enabled for the product.",
            },
            "updatedAt": {
                "data_type": "timestamp",
                "nullable": False,
                "description": "The date and time when the product was last modified.",
            },
            "variantsFirst250": {
                "data_type": "json",
                "nullable": False,
                "description": "A list of variants associated with the product, first 250.",
            },
            "variantsCount": {
                "data_type": "json",
                "nullable": False,
                "description": "The number of variants associated with the product.",
            },
            "vendor": {
                "data_type": "text",
                "nullable": False,
                "description": "The name of the product's vendor.",
            },
        },
    )
    def products(
        updated_at: dlt.sources.incremental[
            pendulum.DateTime
        ] = dlt.sources.incremental(
            "updatedAt",
            initial_value=start_date_obj,
            end_value=end_date_obj,
            range_end="closed",
            range_start="closed",
        ),
        items_per_page: int = items_per_page,
    ) -> Iterable[TDataItem]:
        client = ShopifyGraphQLApi(
            base_url=shop_url,
            access_token=private_app_password,
            api_version="2024-07",
        )

        query = """
query products($after: String, $query: String, $first: Int)  {
  products(after: $after, first: $first, query: $query) {
    nodes {
      availablePublicationsCount {
        count
        precision
      }
      category {
        id
      }
      combinedListing {
        parentProduct {
          id
        }
      }
      combinedListingRole
      compareAtPriceRange {
        maxVariantCompareAtPrice {
          amount
          currencyCode
        }
        minVariantCompareAtPrice {
          amount
          currencyCode
        }
      }
      createdAt
      defaultCursor
      description
      descriptionHtml
      handle
      id
      metafields(first: 250) {
        nodes {
          id
          key
          value
        }
      }
      options {
        linkedMetafield {
          key
          namespace
        }
        name
        optionValues {
          hasVariants
          id
          linkedMetafieldValue
          name
        }
        values
        id
        position
      }
      priceRangeV2 {
        maxVariantPrice {
          amount
          currencyCode
        }
        minVariantPrice {
          amount
          currencyCode
        }
      }
      productType
      publishedAt
      requiresSellingPlan
      status
      tags
      templateSuffix
      totalInventory
      title
      tracksInventory
      updatedAt
      vendor
      variantsCount {
        count
        precision
      }
      variantsFirst250: variants(first: 250) {
        nodes {
          id
          sku
        }
      }
    }
    pageInfo {
      endCursor
      hasNextPage
      hasPreviousPage
    }
  }
}
"""
        variables = {
            "first": items_per_page,
            "query": f"updated_at:>'{updated_at.last_value.isoformat()}'",
        }

        yield from client.get_graphql_pages(
            query,
            data_items_path="data.products.nodes[*]",
            pagination_cursor_path="data.products.pageInfo.endCursor",
            pagination_cursor_has_next_page_path="data.products.pageInfo.hasNextPage",
            pagination_variable_name="after",
            variables=variables,
        )

    return (
        products,
        products_legacy,
        orders,
        customers,
        inventory_items,
        transactions,
        balance,
        events,
        price_rules,
        discounts,
        taxonomy,
    )
