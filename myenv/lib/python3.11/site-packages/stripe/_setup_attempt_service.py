# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._setup_attempt import SetupAttempt
from stripe._stripe_service import StripeService
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class SetupAttemptService(StripeService):
    class ListParams(TypedDict):
        created: NotRequired["SetupAttemptService.ListParamsCreated|int"]
        """
        A filter on the list, based on the object `created` field. The value
        can be a string with an integer Unix timestamp or a
        dictionary with a number of different query options.
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
        setup_intent: str
        """
        Only return SetupAttempts created by the SetupIntent specified by
        this ID.
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

    def list(
        self,
        params: "SetupAttemptService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[SetupAttempt]:
        """
        Returns a list of SetupAttempts that associate with a provided SetupIntent.
        """
        return cast(
            ListObject[SetupAttempt],
            self._request(
                "get",
                "/v1/setup_attempts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "SetupAttemptService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[SetupAttempt]:
        """
        Returns a list of SetupAttempts that associate with a provided SetupIntent.
        """
        return cast(
            ListObject[SetupAttempt],
            await self._request_async(
                "get",
                "/v1/setup_attempts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
