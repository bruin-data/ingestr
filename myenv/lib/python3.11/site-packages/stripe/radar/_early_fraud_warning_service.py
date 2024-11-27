# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.radar._early_fraud_warning import EarlyFraudWarning
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class EarlyFraudWarningService(StripeService):
    class ListParams(TypedDict):
        charge: NotRequired[str]
        """
        Only return early fraud warnings for the charge specified by this charge ID.
        """
        created: NotRequired["EarlyFraudWarningService.ListParamsCreated|int"]
        """
        Only return early fraud warnings that were created during the given date interval.
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
        payment_intent: NotRequired[str]
        """
        Only return early fraud warnings for charges that were created by the PaymentIntent specified by this PaymentIntent ID.
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

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "EarlyFraudWarningService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[EarlyFraudWarning]:
        """
        Returns a list of early fraud warnings.
        """
        return cast(
            ListObject[EarlyFraudWarning],
            self._request(
                "get",
                "/v1/radar/early_fraud_warnings",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "EarlyFraudWarningService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[EarlyFraudWarning]:
        """
        Returns a list of early fraud warnings.
        """
        return cast(
            ListObject[EarlyFraudWarning],
            await self._request_async(
                "get",
                "/v1/radar/early_fraud_warnings",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        early_fraud_warning: str,
        params: "EarlyFraudWarningService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> EarlyFraudWarning:
        """
        Retrieves the details of an early fraud warning that has previously been created.

        Please refer to the [early fraud warning](https://stripe.com/docs/api#early_fraud_warning_object) object reference for more details.
        """
        return cast(
            EarlyFraudWarning,
            self._request(
                "get",
                "/v1/radar/early_fraud_warnings/{early_fraud_warning}".format(
                    early_fraud_warning=sanitize_id(early_fraud_warning),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        early_fraud_warning: str,
        params: "EarlyFraudWarningService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> EarlyFraudWarning:
        """
        Retrieves the details of an early fraud warning that has previously been created.

        Please refer to the [early fraud warning](https://stripe.com/docs/api#early_fraud_warning_object) object reference for more details.
        """
        return cast(
            EarlyFraudWarning,
            await self._request_async(
                "get",
                "/v1/radar/early_fraud_warnings/{early_fraud_warning}".format(
                    early_fraud_warning=sanitize_id(early_fraud_warning),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
