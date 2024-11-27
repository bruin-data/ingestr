# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from typing import ClassVar, List, Optional
from typing_extensions import Literal, NotRequired, Unpack, TYPE_CHECKING

if TYPE_CHECKING:
    from stripe._file import File


class ScheduledQueryRun(ListableAPIResource["ScheduledQueryRun"]):
    """
    If you have [scheduled a Sigma query](https://stripe.com/docs/sigma/scheduled-queries), you'll
    receive a `sigma.scheduled_query_run.created` webhook each time the query
    runs. The webhook contains a `ScheduledQueryRun` object, which you can use to
    retrieve the query results.
    """

    OBJECT_NAME: ClassVar[Literal["scheduled_query_run"]] = (
        "scheduled_query_run"
    )

    class Error(StripeObject):
        message: str
        """
        Information about the run failure.
        """

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
    data_load_time: int
    """
    When the query was run, Sigma contained a snapshot of your Stripe data at this time.
    """
    error: Optional[Error]
    file: Optional["File"]
    """
    The file object representing the results of the query.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["scheduled_query_run"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    result_available_until: int
    """
    Time at which the result expires and is no longer available for download.
    """
    sql: str
    """
    SQL for the query.
    """
    status: str
    """
    The query's execution status, which will be `completed` for successful runs, and `canceled`, `failed`, or `timed_out` otherwise.
    """
    title: str
    """
    Title of the query.
    """

    @classmethod
    def list(
        cls, **params: Unpack["ScheduledQueryRun.ListParams"]
    ) -> ListObject["ScheduledQueryRun"]:
        """
        Returns a list of scheduled query runs.
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
        cls, **params: Unpack["ScheduledQueryRun.ListParams"]
    ) -> ListObject["ScheduledQueryRun"]:
        """
        Returns a list of scheduled query runs.
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
        cls, id: str, **params: Unpack["ScheduledQueryRun.RetrieveParams"]
    ) -> "ScheduledQueryRun":
        """
        Retrieves the details of an scheduled query run.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["ScheduledQueryRun.RetrieveParams"]
    ) -> "ScheduledQueryRun":
        """
        Retrieves the details of an scheduled query run.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def class_url(cls):
        return "/v1/sigma/scheduled_query_runs"

    _inner_class_types = {"error": Error}
