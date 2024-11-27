# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.tax._transaction import Transaction
from stripe.tax._transaction_line_item_service import (
    TransactionLineItemService,
)
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class TransactionService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.line_items = TransactionLineItemService(self._requestor)

    class CreateFromCalculationParams(TypedDict):
        calculation: str
        """
        Tax Calculation ID to be used as input when creating the transaction.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        posted_at: NotRequired[int]
        """
        The Unix timestamp representing when the tax liability is assumed or reduced, which determines the liability posting period and handling in tax liability reports. The timestamp must fall within the `tax_date` and the current time, unless the `tax_date` is scheduled in advance. Defaults to the current time.
        """
        reference: str
        """
        A custom order or sale identifier, such as 'myOrder_123'. Must be unique across all transactions, including reversals.
        """

    class CreateReversalParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        flat_amount: NotRequired[int]
        """
        A flat amount to reverse across the entire transaction, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) in negative. This value represents the total amount to refund from the transaction, including taxes.
        """
        line_items: NotRequired[
            List["TransactionService.CreateReversalParamsLineItem"]
        ]
        """
        The line item amounts to reverse.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mode: Literal["full", "partial"]
        """
        If `partial`, the provided line item or shipping cost amounts are reversed. If `full`, the original transaction is fully reversed.
        """
        original_transaction: str
        """
        The ID of the Transaction to partially or fully reverse.
        """
        reference: str
        """
        A custom identifier for this reversal, such as `myOrder_123-refund_1`, which must be unique across all transactions. The reference helps identify this reversal transaction in exported [tax reports](https://stripe.com/docs/tax/reports).
        """
        shipping_cost: NotRequired[
            "TransactionService.CreateReversalParamsShippingCost"
        ]
        """
        The shipping cost to reverse.
        """

    class CreateReversalParamsLineItem(TypedDict):
        amount: int
        """
        The amount to reverse, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) in negative.
        """
        amount_tax: int
        """
        The amount of tax to reverse, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) in negative.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
        """
        original_line_item: str
        """
        The `id` of the line item to reverse in the original transaction.
        """
        quantity: NotRequired[int]
        """
        The quantity reversed. Appears in [tax exports](https://stripe.com/docs/tax/reports), but does not affect the amount of tax reversed.
        """
        reference: str
        """
        A custom identifier for this line item in the reversal transaction, such as 'L1-refund'.
        """

    class CreateReversalParamsShippingCost(TypedDict):
        amount: int
        """
        The amount to reverse, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) in negative.
        """
        amount_tax: int
        """
        The amount of tax to reverse, in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) in negative.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def retrieve(
        self,
        transaction: str,
        params: "TransactionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Transaction:
        """
        Retrieves a Tax Transaction object.
        """
        return cast(
            Transaction,
            self._request(
                "get",
                "/v1/tax/transactions/{transaction}".format(
                    transaction=sanitize_id(transaction),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        transaction: str,
        params: "TransactionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Transaction:
        """
        Retrieves a Tax Transaction object.
        """
        return cast(
            Transaction,
            await self._request_async(
                "get",
                "/v1/tax/transactions/{transaction}".format(
                    transaction=sanitize_id(transaction),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create_from_calculation(
        self,
        params: "TransactionService.CreateFromCalculationParams",
        options: RequestOptions = {},
    ) -> Transaction:
        """
        Creates a Tax Transaction from a calculation, if that calculation hasn't expired. Calculations expire after 90 days.
        """
        return cast(
            Transaction,
            self._request(
                "post",
                "/v1/tax/transactions/create_from_calculation",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_from_calculation_async(
        self,
        params: "TransactionService.CreateFromCalculationParams",
        options: RequestOptions = {},
    ) -> Transaction:
        """
        Creates a Tax Transaction from a calculation, if that calculation hasn't expired. Calculations expire after 90 days.
        """
        return cast(
            Transaction,
            await self._request_async(
                "post",
                "/v1/tax/transactions/create_from_calculation",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create_reversal(
        self,
        params: "TransactionService.CreateReversalParams",
        options: RequestOptions = {},
    ) -> Transaction:
        """
        Partially or fully reverses a previously created Transaction.
        """
        return cast(
            Transaction,
            self._request(
                "post",
                "/v1/tax/transactions/create_reversal",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_reversal_async(
        self,
        params: "TransactionService.CreateReversalParams",
        options: RequestOptions = {},
    ) -> Transaction:
        """
        Partially or fully reverses a previously created Transaction.
        """
        return cast(
            Transaction,
            await self._request_async(
                "post",
                "/v1/tax/transactions/create_reversal",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
