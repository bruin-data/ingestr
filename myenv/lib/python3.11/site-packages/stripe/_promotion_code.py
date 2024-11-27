# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import sanitize_id
from typing import ClassVar, Dict, List, Optional, cast
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._coupon import Coupon
    from stripe._customer import Customer


class PromotionCode(
    CreateableAPIResource["PromotionCode"],
    ListableAPIResource["PromotionCode"],
    UpdateableAPIResource["PromotionCode"],
):
    """
    A Promotion Code represents a customer-redeemable code for a [coupon](https://stripe.com/docs/api#coupons). It can be used to
    create multiple codes for a single coupon.
    """

    OBJECT_NAME: ClassVar[Literal["promotion_code"]] = "promotion_code"

    class Restrictions(StripeObject):
        class CurrencyOptions(StripeObject):
            minimum_amount: int
            """
            Minimum amount required to redeem this Promotion Code into a Coupon (e.g., a purchase must be $100 or more to work).
            """

        currency_options: Optional[Dict[str, CurrencyOptions]]
        """
        Promotion code restrictions defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """
        first_time_transaction: bool
        """
        A Boolean indicating if the Promotion Code should only be redeemed for Customers without any successful payments or invoices
        """
        minimum_amount: Optional[int]
        """
        Minimum amount required to redeem this Promotion Code into a Coupon (e.g., a purchase must be $100 or more to work).
        """
        minimum_amount_currency: Optional[str]
        """
        Three-letter [ISO code](https://stripe.com/docs/currencies) for minimum_amount
        """
        _inner_class_types = {"currency_options": CurrencyOptions}
        _inner_class_dicts = ["currency_options"]

    class CreateParams(RequestOptions):
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
        restrictions: NotRequired["PromotionCode.CreateParamsRestrictions"]
        """
        Settings that restrict the redemption of the promotion code.
        """

    class CreateParamsRestrictions(TypedDict):
        currency_options: NotRequired[
            Dict[str, "PromotionCode.CreateParamsRestrictionsCurrencyOptions"]
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

    class ListParams(RequestOptions):
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
        created: NotRequired["PromotionCode.ListParamsCreated|int"]
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

    class ModifyParams(RequestOptions):
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
        restrictions: NotRequired["PromotionCode.ModifyParamsRestrictions"]
        """
        Settings that restrict the redemption of the promotion code.
        """

    class ModifyParamsRestrictions(TypedDict):
        currency_options: NotRequired[
            Dict[str, "PromotionCode.ModifyParamsRestrictionsCurrencyOptions"]
        ]
        """
        Promotion codes defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """

    class ModifyParamsRestrictionsCurrencyOptions(TypedDict):
        minimum_amount: NotRequired[int]
        """
        Minimum amount required to redeem this Promotion Code into a Coupon (e.g., a purchase must be $100 or more to work).
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    active: bool
    """
    Whether the promotion code is currently active. A promotion code is only active if the coupon is also valid.
    """
    code: str
    """
    The customer-facing code. Regardless of case, this code must be unique across all active promotion codes for each customer.
    """
    coupon: "Coupon"
    """
    A coupon contains information about a percent-off or amount-off discount you
    might want to apply to a customer. Coupons may be applied to [subscriptions](https://stripe.com/docs/api#subscriptions), [invoices](https://stripe.com/docs/api#invoices),
    [checkout sessions](https://stripe.com/docs/api/checkout/sessions), [quotes](https://stripe.com/docs/api#quotes), and more. Coupons do not work with conventional one-off [charges](https://stripe.com/docs/api#create_charge) or [payment intents](https://stripe.com/docs/api/payment_intents).
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    customer: Optional[ExpandableField["Customer"]]
    """
    The customer that this promotion code can be used by.
    """
    expires_at: Optional[int]
    """
    Date at which the promotion code can no longer be redeemed.
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
    Maximum number of times this promotion code can be redeemed.
    """
    metadata: Optional[Dict[str, str]]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    object: Literal["promotion_code"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    restrictions: Restrictions
    times_redeemed: int
    """
    Number of times this promotion code has been used.
    """

    @classmethod
    def create(
        cls, **params: Unpack["PromotionCode.CreateParams"]
    ) -> "PromotionCode":
        """
        A promotion code points to a coupon. You can optionally restrict the code to a specific customer, redemption limit, and expiration date.
        """
        return cast(
            "PromotionCode",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["PromotionCode.CreateParams"]
    ) -> "PromotionCode":
        """
        A promotion code points to a coupon. You can optionally restrict the code to a specific customer, redemption limit, and expiration date.
        """
        return cast(
            "PromotionCode",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["PromotionCode.ListParams"]
    ) -> ListObject["PromotionCode"]:
        """
        Returns a list of your promotion codes.
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
        cls, **params: Unpack["PromotionCode.ListParams"]
    ) -> ListObject["PromotionCode"]:
        """
        Returns a list of your promotion codes.
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
        cls, id: str, **params: Unpack["PromotionCode.ModifyParams"]
    ) -> "PromotionCode":
        """
        Updates the specified promotion code by setting the values of the parameters passed. Most fields are, by design, not editable.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "PromotionCode",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["PromotionCode.ModifyParams"]
    ) -> "PromotionCode":
        """
        Updates the specified promotion code by setting the values of the parameters passed. Most fields are, by design, not editable.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "PromotionCode",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["PromotionCode.RetrieveParams"]
    ) -> "PromotionCode":
        """
        Retrieves the promotion code with the given ID. In order to retrieve a promotion code by the customer-facing code use [list](https://stripe.com/docs/api/promotion_codes/list) with the desired code.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["PromotionCode.RetrieveParams"]
    ) -> "PromotionCode":
        """
        Retrieves the promotion code with the given ID. In order to retrieve a promotion code by the customer-facing code use [list](https://stripe.com/docs/api/promotion_codes/list) with the desired code.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {"restrictions": Restrictions}
