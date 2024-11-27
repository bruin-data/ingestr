# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._credit_note import CreditNote
from stripe._credit_note_line_item_service import CreditNoteLineItemService
from stripe._credit_note_preview_lines_service import (
    CreditNotePreviewLinesService,
)
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CreditNoteService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.line_items = CreditNoteLineItemService(self._requestor)
        self.preview_lines = CreditNotePreviewLinesService(self._requestor)

    class CreateParams(TypedDict):
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
        lines: NotRequired[List["CreditNoteService.CreateParamsLine"]]
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
            "CreditNoteService.CreateParamsShippingCost"
        ]
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
            "Literal['']|List[CreditNoteService.CreateParamsLineTaxAmount]"
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

    class ListParams(TypedDict):
        created: NotRequired["CreditNoteService.ListParamsCreated|int"]
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

    class PreviewParams(TypedDict):
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
        lines: NotRequired[List["CreditNoteService.PreviewParamsLine"]]
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
            "CreditNoteService.PreviewParamsShippingCost"
        ]
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
            "Literal['']|List[CreditNoteService.PreviewParamsLineTaxAmount]"
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

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
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

    class VoidCreditNoteParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "CreditNoteService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[CreditNote]:
        """
        Returns a list of credit notes.
        """
        return cast(
            ListObject[CreditNote],
            self._request(
                "get",
                "/v1/credit_notes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "CreditNoteService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[CreditNote]:
        """
        Returns a list of credit notes.
        """
        return cast(
            ListObject[CreditNote],
            await self._request_async(
                "get",
                "/v1/credit_notes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "CreditNoteService.CreateParams",
        options: RequestOptions = {},
    ) -> CreditNote:
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
            CreditNote,
            self._request(
                "post",
                "/v1/credit_notes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "CreditNoteService.CreateParams",
        options: RequestOptions = {},
    ) -> CreditNote:
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
            CreditNote,
            await self._request_async(
                "post",
                "/v1/credit_notes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "CreditNoteService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> CreditNote:
        """
        Retrieves the credit note object with the given identifier.
        """
        return cast(
            CreditNote,
            self._request(
                "get",
                "/v1/credit_notes/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "CreditNoteService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> CreditNote:
        """
        Retrieves the credit note object with the given identifier.
        """
        return cast(
            CreditNote,
            await self._request_async(
                "get",
                "/v1/credit_notes/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        id: str,
        params: "CreditNoteService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> CreditNote:
        """
        Updates an existing credit note.
        """
        return cast(
            CreditNote,
            self._request(
                "post",
                "/v1/credit_notes/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        id: str,
        params: "CreditNoteService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> CreditNote:
        """
        Updates an existing credit note.
        """
        return cast(
            CreditNote,
            await self._request_async(
                "post",
                "/v1/credit_notes/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def preview(
        self,
        params: "CreditNoteService.PreviewParams",
        options: RequestOptions = {},
    ) -> CreditNote:
        """
        Get a preview of a credit note without creating it.
        """
        return cast(
            CreditNote,
            self._request(
                "get",
                "/v1/credit_notes/preview",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def preview_async(
        self,
        params: "CreditNoteService.PreviewParams",
        options: RequestOptions = {},
    ) -> CreditNote:
        """
        Get a preview of a credit note without creating it.
        """
        return cast(
            CreditNote,
            await self._request_async(
                "get",
                "/v1/credit_notes/preview",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def void_credit_note(
        self,
        id: str,
        params: "CreditNoteService.VoidCreditNoteParams" = {},
        options: RequestOptions = {},
    ) -> CreditNote:
        """
        Marks a credit note as void. Learn more about [voiding credit notes](https://stripe.com/docs/billing/invoices/credit-notes#voiding).
        """
        return cast(
            CreditNote,
            self._request(
                "post",
                "/v1/credit_notes/{id}/void".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def void_credit_note_async(
        self,
        id: str,
        params: "CreditNoteService.VoidCreditNoteParams" = {},
        options: RequestOptions = {},
    ) -> CreditNote:
        """
        Marks a credit note as void. Learn more about [voiding credit notes](https://stripe.com/docs/billing/invoices/credit-notes#voiding).
        """
        return cast(
            CreditNote,
            await self._request_async(
                "post",
                "/v1/credit_notes/{id}/void".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
