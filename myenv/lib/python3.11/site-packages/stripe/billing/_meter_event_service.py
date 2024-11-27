# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe.billing._meter_event import MeterEvent
from typing import Dict, List, cast
from typing_extensions import NotRequired, TypedDict


class MeterEventService(StripeService):
    class CreateParams(TypedDict):
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

    def create(
        self,
        params: "MeterEventService.CreateParams",
        options: RequestOptions = {},
    ) -> MeterEvent:
        """
        Creates a billing meter event
        """
        return cast(
            MeterEvent,
            self._request(
                "post",
                "/v1/billing/meter_events",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "MeterEventService.CreateParams",
        options: RequestOptions = {},
    ) -> MeterEvent:
        """
        Creates a billing meter event
        """
        return cast(
            MeterEvent,
            await self._request_async(
                "post",
                "/v1/billing/meter_events",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
