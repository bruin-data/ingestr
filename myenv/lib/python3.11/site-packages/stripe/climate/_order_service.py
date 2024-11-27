# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.climate._order import Order
from typing import Dict, List, Union, cast
from typing_extensions import Literal, NotRequired, TypedDict


class OrderService(StripeService):
    class CancelParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(TypedDict):
        amount: NotRequired[int]
        """
        Requested amount of carbon removal units. Either this or `metric_tons` must be specified.
        """
        beneficiary: NotRequired["OrderService.CreateParamsBeneficiary"]
        """
        Publicly sharable reference for the end beneficiary of carbon removal. Assumed to be the Stripe account if not set.
        """
        currency: NotRequired[str]
        """
        Request currency for the order as a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a supported [settlement currency for your account](https://stripe.com/docs/currencies). If omitted, the account's default currency will be used.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        metric_tons: NotRequired[str]
        """
        Requested number of tons for the order. Either this or `amount` must be specified.
        """
        product: str
        """
        Unique identifier of the Climate product.
        """

    class CreateParamsBeneficiary(TypedDict):
        public_name: str
        """
        Publicly displayable name for the end beneficiary of carbon removal.
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
        beneficiary: NotRequired[
            "Literal['']|OrderService.UpdateParamsBeneficiary"
        ]
        """
        Publicly sharable reference for the end beneficiary of carbon removal. Assumed to be the Stripe account if not set.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class UpdateParamsBeneficiary(TypedDict):
        public_name: Union[Literal[""], str]
        """
        Publicly displayable name for the end beneficiary of carbon removal.
        """

    def list(
        self,
        params: "OrderService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Order]:
        """
        Lists all Climate order objects. The orders are returned sorted by creation date, with the
        most recently created orders appearing first.
        """
        return cast(
            ListObject[Order],
            self._request(
                "get",
                "/v1/climate/orders",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "OrderService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Order]:
        """
        Lists all Climate order objects. The orders are returned sorted by creation date, with the
        most recently created orders appearing first.
        """
        return cast(
            ListObject[Order],
            await self._request_async(
                "get",
                "/v1/climate/orders",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self, params: "OrderService.CreateParams", options: RequestOptions = {}
    ) -> Order:
        """
        Creates a Climate order object for a given Climate product. The order will be processed immediately
        after creation and payment will be deducted your Stripe balance.
        """
        return cast(
            Order,
            self._request(
                "post",
                "/v1/climate/orders",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self, params: "OrderService.CreateParams", options: RequestOptions = {}
    ) -> Order:
        """
        Creates a Climate order object for a given Climate product. The order will be processed immediately
        after creation and payment will be deducted your Stripe balance.
        """
        return cast(
            Order,
            await self._request_async(
                "post",
                "/v1/climate/orders",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        order: str,
        params: "OrderService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Order:
        """
        Retrieves the details of a Climate order object with the given ID.
        """
        return cast(
            Order,
            self._request(
                "get",
                "/v1/climate/orders/{order}".format(order=sanitize_id(order)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        order: str,
        params: "OrderService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Order:
        """
        Retrieves the details of a Climate order object with the given ID.
        """
        return cast(
            Order,
            await self._request_async(
                "get",
                "/v1/climate/orders/{order}".format(order=sanitize_id(order)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        order: str,
        params: "OrderService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Order:
        """
        Updates the specified order by setting the values of the parameters passed.
        """
        return cast(
            Order,
            self._request(
                "post",
                "/v1/climate/orders/{order}".format(order=sanitize_id(order)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        order: str,
        params: "OrderService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Order:
        """
        Updates the specified order by setting the values of the parameters passed.
        """
        return cast(
            Order,
            await self._request_async(
                "post",
                "/v1/climate/orders/{order}".format(order=sanitize_id(order)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def cancel(
        self,
        order: str,
        params: "OrderService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Order:
        """
        Cancels a Climate order. You can cancel an order within 24 hours of creation. Stripe refunds the
        reservation amount_subtotal, but not the amount_fees for user-triggered cancellations. Frontier
        might cancel reservations if suppliers fail to deliver. If Frontier cancels the reservation, Stripe
        provides 90 days advance notice and refunds the amount_total.
        """
        return cast(
            Order,
            self._request(
                "post",
                "/v1/climate/orders/{order}/cancel".format(
                    order=sanitize_id(order),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        order: str,
        params: "OrderService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Order:
        """
        Cancels a Climate order. You can cancel an order within 24 hours of creation. Stripe refunds the
        reservation amount_subtotal, but not the amount_fees for user-triggered cancellations. Frontier
        might cancel reservations if suppliers fail to deliver. If Frontier cancels the reservation, Stripe
        provides 90 days advance notice and refunds the amount_total.
        """
        return cast(
            Order,
            await self._request_async(
                "post",
                "/v1/climate/orders/{order}/cancel".format(
                    order=sanitize_id(order),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
