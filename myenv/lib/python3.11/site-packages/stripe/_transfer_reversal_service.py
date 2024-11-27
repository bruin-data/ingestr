# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._reversal import Reversal
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class TransferReversalService(StripeService):
    class CreateParams(TypedDict):
        amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) representing how much of this transfer to reverse. Can only reverse up to the unreversed amount remaining of the transfer. Partial transfer reversals are only allowed for transfers to Stripe Accounts. Defaults to the entire transfer amount.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to a reversal object. This will be unset if you POST an empty value.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        refund_application_fee: NotRequired[bool]
        """
        Boolean indicating whether the application fee should be refunded when reversing this transfer. If a full transfer reversal is given, the full application fee will be refunded. Otherwise, the application fee will be refunded with an amount proportional to the amount of the transfer reversed.
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
        id: str,
        params: "TransferReversalService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Reversal]:
        """
        You can see a list of the reversals belonging to a specific transfer. Note that the 10 most recent reversals are always available by default on the transfer object. If you need more than those 10, you can use this API method and the limit and starting_after parameters to page through additional reversals.
        """
        return cast(
            ListObject[Reversal],
            self._request(
                "get",
                "/v1/transfers/{id}/reversals".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        id: str,
        params: "TransferReversalService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Reversal]:
        """
        You can see a list of the reversals belonging to a specific transfer. Note that the 10 most recent reversals are always available by default on the transfer object. If you need more than those 10, you can use this API method and the limit and starting_after parameters to page through additional reversals.
        """
        return cast(
            ListObject[Reversal],
            await self._request_async(
                "get",
                "/v1/transfers/{id}/reversals".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        id: str,
        params: "TransferReversalService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Reversal:
        """
        When you create a new reversal, you must specify a transfer to create it on.

        When reversing transfers, you can optionally reverse part of the transfer. You can do so as many times as you wish until the entire transfer has been reversed.

        Once entirely reversed, a transfer can't be reversed again. This method will return an error when called on an already-reversed transfer, or when trying to reverse more money than is left on a transfer.
        """
        return cast(
            Reversal,
            self._request(
                "post",
                "/v1/transfers/{id}/reversals".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        id: str,
        params: "TransferReversalService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Reversal:
        """
        When you create a new reversal, you must specify a transfer to create it on.

        When reversing transfers, you can optionally reverse part of the transfer. You can do so as many times as you wish until the entire transfer has been reversed.

        Once entirely reversed, a transfer can't be reversed again. This method will return an error when called on an already-reversed transfer, or when trying to reverse more money than is left on a transfer.
        """
        return cast(
            Reversal,
            await self._request_async(
                "post",
                "/v1/transfers/{id}/reversals".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        transfer: str,
        id: str,
        params: "TransferReversalService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Reversal:
        """
        By default, you can see the 10 most recent reversals stored directly on the transfer object, but you can also retrieve details about a specific reversal stored on the transfer.
        """
        return cast(
            Reversal,
            self._request(
                "get",
                "/v1/transfers/{transfer}/reversals/{id}".format(
                    transfer=sanitize_id(transfer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        transfer: str,
        id: str,
        params: "TransferReversalService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Reversal:
        """
        By default, you can see the 10 most recent reversals stored directly on the transfer object, but you can also retrieve details about a specific reversal stored on the transfer.
        """
        return cast(
            Reversal,
            await self._request_async(
                "get",
                "/v1/transfers/{transfer}/reversals/{id}".format(
                    transfer=sanitize_id(transfer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        transfer: str,
        id: str,
        params: "TransferReversalService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Reversal:
        """
        Updates the specified reversal by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request only accepts metadata and description as arguments.
        """
        return cast(
            Reversal,
            self._request(
                "post",
                "/v1/transfers/{transfer}/reversals/{id}".format(
                    transfer=sanitize_id(transfer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        transfer: str,
        id: str,
        params: "TransferReversalService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Reversal:
        """
        Updates the specified reversal by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request only accepts metadata and description as arguments.
        """
        return cast(
            Reversal,
            await self._request_async(
                "post",
                "/v1/transfers/{transfer}/reversals/{id}".format(
                    transfer=sanitize_id(transfer),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
