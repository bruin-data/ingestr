# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._shipping_rate import ShippingRate
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class ShippingRateService(StripeService):
    class CreateParams(TypedDict):
        delivery_estimate: NotRequired[
            "ShippingRateService.CreateParamsDeliveryEstimate"
        ]
        """
        The estimated range for how long shipping will take, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        display_name: str
        """
        The name of the shipping rate, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        fixed_amount: NotRequired[
            "ShippingRateService.CreateParamsFixedAmount"
        ]
        """
        Describes a fixed amount to charge for shipping. Must be present if type is `fixed_amount`.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Specifies whether the rate is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`.
        """
        tax_code: NotRequired[str]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID. The Shipping tax code is `txcd_92010001`.
        """
        type: NotRequired[Literal["fixed_amount"]]
        """
        The type of calculation to use on the shipping rate.
        """

    class CreateParamsDeliveryEstimate(TypedDict):
        maximum: NotRequired[
            "ShippingRateService.CreateParamsDeliveryEstimateMaximum"
        ]
        """
        The upper bound of the estimated range. If empty, represents no upper bound i.e., infinite.
        """
        minimum: NotRequired[
            "ShippingRateService.CreateParamsDeliveryEstimateMinimum"
        ]
        """
        The lower bound of the estimated range. If empty, represents no lower bound.
        """

    class CreateParamsDeliveryEstimateMaximum(TypedDict):
        unit: Literal["business_day", "day", "hour", "month", "week"]
        """
        A unit of time.
        """
        value: int
        """
        Must be greater than 0.
        """

    class CreateParamsDeliveryEstimateMinimum(TypedDict):
        unit: Literal["business_day", "day", "hour", "month", "week"]
        """
        A unit of time.
        """
        value: int
        """
        Must be greater than 0.
        """

    class CreateParamsFixedAmount(TypedDict):
        amount: int
        """
        A non-negative integer in cents representing how much to charge.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        currency_options: NotRequired[
            Dict[
                str,
                "ShippingRateService.CreateParamsFixedAmountCurrencyOptions",
            ]
        ]
        """
        Shipping rates defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """

    class CreateParamsFixedAmountCurrencyOptions(TypedDict):
        amount: int
        """
        A non-negative integer in cents representing how much to charge.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Specifies whether the rate is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`.
        """

    class ListParams(TypedDict):
        active: NotRequired[bool]
        """
        Only return shipping rates that are active or inactive.
        """
        created: NotRequired["ShippingRateService.ListParamsCreated|int"]
        """
        A filter on the list, based on the object `created` field. The value can be a string with an integer Unix timestamp, or it can be a dictionary with a number of different query options.
        """
        currency: NotRequired[str]
        """
        Only return shipping rates for the given currency.
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
        active: NotRequired[bool]
        """
        Whether the shipping rate can be used for new purchases. Defaults to `true`.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        fixed_amount: NotRequired[
            "ShippingRateService.UpdateParamsFixedAmount"
        ]
        """
        Describes a fixed amount to charge for shipping. Must be present if type is `fixed_amount`.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Specifies whether the rate is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`.
        """

    class UpdateParamsFixedAmount(TypedDict):
        currency_options: NotRequired[
            Dict[
                str,
                "ShippingRateService.UpdateParamsFixedAmountCurrencyOptions",
            ]
        ]
        """
        Shipping rates defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """

    class UpdateParamsFixedAmountCurrencyOptions(TypedDict):
        amount: NotRequired[int]
        """
        A non-negative integer in cents representing how much to charge.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Specifies whether the rate is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`.
        """

    def list(
        self,
        params: "ShippingRateService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ShippingRate]:
        """
        Returns a list of your shipping rates.
        """
        return cast(
            ListObject[ShippingRate],
            self._request(
                "get",
                "/v1/shipping_rates",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "ShippingRateService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[ShippingRate]:
        """
        Returns a list of your shipping rates.
        """
        return cast(
            ListObject[ShippingRate],
            await self._request_async(
                "get",
                "/v1/shipping_rates",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "ShippingRateService.CreateParams",
        options: RequestOptions = {},
    ) -> ShippingRate:
        """
        Creates a new shipping rate object.
        """
        return cast(
            ShippingRate,
            self._request(
                "post",
                "/v1/shipping_rates",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "ShippingRateService.CreateParams",
        options: RequestOptions = {},
    ) -> ShippingRate:
        """
        Creates a new shipping rate object.
        """
        return cast(
            ShippingRate,
            await self._request_async(
                "post",
                "/v1/shipping_rates",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        shipping_rate_token: str,
        params: "ShippingRateService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ShippingRate:
        """
        Returns the shipping rate object with the given ID.
        """
        return cast(
            ShippingRate,
            self._request(
                "get",
                "/v1/shipping_rates/{shipping_rate_token}".format(
                    shipping_rate_token=sanitize_id(shipping_rate_token),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        shipping_rate_token: str,
        params: "ShippingRateService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> ShippingRate:
        """
        Returns the shipping rate object with the given ID.
        """
        return cast(
            ShippingRate,
            await self._request_async(
                "get",
                "/v1/shipping_rates/{shipping_rate_token}".format(
                    shipping_rate_token=sanitize_id(shipping_rate_token),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        shipping_rate_token: str,
        params: "ShippingRateService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> ShippingRate:
        """
        Updates an existing shipping rate object.
        """
        return cast(
            ShippingRate,
            self._request(
                "post",
                "/v1/shipping_rates/{shipping_rate_token}".format(
                    shipping_rate_token=sanitize_id(shipping_rate_token),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        shipping_rate_token: str,
        params: "ShippingRateService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> ShippingRate:
        """
        Updates an existing shipping rate object.
        """
        return cast(
            ShippingRate,
            await self._request_async(
                "post",
                "/v1/shipping_rates/{shipping_rate_token}".format(
                    shipping_rate_token=sanitize_id(shipping_rate_token),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
