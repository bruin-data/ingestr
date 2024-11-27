# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.forwarding._request import Request
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class RequestService(StripeService):
    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        payment_method: str
        """
        The PaymentMethod to insert into the forwarded request. Forwarding previously consumed PaymentMethods is allowed.
        """
        replacements: List[
            Literal[
                "card_cvc", "card_expiry", "card_number", "cardholder_name"
            ]
        ]
        """
        The field kinds to be replaced in the forwarded request.
        """
        request: "RequestService.CreateParamsRequest"
        """
        The request body and headers to be sent to the destination endpoint.
        """
        url: str
        """
        The destination URL for the forwarded request. Must be supported by the config.
        """

    class CreateParamsRequest(TypedDict):
        body: NotRequired[str]
        """
        The body payload to send to the destination endpoint.
        """
        headers: NotRequired[List["RequestService.CreateParamsRequestHeader"]]
        """
        The headers to include in the forwarded request. Can be omitted if no additional headers (excluding Stripe-generated ones such as the Content-Type header) should be included.
        """

    class CreateParamsRequestHeader(TypedDict):
        name: str
        """
        The header name.
        """
        value: str
        """
        The header value.
        """

    class ListParams(TypedDict):
        created: NotRequired["RequestService.ListParamsCreated"]
        """
        Similar to other List endpoints, filters results based on created timestamp. You can pass gt, gte, lt, and lte timestamp values.
        """
        ending_before: NotRequired[str]
        """
        A pagination cursor to fetch the previous page of the list. The value must be a ForwardingRequest ID.
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
        A pagination cursor to fetch the next page of the list. The value must be a ForwardingRequest ID.
        """

    class ListParamsCreated(TypedDict):
        gt: NotRequired[int]
        """
        Return results where the `created` field is greater than this value.
        """
        gte: NotRequired[int]
        """
        Return results where the `created` field is greater than or equal to this value.
        """
        lt: NotRequired[int]
        """
        Return results where the `created` field is less than this value.
        """
        lte: NotRequired[int]
        """
        Return results where the `created` field is less than or equal to this value.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    def list(
        self,
        params: "RequestService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Request]:
        """
        Lists all ForwardingRequest objects.
        """
        return cast(
            ListObject[Request],
            self._request(
                "get",
                "/v1/forwarding/requests",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "RequestService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Request]:
        """
        Lists all ForwardingRequest objects.
        """
        return cast(
            ListObject[Request],
            await self._request_async(
                "get",
                "/v1/forwarding/requests",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "RequestService.CreateParams",
        options: RequestOptions = {},
    ) -> Request:
        """
        Creates a ForwardingRequest object.
        """
        return cast(
            Request,
            self._request(
                "post",
                "/v1/forwarding/requests",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "RequestService.CreateParams",
        options: RequestOptions = {},
    ) -> Request:
        """
        Creates a ForwardingRequest object.
        """
        return cast(
            Request,
            await self._request_async(
                "post",
                "/v1/forwarding/requests",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "RequestService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Request:
        """
        Retrieves a ForwardingRequest object.
        """
        return cast(
            Request,
            self._request(
                "get",
                "/v1/forwarding/requests/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "RequestService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Request:
        """
        Retrieves a ForwardingRequest object.
        """
        return cast(
            Request,
            await self._request_async(
                "get",
                "/v1/forwarding/requests/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
