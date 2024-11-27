# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe.billing_portal._session import Session
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class SessionService(StripeService):
    class CreateParams(TypedDict):
        configuration: NotRequired[str]
        """
        The ID of an existing [configuration](https://stripe.com/docs/api/customer_portal/configuration) to use for this session, describing its functionality and features. If not specified, the session uses the default configuration.
        """
        customer: str
        """
        The ID of an existing customer.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        flow_data: NotRequired["SessionService.CreateParamsFlowData"]
        """
        Information about a specific flow for the customer to go through. See the [docs](https://stripe.com/docs/customer-management/portal-deep-links) to learn more about using customer portal deep links and flows.
        """
        locale: NotRequired[
            Literal[
                "auto",
                "bg",
                "cs",
                "da",
                "de",
                "el",
                "en",
                "en-AU",
                "en-CA",
                "en-GB",
                "en-IE",
                "en-IN",
                "en-NZ",
                "en-SG",
                "es",
                "es-419",
                "et",
                "fi",
                "fil",
                "fr",
                "fr-CA",
                "hr",
                "hu",
                "id",
                "it",
                "ja",
                "ko",
                "lt",
                "lv",
                "ms",
                "mt",
                "nb",
                "nl",
                "pl",
                "pt",
                "pt-BR",
                "ro",
                "ru",
                "sk",
                "sl",
                "sv",
                "th",
                "tr",
                "vi",
                "zh",
                "zh-HK",
                "zh-TW",
            ]
        ]
        """
        The IETF language tag of the locale customer portal is displayed in. If blank or auto, the customer's `preferred_locales` or browser's locale is used.
        """
        on_behalf_of: NotRequired[str]
        """
        The `on_behalf_of` account to use for this session. When specified, only subscriptions and invoices with this `on_behalf_of` account appear in the portal. For more information, see the [docs](https://stripe.com/docs/connect/separate-charges-and-transfers#settlement-merchant). Use the [Accounts API](https://stripe.com/docs/api/accounts/object#account_object-settings-branding) to modify the `on_behalf_of` account's branding settings, which the portal displays.
        """
        return_url: NotRequired[str]
        """
        The default URL to redirect customers to when they click on the portal's link to return to your website.
        """

    class CreateParamsFlowData(TypedDict):
        after_completion: NotRequired[
            "SessionService.CreateParamsFlowDataAfterCompletion"
        ]
        """
        Behavior after the flow is completed.
        """
        subscription_cancel: NotRequired[
            "SessionService.CreateParamsFlowDataSubscriptionCancel"
        ]
        """
        Configuration when `flow_data.type=subscription_cancel`.
        """
        subscription_update: NotRequired[
            "SessionService.CreateParamsFlowDataSubscriptionUpdate"
        ]
        """
        Configuration when `flow_data.type=subscription_update`.
        """
        subscription_update_confirm: NotRequired[
            "SessionService.CreateParamsFlowDataSubscriptionUpdateConfirm"
        ]
        """
        Configuration when `flow_data.type=subscription_update_confirm`.
        """
        type: Literal[
            "payment_method_update",
            "subscription_cancel",
            "subscription_update",
            "subscription_update_confirm",
        ]
        """
        Type of flow that the customer will go through.
        """

    class CreateParamsFlowDataAfterCompletion(TypedDict):
        hosted_confirmation: NotRequired[
            "SessionService.CreateParamsFlowDataAfterCompletionHostedConfirmation"
        ]
        """
        Configuration when `after_completion.type=hosted_confirmation`.
        """
        redirect: NotRequired[
            "SessionService.CreateParamsFlowDataAfterCompletionRedirect"
        ]
        """
        Configuration when `after_completion.type=redirect`.
        """
        type: Literal["hosted_confirmation", "portal_homepage", "redirect"]
        """
        The specified behavior after the flow is completed.
        """

    class CreateParamsFlowDataAfterCompletionHostedConfirmation(TypedDict):
        custom_message: NotRequired[str]
        """
        A custom message to display to the customer after the flow is completed.
        """

    class CreateParamsFlowDataAfterCompletionRedirect(TypedDict):
        return_url: str
        """
        The URL the customer will be redirected to after the flow is completed.
        """

    class CreateParamsFlowDataSubscriptionCancel(TypedDict):
        retention: NotRequired[
            "SessionService.CreateParamsFlowDataSubscriptionCancelRetention"
        ]
        """
        Specify a retention strategy to be used in the cancellation flow.
        """
        subscription: str
        """
        The ID of the subscription to be canceled.
        """

    class CreateParamsFlowDataSubscriptionCancelRetention(TypedDict):
        coupon_offer: "SessionService.CreateParamsFlowDataSubscriptionCancelRetentionCouponOffer"
        """
        Configuration when `retention.type=coupon_offer`.
        """
        type: Literal["coupon_offer"]
        """
        Type of retention strategy to use with the customer.
        """

    class CreateParamsFlowDataSubscriptionCancelRetentionCouponOffer(
        TypedDict
    ):
        coupon: str
        """
        The ID of the coupon to be offered.
        """

    class CreateParamsFlowDataSubscriptionUpdate(TypedDict):
        subscription: str
        """
        The ID of the subscription to be updated.
        """

    class CreateParamsFlowDataSubscriptionUpdateConfirm(TypedDict):
        discounts: NotRequired[
            List[
                "SessionService.CreateParamsFlowDataSubscriptionUpdateConfirmDiscount"
            ]
        ]
        """
        The coupon or promotion code to apply to this subscription update. Currently, only up to one may be specified.
        """
        items: List[
            "SessionService.CreateParamsFlowDataSubscriptionUpdateConfirmItem"
        ]
        """
        The [subscription item](https://stripe.com/docs/api/subscription_items) to be updated through this flow. Currently, only up to one may be specified and subscriptions with multiple items are not updatable.
        """
        subscription: str
        """
        The ID of the subscription to be updated.
        """

    class CreateParamsFlowDataSubscriptionUpdateConfirmDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this subscription update.
        """
        promotion_code: NotRequired[str]
        """
        The ID of a promotion code to apply to this subscription update.
        """

    class CreateParamsFlowDataSubscriptionUpdateConfirmItem(TypedDict):
        id: str
        """
        The ID of the [subscription item](https://stripe.com/docs/api/subscriptions/object#subscription_object-items-data-id) to be updated.
        """
        price: NotRequired[str]
        """
        The price the customer should subscribe to through this flow. The price must also be included in the configuration's [`features.subscription_update.products`](https://stripe.com/docs/api/customer_portal/configuration#portal_configuration_object-features-subscription_update-products).
        """
        quantity: NotRequired[int]
        """
        [Quantity](https://stripe.com/docs/subscriptions/quantities) for this item that the customer should subscribe to through this flow.
        """

    def create(
        self,
        params: "SessionService.CreateParams",
        options: RequestOptions = {},
    ) -> Session:
        """
        Creates a session of the customer portal.
        """
        return cast(
            Session,
            self._request(
                "post",
                "/v1/billing_portal/sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "SessionService.CreateParams",
        options: RequestOptions = {},
    ) -> Session:
        """
        Creates a session of the customer portal.
        """
        return cast(
            Session,
            await self._request_async(
                "post",
                "/v1/billing_portal/sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
