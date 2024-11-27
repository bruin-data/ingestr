# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._balance import Balance
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class BalanceService(StripeService):
    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def retrieve(
        self,
        params: "BalanceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Balance:
        """
        Retrieves the current account balance, based on the authentication that was used to make the request.
         For a sample request, see [Accounting for negative balances](https://stripe.com/docs/connect/account-balances#accounting-for-negative-balances).
        """
        return cast(
            Balance,
            self._request(
                "get",
                "/v1/balance",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        params: "BalanceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Balance:
        """
        Retrieves the current account balance, based on the authentication that was used to make the request.
         For a sample request, see [Accounting for negative balances](https://stripe.com/docs/connect/account-balances#accounting-for-negative-balances).
        """
        return cast(
            Balance,
            await self._request_async(
                "get",
                "/v1/balance",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
