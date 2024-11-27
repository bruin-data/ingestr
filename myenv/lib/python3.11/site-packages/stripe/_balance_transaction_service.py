# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._balance_transaction import BalanceTransaction
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class BalanceTransactionService(StripeService):
    class ListParams(TypedDict):
        created: NotRequired["BalanceTransactionService.ListParamsCreated|int"]
        """
        Only return transactions that were created during the given date interval.
        """
        currency: NotRequired[str]
        """
        Only return transactions in a certain currency. Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
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
        payout: NotRequired[str]
        """
        For automatic Stripe payouts only, only returns transactions that were paid out on the specified payout ID.
        """
        source: NotRequired[str]
        """
        Only returns the original transaction.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        type: NotRequired[str]
        """
        Only returns transactions of the given type. One of: `adjustment`, `advance`, `advance_funding`, `anticipation_repayment`, `application_fee`, `application_fee_refund`, `charge`, `climate_order_purchase`, `climate_order_refund`, `connect_collection_transfer`, `contribution`, `issuing_authorization_hold`, `issuing_authorization_release`, `issuing_dispute`, `issuing_transaction`, `obligation_outbound`, `obligation_reversal_inbound`, `payment`, `payment_failure_refund`, `payment_network_reserve_hold`, `payment_network_reserve_release`, `payment_refund`, `payment_reversal`, `payment_unreconciled`, `payout`, `payout_cancel`, `payout_failure`, `refund`, `refund_failure`, `reserve_transaction`, `reserved_funds`, `stripe_fee`, `stripe_fx_fee`, `tax_fee`, `topup`, `topup_reversal`, `transfer`, `transfer_cancel`, `transfer_failure`, or `transfer_refund`.
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

    def list(
        self,
        params: "BalanceTransactionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[BalanceTransaction]:
        """
        Returns a list of transactions that have contributed to the Stripe account balance (e.g., charges, transfers, and so forth). The transactions are returned in sorted order, with the most recent transactions appearing first.

        Note that this endpoint was previously called “Balance history” and used the path /v1/balance/history.
        """
        return cast(
            ListObject[BalanceTransaction],
            self._request(
                "get",
                "/v1/balance_transactions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "BalanceTransactionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[BalanceTransaction]:
        """
        Returns a list of transactions that have contributed to the Stripe account balance (e.g., charges, transfers, and so forth). The transactions are returned in sorted order, with the most recent transactions appearing first.

        Note that this endpoint was previously called “Balance history” and used the path /v1/balance/history.
        """
        return cast(
            ListObject[BalanceTransaction],
            await self._request_async(
                "get",
                "/v1/balance_transactions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "BalanceTransactionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> BalanceTransaction:
        """
        Retrieves the balance transaction with the given ID.

        Note that this endpoint previously used the path /v1/balance/history/:id.
        """
        return cast(
            BalanceTransaction,
            self._request(
                "get",
                "/v1/balance_transactions/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "BalanceTransactionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> BalanceTransaction:
        """
        Retrieves the balance transaction with the given ID.

        Note that this endpoint previously used the path /v1/balance/history/:id.
        """
        return cast(
            BalanceTransaction,
            await self._request_async(
                "get",
                "/v1/balance_transactions/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
