# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._customer_cash_balance_transaction import (
    CustomerCashBalanceTransaction,
)
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class CustomerService(StripeService):
    class FundCashBalanceParams(TypedDict):
        amount: int
        """
        Amount to be used for this test cash balance transaction. A positive integer representing how much to fund in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) (e.g., 100 cents to fund $1.00 or 100 to fund ¥100, a zero-decimal currency).
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        reference: NotRequired[str]
        """
        A description of the test funding. This simulates free-text references supplied by customers when making bank transfers to their cash balance. You can use this to test how Stripe's [reconciliation algorithm](https://stripe.com/docs/payments/customer-balance/reconciliation) applies to different user inputs.
        """

    def fund_cash_balance(
        self,
        customer: str,
        params: "CustomerService.FundCashBalanceParams",
        options: RequestOptions = {},
    ) -> CustomerCashBalanceTransaction:
        """
        Create an incoming testmode bank transfer
        """
        return cast(
            CustomerCashBalanceTransaction,
            self._request(
                "post",
                "/v1/test_helpers/customers/{customer}/fund_cash_balance".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def fund_cash_balance_async(
        self,
        customer: str,
        params: "CustomerService.FundCashBalanceParams",
        options: RequestOptions = {},
    ) -> CustomerCashBalanceTransaction:
        """
        Create an incoming testmode bank transfer
        """
        return cast(
            CustomerCashBalanceTransaction,
            await self._request_async(
                "post",
                "/v1/test_helpers/customers/{customer}/fund_cash_balance".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
