# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import Any, ClassVar, Dict, List, Optional, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._account import Account
    from stripe._application import Application
    from stripe._customer import Customer
    from stripe._discount import Discount as DiscountResource
    from stripe._invoice import Invoice
    from stripe._line_item import LineItem
    from stripe._subscription import Subscription
    from stripe._subscription_schedule import SubscriptionSchedule
    from stripe._tax_rate import TaxRate
    from stripe.test_helpers._test_clock import TestClock


class Quote(
    CreateableAPIResource["Quote"],
    ListableAPIResource["Quote"],
    UpdateableAPIResource["Quote"],
):
    """
    A Quote is a way to model prices that you'd like to provide to a customer.
    Once accepted, it will automatically create an invoice, subscription or subscription schedule.
    """

    OBJECT_NAME: ClassVar[Literal["quote"]] = "quote"

    class AutomaticTax(StripeObject):
        class Liability(StripeObject):
            account: Optional[ExpandableField["Account"]]
            """
            The connected account being referenced when `type` is `account`.
            """
            type: Literal["account", "self"]
            """
            Type of the account referenced.
            """

        enabled: bool
        """
        Automatically calculate taxes
        """
        liability: Optional[Liability]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """
        status: Optional[
            Literal["complete", "failed", "requires_location_inputs"]
        ]
        """
        The status of the most recent automated tax calculation for this quote.
        """
        _inner_class_types = {"liability": Liability}

    class Computed(StripeObject):
        class Recurring(StripeObject):
            class TotalDetails(StripeObject):
                class Breakdown(StripeObject):
                    class Discount(StripeObject):
                        amount: int
                        """
                        The amount discounted.
                        """
                        discount: "DiscountResource"
                        """
                        A discount represents the actual application of a [coupon](https://stripe.com/docs/api#coupons) or [promotion code](https://stripe.com/docs/api#promotion_codes).
                        It contains information about when the discount began, when it will end, and what it is applied to.

                        Related guide: [Applying discounts to subscriptions](https://stripe.com/docs/billing/subscriptions/discounts)
                        """

                    class Tax(StripeObject):
                        amount: int
                        """
                        Amount of tax applied for this rate.
                        """
                        rate: "TaxRate"
                        """
                        Tax rates can be applied to [invoices](https://stripe.com/docs/billing/invoices/tax-rates), [subscriptions](https://stripe.com/docs/billing/subscriptions/taxes) and [Checkout Sessions](https://stripe.com/docs/payments/checkout/set-up-a-subscription#tax-rates) to collect tax.

                        Related guide: [Tax rates](https://stripe.com/docs/billing/taxes/tax-rates)
                        """
                        taxability_reason: Optional[
                            Literal[
                                "customer_exempt",
                                "not_collecting",
                                "not_subject_to_tax",
                                "not_supported",
                                "portion_product_exempt",
                                "portion_reduced_rated",
                                "portion_standard_rated",
                                "product_exempt",
                                "product_exempt_holiday",
                                "proportionally_rated",
                                "reduced_rated",
                                "reverse_charge",
                                "standard_rated",
                                "taxable_basis_reduced",
                                "zero_rated",
                            ]
                        ]
                        """
                        The reasoning behind this tax, for example, if the product is tax exempt. The possible values for this field may be extended as new tax rules are supported.
                        """
                        taxable_amount: Optional[int]
                        """
                        The amount on which tax is calculated, in cents (or local equivalent).
                        """

                    discounts: List[Discount]
                    """
                    The aggregated discounts.
                    """
                    taxes: List[Tax]
                    """
                    The aggregated tax amounts by rate.
                    """
                    _inner_class_types = {"discounts": Discount, "taxes": Tax}

                amount_discount: int
                """
                This is the sum of all the discounts.
                """
                amount_shipping: Optional[int]
                """
                This is the sum of all the shipping amounts.
                """
                amount_tax: int
                """
                This is the sum of all the tax amounts.
                """
                breakdown: Optional[Breakdown]
                _inner_class_types = {"breakdown": Breakdown}

            amount_subtotal: int
            """
            Total before any discounts or taxes are applied.
            """
            amount_total: int
            """
            Total after discounts and taxes are applied.
            """
            interval: Literal["day", "month", "week", "year"]
            """
            The frequency at which a subscription is billed. One of `day`, `week`, `month` or `year`.
            """
            interval_count: int
            """
            The number of intervals (specified in the `interval` attribute) between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months.
            """
            total_details: TotalDetails
            _inner_class_types = {"total_details": TotalDetails}

        class Upfront(StripeObject):
            class TotalDetails(StripeObject):
                class Breakdown(StripeObject):
                    class Discount(StripeObject):
                        amount: int
                        """
                        The amount discounted.
                        """
                        discount: "DiscountResource"
                        """
                        A discount represents the actual application of a [coupon](https://stripe.com/docs/api#coupons) or [promotion code](https://stripe.com/docs/api#promotion_codes).
                        It contains information about when the discount began, when it will end, and what it is applied to.

                        Related guide: [Applying discounts to subscriptions](https://stripe.com/docs/billing/subscriptions/discounts)
                        """

                    class Tax(StripeObject):
                        amount: int
                        """
                        Amount of tax applied for this rate.
                        """
                        rate: "TaxRate"
                        """
                        Tax rates can be applied to [invoices](https://stripe.com/docs/billing/invoices/tax-rates), [subscriptions](https://stripe.com/docs/billing/subscriptions/taxes) and [Checkout Sessions](https://stripe.com/docs/payments/checkout/set-up-a-subscription#tax-rates) to collect tax.

                        Related guide: [Tax rates](https://stripe.com/docs/billing/taxes/tax-rates)
                        """
                        taxability_reason: Optional[
                            Literal[
                                "customer_exempt",
                                "not_collecting",
                                "not_subject_to_tax",
                                "not_supported",
                                "portion_product_exempt",
                                "portion_reduced_rated",
                                "portion_standard_rated",
                                "product_exempt",
                                "product_exempt_holiday",
                                "proportionally_rated",
                                "reduced_rated",
                                "reverse_charge",
                                "standard_rated",
                                "taxable_basis_reduced",
                                "zero_rated",
                            ]
                        ]
                        """
                        The reasoning behind this tax, for example, if the product is tax exempt. The possible values for this field may be extended as new tax rules are supported.
                        """
                        taxable_amount: Optional[int]
                        """
                        The amount on which tax is calculated, in cents (or local equivalent).
                        """

                    discounts: List[Discount]
                    """
                    The aggregated discounts.
                    """
                    taxes: List[Tax]
                    """
                    The aggregated tax amounts by rate.
                    """
                    _inner_class_types = {"discounts": Discount, "taxes": Tax}

                amount_discount: int
                """
                This is the sum of all the discounts.
                """
                amount_shipping: Optional[int]
                """
                This is the sum of all the shipping amounts.
                """
                amount_tax: int
                """
                This is the sum of all the tax amounts.
                """
                breakdown: Optional[Breakdown]
                _inner_class_types = {"breakdown": Breakdown}

            amount_subtotal: int
            """
            Total before any discounts or taxes are applied.
            """
            amount_total: int
            """
            Total after discounts and taxes are applied.
            """
            line_items: Optional[ListObject["LineItem"]]
            """
            The line items that will appear on the next invoice after this quote is accepted. This does not include pending invoice items that exist on the customer but may still be included in the next invoice.
            """
            total_details: TotalDetails
            _inner_class_types = {"total_details": TotalDetails}

        recurring: Optional[Recurring]
        """
        The definitive totals and line items the customer will be charged on a recurring basis. Takes into account the line items with recurring prices and discounts with `duration=forever` coupons only. Defaults to `null` if no inputted line items with recurring prices.
        """
        upfront: Upfront
        _inner_class_types = {"recurring": Recurring, "upfront": Upfront}

    class FromQuote(StripeObject):
        is_revision: bool
        """
        Whether this quote is a revision of a different quote.
        """
        quote: ExpandableField["Quote"]
        """
        The quote that was cloned.
        """

    class InvoiceSettings(StripeObject):
        class Issuer(StripeObject):
            account: Optional[ExpandableField["Account"]]
            """
            The connected account being referenced when `type` is `account`.
            """
            type: Literal["account", "self"]
            """
            Type of the account referenced.
            """

        days_until_due: Optional[int]
        """
        Number of days within which a customer must pay invoices generated by this quote. This value will be `null` for quotes where `collection_method=charge_automatically`.
        """
        issuer: Issuer
        _inner_class_types = {"issuer": Issuer}

    class StatusTransitions(StripeObject):
        accepted_at: Optional[int]
        """
        The time that the quote was accepted. Measured in seconds since Unix epoch.
        """
        canceled_at: Optional[int]
        """
        The time that the quote was canceled. Measured in seconds since Unix epoch.
        """
        finalized_at: Optional[int]
        """
        The time that the quote was finalized. Measured in seconds since Unix epoch.
        """

    class SubscriptionData(StripeObject):
        description: Optional[str]
        """
        The subscription's description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        effective_date: Optional[int]
        """
        When creating a new subscription, the date of which the subscription schedule will start after the quote is accepted. This date is ignored if it is in the past when the quote is accepted. Measured in seconds since the Unix epoch.
        """
        metadata: Optional[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that will set metadata on the subscription or subscription schedule when the quote is accepted. If a recurring price is included in `line_items`, this field will be passed to the resulting subscription's `metadata` field. If `subscription_data.effective_date` is used, this field will be passed to the resulting subscription schedule's `phases.metadata` field. Unlike object-level metadata, this field is declarative. Updates will clear prior values.
        """
        trial_period_days: Optional[int]
        """
        Integer representing the number of trial period days before the customer is charged for the first time.
        """

    class TotalDetails(StripeObject):
        class Breakdown(StripeObject):
            class Discount(StripeObject):
                amount: int
                """
                The amount discounted.
                """
                discount: "DiscountResource"
                """
                A discount represents the actual application of a [coupon](https://stripe.com/docs/api#coupons) or [promotion code](https://stripe.com/docs/api#promotion_codes).
                It contains information about when the discount began, when it will end, and what it is applied to.

                Related guide: [Applying discounts to subscriptions](https://stripe.com/docs/billing/subscriptions/discounts)
                """

            class Tax(StripeObject):
                amount: int
                """
                Amount of tax applied for this rate.
                """
                rate: "TaxRate"
                """
                Tax rates can be applied to [invoices](https://stripe.com/docs/billing/invoices/tax-rates), [subscriptions](https://stripe.com/docs/billing/subscriptions/taxes) and [Checkout Sessions](https://stripe.com/docs/payments/checkout/set-up-a-subscription#tax-rates) to collect tax.

                Related guide: [Tax rates](https://stripe.com/docs/billing/taxes/tax-rates)
                """
                taxability_reason: Optional[
                    Literal[
                        "customer_exempt",
                        "not_collecting",
                        "not_subject_to_tax",
                        "not_supported",
                        "portion_product_exempt",
                        "portion_reduced_rated",
                        "portion_standard_rated",
                        "product_exempt",
                        "product_exempt_holiday",
                        "proportionally_rated",
                        "reduced_rated",
                        "reverse_charge",
                        "standard_rated",
                        "taxable_basis_reduced",
                        "zero_rated",
                    ]
                ]
                """
                The reasoning behind this tax, for example, if the product is tax exempt. The possible values for this field may be extended as new tax rules are supported.
                """
                taxable_amount: Optional[int]
                """
                The amount on which tax is calculated, in cents (or local equivalent).
                """

            discounts: List[Discount]
            """
            The aggregated discounts.
            """
            taxes: List[Tax]
            """
            The aggregated tax amounts by rate.
            """
            _inner_class_types = {"discounts": Discount, "taxes": Tax}

        amount_discount: int
        """
        This is the sum of all the discounts.
        """
        amount_shipping: Optional[int]
        """
        This is the sum of all the shipping amounts.
        """
        amount_tax: int
        """
        This is the sum of all the tax amounts.
        """
        breakdown: Optional[Breakdown]
        _inner_class_types = {"breakdown": Breakdown}

    class TransferData(StripeObject):
        amount: Optional[int]
        """
        The amount in cents (or local equivalent) that will be transferred to the destination account when the invoice is paid. By default, the entire amount is transferred to the destination.
        """
        amount_percent: Optional[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount will be transferred to the destination.
        """
        destination: ExpandableField["Account"]
        """
        The account where funds from the payment will be transferred to upon payment success.
        """

    class AcceptParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CancelParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(RequestOptions):
        application_fee_amount: NotRequired["Literal['']|int"]
        """
        The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. There cannot be any line items with recurring prices when using this field.
        """
        application_fee_percent: NotRequired["Literal['']|float"]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. There must be at least 1 line item with a recurring price to use this field.
        """
        automatic_tax: NotRequired["Quote.CreateParamsAutomaticTax"]
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
        discounts: NotRequired["Literal['']|List[Quote.CreateParamsDiscount]"]
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
        from_quote: NotRequired["Quote.CreateParamsFromQuote"]
        """
        Clone an existing quote. The new quote will be created in `status=draft`. When using this parameter, you cannot specify any other parameters except for `expires_at`.
        """
        header: NotRequired["Literal['']|str"]
        """
        A header that will be displayed on the quote PDF. If no value is passed, the default header configured in your [quote template settings](https://dashboard.stripe.com/settings/billing/quote) will be used.
        """
        invoice_settings: NotRequired["Quote.CreateParamsInvoiceSettings"]
        """
        All invoices will be billed using the specified settings.
        """
        line_items: NotRequired[List["Quote.CreateParamsLineItem"]]
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
        subscription_data: NotRequired["Quote.CreateParamsSubscriptionData"]
        """
        When creating a subscription or subscription schedule, the specified configuration data will be used. There must be at least one line item with a recurring price for a subscription or subscription schedule to be created. A subscription schedule is created if `subscription_data[effective_date]` is present and in the future, otherwise a subscription is created.
        """
        test_clock: NotRequired[str]
        """
        ID of the test clock to attach to the quote.
        """
        transfer_data: NotRequired[
            "Literal['']|Quote.CreateParamsTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the invoices.
        """

    class CreateParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Controls whether Stripe will automatically compute tax on the resulting invoices or subscriptions as well as the quote itself.
        """
        liability: NotRequired["Quote.CreateParamsAutomaticTaxLiability"]
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
        issuer: NotRequired["Quote.CreateParamsInvoiceSettingsIssuer"]
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
            "Literal['']|List[Quote.CreateParamsLineItemDiscount]"
        ]
        """
        The discounts applied to this line item.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired["Quote.CreateParamsLineItemPriceData"]
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
        recurring: NotRequired["Quote.CreateParamsLineItemPriceDataRecurring"]
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

    class FinalizeQuoteParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expires_at: NotRequired[int]
        """
        A future timestamp on which the quote will be canceled if in `open` or `draft` status. Measured in seconds since the Unix epoch.
        """

    class ListComputedUpfrontLineItemsParams(RequestOptions):
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

    class ListLineItemsParams(RequestOptions):
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

    class ListParams(RequestOptions):
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

    class ModifyParams(RequestOptions):
        application_fee_amount: NotRequired["Literal['']|int"]
        """
        The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. There cannot be any line items with recurring prices when using this field.
        """
        application_fee_percent: NotRequired["Literal['']|float"]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. There must be at least 1 line item with a recurring price to use this field.
        """
        automatic_tax: NotRequired["Quote.ModifyParamsAutomaticTax"]
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
        discounts: NotRequired["Literal['']|List[Quote.ModifyParamsDiscount]"]
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
        invoice_settings: NotRequired["Quote.ModifyParamsInvoiceSettings"]
        """
        All invoices will be billed using the specified settings.
        """
        line_items: NotRequired[List["Quote.ModifyParamsLineItem"]]
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
        subscription_data: NotRequired["Quote.ModifyParamsSubscriptionData"]
        """
        When creating a subscription or subscription schedule, the specified configuration data will be used. There must be at least one line item with a recurring price for a subscription or subscription schedule to be created. A subscription schedule is created if `subscription_data[effective_date]` is present and in the future, otherwise a subscription is created.
        """
        transfer_data: NotRequired[
            "Literal['']|Quote.ModifyParamsTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the invoices.
        """

    class ModifyParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Controls whether Stripe will automatically compute tax on the resulting invoices or subscriptions as well as the quote itself.
        """
        liability: NotRequired["Quote.ModifyParamsAutomaticTaxLiability"]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class ModifyParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class ModifyParamsDiscount(TypedDict):
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

    class ModifyParamsInvoiceSettings(TypedDict):
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay the invoice generated by this quote. This value will be `null` for quotes where `collection_method=charge_automatically`.
        """
        issuer: NotRequired["Quote.ModifyParamsInvoiceSettingsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class ModifyParamsInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class ModifyParamsLineItem(TypedDict):
        discounts: NotRequired[
            "Literal['']|List[Quote.ModifyParamsLineItemDiscount]"
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
        price_data: NotRequired["Quote.ModifyParamsLineItemPriceData"]
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

    class ModifyParamsLineItemDiscount(TypedDict):
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

    class ModifyParamsLineItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: NotRequired["Quote.ModifyParamsLineItemPriceDataRecurring"]
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

    class ModifyParamsLineItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class ModifyParamsSubscriptionData(TypedDict):
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

    class ModifyParamsTransferData(TypedDict):
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

    class PdfParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    amount_subtotal: int
    """
    Total before any discounts or taxes are applied.
    """
    amount_total: int
    """
    Total after discounts and taxes are applied.
    """
    application: Optional[ExpandableField["Application"]]
    """
    ID of the Connect Application that created the quote.
    """
    application_fee_amount: Optional[int]
    """
    The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. Only applicable if there are no line items with recurring prices on the quote.
    """
    application_fee_percent: Optional[float]
    """
    A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. Only applicable if there are line items with recurring prices on the quote.
    """
    automatic_tax: AutomaticTax
    collection_method: Literal["charge_automatically", "send_invoice"]
    """
    Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay invoices at the end of the subscription cycle or on finalization using the default payment method attached to the subscription or customer. When sending an invoice, Stripe will email your customer an invoice with payment instructions and mark the subscription as `active`. Defaults to `charge_automatically`.
    """
    computed: Computed
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: Optional[str]
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    customer: Optional[ExpandableField["Customer"]]
    """
    The customer which this quote belongs to. A customer is required before finalizing the quote. Once specified, it cannot be changed.
    """
    default_tax_rates: Optional[List[ExpandableField["TaxRate"]]]
    """
    The tax rates applied to this quote.
    """
    description: Optional[str]
    """
    A description that will be displayed on the quote PDF.
    """
    discounts: List[ExpandableField["DiscountResource"]]
    """
    The discounts applied to this quote.
    """
    expires_at: int
    """
    The date on which the quote will be canceled if in `open` or `draft` status. Measured in seconds since the Unix epoch.
    """
    footer: Optional[str]
    """
    A footer that will be displayed on the quote PDF.
    """
    from_quote: Optional[FromQuote]
    """
    Details of the quote that was cloned. See the [cloning documentation](https://stripe.com/docs/quotes/clone) for more details.
    """
    header: Optional[str]
    """
    A header that will be displayed on the quote PDF.
    """
    id: str
    """
    Unique identifier for the object.
    """
    invoice: Optional[ExpandableField["Invoice"]]
    """
    The invoice that was created from this quote.
    """
    invoice_settings: InvoiceSettings
    line_items: Optional[ListObject["LineItem"]]
    """
    A list of items the customer is being quoted for.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    number: Optional[str]
    """
    A unique number that identifies this particular quote. This number is assigned once the quote is [finalized](https://stripe.com/docs/quotes/overview#finalize).
    """
    object: Literal["quote"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    on_behalf_of: Optional[ExpandableField["Account"]]
    """
    The account on behalf of which to charge. See the [Connect documentation](https://support.stripe.com/questions/sending-invoices-on-behalf-of-connected-accounts) for details.
    """
    status: Literal["accepted", "canceled", "draft", "open"]
    """
    The status of the quote.
    """
    status_transitions: StatusTransitions
    subscription: Optional[ExpandableField["Subscription"]]
    """
    The subscription that was created or updated from this quote.
    """
    subscription_data: SubscriptionData
    subscription_schedule: Optional[ExpandableField["SubscriptionSchedule"]]
    """
    The subscription schedule that was created or updated from this quote.
    """
    test_clock: Optional[ExpandableField["TestClock"]]
    """
    ID of the test clock this quote belongs to.
    """
    total_details: TotalDetails
    transfer_data: Optional[TransferData]
    """
    The account (if any) the payments will be attributed to for tax reporting, and where funds from each payment will be transferred to for each of the invoices.
    """

    @classmethod
    def _cls_accept(
        cls, quote: str, **params: Unpack["Quote.AcceptParams"]
    ) -> "Quote":
        """
        Accepts the specified quote.
        """
        return cast(
            "Quote",
            cls._static_request(
                "post",
                "/v1/quotes/{quote}/accept".format(quote=sanitize_id(quote)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def accept(quote: str, **params: Unpack["Quote.AcceptParams"]) -> "Quote":
        """
        Accepts the specified quote.
        """
        ...

    @overload
    def accept(self, **params: Unpack["Quote.AcceptParams"]) -> "Quote":
        """
        Accepts the specified quote.
        """
        ...

    @class_method_variant("_cls_accept")
    def accept(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.AcceptParams"]
    ) -> "Quote":
        """
        Accepts the specified quote.
        """
        return cast(
            "Quote",
            self._request(
                "post",
                "/v1/quotes/{quote}/accept".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_accept_async(
        cls, quote: str, **params: Unpack["Quote.AcceptParams"]
    ) -> "Quote":
        """
        Accepts the specified quote.
        """
        return cast(
            "Quote",
            await cls._static_request_async(
                "post",
                "/v1/quotes/{quote}/accept".format(quote=sanitize_id(quote)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def accept_async(
        quote: str, **params: Unpack["Quote.AcceptParams"]
    ) -> "Quote":
        """
        Accepts the specified quote.
        """
        ...

    @overload
    async def accept_async(
        self, **params: Unpack["Quote.AcceptParams"]
    ) -> "Quote":
        """
        Accepts the specified quote.
        """
        ...

    @class_method_variant("_cls_accept_async")
    async def accept_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.AcceptParams"]
    ) -> "Quote":
        """
        Accepts the specified quote.
        """
        return cast(
            "Quote",
            await self._request_async(
                "post",
                "/v1/quotes/{quote}/accept".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def _cls_cancel(
        cls, quote: str, **params: Unpack["Quote.CancelParams"]
    ) -> "Quote":
        """
        Cancels the quote.
        """
        return cast(
            "Quote",
            cls._static_request(
                "post",
                "/v1/quotes/{quote}/cancel".format(quote=sanitize_id(quote)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def cancel(quote: str, **params: Unpack["Quote.CancelParams"]) -> "Quote":
        """
        Cancels the quote.
        """
        ...

    @overload
    def cancel(self, **params: Unpack["Quote.CancelParams"]) -> "Quote":
        """
        Cancels the quote.
        """
        ...

    @class_method_variant("_cls_cancel")
    def cancel(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.CancelParams"]
    ) -> "Quote":
        """
        Cancels the quote.
        """
        return cast(
            "Quote",
            self._request(
                "post",
                "/v1/quotes/{quote}/cancel".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_cancel_async(
        cls, quote: str, **params: Unpack["Quote.CancelParams"]
    ) -> "Quote":
        """
        Cancels the quote.
        """
        return cast(
            "Quote",
            await cls._static_request_async(
                "post",
                "/v1/quotes/{quote}/cancel".format(quote=sanitize_id(quote)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def cancel_async(
        quote: str, **params: Unpack["Quote.CancelParams"]
    ) -> "Quote":
        """
        Cancels the quote.
        """
        ...

    @overload
    async def cancel_async(
        self, **params: Unpack["Quote.CancelParams"]
    ) -> "Quote":
        """
        Cancels the quote.
        """
        ...

    @class_method_variant("_cls_cancel_async")
    async def cancel_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.CancelParams"]
    ) -> "Quote":
        """
        Cancels the quote.
        """
        return cast(
            "Quote",
            await self._request_async(
                "post",
                "/v1/quotes/{quote}/cancel".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def create(cls, **params: Unpack["Quote.CreateParams"]) -> "Quote":
        """
        A quote models prices and services for a customer. Default options for header, description, footer, and expires_at can be set in the dashboard via the [quote template](https://dashboard.stripe.com/settings/billing/quote).
        """
        return cast(
            "Quote",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Quote.CreateParams"]
    ) -> "Quote":
        """
        A quote models prices and services for a customer. Default options for header, description, footer, and expires_at can be set in the dashboard via the [quote template](https://dashboard.stripe.com/settings/billing/quote).
        """
        return cast(
            "Quote",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_finalize_quote(
        cls, quote: str, **params: Unpack["Quote.FinalizeQuoteParams"]
    ) -> "Quote":
        """
        Finalizes the quote.
        """
        return cast(
            "Quote",
            cls._static_request(
                "post",
                "/v1/quotes/{quote}/finalize".format(quote=sanitize_id(quote)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def finalize_quote(
        quote: str, **params: Unpack["Quote.FinalizeQuoteParams"]
    ) -> "Quote":
        """
        Finalizes the quote.
        """
        ...

    @overload
    def finalize_quote(
        self, **params: Unpack["Quote.FinalizeQuoteParams"]
    ) -> "Quote":
        """
        Finalizes the quote.
        """
        ...

    @class_method_variant("_cls_finalize_quote")
    def finalize_quote(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.FinalizeQuoteParams"]
    ) -> "Quote":
        """
        Finalizes the quote.
        """
        return cast(
            "Quote",
            self._request(
                "post",
                "/v1/quotes/{quote}/finalize".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_finalize_quote_async(
        cls, quote: str, **params: Unpack["Quote.FinalizeQuoteParams"]
    ) -> "Quote":
        """
        Finalizes the quote.
        """
        return cast(
            "Quote",
            await cls._static_request_async(
                "post",
                "/v1/quotes/{quote}/finalize".format(quote=sanitize_id(quote)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def finalize_quote_async(
        quote: str, **params: Unpack["Quote.FinalizeQuoteParams"]
    ) -> "Quote":
        """
        Finalizes the quote.
        """
        ...

    @overload
    async def finalize_quote_async(
        self, **params: Unpack["Quote.FinalizeQuoteParams"]
    ) -> "Quote":
        """
        Finalizes the quote.
        """
        ...

    @class_method_variant("_cls_finalize_quote_async")
    async def finalize_quote_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.FinalizeQuoteParams"]
    ) -> "Quote":
        """
        Finalizes the quote.
        """
        return cast(
            "Quote",
            await self._request_async(
                "post",
                "/v1/quotes/{quote}/finalize".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def list(cls, **params: Unpack["Quote.ListParams"]) -> ListObject["Quote"]:
        """
        Returns a list of your quotes.
        """
        result = cls._static_request(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    async def list_async(
        cls, **params: Unpack["Quote.ListParams"]
    ) -> ListObject["Quote"]:
        """
        Returns a list of your quotes.
        """
        result = await cls._static_request_async(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    def _cls_list_computed_upfront_line_items(
        cls,
        quote: str,
        **params: Unpack["Quote.ListComputedUpfrontLineItemsParams"],
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable [computed.upfront.line_items](https://stripe.com/docs/api/quotes/object#quote_object-computed-upfront-line_items) property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of upfront line items.
        """
        return cast(
            ListObject["LineItem"],
            cls._static_request(
                "get",
                "/v1/quotes/{quote}/computed_upfront_line_items".format(
                    quote=sanitize_id(quote)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def list_computed_upfront_line_items(
        quote: str,
        **params: Unpack["Quote.ListComputedUpfrontLineItemsParams"],
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable [computed.upfront.line_items](https://stripe.com/docs/api/quotes/object#quote_object-computed-upfront-line_items) property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of upfront line items.
        """
        ...

    @overload
    def list_computed_upfront_line_items(
        self, **params: Unpack["Quote.ListComputedUpfrontLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable [computed.upfront.line_items](https://stripe.com/docs/api/quotes/object#quote_object-computed-upfront-line_items) property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of upfront line items.
        """
        ...

    @class_method_variant("_cls_list_computed_upfront_line_items")
    def list_computed_upfront_line_items(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.ListComputedUpfrontLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable [computed.upfront.line_items](https://stripe.com/docs/api/quotes/object#quote_object-computed-upfront-line_items) property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of upfront line items.
        """
        return cast(
            ListObject["LineItem"],
            self._request(
                "get",
                "/v1/quotes/{quote}/computed_upfront_line_items".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_list_computed_upfront_line_items_async(
        cls,
        quote: str,
        **params: Unpack["Quote.ListComputedUpfrontLineItemsParams"],
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable [computed.upfront.line_items](https://stripe.com/docs/api/quotes/object#quote_object-computed-upfront-line_items) property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of upfront line items.
        """
        return cast(
            ListObject["LineItem"],
            await cls._static_request_async(
                "get",
                "/v1/quotes/{quote}/computed_upfront_line_items".format(
                    quote=sanitize_id(quote)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def list_computed_upfront_line_items_async(
        quote: str,
        **params: Unpack["Quote.ListComputedUpfrontLineItemsParams"],
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable [computed.upfront.line_items](https://stripe.com/docs/api/quotes/object#quote_object-computed-upfront-line_items) property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of upfront line items.
        """
        ...

    @overload
    async def list_computed_upfront_line_items_async(
        self, **params: Unpack["Quote.ListComputedUpfrontLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable [computed.upfront.line_items](https://stripe.com/docs/api/quotes/object#quote_object-computed-upfront-line_items) property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of upfront line items.
        """
        ...

    @class_method_variant("_cls_list_computed_upfront_line_items_async")
    async def list_computed_upfront_line_items_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.ListComputedUpfrontLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable [computed.upfront.line_items](https://stripe.com/docs/api/quotes/object#quote_object-computed-upfront-line_items) property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of upfront line items.
        """
        return cast(
            ListObject["LineItem"],
            await self._request_async(
                "get",
                "/v1/quotes/{quote}/computed_upfront_line_items".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def _cls_list_line_items(
        cls, quote: str, **params: Unpack["Quote.ListLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable line_items property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["LineItem"],
            cls._static_request(
                "get",
                "/v1/quotes/{quote}/line_items".format(
                    quote=sanitize_id(quote)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def list_line_items(
        quote: str, **params: Unpack["Quote.ListLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable line_items property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        ...

    @overload
    def list_line_items(
        self, **params: Unpack["Quote.ListLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable line_items property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        ...

    @class_method_variant("_cls_list_line_items")
    def list_line_items(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.ListLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable line_items property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["LineItem"],
            self._request(
                "get",
                "/v1/quotes/{quote}/line_items".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_list_line_items_async(
        cls, quote: str, **params: Unpack["Quote.ListLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable line_items property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["LineItem"],
            await cls._static_request_async(
                "get",
                "/v1/quotes/{quote}/line_items".format(
                    quote=sanitize_id(quote)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def list_line_items_async(
        quote: str, **params: Unpack["Quote.ListLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable line_items property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        ...

    @overload
    async def list_line_items_async(
        self, **params: Unpack["Quote.ListLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable line_items property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        ...

    @class_method_variant("_cls_list_line_items_async")
    async def list_line_items_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.ListLineItemsParams"]
    ) -> ListObject["LineItem"]:
        """
        When retrieving a quote, there is an includable line_items property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["LineItem"],
            await self._request_async(
                "get",
                "/v1/quotes/{quote}/line_items".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def modify(
        cls, id: str, **params: Unpack["Quote.ModifyParams"]
    ) -> "Quote":
        """
        A quote models prices and services for a customer.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Quote",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Quote.ModifyParams"]
    ) -> "Quote":
        """
        A quote models prices and services for a customer.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Quote",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def _cls_pdf(cls, quote: str, **params: Unpack["Quote.PdfParams"]) -> Any:
        """
        Download the PDF for a finalized quote. Explanation for special handling can be found [here](https://docs.corp.stripe.com/quotes/overview#quote_pdf)
        """
        return cast(
            Any,
            cls._static_request_stream(
                "get",
                "/v1/quotes/{quote}/pdf".format(quote=sanitize_id(quote)),
                params=params,
                base_address="files",
            ),
        )

    @overload
    @staticmethod
    def pdf(quote: str, **params: Unpack["Quote.PdfParams"]) -> Any:
        """
        Download the PDF for a finalized quote. Explanation for special handling can be found [here](https://docs.corp.stripe.com/quotes/overview#quote_pdf)
        """
        ...

    @overload
    def pdf(self, **params: Unpack["Quote.PdfParams"]) -> Any:
        """
        Download the PDF for a finalized quote. Explanation for special handling can be found [here](https://docs.corp.stripe.com/quotes/overview#quote_pdf)
        """
        ...

    @class_method_variant("_cls_pdf")
    def pdf(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.PdfParams"]
    ) -> Any:
        """
        Download the PDF for a finalized quote. Explanation for special handling can be found [here](https://docs.corp.stripe.com/quotes/overview#quote_pdf)
        """
        return cast(
            Any,
            self._request_stream(
                "get",
                "/v1/quotes/{quote}/pdf".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
                base_address="files",
            ),
        )

    @classmethod
    async def _cls_pdf_async(
        cls, quote: str, **params: Unpack["Quote.PdfParams"]
    ) -> Any:
        """
        Download the PDF for a finalized quote. Explanation for special handling can be found [here](https://docs.corp.stripe.com/quotes/overview#quote_pdf)
        """
        return cast(
            Any,
            await cls._static_request_stream_async(
                "get",
                "/v1/quotes/{quote}/pdf".format(quote=sanitize_id(quote)),
                params=params,
                base_address="files",
            ),
        )

    @overload
    @staticmethod
    async def pdf_async(
        quote: str, **params: Unpack["Quote.PdfParams"]
    ) -> Any:
        """
        Download the PDF for a finalized quote. Explanation for special handling can be found [here](https://docs.corp.stripe.com/quotes/overview#quote_pdf)
        """
        ...

    @overload
    async def pdf_async(self, **params: Unpack["Quote.PdfParams"]) -> Any:
        """
        Download the PDF for a finalized quote. Explanation for special handling can be found [here](https://docs.corp.stripe.com/quotes/overview#quote_pdf)
        """
        ...

    @class_method_variant("_cls_pdf_async")
    async def pdf_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Quote.PdfParams"]
    ) -> Any:
        """
        Download the PDF for a finalized quote. Explanation for special handling can be found [here](https://docs.corp.stripe.com/quotes/overview#quote_pdf)
        """
        return cast(
            Any,
            await self._request_stream_async(
                "get",
                "/v1/quotes/{quote}/pdf".format(
                    quote=sanitize_id(self.get("id"))
                ),
                params=params,
                base_address="files",
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Quote.RetrieveParams"]
    ) -> "Quote":
        """
        Retrieves the quote with the given ID.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Quote.RetrieveParams"]
    ) -> "Quote":
        """
        Retrieves the quote with the given ID.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {
        "automatic_tax": AutomaticTax,
        "computed": Computed,
        "from_quote": FromQuote,
        "invoice_settings": InvoiceSettings,
        "status_transitions": StatusTransitions,
        "subscription_data": SubscriptionData,
        "total_details": TotalDetails,
        "transfer_data": TransferData,
    }
