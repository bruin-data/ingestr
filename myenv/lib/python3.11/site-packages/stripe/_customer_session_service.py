# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._customer_session import CustomerSession
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class CustomerSessionService(StripeService):
    class CreateParams(TypedDict):
        components: "CustomerSessionService.CreateParamsComponents"
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
            "CustomerSessionService.CreateParamsComponentsBuyButton"
        ]
        """
        Configuration for buy button.
        """
        payment_element: NotRequired[
            "CustomerSessionService.CreateParamsComponentsPaymentElement"
        ]
        """
        Configuration for the Payment Element.
        """
        pricing_table: NotRequired[
            "CustomerSessionService.CreateParamsComponentsPricingTable"
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
            "CustomerSessionService.CreateParamsComponentsPaymentElementFeatures"
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

    def create(
        self,
        params: "CustomerSessionService.CreateParams",
        options: RequestOptions = {},
    ) -> CustomerSession:
        """
        Creates a Customer Session object that includes a single-use client secret that you can use on your front-end to grant client-side API access for certain customer resources.
        """
        return cast(
            CustomerSession,
            self._request(
                "post",
                "/v1/customer_sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "CustomerSessionService.CreateParams",
        options: RequestOptions = {},
    ) -> CustomerSession:
        """
        Creates a Customer Session object that includes a single-use client secret that you can use on your front-end to grant client-side API access for certain customer resources.
        """
        return cast(
            CustomerSession,
            await self._request_async(
                "post",
                "/v1/customer_sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
