# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._customer_balance_transaction import CustomerBalanceTransaction
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CustomerBalanceTransactionService(StripeService):
    class CreateParams(TypedDict):
        amount: int
        """
        The integer amount in **cents (or local equivalent)** to apply to the customer's credit balance.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies). Specifies the [`invoice_credit_balance`](https://stripe.com/docs/api/customers/object#customer_object-invoice_credit_balance) that this transaction will apply to. If the customer's `currency` is not set, it will be updated to this value.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

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

    class UpdateParams(TypedDict):
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    def list(
        self,
        customer: str,
        params: "CustomerBalanceTransactionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[CustomerBalanceTransaction]:
        """
        Returns a list of transactions that updated the customer's [balances](https://stripe.com/docs/billing/customer/balance).
        """
        return cast(
            ListObject[CustomerBalanceTransaction],
            self._request(
                "get",
                "/v1/customers/{customer}/balance_transactions".format(
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
        params: "CustomerBalanceTransactionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[CustomerBalanceTransaction]:
        """
        Returns a list of transactions that updated the customer's [balances](https://stripe.com/docs/billing/customer/balance).
        """
        return cast(
            ListObject[CustomerBalanceTransaction],
            await self._request_async(
                "get",
                "/v1/customers/{customer}/balance_transactions".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        customer: str,
        params: "CustomerBalanceTransactionService.CreateParams",
        options: RequestOptions = {},
    ) -> CustomerBalanceTransaction:
        """
        Creates an immutable transaction that updates the customer's credit [balance](https://stripe.com/docs/billing/customer/balance).
        """
        return cast(
            CustomerBalanceTransaction,
            self._request(
                "post",
                "/v1/customers/{customer}/balance_transactions".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        customer: str,
        params: "CustomerBalanceTransactionService.CreateParams",
        options: RequestOptions = {},
    ) -> CustomerBalanceTransaction:
        """
        Creates an immutable transaction that updates the customer's credit [balance](https://stripe.com/docs/billing/customer/balance).
        """
        return cast(
            CustomerBalanceTransaction,
            await self._request_async(
                "post",
                "/v1/customers/{customer}/balance_transactions".format(
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
        params: "CustomerBalanceTransactionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> CustomerBalanceTransaction:
        """
        Retrieves a specific customer balance transaction that updated the customer's [balances](https://stripe.com/docs/billing/customer/balance).
        """
        return cast(
            CustomerBalanceTransaction,
            self._request(
                "get",
                "/v1/customers/{customer}/balance_transactions/{transaction}".format(
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
        params: "CustomerBalanceTransactionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> CustomerBalanceTransaction:
        """
        Retrieves a specific customer balance transaction that updated the customer's [balances](https://stripe.com/docs/billing/customer/balance).
        """
        return cast(
            CustomerBalanceTransaction,
            await self._request_async(
                "get",
                "/v1/customers/{customer}/balance_transactions/{transaction}".format(
                    customer=sanitize_id(customer),
                    transaction=sanitize_id(transaction),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        customer: str,
        transaction: str,
        params: "CustomerBalanceTransactionService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> CustomerBalanceTransaction:
        """
        Most credit balance transaction fields are immutable, but you may update its description and metadata.
        """
        return cast(
            CustomerBalanceTransaction,
            self._request(
                "post",
                "/v1/customers/{customer}/balance_transactions/{transaction}".format(
                    customer=sanitize_id(customer),
                    transaction=sanitize_id(transaction),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        customer: str,
        transaction: str,
        params: "CustomerBalanceTransactionService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> CustomerBalanceTransaction:
        """
        Most credit balance transaction fields are immutable, but you may update its description and metadata.
        """
        return cast(
            CustomerBalanceTransaction,
            await self._request_async(
                "post",
                "/v1/customers/{customer}/balance_transactions/{transaction}".format(
                    customer=sanitize_id(customer),
                    transaction=sanitize_id(transaction),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
