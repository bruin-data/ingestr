# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._deletable_api_resource import DeletableAPIResource
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, List, Optional, cast, overload
from typing_extensions import Literal, NotRequired, Unpack


class TestClock(
    CreateableAPIResource["TestClock"],
    DeletableAPIResource["TestClock"],
    ListableAPIResource["TestClock"],
):
    """
    A test clock enables deterministic control over objects in testmode. With a test clock, you can create
    objects at a frozen time in the past or future, and advance to a specific future time to observe webhooks and state changes. After the clock advances,
    you can either validate the current state of your scenario (and test your assumptions), change the current state of your scenario (and test more complex scenarios), or keep advancing forward in time.
    """

    OBJECT_NAME: ClassVar[Literal["test_helpers.test_clock"]] = (
        "test_helpers.test_clock"
    )

    class AdvanceParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        frozen_time: int
        """
        The time to advance the test clock. Must be after the test clock's current frozen time. Cannot be more than two intervals in the future from the shortest subscription in this test clock. If there are no subscriptions in this test clock, it cannot be more than two years in the future.
        """

    class CreateParams(RequestOptions):
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

    class DeleteParams(RequestOptions):
        pass

    class ListParams(RequestOptions):
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    deletes_after: int
    """
    Time at which this clock is scheduled to auto delete.
    """
    frozen_time: int
    """
    Time at which all objects belonging to this clock are frozen.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    name: Optional[str]
    """
    The custom name supplied at creation.
    """
    object: Literal["test_helpers.test_clock"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    status: Literal["advancing", "internal_failure", "ready"]
    """
    The status of the Test Clock.
    """
    deleted: Optional[Literal[True]]
    """
    Always true for a deleted object
    """

    @classmethod
    def _cls_advance(
        cls, test_clock: str, **params: Unpack["TestClock.AdvanceParams"]
    ) -> "TestClock":
        """
        Starts advancing a test clock to a specified time in the future. Advancement is done when status changes to Ready.
        """
        return cast(
            "TestClock",
            cls._static_request(
                "post",
                "/v1/test_helpers/test_clocks/{test_clock}/advance".format(
                    test_clock=sanitize_id(test_clock)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def advance(
        test_clock: str, **params: Unpack["TestClock.AdvanceParams"]
    ) -> "TestClock":
        """
        Starts advancing a test clock to a specified time in the future. Advancement is done when status changes to Ready.
        """
        ...

    @overload
    def advance(
        self, **params: Unpack["TestClock.AdvanceParams"]
    ) -> "TestClock":
        """
        Starts advancing a test clock to a specified time in the future. Advancement is done when status changes to Ready.
        """
        ...

    @class_method_variant("_cls_advance")
    def advance(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["TestClock.AdvanceParams"]
    ) -> "TestClock":
        """
        Starts advancing a test clock to a specified time in the future. Advancement is done when status changes to Ready.
        """
        return cast(
            "TestClock",
            self._request(
                "post",
                "/v1/test_helpers/test_clocks/{test_clock}/advance".format(
                    test_clock=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_advance_async(
        cls, test_clock: str, **params: Unpack["TestClock.AdvanceParams"]
    ) -> "TestClock":
        """
        Starts advancing a test clock to a specified time in the future. Advancement is done when status changes to Ready.
        """
        return cast(
            "TestClock",
            await cls._static_request_async(
                "post",
                "/v1/test_helpers/test_clocks/{test_clock}/advance".format(
                    test_clock=sanitize_id(test_clock)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def advance_async(
        test_clock: str, **params: Unpack["TestClock.AdvanceParams"]
    ) -> "TestClock":
        """
        Starts advancing a test clock to a specified time in the future. Advancement is done when status changes to Ready.
        """
        ...

    @overload
    async def advance_async(
        self, **params: Unpack["TestClock.AdvanceParams"]
    ) -> "TestClock":
        """
        Starts advancing a test clock to a specified time in the future. Advancement is done when status changes to Ready.
        """
        ...

    @class_method_variant("_cls_advance_async")
    async def advance_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["TestClock.AdvanceParams"]
    ) -> "TestClock":
        """
        Starts advancing a test clock to a specified time in the future. Advancement is done when status changes to Ready.
        """
        return cast(
            "TestClock",
            await self._request_async(
                "post",
                "/v1/test_helpers/test_clocks/{test_clock}/advance".format(
                    test_clock=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def create(cls, **params: Unpack["TestClock.CreateParams"]) -> "TestClock":
        """
        Creates a new test clock that can be attached to new customers and quotes.
        """
        return cast(
            "TestClock",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["TestClock.CreateParams"]
    ) -> "TestClock":
        """
        Creates a new test clock that can be attached to new customers and quotes.
        """
        return cast(
            "TestClock",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_delete(
        cls, sid: str, **params: Unpack["TestClock.DeleteParams"]
    ) -> "TestClock":
        """
        Deletes a test clock.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "TestClock",
            cls._static_request(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    def delete(
        sid: str, **params: Unpack["TestClock.DeleteParams"]
    ) -> "TestClock":
        """
        Deletes a test clock.
        """
        ...

    @overload
    def delete(
        self, **params: Unpack["TestClock.DeleteParams"]
    ) -> "TestClock":
        """
        Deletes a test clock.
        """
        ...

    @class_method_variant("_cls_delete")
    def delete(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["TestClock.DeleteParams"]
    ) -> "TestClock":
        """
        Deletes a test clock.
        """
        return self._request_and_refresh(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    async def _cls_delete_async(
        cls, sid: str, **params: Unpack["TestClock.DeleteParams"]
    ) -> "TestClock":
        """
        Deletes a test clock.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "TestClock",
            await cls._static_request_async(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def delete_async(
        sid: str, **params: Unpack["TestClock.DeleteParams"]
    ) -> "TestClock":
        """
        Deletes a test clock.
        """
        ...

    @overload
    async def delete_async(
        self, **params: Unpack["TestClock.DeleteParams"]
    ) -> "TestClock":
        """
        Deletes a test clock.
        """
        ...

    @class_method_variant("_cls_delete_async")
    async def delete_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["TestClock.DeleteParams"]
    ) -> "TestClock":
        """
        Deletes a test clock.
        """
        return await self._request_and_refresh_async(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    def list(
        cls, **params: Unpack["TestClock.ListParams"]
    ) -> ListObject["TestClock"]:
        """
        Returns a list of your test clocks.
        """
        result = cls._static_request(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    async def list_async(
        cls, **params: Unpack["TestClock.ListParams"]
    ) -> ListObject["TestClock"]:
        """
        Returns a list of your test clocks.
        """
        result = await cls._static_request_async(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["TestClock.RetrieveParams"]
    ) -> "TestClock":
        """
        Retrieves a test clock.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["TestClock.RetrieveParams"]
    ) -> "TestClock":
        """
        Retrieves a test clock.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance
