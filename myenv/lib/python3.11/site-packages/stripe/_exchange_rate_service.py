# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._exchange_rate import ExchangeRate
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class ExchangeRateService(StripeService):
    class ListParams(TypedDict):
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is the currency that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with the exchange rate for currency X your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and total number of supported payout currencies, and the default is the max.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is the currency that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with the exchange rate for currency X, your subsequent call can include `starting_after=X` in order to fetch the next page of the list.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "ExchangeRateService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ExchangeRate]:
        """
        Returns a list of objects that contain the rates at which foreign currencies are converted to one another. Only shows the currencies for which Stripe supports.
        """
        return cast(
            ListObject[ExchangeRate],
            self._request(
                "get",
                "/v1/exchange_rates",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ExchangeRateService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ExchangeRate]:
        """
        Returns a list of objects that contain the rates at which foreign currencies are converted to one another. Only shows the currencies for which Stripe supports.
        """
        return cast(
            ListObject[ExchangeRate],
            await self._request_async(
                "get",
                "/v1/exchange_rates",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        rate_id: str,
        params: "ExchangeRateService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ExchangeRate:
        """
        Retrieves the exchange rates from the given currency to every supported currency.
        """
        return cast(
            ExchangeRate,
            self._request(
                "get",
                "/v1/exchange_rates/{rate_id}".format(
                    rate_id=sanitize_id(rate_id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        rate_id: str,
        params: "ExchangeRateService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ExchangeRate:
        """
        Retrieves the exchange rates from the given currency to every supported currency.
        """
        return cast(
            ExchangeRate,
            await self._request_async(
                "get",
                "/v1/exchange_rates/{rate_id}".format(
                    rate_id=sanitize_id(rate_id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
