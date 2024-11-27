# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from typing import ClassVar, List, Optional, cast
from typing_extensions import Literal, NotRequired, TypedDict, Unpack


class MeterEventAdjustment(CreateableAPIResource["MeterEventAdjustment"]):
    """
    A billing meter event adjustment is a resource that allows you to cancel a meter event. For example, you might create a billing meter event adjustment to cancel a meter event that was created in error or attached to the wrong customer.
    """

    OBJECT_NAME: ClassVar[Literal["billing.meter_event_adjustment"]] = (
        "billing.meter_event_adjustment"
    )

    class Cancel(StripeObject):
        identifier: Optional[str]
        """
        Unique identifier for the event.
        """

    class CreateParams(RequestOptions):
        cancel: NotRequired["MeterEventAdjustment.CreateParamsCancel"]
        """
        Specifies which event to cancel.
        """
        event_name: str
        """
        The name of the meter event. Corresponds with the `event_name` field on a meter.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        type: Literal["cancel"]
        """
        Specifies whether to cancel a single event or a range of events for a time period. Time period cancellation is not supported yet.
        """

    class CreateParamsCancel(TypedDict):
        identifier: NotRequired[str]
        """
        Unique identifier for the event. You can only cancel events within 24 hours of Stripe receiving them.
        """

    cancel: Optional[Cancel]
    """
    Specifies which event to cancel.
    """
    event_name: str
    """
    The name of the meter event. Corresponds with the `event_name` field on a meter.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["billing.meter_event_adjustment"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    status: Literal["complete", "pending"]
    """
    The meter event adjustment's status.
    """
    type: Literal["cancel"]
    """
    Specifies whether to cancel a single event or a range of events for a time period. Time period cancellation is not supported yet.
    """

    @classmethod
    def create(
        cls, **params: Unpack["MeterEventAdjustment.CreateParams"]
    ) -> "MeterEventAdjustment":
        """
        Creates a billing meter event adjustment
        """
        return cast(
            "MeterEventAdjustment",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["MeterEventAdjustment.CreateParams"]
    ) -> "MeterEventAdjustment":
        """
        Creates a billing meter event adjustment
        """
        return cast(
            "MeterEventAdjustment",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    _inner_class_types = {"cancel": Cancel}
