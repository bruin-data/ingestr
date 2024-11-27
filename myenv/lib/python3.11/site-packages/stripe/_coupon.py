# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._deletable_api_resource import DeletableAPIResource
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, Dict, List, Optional, cast, overload
from typing_extensions import Literal, NotRequired, TypedDict, Unpack


class Coupon(
    CreateableAPIResource["Coupon"],
    DeletableAPIResource["Coupon"],
    ListableAPIResource["Coupon"],
    UpdateableAPIResource["Coupon"],
):
    """
    A coupon contains information about a percent-off or amount-off discount you
    might want to apply to a customer. Coupons may be applied to [subscriptions](https://stripe.com/docs/api#subscriptions), [invoices](https://stripe.com/docs/api#invoices),
    [checkout sessions](https://stripe.com/docs/api/checkout/sessions), [quotes](https://stripe.com/docs/api#quotes), and more. Coupons do not work with conventional one-off [charges](https://stripe.com/docs/api#create_charge) or [payment intents](https://stripe.com/docs/api/payment_intents).
    """

    OBJECT_NAME: ClassVar[Literal["coupon"]] = "coupon"

    class AppliesTo(StripeObject):
        products: List[str]
        """
        A list of product IDs this coupon applies to
        """

    class CurrencyOptions(StripeObject):
        amount_off: int
        """
        Amount (in the `currency` specified) that will be taken off the subtotal of any invoices for this customer.
        """

    class CreateParams(RequestOptions):
        amount_off: NotRequired[int]
        """
        A positive integer representing the amount to subtract from an invoice total (required if `percent_off` is not passed).
        """
        applies_to: NotRequired["Coupon.CreateParamsAppliesTo"]
        """
        A hash containing directions for what this Coupon will apply discounts to.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO code for the currency](https://stripe.com/docs/currencies) of the `amount_off` parameter (required if `amount_off` is passed).
        """
        currency_options: NotRequired[
            Dict[str, "Coupon.CreateParamsCurrencyOptions"]
        ]
        """
        Coupons defined in each available currency option (only supported if `amount_off` is passed). Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """
        duration: NotRequired[Literal["forever", "once", "repeating"]]
        """
        Specifies how long the discount will be in effect if used on a subscription. Defaults to `once`.
        """
        duration_in_months: NotRequired[int]
        """
        Required only if `duration` is `repeating`, in which case it must be a positive integer that specifies the number of months the discount will be in effect.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        id: NotRequired[str]
        """
        Unique string of your choice that will be used to identify this coupon when applying it to a customer. If you don't want to specify a particular code, you can leave the ID blank and we'll generate a random code for you.
        """
        max_redemptions: NotRequired[int]
        """
        A positive integer specifying the number of times the coupon can be redeemed before it's no longer valid. For example, you might have a 50% off coupon that the first 20 readers of your blog can use.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: NotRequired[str]
        """
        Name of the coupon displayed to customers on, for instance invoices, or receipts. By default the `id` is shown if `name` is not set.
        """
        percent_off: NotRequired[float]
        """
        A positive float larger than 0, and smaller or equal to 100, that represents the discount the coupon will apply (required if `amount_off` is not passed).
        """
        redeem_by: NotRequired[int]
        """
        Unix timestamp specifying the last time at which the coupon can be redeemed. After the redeem_by date, the coupon can no longer be applied to new customers.
        """

    class CreateParamsAppliesTo(TypedDict):
        products: NotRequired[List[str]]
        """
        An array of Product IDs that this Coupon will apply to.
        """

    class CreateParamsCurrencyOptions(TypedDict):
        amount_off: int
        """
        A positive integer representing the amount to subtract from an invoice total.
        """

    class DeleteParams(RequestOptions):
        pass

    class ListParams(RequestOptions):
        created: NotRequired["Coupon.ListParamsCreated|int"]
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

    class ModifyParams(RequestOptions):
        currency_options: NotRequired[
            Dict[str, "Coupon.ModifyParamsCurrencyOptions"]
        ]
        """
        Coupons defined in each available currency option (only supported if the coupon is amount-based). Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: NotRequired[str]
        """
        Name of the coupon displayed to customers on, for instance invoices, or receipts. By default the `id` is shown if `name` is not set.
        """

    class ModifyParamsCurrencyOptions(TypedDict):
        amount_off: int
        """
        A positive integer representing the amount to subtract from an invoice total.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    amount_off: Optional[int]
    """
    Amount (in the `currency` specified) that will be taken off the subtotal of any invoices for this customer.
    """
    applies_to: Optional[AppliesTo]
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: Optional[str]
    """
    If `amount_off` has been set, the three-letter [ISO code for the currency](https://stripe.com/docs/currencies) of the amount to take off.
    """
    currency_options: Optional[Dict[str, CurrencyOptions]]
    """
    Coupons defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
    """
    duration: Literal["forever", "once", "repeating"]
    """
    One of `forever`, `once`, and `repeating`. Describes how long a customer who applies this coupon will get the discount.
    """
    duration_in_months: Optional[int]
    """
    If `duration` is `repeating`, the number of months the coupon applies. Null if coupon `duration` is `forever` or `once`.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    max_redemptions: Optional[int]
    """
    Maximum number of times this coupon can be redeemed, in total, across all customers, before it is no longer valid.
    """
    metadata: Optional[Dict[str, str]]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    name: Optional[str]
    """
    Name of the coupon displayed to customers on for instance invoices or receipts.
    """
    object: Literal["coupon"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    percent_off: Optional[float]
    """
    Percent that will be taken off the subtotal of any invoices for this customer for the duration of the coupon. For example, a coupon with percent_off of 50 will make a $ (or local equivalent)100 invoice $ (or local equivalent)50 instead.
    """
    redeem_by: Optional[int]
    """
    Date after which the coupon can no longer be redeemed.
    """
    times_redeemed: int
    """
    Number of times this coupon has been applied to a customer.
    """
    valid: bool
    """
    Taking account of the above properties, whether this coupon can still be applied to a customer.
    """
    deleted: Optional[Literal[True]]
    """
    Always true for a deleted object
    """

    @classmethod
    def create(cls, **params: Unpack["Coupon.CreateParams"]) -> "Coupon":
        """
        You can create coupons easily via the [coupon management](https://dashboard.stripe.com/coupons) page of the Stripe dashboard. Coupon creation is also accessible via the API if you need to create coupons on the fly.

        A coupon has either a percent_off or an amount_off and currency. If you set an amount_off, that amount will be subtracted from any invoice's subtotal. For example, an invoice with a subtotal of 100 will have a final total of 0 if a coupon with an amount_off of 200 is applied to it and an invoice with a subtotal of 300 will have a final total of 100 if a coupon with an amount_off of 200 is applied to it.
        """
        return cast(
            "Coupon",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Coupon.CreateParams"]
    ) -> "Coupon":
        """
        You can create coupons easily via the [coupon management](https://dashboard.stripe.com/coupons) page of the Stripe dashboard. Coupon creation is also accessible via the API if you need to create coupons on the fly.

        A coupon has either a percent_off or an amount_off and currency. If you set an amount_off, that amount will be subtracted from any invoice's subtotal. For example, an invoice with a subtotal of 100 will have a final total of 0 if a coupon with an amount_off of 200 is applied to it and an invoice with a subtotal of 300 will have a final total of 100 if a coupon with an amount_off of 200 is applied to it.
        """
        return cast(
            "Coupon",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_delete(
        cls, sid: str, **params: Unpack["Coupon.DeleteParams"]
    ) -> "Coupon":
        """
        You can delete coupons via the [coupon management](https://dashboard.stripe.com/coupons) page of the Stripe dashboard. However, deleting a coupon does not affect any customers who have already applied the coupon; it means that new customers can't redeem the coupon. You can also delete coupons via the API.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "Coupon",
            cls._static_request(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    def delete(sid: str, **params: Unpack["Coupon.DeleteParams"]) -> "Coupon":
        """
        You can delete coupons via the [coupon management](https://dashboard.stripe.com/coupons) page of the Stripe dashboard. However, deleting a coupon does not affect any customers who have already applied the coupon; it means that new customers can't redeem the coupon. You can also delete coupons via the API.
        """
        ...

    @overload
    def delete(self, **params: Unpack["Coupon.DeleteParams"]) -> "Coupon":
        """
        You can delete coupons via the [coupon management](https://dashboard.stripe.com/coupons) page of the Stripe dashboard. However, deleting a coupon does not affect any customers who have already applied the coupon; it means that new customers can't redeem the coupon. You can also delete coupons via the API.
        """
        ...

    @class_method_variant("_cls_delete")
    def delete(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Coupon.DeleteParams"]
    ) -> "Coupon":
        """
        You can delete coupons via the [coupon management](https://dashboard.stripe.com/coupons) page of the Stripe dashboard. However, deleting a coupon does not affect any customers who have already applied the coupon; it means that new customers can't redeem the coupon. You can also delete coupons via the API.
        """
        return self._request_and_refresh(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    async def _cls_delete_async(
        cls, sid: str, **params: Unpack["Coupon.DeleteParams"]
    ) -> "Coupon":
        """
        You can delete coupons via the [coupon management](https://dashboard.stripe.com/coupons) page of the Stripe dashboard. However, deleting a coupon does not affect any customers who have already applied the coupon; it means that new customers can't redeem the coupon. You can also delete coupons via the API.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "Coupon",
            await cls._static_request_async(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def delete_async(
        sid: str, **params: Unpack["Coupon.DeleteParams"]
    ) -> "Coupon":
        """
        You can delete coupons via the [coupon management](https://dashboard.stripe.com/coupons) page of the Stripe dashboard. However, deleting a coupon does not affect any customers who have already applied the coupon; it means that new customers can't redeem the coupon. You can also delete coupons via the API.
        """
        ...

    @overload
    async def delete_async(
        self, **params: Unpack["Coupon.DeleteParams"]
    ) -> "Coupon":
        """
        You can delete coupons via the [coupon management](https://dashboard.stripe.com/coupons) page of the Stripe dashboard. However, deleting a coupon does not affect any customers who have already applied the coupon; it means that new customers can't redeem the coupon. You can also delete coupons via the API.
        """
        ...

    @class_method_variant("_cls_delete_async")
    async def delete_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Coupon.DeleteParams"]
    ) -> "Coupon":
        """
        You can delete coupons via the [coupon management](https://dashboard.stripe.com/coupons) page of the Stripe dashboard. However, deleting a coupon does not affect any customers who have already applied the coupon; it means that new customers can't redeem the coupon. You can also delete coupons via the API.
        """
        return await self._request_and_refresh_async(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    def list(
        cls, **params: Unpack["Coupon.ListParams"]
    ) -> ListObject["Coupon"]:
        """
        Returns a list of your coupons.
        """
        result = cls._static_request(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    async def list_async(
        cls, **params: Unpack["Coupon.ListParams"]
    ) -> ListObject["Coupon"]:
        """
        Returns a list of your coupons.
        """
        result = await cls._static_request_async(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    def modify(
        cls, id: str, **params: Unpack["Coupon.ModifyParams"]
    ) -> "Coupon":
        """
        Updates the metadata of a coupon. Other coupon details (currency, duration, amount_off) are, by design, not editable.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Coupon",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Coupon.ModifyParams"]
    ) -> "Coupon":
        """
        Updates the metadata of a coupon. Other coupon details (currency, duration, amount_off) are, by design, not editable.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Coupon",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Coupon.RetrieveParams"]
    ) -> "Coupon":
        """
        Retrieves the coupon with the given ID.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Coupon.RetrieveParams"]
    ) -> "Coupon":
        """
        Retrieves the coupon with the given ID.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {
        "applies_to": AppliesTo,
        "currency_options": CurrencyOptions,
    }
