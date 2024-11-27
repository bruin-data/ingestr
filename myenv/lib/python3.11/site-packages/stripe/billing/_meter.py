# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._nested_resource_class_methods import nested_resource_class_methods
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, List, Optional, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe.billing._meter_event_summary import MeterEventSummary


@nested_resource_class_methods("event_summary")
class Meter(
    CreateableAPIResource["Meter"],
    ListableAPIResource["Meter"],
    UpdateableAPIResource["Meter"],
):
    """
    A billing meter is a resource that allows you to track usage of a particular event. For example, you might create a billing meter to track the number of API calls made by a particular user. You can then attach the billing meter to a price and attach the price to a subscription to charge the user for the number of API calls they make.
    """

    OBJECT_NAME: ClassVar[Literal["billing.meter"]] = "billing.meter"

    class CustomerMapping(StripeObject):
        event_payload_key: str
        """
        The key in the meter event payload to use for mapping the event to a customer.
        """
        type: Literal["by_id"]
        """
        The method for mapping a meter event to a customer.
        """

    class DefaultAggregation(StripeObject):
        formula: Literal["count", "sum"]
        """
        Specifies how events are aggregated.
        """

    class StatusTransitions(StripeObject):
        deactivated_at: Optional[int]
        """
        The time the meter was deactivated, if any. Measured in seconds since Unix epoch.
        """

    class ValueSettings(StripeObject):
        event_payload_key: str
        """
        The key in the meter event payload to use as the value for this meter.
        """

    class CreateParams(RequestOptions):
        customer_mapping: NotRequired["Meter.CreateParamsCustomerMapping"]
        """
        Fields that specify how to map a meter event to a customer.
        """
        default_aggregation: "Meter.CreateParamsDefaultAggregation"
        """
        The default settings to aggregate a meter's events with.
        """
        display_name: str
        """
        The meter's name.
        """
        event_name: str
        """
        The name of the meter event to record usage for. Corresponds with the `event_name` field on meter events.
        """
        event_time_window: NotRequired[Literal["day", "hour"]]
        """
        The time window to pre-aggregate meter events for, if any.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        value_settings: NotRequired["Meter.CreateParamsValueSettings"]
        """
        Fields that specify how to calculate a meter event's value.
        """

    class CreateParamsCustomerMapping(TypedDict):
        event_payload_key: str
        """
        The key in the usage event payload to use for mapping the event to a customer.
        """
        type: Literal["by_id"]
        """
        The method for mapping a meter event to a customer. Must be `by_id`.
        """

    class CreateParamsDefaultAggregation(TypedDict):
        formula: Literal["count", "sum"]
        """
        Specifies how events are aggregated. Allowed values are `count` to count the number of events and `sum` to sum each event's value.
        """

    class CreateParamsValueSettings(TypedDict):
        event_payload_key: str
        """
        The key in the usage event payload to use as the value for this meter. For example, if the event payload contains usage on a `bytes_used` field, then set the event_payload_key to "bytes_used".
        """

    class DeactivateParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListEventSummariesParams(RequestOptions):
        customer: str
        """
        The customer for which to fetch event summaries.
        """
        end_time: int
        """
        The timestamp from when to stop aggregating meter events (exclusive). Must be aligned with minute boundaries.
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
        start_time: int
        """
        The timestamp from when to start aggregating meter events (inclusive). Must be aligned with minute boundaries.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        value_grouping_window: NotRequired[Literal["day", "hour"]]
        """
        Specifies what granularity to use when generating event summaries. If not specified, a single event summary would be returned for the specified time range. For hourly granularity, start and end times must align with hour boundaries (e.g., 00:00, 01:00, ..., 23:00). For daily granularity, start and end times must align with UTC day boundaries (00:00 UTC).
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
        status: NotRequired[Literal["active", "inactive"]]
        """
        Filter results to only include meters with the given status.
        """

    class ModifyParams(RequestOptions):
        display_name: NotRequired[str]
        """
        The meter's name.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ReactivateParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
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
    customer_mapping: CustomerMapping
    default_aggregation: DefaultAggregation
    display_name: str
    """
    The meter's name.
    """
    event_name: str
    """
    The name of the meter event to record usage for. Corresponds with the `event_name` field on meter events.
    """
    event_time_window: Optional[Literal["day", "hour"]]
    """
    The time window to pre-aggregate meter events for, if any.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["billing.meter"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    status: Literal["active", "inactive"]
    """
    The meter's status.
    """
    status_transitions: StatusTransitions
    updated: int
    """
    Time at which the object was last updated. Measured in seconds since the Unix epoch.
    """
    value_settings: ValueSettings

    @classmethod
    def create(cls, **params: Unpack["Meter.CreateParams"]) -> "Meter":
        """
        Creates a billing meter
        """
        return cast(
            "Meter",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Meter.CreateParams"]
    ) -> "Meter":
        """
        Creates a billing meter
        """
        return cast(
            "Meter",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_deactivate(
        cls, id: str, **params: Unpack["Meter.DeactivateParams"]
    ) -> "Meter":
        """
        Deactivates a billing meter
        """
        return cast(
            "Meter",
            cls._static_request(
                "post",
                "/v1/billing/meters/{id}/deactivate".format(
                    id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def deactivate(
        id: str, **params: Unpack["Meter.DeactivateParams"]
    ) -> "Meter":
        """
        Deactivates a billing meter
        """
        ...

    @overload
    def deactivate(
        self, **params: Unpack["Meter.DeactivateParams"]
    ) -> "Meter":
        """
        Deactivates a billing meter
        """
        ...

    @class_method_variant("_cls_deactivate")
    def deactivate(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Meter.DeactivateParams"]
    ) -> "Meter":
        """
        Deactivates a billing meter
        """
        return cast(
            "Meter",
            self._request(
                "post",
                "/v1/billing/meters/{id}/deactivate".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_deactivate_async(
        cls, id: str, **params: Unpack["Meter.DeactivateParams"]
    ) -> "Meter":
        """
        Deactivates a billing meter
        """
        return cast(
            "Meter",
            await cls._static_request_async(
                "post",
                "/v1/billing/meters/{id}/deactivate".format(
                    id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def deactivate_async(
        id: str, **params: Unpack["Meter.DeactivateParams"]
    ) -> "Meter":
        """
        Deactivates a billing meter
        """
        ...

    @overload
    async def deactivate_async(
        self, **params: Unpack["Meter.DeactivateParams"]
    ) -> "Meter":
        """
        Deactivates a billing meter
        """
        ...

    @class_method_variant("_cls_deactivate_async")
    async def deactivate_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Meter.DeactivateParams"]
    ) -> "Meter":
        """
        Deactivates a billing meter
        """
        return cast(
            "Meter",
            await self._request_async(
                "post",
                "/v1/billing/meters/{id}/deactivate".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def list(cls, **params: Unpack["Meter.ListParams"]) -> ListObject["Meter"]:
        """
        Retrieve a list of billing meters.
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
        cls, **params: Unpack["Meter.ListParams"]
    ) -> ListObject["Meter"]:
        """
        Retrieve a list of billing meters.
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
    def modify(
        cls, id: str, **params: Unpack["Meter.ModifyParams"]
    ) -> "Meter":
        """
        Updates a billing meter
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Meter",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Meter.ModifyParams"]
    ) -> "Meter":
        """
        Updates a billing meter
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Meter",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def _cls_reactivate(
        cls, id: str, **params: Unpack["Meter.ReactivateParams"]
    ) -> "Meter":
        """
        Reactivates a billing meter
        """
        return cast(
            "Meter",
            cls._static_request(
                "post",
                "/v1/billing/meters/{id}/reactivate".format(
                    id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def reactivate(
        id: str, **params: Unpack["Meter.ReactivateParams"]
    ) -> "Meter":
        """
        Reactivates a billing meter
        """
        ...

    @overload
    def reactivate(
        self, **params: Unpack["Meter.ReactivateParams"]
    ) -> "Meter":
        """
        Reactivates a billing meter
        """
        ...

    @class_method_variant("_cls_reactivate")
    def reactivate(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Meter.ReactivateParams"]
    ) -> "Meter":
        """
        Reactivates a billing meter
        """
        return cast(
            "Meter",
            self._request(
                "post",
                "/v1/billing/meters/{id}/reactivate".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_reactivate_async(
        cls, id: str, **params: Unpack["Meter.ReactivateParams"]
    ) -> "Meter":
        """
        Reactivates a billing meter
        """
        return cast(
            "Meter",
            await cls._static_request_async(
                "post",
                "/v1/billing/meters/{id}/reactivate".format(
                    id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def reactivate_async(
        id: str, **params: Unpack["Meter.ReactivateParams"]
    ) -> "Meter":
        """
        Reactivates a billing meter
        """
        ...

    @overload
    async def reactivate_async(
        self, **params: Unpack["Meter.ReactivateParams"]
    ) -> "Meter":
        """
        Reactivates a billing meter
        """
        ...

    @class_method_variant("_cls_reactivate_async")
    async def reactivate_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Meter.ReactivateParams"]
    ) -> "Meter":
        """
        Reactivates a billing meter
        """
        return cast(
            "Meter",
            await self._request_async(
                "post",
                "/v1/billing/meters/{id}/reactivate".format(
                    id=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Meter.RetrieveParams"]
    ) -> "Meter":
        """
        Retrieves a billing meter given an ID
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Meter.RetrieveParams"]
    ) -> "Meter":
        """
        Retrieves a billing meter given an ID
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def list_event_summaries(
        cls, id: str, **params: Unpack["Meter.ListEventSummariesParams"]
    ) -> ListObject["MeterEventSummary"]:
        """
        Retrieve a list of billing meter event summaries.
        """
        return cast(
            ListObject["MeterEventSummary"],
            cls._static_request(
                "get",
                "/v1/billing/meters/{id}/event_summaries".format(
                    id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    @classmethod
    async def list_event_summaries_async(
        cls, id: str, **params: Unpack["Meter.ListEventSummariesParams"]
    ) -> ListObject["MeterEventSummary"]:
        """
        Retrieve a list of billing meter event summaries.
        """
        return cast(
            ListObject["MeterEventSummary"],
            await cls._static_request_async(
                "get",
                "/v1/billing/meters/{id}/event_summaries".format(
                    id=sanitize_id(id)
                ),
                params=params,
            ),
        )

    _inner_class_types = {
        "customer_mapping": CustomerMapping,
        "default_aggregation": DefaultAggregation,
        "status_transitions": StatusTransitions,
        "value_settings": ValueSettings,
    }
