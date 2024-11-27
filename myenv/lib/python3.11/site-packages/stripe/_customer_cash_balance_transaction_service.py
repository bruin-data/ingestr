# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._customer_cash_balance_transaction import (
    CustomerCashBalanceTransaction,
)
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class CustomerCashBalanceTransactionService(StripeService):
    class ListParams(TypedDict):
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

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        customer: str,
        params: "CustomerCashBalanceTransactionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[CustomerCashBalanceTransaction]:
        """
        Returns a list of transactions that modified the customer's [cash balance](https://stripe.com/docs/payments/customer-balance).
        """
        return cast(
            ListObject[CustomerCashBalanceTransaction],
            self._request(
                "get",
                "/v1/customers/{customer}/cash_balance_transactions".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        customer: str,
        params: "CustomerCashBalanceTransactionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[CustomerCashBalanceTransaction]:
        """
        Returns a list of transactions that modified the customer's [cash balance](https://stripe.com/docs/payments/customer-balance).
        """
        return cast(
            ListObject[CustomerCashBalanceTransaction],
            await self._request_async(
                "get",
                "/v1/customers/{customer}/cash_balance_transactions".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        customer: str,
        transaction: str,
        params: "CustomerCashBalanceTransactionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> CustomerCashBalanceTransaction:
        """
        Retrieves a specific cash balance transaction, which updated the customer's [cash balance](https://stripe.com/docs/payments/customer-balance).
        """
        return cast(
            CustomerCashBalanceTransaction,
            self._request(
                "get",
                "/v1/customers/{customer}/cash_balance_transactions/{transaction}".format(
                    customer=sanitize_id(customer),
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
        customer: str,
        transaction: str,
        params: "CustomerCashBalanceTransactionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> CustomerCashBalanceTransaction:
        """
        Retrieves a specific cash balance transaction, which updated the customer's [cash balance](https://stripe.com/docs/payments/customer-balance).
        """
        return cast(
            CustomerCashBalanceTransaction,
            await self._request_async(
                "get",
                "/v1/customers/{customer}/cash_balance_transactions/{transaction}".format(
                    customer=sanitize_id(customer),
                    transaction=sanitize_id(transaction),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
