# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.test_helpers._test_clock import TestClock
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class TestClockService(StripeService):
    class AdvanceParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        frozen_time: int
        """
        The time to advance the test clock. Must be after the test clock's current frozen time. Cannot be more than two intervals in the future from the shortest subscription in this test clock. If there are no subscriptions in this test clock, it cannot be more than two years in the future.
        """

    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        frozen_time: int
        """
        The initial frozen time for this test clock.
        """
        name: NotRequired[str]
        """
        The name for this test clock.
        """

    class DeleteParams(TypedDict):
        pass

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

    def delete(
        self,
        test_clock: str,
        params: "TestClockService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> TestClock:
        """
        Deletes a test clock.
        """
        return cast(
            TestClock,
            self._request(
                "delete",
                "/v1/test_helpers/test_clocks/{test_clock}".format(
                    test_clock=sanitize_id(test_clock),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        test_clock: str,
        params: "TestClockService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> TestClock:
        """
        Deletes a test clock.
        """
        return cast(
            TestClock,
            await self._request_async(
                "delete",
                "/v1/test_helpers/test_clocks/{test_clock}".format(
                    test_clock=sanitize_id(test_clock),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        test_clock: str,
        params: "TestClockService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> TestClock:
        """
        Retrieves a test clock.
        """
        return cast(
            TestClock,
            self._request(
                "get",
                "/v1/test_helpers/test_clocks/{test_clock}".format(
                    test_clock=sanitize_id(test_clock),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        test_clock: str,
        params: "TestClockService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> TestClock:
        """
        Retrieves a test clock.
        """
        return cast(
            TestClock,
            await self._request_async(
                "get",
                "/v1/test_helpers/test_clocks/{test_clock}".format(
                    test_clock=sanitize_id(test_clock),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "TestClockService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[TestClock]:
        """
        Returns a list of your test clocks.
        """
        return cast(
            ListObject[TestClock],
            self._request(
                "get",
                "/v1/test_helpers/test_clocks",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "TestClockService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[TestClock]:
        """
        Returns a list of your test clocks.
        """
        return cast(
            ListObject[TestClock],
            await self._request_async(
                "get",
                "/v1/test_helpers/test_clocks",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "TestClockService.CreateParams",
        options: RequestOptions = {},
    ) -> TestClock:
        """
        Creates a new test clock that can be attached to new customers and quotes.
        """
        return cast(
            TestClock,
            self._request(
                "post",
                "/v1/test_helpers/test_clocks",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "TestClockService.CreateParams",
        options: RequestOptions = {},
    ) -> TestClock:
        """
        Creates a new test clock that can be attached to new customers and quotes.
        """
        return cast(
            TestClock,
            await self._request_async(
                "post",
                "/v1/test_helpers/test_clocks",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def advance(
        self,
        test_clock: str,
        params: "TestClockService.AdvanceParams",
        options: RequestOptions = {},
    ) -> TestClock:
        """
        Starts advancing a test clock to a specified time in the future. Advancement is done when status changes to Ready.
        """
        return cast(
            TestClock,
            self._request(
                "post",
                "/v1/test_helpers/test_clocks/{test_clock}/advance".format(
                    test_clock=sanitize_id(test_clock),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def advance_async(
        self,
        test_clock: str,
        params: "TestClockService.AdvanceParams",
        options: RequestOptions = {},
    ) -> TestClock:
        """
        Starts advancing a test clock to a specified time in the future. Advancement is done when status changes to Ready.
        """
        return cast(
            TestClock,
            await self._request_async(
                "post",
                "/v1/test_helpers/test_clocks/{test_clock}/advance".format(
                    test_clock=sanitize_id(test_clock),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
