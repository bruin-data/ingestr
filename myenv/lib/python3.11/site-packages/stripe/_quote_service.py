# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._quote import Quote
from stripe._quote_computed_upfront_line_items_service import (
    QuoteComputedUpfrontLineItemsService,
)
from stripe._quote_line_item_service import QuoteLineItemService
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Any, Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class QuoteService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.line_items = QuoteLineItemService(self._requestor)
        self.computed_upfront_line_items = (
            QuoteComputedUpfrontLineItemsService(
                self._requestor,
            )
        )

    class AcceptParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CancelParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(TypedDict):
        application_fee_amount: NotRequired["Literal['']|int"]
        """
        The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. There cannot be any line items with recurring prices when using this field.
        """
        application_fee_percent: NotRequired["Literal['']|float"]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. There must be at least 1 line item with a recurring price to use this field.
        """
        automatic_tax: NotRequired["QuoteService.CreateParamsAutomaticTax"]
        """
        Settings for automatic tax lookup for this quote and resulting invoices and subscriptions.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay invoices at the end of the subscription cycle or at invoice finalization using the default payment method attached to the subscription or customer. When sending an invoice, Stripe will email your customer an invoice with payment instructions and mark the subscription as `active`. Defaults to `charge_automatically`.
        """
        customer: NotRequired[str]
        """
        The customer for which this quote belongs to. A customer is required before finalizing the quote. Once specified, it cannot be changed.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates that will apply to any line item that does not have `tax_rates` set.
        """
        description: NotRequired["Literal['']|str"]
        """
        A description that will be displayed on the quote PDF. If no value is passed, the default description configured in your [quote template settings](https://dashboard.stripe.com/settings/billing/quote) will be used.
        """
        discounts: NotRequired[
            "Literal['']|List[QuoteService.CreateParamsDiscount]"
        ]
        """
        The discounts applied to the quote. You can only set up to one discount.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expires_at: NotRequired[int]
        """
        A future timestamp on which the quote will be canceled if in `open` or `draft` status. Measured in seconds since the Unix epoch. If no value is passed, the default expiration date configured in your [quote template settings](https://dashboard.stripe.com/settings/billing/quote) will be used.
        """
        footer: NotRequired["Literal['']|str"]
        """
        A footer that will be displayed on the quote PDF. If no value is passed, the default footer configured in your [quote template settings](https://dashboard.stripe.com/settings/billing/quote) will be used.
        """
        from_quote: NotRequired["QuoteService.CreateParamsFromQuote"]
        """
        Clone an existing quote. The new quote will be created in `status=draft`. When using this parameter, you cannot specify any other parameters except for `expires_at`.
        """
        header: NotRequired["Literal['']|str"]
        """
        A header that will be displayed on the quote PDF. If no value is passed, the default header configured in your [quote template settings](https://dashboard.stripe.com/settings/billing/quote) will be used.
        """
        invoice_settings: NotRequired[
            "QuoteService.CreateParamsInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        line_items: NotRequired[List["QuoteService.CreateParamsLineItem"]]
        """
        A list of line items the customer is being quoted for. Each line item includes information about the product, the quantity, and the resulting cost.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account on behalf of which to charge.
        """
        subscription_data: NotRequired[
            "QuoteService.CreateParamsSubscriptionData"
        ]
        """
        When creating a subscription or subscription schedule, the specified configuration data will be used. There must be at least one line item with a recurring price for a subscription or subscription schedule to be created. A subscription schedule is created if `subscription_data[effective_date]` is present and in the future, otherwise a subscription is created.
        """
        test_clock: NotRequired[str]
        """
        ID of the test clock to attach to the quote.
        """
        transfer_data: NotRequired[
            "Literal['']|QuoteService.CreateParamsTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the invoices.
        """

    class CreateParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Controls whether Stripe will automatically compute tax on the resulting invoices or subscriptions as well as the quote itself.
        """
        liability: NotRequired[
            "QuoteService.CreateParamsAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class CreateParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
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

    class CreateParamsFromQuote(TypedDict):
        is_revision: NotRequired[bool]
        """
        Whether this quote is a revision of the previous quote.
        """
        quote: str
        """
        The `id` of the quote that will be cloned.
        """

    class CreateParamsInvoiceSettings(TypedDict):
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay the invoice generated by this quote. This value will be `null` for quotes where `collection_method=charge_automatically`.
        """
        issuer: NotRequired["QuoteService.CreateParamsInvoiceSettingsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class CreateParamsInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsLineItem(TypedDict):
        discounts: NotRequired[
            "Literal['']|List[QuoteService.CreateParamsLineItemDiscount]"
        ]
        """
        The discounts applied to this line item.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired["QuoteService.CreateParamsLineItemPriceData"]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        The quantity of the line item.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the line item. When set, the `default_tax_rates` on the quote do not apply to this line item.
        """

    class CreateParamsLineItemDiscount(TypedDict):
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

    class CreateParamsLineItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: NotRequired[
            "QuoteService.CreateParamsLineItemPriceDataRecurring"
        ]
        """
        The recurring components of a price such as `interval` and `interval_count`.
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

    class CreateParamsLineItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class CreateParamsSubscriptionData(TypedDict):
        description: NotRequired[str]
        """
        The subscription's description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        effective_date: NotRequired[
            "Literal['']|Literal['current_period_end']|int"
        ]
        """
        When creating a new subscription, the date of which the subscription schedule will start after the quote is accepted. When updating a subscription, the date of which the subscription will be updated using a subscription schedule. The special value `current_period_end` can be provided to update a subscription at the end of its current period. The `effective_date` is ignored if it is in the past when the quote is accepted.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that will set metadata on the subscription or subscription schedule when the quote is accepted. If a recurring price is included in `line_items`, this field will be passed to the resulting subscription's `metadata` field. If `subscription_data.effective_date` is used, this field will be passed to the resulting subscription schedule's `phases.metadata` field. Unlike object-level metadata, this field is declarative. Updates will clear prior values.
        """
        trial_period_days: NotRequired["Literal['']|int"]
        """
        Integer representing the number of trial period days before the customer is charged for the first time.
        """

    class CreateParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount that will be transferred automatically when the invoice is paid. If no amount is set, the full amount is transferred. There cannot be any line items with recurring prices when using this field.
        """
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination. There must be at least 1 line item with a recurring price to use this field.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class FinalizeQuoteParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expires_at: NotRequired[int]
        """
        A future timestamp on which the quote will be canceled if in `open` or `draft` status. Measured in seconds since the Unix epoch.
        """

    class ListParams(TypedDict):
        customer: NotRequired[str]
        """
        The ID of the customer whose quotes will be retrieved.
        """
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[Literal["accepted", "canceled", "draft", "open"]]
        """
        The status of the quote.
        """
        test_clock: NotRequired[str]
        """
        Provides a list of quotes that are associated with the specified test clock. The response will not include quotes with test clocks if this and the customer parameter is not set.
        """

    class PdfParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        application_fee_amount: NotRequired["Literal['']|int"]
        """
        The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. There cannot be any line items with recurring prices when using this field.
        """
        application_fee_percent: NotRequired["Literal['']|float"]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. There must be at least 1 line item with a recurring price to use this field.
        """
        automatic_tax: NotRequired["QuoteService.UpdateParamsAutomaticTax"]
        """
        Settings for automatic tax lookup for this quote and resulting invoices and subscriptions.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay invoices at the end of the subscription cycle or at invoice finalization using the default payment method attached to the subscription or customer. When sending an invoice, Stripe will email your customer an invoice with payment instructions and mark the subscription as `active`. Defaults to `charge_automatically`.
        """
        customer: NotRequired[str]
        """
        The customer for which this quote belongs to. A customer is required before finalizing the quote. Once specified, it cannot be changed.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates that will apply to any line item that does not have `tax_rates` set.
        """
        description: NotRequired["Literal['']|str"]
        """
        A description that will be displayed on the quote PDF.
        """
        discounts: NotRequired[
            "Literal['']|List[QuoteService.UpdateParamsDiscount]"
        ]
        """
        The discounts applied to the quote. You can only set up to one discount.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expires_at: NotRequired[int]
        """
        A future timestamp on which the quote will be canceled if in `open` or `draft` status. Measured in seconds since the Unix epoch.
        """
        footer: NotRequired["Literal['']|str"]
        """
        A footer that will be displayed on the quote PDF.
        """
        header: NotRequired["Literal['']|str"]
        """
        A header that will be displayed on the quote PDF.
        """
        invoice_settings: NotRequired[
            "QuoteService.UpdateParamsInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        line_items: NotRequired[List["QuoteService.UpdateParamsLineItem"]]
        """
        A list of line items the customer is being quoted for. Each line item includes information about the product, the quantity, and the resulting cost.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account on behalf of which to charge.
        """
        subscription_data: NotRequired[
            "QuoteService.UpdateParamsSubscriptionData"
        ]
        """
        When creating a subscription or subscription schedule, the specified configuration data will be used. There must be at least one line item with a recurring price for a subscription or subscription schedule to be created. A subscription schedule is created if `subscription_data[effective_date]` is present and in the future, otherwise a subscription is created.
        """
        transfer_data: NotRequired[
            "Literal['']|QuoteService.UpdateParamsTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the invoices.
        """

    class UpdateParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Controls whether Stripe will automatically compute tax on the resulting invoices or subscriptions as well as the quote itself.
        """
        liability: NotRequired[
            "QuoteService.UpdateParamsAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class UpdateParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
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

    class UpdateParamsInvoiceSettings(TypedDict):
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay the invoice generated by this quote. This value will be `null` for quotes where `collection_method=charge_automatically`.
        """
        issuer: NotRequired["QuoteService.UpdateParamsInvoiceSettingsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class UpdateParamsInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpdateParamsLineItem(TypedDict):
        discounts: NotRequired[
            "Literal['']|List[QuoteService.UpdateParamsLineItemDiscount]"
        ]
        """
        The discounts applied to this line item.
        """
        id: NotRequired[str]
        """
        The ID of an existing line item on the quote.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired["QuoteService.UpdateParamsLineItemPriceData"]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        The quantity of the line item.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the line item. When set, the `default_tax_rates` on the quote do not apply to this line item.
        """

    class UpdateParamsLineItemDiscount(TypedDict):
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

    class UpdateParamsLineItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: NotRequired[
            "QuoteService.UpdateParamsLineItemPriceDataRecurring"
        ]
        """
        The recurring components of a price such as `interval` and `interval_count`.
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

    class UpdateParamsLineItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpdateParamsSubscriptionData(TypedDict):
        description: NotRequired["Literal['']|str"]
        """
        The subscription's description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        effective_date: NotRequired[
            "Literal['']|Literal['current_period_end']|int"
        ]
        """
        When creating a new subscription, the date of which the subscription schedule will start after the quote is accepted. When updating a subscription, the date of which the subscription will be updated using a subscription schedule. The special value `current_period_end` can be provided to update a subscription at the end of its current period. The `effective_date` is ignored if it is in the past when the quote is accepted.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that will set metadata on the subscription or subscription schedule when the quote is accepted. If a recurring price is included in `line_items`, this field will be passed to the resulting subscription's `metadata` field. If `subscription_data.effective_date` is used, this field will be passed to the resulting subscription schedule's `phases.metadata` field. Unlike object-level metadata, this field is declarative. Updates will clear prior values.
        """
        trial_period_days: NotRequired["Literal['']|int"]
        """
        Integer representing the number of trial period days before the customer is charged for the first time.
        """

    class UpdateParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount that will be transferred automatically when the invoice is paid. If no amount is set, the full amount is transferred. There cannot be any line items with recurring prices when using this field.
        """
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination. There must be at least 1 line item with a recurring price to use this field.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    def list(
        self,
        params: "QuoteService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Quote]:
        """
        Returns a list of your quotes.
        """
        return cast(
            ListObject[Quote],
            self._request(
                "get",
                "/v1/quotes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "QuoteService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Quote]:
        """
        Returns a list of your quotes.
        """
        return cast(
            ListObject[Quote],
            await self._request_async(
                "get",
                "/v1/quotes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "QuoteService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        A quote models prices and services for a customer. Default options for header, description, footer, and expires_at can be set in the dashboard via the [quote template](https://dashboard.stripe.com/settings/billing/quote).
        """
        return cast(
            Quote,
            self._request(
                "post",
                "/v1/quotes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "QuoteService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        A quote models prices and services for a customer. Default options for header, description, footer, and expires_at can be set in the dashboard via the [quote template](https://dashboard.stripe.com/settings/billing/quote).
        """
        return cast(
            Quote,
            await self._request_async(
                "post",
                "/v1/quotes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        quote: str,
        params: "QuoteService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        Retrieves the quote with the given ID.
        """
        return cast(
            Quote,
            self._request(
                "get",
                "/v1/quotes/{quote}".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        quote: str,
        params: "QuoteService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        Retrieves the quote with the given ID.
        """
        return cast(
            Quote,
            await self._request_async(
                "get",
                "/v1/quotes/{quote}".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        quote: str,
        params: "QuoteService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        A quote models prices and services for a customer.
        """
        return cast(
            Quote,
            self._request(
                "post",
                "/v1/quotes/{quote}".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        quote: str,
        params: "QuoteService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        A quote models prices and services for a customer.
        """
        return cast(
            Quote,
            await self._request_async(
                "post",
                "/v1/quotes/{quote}".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def accept(
        self,
        quote: str,
        params: "QuoteService.AcceptParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        Accepts the specified quote.
        """
        return cast(
            Quote,
            self._request(
                "post",
                "/v1/quotes/{quote}/accept".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def accept_async(
        self,
        quote: str,
        params: "QuoteService.AcceptParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        Accepts the specified quote.
        """
        return cast(
            Quote,
            await self._request_async(
                "post",
                "/v1/quotes/{quote}/accept".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def cancel(
        self,
        quote: str,
        params: "QuoteService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        Cancels the quote.
        """
        return cast(
            Quote,
            self._request(
                "post",
                "/v1/quotes/{quote}/cancel".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        quote: str,
        params: "QuoteService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        Cancels the quote.
        """
        return cast(
            Quote,
            await self._request_async(
                "post",
                "/v1/quotes/{quote}/cancel".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def finalize_quote(
        self,
        quote: str,
        params: "QuoteService.FinalizeQuoteParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        Finalizes the quote.
        """
        return cast(
            Quote,
            self._request(
                "post",
                "/v1/quotes/{quote}/finalize".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def finalize_quote_async(
        self,
        quote: str,
        params: "QuoteService.FinalizeQuoteParams" = {},
        options: RequestOptions = {},
    ) -> Quote:
        """
        Finalizes the quote.
        """
        return cast(
            Quote,
            await self._request_async(
                "post",
                "/v1/quotes/{quote}/finalize".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def pdf(
        self,
        quote: str,
        params: "QuoteService.PdfParams" = {},
        options: RequestOptions = {},
    ) -> Any:
        """
        Download the PDF for a finalized quote. Explanation for special handling can be found [here](https://docs.corp.stripe.com/quotes/overview#quote_pdf)
        """
        return cast(
            Any,
            self._request_stream(
                "get",
                "/v1/quotes/{quote}/pdf".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="files",
                params=params,
                options=options,
            ),
        )

    async def pdf_async(
        self,
        quote: str,
        params: "QuoteService.PdfParams" = {},
        options: RequestOptions = {},
    ) -> Any:
        """
        Download the PDF for a finalized quote. Explanation for special handling can be found [here](https://docs.corp.stripe.com/quotes/overview#quote_pdf)
        """
        return cast(
            Any,
            await self._request_stream_async(
                "get",
                "/v1/quotes/{quote}/pdf".format(quote=sanitize_id(quote)),
                api_mode="V1",
                base_address="files",
                params=params,
                options=options,
            ),
        )
