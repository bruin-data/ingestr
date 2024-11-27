# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._topup import Topup
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class TopupService(StripeService):
    class CancelParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(TypedDict):
        amount: int
        """
        A positive integer representing how much to transfer.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        source: NotRequired[str]
        """
        The ID of a source to transfer funds from. For most users, this should be left unspecified which will use the bank account that was set up in the dashboard for the specified currency. In test mode, this can be a test bank token (see [Testing Top-ups](https://stripe.com/docs/connect/testing#testing-top-ups)).
        """
        statement_descriptor: NotRequired[str]
        """
        Extra information about a top-up for the source's bank statement. Limited to 15 ASCII characters.
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies this top-up as part of a group.
        """

    class ListParams(TypedDict):
        amount: NotRequired["TopupService.ListParamsAmount|int"]
        """
        A positive integer representing how much to transfer.
        """
        created: NotRequired["TopupService.ListParamsCreated|int"]
        """
        A filter on the list, based on the object `created` field. The value can be a string with an integer Unix timestamp, or it can be a dictionary with a number of different query options.
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
            Literal["canceled", "failed", "pending", "succeeded"]
        ]
        """
        Only return top-ups that have the given status. One of `canceled`, `failed`, `pending` or `succeeded`.
        """

    class ListParamsAmount(TypedDict):
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
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
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
        params: "TopupService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Topup]:
        """
        Returns a list of top-ups.
        """
        return cast(
            ListObject[Topup],
            self._request(
                "get",
                "/v1/topups",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "TopupService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Topup]:
        """
        Returns a list of top-ups.
        """
        return cast(
            ListObject[Topup],
            await self._request_async(
                "get",
                "/v1/topups",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self, params: "TopupService.CreateParams", options: RequestOptions = {}
    ) -> Topup:
        """
        Top up the balance of an account
        """
        return cast(
            Topup,
            self._request(
                "post",
                "/v1/topups",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self, params: "TopupService.CreateParams", options: RequestOptions = {}
    ) -> Topup:
        """
        Top up the balance of an account
        """
        return cast(
            Topup,
            await self._request_async(
                "post",
                "/v1/topups",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        topup: str,
        params: "TopupService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Topup:
        """
        Retrieves the details of a top-up that has previously been created. Supply the unique top-up ID that was returned from your previous request, and Stripe will return the corresponding top-up information.
        """
        return cast(
            Topup,
            self._request(
                "get",
                "/v1/topups/{topup}".format(topup=sanitize_id(topup)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        topup: str,
        params: "TopupService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Topup:
        """
        Retrieves the details of a top-up that has previously been created. Supply the unique top-up ID that was returned from your previous request, and Stripe will return the corresponding top-up information.
        """
        return cast(
            Topup,
            await self._request_async(
                "get",
                "/v1/topups/{topup}".format(topup=sanitize_id(topup)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        topup: str,
        params: "TopupService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Topup:
        """
        Updates the metadata of a top-up. Other top-up details are not editable by design.
        """
        return cast(
            Topup,
            self._request(
                "post",
                "/v1/topups/{topup}".format(topup=sanitize_id(topup)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        topup: str,
        params: "TopupService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Topup:
        """
        Updates the metadata of a top-up. Other top-up details are not editable by design.
        """
        return cast(
            Topup,
            await self._request_async(
                "post",
                "/v1/topups/{topup}".format(topup=sanitize_id(topup)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def cancel(
        self,
        topup: str,
        params: "TopupService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Topup:
        """
        Cancels a top-up. Only pending top-ups can be canceled.
        """
        return cast(
            Topup,
            self._request(
                "post",
                "/v1/topups/{topup}/cancel".format(topup=sanitize_id(topup)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        topup: str,
        params: "TopupService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Topup:
        """
        Cancels a top-up. Only pending top-ups can be canceled.
        """
        return cast(
            Topup,
            await self._request_async(
                "post",
                "/v1/topups/{topup}/cancel".format(topup=sanitize_id(topup)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
