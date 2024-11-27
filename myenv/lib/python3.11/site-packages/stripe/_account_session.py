# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from typing import ClassVar, List, cast
from typing_extensions import Literal, NotRequired, TypedDict, Unpack


class AccountSession(CreateableAPIResource["AccountSession"]):
    """
    An AccountSession allows a Connect platform to grant access to a connected account in Connect embedded components.

    We recommend that you create an AccountSession each time you need to display an embedded component
    to your user. Do not save AccountSessions to your database as they expire relatively
    quickly, and cannot be used more than once.

    Related guide: [Connect embedded components](https://stripe.com/docs/connect/get-started-connect-embedded-components)
    """

    OBJECT_NAME: ClassVar[Literal["account_session"]] = "account_session"

    class Components(StripeObject):
        class AccountManagement(StripeObject):
            class Features(StripeObject):
                external_account_collection: bool
                """
                Whether to allow platforms to control bank account collection for their connected accounts. This feature can only be false for custom accounts (or accounts where the platform is compliance owner). Otherwise, bank account collection is determined by compliance requirements.
                """

            enabled: bool
            """
            Whether the embedded component is enabled.
            """
            features: Features
            _inner_class_types = {"features": Features}

        class AccountOnboarding(StripeObject):
            class Features(StripeObject):
                external_account_collection: bool
                """
                Whether to allow platforms to control bank account collection for their connected accounts. This feature can only be false for custom accounts (or accounts where the platform is compliance owner). Otherwise, bank account collection is determined by compliance requirements.
                """

            enabled: bool
            """
            Whether the embedded component is enabled.
            """
            features: Features
            _inner_class_types = {"features": Features}

        class Balances(StripeObject):
            class Features(StripeObject):
                edit_payout_schedule: bool
                """
                Whether to allow payout schedule to be changed. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
                """
                external_account_collection: bool
                """
                Whether to allow platforms to control bank account collection for their connected accounts. This feature can only be false for custom accounts (or accounts where the platform is compliance owner). Otherwise, bank account collection is determined by compliance requirements.
                """
                instant_payouts: bool
                """
                Whether to allow creation of instant payouts. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
                """
                standard_payouts: bool
                """
                Whether to allow creation of standard payouts. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
                """

            enabled: bool
            """
            Whether the embedded component is enabled.
            """
            features: Features
            _inner_class_types = {"features": Features}

        class Documents(StripeObject):
            class Features(StripeObject):
                pass

            enabled: bool
            """
            Whether the embedded component is enabled.
            """
            features: Features
            _inner_class_types = {"features": Features}

        class NotificationBanner(StripeObject):
            class Features(StripeObject):
                external_account_collection: bool
                """
                Whether to allow platforms to control bank account collection for their connected accounts. This feature can only be false for custom accounts (or accounts where the platform is compliance owner). Otherwise, bank account collection is determined by compliance requirements.
                """

            enabled: bool
            """
            Whether the embedded component is enabled.
            """
            features: Features
            _inner_class_types = {"features": Features}

        class PaymentDetails(StripeObject):
            class Features(StripeObject):
                capture_payments: bool
                """
                Whether to allow capturing and cancelling payment intents. This is `true` by default.
                """
                destination_on_behalf_of_charge_management: bool
                """
                Whether to allow connected accounts to manage destination charges that are created on behalf of them. This is `false` by default.
                """
                dispute_management: bool
                """
                Whether to allow responding to disputes, including submitting evidence and accepting disputes. This is `true` by default.
                """
                refund_management: bool
                """
                Whether to allow sending refunds. This is `true` by default.
                """

            enabled: bool
            """
            Whether the embedded component is enabled.
            """
            features: Features
            _inner_class_types = {"features": Features}

        class Payments(StripeObject):
            class Features(StripeObject):
                capture_payments: bool
                """
                Whether to allow capturing and cancelling payment intents. This is `true` by default.
                """
                destination_on_behalf_of_charge_management: bool
                """
                Whether to allow connected accounts to manage destination charges that are created on behalf of them. This is `false` by default.
                """
                dispute_management: bool
                """
                Whether to allow responding to disputes, including submitting evidence and accepting disputes. This is `true` by default.
                """
                refund_management: bool
                """
                Whether to allow sending refunds. This is `true` by default.
                """

            enabled: bool
            """
            Whether the embedded component is enabled.
            """
            features: Features
            _inner_class_types = {"features": Features}

        class Payouts(StripeObject):
            class Features(StripeObject):
                edit_payout_schedule: bool
                """
                Whether to allow payout schedule to be changed. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
                """
                external_account_collection: bool
                """
                Whether to allow platforms to control bank account collection for their connected accounts. This feature can only be false for custom accounts (or accounts where the platform is compliance owner). Otherwise, bank account collection is determined by compliance requirements.
                """
                instant_payouts: bool
                """
                Whether to allow creation of instant payouts. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
                """
                standard_payouts: bool
                """
                Whether to allow creation of standard payouts. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
                """

            enabled: bool
            """
            Whether the embedded component is enabled.
            """
            features: Features
            _inner_class_types = {"features": Features}

        class PayoutsList(StripeObject):
            class Features(StripeObject):
                pass

            enabled: bool
            """
            Whether the embedded component is enabled.
            """
            features: Features
            _inner_class_types = {"features": Features}

        class TaxRegistrations(StripeObject):
            class Features(StripeObject):
                pass

            enabled: bool
            """
            Whether the embedded component is enabled.
            """
            features: Features
            _inner_class_types = {"features": Features}

        class TaxSettings(StripeObject):
            class Features(StripeObject):
                pass

            enabled: bool
            """
            Whether the embedded component is enabled.
            """
            features: Features
            _inner_class_types = {"features": Features}

        account_management: AccountManagement
        account_onboarding: AccountOnboarding
        balances: Balances
        documents: Documents
        notification_banner: NotificationBanner
        payment_details: PaymentDetails
        payments: Payments
        payouts: Payouts
        payouts_list: PayoutsList
        tax_registrations: TaxRegistrations
        tax_settings: TaxSettings
        _inner_class_types = {
            "account_management": AccountManagement,
            "account_onboarding": AccountOnboarding,
            "balances": Balances,
            "documents": Documents,
            "notification_banner": NotificationBanner,
            "payment_details": PaymentDetails,
            "payments": Payments,
            "payouts": Payouts,
            "payouts_list": PayoutsList,
            "tax_registrations": TaxRegistrations,
            "tax_settings": TaxSettings,
        }

    class CreateParams(RequestOptions):
        account: str
        """
        The identifier of the account to create an Account Session for.
        """
        components: "AccountSession.CreateParamsComponents"
        """
        Each key of the dictionary represents an embedded component, and each embedded component maps to its configuration (e.g. whether it has been enabled or not).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParamsComponents(TypedDict):
        account_management: NotRequired[
            "AccountSession.CreateParamsComponentsAccountManagement"
        ]
        """
        Configuration for the account management embedded component.
        """
        account_onboarding: NotRequired[
            "AccountSession.CreateParamsComponentsAccountOnboarding"
        ]
        """
        Configuration for the account onboarding embedded component.
        """
        balances: NotRequired["AccountSession.CreateParamsComponentsBalances"]
        """
        Configuration for the balances embedded component.
        """
        documents: NotRequired[
            "AccountSession.CreateParamsComponentsDocuments"
        ]
        """
        Configuration for the documents embedded component.
        """
        notification_banner: NotRequired[
            "AccountSession.CreateParamsComponentsNotificationBanner"
        ]
        """
        Configuration for the notification banner embedded component.
        """
        payment_details: NotRequired[
            "AccountSession.CreateParamsComponentsPaymentDetails"
        ]
        """
        Configuration for the payment details embedded component.
        """
        payments: NotRequired["AccountSession.CreateParamsComponentsPayments"]
        """
        Configuration for the payments embedded component.
        """
        payouts: NotRequired["AccountSession.CreateParamsComponentsPayouts"]
        """
        Configuration for the payouts embedded component.
        """
        payouts_list: NotRequired[
            "AccountSession.CreateParamsComponentsPayoutsList"
        ]
        """
        Configuration for the payouts list embedded component.
        """
        tax_registrations: NotRequired[
            "AccountSession.CreateParamsComponentsTaxRegistrations"
        ]
        """
        Configuration for the tax registrations embedded component.
        """
        tax_settings: NotRequired[
            "AccountSession.CreateParamsComponentsTaxSettings"
        ]
        """
        Configuration for the tax settings embedded component.
        """

    class CreateParamsComponentsAccountManagement(TypedDict):
        enabled: bool
        """
        Whether the embedded component is enabled.
        """
        features: NotRequired[
            "AccountSession.CreateParamsComponentsAccountManagementFeatures"
        ]
        """
        The list of features enabled in the embedded component.
        """

    class CreateParamsComponentsAccountManagementFeatures(TypedDict):
        external_account_collection: NotRequired[bool]
        """
        Whether to allow platforms to control bank account collection for their connected accounts. This feature can only be false for custom accounts (or accounts where the platform is compliance owner). Otherwise, bank account collection is determined by compliance requirements.
        """

    class CreateParamsComponentsAccountOnboarding(TypedDict):
        enabled: bool
        """
        Whether the embedded component is enabled.
        """
        features: NotRequired[
            "AccountSession.CreateParamsComponentsAccountOnboardingFeatures"
        ]
        """
        The list of features enabled in the embedded component.
        """

    class CreateParamsComponentsAccountOnboardingFeatures(TypedDict):
        external_account_collection: NotRequired[bool]
        """
        Whether to allow platforms to control bank account collection for their connected accounts. This feature can only be false for custom accounts (or accounts where the platform is compliance owner). Otherwise, bank account collection is determined by compliance requirements.
        """

    class CreateParamsComponentsBalances(TypedDict):
        enabled: bool
        """
        Whether the embedded component is enabled.
        """
        features: NotRequired[
            "AccountSession.CreateParamsComponentsBalancesFeatures"
        ]
        """
        The list of features enabled in the embedded component.
        """

    class CreateParamsComponentsBalancesFeatures(TypedDict):
        edit_payout_schedule: NotRequired[bool]
        """
        Whether to allow payout schedule to be changed. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
        """
        external_account_collection: NotRequired[bool]
        """
        Whether to allow platforms to control bank account collection for their connected accounts. This feature can only be false for custom accounts (or accounts where the platform is compliance owner). Otherwise, bank account collection is determined by compliance requirements.
        """
        instant_payouts: NotRequired[bool]
        """
        Whether to allow creation of instant payouts. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
        """
        standard_payouts: NotRequired[bool]
        """
        Whether to allow creation of standard payouts. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
        """

    class CreateParamsComponentsDocuments(TypedDict):
        enabled: bool
        """
        Whether the embedded component is enabled.
        """
        features: NotRequired[
            "AccountSession.CreateParamsComponentsDocumentsFeatures"
        ]
        """
        The list of features enabled in the embedded component.
        """

    class CreateParamsComponentsDocumentsFeatures(TypedDict):
        pass

    class CreateParamsComponentsNotificationBanner(TypedDict):
        enabled: bool
        """
        Whether the embedded component is enabled.
        """
        features: NotRequired[
            "AccountSession.CreateParamsComponentsNotificationBannerFeatures"
        ]
        """
        The list of features enabled in the embedded component.
        """

    class CreateParamsComponentsNotificationBannerFeatures(TypedDict):
        external_account_collection: NotRequired[bool]
        """
        Whether to allow platforms to control bank account collection for their connected accounts. This feature can only be false for custom accounts (or accounts where the platform is compliance owner). Otherwise, bank account collection is determined by compliance requirements.
        """

    class CreateParamsComponentsPaymentDetails(TypedDict):
        enabled: bool
        """
        Whether the embedded component is enabled.
        """
        features: NotRequired[
            "AccountSession.CreateParamsComponentsPaymentDetailsFeatures"
        ]
        """
        The list of features enabled in the embedded component.
        """

    class CreateParamsComponentsPaymentDetailsFeatures(TypedDict):
        capture_payments: NotRequired[bool]
        """
        Whether to allow capturing and cancelling payment intents. This is `true` by default.
        """
        destination_on_behalf_of_charge_management: NotRequired[bool]
        """
        Whether to allow connected accounts to manage destination charges that are created on behalf of them. This is `false` by default.
        """
        dispute_management: NotRequired[bool]
        """
        Whether to allow responding to disputes, including submitting evidence and accepting disputes. This is `true` by default.
        """
        refund_management: NotRequired[bool]
        """
        Whether to allow sending refunds. This is `true` by default.
        """

    class CreateParamsComponentsPayments(TypedDict):
        enabled: bool
        """
        Whether the embedded component is enabled.
        """
        features: NotRequired[
            "AccountSession.CreateParamsComponentsPaymentsFeatures"
        ]
        """
        The list of features enabled in the embedded component.
        """

    class CreateParamsComponentsPaymentsFeatures(TypedDict):
        capture_payments: NotRequired[bool]
        """
        Whether to allow capturing and cancelling payment intents. This is `true` by default.
        """
        destination_on_behalf_of_charge_management: NotRequired[bool]
        """
        Whether to allow connected accounts to manage destination charges that are created on behalf of them. This is `false` by default.
        """
        dispute_management: NotRequired[bool]
        """
        Whether to allow responding to disputes, including submitting evidence and accepting disputes. This is `true` by default.
        """
        refund_management: NotRequired[bool]
        """
        Whether to allow sending refunds. This is `true` by default.
        """

    class CreateParamsComponentsPayouts(TypedDict):
        enabled: bool
        """
        Whether the embedded component is enabled.
        """
        features: NotRequired[
            "AccountSession.CreateParamsComponentsPayoutsFeatures"
        ]
        """
        The list of features enabled in the embedded component.
        """

    class CreateParamsComponentsPayoutsFeatures(TypedDict):
        edit_payout_schedule: NotRequired[bool]
        """
        Whether to allow payout schedule to be changed. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
        """
        external_account_collection: NotRequired[bool]
        """
        Whether to allow platforms to control bank account collection for their connected accounts. This feature can only be false for custom accounts (or accounts where the platform is compliance owner). Otherwise, bank account collection is determined by compliance requirements.
        """
        instant_payouts: NotRequired[bool]
        """
        Whether to allow creation of instant payouts. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
        """
        standard_payouts: NotRequired[bool]
        """
        Whether to allow creation of standard payouts. Default `true` when Stripe owns Loss Liability, default `false` otherwise.
        """

    class CreateParamsComponentsPayoutsList(TypedDict):
        enabled: bool
        """
        Whether the embedded component is enabled.
        """
        features: NotRequired[
            "AccountSession.CreateParamsComponentsPayoutsListFeatures"
        ]
        """
        The list of features enabled in the embedded component.
        """

    class CreateParamsComponentsPayoutsListFeatures(TypedDict):
        pass

    class CreateParamsComponentsTaxRegistrations(TypedDict):
        enabled: bool
        """
        Whether the embedded component is enabled.
        """
        features: NotRequired[
            "AccountSession.CreateParamsComponentsTaxRegistrationsFeatures"
        ]
        """
        The list of features enabled in the embedded component.
        """

    class CreateParamsComponentsTaxRegistrationsFeatures(TypedDict):
        pass

    class CreateParamsComponentsTaxSettings(TypedDict):
        enabled: bool
        """
        Whether the embedded component is enabled.
        """
        features: NotRequired[
            "AccountSession.CreateParamsComponentsTaxSettingsFeatures"
        ]
        """
        The list of features enabled in the embedded component.
        """

    class CreateParamsComponentsTaxSettingsFeatures(TypedDict):
        pass

    account: str
    """
    The ID of the account the AccountSession was created for
    """
    client_secret: str
    """
    The client secret of this AccountSession. Used on the client to set up secure access to the given `account`.

    The client secret can be used to provide access to `account` from your frontend. It should not be stored, logged, or exposed to anyone other than the connected account. Make sure that you have TLS enabled on any page that includes the client secret.

    Refer to our docs to [setup Connect embedded components](https://stripe.com/docs/connect/get-started-connect-embedded-components) and learn about how `client_secret` should be handled.
    """
    components: Components
    expires_at: int
    """
    The timestamp at which this AccountSession will expire.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["account_session"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """

    @classmethod
    def create(
        cls, **params: Unpack["AccountSession.CreateParams"]
    ) -> "AccountSession":
        """
        Creates a AccountSession object that includes a single-use token that the platform can use on their front-end to grant client-side API access.
        """
        return cast(
            "AccountSession",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["AccountSession.CreateParams"]
    ) -> "AccountSession":
        """
        Creates a AccountSession object that includes a single-use token that the platform can use on their front-end to grant client-side API access.
        """
        return cast(
            "AccountSession",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    _inner_class_types = {"components": Components}
