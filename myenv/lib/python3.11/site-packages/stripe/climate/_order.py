# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, Dict, List, Optional, Union, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe.climate._product import Product
    from stripe.climate._supplier import Supplier


class Order(
    CreateableAPIResource["Order"],
    ListableAPIResource["Order"],
    UpdateableAPIResource["Order"],
):
    """
    Orders represent your intent to purchase a particular Climate product. When you create an order, the
    payment is deducted from your merchant balance.
    """

    OBJECT_NAME: ClassVar[Literal["climate.order"]] = "climate.order"

    class Beneficiary(StripeObject):
        public_name: str
        """
        Publicly displayable name for the end beneficiary of carbon removal.
        """

    class DeliveryDetail(StripeObject):
        class Location(StripeObject):
            city: Optional[str]
            """
            The city where the supplier is located.
            """
            country: str
            """
            Two-letter ISO code representing the country where the supplier is located.
            """
            latitude: Optional[float]
            """
            The geographic latitude where the supplier is located.
            """
            longitude: Optional[float]
            """
            The geographic longitude where the supplier is located.
            """
            region: Optional[str]
            """
            The state/county/province/region where the supplier is located.
            """

        delivered_at: int
        """
        Time at which the delivery occurred. Measured in seconds since the Unix epoch.
        """
        location: Optional[Location]
        """
        Specific location of this delivery.
        """
        metric_tons: str
        """
        Quantity of carbon removal supplied by this delivery.
        """
        registry_url: Optional[str]
        """
        Once retired, a URL to the registry entry for the tons from this delivery.
        """
        supplier: "Supplier"
        """
        A supplier of carbon removal.
        """
        _inner_class_types = {"location": Location}

    class CancelParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(RequestOptions):
        amount: NotRequired[int]
        """
        Requested amount of carbon removal units. Either this or `metric_tons` must be specified.
        """
        beneficiary: NotRequired["Order.CreateParamsBeneficiary"]
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

    class ListParams(RequestOptions):
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

    class ModifyParams(RequestOptions):
        beneficiary: NotRequired["Literal['']|Order.ModifyParamsBeneficiary"]
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

    class ModifyParamsBeneficiary(TypedDict):
        public_name: Union[Literal[""], str]
        """
        Publicly displayable name for the end beneficiary of carbon removal.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    amount_fees: int
    """
    Total amount of [Frontier](https://frontierclimate.com/)'s service fees in the currency's smallest unit.
    """
    amount_subtotal: int
    """
    Total amount of the carbon removal in the currency's smallest unit.
    """
    amount_total: int
    """
    Total amount of the order including fees in the currency's smallest unit.
    """
    beneficiary: Optional[Beneficiary]
    canceled_at: Optional[int]
    """
    Time at which the order was canceled. Measured in seconds since the Unix epoch.
    """
    cancellation_reason: Optional[
        Literal["expired", "product_unavailable", "requested"]
    ]
    """
    Reason for the cancellation of this order.
    """
    certificate: Optional[str]
    """
    For delivered orders, a URL to a delivery certificate for the order.
    """
    confirmed_at: Optional[int]
    """
    Time at which the order was confirmed. Measured in seconds since the Unix epoch.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase, representing the currency for this order.
    """
    delayed_at: Optional[int]
    """
    Time at which the order's expected_delivery_year was delayed. Measured in seconds since the Unix epoch.
    """
    delivered_at: Optional[int]
    """
    Time at which the order was delivered. Measured in seconds since the Unix epoch.
    """
    delivery_details: List[DeliveryDetail]
    """
    Details about the delivery of carbon removal for this order.
    """
    expected_delivery_year: int
    """
    The year this order is expected to be delivered.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    metric_tons: str
    """
    Quantity of carbon removal that is included in this order.
    """
    object: Literal["climate.order"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    product: ExpandableField["Product"]
    """
    Unique ID for the Climate `Product` this order is purchasing.
    """
    product_substituted_at: Optional[int]
    """
    Time at which the order's product was substituted for a different product. Measured in seconds since the Unix epoch.
    """
    status: Literal[
        "awaiting_funds", "canceled", "confirmed", "delivered", "open"
    ]
    """
    The current status of this order.
    """

    @classmethod
    def _cls_cancel(
        cls, order: str, **params: Unpack["Order.CancelParams"]
    ) -> "Order":
        """
        Cancels a Climate order. You can cancel an order within 24 hours of creation. Stripe refunds the
        reservation amount_subtotal, but not the amount_fees for user-triggered cancellations. Frontier
        might cancel reservations if suppliers fail to deliver. If Frontier cancels the reservation, Stripe
        provides 90 days advance notice and refunds the amount_total.
        """
        return cast(
            "Order",
            cls._static_request(
                "post",
                "/v1/climate/orders/{order}/cancel".format(
                    order=sanitize_id(order)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def cancel(order: str, **params: Unpack["Order.CancelParams"]) -> "Order":
        """
        Cancels a Climate order. You can cancel an order within 24 hours of creation. Stripe refunds the
        reservation amount_subtotal, but not the amount_fees for user-triggered cancellations. Frontier
        might cancel reservations if suppliers fail to deliver. If Frontier cancels the reservation, Stripe
        provides 90 days advance notice and refunds the amount_total.
        """
        ...

    @overload
    def cancel(self, **params: Unpack["Order.CancelParams"]) -> "Order":
        """
        Cancels a Climate order. You can cancel an order within 24 hours of creation. Stripe refunds the
        reservation amount_subtotal, but not the amount_fees for user-triggered cancellations. Frontier
        might cancel reservations if suppliers fail to deliver. If Frontier cancels the reservation, Stripe
        provides 90 days advance notice and refunds the amount_total.
        """
        ...

    @class_method_variant("_cls_cancel")
    def cancel(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Order.CancelParams"]
    ) -> "Order":
        """
        Cancels a Climate order. You can cancel an order within 24 hours of creation. Stripe refunds the
        reservation amount_subtotal, but not the amount_fees for user-triggered cancellations. Frontier
        might cancel reservations if suppliers fail to deliver. If Frontier cancels the reservation, Stripe
        provides 90 days advance notice and refunds the amount_total.
        """
        return cast(
            "Order",
            self._request(
                "post",
                "/v1/climate/orders/{order}/cancel".format(
                    order=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_cancel_async(
        cls, order: str, **params: Unpack["Order.CancelParams"]
    ) -> "Order":
        """
        Cancels a Climate order. You can cancel an order within 24 hours of creation. Stripe refunds the
        reservation amount_subtotal, but not the amount_fees for user-triggered cancellations. Frontier
        might cancel reservations if suppliers fail to deliver. If Frontier cancels the reservation, Stripe
        provides 90 days advance notice and refunds the amount_total.
        """
        return cast(
            "Order",
            await cls._static_request_async(
                "post",
                "/v1/climate/orders/{order}/cancel".format(
                    order=sanitize_id(order)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def cancel_async(
        order: str, **params: Unpack["Order.CancelParams"]
    ) -> "Order":
        """
        Cancels a Climate order. You can cancel an order within 24 hours of creation. Stripe refunds the
        reservation amount_subtotal, but not the amount_fees for user-triggered cancellations. Frontier
        might cancel reservations if suppliers fail to deliver. If Frontier cancels the reservation, Stripe
        provides 90 days advance notice and refunds the amount_total.
        """
        ...

    @overload
    async def cancel_async(
        self, **params: Unpack["Order.CancelParams"]
    ) -> "Order":
        """
        Cancels a Climate order. You can cancel an order within 24 hours of creation. Stripe refunds the
        reservation amount_subtotal, but not the amount_fees for user-triggered cancellations. Frontier
        might cancel reservations if suppliers fail to deliver. If Frontier cancels the reservation, Stripe
        provides 90 days advance notice and refunds the amount_total.
        """
        ...

    @class_method_variant("_cls_cancel_async")
    async def cancel_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Order.CancelParams"]
    ) -> "Order":
        """
        Cancels a Climate order. You can cancel an order within 24 hours of creation. Stripe refunds the
        reservation amount_subtotal, but not the amount_fees for user-triggered cancellations. Frontier
        might cancel reservations if suppliers fail to deliver. If Frontier cancels the reservation, Stripe
        provides 90 days advance notice and refunds the amount_total.
        """
        return cast(
            "Order",
            await self._request_async(
                "post",
                "/v1/climate/orders/{order}/cancel".format(
                    order=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def create(cls, **params: Unpack["Order.CreateParams"]) -> "Order":
        """
        Creates a Climate order object for a given Climate product. The order will be processed immediately
        after creation and payment will be deducted your Stripe balance.
        """
        return cast(
            "Order",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Order.CreateParams"]
    ) -> "Order":
        """
        Creates a Climate order object for a given Climate product. The order will be processed immediately
        after creation and payment will be deducted your Stripe balance.
        """
        return cast(
            "Order",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(cls, **params: Unpack["Order.ListParams"]) -> ListObject["Order"]:
        """
        Lists all Climate order objects. The orders are returned sorted by creation date, with the
        most recently created orders appearing first.
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
        cls, **params: Unpack["Order.ListParams"]
    ) -> ListObject["Order"]:
        """
        Lists all Climate order objects. The orders are returned sorted by creation date, with the
        most recently created orders appearing first.
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
        cls, id: str, **params: Unpack["Order.ModifyParams"]
    ) -> "Order":
        """
        Updates the specified order by setting the values of the parameters passed.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Order",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Order.ModifyParams"]
    ) -> "Order":
        """
        Updates the specified order by setting the values of the parameters passed.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Order",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Order.RetrieveParams"]
    ) -> "Order":
        """
        Retrieves the details of a Climate order object with the given ID.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Order.RetrieveParams"]
    ) -> "Order":
        """
        Retrieves the details of a Climate order object with the given ID.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {
        "beneficiary": Beneficiary,
        "delivery_details": DeliveryDetail,
    }
