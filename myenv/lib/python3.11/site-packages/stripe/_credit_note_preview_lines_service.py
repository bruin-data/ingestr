# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._credit_note_line_item import CreditNoteLineItem
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CreditNotePreviewLinesService(StripeService):
    class ListParams(TypedDict):
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
        lines: NotRequired[
            List["CreditNotePreviewLinesService.ListParamsLine"]
        ]
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
        shipping_cost: NotRequired[
            "CreditNotePreviewLinesService.ListParamsShippingCost"
        ]
        """
        When shipping_cost contains the shipping_rate from the invoice, the shipping_cost is included in the credit note.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """

    class ListParamsLine(TypedDict):
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
            "Literal['']|List[CreditNotePreviewLinesService.ListParamsLineTaxAmount]"
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

    class ListParamsLineTaxAmount(TypedDict):
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

    class ListParamsShippingCost(TypedDict):
        shipping_rate: NotRequired[str]
        """
        The ID of the shipping rate to use for this order.
        """

    def list(
        self,
        params: "CreditNotePreviewLinesService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[CreditNoteLineItem]:
        """
        When retrieving a credit note preview, you'll get a lines property containing the first handful of those items. This URL you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject[CreditNoteLineItem],
            self._request(
                "get",
                "/v1/credit_notes/preview/lines",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "CreditNotePreviewLinesService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[CreditNoteLineItem]:
        """
        When retrieving a credit note preview, you'll get a lines property containing the first handful of those items. This URL you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject[CreditNoteLineItem],
            await self._request_async(
                "get",
                "/v1/credit_notes/preview/lines",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
