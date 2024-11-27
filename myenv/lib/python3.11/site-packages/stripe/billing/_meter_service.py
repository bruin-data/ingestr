# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.billing._meter import Meter
from stripe.billing._meter_event_summary_service import (
    MeterEventSummaryService,
)
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class MeterService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.event_summaries = MeterEventSummaryService(self._requestor)

    class CreateParams(TypedDict):
        customer_mapping: NotRequired[
            "MeterService.CreateParamsCustomerMapping"
        ]
        """
        Fields that specify how to map a meter event to a customer.
        """
        default_aggregation: "MeterService.CreateParamsDefaultAggregation"
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
        value_settings: NotRequired["MeterService.CreateParamsValueSettings"]
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

    class DeactivateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

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
        status: NotRequired[Literal["active", "inactive"]]
        """
        Filter results to only include meters with the given status.
        """

    class ReactivateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        display_name: NotRequired[str]
        """
        The meter's name.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "MeterService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Meter]:
        """
        Retrieve a list of billing meters.
        """
        return cast(
            ListObject[Meter],
            self._request(
                "get",
                "/v1/billing/meters",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "MeterService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Meter]:
        """
        Retrieve a list of billing meters.
        """
        return cast(
            ListObject[Meter],
            await self._request_async(
                "get",
                "/v1/billing/meters",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self, params: "MeterService.CreateParams", options: RequestOptions = {}
    ) -> Meter:
        """
        Creates a billing meter
        """
        return cast(
            Meter,
            self._request(
                "post",
                "/v1/billing/meters",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self, params: "MeterService.CreateParams", options: RequestOptions = {}
    ) -> Meter:
        """
        Creates a billing meter
        """
        return cast(
            Meter,
            await self._request_async(
                "post",
                "/v1/billing/meters",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "MeterService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Meter:
        """
        Retrieves a billing meter given an ID
        """
        return cast(
            Meter,
            self._request(
                "get",
                "/v1/billing/meters/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "MeterService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Meter:
        """
        Retrieves a billing meter given an ID
        """
        return cast(
            Meter,
            await self._request_async(
                "get",
                "/v1/billing/meters/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        id: str,
        params: "MeterService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Meter:
        """
        Updates a billing meter
        """
        return cast(
            Meter,
            self._request(
                "post",
                "/v1/billing/meters/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        id: str,
        params: "MeterService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Meter:
        """
        Updates a billing meter
        """
        return cast(
            Meter,
            await self._request_async(
                "post",
                "/v1/billing/meters/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def deactivate(
        self,
        id: str,
        params: "MeterService.DeactivateParams" = {},
        options: RequestOptions = {},
    ) -> Meter:
        """
        Deactivates a billing meter
        """
        return cast(
            Meter,
            self._request(
                "post",
                "/v1/billing/meters/{id}/deactivate".format(
                    id=sanitize_id(id)
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def deactivate_async(
        self,
        id: str,
        params: "MeterService.DeactivateParams" = {},
        options: RequestOptions = {},
    ) -> Meter:
        """
        Deactivates a billing meter
        """
        return cast(
            Meter,
            await self._request_async(
                "post",
                "/v1/billing/meters/{id}/deactivate".format(
                    id=sanitize_id(id)
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def reactivate(
        self,
        id: str,
        params: "MeterService.ReactivateParams" = {},
        options: RequestOptions = {},
    ) -> Meter:
        """
        Reactivates a billing meter
        """
        return cast(
            Meter,
            self._request(
                "post",
                "/v1/billing/meters/{id}/reactivate".format(
                    id=sanitize_id(id)
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def reactivate_async(
        self,
        id: str,
        params: "MeterService.ReactivateParams" = {},
        options: RequestOptions = {},
    ) -> Meter:
        """
        Reactivates a billing meter
        """
        return cast(
            Meter,
            await self._request_async(
                "post",
                "/v1/billing/meters/{id}/reactivate".format(
                    id=sanitize_id(id)
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
