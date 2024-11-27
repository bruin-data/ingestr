# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.issuing._token import Token
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class TokenService(StripeService):
    class ListParams(TypedDict):
        card: str
        """
        The Issuing card identifier to list tokens for.
        """
        created: NotRequired["TokenService.ListParamsCreated|int"]
        """
        Only return Issuing tokens that were created during the given date interval.
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
        status: NotRequired[
            Literal["active", "deleted", "requested", "suspended"]
        ]
        """
        Select Issuing tokens with the given status.
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
        status: Literal["active", "deleted", "suspended"]
        """
        Specifies which status the token should be updated to.
        """

    def list(
        self, params: "TokenService.ListParams", options: RequestOptions = {}
    ) -> ListObject[Token]:
        """
        Lists all Issuing Token objects for a given card.
        """
        return cast(
            ListObject[Token],
            self._request(
                "get",
                "/v1/issuing/tokens",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self, params: "TokenService.ListParams", options: RequestOptions = {}
    ) -> ListObject[Token]:
        """
        Lists all Issuing Token objects for a given card.
        """
        return cast(
            ListObject[Token],
            await self._request_async(
                "get",
                "/v1/issuing/tokens",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        token: str,
        params: "TokenService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Token:
        """
        Retrieves an Issuing Token object.
        """
        return cast(
            Token,
            self._request(
                "get",
                "/v1/issuing/tokens/{token}".format(token=sanitize_id(token)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        token: str,
        params: "TokenService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Token:
        """
        Retrieves an Issuing Token object.
        """
        return cast(
            Token,
            await self._request_async(
                "get",
                "/v1/issuing/tokens/{token}".format(token=sanitize_id(token)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        token: str,
        params: "TokenService.UpdateParams",
        options: RequestOptions = {},
    ) -> Token:
        """
        Attempts to update the specified Issuing Token object to the status specified.
        """
        return cast(
            Token,
            self._request(
                "post",
                "/v1/issuing/tokens/{token}".format(token=sanitize_id(token)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        token: str,
        params: "TokenService.UpdateParams",
        options: RequestOptions = {},
    ) -> Token:
        """
        Attempts to update the specified Issuing Token object to the status specified.
        """
        return cast(
            Token,
            await self._request_async(
                "post",
                "/v1/issuing/tokens/{token}".format(token=sanitize_id(token)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
