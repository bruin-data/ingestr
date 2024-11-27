# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._invoice_item import InvoiceItem
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class InvoiceItemService(StripeService):
    class CreateParams(TypedDict):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. Passing in a negative `amount` will reduce the `amount_due` on the invoice.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        customer: str
        """
        The ID of the customer who will be billed when this invoice item is billed.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to the invoice item. The description is displayed in the invoice for easy tracking.
        """
        discountable: NotRequired[bool]
        """
        Controls whether discounts apply to this invoice item. Defaults to false for prorations or negative invoice items, and true for all other invoice items.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceItemService.CreateParamsDiscount]"
        ]
        """
        The coupons and promotion codes to redeem into discounts for the invoice item or invoice line item.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice: NotRequired[str]
        """
        The ID of an existing invoice to add this invoice item to. When left blank, the invoice item will be added to the next upcoming scheduled invoice. This is useful when adding invoice items in response to an invoice.created webhook. You can only add invoice items to draft invoices and there is a maximum of 250 items per invoice.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        period: NotRequired["InvoiceItemService.CreateParamsPeriod"]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired["InvoiceItemService.CreateParamsPriceData"]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the invoice item.
        """
        subscription: NotRequired[str]
        """
        The ID of a subscription to add this invoice item to. When left blank, the invoice item is added to the next upcoming scheduled invoice. When set, scheduled invoices for subscriptions other than the specified subscription will ignore the invoice item. Use this when you want to express that an invoice item has been accrued within the context of a particular subscription.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tax_code: NotRequired["Literal['']|str"]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """
        tax_rates: NotRequired[List[str]]
        """
        The tax rates which apply to the invoice item. When set, the `default_tax_rates` on the invoice do not apply to this invoice item.
        """
        unit_amount: NotRequired[int]
        """
        The integer unit amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. This `unit_amount` will be multiplied by the quantity to get the full amount. Passing in a negative `unit_amount` will reduce the `amount_due` on the invoice.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreateParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreateParamsPeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class CreateParamsPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class DeleteParams(TypedDict):
        pass

    class ListParams(TypedDict):
        created: NotRequired["InvoiceItemService.ListParamsCreated|int"]
        """
        Only return invoice items that were created during the given date interval.
        """
        customer: NotRequired[str]
        """
        The identifier of the customer whose invoice items to return. If none is provided, all invoice items will be returned.
        """
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice: NotRequired[str]
        """
        Only return invoice items belonging to this invoice. If none is provided, all invoice items will be returned. If specifying an invoice, no customer identifier is needed.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        pending: NotRequired[bool]
        """
        Set to `true` to only show pending invoice items, which are not yet attached to any invoices. Set to `false` to only show invoice items already attached to invoices. If unspecified, no filter is applied.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """

    class ListParamsCreated(TypedDict):
        gt: NotRequired[int]
        """
        Minimum value to filter by (exclusive)
        """
        gte: NotRequired[int]
        """
        Minimum value to filter by (inclusive)
        """
        lt: NotRequired[int]
        """
        Maximum value to filter by (exclusive)
        """
        lte: NotRequired[int]
        """
        Maximum value to filter by (inclusive)
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. If you want to apply a credit to the customer's account, pass a negative amount.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to the invoice item. The description is displayed in the invoice for easy tracking.
        """
        discountable: NotRequired[bool]
        """
        Controls whether discounts apply to this invoice item. Defaults to false for prorations or negative invoice items, and true for all other invoice items. Cannot be set to true for prorations.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceItemService.UpdateParamsDiscount]"
        ]
        """
        The coupons, promotion codes & existing discounts which apply to the invoice item or invoice line item. Item discounts are applied before invoice discounts. Pass an empty string to remove previously-defined discounts.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        period: NotRequired["InvoiceItemService.UpdateParamsPeriod"]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired["InvoiceItemService.UpdateParamsPriceData"]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the invoice item.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tax_code: NotRequired["Literal['']|str"]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the invoice item. When set, the `default_tax_rates` on the invoice do not apply to this invoice item. Pass an empty string to remove previously-defined tax rates.
        """
        unit_amount: NotRequired[int]
        """
        The integer unit amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. This unit_amount will be multiplied by the quantity to get the full amount. If you want to apply a credit to the customer's account, pass a negative unit_amount.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpdateParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpdateParamsPeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class UpdateParamsPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    def delete(
        self,
        invoiceitem: str,
        params: "InvoiceItemService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> InvoiceItem:
        """
        Deletes an invoice item, removing it from an invoice. Deleting invoice items is only possible when they're not attached to invoices, or if it's attached to a draft invoice.
        """
        return cast(
            InvoiceItem,
            self._request(
                "delete",
                "/v1/invoiceitems/{invoiceitem}".format(
                    invoiceitem=sanitize_id(invoiceitem),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        invoiceitem: str,
        params: "InvoiceItemService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> InvoiceItem:
        """
        Deletes an invoice item, removing it from an invoice. Deleting invoice items is only possible when they're not attached to invoices, or if it's attached to a draft invoice.
        """
        return cast(
            InvoiceItem,
            await self._request_async(
                "delete",
                "/v1/invoiceitems/{invoiceitem}".format(
                    invoiceitem=sanitize_id(invoiceitem),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        invoiceitem: str,
        params: "InvoiceItemService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> InvoiceItem:
        """
        Retrieves the invoice item with the given ID.
        """
        return cast(
            InvoiceItem,
            self._request(
                "get",
                "/v1/invoiceitems/{invoiceitem}".format(
                    invoiceitem=sanitize_id(invoiceitem),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        invoiceitem: str,
        params: "InvoiceItemService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> InvoiceItem:
        """
        Retrieves the invoice item with the given ID.
        """
        return cast(
            InvoiceItem,
            await self._request_async(
                "get",
                "/v1/invoiceitems/{invoiceitem}".format(
                    invoiceitem=sanitize_id(invoiceitem),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        invoiceitem: str,
        params: "InvoiceItemService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> InvoiceItem:
        """
        Updates the amount or description of an invoice item on an upcoming invoice. Updating an invoice item is only possible before the invoice it's attached to is closed.
        """
        return cast(
            InvoiceItem,
            self._request(
                "post",
                "/v1/invoiceitems/{invoiceitem}".format(
                    invoiceitem=sanitize_id(invoiceitem),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        invoiceitem: str,
        params: "InvoiceItemService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> InvoiceItem:
        """
        Updates the amount or description of an invoice item on an upcoming invoice. Updating an invoice item is only possible before the invoice it's attached to is closed.
        """
        return cast(
            InvoiceItem,
            await self._request_async(
                "post",
                "/v1/invoiceitems/{invoiceitem}".format(
                    invoiceitem=sanitize_id(invoiceitem),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "InvoiceItemService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[InvoiceItem]:
        """
        Returns a list of your invoice items. Invoice items are returned sorted by creation date, with the most recently created invoice items appearing first.
        """
        return cast(
            ListObject[InvoiceItem],
            self._request(
                "get",
                "/v1/invoiceitems",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "InvoiceItemService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[InvoiceItem]:
        """
        Returns a list of your invoice items. Invoice items are returned sorted by creation date, with the most recently created invoice items appearing first.
        """
        return cast(
            ListObject[InvoiceItem],
            await self._request_async(
                "get",
                "/v1/invoiceitems",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "InvoiceItemService.CreateParams",
        options: RequestOptions = {},
    ) -> InvoiceItem:
        """
        Creates an item to be added to a draft invoice (up to 250 items per invoice). If no invoice is specified, the item will be on the next invoice created for the customer specified.
        """
        return cast(
            InvoiceItem,
            self._request(
                "post",
                "/v1/invoiceitems",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "InvoiceItemService.CreateParams",
        options: RequestOptions = {},
    ) -> InvoiceItem:
        """
        Creates an item to be added to a draft invoice (up to 250 items per invoice). If no invoice is specified, the item will be on the next invoice created for the customer specified.
        """
        return cast(
            InvoiceItem,
            await self._request_async(
                "post",
                "/v1/invoiceitems",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
