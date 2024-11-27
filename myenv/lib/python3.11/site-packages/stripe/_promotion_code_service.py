# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._promotion_code import PromotionCode
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class PromotionCodeService(StripeService):
    class CreateParams(TypedDict):
        active: NotRequired[bool]
        """
        Whether the promotion code is currently active.
        """
        code: NotRequired[str]
        """
        The customer-facing code. Regardless of case, this code must be unique across all active promotion codes for a specific customer. If left blank, we will generate one automatically.
        """
        coupon: str
        """
        The coupon for this promotion code.
        """
        customer: NotRequired[str]
        """
        The customer that this promotion code can be used by. If not set, the promotion code can be used by all customers.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expires_at: NotRequired[int]
        """
        The timestamp at which this promotion code will expire. If the coupon has specified a `redeems_by`, then this value cannot be after the coupon's `redeems_by`.
        """
        max_redemptions: NotRequired[int]
        """
        A positive integer specifying the number of times the promotion code can be redeemed. If the coupon has specified a `max_redemptions`, then this value cannot be greater than the coupon's `max_redemptions`.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        restrictions: NotRequired[
            "PromotionCodeService.CreateParamsRestrictions"
        ]
        """
        Settings that restrict the redemption of the promotion code.
        """

    class CreateParamsRestrictions(TypedDict):
        currency_options: NotRequired[
            Dict[
                str,
                "PromotionCodeService.CreateParamsRestrictionsCurrencyOptions",
            ]
        ]
        """
        Promotion codes defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """
        first_time_transaction: NotRequired[bool]
        """
        A Boolean indicating if the Promotion Code should only be redeemed for Customers without any successful payments or invoices
        """
        minimum_amount: NotRequired[int]
        """
        Minimum amount required to redeem this Promotion Code into a Coupon (e.g., a purchase must be $100 or more to work).
        """
        minimum_amount_currency: NotRequired[str]
        """
        Three-letter [ISO code](https://stripe.com/docs/currencies) for minimum_amount
        """

    class CreateParamsRestrictionsCurrencyOptions(TypedDict):
        minimum_amount: NotRequired[int]
        """
        Minimum amount required to redeem this Promotion Code into a Coupon (e.g., a purchase must be $100 or more to work).
        """

    class ListParams(TypedDict):
        active: NotRequired[bool]
        """
        Filter promotion codes by whether they are active.
        """
        code: NotRequired[str]
        """
        Only return promotion codes that have this case-insensitive code.
        """
        coupon: NotRequired[str]
        """
        Only return promotion codes for this coupon.
        """
        created: NotRequired["PromotionCodeService.ListParamsCreated|int"]
        """
        A filter on the list, based on the object `created` field. The value can be a string with an integer Unix timestamp, or it can be a dictionary with a number of different query options.
        """
        customer: NotRequired[str]
        """
        Only return promotion codes that are restricted to this customer.
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
        Whether the promotion code is currently active. A promotion code can only be reactivated when the coupon is still valid and the promotion code is otherwise redeemable.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        restrictions: NotRequired[
            "PromotionCodeService.UpdateParamsRestrictions"
        ]
        """
        Settings that restrict the redemption of the promotion code.
        """

    class UpdateParamsRestrictions(TypedDict):
        currency_options: NotRequired[
            Dict[
                str,
                "PromotionCodeService.UpdateParamsRestrictionsCurrencyOptions",
            ]
        ]
        """
        Promotion codes defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """

    class UpdateParamsRestrictionsCurrencyOptions(TypedDict):
        minimum_amount: NotRequired[int]
        """
        Minimum amount required to redeem this Promotion Code into a Coupon (e.g., a purchase must be $100 or more to work).
        """

    def list(
        self,
        params: "PromotionCodeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PromotionCode]:
        """
        Returns a list of your promotion codes.
        """
        return cast(
            ListObject[PromotionCode],
            self._request(
                "get",
                "/v1/promotion_codes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "PromotionCodeService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PromotionCode]:
        """
        Returns a list of your promotion codes.
        """
        return cast(
            ListObject[PromotionCode],
            await self._request_async(
                "get",
                "/v1/promotion_codes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "PromotionCodeService.CreateParams",
        options: RequestOptions = {},
    ) -> PromotionCode:
        """
        A promotion code points to a coupon. You can optionally restrict the code to a specific customer, redemption limit, and expiration date.
        """
        return cast(
            PromotionCode,
            self._request(
                "post",
                "/v1/promotion_codes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "PromotionCodeService.CreateParams",
        options: RequestOptions = {},
    ) -> PromotionCode:
        """
        A promotion code points to a coupon. You can optionally restrict the code to a specific customer, redemption limit, and expiration date.
        """
        return cast(
            PromotionCode,
            await self._request_async(
                "post",
                "/v1/promotion_codes",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        promotion_code: str,
        params: "PromotionCodeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PromotionCode:
        """
        Retrieves the promotion code with the given ID. In order to retrieve a promotion code by the customer-facing code use [list](https://stripe.com/docs/api/promotion_codes/list) with the desired code.
        """
        return cast(
            PromotionCode,
            self._request(
                "get",
                "/v1/promotion_codes/{promotion_code}".format(
                    promotion_code=sanitize_id(promotion_code),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        promotion_code: str,
        params: "PromotionCodeService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PromotionCode:
        """
        Retrieves the promotion code with the given ID. In order to retrieve a promotion code by the customer-facing code use [list](https://stripe.com/docs/api/promotion_codes/list) with the desired code.
        """
        return cast(
            PromotionCode,
            await self._request_async(
                "get",
                "/v1/promotion_codes/{promotion_code}".format(
                    promotion_code=sanitize_id(promotion_code),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        promotion_code: str,
        params: "PromotionCodeService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PromotionCode:
        """
        Updates the specified promotion code by setting the values of the parameters passed. Most fields are, by design, not editable.
        """
        return cast(
            PromotionCode,
            self._request(
                "post",
                "/v1/promotion_codes/{promotion_code}".format(
                    promotion_code=sanitize_id(promotion_code),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        promotion_code: str,
        params: "PromotionCodeService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PromotionCode:
        """
        Updates the specified promotion code by setting the values of the parameters passed. Most fields are, by design, not editable.
        """
        return cast(
            PromotionCode,
            await self._request_async(
                "post",
                "/v1/promotion_codes/{promotion_code}".format(
                    promotion_code=sanitize_id(promotion_code),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
