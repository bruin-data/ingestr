# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe.billing._meter_event_adjustment import MeterEventAdjustment
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class MeterEventAdjustmentService(StripeService):
    class CreateParams(TypedDict):
        cancel: NotRequired["MeterEventAdjustmentService.CreateParamsCancel"]
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

    def create(
        self,
        params: "MeterEventAdjustmentService.CreateParams",
        options: RequestOptions = {},
    ) -> MeterEventAdjustment:
        """
        Creates a billing meter event adjustment
        """
        return cast(
            MeterEventAdjustment,
            self._request(
                "post",
                "/v1/billing/meter_event_adjustments",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "MeterEventAdjustmentService.CreateParams",
        options: RequestOptions = {},
    ) -> MeterEventAdjustment:
        """
        Creates a billing meter event adjustment
        """
        return cast(
            MeterEventAdjustment,
            await self._request_async(
                "post",
                "/v1/billing/meter_event_adjustments",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
