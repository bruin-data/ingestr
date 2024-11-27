# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.issuing._authorization import Authorization
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class AuthorizationService(StripeService):
    class ApproveParams(TypedDict):
        amount: NotRequired[int]
        """
        If the authorization's `pending_request.is_amount_controllable` property is `true`, you may provide this value to control how much to hold for the authorization. Must be positive (use [`decline`](https://stripe.com/docs/api/issuing/authorizations/decline) to decline an authorization request).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class DeclineParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class ListParams(TypedDict):
        card: NotRequired[str]
        """
        Only return authorizations that belong to the given card.
        """
        cardholder: NotRequired[str]
        """
        Only return authorizations that belong to the given cardholder.
        """
        created: NotRequired["AuthorizationService.ListParamsCreated|int"]
        """
        Only return authorizations that were created during the given date interval.
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
        status: NotRequired[Literal["closed", "pending", "reversed"]]
        """
        Only return authorizations with the given status. One of `pending`, `closed`, or `reversed`.
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

    class UpdateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    def list(
        self,
        params: "AuthorizationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Authorization]:
        """
        Returns a list of Issuing Authorization objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[Authorization],
            self._request(
                "get",
                "/v1/issuing/authorizations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "AuthorizationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Authorization]:
        """
        Returns a list of Issuing Authorization objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[Authorization],
            await self._request_async(
                "get",
                "/v1/issuing/authorizations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        authorization: str,
        params: "AuthorizationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Authorization:
        """
        Retrieves an Issuing Authorization object.
        """
        return cast(
            Authorization,
            self._request(
                "get",
                "/v1/issuing/authorizations/{authorization}".format(
                    authorization=sanitize_id(authorization),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        authorization: str,
        params: "AuthorizationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Authorization:
        """
        Retrieves an Issuing Authorization object.
        """
        return cast(
            Authorization,
            await self._request_async(
                "get",
                "/v1/issuing/authorizations/{authorization}".format(
                    authorization=sanitize_id(authorization),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        authorization: str,
        params: "AuthorizationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Authorization:
        """
        Updates the specified Issuing Authorization object by setting the values of the parameters passed. Any parameters not provided will be left unchanged.
        """
        return cast(
            Authorization,
            self._request(
                "post",
                "/v1/issuing/authorizations/{authorization}".format(
                    authorization=sanitize_id(authorization),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        authorization: str,
        params: "AuthorizationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Authorization:
        """
        Updates the specified Issuing Authorization object by setting the values of the parameters passed. Any parameters not provided will be left unchanged.
        """
        return cast(
            Authorization,
            await self._request_async(
                "post",
                "/v1/issuing/authorizations/{authorization}".format(
                    authorization=sanitize_id(authorization),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def approve(
        self,
        authorization: str,
        params: "AuthorizationService.ApproveParams" = {},
        options: RequestOptions = {},
    ) -> Authorization:
        """
        [Deprecated] Approves a pending Issuing Authorization object. This request should be made within the timeout window of the [real-time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to approve an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            Authorization,
            self._request(
                "post",
                "/v1/issuing/authorizations/{authorization}/approve".format(
                    authorization=sanitize_id(authorization),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def approve_async(
        self,
        authorization: str,
        params: "AuthorizationService.ApproveParams" = {},
        options: RequestOptions = {},
    ) -> Authorization:
        """
        [Deprecated] Approves a pending Issuing Authorization object. This request should be made within the timeout window of the [real-time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to approve an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            Authorization,
            await self._request_async(
                "post",
                "/v1/issuing/authorizations/{authorization}/approve".format(
                    authorization=sanitize_id(authorization),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def decline(
        self,
        authorization: str,
        params: "AuthorizationService.DeclineParams" = {},
        options: RequestOptions = {},
    ) -> Authorization:
        """
        [Deprecated] Declines a pending Issuing Authorization object. This request should be made within the timeout window of the [real time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to decline an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            Authorization,
            self._request(
                "post",
                "/v1/issuing/authorizations/{authorization}/decline".format(
                    authorization=sanitize_id(authorization),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def decline_async(
        self,
        authorization: str,
        params: "AuthorizationService.DeclineParams" = {},
        options: RequestOptions = {},
    ) -> Authorization:
        """
        [Deprecated] Declines a pending Issuing Authorization object. This request should be made within the timeout window of the [real time authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations) flow.
        This method is deprecated. Instead, [respond directly to the webhook request to decline an authorization](https://stripe.com/docs/issuing/controls/real-time-authorizations#authorization-handling).
        """
        return cast(
            Authorization,
            await self._request_async(
                "post",
                "/v1/issuing/authorizations/{authorization}/decline".format(
                    authorization=sanitize_id(authorization),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
