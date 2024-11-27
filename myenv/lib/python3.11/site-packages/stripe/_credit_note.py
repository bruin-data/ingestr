# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._nested_resource_class_methods import nested_resource_class_methods
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, Dict, List, Optional, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._credit_note_line_item import CreditNoteLineItem
    from stripe._customer import Customer
    from stripe._customer_balance_transaction import CustomerBalanceTransaction
    from stripe._discount import Discount
    from stripe._invoice import Invoice
    from stripe._refund import Refund
    from stripe._shipping_rate import ShippingRate
    from stripe._tax_rate import TaxRate


@nested_resource_class_methods("line")
class CreditNote(
    CreateableAPIResource["CreditNote"],
    ListableAPIResource["CreditNote"],
    UpdateableAPIResource["CreditNote"],
):
    """
    Issue a credit note to adjust an invoice's amount after the invoice is finalized.

    Related guide: [Credit notes](https://stripe.com/docs/billing/invoices/credit-notes)
    """

    OBJECT_NAME: ClassVar[Literal["credit_note"]] = "credit_note"

    class DiscountAmount(StripeObject):
        amount: int
        """
        The amount, in cents (or local equivalent), of the discount.
        """
        discount: ExpandableField["Discount"]
        """
        The discount that was applied to get this discount amount.
        """

    class ShippingCost(StripeObject):
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

        amount_subtotal: int
        """
        Total shipping cost before any taxes are applied.
        """
        amount_tax: int
        """
        Total tax amount applied due to shipping costs. If no tax was applied, defaults to 0.
        """
        amount_total: int
        """
        Total shipping cost after taxes are applied.
        """
        shipping_rate: Optional[ExpandableField["ShippingRate"]]
        """
        The ID of the ShippingRate for this invoice.
        """
        taxes: Optional[List[Tax]]
        """
        The taxes applied to the shipping rate.
        """
        _inner_class_types = {"taxes": Tax}

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

    class CreateParams(RequestOptions):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the total amount of the credit note.
        """
        credit_amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the amount to credit the customer's balance, which will be automatically applied to their next invoice.
        """
        effective_at: NotRequired[int]
        """
        The date when this credit note is in effect. Same as `created` unless overwritten. When defined, this value replaces the system-generated 'Date of issue' printed on the credit note PDF.
        """
        email_type: NotRequired[Literal["credit_note", "none"]]
        """
        Type of email to send to the customer, one of `credit_note` or `none` and the default is `credit_note`.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice: str
        """
        ID of the invoice.
        """
        lines: NotRequired[List["CreditNote.CreateParamsLine"]]
        """
        Line items that make up the credit note.
        """
        memo: NotRequired[str]
        """
        The credit note's memo appears on the credit note PDF.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        out_of_band_amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the amount that is credited outside of Stripe.
        """
        reason: NotRequired[
            Literal[
                "duplicate",
                "fraudulent",
                "order_change",
                "product_unsatisfactory",
            ]
        ]
        """
        Reason for issuing this credit note, one of `duplicate`, `fraudulent`, `order_change`, or `product_unsatisfactory`
        """
        refund: NotRequired[str]
        """
        ID of an existing refund to link this credit note to.
        """
        refund_amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the amount to refund. If set, a refund will be created for the charge associated with the invoice.
        """
        shipping_cost: NotRequired["CreditNote.CreateParamsShippingCost"]
        """
        When shipping_cost contains the shipping_rate from the invoice, the shipping_cost is included in the credit note.
        """

    class CreateParamsLine(TypedDict):
        amount: NotRequired[int]
        """
        The line item amount to credit. Only valid when `type` is `invoice_line_item`.
        """
        description: NotRequired[str]
        """
        The description of the credit note line item. Only valid when the `type` is `custom_line_item`.
        """
        invoice_line_item: NotRequired[str]
        """
        The invoice line item to credit. Only valid when the `type` is `invoice_line_item`.
        """
        quantity: NotRequired[int]
        """
        The line item quantity to credit.
        """
        tax_amounts: NotRequired[
            "Literal['']|List[CreditNote.CreateParamsLineTaxAmount]"
        ]
        """
        A list of up to 10 tax amounts for the credit note line item. Cannot be mixed with `tax_rates`.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the credit note line item. Only valid when the `type` is `custom_line_item` and cannot be mixed with `tax_amounts`.
        """
        type: Literal["custom_line_item", "invoice_line_item"]
        """
        Type of the credit note line item, one of `invoice_line_item` or `custom_line_item`
        """
        unit_amount: NotRequired[int]
        """
        The integer unit amount in cents (or local equivalent) of the credit note line item. This `unit_amount` will be multiplied by the quantity to get the full amount to credit for this line item. Only valid when `type` is `custom_line_item`.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreateParamsLineTaxAmount(TypedDict):
        amount: int
        """
        The amount, in cents (or local equivalent), of the tax.
        """
        tax_rate: str
        """
        The id of the tax rate for this tax amount. The tax rate must have been automatically created by Stripe.
        """
        taxable_amount: int
        """
        The amount on which tax is calculated, in cents (or local equivalent).
        """

    class CreateParamsShippingCost(TypedDict):
        shipping_rate: NotRequired[str]
        """
        The ID of the shipping rate to use for this order.
        """

    class ListLinesParams(RequestOptions):
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
        created: NotRequired["CreditNote.ListParamsCreated|int"]
        """
        Only return credit notes that were created during the given date interval.
        """
        customer: NotRequired[str]
        """
        Only return credit notes for the customer specified by this customer ID.
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
        Only return credit notes for the invoice specified by this invoice ID.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
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

    class ModifyParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        memo: NotRequired[str]
        """
        Credit note memo.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class PreviewLinesParams(RequestOptions):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the total amount of the credit note.
        """
        credit_amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the amount to credit the customer's balance, which will be automatically applied to their next invoice.
        """
        effective_at: NotRequired[int]
        """
        The date when this credit note is in effect. Same as `created` unless overwritten. When defined, this value replaces the system-generated 'Date of issue' printed on the credit note PDF.
        """
        email_type: NotRequired[Literal["credit_note", "none"]]
        """
        Type of email to send to the customer, one of `credit_note` or `none` and the default is `credit_note`.
        """
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice: str
        """
        ID of the invoice.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        lines: NotRequired[List["CreditNote.PreviewLinesParamsLine"]]
        """
        Line items that make up the credit note.
        """
        memo: NotRequired[str]
        """
        The credit note's memo appears on the credit note PDF.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        out_of_band_amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the amount that is credited outside of Stripe.
        """
        reason: NotRequired[
            Literal[
                "duplicate",
                "fraudulent",
                "order_change",
                "product_unsatisfactory",
            ]
        ]
        """
        Reason for issuing this credit note, one of `duplicate`, `fraudulent`, `order_change`, or `product_unsatisfactory`
        """
        refund: NotRequired[str]
        """
        ID of an existing refund to link this credit note to.
        """
        refund_amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the amount to refund. If set, a refund will be created for the charge associated with the invoice.
        """
        shipping_cost: NotRequired["CreditNote.PreviewLinesParamsShippingCost"]
        """
        When shipping_cost contains the shipping_rate from the invoice, the shipping_cost is included in the credit note.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """

    class PreviewLinesParamsLine(TypedDict):
        amount: NotRequired[int]
        """
        The line item amount to credit. Only valid when `type` is `invoice_line_item`.
        """
        description: NotRequired[str]
        """
        The description of the credit note line item. Only valid when the `type` is `custom_line_item`.
        """
        invoice_line_item: NotRequired[str]
        """
        The invoice line item to credit. Only valid when the `type` is `invoice_line_item`.
        """
        quantity: NotRequired[int]
        """
        The line item quantity to credit.
        """
        tax_amounts: NotRequired[
            "Literal['']|List[CreditNote.PreviewLinesParamsLineTaxAmount]"
        ]
        """
        A list of up to 10 tax amounts for the credit note line item. Cannot be mixed with `tax_rates`.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the credit note line item. Only valid when the `type` is `custom_line_item` and cannot be mixed with `tax_amounts`.
        """
        type: Literal["custom_line_item", "invoice_line_item"]
        """
        Type of the credit note line item, one of `invoice_line_item` or `custom_line_item`
        """
        unit_amount: NotRequired[int]
        """
        The integer unit amount in cents (or local equivalent) of the credit note line item. This `unit_amount` will be multiplied by the quantity to get the full amount to credit for this line item. Only valid when `type` is `custom_line_item`.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class PreviewLinesParamsLineTaxAmount(TypedDict):
        amount: int
        """
        The amount, in cents (or local equivalent), of the tax.
        """
        tax_rate: str
        """
        The id of the tax rate for this tax amount. The tax rate must have been automatically created by Stripe.
        """
        taxable_amount: int
        """
        The amount on which tax is calculated, in cents (or local equivalent).
        """

    class PreviewLinesParamsShippingCost(TypedDict):
        shipping_rate: NotRequired[str]
        """
        The ID of the shipping rate to use for this order.
        """

    class PreviewParams(RequestOptions):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the total amount of the credit note.
        """
        credit_amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the amount to credit the customer's balance, which will be automatically applied to their next invoice.
        """
        effective_at: NotRequired[int]
        """
        The date when this credit note is in effect. Same as `created` unless overwritten. When defined, this value replaces the system-generated 'Date of issue' printed on the credit note PDF.
        """
        email_type: NotRequired[Literal["credit_note", "none"]]
        """
        Type of email to send to the customer, one of `credit_note` or `none` and the default is `credit_note`.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice: str
        """
        ID of the invoice.
        """
        lines: NotRequired[List["CreditNote.PreviewParamsLine"]]
        """
        Line items that make up the credit note.
        """
        memo: NotRequired[str]
        """
        The credit note's memo appears on the credit note PDF.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        out_of_band_amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the amount that is credited outside of Stripe.
        """
        reason: NotRequired[
            Literal[
                "duplicate",
                "fraudulent",
                "order_change",
                "product_unsatisfactory",
            ]
        ]
        """
        Reason for issuing this credit note, one of `duplicate`, `fraudulent`, `order_change`, or `product_unsatisfactory`
        """
        refund: NotRequired[str]
        """
        ID of an existing refund to link this credit note to.
        """
        refund_amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) representing the amount to refund. If set, a refund will be created for the charge associated with the invoice.
        """
        shipping_cost: NotRequired["CreditNote.PreviewParamsShippingCost"]
        """
        When shipping_cost contains the shipping_rate from the invoice, the shipping_cost is included in the credit note.
        """

    class PreviewParamsLine(TypedDict):
        amount: NotRequired[int]
        """
        The line item amount to credit. Only valid when `type` is `invoice_line_item`.
        """
        description: NotRequired[str]
        """
        The description of the credit note line item. Only valid when the `type` is `custom_line_item`.
        """
        invoice_line_item: NotRequired[str]
        """
        The invoice line item to credit. Only valid when the `type` is `invoice_line_item`.
        """
        quantity: NotRequired[int]
        """
        The line item quantity to credit.
        """
        tax_amounts: NotRequired[
            "Literal['']|List[CreditNote.PreviewParamsLineTaxAmount]"
        ]
        """
        A list of up to 10 tax amounts for the credit note line item. Cannot be mixed with `tax_rates`.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the credit note line item. Only valid when the `type` is `custom_line_item` and cannot be mixed with `tax_amounts`.
        """
        type: Literal["custom_line_item", "invoice_line_item"]
        """
        Type of the credit note line item, one of `invoice_line_item` or `custom_line_item`
        """
        unit_amount: NotRequired[int]
        """
        The integer unit amount in cents (or local equivalent) of the credit note line item. This `unit_amount` will be multiplied by the quantity to get the full amount to credit for this line item. Only valid when `type` is `custom_line_item`.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class PreviewParamsLineTaxAmount(TypedDict):
        amount: int
        """
        The amount, in cents (or local equivalent), of the tax.
        """
        tax_rate: str
        """
        The id of the tax rate for this tax amount. The tax rate must have been automatically created by Stripe.
        """
        taxable_amount: int
        """
        The amount on which tax is calculated, in cents (or local equivalent).
        """

    class PreviewParamsShippingCost(TypedDict):
        shipping_rate: NotRequired[str]
        """
        The ID of the shipping rate to use for this order.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class VoidCreditNoteParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    amount: int
    """
    The integer amount in cents (or local equivalent) representing the total amount of the credit note, including tax.
    """
    amount_shipping: int
    """
    This is the sum of all the shipping amounts.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    customer: ExpandableField["Customer"]
    """
    ID of the customer.
    """
    customer_balance_transaction: Optional[
        ExpandableField["CustomerBalanceTransaction"]
    ]
    """
    Customer balance transaction related to this credit note.
    """
    discount_amount: int
    """
    The integer amount in cents (or local equivalent) representing the total amount of discount that was credited.
    """
    discount_amounts: List[DiscountAmount]
    """
    The aggregate amounts calculated per discount for all line items.
    """
    effective_at: Optional[int]
    """
    The date when this credit note is in effect. Same as `created` unless overwritten. When defined, this value replaces the system-generated 'Date of issue' printed on the credit note PDF.
    """
    id: str
    """
    Unique identifier for the object.
    """
    invoice: ExpandableField["Invoice"]
    """
    ID of the invoice.
    """
    lines: ListObject["CreditNoteLineItem"]
    """
    Line items that make up the credit note
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    memo: Optional[str]
    """
    Customer-facing text that appears on the credit note PDF.
    """
    metadata: Optional[Dict[str, str]]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    number: str
    """
    A unique number that identifies this particular credit note and appears on the PDF of the credit note and its associated invoice.
    """
    object: Literal["credit_note"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    out_of_band_amount: Optional[int]
    """
    Amount that was credited outside of Stripe.
    """
    pdf: str
    """
    The link to download the PDF of the credit note.
    """
    reason: Optional[
        Literal[
            "duplicate", "fraudulent", "order_change", "product_unsatisfactory"
        ]
    ]
    """
    Reason for issuing this credit note, one of `duplicate`, `fraudulent`, `order_change`, or `product_unsatisfactory`
    """
    refund: Optional[ExpandableField["Refund"]]
    """
    Refund related to this credit note.
    """
    shipping_cost: Optional[ShippingCost]
    """
    The details of the cost of shipping, including the ShippingRate applied to the invoice.
    """
    status: Literal["issued", "void"]
    """
    Status of this credit note, one of `issued` or `void`. Learn more about [voiding credit notes](https://stripe.com/docs/billing/invoices/credit-notes#voiding).
    """
    subtotal: int
    """
    The integer amount in cents (or local equivalent) representing the amount of the credit note, excluding exclusive tax and invoice level discounts.
    """
    subtotal_excluding_tax: Optional[int]
    """
    The integer amount in cents (or local equivalent) representing the amount of the credit note, excluding all tax and invoice level discounts.
    """
    tax_amounts: List[TaxAmount]
    """
    The aggregate amounts calculated per tax rate for all line items.
    """
    total: int
    """
    The integer amount in cents (or local equivalent) representing the total amount of the credit note, including tax and all discount.
    """
    total_excluding_tax: Optional[int]
    """
    The integer amount in cents (or local equivalent) representing the total amount of the credit note, excluding tax, but including discounts.
    """
    type: Literal["post_payment", "pre_payment"]
    """
    Type of this credit note, one of `pre_payment` or `post_payment`. A `pre_payment` credit note means it was issued when the invoice was open. A `post_payment` credit note means it was issued when the invoice was paid.
    """
    voided_at: Optional[int]
    """
    The time that the credit note was voided.
    """

    @classmethod
    def create(
        cls, **params: Unpack["CreditNote.CreateParams"]
    ) -> "CreditNote":
        """
        Issue a credit note to adjust the amount of a finalized invoice. For a status=open invoice, a credit note reduces
        its amount_due. For a status=paid invoice, a credit note does not affect its amount_due. Instead, it can result
        in any combination of the following:


        Refund: create a new refund (using refund_amount) or link an existing refund (using refund).
        Customer balance credit: credit the customer's balance (using credit_amount) which will be automatically applied to their next invoice when it's finalized.
        Outside of Stripe credit: record the amount that is or will be credited outside of Stripe (using out_of_band_amount).


        For post-payment credit notes the sum of the refund, credit and outside of Stripe amounts must equal the credit note total.

        You may issue multiple credit notes for an invoice. Each credit note will increment the invoice's pre_payment_credit_notes_amount
        or post_payment_credit_notes_amount depending on its status at the time of credit note creation.
        """
        return cast(
            "CreditNote",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["CreditNote.CreateParams"]
    ) -> "CreditNote":
        """
        Issue a credit note to adjust the amount of a finalized invoice. For a status=open invoice, a credit note reduces
        its amount_due. For a status=paid invoice, a credit note does not affect its amount_due. Instead, it can result
        in any combination of the following:


        Refund: create a new refund (using refund_amount) or link an existing refund (using refund).
        Customer balance credit: credit the customer's balance (using credit_amount) which will be automatically applied to their next invoice when it's finalized.
        Outside of Stripe credit: record the amount that is or will be credited outside of Stripe (using out_of_band_amount).


        For post-payment credit notes the sum of the refund, credit and outside of Stripe amounts must equal the credit note total.

        You may issue multiple credit notes for an invoice. Each credit note will increment the invoice's pre_payment_credit_notes_amount
        or post_payment_credit_notes_amount depending on its status at the time of credit note creation.
        """
        return cast(
            "CreditNote",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["CreditNote.ListParams"]
    ) -> ListObject["CreditNote"]:
        """
        Returns a list of credit notes.
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
        cls, **params: Unpack["CreditNote.ListParams"]
    ) -> ListObject["CreditNote"]:
        """
        Returns a list of credit notes.
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
    def modify(
        cls, id: str, **params: Unpack["CreditNote.ModifyParams"]
    ) -> "CreditNote":
        """
        Updates an existing credit note.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "CreditNote",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["CreditNote.ModifyParams"]
    ) -> "CreditNote":
        """
        Updates an existing credit note.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "CreditNote",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def preview(
        cls, **params: Unpack["CreditNote.PreviewParams"]
    ) -> "CreditNote":
        """
        Get a preview of a credit note without creating it.
        """
        return cast(
            "CreditNote",
            cls._static_request(
                "get",
                "/v1/credit_notes/preview",
                params=params,
            ),
        )

    @classmethod
    async def preview_async(
        cls, **params: Unpack["CreditNote.PreviewParams"]
    ) -> "CreditNote":
        """
        Get a preview of a credit note without creating it.
        """
        return cast(
            "CreditNote",
            await cls._static_request_async(
                "get",
                "/v1/credit_notes/preview",
                params=params,
            ),
        )

    @classmethod
    def preview_lines(
        cls, **params: Unpack["CreditNote.PreviewLinesParams"]
    ) -> ListObject["CreditNoteLineItem"]:
        """
        When retrieving a credit note preview, you'll get a lines property containing the first handful of those items. This URL you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["CreditNoteLineItem"],
            cls._static_request(
                "get",
                "/v1/credit_notes/preview/lines",
                params=params,
            ),
        )

    @classmethod
    async def preview_lines_async(
        cls, **params: Unpack["CreditNote.PreviewLinesParams"]
    ) -> ListObject["CreditNoteLineItem"]:
        """
        When retrieving a credit note preview, you'll get a lines property containing the first handful of those items. This URL you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["CreditNoteLineItem"],
            await cls._static_request_async(
                "get",
                "/v1/credit_notes/preview/lines",
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["CreditNote.RetrieveParams"]
    ) -> "CreditNote":
        """
        Retrieves the credit note object with the given identifier.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["CreditNote.RetrieveParams"]
    ) -> "CreditNote":
        """
        Retrieves the credit note object with the given identifier.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def _cls_void_credit_note(
        cls, id: str, **params: Unpack["CreditNote.VoidCreditNoteParams"]
    ) -> "CreditNote":
        """
        Marks a credit note as void. Learn more about [voiding credit notes](https://stripe.com/docs/billing/invoices/credit-notes#voiding).
        """
        return cast(
            "CreditNote",
            cls._static_request(
                "post",
                "/v1/credit_notes/{id}/void".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def void_credit_note(
        id: str, **params: Unpack["CreditNote.VoidCreditNoteParams"]
    ) -> "CreditNote":
        """
        Marks a credit note as void. Learn more about [voiding credit notes](https://stripe.com/docs/billing/invoices/credit-notes#voiding).
        """
        ...

    @overload
    def void_credit_note(
        self, **params: Unpack["CreditNote.VoidCreditNoteParams"]
    ) -> "CreditNote":
        """
        Marks a credit note as void. Learn more about [voiding credit notes](https://stripe.com/docs/billing/invoices/credit-notes#voiding).
        """
        ...

    @class_method_variant("_cls_void_credit_note")
    def void_credit_note(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["CreditNote.VoidCreditNoteParams"]
    ) -> "CreditNote":
        """
        Marks a credit note as void. Learn more about [voiding credit notes](https://stripe.com/docs/billing/invoices/credit-notes#voiding).
        """
        return cast(
            "CreditNote",
            self._request(
                "post",
                "/v1/credit_notes/{id}/void".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_void_credit_note_async(
        cls, id: str, **params: Unpack["CreditNote.VoidCreditNoteParams"]
    ) -> "CreditNote":
        """
        Marks a credit note as void. Learn more about [voiding credit notes](https://stripe.com/docs/billing/invoices/credit-notes#voiding).
        """
        return cast(
            "CreditNote",
            await cls._static_request_async(
                "post",
                "/v1/credit_notes/{id}/void".format(id=sanitize_id(id)),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def void_credit_note_async(
        id: str, **params: Unpack["CreditNote.VoidCreditNoteParams"]
    ) -> "CreditNote":
        """
        Marks a credit note as void. Learn more about [voiding credit notes](https://stripe.com/docs/billing/invoices/credit-notes#voiding).
        """
        ...

    @overload
    async def void_credit_note_async(
        self, **params: Unpack["CreditNote.VoidCreditNoteParams"]
    ) -> "CreditNote":
        """
        Marks a credit note as void. Learn more about [voiding credit notes](https://stripe.com/docs/billing/invoices/credit-notes#voiding).
        """
        ...

    @class_method_variant("_cls_void_credit_note_async")
    async def void_credit_note_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["CreditNote.VoidCreditNoteParams"]
    ) -> "CreditNote":
        """
        Marks a credit note as void. Learn more about [voiding credit notes](https://stripe.com/docs/billing/invoices/credit-notes#voiding).
        """
        return cast(
            "CreditNote",
            await self._request_async(
                "post",
                "/v1/credit_notes/{id}/void".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def list_lines(
        cls, credit_note: str, **params: Unpack["CreditNote.ListLinesParams"]
    ) -> ListObject["CreditNoteLineItem"]:
        """
        When retrieving a credit note, you'll get a lines property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["CreditNoteLineItem"],
            cls._static_request(
                "get",
                "/v1/credit_notes/{credit_note}/lines".format(
                    credit_note=sanitize_id(credit_note)
                ),
                params=params,
            ),
        )

    @classmethod
    async def list_lines_async(
        cls, credit_note: str, **params: Unpack["CreditNote.ListLinesParams"]
    ) -> ListObject["CreditNoteLineItem"]:
        """
        When retrieving a credit note, you'll get a lines property containing the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["CreditNoteLineItem"],
            await cls._static_request_async(
                "get",
                "/v1/credit_notes/{credit_note}/lines".format(
                    credit_note=sanitize_id(credit_note)
                ),
                params=params,
            ),
        )

    _inner_class_types = {
        "discount_amounts": DiscountAmount,
        "shipping_cost": ShippingCost,
        "tax_amounts": TaxAmount,
    }
