# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._event import Event
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import NotRequired, TypedDict


class EventService(StripeService):
    class ListParams(TypedDict):
        created: NotRequired["EventService.ListParamsCreated|int"]
        """
        Only return events that were created during the given date interval.
        """
        delivery_success: NotRequired[bool]
        """
        Filter events by whether all webhooks were successfully delivered. If false, events which are still pending or have failed all delivery attempts to a webhook endpoint will be returned.
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
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        type: NotRequired[str]
        """
        A string containing a specific event name, or group of events using * as a wildcard. The list will be filtered to include only events with a matching event property.
        """
        types: NotRequired[List[str]]
        """
        An array of up to 20 strings containing specific event names. The list will be filtered to include only events with a matching event property. You may pass either `type` or `types`, but not both.
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

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "EventService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Event]:
        """
        List events, going back up to 30 days. Each event data is rendered according to Stripe API version at its creation time, specified in [event object](https://docs.stripe.com/api/events/object) api_version attribute (not according to your current Stripe API version or Stripe-Version header).
        """
        return cast(
            ListObject[Event],
            self._request(
                "get",
                "/v1/events",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "EventService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Event]:
        """
        List events, going back up to 30 days. Each event data is rendered according to Stripe API version at its creation time, specified in [event object](https://docs.stripe.com/api/events/object) api_version attribute (not according to your current Stripe API version or Stripe-Version header).
        """
        return cast(
            ListObject[Event],
            await self._request_async(
                "get",
                "/v1/events",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "EventService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Event:
        """
        Retrieves the details of an event if it was created in the last 30 days. Supply the unique identifier of the event, which you might have received in a webhook.
        """
        return cast(
            Event,
            self._request(
                "get",
                "/v1/events/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "EventService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Event:
        """
        Retrieves the details of an event if it was created in the last 30 days. Supply the unique identifier of the event, which you might have received in a webhook.
        """
        return cast(
            Event,
            await self._request_async(
                "get",
                "/v1/events/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
