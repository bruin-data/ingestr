# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._application_fee_refund import ApplicationFeeRefund
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class ApplicationFeeRefundService(StripeService):
    class CreateParams(TypedDict):
        amount: NotRequired[int]
        """
        A positive integer, in _cents (or local equivalent)_, representing how much of this fee to refund. Can refund only up to the remaining unrefunded amount of the fee.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
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

    def retrieve(
        self,
        fee: str,
        id: str,
        params: "ApplicationFeeRefundService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ApplicationFeeRefund:
        """
        By default, you can see the 10 most recent refunds stored directly on the application fee object, but you can also retrieve details about a specific refund stored on the application fee.
        """
        return cast(
            ApplicationFeeRefund,
            self._request(
                "get",
                "/v1/application_fees/{fee}/refunds/{id}".format(
                    fee=sanitize_id(fee),
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
        fee: str,
        id: str,
        params: "ApplicationFeeRefundService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ApplicationFeeRefund:
        """
        By default, you can see the 10 most recent refunds stored directly on the application fee object, but you can also retrieve details about a specific refund stored on the application fee.
        """
        return cast(
            ApplicationFeeRefund,
            await self._request_async(
                "get",
                "/v1/application_fees/{fee}/refunds/{id}".format(
                    fee=sanitize_id(fee),
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
        fee: str,
        id: str,
        params: "ApplicationFeeRefundService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> ApplicationFeeRefund:
        """
        Updates the specified application fee refund by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request only accepts metadata as an argument.
        """
        return cast(
            ApplicationFeeRefund,
            self._request(
                "post",
                "/v1/application_fees/{fee}/refunds/{id}".format(
                    fee=sanitize_id(fee),
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
        fee: str,
        id: str,
        params: "ApplicationFeeRefundService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> ApplicationFeeRefund:
        """
        Updates the specified application fee refund by setting the values of the parameters passed. Any parameters not provided will be left unchanged.

        This request only accepts metadata as an argument.
        """
        return cast(
            ApplicationFeeRefund,
            await self._request_async(
                "post",
                "/v1/application_fees/{fee}/refunds/{id}".format(
                    fee=sanitize_id(fee),
                    id=sanitize_id(id),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        id: str,
        params: "ApplicationFeeRefundService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ApplicationFeeRefund]:
        """
        You can see a list of the refunds belonging to a specific application fee. Note that the 10 most recent refunds are always available by default on the application fee object. If you need more than those 10, you can use this API method and the limit and starting_after parameters to page through additional refunds.
        """
        return cast(
            ListObject[ApplicationFeeRefund],
            self._request(
                "get",
                "/v1/application_fees/{id}/refunds".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        id: str,
        params: "ApplicationFeeRefundService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ApplicationFeeRefund]:
        """
        You can see a list of the refunds belonging to a specific application fee. Note that the 10 most recent refunds are always available by default on the application fee object. If you need more than those 10, you can use this API method and the limit and starting_after parameters to page through additional refunds.
        """
        return cast(
            ListObject[ApplicationFeeRefund],
            await self._request_async(
                "get",
                "/v1/application_fees/{id}/refunds".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        id: str,
        params: "ApplicationFeeRefundService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> ApplicationFeeRefund:
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        return cast(
            ApplicationFeeRefund,
            self._request(
                "post",
                "/v1/application_fees/{id}/refunds".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        id: str,
        params: "ApplicationFeeRefundService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> ApplicationFeeRefund:
        """
        Refunds an application fee that has previously been collected but not yet refunded.
        Funds will be refunded to the Stripe account from which the fee was originally collected.

        You can optionally refund only part of an application fee.
        You can do so multiple times, until the entire fee has been refunded.

        Once entirely refunded, an application fee can't be refunded again.
        This method will raise an error when called on an already-refunded application fee,
        or when trying to refund more money than is left on an application fee.
        """
        return cast(
            ApplicationFeeRefund,
            await self._request_async(
                "post",
                "/v1/application_fees/{id}/refunds".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
