# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._request_options import RequestOptions
from typing import ClassVar, Dict, List, cast
from typing_extensions import Literal, NotRequired, Unpack


class MeterEvent(CreateableAPIResource["MeterEvent"]):
    """
    A billing meter event represents a customer's usage of a product. Meter events are used to bill a customer based on their usage.
    Meter events are associated with billing meters, which define the shape of the event's payload and how those events are aggregated for billing.
    """

    OBJECT_NAME: ClassVar[Literal["billing.meter_event"]] = (
        "billing.meter_event"
    )

    class CreateParams(RequestOptions):
        event_name: str
        """
        The name of the meter event. Corresponds with the `event_name` field on a meter.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        identifier: NotRequired[str]
        """
        A unique identifier for the event. If not provided, one will be generated. We recommend using a globally unique identifier for this. We'll enforce uniqueness within a rolling 24 hour period.
        """
        payload: Dict[str, str]
        """
        The payload of the event. This must contain the fields corresponding to a meter's `customer_mapping.event_payload_key` (default is `stripe_customer_id`) and `value_settings.event_payload_key` (default is `value`). Read more about the [payload](https://docs.stripe.com/billing/subscriptions/usage-based/recording-usage#payload-key-overrides).
        """
        timestamp: NotRequired[int]
        """
        The time of the event. Measured in seconds since the Unix epoch. Must be within the past 35 calendar days or up to 5 minutes in the future. Defaults to current timestamp if not specified.
        """

    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    event_name: str
    """
    The name of the meter event. Corresponds with the `event_name` field on a meter.
    """
    identifier: str
    """
    A unique identifier for the event.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["billing.meter_event"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    payload: Dict[str, str]
    """
    The payload of the event. This contains the fields corresponding to a meter's `customer_mapping.event_payload_key` (default is `stripe_customer_id`) and `value_settings.event_payload_key` (default is `value`). Read more about the [payload](https://stripe.com/docs/billing/subscriptions/usage-based/recording-usage#payload-key-overrides).
    """
    timestamp: int
    """
    The timestamp passed in when creating the event. Measured in seconds since the Unix epoch.
    """

    @classmethod
    def create(
        cls, **params: Unpack["MeterEvent.CreateParams"]
    ) -> "MeterEvent":
        """
        Creates a billing meter event
        """
        return cast(
            "MeterEvent",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["MeterEvent.CreateParams"]
    ) -> "MeterEvent":
        """
        Creates a billing meter event
        """
        return cast(
            "MeterEvent",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )
