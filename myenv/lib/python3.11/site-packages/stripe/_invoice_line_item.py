# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._expandable_field import ExpandableField
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import sanitize_id
from typing import ClassVar, Dict, List, Optional, cast
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._discount import Discount
    from stripe._invoice_item import InvoiceItem
    from stripe._plan import Plan
    from stripe._price import Price
    from stripe._subscription import Subscription
    from stripe._subscription_item import SubscriptionItem
    from stripe._tax_rate import TaxRate


class InvoiceLineItem(UpdateableAPIResource["InvoiceLineItem"]):
    OBJECT_NAME: ClassVar[Literal["line_item"]] = "line_item"

    class DiscountAmount(StripeObject):
        amount: int
        """
        The amount, in cents (or local equivalent), of the discount.
        """
        discount: ExpandableField["Discount"]
        """
        The discount that was applied to get this discount amount.
        """

    class Period(StripeObject):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class ProrationDetails(StripeObject):
        class CreditedItems(StripeObject):
            invoice: str
            """
            Invoice containing the credited invoice line items
            """
            invoice_line_items: List[str]
            """
            Credited invoice line items
            """

        credited_items: Optional[CreditedItems]
        """
        For a credit proration `line_item`, the original debit line_items to which the credit proration applies.
        """
        _inner_class_types = {"credited_items": CreditedItems}

    class TaxAmount(StripeObject):
        amount: int
        """
        The amount, in cents (or local equivalent), of the tax.
        """
        inclusive: bool
        """
        Whether this tax amount is inclusive or exclusive.
        """
        tax_rate: ExpandableField["TaxRate"]
        """
        The tax rate that was applied to get this tax amount.
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

    class ModifyParams(RequestOptions):
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
        Controls whether discounts apply to this line item. Defaults to false for prorations or negative line items, and true for all other line items. Cannot be set to true for prorations.
        """
        discounts: NotRequired[
            "Literal['']|List[InvoiceLineItem.ModifyParamsDiscount]"
        ]
        """
        The coupons, promotion codes & existing discounts which apply to the line item. Item discounts are applied before invoice discounts. Pass an empty string to remove previously-defined discounts.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`. For [type=subscription](https://stripe.com/docs/api/invoices/line_item#invoice_line_item_object-type) line items, the incoming metadata specified on the request is directly used to set this value, in contrast to [type=invoiceitem](api/invoices/line_item#invoice_line_item_object-type) line items, where any existing metadata on the invoice line is merged with the incoming data.
        """
        period: NotRequired["InvoiceLineItem.ModifyParamsPeriod"]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired["InvoiceLineItem.ModifyParamsPriceData"]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the line item.
        """
        tax_amounts: NotRequired[
            "Literal['']|List[InvoiceLineItem.ModifyParamsTaxAmount]"
        ]
        """
        A list of up to 10 tax amounts for this line item. This can be useful if you calculate taxes on your own or use a third-party to calculate them. You cannot set tax amounts if any line item has [tax_rates](https://stripe.com/docs/api/invoices/line_item#invoice_line_item_object-tax_rates) or if the invoice has [default_tax_rates](https://stripe.com/docs/api/invoices/object#invoice_object-default_tax_rates) or uses [automatic tax](https://stripe.com/docs/tax/invoicing). Pass an empty string to remove previously defined tax amounts.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the line item. When set, the `default_tax_rates` on the invoice do not apply to this line item. Pass an empty string to remove previously-defined tax rates.
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

    class ModifyParamsPeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class ModifyParamsPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: NotRequired[str]
        """
        The ID of the product that this price will belong to. One of `product` or `product_data` is required.
        """
        product_data: NotRequired[
            "InvoiceLineItem.ModifyParamsPriceDataProductData"
        ]
        """
        Data used to generate a new product object inline. One of `product` or `product_data` is required.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A non-negative integer in cents (or local equivalent) representing how much to charge. One of `unit_amount` or `unit_amount_decimal` is required.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class ModifyParamsPriceDataProductData(TypedDict):
        description: NotRequired[str]
        """
        The product's description, meant to be displayable to the customer. Use this field to optionally store a long form explanation of the product being sold for your own rendering purposes.
        """
        images: NotRequired[List[str]]
        """
        A list of up to 8 URLs of images for this product, meant to be displayable to the customer.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: str
        """
        The product's name, meant to be displayable to the customer.
        """
        tax_code: NotRequired[str]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """

    class ModifyParamsTaxAmount(TypedDict):
        amount: int
        """
        The amount, in cents (or local equivalent), of the tax.
        """
        tax_rate_data: "InvoiceLineItem.ModifyParamsTaxAmountTaxRateData"
        """
        Data to find or create a TaxRate object.

        Stripe automatically creates or reuses a TaxRate object for each tax amount. If the `tax_rate_data` exactly matches a previous value, Stripe will reuse the TaxRate object. TaxRate objects created automatically by Stripe are immediately archived, do not appear in the line item's `tax_rates`, and cannot be directly added to invoices, payments, or line items.
        """
        taxable_amount: int
        """
        The amount on which tax is calculated, in cents (or local equivalent).
        """

    class ModifyParamsTaxAmountTaxRateData(TypedDict):
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the tax rate for your internal use only. It will not be visible to your customers.
        """
        display_name: str
        """
        The display name of the tax rate, which will be shown to users.
        """
        inclusive: bool
        """
        This specifies if the tax rate is inclusive or exclusive.
        """
        jurisdiction: NotRequired[str]
        """
        The jurisdiction for the tax rate. You can use this label field for tax reporting purposes. It also appears on your customer's invoice.
        """
        percentage: float
        """
        The statutory tax rate percent. This field accepts decimal values between 0 and 100 inclusive with at most 4 decimal places. To accommodate fixed-amount taxes, set the percentage to zero. Stripe will not display zero percentages on the invoice unless the `amount` of the tax is also zero.
        """
        state: NotRequired[str]
        """
        [ISO 3166-2 subdivision code](https://en.wikipedia.org/wiki/ISO_3166-2:US), without country prefix. For example, "NY" for New York, United States.
        """
        tax_type: NotRequired[
            Literal[
                "amusement_tax",
                "communications_tax",
                "gst",
                "hst",
                "igst",
                "jct",
                "lease_tax",
                "pst",
                "qst",
                "rst",
                "sales_tax",
                "vat",
            ]
        ]
        """
        The high-level tax type, such as `vat` or `sales_tax`.
        """

    amount: int
    """
    The amount, in cents (or local equivalent).
    """
    amount_excluding_tax: Optional[int]
    """
    The integer amount in cents (or local equivalent) representing the amount for this line item, excluding all tax and discounts.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    description: Optional[str]
    """
    An arbitrary string attached to the object. Often useful for displaying to users.
    """
    discount_amounts: Optional[List[DiscountAmount]]
    """
    The amount of discount calculated per discount for this line item.
    """
    discountable: bool
    """
    If true, discounts will apply to this line item. Always false for prorations.
    """
    discounts: List[ExpandableField["Discount"]]
    """
    The discounts applied to the invoice line item. Line item discounts are applied before invoice discounts. Use `expand[]=discounts` to expand each discount.
    """
    id: str
    """
    Unique identifier for the object.
    """
    invoice: Optional[str]
    """
    The ID of the invoice that contains this line item.
    """
    invoice_item: Optional[ExpandableField["InvoiceItem"]]
    """
    The ID of the [invoice item](https://stripe.com/docs/api/invoiceitems) associated with this line item if any.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Note that for line items with `type=subscription`, `metadata` reflects the current metadata from the subscription associated with the line item, unless the invoice line was directly updated with different metadata after creation.
    """
    object: Literal["line_item"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    period: Period
    plan: Optional["Plan"]
    """
    The plan of the subscription, if the line item is a subscription or a proration.
    """
    price: Optional["Price"]
    """
    The price of the line item.
    """
    proration: bool
    """
    Whether this is a proration.
    """
    proration_details: Optional[ProrationDetails]
    """
    Additional details for proration line items
    """
    quantity: Optional[int]
    """
    The quantity of the subscription, if the line item is a subscription or a proration.
    """
    subscription: Optional[ExpandableField["Subscription"]]
    """
    The subscription that the invoice item pertains to, if any.
    """
    subscription_item: Optional[ExpandableField["SubscriptionItem"]]
    """
    The subscription item that generated this line item. Left empty if the line item is not an explicit result of a subscription.
    """
    tax_amounts: Optional[List[TaxAmount]]
    """
    The amount of tax calculated per tax rate for this line item
    """
    tax_rates: Optional[List["TaxRate"]]
    """
    The tax rates which apply to the line item.
    """
    type: Literal["invoiceitem", "subscription"]
    """
    A string identifying the type of the source of this line item, either an `invoiceitem` or a `subscription`.
    """
    unit_amount_excluding_tax: Optional[str]
    """
    The amount in cents (or local equivalent) representing the unit amount for this line item, excluding all tax and discounts.
    """

    @classmethod
    def modify(
        cls, id: str, **params: Unpack["InvoiceLineItem.ModifyParams"]
    ) -> "InvoiceLineItem":
        """
        Updates an invoice's line item. Some fields, such as tax_amounts, only live on the invoice line item,
        so they can only be updated through this endpoint. Other fields, such as amount, live on both the invoice
        item and the invoice line item, so updates on this endpoint will propagate to the invoice item as well.
        Updating an invoice's line item is only possible before the invoice is finalized.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "InvoiceLineItem",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["InvoiceLineItem.ModifyParams"]
    ) -> "InvoiceLineItem":
        """
        Updates an invoice's line item. Some fields, such as tax_amounts, only live on the invoice line item,
        so they can only be updated through this endpoint. Other fields, such as amount, live on both the invoice
        item and the invoice line item, so updates on this endpoint will propagate to the invoice item as well.
        Updating an invoice's line item is only possible before the invoice is finalized.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "InvoiceLineItem",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    _inner_class_types = {
        "discount_amounts": DiscountAmount,
        "period": Period,
        "proration_details": ProrationDetails,
        "tax_amounts": TaxAmount,
    }
