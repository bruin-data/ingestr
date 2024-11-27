# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._expandable_field import ExpandableField
from stripe._stripe_object import StripeObject
from typing import ClassVar, List, Optional
from typing_extensions import Literal, TYPE_CHECKING

if TYPE_CHECKING:
    from stripe._discount import Discount
    from stripe._tax_rate import TaxRate


class CreditNoteLineItem(StripeObject):
    """
    The credit note line item object
    """

    OBJECT_NAME: ClassVar[Literal["credit_note_line_item"]] = (
        "credit_note_line_item"
    )

    class DiscountAmount(StripeObject):
        amount: int
        """
        The amount, in cents (or local equivalent), of the discount.
        """
        discount: ExpandableField["Discount"]
        """
        The discount that was applied to get this discount amount.
        """

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

    amount: int
    """
    The integer amount in cents (or local equivalent) representing the gross amount being credited for this line item, excluding (exclusive) tax and discounts.
    """
    amount_excluding_tax: Optional[int]
    """
    The integer amount in cents (or local equivalent) representing the amount being credited for this line item, excluding all tax and discounts.
    """
    description: Optional[str]
    """
    Description of the item being credited.
    """
    discount_amount: int
    """
    The integer amount in cents (or local equivalent) representing the discount being credited for this line item.
    """
    discount_amounts: List[DiscountAmount]
    """
    The amount of discount calculated per discount for this line item
    """
    id: str
    """
    Unique identifier for the object.
    """
    invoice_line_item: Optional[str]
    """
    ID of the invoice line item being credited
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["credit_note_line_item"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    quantity: Optional[int]
    """
    The number of units of product being credited.
    """
    tax_amounts: List[TaxAmount]
    """
    The amount of tax calculated per tax rate for this line item
    """
    tax_rates: List["TaxRate"]
    """
    The tax rates which apply to the line item.
    """
    type: Literal["custom_line_item", "invoice_line_item"]
    """
    The type of the credit note line item, one of `invoice_line_item` or `custom_line_item`. When the type is `invoice_line_item` there is an additional `invoice_line_item` property on the resource the value of which is the id of the credited line item on the invoice.
    """
    unit_amount: Optional[int]
    """
    The cost of each unit of product being credited.
    """
    unit_amount_decimal: Optional[str]
    """
    Same as `unit_amount`, but contains a decimal value with at most 12 decimal places.
    """
    unit_amount_excluding_tax: Optional[str]
    """
    The amount in cents (or local equivalent) representing the unit amount being credited for this line item, excluding all tax and discounts.
    """
    _inner_class_types = {
        "discount_amounts": DiscountAmount,
        "tax_amounts": TaxAmount,
    }
