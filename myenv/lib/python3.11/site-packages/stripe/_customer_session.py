# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from typing import ClassVar, List, Optional, cast
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._customer import Customer


class CustomerSession(CreateableAPIResource["CustomerSession"]):
    """
    A Customer Session allows you to grant Stripe's frontend SDKs (like Stripe.js) client-side access
    control over a Customer.
    """

    OBJECT_NAME: ClassVar[Literal["customer_session"]] = "customer_session"

    class Components(StripeObject):
        class BuyButton(StripeObject):
            enabled: bool
            """
            Whether the buy button is enabled.
            """

        class PaymentElement(StripeObject):
            class Features(StripeObject):
                payment_method_allow_redisplay_filters: List[
                    Literal["always", "limited", "unspecified"]
                ]
                """
                A list of [`allow_redisplay`](https://docs.stripe.com/api/payment_methods/object#payment_method_object-allow_redisplay) values that controls which saved payment methods the Payment Element displays by filtering to only show payment methods with an `allow_redisplay` value that is present in this list.

                If not specified, defaults to ["always"]. In order to display all saved payment methods, specify ["always", "limited", "unspecified"].
                """
                payment_method_redisplay: Literal["disabled", "enabled"]
                """
                Controls whether or not the Payment Element shows saved payment methods. This parameter defaults to `disabled`.
                """
                payment_method_redisplay_limit: Optional[int]
                """
                Determines the max number of saved payment methods for the Payment Element to display. This parameter defaults to `3`.
                """
                payment_method_remove: Literal["disabled", "enabled"]
                """
                Controls whether the Payment Element displays the option to remove a saved payment method. This parameter defaults to `disabled`.

                Allowing buyers to remove their saved payment methods impacts subscriptions that depend on that payment method. Removing the payment method detaches the [`customer` object](https://docs.stripe.com/api/payment_methods/object#payment_method_object-customer) from that [PaymentMethod](https://docs.stripe.com/api/payment_methods).
                """
                payment_method_save: Literal["disabled", "enabled"]
                """
                Controls whether the Payment Element displays a checkbox offering to save a new payment method. This parameter defaults to `disabled`.

                If a customer checks the box, the [`allow_redisplay`](https://docs.stripe.com/api/payment_methods/object#payment_method_object-allow_redisplay) value on the PaymentMethod is set to `'always'` at confirmation time. For PaymentIntents, the [`setup_future_usage`](https://docs.stripe.com/api/payment_intents/object#payment_intent_object-setup_future_usage) value is also set to the value defined in `payment_method_save_usage`.
                """
                payment_method_save_usage: Optional[
                    Literal["off_session", "on_session"]
                ]
                """
                When using PaymentIntents and the customer checks the save checkbox, this field determines the [`setup_future_usage`](https://docs.stripe.com/api/payment_intents/object#payment_intent_object-setup_future_usage) value used to confirm the PaymentIntent.

                When using SetupIntents, directly configure the [`usage`](https://docs.stripe.com/api/setup_intents/object#setup_intent_object-usage) value on SetupIntent creation.
                """

            enabled: bool
            """
            Whether the Payment Element is enabled.
            """
            features: Optional[Features]
            """
            This hash defines whether the Payment Element supports certain features.
            """
            _inner_class_types = {"features": Features}

        class PricingTable(StripeObject):
            enabled: bool
            """
            Whether the pricing table is enabled.
            """

        buy_button: BuyButton
        """
        This hash contains whether the buy button is enabled.
        """
        payment_element: PaymentElement
        """
        This hash contains whether the Payment Element is enabled and the features it supports.
        """
        pricing_table: PricingTable
        """
        This hash contains whether the pricing table is enabled.
        """
        _inner_class_types = {
            "buy_button": BuyButton,
            "payment_element": PaymentElement,
            "pricing_table": PricingTable,
        }

    class CreateParams(RequestOptions):
        components: "CustomerSession.CreateParamsComponents"
        """
        Configuration for each component. Exactly 1 component must be enabled.
        """
        customer: str
        """
        The ID of an existing customer for which to create the Customer Session.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParamsComponents(TypedDict):
        buy_button: NotRequired[
            "CustomerSession.CreateParamsComponentsBuyButton"
        ]
        """
        Configuration for buy button.
        """
        payment_element: NotRequired[
            "CustomerSession.CreateParamsComponentsPaymentElement"
        ]
        """
        Configuration for the Payment Element.
        """
        pricing_table: NotRequired[
            "CustomerSession.CreateParamsComponentsPricingTable"
        ]
        """
        Configuration for the pricing table.
        """

    class CreateParamsComponentsBuyButton(TypedDict):
        enabled: bool
        """
        Whether the buy button is enabled.
        """

    class CreateParamsComponentsPaymentElement(TypedDict):
        enabled: bool
        """
        Whether the Payment Element is enabled.
        """
        features: NotRequired[
            "CustomerSession.CreateParamsComponentsPaymentElementFeatures"
        ]
        """
        This hash defines whether the Payment Element supports certain features.
        """

    class CreateParamsComponentsPaymentElementFeatures(TypedDict):
        payment_method_allow_redisplay_filters: NotRequired[
            List[Literal["always", "limited", "unspecified"]]
        ]
        """
        A list of [`allow_redisplay`](https://docs.stripe.com/api/payment_methods/object#payment_method_object-allow_redisplay) values that controls which saved payment methods the Payment Element displays by filtering to only show payment methods with an `allow_redisplay` value that is present in this list.

        If not specified, defaults to ["always"]. In order to display all saved payment methods, specify ["always", "limited", "unspecified"].
        """
        payment_method_redisplay: NotRequired[Literal["disabled", "enabled"]]
        """
        Controls whether or not the Payment Element shows saved payment methods. This parameter defaults to `disabled`.
        """
        payment_method_redisplay_limit: NotRequired[int]
        """
        Determines the max number of saved payment methods for the Payment Element to display. This parameter defaults to `3`.
        """
        payment_method_remove: NotRequired[Literal["disabled", "enabled"]]
        """
        Controls whether the Payment Element displays the option to remove a saved payment method. This parameter defaults to `disabled`.

        Allowing buyers to remove their saved payment methods impacts subscriptions that depend on that payment method. Removing the payment method detaches the [`customer` object](https://docs.stripe.com/api/payment_methods/object#payment_method_object-customer) from that [PaymentMethod](https://docs.stripe.com/api/payment_methods).
        """
        payment_method_save: NotRequired[Literal["disabled", "enabled"]]
        """
        Controls whether the Payment Element displays a checkbox offering to save a new payment method. This parameter defaults to `disabled`.

        If a customer checks the box, the [`allow_redisplay`](https://docs.stripe.com/api/payment_methods/object#payment_method_object-allow_redisplay) value on the PaymentMethod is set to `'always'` at confirmation time. For PaymentIntents, the [`setup_future_usage`](https://docs.stripe.com/api/payment_intents/object#payment_intent_object-setup_future_usage) value is also set to the value defined in `payment_method_save_usage`.
        """
        payment_method_save_usage: NotRequired[
            Literal["off_session", "on_session"]
        ]
        """
        When using PaymentIntents and the customer checks the save checkbox, this field determines the [`setup_future_usage`](https://docs.stripe.com/api/payment_intents/object#payment_intent_object-setup_future_usage) value used to confirm the PaymentIntent.

        When using SetupIntents, directly configure the [`usage`](https://docs.stripe.com/api/setup_intents/object#setup_intent_object-usage) value on SetupIntent creation.
        """

    class CreateParamsComponentsPricingTable(TypedDict):
        enabled: bool
        """
        Whether the pricing table is enabled.
        """

    client_secret: str
    """
    The client secret of this Customer Session. Used on the client to set up secure access to the given `customer`.

    The client secret can be used to provide access to `customer` from your frontend. It should not be stored, logged, or exposed to anyone other than the relevant customer. Make sure that you have TLS enabled on any page that includes the client secret.
    """
    components: Optional[Components]
    """
    Configuration for the components supported by this Customer Session.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    customer: ExpandableField["Customer"]
    """
    The Customer the Customer Session was created for.
    """
    expires_at: int
    """
    The timestamp at which this Customer Session will expire.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["customer_session"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """

    @classmethod
    def create(
        cls, **params: Unpack["CustomerSession.CreateParams"]
    ) -> "CustomerSession":
        """
        Creates a Customer Session object that includes a single-use client secret that you can use on your front-end to grant client-side API access for certain customer resources.
        """
        return cast(
            "CustomerSession",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["CustomerSession.CreateParams"]
    ) -> "CustomerSession":
        """
        Creates a Customer Session object that includes a single-use client secret that you can use on your front-end to grant client-side API access for certain customer resources.
        """
        return cast(
            "CustomerSession",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    _inner_class_types = {"components": Components}
