# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._cash_balance import CashBalance
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CustomerCashBalanceService(StripeService):
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
        settings: NotRequired[
            "CustomerCashBalanceService.UpdateParamsSettings"
        ]
        """
        A hash of settings for this cash balance.
        """

    class UpdateParamsSettings(TypedDict):
        reconciliation_mode: NotRequired[
            Literal["automatic", "manual", "merchant_default"]
        ]
        """
        Controls how funds transferred by the customer are applied to payment intents and invoices. Valid options are `automatic`, `manual`, or `merchant_default`. For more information about these reconciliation modes, see [Reconciliation](https://stripe.com/docs/payments/customer-balance/reconciliation).
        """

    def retrieve(
        self,
        customer: str,
        params: "CustomerCashBalanceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> CashBalance:
        """
        Retrieves a customer's cash balance.
        """
        return cast(
            CashBalance,
            self._request(
                "get",
                "/v1/customers/{customer}/cash_balance".format(
                    customer=sanitize_id(customer),
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
        params: "CustomerCashBalanceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> CashBalance:
        """
        Retrieves a customer's cash balance.
        """
        return cast(
            CashBalance,
            await self._request_async(
                "get",
                "/v1/customers/{customer}/cash_balance".format(
                    customer=sanitize_id(customer),
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
        params: "CustomerCashBalanceService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> CashBalance:
        """
        Changes the settings on a customer's cash balance.
        """
        return cast(
            CashBalance,
            self._request(
                "post",
                "/v1/customers/{customer}/cash_balance".format(
                    customer=sanitize_id(customer),
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
        params: "CustomerCashBalanceService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> CashBalance:
        """
        Changes the settings on a customer's cash balance.
        """
        return cast(
            CashBalance,
            await self._request_async(
                "post",
                "/v1/customers/{customer}/cash_balance".format(
                    customer=sanitize_id(customer),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
