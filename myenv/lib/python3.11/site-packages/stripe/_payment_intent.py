# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._search_result_object import SearchResultObject
from stripe._searchable_api_resource import SearchableAPIResource
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import (
    Any,
    AsyncIterator,
    ClassVar,
    Dict,
    Iterator,
    List,
    Optional,
    Union,
    cast,
    overload,
)
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._account import Account
    from stripe._application import Application
    from stripe._bank_account import BankAccount
    from stripe._card import Card as CardResource
    from stripe._charge import Charge
    from stripe._customer import Customer
    from stripe._invoice import Invoice
    from stripe._payment_method import PaymentMethod
    from stripe._review import Review
    from stripe._setup_intent import SetupIntent
    from stripe._source import Source


class PaymentIntent(
    CreateableAPIResource["PaymentIntent"],
    ListableAPIResource["PaymentIntent"],
    SearchableAPIResource["PaymentIntent"],
    UpdateableAPIResource["PaymentIntent"],
):
    """
    A PaymentIntent guides you through the process of collecting a payment from your customer.
    We recommend that you create exactly one PaymentIntent for each order or
    customer session in your system. You can reference the PaymentIntent later to
    see the history of payment attempts for a particular session.

    A PaymentIntent transitions through
    [multiple statuses](https://stripe.com/docs/payments/intents#intent-statuses)
    throughout its lifetime as it interfaces with Stripe.js to perform
    authentication flows and ultimately creates at most one successful charge.

    Related guide: [Payment Intents API](https://stripe.com/docs/payments/payment-intents)
    """

    OBJECT_NAME: ClassVar[Literal["payment_intent"]] = "payment_intent"

    class AmountDetails(StripeObject):
        class Tip(StripeObject):
            amount: Optional[int]
            """
            Portion of the amount that corresponds to a tip.
            """

        tip: Optional[Tip]
        _inner_class_types = {"tip": Tip}

    class AutomaticPaymentMethods(StripeObject):
        allow_redirects: Optional[Literal["always", "never"]]
        """
        Controls whether this PaymentIntent will accept redirect-based payment methods.

        Redirect-based payment methods may require your customer to be redirected to a payment method's app or site for authentication or additional steps. To [confirm](https://stripe.com/docs/api/payment_intents/confirm) this PaymentIntent, you may be required to provide a `return_url` to redirect customers back to your site after they authenticate or complete the payment.
        """
        enabled: bool
        """
        Automatically calculates compatible payment methods
        """

    class LastPaymentError(StripeObject):
        charge: Optional[str]
        """
        For card errors, the ID of the failed charge.
        """
        code: Optional[
            Literal[
                "account_closed",
                "account_country_invalid_address",
                "account_error_country_change_requires_additional_steps",
                "account_information_mismatch",
                "account_invalid",
                "account_number_invalid",
                "acss_debit_session_incomplete",
                "alipay_upgrade_required",
                "amount_too_large",
                "amount_too_small",
                "api_key_expired",
                "application_fees_not_allowed",
                "authentication_required",
                "balance_insufficient",
                "balance_invalid_parameter",
                "bank_account_bad_routing_numbers",
                "bank_account_declined",
                "bank_account_exists",
                "bank_account_restricted",
                "bank_account_unusable",
                "bank_account_unverified",
                "bank_account_verification_failed",
                "billing_invalid_mandate",
                "bitcoin_upgrade_required",
                "capture_charge_authorization_expired",
                "capture_unauthorized_payment",
                "card_decline_rate_limit_exceeded",
                "card_declined",
                "cardholder_phone_number_required",
                "charge_already_captured",
                "charge_already_refunded",
                "charge_disputed",
                "charge_exceeds_source_limit",
                "charge_exceeds_transaction_limit",
                "charge_expired_for_capture",
                "charge_invalid_parameter",
                "charge_not_refundable",
                "clearing_code_unsupported",
                "country_code_invalid",
                "country_unsupported",
                "coupon_expired",
                "customer_max_payment_methods",
                "customer_max_subscriptions",
                "customer_tax_location_invalid",
                "debit_not_authorized",
                "email_invalid",
                "expired_card",
                "financial_connections_account_inactive",
                "financial_connections_no_successful_transaction_refresh",
                "forwarding_api_inactive",
                "forwarding_api_invalid_parameter",
                "forwarding_api_upstream_connection_error",
                "forwarding_api_upstream_connection_timeout",
                "idempotency_key_in_use",
                "incorrect_address",
                "incorrect_cvc",
                "incorrect_number",
                "incorrect_zip",
                "instant_payouts_config_disabled",
                "instant_payouts_currency_disabled",
                "instant_payouts_limit_exceeded",
                "instant_payouts_unsupported",
                "insufficient_funds",
                "intent_invalid_state",
                "intent_verification_method_missing",
                "invalid_card_type",
                "invalid_characters",
                "invalid_charge_amount",
                "invalid_cvc",
                "invalid_expiry_month",
                "invalid_expiry_year",
                "invalid_mandate_reference_prefix_format",
                "invalid_number",
                "invalid_source_usage",
                "invalid_tax_location",
                "invoice_no_customer_line_items",
                "invoice_no_payment_method_types",
                "invoice_no_subscription_line_items",
                "invoice_not_editable",
                "invoice_on_behalf_of_not_editable",
                "invoice_payment_intent_requires_action",
                "invoice_upcoming_none",
                "livemode_mismatch",
                "lock_timeout",
                "missing",
                "no_account",
                "not_allowed_on_standard_account",
                "out_of_inventory",
                "ownership_declaration_not_allowed",
                "parameter_invalid_empty",
                "parameter_invalid_integer",
                "parameter_invalid_string_blank",
                "parameter_invalid_string_empty",
                "parameter_missing",
                "parameter_unknown",
                "parameters_exclusive",
                "payment_intent_action_required",
                "payment_intent_authentication_failure",
                "payment_intent_incompatible_payment_method",
                "payment_intent_invalid_parameter",
                "payment_intent_konbini_rejected_confirmation_number",
                "payment_intent_mandate_invalid",
                "payment_intent_payment_attempt_expired",
                "payment_intent_payment_attempt_failed",
                "payment_intent_unexpected_state",
                "payment_method_bank_account_already_verified",
                "payment_method_bank_account_blocked",
                "payment_method_billing_details_address_missing",
                "payment_method_configuration_failures",
                "payment_method_currency_mismatch",
                "payment_method_customer_decline",
                "payment_method_invalid_parameter",
                "payment_method_invalid_parameter_testmode",
                "payment_method_microdeposit_failed",
                "payment_method_microdeposit_verification_amounts_invalid",
                "payment_method_microdeposit_verification_amounts_mismatch",
                "payment_method_microdeposit_verification_attempts_exceeded",
                "payment_method_microdeposit_verification_descriptor_code_mismatch",
                "payment_method_microdeposit_verification_timeout",
                "payment_method_not_available",
                "payment_method_provider_decline",
                "payment_method_provider_timeout",
                "payment_method_unactivated",
                "payment_method_unexpected_state",
                "payment_method_unsupported_type",
                "payout_reconciliation_not_ready",
                "payouts_limit_exceeded",
                "payouts_not_allowed",
                "platform_account_required",
                "platform_api_key_expired",
                "postal_code_invalid",
                "processing_error",
                "product_inactive",
                "progressive_onboarding_limit_exceeded",
                "rate_limit",
                "refer_to_customer",
                "refund_disputed_payment",
                "resource_already_exists",
                "resource_missing",
                "return_intent_already_processed",
                "routing_number_invalid",
                "secret_key_required",
                "sepa_unsupported_account",
                "setup_attempt_failed",
                "setup_intent_authentication_failure",
                "setup_intent_invalid_parameter",
                "setup_intent_mandate_invalid",
                "setup_intent_setup_attempt_expired",
                "setup_intent_unexpected_state",
                "shipping_address_invalid",
                "shipping_calculation_failed",
                "sku_inactive",
                "state_unsupported",
                "status_transition_invalid",
                "stripe_tax_inactive",
                "tax_id_invalid",
                "taxes_calculation_failed",
                "terminal_location_country_unsupported",
                "terminal_reader_busy",
                "terminal_reader_hardware_fault",
                "terminal_reader_invalid_location_for_payment",
                "terminal_reader_offline",
                "terminal_reader_timeout",
                "testmode_charges_only",
                "tls_version_unsupported",
                "token_already_used",
                "token_card_network_invalid",
                "token_in_use",
                "transfer_source_balance_parameters_mismatch",
                "transfers_not_allowed",
                "url_invalid",
            ]
        ]
        """
        For some errors that could be handled programmatically, a short string indicating the [error code](https://stripe.com/docs/error-codes) reported.
        """
        decline_code: Optional[str]
        """
        For card errors resulting from a card issuer decline, a short string indicating the [card issuer's reason for the decline](https://stripe.com/docs/declines#issuer-declines) if they provide one.
        """
        doc_url: Optional[str]
        """
        A URL to more information about the [error code](https://stripe.com/docs/error-codes) reported.
        """
        message: Optional[str]
        """
        A human-readable message providing more details about the error. For card errors, these messages can be shown to your users.
        """
        param: Optional[str]
        """
        If the error is parameter-specific, the parameter related to the error. For example, you can use this to display a message near the correct form field.
        """
        payment_intent: Optional["PaymentIntent"]
        """
        A PaymentIntent guides you through the process of collecting a payment from your customer.
        We recommend that you create exactly one PaymentIntent for each order or
        customer session in your system. You can reference the PaymentIntent later to
        see the history of payment attempts for a particular session.

        A PaymentIntent transitions through
        [multiple statuses](https://stripe.com/docs/payments/intents#intent-statuses)
        throughout its lifetime as it interfaces with Stripe.js to perform
        authentication flows and ultimately creates at most one successful charge.

        Related guide: [Payment Intents API](https://stripe.com/docs/payments/payment-intents)
        """
        payment_method: Optional["PaymentMethod"]
        """
        PaymentMethod objects represent your customer's payment instruments.
        You can use them with [PaymentIntents](https://stripe.com/docs/payments/payment-intents) to collect payments or save them to
        Customer objects to store instrument details for future payments.

        Related guides: [Payment Methods](https://stripe.com/docs/payments/payment-methods) and [More Payment Scenarios](https://stripe.com/docs/payments/more-payment-scenarios).
        """
        payment_method_type: Optional[str]
        """
        If the error is specific to the type of payment method, the payment method type that had a problem. This field is only populated for invoice-related errors.
        """
        request_log_url: Optional[str]
        """
        A URL to the request log entry in your dashboard.
        """
        setup_intent: Optional["SetupIntent"]
        """
        A SetupIntent guides you through the process of setting up and saving a customer's payment credentials for future payments.
        For example, you can use a SetupIntent to set up and save your customer's card without immediately collecting a payment.
        Later, you can use [PaymentIntents](https://stripe.com/docs/api#payment_intents) to drive the payment flow.

        Create a SetupIntent when you're ready to collect your customer's payment credentials.
        Don't maintain long-lived, unconfirmed SetupIntents because they might not be valid.
        The SetupIntent transitions through multiple [statuses](https://docs.stripe.com/payments/intents#intent-statuses) as it guides
        you through the setup process.

        Successful SetupIntents result in payment credentials that are optimized for future payments.
        For example, cardholders in [certain regions](https://stripe.com/guides/strong-customer-authentication) might need to be run through
        [Strong Customer Authentication](https://docs.stripe.com/strong-customer-authentication) during payment method collection
        to streamline later [off-session payments](https://docs.stripe.com/payments/setup-intents).
        If you use the SetupIntent with a [Customer](https://stripe.com/docs/api#setup_intent_object-customer),
        it automatically attaches the resulting payment method to that Customer after successful setup.
        We recommend using SetupIntents or [setup_future_usage](https://stripe.com/docs/api#payment_intent_object-setup_future_usage) on
        PaymentIntents to save payment methods to prevent saving invalid or unoptimized payment methods.

        By using SetupIntents, you can reduce friction for your customers, even as regulations change over time.

        Related guide: [Setup Intents API](https://docs.stripe.com/payments/setup-intents)
        """
        source: Optional[
            Union["Account", "BankAccount", "CardResource", "Source"]
        ]
        type: Literal[
            "api_error",
            "card_error",
            "idempotency_error",
            "invalid_request_error",
        ]
        """
        The type of error returned. One of `api_error`, `card_error`, `idempotency_error`, or `invalid_request_error`
        """

    class NextAction(StripeObject):
        class AlipayHandleRedirect(StripeObject):
            native_data: Optional[str]
            """
            The native data to be used with Alipay SDK you must redirect your customer to in order to authenticate the payment in an Android App.
            """
            native_url: Optional[str]
            """
            The native URL you must redirect your customer to in order to authenticate the payment in an iOS App.
            """
            return_url: Optional[str]
            """
            If the customer does not exit their browser while authenticating, they will be redirected to this specified URL after completion.
            """
            url: Optional[str]
            """
            The URL you must redirect your customer to in order to authenticate the payment.
            """

        class BoletoDisplayDetails(StripeObject):
            expires_at: Optional[int]
            """
            The timestamp after which the boleto expires.
            """
            hosted_voucher_url: Optional[str]
            """
            The URL to the hosted boleto voucher page, which allows customers to view the boleto voucher.
            """
            number: Optional[str]
            """
            The boleto number.
            """
            pdf: Optional[str]
            """
            The URL to the downloadable boleto voucher PDF.
            """

        class CardAwaitNotification(StripeObject):
            charge_attempt_at: Optional[int]
            """
            The time that payment will be attempted. If customer approval is required, they need to provide approval before this time.
            """
            customer_approval_required: Optional[bool]
            """
            For payments greater than INR 15000, the customer must provide explicit approval of the payment with their bank. For payments of lower amount, no customer action is required.
            """

        class CashappHandleRedirectOrDisplayQrCode(StripeObject):
            class QrCode(StripeObject):
                expires_at: int
                """
                The date (unix timestamp) when the QR code expires.
                """
                image_url_png: str
                """
                The image_url_png string used to render QR code
                """
                image_url_svg: str
                """
                The image_url_svg string used to render QR code
                """

            hosted_instructions_url: str
            """
            The URL to the hosted Cash App Pay instructions page, which allows customers to view the QR code, and supports QR code refreshing on expiration.
            """
            mobile_auth_url: str
            """
            The url for mobile redirect based auth
            """
            qr_code: QrCode
            _inner_class_types = {"qr_code": QrCode}

        class DisplayBankTransferInstructions(StripeObject):
            class FinancialAddress(StripeObject):
                class Aba(StripeObject):
                    account_number: str
                    """
                    The ABA account number
                    """
                    bank_name: str
                    """
                    The bank name
                    """
                    routing_number: str
                    """
                    The ABA routing number
                    """

                class Iban(StripeObject):
                    account_holder_name: str
                    """
                    The name of the person or business that owns the bank account
                    """
                    bic: str
                    """
                    The BIC/SWIFT code of the account.
                    """
                    country: str
                    """
                    Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
                    """
                    iban: str
                    """
                    The IBAN of the account.
                    """

                class SortCode(StripeObject):
                    account_holder_name: str
                    """
                    The name of the person or business that owns the bank account
                    """
                    account_number: str
                    """
                    The account number
                    """
                    sort_code: str
                    """
                    The six-digit sort code
                    """

                class Spei(StripeObject):
                    bank_code: str
                    """
                    The three-digit bank code
                    """
                    bank_name: str
                    """
                    The short banking institution name
                    """
                    clabe: str
                    """
                    The CLABE number
                    """

                class Swift(StripeObject):
                    account_number: str
                    """
                    The account number
                    """
                    bank_name: str
                    """
                    The bank name
                    """
                    swift_code: str
                    """
                    The SWIFT code
                    """

                class Zengin(StripeObject):
                    account_holder_name: Optional[str]
                    """
                    The account holder name
                    """
                    account_number: Optional[str]
                    """
                    The account number
                    """
                    account_type: Optional[str]
                    """
                    The bank account type. In Japan, this can only be `futsu` or `toza`.
                    """
                    bank_code: Optional[str]
                    """
                    The bank code of the account
                    """
                    bank_name: Optional[str]
                    """
                    The bank name of the account
                    """
                    branch_code: Optional[str]
                    """
                    The branch code of the account
                    """
                    branch_name: Optional[str]
                    """
                    The branch name of the account
                    """

                aba: Optional[Aba]
                """
                ABA Records contain U.S. bank account details per the ABA format.
                """
                iban: Optional[Iban]
                """
                Iban Records contain E.U. bank account details per the SEPA format.
                """
                sort_code: Optional[SortCode]
                """
                Sort Code Records contain U.K. bank account details per the sort code format.
                """
                spei: Optional[Spei]
                """
                SPEI Records contain Mexico bank account details per the SPEI format.
                """
                supported_networks: Optional[
                    List[
                        Literal[
                            "ach",
                            "bacs",
                            "domestic_wire_us",
                            "fps",
                            "sepa",
                            "spei",
                            "swift",
                            "zengin",
                        ]
                    ]
                ]
                """
                The payment networks supported by this FinancialAddress
                """
                swift: Optional[Swift]
                """
                SWIFT Records contain U.S. bank account details per the SWIFT format.
                """
                type: Literal[
                    "aba", "iban", "sort_code", "spei", "swift", "zengin"
                ]
                """
                The type of financial address
                """
                zengin: Optional[Zengin]
                """
                Zengin Records contain Japan bank account details per the Zengin format.
                """
                _inner_class_types = {
                    "aba": Aba,
                    "iban": Iban,
                    "sort_code": SortCode,
                    "spei": Spei,
                    "swift": Swift,
                    "zengin": Zengin,
                }

            amount_remaining: Optional[int]
            """
            The remaining amount that needs to be transferred to complete the payment.
            """
            currency: Optional[str]
            """
            Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
            """
            financial_addresses: Optional[List[FinancialAddress]]
            """
            A list of financial addresses that can be used to fund the customer balance
            """
            hosted_instructions_url: Optional[str]
            """
            A link to a hosted page that guides your customer through completing the transfer.
            """
            reference: Optional[str]
            """
            A string identifying this payment. Instruct your customer to include this code in the reference or memo field of their bank transfer.
            """
            type: Literal[
                "eu_bank_transfer",
                "gb_bank_transfer",
                "jp_bank_transfer",
                "mx_bank_transfer",
                "us_bank_transfer",
            ]
            """
            Type of bank transfer
            """
            _inner_class_types = {"financial_addresses": FinancialAddress}

        class KonbiniDisplayDetails(StripeObject):
            class Stores(StripeObject):
                class Familymart(StripeObject):
                    confirmation_number: Optional[str]
                    """
                    The confirmation number.
                    """
                    payment_code: str
                    """
                    The payment code.
                    """

                class Lawson(StripeObject):
                    confirmation_number: Optional[str]
                    """
                    The confirmation number.
                    """
                    payment_code: str
                    """
                    The payment code.
                    """

                class Ministop(StripeObject):
                    confirmation_number: Optional[str]
                    """
                    The confirmation number.
                    """
                    payment_code: str
                    """
                    The payment code.
                    """

                class Seicomart(StripeObject):
                    confirmation_number: Optional[str]
                    """
                    The confirmation number.
                    """
                    payment_code: str
                    """
                    The payment code.
                    """

                familymart: Optional[Familymart]
                """
                FamilyMart instruction details.
                """
                lawson: Optional[Lawson]
                """
                Lawson instruction details.
                """
                ministop: Optional[Ministop]
                """
                Ministop instruction details.
                """
                seicomart: Optional[Seicomart]
                """
                Seicomart instruction details.
                """
                _inner_class_types = {
                    "familymart": Familymart,
                    "lawson": Lawson,
                    "ministop": Ministop,
                    "seicomart": Seicomart,
                }

            expires_at: int
            """
            The timestamp at which the pending Konbini payment expires.
            """
            hosted_voucher_url: Optional[str]
            """
            The URL for the Konbini payment instructions page, which allows customers to view and print a Konbini voucher.
            """
            stores: Stores
            _inner_class_types = {"stores": Stores}

        class MultibancoDisplayDetails(StripeObject):
            entity: Optional[str]
            """
            Entity number associated with this Multibanco payment.
            """
            expires_at: Optional[int]
            """
            The timestamp at which the Multibanco voucher expires.
            """
            hosted_voucher_url: Optional[str]
            """
            The URL for the hosted Multibanco voucher page, which allows customers to view a Multibanco voucher.
            """
            reference: Optional[str]
            """
            Reference number associated with this Multibanco payment.
            """

        class OxxoDisplayDetails(StripeObject):
            expires_after: Optional[int]
            """
            The timestamp after which the OXXO voucher expires.
            """
            hosted_voucher_url: Optional[str]
            """
            The URL for the hosted OXXO voucher page, which allows customers to view and print an OXXO voucher.
            """
            number: Optional[str]
            """
            OXXO reference number.
            """

        class PaynowDisplayQrCode(StripeObject):
            data: str
            """
            The raw data string used to generate QR code, it should be used together with QR code library.
            """
            hosted_instructions_url: Optional[str]
            """
            The URL to the hosted PayNow instructions page, which allows customers to view the PayNow QR code.
            """
            image_url_png: str
            """
            The image_url_png string used to render QR code
            """
            image_url_svg: str
            """
            The image_url_svg string used to render QR code
            """

        class PixDisplayQrCode(StripeObject):
            data: Optional[str]
            """
            The raw data string used to generate QR code, it should be used together with QR code library.
            """
            expires_at: Optional[int]
            """
            The date (unix timestamp) when the PIX expires.
            """
            hosted_instructions_url: Optional[str]
            """
            The URL to the hosted pix instructions page, which allows customers to view the pix QR code.
            """
            image_url_png: Optional[str]
            """
            The image_url_png string used to render png QR code
            """
            image_url_svg: Optional[str]
            """
            The image_url_svg string used to render svg QR code
            """

        class PromptpayDisplayQrCode(StripeObject):
            data: str
            """
            The raw data string used to generate QR code, it should be used together with QR code library.
            """
            hosted_instructions_url: str
            """
            The URL to the hosted PromptPay instructions page, which allows customers to view the PromptPay QR code.
            """
            image_url_png: str
            """
            The PNG path used to render the QR code, can be used as the source in an HTML img tag
            """
            image_url_svg: str
            """
            The SVG path used to render the QR code, can be used as the source in an HTML img tag
            """

        class RedirectToUrl(StripeObject):
            return_url: Optional[str]
            """
            If the customer does not exit their browser while authenticating, they will be redirected to this specified URL after completion.
            """
            url: Optional[str]
            """
            The URL you must redirect your customer to in order to authenticate the payment.
            """

        class SwishHandleRedirectOrDisplayQrCode(StripeObject):
            class QrCode(StripeObject):
                data: str
                """
                The raw data string used to generate QR code, it should be used together with QR code library.
                """
                image_url_png: str
                """
                The image_url_png string used to render QR code
                """
                image_url_svg: str
                """
                The image_url_svg string used to render QR code
                """

            hosted_instructions_url: str
            """
            The URL to the hosted Swish instructions page, which allows customers to view the QR code.
            """
            mobile_auth_url: str
            """
            The url for mobile redirect based auth (for internal use only and not typically available in standard API requests).
            """
            qr_code: QrCode
            _inner_class_types = {"qr_code": QrCode}

        class VerifyWithMicrodeposits(StripeObject):
            arrival_date: int
            """
            The timestamp when the microdeposits are expected to land.
            """
            hosted_verification_url: str
            """
            The URL for the hosted verification page, which allows customers to verify their bank account.
            """
            microdeposit_type: Optional[Literal["amounts", "descriptor_code"]]
            """
            The type of the microdeposit sent to the customer. Used to distinguish between different verification methods.
            """

        class WechatPayDisplayQrCode(StripeObject):
            data: str
            """
            The data being used to generate QR code
            """
            hosted_instructions_url: str
            """
            The URL to the hosted WeChat Pay instructions page, which allows customers to view the WeChat Pay QR code.
            """
            image_data_url: str
            """
            The base64 image data for a pre-generated QR code
            """
            image_url_png: str
            """
            The image_url_png string used to render QR code
            """
            image_url_svg: str
            """
            The image_url_svg string used to render QR code
            """

        class WechatPayRedirectToAndroidApp(StripeObject):
            app_id: str
            """
            app_id is the APP ID registered on WeChat open platform
            """
            nonce_str: str
            """
            nonce_str is a random string
            """
            package: str
            """
            package is static value
            """
            partner_id: str
            """
            an unique merchant ID assigned by WeChat Pay
            """
            prepay_id: str
            """
            an unique trading ID assigned by WeChat Pay
            """
            sign: str
            """
            A signature
            """
            timestamp: str
            """
            Specifies the current time in epoch format
            """

        class WechatPayRedirectToIosApp(StripeObject):
            native_url: str
            """
            An universal link that redirect to WeChat Pay app
            """

        alipay_handle_redirect: Optional[AlipayHandleRedirect]
        boleto_display_details: Optional[BoletoDisplayDetails]
        card_await_notification: Optional[CardAwaitNotification]
        cashapp_handle_redirect_or_display_qr_code: Optional[
            CashappHandleRedirectOrDisplayQrCode
        ]
        display_bank_transfer_instructions: Optional[
            DisplayBankTransferInstructions
        ]
        konbini_display_details: Optional[KonbiniDisplayDetails]
        multibanco_display_details: Optional[MultibancoDisplayDetails]
        oxxo_display_details: Optional[OxxoDisplayDetails]
        paynow_display_qr_code: Optional[PaynowDisplayQrCode]
        pix_display_qr_code: Optional[PixDisplayQrCode]
        promptpay_display_qr_code: Optional[PromptpayDisplayQrCode]
        redirect_to_url: Optional[RedirectToUrl]
        swish_handle_redirect_or_display_qr_code: Optional[
            SwishHandleRedirectOrDisplayQrCode
        ]
        type: str
        """
        Type of the next action to perform, one of `redirect_to_url`, `use_stripe_sdk`, `alipay_handle_redirect`, `oxxo_display_details`, or `verify_with_microdeposits`.
        """
        use_stripe_sdk: Optional[Dict[str, Any]]
        """
        When confirming a PaymentIntent with Stripe.js, Stripe.js depends on the contents of this dictionary to invoke authentication flows. The shape of the contents is subject to change and is only intended to be used by Stripe.js.
        """
        verify_with_microdeposits: Optional[VerifyWithMicrodeposits]
        wechat_pay_display_qr_code: Optional[WechatPayDisplayQrCode]
        wechat_pay_redirect_to_android_app: Optional[
            WechatPayRedirectToAndroidApp
        ]
        wechat_pay_redirect_to_ios_app: Optional[WechatPayRedirectToIosApp]
        _inner_class_types = {
            "alipay_handle_redirect": AlipayHandleRedirect,
            "boleto_display_details": BoletoDisplayDetails,
            "card_await_notification": CardAwaitNotification,
            "cashapp_handle_redirect_or_display_qr_code": CashappHandleRedirectOrDisplayQrCode,
            "display_bank_transfer_instructions": DisplayBankTransferInstructions,
            "konbini_display_details": KonbiniDisplayDetails,
            "multibanco_display_details": MultibancoDisplayDetails,
            "oxxo_display_details": OxxoDisplayDetails,
            "paynow_display_qr_code": PaynowDisplayQrCode,
            "pix_display_qr_code": PixDisplayQrCode,
            "promptpay_display_qr_code": PromptpayDisplayQrCode,
            "redirect_to_url": RedirectToUrl,
            "swish_handle_redirect_or_display_qr_code": SwishHandleRedirectOrDisplayQrCode,
            "verify_with_microdeposits": VerifyWithMicrodeposits,
            "wechat_pay_display_qr_code": WechatPayDisplayQrCode,
            "wechat_pay_redirect_to_android_app": WechatPayRedirectToAndroidApp,
            "wechat_pay_redirect_to_ios_app": WechatPayRedirectToIosApp,
        }

    class PaymentMethodConfigurationDetails(StripeObject):
        id: str
        """
        ID of the payment method configuration used.
        """
        parent: Optional[str]
        """
        ID of the parent payment method configuration used.
        """

    class PaymentMethodOptions(StripeObject):
        class AcssDebit(StripeObject):
            class MandateOptions(StripeObject):
                custom_mandate_url: Optional[str]
                """
                A URL for custom mandate text
                """
                interval_description: Optional[str]
                """
                Description of the interval. Only required if the 'payment_schedule' parameter is 'interval' or 'combined'.
                """
                payment_schedule: Optional[
                    Literal["combined", "interval", "sporadic"]
                ]
                """
                Payment schedule for the mandate.
                """
                transaction_type: Optional[Literal["business", "personal"]]
                """
                Transaction type of the mandate.
                """

            mandate_options: Optional[MandateOptions]
            setup_future_usage: Optional[
                Literal["none", "off_session", "on_session"]
            ]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """
            verification_method: Optional[
                Literal["automatic", "instant", "microdeposits"]
            ]
            """
            Bank account verification method.
            """
            _inner_class_types = {"mandate_options": MandateOptions}

        class Affirm(StripeObject):
            capture_method: Optional[Literal["manual"]]
            """
            Controls when the funds will be captured from the customer's account.
            """
            preferred_locale: Optional[str]
            """
            Preferred language of the Affirm authorization page that the customer is redirected to.
            """
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class AfterpayClearpay(StripeObject):
            capture_method: Optional[Literal["manual"]]
            """
            Controls when the funds will be captured from the customer's account.
            """
            reference: Optional[str]
            """
            An internal identifier or reference that this payment corresponds to. You must limit the identifier to 128 characters, and it can only contain letters, numbers, underscores, backslashes, and dashes.
            This field differs from the statement descriptor and item name.
            """
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Alipay(StripeObject):
            setup_future_usage: Optional[Literal["none", "off_session"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class AmazonPay(StripeObject):
            capture_method: Optional[Literal["manual"]]
            """
            Controls when the funds will be captured from the customer's account.
            """
            setup_future_usage: Optional[Literal["none", "off_session"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class AuBecsDebit(StripeObject):
            setup_future_usage: Optional[
                Literal["none", "off_session", "on_session"]
            ]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class BacsDebit(StripeObject):
            setup_future_usage: Optional[
                Literal["none", "off_session", "on_session"]
            ]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Bancontact(StripeObject):
            preferred_language: Literal["de", "en", "fr", "nl"]
            """
            Preferred language of the Bancontact authorization page that the customer is redirected to.
            """
            setup_future_usage: Optional[Literal["none", "off_session"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Blik(StripeObject):
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Boleto(StripeObject):
            expires_after_days: int
            """
            The number of calendar days before a Boleto voucher expires. For example, if you create a Boleto voucher on Monday and you set expires_after_days to 2, the Boleto voucher will expire on Wednesday at 23:59 America/Sao_Paulo time.
            """
            setup_future_usage: Optional[
                Literal["none", "off_session", "on_session"]
            ]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Card(StripeObject):
            class Installments(StripeObject):
                class AvailablePlan(StripeObject):
                    count: Optional[int]
                    """
                    For `fixed_count` installment plans, this is the number of installment payments your customer will make to their credit card.
                    """
                    interval: Optional[Literal["month"]]
                    """
                    For `fixed_count` installment plans, this is the interval between installment payments your customer will make to their credit card.
                    One of `month`.
                    """
                    type: Literal["fixed_count"]
                    """
                    Type of installment plan, one of `fixed_count`.
                    """

                class Plan(StripeObject):
                    count: Optional[int]
                    """
                    For `fixed_count` installment plans, this is the number of installment payments your customer will make to their credit card.
                    """
                    interval: Optional[Literal["month"]]
                    """
                    For `fixed_count` installment plans, this is the interval between installment payments your customer will make to their credit card.
                    One of `month`.
                    """
                    type: Literal["fixed_count"]
                    """
                    Type of installment plan, one of `fixed_count`.
                    """

                available_plans: Optional[List[AvailablePlan]]
                """
                Installment plans that may be selected for this PaymentIntent.
                """
                enabled: bool
                """
                Whether Installments are enabled for this PaymentIntent.
                """
                plan: Optional[Plan]
                """
                Installment plan selected for this PaymentIntent.
                """
                _inner_class_types = {
                    "available_plans": AvailablePlan,
                    "plan": Plan,
                }

            class MandateOptions(StripeObject):
                amount: int
                """
                Amount to be charged for future payments.
                """
                amount_type: Literal["fixed", "maximum"]
                """
                One of `fixed` or `maximum`. If `fixed`, the `amount` param refers to the exact amount to be charged in future payments. If `maximum`, the amount charged can be up to the value passed for the `amount` param.
                """
                description: Optional[str]
                """
                A description of the mandate or subscription that is meant to be displayed to the customer.
                """
                end_date: Optional[int]
                """
                End date of the mandate or subscription. If not provided, the mandate will be active until canceled. If provided, end date should be after start date.
                """
                interval: Literal["day", "month", "sporadic", "week", "year"]
                """
                Specifies payment frequency. One of `day`, `week`, `month`, `year`, or `sporadic`.
                """
                interval_count: Optional[int]
                """
                The number of intervals between payments. For example, `interval=month` and `interval_count=3` indicates one payment every three months. Maximum of one year interval allowed (1 year, 12 months, or 52 weeks). This parameter is optional when `interval=sporadic`.
                """
                reference: str
                """
                Unique identifier for the mandate or subscription.
                """
                start_date: int
                """
                Start date of the mandate or subscription. Start date should not be lesser than yesterday.
                """
                supported_types: Optional[List[Literal["india"]]]
                """
                Specifies the type of mandates supported. Possible values are `india`.
                """

            capture_method: Optional[Literal["manual"]]
            """
            Controls when the funds will be captured from the customer's account.
            """
            installments: Optional[Installments]
            """
            Installment details for this payment (Mexico only).

            For more information, see the [installments integration guide](https://stripe.com/docs/payments/installments).
            """
            mandate_options: Optional[MandateOptions]
            """
            Configuration options for setting up an eMandate for cards issued in India.
            """
            network: Optional[
                Literal[
                    "amex",
                    "cartes_bancaires",
                    "diners",
                    "discover",
                    "eftpos_au",
                    "girocard",
                    "interac",
                    "jcb",
                    "mastercard",
                    "unionpay",
                    "unknown",
                    "visa",
                ]
            ]
            """
            Selected network to process this payment intent on. Depends on the available networks of the card attached to the payment intent. Can be only set confirm-time.
            """
            request_extended_authorization: Optional[
                Literal["if_available", "never"]
            ]
            """
            Request ability to [capture beyond the standard authorization validity window](https://stripe.com/docs/payments/extended-authorization) for this PaymentIntent.
            """
            request_incremental_authorization: Optional[
                Literal["if_available", "never"]
            ]
            """
            Request ability to [increment the authorization](https://stripe.com/docs/payments/incremental-authorization) for this PaymentIntent.
            """
            request_multicapture: Optional[Literal["if_available", "never"]]
            """
            Request ability to make [multiple captures](https://stripe.com/docs/payments/multicapture) for this PaymentIntent.
            """
            request_overcapture: Optional[Literal["if_available", "never"]]
            """
            Request ability to [overcapture](https://stripe.com/docs/payments/overcapture) for this PaymentIntent.
            """
            request_three_d_secure: Optional[
                Literal["any", "automatic", "challenge"]
            ]
            """
            We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. If not provided, this value defaults to `automatic`. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
            """
            require_cvc_recollection: Optional[bool]
            """
            When enabled, using a card that is attached to a customer will require the CVC to be provided again (i.e. using the cvc_token parameter).
            """
            setup_future_usage: Optional[
                Literal["none", "off_session", "on_session"]
            ]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """
            statement_descriptor_suffix_kana: Optional[str]
            """
            Provides information about a card payment that customers see on their statements. Concatenated with the Kana prefix (shortened Kana descriptor) or Kana statement descriptor that's set on the account to form the complete statement descriptor. Maximum 22 characters. On card statements, the *concatenation* of both prefix and suffix (including separators) will appear truncated to 22 characters.
            """
            statement_descriptor_suffix_kanji: Optional[str]
            """
            Provides information about a card payment that customers see on their statements. Concatenated with the Kanji prefix (shortened Kanji descriptor) or Kanji statement descriptor that's set on the account to form the complete statement descriptor. Maximum 17 characters. On card statements, the *concatenation* of both prefix and suffix (including separators) will appear truncated to 17 characters.
            """
            _inner_class_types = {
                "installments": Installments,
                "mandate_options": MandateOptions,
            }

        class CardPresent(StripeObject):
            class Routing(StripeObject):
                requested_priority: Optional[
                    Literal["domestic", "international"]
                ]
                """
                Requested routing priority
                """

            request_extended_authorization: Optional[bool]
            """
            Request ability to capture this payment beyond the standard [authorization validity window](https://stripe.com/docs/terminal/features/extended-authorizations#authorization-validity)
            """
            request_incremental_authorization_support: Optional[bool]
            """
            Request ability to [increment](https://stripe.com/docs/terminal/features/incremental-authorizations) this PaymentIntent if the combination of MCC and card brand is eligible. Check [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported) in the [Confirm](https://stripe.com/docs/api/payment_intents/confirm) response to verify support.
            """
            routing: Optional[Routing]
            _inner_class_types = {"routing": Routing}

        class Cashapp(StripeObject):
            capture_method: Optional[Literal["manual"]]
            """
            Controls when the funds will be captured from the customer's account.
            """
            setup_future_usage: Optional[
                Literal["none", "off_session", "on_session"]
            ]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class CustomerBalance(StripeObject):
            class BankTransfer(StripeObject):
                class EuBankTransfer(StripeObject):
                    country: Literal["BE", "DE", "ES", "FR", "IE", "NL"]
                    """
                    The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
                    """

                eu_bank_transfer: Optional[EuBankTransfer]
                requested_address_types: Optional[
                    List[
                        Literal[
                            "aba",
                            "iban",
                            "sepa",
                            "sort_code",
                            "spei",
                            "swift",
                            "zengin",
                        ]
                    ]
                ]
                """
                List of address types that should be returned in the financial_addresses response. If not specified, all valid types will be returned.

                Permitted values include: `sort_code`, `zengin`, `iban`, or `spei`.
                """
                type: Optional[
                    Literal[
                        "eu_bank_transfer",
                        "gb_bank_transfer",
                        "jp_bank_transfer",
                        "mx_bank_transfer",
                        "us_bank_transfer",
                    ]
                ]
                """
                The bank transfer type that this PaymentIntent is allowed to use for funding Permitted values include: `eu_bank_transfer`, `gb_bank_transfer`, `jp_bank_transfer`, `mx_bank_transfer`, or `us_bank_transfer`.
                """
                _inner_class_types = {"eu_bank_transfer": EuBankTransfer}

            bank_transfer: Optional[BankTransfer]
            funding_type: Optional[Literal["bank_transfer"]]
            """
            The funding method type to be used when there are not enough funds in the customer balance. Permitted values include: `bank_transfer`.
            """
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """
            _inner_class_types = {"bank_transfer": BankTransfer}

        class Eps(StripeObject):
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Fpx(StripeObject):
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Giropay(StripeObject):
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Grabpay(StripeObject):
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Ideal(StripeObject):
            setup_future_usage: Optional[Literal["none", "off_session"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class InteracPresent(StripeObject):
            pass

        class Klarna(StripeObject):
            capture_method: Optional[Literal["manual"]]
            """
            Controls when the funds will be captured from the customer's account.
            """
            preferred_locale: Optional[str]
            """
            Preferred locale of the Klarna checkout page that the customer is redirected to.
            """
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Konbini(StripeObject):
            confirmation_number: Optional[str]
            """
            An optional 10 to 11 digit numeric-only string determining the confirmation code at applicable convenience stores.
            """
            expires_after_days: Optional[int]
            """
            The number of calendar days (between 1 and 60) after which Konbini payment instructions will expire. For example, if a PaymentIntent is confirmed with Konbini and `expires_after_days` set to 2 on Monday JST, the instructions will expire on Wednesday 23:59:59 JST.
            """
            expires_at: Optional[int]
            """
            The timestamp at which the Konbini payment instructions will expire. Only one of `expires_after_days` or `expires_at` may be set.
            """
            product_description: Optional[str]
            """
            A product descriptor of up to 22 characters, which will appear to customers at the convenience store.
            """
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Link(StripeObject):
            capture_method: Optional[Literal["manual"]]
            """
            Controls when the funds will be captured from the customer's account.
            """
            persistent_token: Optional[str]
            """
            [Deprecated] This is a legacy parameter that no longer has any function.
            """
            setup_future_usage: Optional[Literal["none", "off_session"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Mobilepay(StripeObject):
            capture_method: Optional[Literal["manual"]]
            """
            Controls when the funds will be captured from the customer's account.
            """
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Multibanco(StripeObject):
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Oxxo(StripeObject):
            expires_after_days: int
            """
            The number of calendar days before an OXXO invoice expires. For example, if you create an OXXO invoice on Monday and you set expires_after_days to 2, the OXXO invoice will expire on Wednesday at 23:59 America/Mexico_City time.
            """
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class P24(StripeObject):
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Paynow(StripeObject):
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Paypal(StripeObject):
            capture_method: Optional[Literal["manual"]]
            """
            Controls when the funds will be captured from the customer's account.
            """
            preferred_locale: Optional[str]
            """
            Preferred locale of the PayPal checkout page that the customer is redirected to.
            """
            reference: Optional[str]
            """
            A reference of the PayPal transaction visible to customer which is mapped to PayPal's invoice ID. This must be a globally unique ID if you have configured in your PayPal settings to block multiple payments per invoice ID.
            """
            setup_future_usage: Optional[Literal["none", "off_session"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Pix(StripeObject):
            expires_after_seconds: Optional[int]
            """
            The number of seconds (between 10 and 1209600) after which Pix payment will expire.
            """
            expires_at: Optional[int]
            """
            The timestamp at which the Pix expires.
            """
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Promptpay(StripeObject):
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class RevolutPay(StripeObject):
            capture_method: Optional[Literal["manual"]]
            """
            Controls when the funds will be captured from the customer's account.
            """
            setup_future_usage: Optional[Literal["none", "off_session"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class SepaDebit(StripeObject):
            class MandateOptions(StripeObject):
                pass

            mandate_options: Optional[MandateOptions]
            setup_future_usage: Optional[
                Literal["none", "off_session", "on_session"]
            ]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """
            _inner_class_types = {"mandate_options": MandateOptions}

        class Sofort(StripeObject):
            preferred_language: Optional[
                Literal["de", "en", "es", "fr", "it", "nl", "pl"]
            ]
            """
            Preferred language of the SOFORT authorization page that the customer is redirected to.
            """
            setup_future_usage: Optional[Literal["none", "off_session"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Swish(StripeObject):
            reference: Optional[str]
            """
            The order ID displayed in the Swish app after the payment is authorized.
            """
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Twint(StripeObject):
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class UsBankAccount(StripeObject):
            class FinancialConnections(StripeObject):
                class Filters(StripeObject):
                    account_subcategories: Optional[
                        List[Literal["checking", "savings"]]
                    ]
                    """
                    The account subcategories to use to filter for possible accounts to link. Valid subcategories are `checking` and `savings`.
                    """

                filters: Optional[Filters]
                permissions: Optional[
                    List[
                        Literal[
                            "balances",
                            "ownership",
                            "payment_method",
                            "transactions",
                        ]
                    ]
                ]
                """
                The list of permissions to request. The `payment_method` permission must be included.
                """
                prefetch: Optional[
                    List[Literal["balances", "ownership", "transactions"]]
                ]
                """
                Data features requested to be retrieved upon account creation.
                """
                return_url: Optional[str]
                """
                For webview integrations only. Upon completing OAuth login in the native browser, the user will be redirected to this URL to return to your app.
                """
                _inner_class_types = {"filters": Filters}

            class MandateOptions(StripeObject):
                collection_method: Optional[Literal["paper"]]
                """
                Mandate collection method
                """

            financial_connections: Optional[FinancialConnections]
            mandate_options: Optional[MandateOptions]
            preferred_settlement_speed: Optional[
                Literal["fastest", "standard"]
            ]
            """
            Preferred transaction settlement speed
            """
            setup_future_usage: Optional[
                Literal["none", "off_session", "on_session"]
            ]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """
            verification_method: Optional[
                Literal["automatic", "instant", "microdeposits"]
            ]
            """
            Bank account verification method.
            """
            _inner_class_types = {
                "financial_connections": FinancialConnections,
                "mandate_options": MandateOptions,
            }

        class WechatPay(StripeObject):
            app_id: Optional[str]
            """
            The app ID registered with WeChat Pay. Only required when client is ios or android.
            """
            client: Optional[Literal["android", "ios", "web"]]
            """
            The client type that the end customer will pay from
            """
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        class Zip(StripeObject):
            setup_future_usage: Optional[Literal["none"]]
            """
            Indicates that you intend to make future payments with this PaymentIntent's payment method.

            If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

            If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

            When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
            """

        acss_debit: Optional[AcssDebit]
        affirm: Optional[Affirm]
        afterpay_clearpay: Optional[AfterpayClearpay]
        alipay: Optional[Alipay]
        amazon_pay: Optional[AmazonPay]
        au_becs_debit: Optional[AuBecsDebit]
        bacs_debit: Optional[BacsDebit]
        bancontact: Optional[Bancontact]
        blik: Optional[Blik]
        boleto: Optional[Boleto]
        card: Optional[Card]
        card_present: Optional[CardPresent]
        cashapp: Optional[Cashapp]
        customer_balance: Optional[CustomerBalance]
        eps: Optional[Eps]
        fpx: Optional[Fpx]
        giropay: Optional[Giropay]
        grabpay: Optional[Grabpay]
        ideal: Optional[Ideal]
        interac_present: Optional[InteracPresent]
        klarna: Optional[Klarna]
        konbini: Optional[Konbini]
        link: Optional[Link]
        mobilepay: Optional[Mobilepay]
        multibanco: Optional[Multibanco]
        oxxo: Optional[Oxxo]
        p24: Optional[P24]
        paynow: Optional[Paynow]
        paypal: Optional[Paypal]
        pix: Optional[Pix]
        promptpay: Optional[Promptpay]
        revolut_pay: Optional[RevolutPay]
        sepa_debit: Optional[SepaDebit]
        sofort: Optional[Sofort]
        swish: Optional[Swish]
        twint: Optional[Twint]
        us_bank_account: Optional[UsBankAccount]
        wechat_pay: Optional[WechatPay]
        zip: Optional[Zip]
        _inner_class_types = {
            "acss_debit": AcssDebit,
            "affirm": Affirm,
            "afterpay_clearpay": AfterpayClearpay,
            "alipay": Alipay,
            "amazon_pay": AmazonPay,
            "au_becs_debit": AuBecsDebit,
            "bacs_debit": BacsDebit,
            "bancontact": Bancontact,
            "blik": Blik,
            "boleto": Boleto,
            "card": Card,
            "card_present": CardPresent,
            "cashapp": Cashapp,
            "customer_balance": CustomerBalance,
            "eps": Eps,
            "fpx": Fpx,
            "giropay": Giropay,
            "grabpay": Grabpay,
            "ideal": Ideal,
            "interac_present": InteracPresent,
            "klarna": Klarna,
            "konbini": Konbini,
            "link": Link,
            "mobilepay": Mobilepay,
            "multibanco": Multibanco,
            "oxxo": Oxxo,
            "p24": P24,
            "paynow": Paynow,
            "paypal": Paypal,
            "pix": Pix,
            "promptpay": Promptpay,
            "revolut_pay": RevolutPay,
            "sepa_debit": SepaDebit,
            "sofort": Sofort,
            "swish": Swish,
            "twint": Twint,
            "us_bank_account": UsBankAccount,
            "wechat_pay": WechatPay,
            "zip": Zip,
        }

    class Processing(StripeObject):
        class Card(StripeObject):
            class CustomerNotification(StripeObject):
                approval_requested: Optional[bool]
                """
                Whether customer approval has been requested for this payment. For payments greater than INR 15000 or mandate amount, the customer must provide explicit approval of the payment with their bank.
                """
                completes_at: Optional[int]
                """
                If customer approval is required, they need to provide approval before this time.
                """

            customer_notification: Optional[CustomerNotification]
            _inner_class_types = {
                "customer_notification": CustomerNotification
            }

        card: Optional[Card]
        type: Literal["card"]
        """
        Type of the payment method for which payment is in `processing` state, one of `card`.
        """
        _inner_class_types = {"card": Card}

    class Shipping(StripeObject):
        class Address(StripeObject):
            city: Optional[str]
            """
            City, district, suburb, town, or village.
            """
            country: Optional[str]
            """
            Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
            """
            line1: Optional[str]
            """
            Address line 1 (e.g., street, PO Box, or company name).
            """
            line2: Optional[str]
            """
            Address line 2 (e.g., apartment, suite, unit, or building).
            """
            postal_code: Optional[str]
            """
            ZIP or postal code.
            """
            state: Optional[str]
            """
            State, county, province, or region.
            """

        address: Optional[Address]
        carrier: Optional[str]
        """
        The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc.
        """
        name: Optional[str]
        """
        Recipient name.
        """
        phone: Optional[str]
        """
        Recipient phone (including extension).
        """
        tracking_number: Optional[str]
        """
        The tracking number for a physical product, obtained from the delivery service. If multiple tracking numbers were generated for this purchase, please separate them with commas.
        """
        _inner_class_types = {"address": Address}

    class TransferData(StripeObject):
        amount: Optional[int]
        """
        Amount intended to be collected by this PaymentIntent. A positive integer representing how much to charge in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) (e.g., 100 cents to charge $1.00 or 100 to charge ¥100, a zero-decimal currency). The minimum amount is $0.50 US or [equivalent in charge currency](https://stripe.com/docs/currencies#minimum-and-maximum-charge-amounts). The amount value supports up to eight digits (e.g., a value of 99999999 for a USD charge of $999,999.99).
        """
        destination: ExpandableField["Account"]
        """
        The account (if any) that the payment is attributed to for tax
        reporting, and where funds from the payment are transferred to after
        payment success.
        """

    class ApplyCustomerBalanceParams(RequestOptions):
        amount: NotRequired[int]
        """
        Amount that you intend to apply to this PaymentIntent from the customer's cash balance.

        A positive integer representing how much to charge in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) (for example, 100 cents to charge 1 USD or 100 to charge 100 JPY, a zero-decimal currency).

        The maximum amount is the amount of the PaymentIntent.

        When you omit the amount, it defaults to the remaining amount requested on the PaymentIntent.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CancelParams(RequestOptions):
        cancellation_reason: NotRequired[
            Literal[
                "abandoned", "duplicate", "fraudulent", "requested_by_customer"
            ]
        ]
        """
        Reason for canceling this PaymentIntent. Possible values are: `duplicate`, `fraudulent`, `requested_by_customer`, or `abandoned`
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CaptureParams(RequestOptions):
        amount_to_capture: NotRequired[int]
        """
        The amount to capture from the PaymentIntent, which must be less than or equal to the original amount. Any additional amount is automatically refunded. Defaults to the full `amount_capturable` if it's not provided.
        """
        application_fee_amount: NotRequired[int]
        """
        The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. The amount of the application fee collected will be capped at the total payment amount. For more information, see the PaymentIntents [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        final_capture: NotRequired[bool]
        """
        Defaults to `true`. When capturing a PaymentIntent, setting `final_capture` to `false` notifies Stripe to not release the remaining uncaptured funds to make sure that they're captured in future requests. You can only use this setting when [multicapture](https://stripe.com/docs/payments/multicapture) is available for PaymentIntents.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        statement_descriptor: NotRequired[str]
        """
        Text that appears on the customer's statement as the [statement descriptor](https://docs.stripe.com/get-started/account/statement-descriptors) for a non-card charge. This value overrides the account's default statement descriptor. Setting this value for a card charge returns an error. For card charges, set the [statement_descriptor_suffix](https://docs.stripe.com/get-started/account/statement-descriptors#dynamic) instead.
        """
        statement_descriptor_suffix: NotRequired[str]
        """
        Provides information about a card charge. Concatenated to the account's [statement descriptor prefix](https://docs.corp.stripe.com/get-started/account/statement-descriptors#static) to form the complete statement descriptor that appears on the customer's statement.
        """
        transfer_data: NotRequired["PaymentIntent.CaptureParamsTransferData"]
        """
        The parameters that you can use to automatically create a transfer after the payment
        is captured. Learn more about the [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """

    class CaptureParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount that will be transferred automatically when a charge succeeds.
        """

    class ConfirmParams(RequestOptions):
        capture_method: NotRequired[
            Literal["automatic", "automatic_async", "manual"]
        ]
        """
        Controls when the funds will be captured from the customer's account.
        """
        confirmation_token: NotRequired[str]
        """
        ID of the ConfirmationToken used to confirm this PaymentIntent.

        If the provided ConfirmationToken contains properties that are also being provided in this request, such as `payment_method`, then the values in this request will take precedence.
        """
        error_on_requires_action: NotRequired[bool]
        """
        Set to `true` to fail the payment attempt if the PaymentIntent transitions into `requires_action`. This parameter is intended for simpler integrations that do not handle customer actions, like [saving cards without authentication](https://stripe.com/docs/payments/save-card-without-authentication).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        mandate: NotRequired[str]
        """
        ID of the mandate that's used for this payment.
        """
        mandate_data: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsMandateData"
        ]
        off_session: NotRequired["bool|Literal['one_off', 'recurring']"]
        """
        Set to `true` to indicate that the customer isn't in your checkout flow during this payment attempt and can't authenticate. Use this parameter in scenarios where you collect card details and [charge them later](https://stripe.com/docs/payments/cards/charging-saved-cards).
        """
        payment_method: NotRequired[str]
        """
        ID of the payment method (a PaymentMethod, Card, or [compatible Source](https://stripe.com/docs/payments/payment-methods/transitioning#compatibility) object) to attach to this PaymentIntent.
        """
        payment_method_data: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodData"
        ]
        """
        If provided, this hash will be used to create a PaymentMethod. The new PaymentMethod will appear
        in the [payment_method](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-payment_method)
        property on the PaymentIntent.
        """
        payment_method_options: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptions"
        ]
        """
        Payment method-specific configuration for this PaymentIntent.
        """
        payment_method_types: NotRequired[List[str]]
        """
        The list of payment method types (for example, a card) that this PaymentIntent can use. Use `automatic_payment_methods` to manage payment methods from the [Stripe Dashboard](https://dashboard.stripe.com/settings/payment_methods).
        """
        radar_options: NotRequired["PaymentIntent.ConfirmParamsRadarOptions"]
        """
        Options to configure Radar. Learn more about [Radar Sessions](https://stripe.com/docs/radar/radar-session).
        """
        receipt_email: NotRequired["Literal['']|str"]
        """
        Email address that the receipt for the resulting payment will be sent to. If `receipt_email` is specified for a payment in live mode, a receipt will be sent regardless of your [email settings](https://dashboard.stripe.com/account/emails).
        """
        return_url: NotRequired[str]
        """
        The URL to redirect your customer back to after they authenticate or cancel their payment on the payment method's app or site.
        If you'd prefer to redirect to a mobile application, you can alternatively supply an application URI scheme.
        This parameter is only used for cards and other redirect-based payment methods.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        shipping: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsShipping"
        ]
        """
        Shipping information for this PaymentIntent.
        """
        use_stripe_sdk: NotRequired[bool]
        """
        Set to `true` when confirming server-side and using Stripe.js, iOS, or Android client-side SDKs to handle the next actions.
        """

    class ConfirmParamsMandateData(TypedDict):
        customer_acceptance: NotRequired[
            "PaymentIntent.ConfirmParamsMandateDataCustomerAcceptance"
        ]
        """
        This hash contains details about the customer acceptance of the Mandate.
        """

    class ConfirmParamsMandateDataCustomerAcceptance(TypedDict):
        accepted_at: NotRequired[int]
        """
        The time at which the customer accepted the Mandate.
        """
        offline: NotRequired[
            "PaymentIntent.ConfirmParamsMandateDataCustomerAcceptanceOffline"
        ]
        """
        If this is a Mandate accepted offline, this hash contains details about the offline acceptance.
        """
        online: NotRequired[
            "PaymentIntent.ConfirmParamsMandateDataCustomerAcceptanceOnline"
        ]
        """
        If this is a Mandate accepted online, this hash contains details about the online acceptance.
        """
        type: Literal["offline", "online"]
        """
        The type of customer acceptance information included with the Mandate. One of `online` or `offline`.
        """

    class ConfirmParamsMandateDataCustomerAcceptanceOffline(TypedDict):
        pass

    class ConfirmParamsMandateDataCustomerAcceptanceOnline(TypedDict):
        ip_address: NotRequired[str]
        """
        The IP address from which the Mandate was accepted by the customer.
        """
        user_agent: NotRequired[str]
        """
        The user agent of the browser from which the Mandate was accepted by the customer.
        """

    class ConfirmParamsPaymentMethodData(TypedDict):
        acss_debit: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataAcssDebit"
        ]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataAlipay"
        ]
        """
        If this is an `Alipay` PaymentMethod, this hash contains details about the Alipay payment method.
        """
        allow_redisplay: NotRequired[
            Literal["always", "limited", "unspecified"]
        ]
        """
        This field indicates whether this payment method can be shown again to its customer in a checkout flow. Stripe products such as Checkout and Elements use this field to determine whether a payment method can be shown as a saved payment method in a checkout flow. The field defaults to `unspecified`.
        """
        amazon_pay: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataAmazonPay"
        ]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired["PaymentIntent.ConfirmParamsPaymentMethodDataBlik"]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        cashapp: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer_balance: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired["PaymentIntent.ConfirmParamsPaymentMethodDataEps"]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        fpx: NotRequired["PaymentIntent.ConfirmParamsPaymentMethodDataFpx"]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired["PaymentIntent.ConfirmParamsPaymentMethodDataIdeal"]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired["PaymentIntent.ConfirmParamsPaymentMethodDataLink"]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataMobilepay"
        ]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired["PaymentIntent.ConfirmParamsPaymentMethodDataOxxo"]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired["PaymentIntent.ConfirmParamsPaymentMethodDataP24"]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        paynow: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired["PaymentIntent.ConfirmParamsPaymentMethodDataPix"]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataRevolutPay"
        ]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired["PaymentIntent.ConfirmParamsPaymentMethodDataSwish"]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired["PaymentIntent.ConfirmParamsPaymentMethodDataTwint"]
        """
        If this is a TWINT PaymentMethod, this hash contains details about the TWINT payment method.
        """
        type: Literal[
            "acss_debit",
            "affirm",
            "afterpay_clearpay",
            "alipay",
            "amazon_pay",
            "au_becs_debit",
            "bacs_debit",
            "bancontact",
            "blik",
            "boleto",
            "cashapp",
            "customer_balance",
            "eps",
            "fpx",
            "giropay",
            "grabpay",
            "ideal",
            "klarna",
            "konbini",
            "link",
            "mobilepay",
            "multibanco",
            "oxxo",
            "p24",
            "paynow",
            "paypal",
            "pix",
            "promptpay",
            "revolut_pay",
            "sepa_debit",
            "sofort",
            "swish",
            "twint",
            "us_bank_account",
            "wechat_pay",
            "zip",
        ]
        """
        The type of the PaymentMethod. An additional hash is included on the PaymentMethod with a name matching this value. It contains additional information specific to the PaymentMethod type.
        """
        us_bank_account: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataWechatPay"
        ]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired["PaymentIntent.ConfirmParamsPaymentMethodDataZip"]
        """
        If this is a `zip` PaymentMethod, this hash contains details about the Zip payment method.
        """

    class ConfirmParamsPaymentMethodDataAcssDebit(TypedDict):
        account_number: str
        """
        Customer's bank account number.
        """
        institution_number: str
        """
        Institution number of the customer's bank.
        """
        transit_number: str
        """
        Transit number of the customer's bank.
        """

    class ConfirmParamsPaymentMethodDataAffirm(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataAfterpayClearpay(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataAlipay(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataAmazonPay(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataAuBecsDebit(TypedDict):
        account_number: str
        """
        The account number for the bank account.
        """
        bsb_number: str
        """
        Bank-State-Branch number of the bank account.
        """

    class ConfirmParamsPaymentMethodDataBacsDebit(TypedDict):
        account_number: NotRequired[str]
        """
        Account number of the bank account that the funds will be debited from.
        """
        sort_code: NotRequired[str]
        """
        Sort code of the bank account. (e.g., `10-20-30`)
        """

    class ConfirmParamsPaymentMethodDataBancontact(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataBillingDetails(TypedDict):
        address: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodDataBillingDetailsAddress"
        ]
        """
        Billing address.
        """
        email: NotRequired["Literal['']|str"]
        """
        Email address.
        """
        name: NotRequired["Literal['']|str"]
        """
        Full name.
        """
        phone: NotRequired["Literal['']|str"]
        """
        Billing phone number (including extension).
        """

    class ConfirmParamsPaymentMethodDataBillingDetailsAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class ConfirmParamsPaymentMethodDataBlik(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataBoleto(TypedDict):
        tax_id: str
        """
        The tax ID of the customer (CPF for individual consumers or CNPJ for businesses consumers)
        """

    class ConfirmParamsPaymentMethodDataCashapp(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataCustomerBalance(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataEps(TypedDict):
        bank: NotRequired[
            Literal[
                "arzte_und_apotheker_bank",
                "austrian_anadi_bank_ag",
                "bank_austria",
                "bankhaus_carl_spangler",
                "bankhaus_schelhammer_und_schattera_ag",
                "bawag_psk_ag",
                "bks_bank_ag",
                "brull_kallmus_bank_ag",
                "btv_vier_lander_bank",
                "capital_bank_grawe_gruppe_ag",
                "deutsche_bank_ag",
                "dolomitenbank",
                "easybank_ag",
                "erste_bank_und_sparkassen",
                "hypo_alpeadriabank_international_ag",
                "hypo_bank_burgenland_aktiengesellschaft",
                "hypo_noe_lb_fur_niederosterreich_u_wien",
                "hypo_oberosterreich_salzburg_steiermark",
                "hypo_tirol_bank_ag",
                "hypo_vorarlberg_bank_ag",
                "marchfelder_bank",
                "oberbank_ag",
                "raiffeisen_bankengruppe_osterreich",
                "schoellerbank_ag",
                "sparda_bank_wien",
                "volksbank_gruppe",
                "volkskreditbank_ag",
                "vr_bank_braunau",
            ]
        ]
        """
        The customer's bank.
        """

    class ConfirmParamsPaymentMethodDataFpx(TypedDict):
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        Account holder type for FPX transaction
        """
        bank: Literal[
            "affin_bank",
            "agrobank",
            "alliance_bank",
            "ambank",
            "bank_islam",
            "bank_muamalat",
            "bank_of_china",
            "bank_rakyat",
            "bsn",
            "cimb",
            "deutsche_bank",
            "hong_leong_bank",
            "hsbc",
            "kfh",
            "maybank2e",
            "maybank2u",
            "ocbc",
            "pb_enterprise",
            "public_bank",
            "rhb",
            "standard_chartered",
            "uob",
        ]
        """
        The customer's bank.
        """

    class ConfirmParamsPaymentMethodDataGiropay(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataGrabpay(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataIdeal(TypedDict):
        bank: NotRequired[
            Literal[
                "abn_amro",
                "asn_bank",
                "bunq",
                "handelsbanken",
                "ing",
                "knab",
                "moneyou",
                "n26",
                "nn",
                "rabobank",
                "regiobank",
                "revolut",
                "sns_bank",
                "triodos_bank",
                "van_lanschot",
                "yoursafe",
            ]
        ]
        """
        The customer's bank.
        """

    class ConfirmParamsPaymentMethodDataInteracPresent(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataKlarna(TypedDict):
        dob: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodDataKlarnaDob"
        ]
        """
        Customer's date of birth
        """

    class ConfirmParamsPaymentMethodDataKlarnaDob(TypedDict):
        day: int
        """
        The day of birth, between 1 and 31.
        """
        month: int
        """
        The month of birth, between 1 and 12.
        """
        year: int
        """
        The four-digit year of birth.
        """

    class ConfirmParamsPaymentMethodDataKonbini(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataLink(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataMobilepay(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataMultibanco(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataOxxo(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataP24(TypedDict):
        bank: NotRequired[
            Literal[
                "alior_bank",
                "bank_millennium",
                "bank_nowy_bfg_sa",
                "bank_pekao_sa",
                "banki_spbdzielcze",
                "blik",
                "bnp_paribas",
                "boz",
                "citi_handlowy",
                "credit_agricole",
                "envelobank",
                "etransfer_pocztowy24",
                "getin_bank",
                "ideabank",
                "ing",
                "inteligo",
                "mbank_mtransfer",
                "nest_przelew",
                "noble_pay",
                "pbac_z_ipko",
                "plus_bank",
                "santander_przelew24",
                "tmobile_usbugi_bankowe",
                "toyota_bank",
                "velobank",
                "volkswagen_bank",
            ]
        ]
        """
        The customer's bank.
        """

    class ConfirmParamsPaymentMethodDataPaynow(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataPaypal(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataPix(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataPromptpay(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataRadarOptions(TypedDict):
        session: NotRequired[str]
        """
        A [Radar Session](https://stripe.com/docs/radar/radar-session) is a snapshot of the browser metadata and device details that help Radar make more accurate predictions on your payments.
        """

    class ConfirmParamsPaymentMethodDataRevolutPay(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataSepaDebit(TypedDict):
        iban: str
        """
        IBAN of the bank account.
        """

    class ConfirmParamsPaymentMethodDataSofort(TypedDict):
        country: Literal["AT", "BE", "DE", "ES", "IT", "NL"]
        """
        Two-letter ISO code representing the country the bank account is located in.
        """

    class ConfirmParamsPaymentMethodDataSwish(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataTwint(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataUsBankAccount(TypedDict):
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        Account holder type: individual or company.
        """
        account_number: NotRequired[str]
        """
        Account number of the bank account.
        """
        account_type: NotRequired[Literal["checking", "savings"]]
        """
        Account type: checkings or savings. Defaults to checking if omitted.
        """
        financial_connections_account: NotRequired[str]
        """
        The ID of a Financial Connections Account to use as a payment method.
        """
        routing_number: NotRequired[str]
        """
        Routing number of the bank account.
        """

    class ConfirmParamsPaymentMethodDataWechatPay(TypedDict):
        pass

    class ConfirmParamsPaymentMethodDataZip(TypedDict):
        pass

    class ConfirmParamsPaymentMethodOptions(TypedDict):
        acss_debit: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsAcssDebit"
        ]
        """
        If this is a `acss_debit` PaymentMethod, this sub-hash contains details about the ACSS Debit payment method options.
        """
        affirm: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this sub-hash contains details about the Affirm payment method options.
        """
        afterpay_clearpay: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsAfterpayClearpay"
        ]
        """
        If this is a `afterpay_clearpay` PaymentMethod, this sub-hash contains details about the Afterpay Clearpay payment method options.
        """
        alipay: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsAlipay"
        ]
        """
        If this is a `alipay` PaymentMethod, this sub-hash contains details about the Alipay payment method options.
        """
        amazon_pay: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsAmazonPay"
        ]
        """
        If this is a `amazon_pay` PaymentMethod, this sub-hash contains details about the Amazon Pay payment method options.
        """
        au_becs_debit: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsAuBecsDebit"
        ]
        """
        If this is a `au_becs_debit` PaymentMethod, this sub-hash contains details about the AU BECS Direct Debit payment method options.
        """
        bacs_debit: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this sub-hash contains details about the BACS Debit payment method options.
        """
        bancontact: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this sub-hash contains details about the Bancontact payment method options.
        """
        blik: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this sub-hash contains details about the BLIK payment method options.
        """
        boleto: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this sub-hash contains details about the Boleto payment method options.
        """
        card: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsCard"
        ]
        """
        Configuration for any card payments attempted on this PaymentIntent.
        """
        card_present: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsCardPresent"
        ]
        """
        If this is a `card_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        cashapp: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this sub-hash contains details about the Cash App Pay payment method options.
        """
        customer_balance: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsCustomerBalance"
        ]
        """
        If this is a `customer balance` PaymentMethod, this sub-hash contains details about the customer balance payment method options.
        """
        eps: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsEps"
        ]
        """
        If this is a `eps` PaymentMethod, this sub-hash contains details about the EPS payment method options.
        """
        fpx: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsFpx"
        ]
        """
        If this is a `fpx` PaymentMethod, this sub-hash contains details about the FPX payment method options.
        """
        giropay: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this sub-hash contains details about the Giropay payment method options.
        """
        grabpay: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this sub-hash contains details about the Grabpay payment method options.
        """
        ideal: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsIdeal"
        ]
        """
        If this is a `ideal` PaymentMethod, this sub-hash contains details about the Ideal payment method options.
        """
        interac_present: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsInteracPresent"
        ]
        """
        If this is a `interac_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        klarna: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this sub-hash contains details about the Klarna payment method options.
        """
        konbini: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this sub-hash contains details about the Konbini payment method options.
        """
        link: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsLink"
        ]
        """
        If this is a `link` PaymentMethod, this sub-hash contains details about the Link payment method options.
        """
        mobilepay: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsMobilepay"
        ]
        """
        If this is a `MobilePay` PaymentMethod, this sub-hash contains details about the MobilePay payment method options.
        """
        multibanco: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this sub-hash contains details about the Multibanco payment method options.
        """
        oxxo: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsOxxo"
        ]
        """
        If this is a `oxxo` PaymentMethod, this sub-hash contains details about the OXXO payment method options.
        """
        p24: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsP24"
        ]
        """
        If this is a `p24` PaymentMethod, this sub-hash contains details about the Przelewy24 payment method options.
        """
        paynow: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this sub-hash contains details about the PayNow payment method options.
        """
        paypal: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this sub-hash contains details about the PayPal payment method options.
        """
        pix: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsPix"
        ]
        """
        If this is a `pix` PaymentMethod, this sub-hash contains details about the Pix payment method options.
        """
        promptpay: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this sub-hash contains details about the PromptPay payment method options.
        """
        revolut_pay: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsRevolutPay"
        ]
        """
        If this is a `revolut_pay` PaymentMethod, this sub-hash contains details about the Revolut Pay payment method options.
        """
        sepa_debit: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentIntent, this sub-hash contains details about the SEPA Debit payment method options.
        """
        sofort: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this sub-hash contains details about the SOFORT payment method options.
        """
        swish: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsSwish"
        ]
        """
        If this is a `Swish` PaymentMethod, this sub-hash contains details about the Swish payment method options.
        """
        twint: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsTwint"
        ]
        """
        If this is a `twint` PaymentMethod, this sub-hash contains details about the TWINT payment method options.
        """
        us_bank_account: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If this is a `us_bank_account` PaymentMethod, this sub-hash contains details about the US bank account payment method options.
        """
        wechat_pay: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsWechatPay"
        ]
        """
        If this is a `wechat_pay` PaymentMethod, this sub-hash contains details about the WeChat Pay payment method options.
        """
        zip: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsZip"
        ]
        """
        If this is a `zip` PaymentMethod, this sub-hash contains details about the Zip payment method options.
        """

    class ConfirmParamsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsAcssDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Bank account verification method.
        """

    class ConfirmParamsPaymentMethodOptionsAcssDebitMandateOptions(TypedDict):
        custom_mandate_url: NotRequired["Literal['']|str"]
        """
        A URL for custom mandate text to render during confirmation step.
        The URL will be rendered with additional GET parameters `payment_intent` and `payment_intent_client_secret` when confirming a Payment Intent,
        or `setup_intent` and `setup_intent_client_secret` when confirming a Setup Intent.
        """
        interval_description: NotRequired[str]
        """
        Description of the mandate interval. Only required if 'payment_schedule' parameter is 'interval' or 'combined'.
        """
        payment_schedule: NotRequired[
            Literal["combined", "interval", "sporadic"]
        ]
        """
        Payment schedule for the mandate.
        """
        transaction_type: NotRequired[Literal["business", "personal"]]
        """
        Transaction type of the mandate.
        """

    class ConfirmParamsPaymentMethodOptionsAffirm(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        preferred_locale: NotRequired[str]
        """
        Preferred language of the Affirm authorization page that the customer is redirected to.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsAfterpayClearpay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        reference: NotRequired[str]
        """
        An internal identifier or reference that this payment corresponds to. You must limit the identifier to 128 characters, and it can only contain letters, numbers, underscores, backslashes, and dashes.
        This field differs from the statement descriptor and item name.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsAlipay(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsAmazonPay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class ConfirmParamsPaymentMethodOptionsAuBecsDebit(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsBacsDebit(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsBancontact(TypedDict):
        preferred_language: NotRequired[Literal["de", "en", "fr", "nl"]]
        """
        Preferred language of the Bancontact authorization page that the customer is redirected to.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsBlik(TypedDict):
        code: NotRequired[str]
        """
        The 6-digit BLIK code that a customer has generated using their banking application. Can only be set on confirmation.
        """
        setup_future_usage: NotRequired["Literal['']|Literal['none']"]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsBoleto(TypedDict):
        expires_after_days: NotRequired[int]
        """
        The number of calendar days before a Boleto voucher expires. For example, if you create a Boleto voucher on Monday and you set expires_after_days to 2, the Boleto invoice will expire on Wednesday at 23:59 America/Sao_Paulo time.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsCard(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        cvc_token: NotRequired[str]
        """
        A single-use `cvc_update` Token that represents a card CVC value. When provided, the CVC value will be verified during the card payment attempt. This parameter can only be provided during confirmation.
        """
        installments: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsCardInstallments"
        ]
        """
        Installment configuration for payments attempted on this PaymentIntent (Mexico Only).

        For more information, see the [installments integration guide](https://stripe.com/docs/payments/installments).
        """
        mandate_options: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsCardMandateOptions"
        ]
        """
        Configuration options for setting up an eMandate for cards issued in India.
        """
        moto: NotRequired[bool]
        """
        When specified, this parameter indicates that a transaction will be marked
        as MOTO (Mail Order Telephone Order) and thus out of scope for SCA. This
        parameter can only be provided during confirmation.
        """
        network: NotRequired[
            Literal[
                "amex",
                "cartes_bancaires",
                "diners",
                "discover",
                "eftpos_au",
                "girocard",
                "interac",
                "jcb",
                "mastercard",
                "unionpay",
                "unknown",
                "visa",
            ]
        ]
        """
        Selected network to process this PaymentIntent on. Depends on the available networks of the card attached to the PaymentIntent. Can be only set confirm-time.
        """
        request_extended_authorization: NotRequired[
            Literal["if_available", "never"]
        ]
        """
        Request ability to [capture beyond the standard authorization validity window](https://stripe.com/docs/payments/extended-authorization) for this PaymentIntent.
        """
        request_incremental_authorization: NotRequired[
            Literal["if_available", "never"]
        ]
        """
        Request ability to [increment the authorization](https://stripe.com/docs/payments/incremental-authorization) for this PaymentIntent.
        """
        request_multicapture: NotRequired[Literal["if_available", "never"]]
        """
        Request ability to make [multiple captures](https://stripe.com/docs/payments/multicapture) for this PaymentIntent.
        """
        request_overcapture: NotRequired[Literal["if_available", "never"]]
        """
        Request ability to [overcapture](https://stripe.com/docs/payments/overcapture) for this PaymentIntent.
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. If not provided, this value defaults to `automatic`. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """
        require_cvc_recollection: NotRequired[bool]
        """
        When enabled, using a card that is attached to a customer will require the CVC to be provided again (i.e. using the cvc_token parameter).
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        statement_descriptor_suffix_kana: NotRequired["Literal['']|str"]
        """
        Provides information about a card payment that customers see on their statements. Concatenated with the Kana prefix (shortened Kana descriptor) or Kana statement descriptor that's set on the account to form the complete statement descriptor. Maximum 22 characters. On card statements, the *concatenation* of both prefix and suffix (including separators) will appear truncated to 22 characters.
        """
        statement_descriptor_suffix_kanji: NotRequired["Literal['']|str"]
        """
        Provides information about a card payment that customers see on their statements. Concatenated with the Kanji prefix (shortened Kanji descriptor) or Kanji statement descriptor that's set on the account to form the complete statement descriptor. Maximum 17 characters. On card statements, the *concatenation* of both prefix and suffix (including separators) will appear truncated to 17 characters.
        """
        three_d_secure: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsCardThreeDSecure"
        ]
        """
        If 3D Secure authentication was performed with a third-party provider,
        the authentication details to use for this payment.
        """

    class ConfirmParamsPaymentMethodOptionsCardInstallments(TypedDict):
        enabled: NotRequired[bool]
        """
        Setting to true enables installments for this PaymentIntent.
        This will cause the response to contain a list of available installment plans.
        Setting to false will prevent any selected plan from applying to a charge.
        """
        plan: NotRequired[
            "Literal['']|PaymentIntent.ConfirmParamsPaymentMethodOptionsCardInstallmentsPlan"
        ]
        """
        The selected installment plan to use for this payment attempt.
        This parameter can only be provided during confirmation.
        """

    class ConfirmParamsPaymentMethodOptionsCardInstallmentsPlan(TypedDict):
        count: NotRequired[int]
        """
        For `fixed_count` installment plans, this is required. It represents the number of installment payments your customer will make to their credit card.
        """
        interval: NotRequired[Literal["month"]]
        """
        For `fixed_count` installment plans, this is required. It represents the interval between installment payments your customer will make to their credit card.
        One of `month`.
        """
        type: Literal["fixed_count"]
        """
        Type of installment plan, one of `fixed_count`.
        """

    class ConfirmParamsPaymentMethodOptionsCardMandateOptions(TypedDict):
        amount: int
        """
        Amount to be charged for future payments.
        """
        amount_type: Literal["fixed", "maximum"]
        """
        One of `fixed` or `maximum`. If `fixed`, the `amount` param refers to the exact amount to be charged in future payments. If `maximum`, the amount charged can be up to the value passed for the `amount` param.
        """
        description: NotRequired[str]
        """
        A description of the mandate or subscription that is meant to be displayed to the customer.
        """
        end_date: NotRequired[int]
        """
        End date of the mandate or subscription. If not provided, the mandate will be active until canceled. If provided, end date should be after start date.
        """
        interval: Literal["day", "month", "sporadic", "week", "year"]
        """
        Specifies payment frequency. One of `day`, `week`, `month`, `year`, or `sporadic`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between payments. For example, `interval=month` and `interval_count=3` indicates one payment every three months. Maximum of one year interval allowed (1 year, 12 months, or 52 weeks). This parameter is optional when `interval=sporadic`.
        """
        reference: str
        """
        Unique identifier for the mandate or subscription.
        """
        start_date: int
        """
        Start date of the mandate or subscription. Start date should not be lesser than yesterday.
        """
        supported_types: NotRequired[List[Literal["india"]]]
        """
        Specifies the type of mandates supported. Possible values are `india`.
        """

    class ConfirmParamsPaymentMethodOptionsCardPresent(TypedDict):
        request_extended_authorization: NotRequired[bool]
        """
        Request ability to capture this payment beyond the standard [authorization validity window](https://stripe.com/docs/terminal/features/extended-authorizations#authorization-validity)
        """
        request_incremental_authorization_support: NotRequired[bool]
        """
        Request ability to [increment](https://stripe.com/docs/terminal/features/incremental-authorizations) this PaymentIntent if the combination of MCC and card brand is eligible. Check [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported) in the [Confirm](https://stripe.com/docs/api/payment_intents/confirm) response to verify support.
        """
        routing: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsCardPresentRouting"
        ]
        """
        Network routing priority on co-branded EMV cards supporting domestic debit and international card schemes.
        """

    class ConfirmParamsPaymentMethodOptionsCardPresentRouting(TypedDict):
        requested_priority: NotRequired[Literal["domestic", "international"]]
        """
        Routing requested priority
        """

    class ConfirmParamsPaymentMethodOptionsCardThreeDSecure(TypedDict):
        ares_trans_status: NotRequired[
            Literal["A", "C", "I", "N", "R", "U", "Y"]
        ]
        """
        The `transStatus` returned from the card Issuer's ACS in the ARes.
        """
        cryptogram: str
        """
        The cryptogram, also known as the "authentication value" (AAV, CAVV or
        AEVV). This value is 20 bytes, base64-encoded into a 28-character string.
        (Most 3D Secure providers will return the base64-encoded version, which
        is what you should specify here.)
        """
        electronic_commerce_indicator: NotRequired[
            Literal["01", "02", "05", "06", "07"]
        ]
        """
        The Electronic Commerce Indicator (ECI) is returned by your 3D Secure
        provider and indicates what degree of authentication was performed.
        """
        exemption_indicator: NotRequired[Literal["low_risk", "none"]]
        """
        The exemption requested via 3DS and accepted by the issuer at authentication time.
        """
        network_options: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions"
        ]
        """
        Network specific 3DS fields. Network specific arguments require an
        explicit card brand choice. The parameter `payment_method_options.card.network``
        must be populated accordingly
        """
        requestor_challenge_indicator: NotRequired[str]
        """
        The challenge indicator (`threeDSRequestorChallengeInd`) which was requested in the
        AReq sent to the card Issuer's ACS. A string containing 2 digits from 01-99.
        """
        transaction_id: str
        """
        For 3D Secure 1, the XID. For 3D Secure 2, the Directory Server
        Transaction ID (dsTransID).
        """
        version: Literal["1.0.2", "2.1.0", "2.2.0"]
        """
        The version of 3D Secure that was performed.
        """

    class ConfirmParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions(
        TypedDict,
    ):
        cartes_bancaires: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires"
        ]
        """
        Cartes Bancaires-specific 3DS fields.
        """

    class ConfirmParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires(
        TypedDict,
    ):
        cb_avalgo: Literal["0", "1", "2", "3", "4", "A"]
        """
        The cryptogram calculation algorithm used by the card Issuer's ACS
        to calculate the Authentication cryptogram. Also known as `cavvAlgorithm`.
        messageExtension: CB-AVALGO
        """
        cb_exemption: NotRequired[str]
        """
        The exemption indicator returned from Cartes Bancaires in the ARes.
        message extension: CB-EXEMPTION; string (4 characters)
        This is a 3 byte bitmap (low significant byte first and most significant
        bit first) that has been Base64 encoded
        """
        cb_score: NotRequired[int]
        """
        The risk score returned from Cartes Bancaires in the ARes.
        message extension: CB-SCORE; numeric value 0-99
        """

    class ConfirmParamsPaymentMethodOptionsCashapp(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsCustomerBalance(TypedDict):
        bank_transfer: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsCustomerBalanceBankTransfer"
        ]
        """
        Configuration for the bank transfer funding type, if the `funding_type` is set to `bank_transfer`.
        """
        funding_type: NotRequired[Literal["bank_transfer"]]
        """
        The funding method type to be used when there are not enough funds in the customer balance. Permitted values include: `bank_transfer`.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsCustomerBalanceBankTransfer(
        TypedDict,
    ):
        eu_bank_transfer: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
        ]
        """
        Configuration for the eu_bank_transfer funding type.
        """
        requested_address_types: NotRequired[
            List[
                Literal[
                    "aba",
                    "iban",
                    "sepa",
                    "sort_code",
                    "spei",
                    "swift",
                    "zengin",
                ]
            ]
        ]
        """
        List of address types that should be returned in the financial_addresses response. If not specified, all valid types will be returned.

        Permitted values include: `sort_code`, `zengin`, `iban`, or `spei`.
        """
        type: Literal[
            "eu_bank_transfer",
            "gb_bank_transfer",
            "jp_bank_transfer",
            "mx_bank_transfer",
            "us_bank_transfer",
        ]
        """
        The list of bank transfer types that this PaymentIntent is allowed to use for funding Permitted values include: `eu_bank_transfer`, `gb_bank_transfer`, `jp_bank_transfer`, `mx_bank_transfer`, or `us_bank_transfer`.
        """

    class ConfirmParamsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer(
        TypedDict,
    ):
        country: str
        """
        The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
        """

    class ConfirmParamsPaymentMethodOptionsEps(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsFpx(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsGiropay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsGrabpay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsIdeal(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsInteracPresent(TypedDict):
        pass

    class ConfirmParamsPaymentMethodOptionsKlarna(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        preferred_locale: NotRequired[
            Literal[
                "cs-CZ",
                "da-DK",
                "de-AT",
                "de-CH",
                "de-DE",
                "el-GR",
                "en-AT",
                "en-AU",
                "en-BE",
                "en-CA",
                "en-CH",
                "en-CZ",
                "en-DE",
                "en-DK",
                "en-ES",
                "en-FI",
                "en-FR",
                "en-GB",
                "en-GR",
                "en-IE",
                "en-IT",
                "en-NL",
                "en-NO",
                "en-NZ",
                "en-PL",
                "en-PT",
                "en-RO",
                "en-SE",
                "en-US",
                "es-ES",
                "es-US",
                "fi-FI",
                "fr-BE",
                "fr-CA",
                "fr-CH",
                "fr-FR",
                "it-CH",
                "it-IT",
                "nb-NO",
                "nl-BE",
                "nl-NL",
                "pl-PL",
                "pt-PT",
                "ro-RO",
                "sv-FI",
                "sv-SE",
            ]
        ]
        """
        Preferred language of the Klarna authorization page that the customer is redirected to
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsKonbini(TypedDict):
        confirmation_number: NotRequired["Literal['']|str"]
        """
        An optional 10 to 11 digit numeric-only string determining the confirmation code at applicable convenience stores. Must not consist of only zeroes and could be rejected in case of insufficient uniqueness. We recommend to use the customer's phone number.
        """
        expires_after_days: NotRequired["Literal['']|int"]
        """
        The number of calendar days (between 1 and 60) after which Konbini payment instructions will expire. For example, if a PaymentIntent is confirmed with Konbini and `expires_after_days` set to 2 on Monday JST, the instructions will expire on Wednesday 23:59:59 JST. Defaults to 3 days.
        """
        expires_at: NotRequired["Literal['']|int"]
        """
        The timestamp at which the Konbini payment instructions will expire. Only one of `expires_after_days` or `expires_at` may be set.
        """
        product_description: NotRequired["Literal['']|str"]
        """
        A product descriptor of up to 22 characters, which will appear to customers at the convenience store.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsLink(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        persistent_token: NotRequired[str]
        """
        [Deprecated] This is a legacy parameter that no longer has any function.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsMobilepay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsMultibanco(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsOxxo(TypedDict):
        expires_after_days: NotRequired[int]
        """
        The number of calendar days before an OXXO voucher expires. For example, if you create an OXXO voucher on Monday and you set expires_after_days to 2, the OXXO invoice will expire on Wednesday at 23:59 America/Mexico_City time.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsP24(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        tos_shown_and_accepted: NotRequired[bool]
        """
        Confirm that the payer has accepted the P24 terms and conditions.
        """

    class ConfirmParamsPaymentMethodOptionsPaynow(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsPaypal(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds will be captured from the customer's account.
        """
        preferred_locale: NotRequired[
            Literal[
                "cs-CZ",
                "da-DK",
                "de-AT",
                "de-DE",
                "de-LU",
                "el-GR",
                "en-GB",
                "en-US",
                "es-ES",
                "fi-FI",
                "fr-BE",
                "fr-FR",
                "fr-LU",
                "hu-HU",
                "it-IT",
                "nl-BE",
                "nl-NL",
                "pl-PL",
                "pt-PT",
                "sk-SK",
                "sv-SE",
            ]
        ]
        """
        [Preferred locale](https://stripe.com/docs/payments/paypal/supported-locales) of the PayPal checkout page that the customer is redirected to.
        """
        reference: NotRequired[str]
        """
        A reference of the PayPal transaction visible to customer which is mapped to PayPal's invoice ID. This must be a globally unique ID if you have configured in your PayPal settings to block multiple payments per invoice ID.
        """
        risk_correlation_id: NotRequired[str]
        """
        The risk correlation ID for an on-session payment using a saved PayPal payment method.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsPix(TypedDict):
        expires_after_seconds: NotRequired[int]
        """
        The number of seconds (between 10 and 1209600) after which Pix payment will expire. Defaults to 86400 seconds.
        """
        expires_at: NotRequired[int]
        """
        The timestamp at which the Pix expires (between 10 and 1209600 seconds in the future). Defaults to 1 day in the future.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsPromptpay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsRevolutPay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class ConfirmParamsPaymentMethodOptionsSepaDebit(TypedDict):
        mandate_options: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsSepaDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsSepaDebitMandateOptions(TypedDict):
        pass

    class ConfirmParamsPaymentMethodOptionsSofort(TypedDict):
        preferred_language: NotRequired[
            "Literal['']|Literal['de', 'en', 'es', 'fr', 'it', 'nl', 'pl']"
        ]
        """
        Language shown to the payer on redirect.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsSwish(TypedDict):
        reference: NotRequired["Literal['']|str"]
        """
        The order ID displayed in the Swish app after the payment is authorized.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsTwint(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsUsBankAccount(TypedDict):
        financial_connections: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        mandate_options: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsUsBankAccountMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        networks: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsUsBankAccountNetworks"
        ]
        """
        Additional fields for network related functions
        """
        preferred_settlement_speed: NotRequired[
            "Literal['']|Literal['fastest', 'standard']"
        ]
        """
        Preferred transaction settlement speed
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Bank account verification method.
        """

    class ConfirmParamsPaymentMethodOptionsUsBankAccountFinancialConnections(
        TypedDict,
    ):
        filters: NotRequired[
            "PaymentIntent.ConfirmParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
        ]
        """
        Provide filters for the linked accounts that the customer can select for the payment method
        """
        permissions: NotRequired[
            List[
                Literal[
                    "balances", "ownership", "payment_method", "transactions"
                ]
            ]
        ]
        """
        The list of permissions to request. If this parameter is passed, the `payment_method` permission must be included. Valid permissions include: `balances`, `ownership`, `payment_method`, and `transactions`.
        """
        prefetch: NotRequired[
            List[Literal["balances", "ownership", "transactions"]]
        ]
        """
        List of data features that you would like to retrieve upon account creation.
        """
        return_url: NotRequired[str]
        """
        For webview integrations only. Upon completing OAuth login in the native browser, the user will be redirected to this URL to return to your app.
        """

    class ConfirmParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters(
        TypedDict,
    ):
        account_subcategories: NotRequired[
            List[Literal["checking", "savings"]]
        ]
        """
        The account subcategories to use to filter for selectable accounts. Valid subcategories are `checking` and `savings`.
        """

    class ConfirmParamsPaymentMethodOptionsUsBankAccountMandateOptions(
        TypedDict,
    ):
        collection_method: NotRequired["Literal['']|Literal['paper']"]
        """
        The method used to collect offline mandate customer acceptance.
        """

    class ConfirmParamsPaymentMethodOptionsUsBankAccountNetworks(TypedDict):
        requested: NotRequired[List[Literal["ach", "us_domestic_wire"]]]
        """
        Triggers validations to run across the selected networks
        """

    class ConfirmParamsPaymentMethodOptionsWechatPay(TypedDict):
        app_id: NotRequired[str]
        """
        The app ID registered with WeChat Pay. Only required when client is ios or android.
        """
        client: Literal["android", "ios", "web"]
        """
        The client type that the end customer will pay from
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsPaymentMethodOptionsZip(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ConfirmParamsRadarOptions(TypedDict):
        session: NotRequired[str]
        """
        A [Radar Session](https://stripe.com/docs/radar/radar-session) is a snapshot of the browser metadata and device details that help Radar make more accurate predictions on your payments.
        """

    class ConfirmParamsShipping(TypedDict):
        address: "PaymentIntent.ConfirmParamsShippingAddress"
        """
        Shipping address.
        """
        carrier: NotRequired[str]
        """
        The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc.
        """
        name: str
        """
        Recipient name.
        """
        phone: NotRequired[str]
        """
        Recipient phone (including extension).
        """
        tracking_number: NotRequired[str]
        """
        The tracking number for a physical product, obtained from the delivery service. If multiple tracking numbers were generated for this purchase, please separate them with commas.
        """

    class ConfirmParamsShippingAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class CreateParams(RequestOptions):
        amount: int
        """
        Amount intended to be collected by this PaymentIntent. A positive integer representing how much to charge in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) (e.g., 100 cents to charge $1.00 or 100 to charge ¥100, a zero-decimal currency). The minimum amount is $0.50 US or [equivalent in charge currency](https://stripe.com/docs/currencies#minimum-and-maximum-charge-amounts). The amount value supports up to eight digits (e.g., a value of 99999999 for a USD charge of $999,999.99).
        """
        application_fee_amount: NotRequired[int]
        """
        The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. The amount of the application fee collected will be capped at the total payment amount. For more information, see the PaymentIntents [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """
        automatic_payment_methods: NotRequired[
            "PaymentIntent.CreateParamsAutomaticPaymentMethods"
        ]
        """
        When you enable this parameter, this PaymentIntent accepts payment methods that you enable in the Dashboard and that are compatible with this PaymentIntent's other parameters.
        """
        capture_method: NotRequired[
            Literal["automatic", "automatic_async", "manual"]
        ]
        """
        Controls when the funds will be captured from the customer's account.
        """
        confirm: NotRequired[bool]
        """
        Set to `true` to attempt to [confirm this PaymentIntent](https://stripe.com/docs/api/payment_intents/confirm) immediately. This parameter defaults to `false`. When creating and confirming a PaymentIntent at the same time, you can also provide the parameters available in the [Confirm API](https://stripe.com/docs/api/payment_intents/confirm).
        """
        confirmation_method: NotRequired[Literal["automatic", "manual"]]
        """
        Describes whether we can confirm this PaymentIntent automatically, or if it requires customer action to confirm the payment.
        """
        confirmation_token: NotRequired[str]
        """
        ID of the ConfirmationToken used to confirm this PaymentIntent.

        If the provided ConfirmationToken contains properties that are also being provided in this request, such as `payment_method`, then the values in this request will take precedence.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        customer: NotRequired[str]
        """
        ID of the Customer this PaymentIntent belongs to, if one exists.

        Payment methods attached to other Customers cannot be used with this PaymentIntent.

        If [setup_future_usage](https://stripe.com/docs/api#payment_intent_object-setup_future_usage) is set and this PaymentIntent's payment method is not `card_present`, then the payment method attaches to the Customer after the PaymentIntent has been confirmed and any required actions from the user are complete. If the payment method is `card_present` and isn't a digital wallet, then a [generated_card](https://docs.corp.stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card is created and attached to the Customer instead.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        error_on_requires_action: NotRequired[bool]
        """
        Set to `true` to fail the payment attempt if the PaymentIntent transitions into `requires_action`. Use this parameter for simpler integrations that don't handle customer actions, such as [saving cards without authentication](https://stripe.com/docs/payments/save-card-without-authentication). This parameter can only be used with [`confirm=true`](https://stripe.com/docs/api/payment_intents/create#create_payment_intent-confirm).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        mandate: NotRequired[str]
        """
        ID of the mandate that's used for this payment. This parameter can only be used with [`confirm=true`](https://stripe.com/docs/api/payment_intents/create#create_payment_intent-confirm).
        """
        mandate_data: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsMandateData"
        ]
        """
        This hash contains details about the Mandate to create. This parameter can only be used with [`confirm=true`](https://stripe.com/docs/api/payment_intents/create#create_payment_intent-confirm).
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        off_session: NotRequired["bool|Literal['one_off', 'recurring']"]
        """
        Set to `true` to indicate that the customer isn't in your checkout flow during this payment attempt and can't authenticate. Use this parameter in scenarios where you collect card details and [charge them later](https://stripe.com/docs/payments/cards/charging-saved-cards). This parameter can only be used with [`confirm=true`](https://stripe.com/docs/api/payment_intents/create#create_payment_intent-confirm).
        """
        on_behalf_of: NotRequired[str]
        """
        The Stripe account ID that these funds are intended for. Learn more about the [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """
        payment_method: NotRequired[str]
        """
        ID of the payment method (a PaymentMethod, Card, or [compatible Source](https://stripe.com/docs/payments/payment-methods#compatibility) object) to attach to this PaymentIntent.

        If you don't provide the `payment_method` parameter or the `source` parameter with `confirm=true`, `source` automatically populates with `customer.default_source` to improve migration for users of the Charges API. We recommend that you explicitly provide the `payment_method` moving forward.
        """
        payment_method_configuration: NotRequired[str]
        """
        The ID of the payment method configuration to use with this PaymentIntent.
        """
        payment_method_data: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodData"
        ]
        """
        If provided, this hash will be used to create a PaymentMethod. The new PaymentMethod will appear
        in the [payment_method](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-payment_method)
        property on the PaymentIntent.
        """
        payment_method_options: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptions"
        ]
        """
        Payment method-specific configuration for this PaymentIntent.
        """
        payment_method_types: NotRequired[List[str]]
        """
        The list of payment method types (for example, a card) that this PaymentIntent can use. If you don't provide this, it defaults to ["card"]. Use `automatic_payment_methods` to manage payment methods from the [Stripe Dashboard](https://dashboard.stripe.com/settings/payment_methods).
        """
        radar_options: NotRequired["PaymentIntent.CreateParamsRadarOptions"]
        """
        Options to configure Radar. Learn more about [Radar Sessions](https://stripe.com/docs/radar/radar-session).
        """
        receipt_email: NotRequired[str]
        """
        Email address to send the receipt to. If you specify `receipt_email` for a payment in live mode, you send a receipt regardless of your [email settings](https://dashboard.stripe.com/account/emails).
        """
        return_url: NotRequired[str]
        """
        The URL to redirect your customer back to after they authenticate or cancel their payment on the payment method's app or site. If you'd prefer to redirect to a mobile application, you can alternatively supply an application URI scheme. This parameter can only be used with [`confirm=true`](https://stripe.com/docs/api/payment_intents/create#create_payment_intent-confirm).
        """
        setup_future_usage: NotRequired[Literal["off_session", "on_session"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """
        shipping: NotRequired["PaymentIntent.CreateParamsShipping"]
        """
        Shipping information for this PaymentIntent.
        """
        statement_descriptor: NotRequired[str]
        """
        Text that appears on the customer's statement as the [statement descriptor](https://docs.stripe.com/get-started/account/statement-descriptors) for a non-card charge. This value overrides the account's default statement descriptor. Setting this value for a card charge returns an error. For card charges, set the [statement_descriptor_suffix](https://docs.stripe.com/get-started/account/statement-descriptors#dynamic) instead.
        """
        statement_descriptor_suffix: NotRequired[str]
        """
        Provides information about a card charge. Concatenated to the account's [statement descriptor prefix](https://docs.corp.stripe.com/get-started/account/statement-descriptors#static) to form the complete statement descriptor that appears on the customer's statement.
        """
        transfer_data: NotRequired["PaymentIntent.CreateParamsTransferData"]
        """
        The parameters that you can use to automatically create a Transfer.
        Learn more about the [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies the resulting payment as part of a group. Learn more about the [use case for connected accounts](https://stripe.com/docs/connect/separate-charges-and-transfers).
        """
        use_stripe_sdk: NotRequired[bool]
        """
        Set to `true` when confirming server-side and using Stripe.js, iOS, or Android client-side SDKs to handle the next actions.
        """

    class CreateParamsAutomaticPaymentMethods(TypedDict):
        allow_redirects: NotRequired[Literal["always", "never"]]
        """
        Controls whether this PaymentIntent will accept redirect-based payment methods.

        Redirect-based payment methods may require your customer to be redirected to a payment method's app or site for authentication or additional steps. To [confirm](https://stripe.com/docs/api/payment_intents/confirm) this PaymentIntent, you may be required to provide a `return_url` to redirect customers back to your site after they authenticate or complete the payment.
        """
        enabled: bool
        """
        Whether this feature is enabled.
        """

    class CreateParamsMandateData(TypedDict):
        customer_acceptance: (
            "PaymentIntent.CreateParamsMandateDataCustomerAcceptance"
        )
        """
        This hash contains details about the customer acceptance of the Mandate.
        """

    class CreateParamsMandateDataCustomerAcceptance(TypedDict):
        accepted_at: NotRequired[int]
        """
        The time at which the customer accepted the Mandate.
        """
        offline: NotRequired[
            "PaymentIntent.CreateParamsMandateDataCustomerAcceptanceOffline"
        ]
        """
        If this is a Mandate accepted offline, this hash contains details about the offline acceptance.
        """
        online: NotRequired[
            "PaymentIntent.CreateParamsMandateDataCustomerAcceptanceOnline"
        ]
        """
        If this is a Mandate accepted online, this hash contains details about the online acceptance.
        """
        type: Literal["offline", "online"]
        """
        The type of customer acceptance information included with the Mandate. One of `online` or `offline`.
        """

    class CreateParamsMandateDataCustomerAcceptanceOffline(TypedDict):
        pass

    class CreateParamsMandateDataCustomerAcceptanceOnline(TypedDict):
        ip_address: str
        """
        The IP address from which the Mandate was accepted by the customer.
        """
        user_agent: str
        """
        The user agent of the browser from which the Mandate was accepted by the customer.
        """

    class CreateParamsPaymentMethodData(TypedDict):
        acss_debit: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataAcssDebit"
        ]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataAlipay"
        ]
        """
        If this is an `Alipay` PaymentMethod, this hash contains details about the Alipay payment method.
        """
        allow_redisplay: NotRequired[
            Literal["always", "limited", "unspecified"]
        ]
        """
        This field indicates whether this payment method can be shown again to its customer in a checkout flow. Stripe products such as Checkout and Elements use this field to determine whether a payment method can be shown as a saved payment method in a checkout flow. The field defaults to `unspecified`.
        """
        amazon_pay: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataAmazonPay"
        ]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired["PaymentIntent.CreateParamsPaymentMethodDataBlik"]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        cashapp: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer_balance: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired["PaymentIntent.CreateParamsPaymentMethodDataEps"]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        fpx: NotRequired["PaymentIntent.CreateParamsPaymentMethodDataFpx"]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired["PaymentIntent.CreateParamsPaymentMethodDataIdeal"]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired["PaymentIntent.CreateParamsPaymentMethodDataLink"]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataMobilepay"
        ]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired["PaymentIntent.CreateParamsPaymentMethodDataOxxo"]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired["PaymentIntent.CreateParamsPaymentMethodDataP24"]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        paynow: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired["PaymentIntent.CreateParamsPaymentMethodDataPix"]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataRevolutPay"
        ]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired["PaymentIntent.CreateParamsPaymentMethodDataSwish"]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired["PaymentIntent.CreateParamsPaymentMethodDataTwint"]
        """
        If this is a TWINT PaymentMethod, this hash contains details about the TWINT payment method.
        """
        type: Literal[
            "acss_debit",
            "affirm",
            "afterpay_clearpay",
            "alipay",
            "amazon_pay",
            "au_becs_debit",
            "bacs_debit",
            "bancontact",
            "blik",
            "boleto",
            "cashapp",
            "customer_balance",
            "eps",
            "fpx",
            "giropay",
            "grabpay",
            "ideal",
            "klarna",
            "konbini",
            "link",
            "mobilepay",
            "multibanco",
            "oxxo",
            "p24",
            "paynow",
            "paypal",
            "pix",
            "promptpay",
            "revolut_pay",
            "sepa_debit",
            "sofort",
            "swish",
            "twint",
            "us_bank_account",
            "wechat_pay",
            "zip",
        ]
        """
        The type of the PaymentMethod. An additional hash is included on the PaymentMethod with a name matching this value. It contains additional information specific to the PaymentMethod type.
        """
        us_bank_account: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataWechatPay"
        ]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired["PaymentIntent.CreateParamsPaymentMethodDataZip"]
        """
        If this is a `zip` PaymentMethod, this hash contains details about the Zip payment method.
        """

    class CreateParamsPaymentMethodDataAcssDebit(TypedDict):
        account_number: str
        """
        Customer's bank account number.
        """
        institution_number: str
        """
        Institution number of the customer's bank.
        """
        transit_number: str
        """
        Transit number of the customer's bank.
        """

    class CreateParamsPaymentMethodDataAffirm(TypedDict):
        pass

    class CreateParamsPaymentMethodDataAfterpayClearpay(TypedDict):
        pass

    class CreateParamsPaymentMethodDataAlipay(TypedDict):
        pass

    class CreateParamsPaymentMethodDataAmazonPay(TypedDict):
        pass

    class CreateParamsPaymentMethodDataAuBecsDebit(TypedDict):
        account_number: str
        """
        The account number for the bank account.
        """
        bsb_number: str
        """
        Bank-State-Branch number of the bank account.
        """

    class CreateParamsPaymentMethodDataBacsDebit(TypedDict):
        account_number: NotRequired[str]
        """
        Account number of the bank account that the funds will be debited from.
        """
        sort_code: NotRequired[str]
        """
        Sort code of the bank account. (e.g., `10-20-30`)
        """

    class CreateParamsPaymentMethodDataBancontact(TypedDict):
        pass

    class CreateParamsPaymentMethodDataBillingDetails(TypedDict):
        address: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodDataBillingDetailsAddress"
        ]
        """
        Billing address.
        """
        email: NotRequired["Literal['']|str"]
        """
        Email address.
        """
        name: NotRequired["Literal['']|str"]
        """
        Full name.
        """
        phone: NotRequired["Literal['']|str"]
        """
        Billing phone number (including extension).
        """

    class CreateParamsPaymentMethodDataBillingDetailsAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class CreateParamsPaymentMethodDataBlik(TypedDict):
        pass

    class CreateParamsPaymentMethodDataBoleto(TypedDict):
        tax_id: str
        """
        The tax ID of the customer (CPF for individual consumers or CNPJ for businesses consumers)
        """

    class CreateParamsPaymentMethodDataCashapp(TypedDict):
        pass

    class CreateParamsPaymentMethodDataCustomerBalance(TypedDict):
        pass

    class CreateParamsPaymentMethodDataEps(TypedDict):
        bank: NotRequired[
            Literal[
                "arzte_und_apotheker_bank",
                "austrian_anadi_bank_ag",
                "bank_austria",
                "bankhaus_carl_spangler",
                "bankhaus_schelhammer_und_schattera_ag",
                "bawag_psk_ag",
                "bks_bank_ag",
                "brull_kallmus_bank_ag",
                "btv_vier_lander_bank",
                "capital_bank_grawe_gruppe_ag",
                "deutsche_bank_ag",
                "dolomitenbank",
                "easybank_ag",
                "erste_bank_und_sparkassen",
                "hypo_alpeadriabank_international_ag",
                "hypo_bank_burgenland_aktiengesellschaft",
                "hypo_noe_lb_fur_niederosterreich_u_wien",
                "hypo_oberosterreich_salzburg_steiermark",
                "hypo_tirol_bank_ag",
                "hypo_vorarlberg_bank_ag",
                "marchfelder_bank",
                "oberbank_ag",
                "raiffeisen_bankengruppe_osterreich",
                "schoellerbank_ag",
                "sparda_bank_wien",
                "volksbank_gruppe",
                "volkskreditbank_ag",
                "vr_bank_braunau",
            ]
        ]
        """
        The customer's bank.
        """

    class CreateParamsPaymentMethodDataFpx(TypedDict):
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        Account holder type for FPX transaction
        """
        bank: Literal[
            "affin_bank",
            "agrobank",
            "alliance_bank",
            "ambank",
            "bank_islam",
            "bank_muamalat",
            "bank_of_china",
            "bank_rakyat",
            "bsn",
            "cimb",
            "deutsche_bank",
            "hong_leong_bank",
            "hsbc",
            "kfh",
            "maybank2e",
            "maybank2u",
            "ocbc",
            "pb_enterprise",
            "public_bank",
            "rhb",
            "standard_chartered",
            "uob",
        ]
        """
        The customer's bank.
        """

    class CreateParamsPaymentMethodDataGiropay(TypedDict):
        pass

    class CreateParamsPaymentMethodDataGrabpay(TypedDict):
        pass

    class CreateParamsPaymentMethodDataIdeal(TypedDict):
        bank: NotRequired[
            Literal[
                "abn_amro",
                "asn_bank",
                "bunq",
                "handelsbanken",
                "ing",
                "knab",
                "moneyou",
                "n26",
                "nn",
                "rabobank",
                "regiobank",
                "revolut",
                "sns_bank",
                "triodos_bank",
                "van_lanschot",
                "yoursafe",
            ]
        ]
        """
        The customer's bank.
        """

    class CreateParamsPaymentMethodDataInteracPresent(TypedDict):
        pass

    class CreateParamsPaymentMethodDataKlarna(TypedDict):
        dob: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodDataKlarnaDob"
        ]
        """
        Customer's date of birth
        """

    class CreateParamsPaymentMethodDataKlarnaDob(TypedDict):
        day: int
        """
        The day of birth, between 1 and 31.
        """
        month: int
        """
        The month of birth, between 1 and 12.
        """
        year: int
        """
        The four-digit year of birth.
        """

    class CreateParamsPaymentMethodDataKonbini(TypedDict):
        pass

    class CreateParamsPaymentMethodDataLink(TypedDict):
        pass

    class CreateParamsPaymentMethodDataMobilepay(TypedDict):
        pass

    class CreateParamsPaymentMethodDataMultibanco(TypedDict):
        pass

    class CreateParamsPaymentMethodDataOxxo(TypedDict):
        pass

    class CreateParamsPaymentMethodDataP24(TypedDict):
        bank: NotRequired[
            Literal[
                "alior_bank",
                "bank_millennium",
                "bank_nowy_bfg_sa",
                "bank_pekao_sa",
                "banki_spbdzielcze",
                "blik",
                "bnp_paribas",
                "boz",
                "citi_handlowy",
                "credit_agricole",
                "envelobank",
                "etransfer_pocztowy24",
                "getin_bank",
                "ideabank",
                "ing",
                "inteligo",
                "mbank_mtransfer",
                "nest_przelew",
                "noble_pay",
                "pbac_z_ipko",
                "plus_bank",
                "santander_przelew24",
                "tmobile_usbugi_bankowe",
                "toyota_bank",
                "velobank",
                "volkswagen_bank",
            ]
        ]
        """
        The customer's bank.
        """

    class CreateParamsPaymentMethodDataPaynow(TypedDict):
        pass

    class CreateParamsPaymentMethodDataPaypal(TypedDict):
        pass

    class CreateParamsPaymentMethodDataPix(TypedDict):
        pass

    class CreateParamsPaymentMethodDataPromptpay(TypedDict):
        pass

    class CreateParamsPaymentMethodDataRadarOptions(TypedDict):
        session: NotRequired[str]
        """
        A [Radar Session](https://stripe.com/docs/radar/radar-session) is a snapshot of the browser metadata and device details that help Radar make more accurate predictions on your payments.
        """

    class CreateParamsPaymentMethodDataRevolutPay(TypedDict):
        pass

    class CreateParamsPaymentMethodDataSepaDebit(TypedDict):
        iban: str
        """
        IBAN of the bank account.
        """

    class CreateParamsPaymentMethodDataSofort(TypedDict):
        country: Literal["AT", "BE", "DE", "ES", "IT", "NL"]
        """
        Two-letter ISO code representing the country the bank account is located in.
        """

    class CreateParamsPaymentMethodDataSwish(TypedDict):
        pass

    class CreateParamsPaymentMethodDataTwint(TypedDict):
        pass

    class CreateParamsPaymentMethodDataUsBankAccount(TypedDict):
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        Account holder type: individual or company.
        """
        account_number: NotRequired[str]
        """
        Account number of the bank account.
        """
        account_type: NotRequired[Literal["checking", "savings"]]
        """
        Account type: checkings or savings. Defaults to checking if omitted.
        """
        financial_connections_account: NotRequired[str]
        """
        The ID of a Financial Connections Account to use as a payment method.
        """
        routing_number: NotRequired[str]
        """
        Routing number of the bank account.
        """

    class CreateParamsPaymentMethodDataWechatPay(TypedDict):
        pass

    class CreateParamsPaymentMethodDataZip(TypedDict):
        pass

    class CreateParamsPaymentMethodOptions(TypedDict):
        acss_debit: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsAcssDebit"
        ]
        """
        If this is a `acss_debit` PaymentMethod, this sub-hash contains details about the ACSS Debit payment method options.
        """
        affirm: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this sub-hash contains details about the Affirm payment method options.
        """
        afterpay_clearpay: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsAfterpayClearpay"
        ]
        """
        If this is a `afterpay_clearpay` PaymentMethod, this sub-hash contains details about the Afterpay Clearpay payment method options.
        """
        alipay: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsAlipay"
        ]
        """
        If this is a `alipay` PaymentMethod, this sub-hash contains details about the Alipay payment method options.
        """
        amazon_pay: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsAmazonPay"
        ]
        """
        If this is a `amazon_pay` PaymentMethod, this sub-hash contains details about the Amazon Pay payment method options.
        """
        au_becs_debit: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsAuBecsDebit"
        ]
        """
        If this is a `au_becs_debit` PaymentMethod, this sub-hash contains details about the AU BECS Direct Debit payment method options.
        """
        bacs_debit: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this sub-hash contains details about the BACS Debit payment method options.
        """
        bancontact: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this sub-hash contains details about the Bancontact payment method options.
        """
        blik: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this sub-hash contains details about the BLIK payment method options.
        """
        boleto: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this sub-hash contains details about the Boleto payment method options.
        """
        card: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsCard"
        ]
        """
        Configuration for any card payments attempted on this PaymentIntent.
        """
        card_present: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsCardPresent"
        ]
        """
        If this is a `card_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        cashapp: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this sub-hash contains details about the Cash App Pay payment method options.
        """
        customer_balance: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsCustomerBalance"
        ]
        """
        If this is a `customer balance` PaymentMethod, this sub-hash contains details about the customer balance payment method options.
        """
        eps: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsEps"
        ]
        """
        If this is a `eps` PaymentMethod, this sub-hash contains details about the EPS payment method options.
        """
        fpx: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsFpx"
        ]
        """
        If this is a `fpx` PaymentMethod, this sub-hash contains details about the FPX payment method options.
        """
        giropay: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this sub-hash contains details about the Giropay payment method options.
        """
        grabpay: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this sub-hash contains details about the Grabpay payment method options.
        """
        ideal: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsIdeal"
        ]
        """
        If this is a `ideal` PaymentMethod, this sub-hash contains details about the Ideal payment method options.
        """
        interac_present: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsInteracPresent"
        ]
        """
        If this is a `interac_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        klarna: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this sub-hash contains details about the Klarna payment method options.
        """
        konbini: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this sub-hash contains details about the Konbini payment method options.
        """
        link: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsLink"
        ]
        """
        If this is a `link` PaymentMethod, this sub-hash contains details about the Link payment method options.
        """
        mobilepay: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsMobilepay"
        ]
        """
        If this is a `MobilePay` PaymentMethod, this sub-hash contains details about the MobilePay payment method options.
        """
        multibanco: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this sub-hash contains details about the Multibanco payment method options.
        """
        oxxo: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsOxxo"
        ]
        """
        If this is a `oxxo` PaymentMethod, this sub-hash contains details about the OXXO payment method options.
        """
        p24: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsP24"
        ]
        """
        If this is a `p24` PaymentMethod, this sub-hash contains details about the Przelewy24 payment method options.
        """
        paynow: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this sub-hash contains details about the PayNow payment method options.
        """
        paypal: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this sub-hash contains details about the PayPal payment method options.
        """
        pix: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsPix"
        ]
        """
        If this is a `pix` PaymentMethod, this sub-hash contains details about the Pix payment method options.
        """
        promptpay: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this sub-hash contains details about the PromptPay payment method options.
        """
        revolut_pay: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsRevolutPay"
        ]
        """
        If this is a `revolut_pay` PaymentMethod, this sub-hash contains details about the Revolut Pay payment method options.
        """
        sepa_debit: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentIntent, this sub-hash contains details about the SEPA Debit payment method options.
        """
        sofort: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this sub-hash contains details about the SOFORT payment method options.
        """
        swish: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsSwish"
        ]
        """
        If this is a `Swish` PaymentMethod, this sub-hash contains details about the Swish payment method options.
        """
        twint: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsTwint"
        ]
        """
        If this is a `twint` PaymentMethod, this sub-hash contains details about the TWINT payment method options.
        """
        us_bank_account: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If this is a `us_bank_account` PaymentMethod, this sub-hash contains details about the US bank account payment method options.
        """
        wechat_pay: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsWechatPay"
        ]
        """
        If this is a `wechat_pay` PaymentMethod, this sub-hash contains details about the WeChat Pay payment method options.
        """
        zip: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsZip"
        ]
        """
        If this is a `zip` PaymentMethod, this sub-hash contains details about the Zip payment method options.
        """

    class CreateParamsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsAcssDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Bank account verification method.
        """

    class CreateParamsPaymentMethodOptionsAcssDebitMandateOptions(TypedDict):
        custom_mandate_url: NotRequired["Literal['']|str"]
        """
        A URL for custom mandate text to render during confirmation step.
        The URL will be rendered with additional GET parameters `payment_intent` and `payment_intent_client_secret` when confirming a Payment Intent,
        or `setup_intent` and `setup_intent_client_secret` when confirming a Setup Intent.
        """
        interval_description: NotRequired[str]
        """
        Description of the mandate interval. Only required if 'payment_schedule' parameter is 'interval' or 'combined'.
        """
        payment_schedule: NotRequired[
            Literal["combined", "interval", "sporadic"]
        ]
        """
        Payment schedule for the mandate.
        """
        transaction_type: NotRequired[Literal["business", "personal"]]
        """
        Transaction type of the mandate.
        """

    class CreateParamsPaymentMethodOptionsAffirm(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        preferred_locale: NotRequired[str]
        """
        Preferred language of the Affirm authorization page that the customer is redirected to.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsAfterpayClearpay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        reference: NotRequired[str]
        """
        An internal identifier or reference that this payment corresponds to. You must limit the identifier to 128 characters, and it can only contain letters, numbers, underscores, backslashes, and dashes.
        This field differs from the statement descriptor and item name.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsAlipay(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsAmazonPay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsAuBecsDebit(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsBacsDebit(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsBancontact(TypedDict):
        preferred_language: NotRequired[Literal["de", "en", "fr", "nl"]]
        """
        Preferred language of the Bancontact authorization page that the customer is redirected to.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsBlik(TypedDict):
        code: NotRequired[str]
        """
        The 6-digit BLIK code that a customer has generated using their banking application. Can only be set on confirmation.
        """
        setup_future_usage: NotRequired["Literal['']|Literal['none']"]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsBoleto(TypedDict):
        expires_after_days: NotRequired[int]
        """
        The number of calendar days before a Boleto voucher expires. For example, if you create a Boleto voucher on Monday and you set expires_after_days to 2, the Boleto invoice will expire on Wednesday at 23:59 America/Sao_Paulo time.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsCard(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        cvc_token: NotRequired[str]
        """
        A single-use `cvc_update` Token that represents a card CVC value. When provided, the CVC value will be verified during the card payment attempt. This parameter can only be provided during confirmation.
        """
        installments: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsCardInstallments"
        ]
        """
        Installment configuration for payments attempted on this PaymentIntent (Mexico Only).

        For more information, see the [installments integration guide](https://stripe.com/docs/payments/installments).
        """
        mandate_options: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsCardMandateOptions"
        ]
        """
        Configuration options for setting up an eMandate for cards issued in India.
        """
        moto: NotRequired[bool]
        """
        When specified, this parameter indicates that a transaction will be marked
        as MOTO (Mail Order Telephone Order) and thus out of scope for SCA. This
        parameter can only be provided during confirmation.
        """
        network: NotRequired[
            Literal[
                "amex",
                "cartes_bancaires",
                "diners",
                "discover",
                "eftpos_au",
                "girocard",
                "interac",
                "jcb",
                "mastercard",
                "unionpay",
                "unknown",
                "visa",
            ]
        ]
        """
        Selected network to process this PaymentIntent on. Depends on the available networks of the card attached to the PaymentIntent. Can be only set confirm-time.
        """
        request_extended_authorization: NotRequired[
            Literal["if_available", "never"]
        ]
        """
        Request ability to [capture beyond the standard authorization validity window](https://stripe.com/docs/payments/extended-authorization) for this PaymentIntent.
        """
        request_incremental_authorization: NotRequired[
            Literal["if_available", "never"]
        ]
        """
        Request ability to [increment the authorization](https://stripe.com/docs/payments/incremental-authorization) for this PaymentIntent.
        """
        request_multicapture: NotRequired[Literal["if_available", "never"]]
        """
        Request ability to make [multiple captures](https://stripe.com/docs/payments/multicapture) for this PaymentIntent.
        """
        request_overcapture: NotRequired[Literal["if_available", "never"]]
        """
        Request ability to [overcapture](https://stripe.com/docs/payments/overcapture) for this PaymentIntent.
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. If not provided, this value defaults to `automatic`. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """
        require_cvc_recollection: NotRequired[bool]
        """
        When enabled, using a card that is attached to a customer will require the CVC to be provided again (i.e. using the cvc_token parameter).
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        statement_descriptor_suffix_kana: NotRequired["Literal['']|str"]
        """
        Provides information about a card payment that customers see on their statements. Concatenated with the Kana prefix (shortened Kana descriptor) or Kana statement descriptor that's set on the account to form the complete statement descriptor. Maximum 22 characters. On card statements, the *concatenation* of both prefix and suffix (including separators) will appear truncated to 22 characters.
        """
        statement_descriptor_suffix_kanji: NotRequired["Literal['']|str"]
        """
        Provides information about a card payment that customers see on their statements. Concatenated with the Kanji prefix (shortened Kanji descriptor) or Kanji statement descriptor that's set on the account to form the complete statement descriptor. Maximum 17 characters. On card statements, the *concatenation* of both prefix and suffix (including separators) will appear truncated to 17 characters.
        """
        three_d_secure: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsCardThreeDSecure"
        ]
        """
        If 3D Secure authentication was performed with a third-party provider,
        the authentication details to use for this payment.
        """

    class CreateParamsPaymentMethodOptionsCardInstallments(TypedDict):
        enabled: NotRequired[bool]
        """
        Setting to true enables installments for this PaymentIntent.
        This will cause the response to contain a list of available installment plans.
        Setting to false will prevent any selected plan from applying to a charge.
        """
        plan: NotRequired[
            "Literal['']|PaymentIntent.CreateParamsPaymentMethodOptionsCardInstallmentsPlan"
        ]
        """
        The selected installment plan to use for this payment attempt.
        This parameter can only be provided during confirmation.
        """

    class CreateParamsPaymentMethodOptionsCardInstallmentsPlan(TypedDict):
        count: NotRequired[int]
        """
        For `fixed_count` installment plans, this is required. It represents the number of installment payments your customer will make to their credit card.
        """
        interval: NotRequired[Literal["month"]]
        """
        For `fixed_count` installment plans, this is required. It represents the interval between installment payments your customer will make to their credit card.
        One of `month`.
        """
        type: Literal["fixed_count"]
        """
        Type of installment plan, one of `fixed_count`.
        """

    class CreateParamsPaymentMethodOptionsCardMandateOptions(TypedDict):
        amount: int
        """
        Amount to be charged for future payments.
        """
        amount_type: Literal["fixed", "maximum"]
        """
        One of `fixed` or `maximum`. If `fixed`, the `amount` param refers to the exact amount to be charged in future payments. If `maximum`, the amount charged can be up to the value passed for the `amount` param.
        """
        description: NotRequired[str]
        """
        A description of the mandate or subscription that is meant to be displayed to the customer.
        """
        end_date: NotRequired[int]
        """
        End date of the mandate or subscription. If not provided, the mandate will be active until canceled. If provided, end date should be after start date.
        """
        interval: Literal["day", "month", "sporadic", "week", "year"]
        """
        Specifies payment frequency. One of `day`, `week`, `month`, `year`, or `sporadic`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between payments. For example, `interval=month` and `interval_count=3` indicates one payment every three months. Maximum of one year interval allowed (1 year, 12 months, or 52 weeks). This parameter is optional when `interval=sporadic`.
        """
        reference: str
        """
        Unique identifier for the mandate or subscription.
        """
        start_date: int
        """
        Start date of the mandate or subscription. Start date should not be lesser than yesterday.
        """
        supported_types: NotRequired[List[Literal["india"]]]
        """
        Specifies the type of mandates supported. Possible values are `india`.
        """

    class CreateParamsPaymentMethodOptionsCardPresent(TypedDict):
        request_extended_authorization: NotRequired[bool]
        """
        Request ability to capture this payment beyond the standard [authorization validity window](https://stripe.com/docs/terminal/features/extended-authorizations#authorization-validity)
        """
        request_incremental_authorization_support: NotRequired[bool]
        """
        Request ability to [increment](https://stripe.com/docs/terminal/features/incremental-authorizations) this PaymentIntent if the combination of MCC and card brand is eligible. Check [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported) in the [Confirm](https://stripe.com/docs/api/payment_intents/confirm) response to verify support.
        """
        routing: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsCardPresentRouting"
        ]
        """
        Network routing priority on co-branded EMV cards supporting domestic debit and international card schemes.
        """

    class CreateParamsPaymentMethodOptionsCardPresentRouting(TypedDict):
        requested_priority: NotRequired[Literal["domestic", "international"]]
        """
        Routing requested priority
        """

    class CreateParamsPaymentMethodOptionsCardThreeDSecure(TypedDict):
        ares_trans_status: NotRequired[
            Literal["A", "C", "I", "N", "R", "U", "Y"]
        ]
        """
        The `transStatus` returned from the card Issuer's ACS in the ARes.
        """
        cryptogram: str
        """
        The cryptogram, also known as the "authentication value" (AAV, CAVV or
        AEVV). This value is 20 bytes, base64-encoded into a 28-character string.
        (Most 3D Secure providers will return the base64-encoded version, which
        is what you should specify here.)
        """
        electronic_commerce_indicator: NotRequired[
            Literal["01", "02", "05", "06", "07"]
        ]
        """
        The Electronic Commerce Indicator (ECI) is returned by your 3D Secure
        provider and indicates what degree of authentication was performed.
        """
        exemption_indicator: NotRequired[Literal["low_risk", "none"]]
        """
        The exemption requested via 3DS and accepted by the issuer at authentication time.
        """
        network_options: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions"
        ]
        """
        Network specific 3DS fields. Network specific arguments require an
        explicit card brand choice. The parameter `payment_method_options.card.network``
        must be populated accordingly
        """
        requestor_challenge_indicator: NotRequired[str]
        """
        The challenge indicator (`threeDSRequestorChallengeInd`) which was requested in the
        AReq sent to the card Issuer's ACS. A string containing 2 digits from 01-99.
        """
        transaction_id: str
        """
        For 3D Secure 1, the XID. For 3D Secure 2, the Directory Server
        Transaction ID (dsTransID).
        """
        version: Literal["1.0.2", "2.1.0", "2.2.0"]
        """
        The version of 3D Secure that was performed.
        """

    class CreateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions(
        TypedDict,
    ):
        cartes_bancaires: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires"
        ]
        """
        Cartes Bancaires-specific 3DS fields.
        """

    class CreateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires(
        TypedDict,
    ):
        cb_avalgo: Literal["0", "1", "2", "3", "4", "A"]
        """
        The cryptogram calculation algorithm used by the card Issuer's ACS
        to calculate the Authentication cryptogram. Also known as `cavvAlgorithm`.
        messageExtension: CB-AVALGO
        """
        cb_exemption: NotRequired[str]
        """
        The exemption indicator returned from Cartes Bancaires in the ARes.
        message extension: CB-EXEMPTION; string (4 characters)
        This is a 3 byte bitmap (low significant byte first and most significant
        bit first) that has been Base64 encoded
        """
        cb_score: NotRequired[int]
        """
        The risk score returned from Cartes Bancaires in the ARes.
        message extension: CB-SCORE; numeric value 0-99
        """

    class CreateParamsPaymentMethodOptionsCashapp(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsCustomerBalance(TypedDict):
        bank_transfer: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsCustomerBalanceBankTransfer"
        ]
        """
        Configuration for the bank transfer funding type, if the `funding_type` is set to `bank_transfer`.
        """
        funding_type: NotRequired[Literal["bank_transfer"]]
        """
        The funding method type to be used when there are not enough funds in the customer balance. Permitted values include: `bank_transfer`.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsCustomerBalanceBankTransfer(
        TypedDict,
    ):
        eu_bank_transfer: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
        ]
        """
        Configuration for the eu_bank_transfer funding type.
        """
        requested_address_types: NotRequired[
            List[
                Literal[
                    "aba",
                    "iban",
                    "sepa",
                    "sort_code",
                    "spei",
                    "swift",
                    "zengin",
                ]
            ]
        ]
        """
        List of address types that should be returned in the financial_addresses response. If not specified, all valid types will be returned.

        Permitted values include: `sort_code`, `zengin`, `iban`, or `spei`.
        """
        type: Literal[
            "eu_bank_transfer",
            "gb_bank_transfer",
            "jp_bank_transfer",
            "mx_bank_transfer",
            "us_bank_transfer",
        ]
        """
        The list of bank transfer types that this PaymentIntent is allowed to use for funding Permitted values include: `eu_bank_transfer`, `gb_bank_transfer`, `jp_bank_transfer`, `mx_bank_transfer`, or `us_bank_transfer`.
        """

    class CreateParamsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer(
        TypedDict,
    ):
        country: str
        """
        The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
        """

    class CreateParamsPaymentMethodOptionsEps(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsFpx(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsGiropay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsGrabpay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsIdeal(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsInteracPresent(TypedDict):
        pass

    class CreateParamsPaymentMethodOptionsKlarna(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        preferred_locale: NotRequired[
            Literal[
                "cs-CZ",
                "da-DK",
                "de-AT",
                "de-CH",
                "de-DE",
                "el-GR",
                "en-AT",
                "en-AU",
                "en-BE",
                "en-CA",
                "en-CH",
                "en-CZ",
                "en-DE",
                "en-DK",
                "en-ES",
                "en-FI",
                "en-FR",
                "en-GB",
                "en-GR",
                "en-IE",
                "en-IT",
                "en-NL",
                "en-NO",
                "en-NZ",
                "en-PL",
                "en-PT",
                "en-RO",
                "en-SE",
                "en-US",
                "es-ES",
                "es-US",
                "fi-FI",
                "fr-BE",
                "fr-CA",
                "fr-CH",
                "fr-FR",
                "it-CH",
                "it-IT",
                "nb-NO",
                "nl-BE",
                "nl-NL",
                "pl-PL",
                "pt-PT",
                "ro-RO",
                "sv-FI",
                "sv-SE",
            ]
        ]
        """
        Preferred language of the Klarna authorization page that the customer is redirected to
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsKonbini(TypedDict):
        confirmation_number: NotRequired["Literal['']|str"]
        """
        An optional 10 to 11 digit numeric-only string determining the confirmation code at applicable convenience stores. Must not consist of only zeroes and could be rejected in case of insufficient uniqueness. We recommend to use the customer's phone number.
        """
        expires_after_days: NotRequired["Literal['']|int"]
        """
        The number of calendar days (between 1 and 60) after which Konbini payment instructions will expire. For example, if a PaymentIntent is confirmed with Konbini and `expires_after_days` set to 2 on Monday JST, the instructions will expire on Wednesday 23:59:59 JST. Defaults to 3 days.
        """
        expires_at: NotRequired["Literal['']|int"]
        """
        The timestamp at which the Konbini payment instructions will expire. Only one of `expires_after_days` or `expires_at` may be set.
        """
        product_description: NotRequired["Literal['']|str"]
        """
        A product descriptor of up to 22 characters, which will appear to customers at the convenience store.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsLink(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        persistent_token: NotRequired[str]
        """
        [Deprecated] This is a legacy parameter that no longer has any function.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsMobilepay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsMultibanco(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsOxxo(TypedDict):
        expires_after_days: NotRequired[int]
        """
        The number of calendar days before an OXXO voucher expires. For example, if you create an OXXO voucher on Monday and you set expires_after_days to 2, the OXXO invoice will expire on Wednesday at 23:59 America/Mexico_City time.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsP24(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        tos_shown_and_accepted: NotRequired[bool]
        """
        Confirm that the payer has accepted the P24 terms and conditions.
        """

    class CreateParamsPaymentMethodOptionsPaynow(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsPaypal(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds will be captured from the customer's account.
        """
        preferred_locale: NotRequired[
            Literal[
                "cs-CZ",
                "da-DK",
                "de-AT",
                "de-DE",
                "de-LU",
                "el-GR",
                "en-GB",
                "en-US",
                "es-ES",
                "fi-FI",
                "fr-BE",
                "fr-FR",
                "fr-LU",
                "hu-HU",
                "it-IT",
                "nl-BE",
                "nl-NL",
                "pl-PL",
                "pt-PT",
                "sk-SK",
                "sv-SE",
            ]
        ]
        """
        [Preferred locale](https://stripe.com/docs/payments/paypal/supported-locales) of the PayPal checkout page that the customer is redirected to.
        """
        reference: NotRequired[str]
        """
        A reference of the PayPal transaction visible to customer which is mapped to PayPal's invoice ID. This must be a globally unique ID if you have configured in your PayPal settings to block multiple payments per invoice ID.
        """
        risk_correlation_id: NotRequired[str]
        """
        The risk correlation ID for an on-session payment using a saved PayPal payment method.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsPix(TypedDict):
        expires_after_seconds: NotRequired[int]
        """
        The number of seconds (between 10 and 1209600) after which Pix payment will expire. Defaults to 86400 seconds.
        """
        expires_at: NotRequired[int]
        """
        The timestamp at which the Pix expires (between 10 and 1209600 seconds in the future). Defaults to 1 day in the future.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsPromptpay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsRevolutPay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsSepaDebit(TypedDict):
        mandate_options: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsSepaDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsSepaDebitMandateOptions(TypedDict):
        pass

    class CreateParamsPaymentMethodOptionsSofort(TypedDict):
        preferred_language: NotRequired[
            "Literal['']|Literal['de', 'en', 'es', 'fr', 'it', 'nl', 'pl']"
        ]
        """
        Language shown to the payer on redirect.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsSwish(TypedDict):
        reference: NotRequired["Literal['']|str"]
        """
        The order ID displayed in the Swish app after the payment is authorized.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsTwint(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsUsBankAccount(TypedDict):
        financial_connections: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        mandate_options: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsUsBankAccountMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        networks: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsUsBankAccountNetworks"
        ]
        """
        Additional fields for network related functions
        """
        preferred_settlement_speed: NotRequired[
            "Literal['']|Literal['fastest', 'standard']"
        ]
        """
        Preferred transaction settlement speed
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Bank account verification method.
        """

    class CreateParamsPaymentMethodOptionsUsBankAccountFinancialConnections(
        TypedDict,
    ):
        filters: NotRequired[
            "PaymentIntent.CreateParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
        ]
        """
        Provide filters for the linked accounts that the customer can select for the payment method
        """
        permissions: NotRequired[
            List[
                Literal[
                    "balances", "ownership", "payment_method", "transactions"
                ]
            ]
        ]
        """
        The list of permissions to request. If this parameter is passed, the `payment_method` permission must be included. Valid permissions include: `balances`, `ownership`, `payment_method`, and `transactions`.
        """
        prefetch: NotRequired[
            List[Literal["balances", "ownership", "transactions"]]
        ]
        """
        List of data features that you would like to retrieve upon account creation.
        """
        return_url: NotRequired[str]
        """
        For webview integrations only. Upon completing OAuth login in the native browser, the user will be redirected to this URL to return to your app.
        """

    class CreateParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters(
        TypedDict,
    ):
        account_subcategories: NotRequired[
            List[Literal["checking", "savings"]]
        ]
        """
        The account subcategories to use to filter for selectable accounts. Valid subcategories are `checking` and `savings`.
        """

    class CreateParamsPaymentMethodOptionsUsBankAccountMandateOptions(
        TypedDict,
    ):
        collection_method: NotRequired["Literal['']|Literal['paper']"]
        """
        The method used to collect offline mandate customer acceptance.
        """

    class CreateParamsPaymentMethodOptionsUsBankAccountNetworks(TypedDict):
        requested: NotRequired[List[Literal["ach", "us_domestic_wire"]]]
        """
        Triggers validations to run across the selected networks
        """

    class CreateParamsPaymentMethodOptionsWechatPay(TypedDict):
        app_id: NotRequired[str]
        """
        The app ID registered with WeChat Pay. Only required when client is ios or android.
        """
        client: Literal["android", "ios", "web"]
        """
        The client type that the end customer will pay from
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsPaymentMethodOptionsZip(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class CreateParamsRadarOptions(TypedDict):
        session: NotRequired[str]
        """
        A [Radar Session](https://stripe.com/docs/radar/radar-session) is a snapshot of the browser metadata and device details that help Radar make more accurate predictions on your payments.
        """

    class CreateParamsShipping(TypedDict):
        address: "PaymentIntent.CreateParamsShippingAddress"
        """
        Shipping address.
        """
        carrier: NotRequired[str]
        """
        The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc.
        """
        name: str
        """
        Recipient name.
        """
        phone: NotRequired[str]
        """
        Recipient phone (including extension).
        """
        tracking_number: NotRequired[str]
        """
        The tracking number for a physical product, obtained from the delivery service. If multiple tracking numbers were generated for this purchase, please separate them with commas.
        """

    class CreateParamsShippingAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class CreateParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount that will be transferred automatically when a charge succeeds.
        The amount is capped at the total transaction amount and if no amount is set,
        the full amount is transferred.

        If you intend to collect a fee and you need a more robust reporting experience, using
        [application_fee_amount](https://stripe.com/docs/api/payment_intents/create#create_payment_intent-application_fee_amount)
        might be a better fit for your integration.
        """
        destination: str
        """
        If specified, successful charges will be attributed to the destination
        account for tax reporting, and the funds from charges will be transferred
        to the destination account. The ID of the resulting transfer will be
        returned on the successful charge's `transfer` field.
        """

    class IncrementAuthorizationParams(RequestOptions):
        amount: int
        """
        The updated total amount that you intend to collect from the cardholder. This amount must be greater than the currently authorized amount.
        """
        application_fee_amount: NotRequired[int]
        """
        The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. The amount of the application fee collected will be capped at the total payment amount. For more information, see the PaymentIntents [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        statement_descriptor: NotRequired[str]
        """
        Text that appears on the customer's statement as the [statement descriptor](https://docs.stripe.com/get-started/account/statement-descriptors) for a non-card charge. This value overrides the account's default statement descriptor. Setting this value for a card charge returns an error. For card charges, set the [statement_descriptor_suffix](https://docs.stripe.com/get-started/account/statement-descriptors#dynamic) instead.
        """
        transfer_data: NotRequired[
            "PaymentIntent.IncrementAuthorizationParamsTransferData"
        ]
        """
        The parameters used to automatically create a transfer after the payment is captured.
        Learn more about the [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """

    class IncrementAuthorizationParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount that will be transferred automatically when a charge succeeds.
        """

    class ListParams(RequestOptions):
        created: NotRequired["PaymentIntent.ListParamsCreated|int"]
        """
        A filter on the list, based on the object `created` field. The value can be a string with an integer Unix timestamp or a dictionary with a number of different query options.
        """
        customer: NotRequired[str]
        """
        Only return PaymentIntents for the customer that this customer ID specifies.
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
        amount: NotRequired[int]
        """
        Amount intended to be collected by this PaymentIntent. A positive integer representing how much to charge in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) (e.g., 100 cents to charge $1.00 or 100 to charge ¥100, a zero-decimal currency). The minimum amount is $0.50 US or [equivalent in charge currency](https://stripe.com/docs/currencies#minimum-and-maximum-charge-amounts). The amount value supports up to eight digits (e.g., a value of 99999999 for a USD charge of $999,999.99).
        """
        application_fee_amount: NotRequired["Literal['']|int"]
        """
        The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. The amount of the application fee collected will be capped at the total payment amount. For more information, see the PaymentIntents [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """
        capture_method: NotRequired[
            Literal["automatic", "automatic_async", "manual"]
        ]
        """
        Controls when the funds will be captured from the customer's account.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        customer: NotRequired[str]
        """
        ID of the Customer this PaymentIntent belongs to, if one exists.

        Payment methods attached to other Customers cannot be used with this PaymentIntent.

        If [setup_future_usage](https://stripe.com/docs/api#payment_intent_object-setup_future_usage) is set and this PaymentIntent's payment method is not `card_present`, then the payment method attaches to the Customer after the PaymentIntent has been confirmed and any required actions from the user are complete. If the payment method is `card_present` and isn't a digital wallet, then a [generated_card](https://docs.corp.stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card is created and attached to the Customer instead.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        payment_method: NotRequired[str]
        """
        ID of the payment method (a PaymentMethod, Card, or [compatible Source](https://stripe.com/docs/payments/payment-methods/transitioning#compatibility) object) to attach to this PaymentIntent. To unset this field to null, pass in an empty string.
        """
        payment_method_configuration: NotRequired[str]
        """
        The ID of the payment method configuration to use with this PaymentIntent.
        """
        payment_method_data: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodData"
        ]
        """
        If provided, this hash will be used to create a PaymentMethod. The new PaymentMethod will appear
        in the [payment_method](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-payment_method)
        property on the PaymentIntent.
        """
        payment_method_options: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptions"
        ]
        """
        Payment-method-specific configuration for this PaymentIntent.
        """
        payment_method_types: NotRequired[List[str]]
        """
        The list of payment method types (for example, card) that this PaymentIntent can use. Use `automatic_payment_methods` to manage payment methods from the [Stripe Dashboard](https://dashboard.stripe.com/settings/payment_methods).
        """
        receipt_email: NotRequired["Literal['']|str"]
        """
        Email address that the receipt for the resulting payment will be sent to. If `receipt_email` is specified for a payment in live mode, a receipt will be sent regardless of your [email settings](https://dashboard.stripe.com/account/emails).
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        shipping: NotRequired["Literal['']|PaymentIntent.ModifyParamsShipping"]
        """
        Shipping information for this PaymentIntent.
        """
        statement_descriptor: NotRequired[str]
        """
        Text that appears on the customer's statement as the [statement descriptor](https://docs.stripe.com/get-started/account/statement-descriptors) for a non-card charge. This value overrides the account's default statement descriptor. Setting this value for a card charge returns an error. For card charges, set the [statement_descriptor_suffix](https://docs.stripe.com/get-started/account/statement-descriptors#dynamic) instead.
        """
        statement_descriptor_suffix: NotRequired[str]
        """
        Provides information about a card charge. Concatenated to the account's [statement descriptor prefix](https://docs.corp.stripe.com/get-started/account/statement-descriptors#static) to form the complete statement descriptor that appears on the customer's statement.
        """
        transfer_data: NotRequired["PaymentIntent.ModifyParamsTransferData"]
        """
        Use this parameter to automatically create a Transfer when the payment succeeds. Learn more about the [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies the resulting payment as part of a group. You can only provide `transfer_group` if it hasn't been set. Learn more about the [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """

    class ModifyParamsPaymentMethodData(TypedDict):
        acss_debit: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataAcssDebit"
        ]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataAlipay"
        ]
        """
        If this is an `Alipay` PaymentMethod, this hash contains details about the Alipay payment method.
        """
        allow_redisplay: NotRequired[
            Literal["always", "limited", "unspecified"]
        ]
        """
        This field indicates whether this payment method can be shown again to its customer in a checkout flow. Stripe products such as Checkout and Elements use this field to determine whether a payment method can be shown as a saved payment method in a checkout flow. The field defaults to `unspecified`.
        """
        amazon_pay: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataAmazonPay"
        ]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired["PaymentIntent.ModifyParamsPaymentMethodDataBlik"]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        cashapp: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer_balance: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired["PaymentIntent.ModifyParamsPaymentMethodDataEps"]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        fpx: NotRequired["PaymentIntent.ModifyParamsPaymentMethodDataFpx"]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired["PaymentIntent.ModifyParamsPaymentMethodDataIdeal"]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired["PaymentIntent.ModifyParamsPaymentMethodDataLink"]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataMobilepay"
        ]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired["PaymentIntent.ModifyParamsPaymentMethodDataOxxo"]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired["PaymentIntent.ModifyParamsPaymentMethodDataP24"]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        paynow: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired["PaymentIntent.ModifyParamsPaymentMethodDataPix"]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataRevolutPay"
        ]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired["PaymentIntent.ModifyParamsPaymentMethodDataSwish"]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired["PaymentIntent.ModifyParamsPaymentMethodDataTwint"]
        """
        If this is a TWINT PaymentMethod, this hash contains details about the TWINT payment method.
        """
        type: Literal[
            "acss_debit",
            "affirm",
            "afterpay_clearpay",
            "alipay",
            "amazon_pay",
            "au_becs_debit",
            "bacs_debit",
            "bancontact",
            "blik",
            "boleto",
            "cashapp",
            "customer_balance",
            "eps",
            "fpx",
            "giropay",
            "grabpay",
            "ideal",
            "klarna",
            "konbini",
            "link",
            "mobilepay",
            "multibanco",
            "oxxo",
            "p24",
            "paynow",
            "paypal",
            "pix",
            "promptpay",
            "revolut_pay",
            "sepa_debit",
            "sofort",
            "swish",
            "twint",
            "us_bank_account",
            "wechat_pay",
            "zip",
        ]
        """
        The type of the PaymentMethod. An additional hash is included on the PaymentMethod with a name matching this value. It contains additional information specific to the PaymentMethod type.
        """
        us_bank_account: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataWechatPay"
        ]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired["PaymentIntent.ModifyParamsPaymentMethodDataZip"]
        """
        If this is a `zip` PaymentMethod, this hash contains details about the Zip payment method.
        """

    class ModifyParamsPaymentMethodDataAcssDebit(TypedDict):
        account_number: str
        """
        Customer's bank account number.
        """
        institution_number: str
        """
        Institution number of the customer's bank.
        """
        transit_number: str
        """
        Transit number of the customer's bank.
        """

    class ModifyParamsPaymentMethodDataAffirm(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataAfterpayClearpay(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataAlipay(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataAmazonPay(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataAuBecsDebit(TypedDict):
        account_number: str
        """
        The account number for the bank account.
        """
        bsb_number: str
        """
        Bank-State-Branch number of the bank account.
        """

    class ModifyParamsPaymentMethodDataBacsDebit(TypedDict):
        account_number: NotRequired[str]
        """
        Account number of the bank account that the funds will be debited from.
        """
        sort_code: NotRequired[str]
        """
        Sort code of the bank account. (e.g., `10-20-30`)
        """

    class ModifyParamsPaymentMethodDataBancontact(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataBillingDetails(TypedDict):
        address: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodDataBillingDetailsAddress"
        ]
        """
        Billing address.
        """
        email: NotRequired["Literal['']|str"]
        """
        Email address.
        """
        name: NotRequired["Literal['']|str"]
        """
        Full name.
        """
        phone: NotRequired["Literal['']|str"]
        """
        Billing phone number (including extension).
        """

    class ModifyParamsPaymentMethodDataBillingDetailsAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class ModifyParamsPaymentMethodDataBlik(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataBoleto(TypedDict):
        tax_id: str
        """
        The tax ID of the customer (CPF for individual consumers or CNPJ for businesses consumers)
        """

    class ModifyParamsPaymentMethodDataCashapp(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataCustomerBalance(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataEps(TypedDict):
        bank: NotRequired[
            Literal[
                "arzte_und_apotheker_bank",
                "austrian_anadi_bank_ag",
                "bank_austria",
                "bankhaus_carl_spangler",
                "bankhaus_schelhammer_und_schattera_ag",
                "bawag_psk_ag",
                "bks_bank_ag",
                "brull_kallmus_bank_ag",
                "btv_vier_lander_bank",
                "capital_bank_grawe_gruppe_ag",
                "deutsche_bank_ag",
                "dolomitenbank",
                "easybank_ag",
                "erste_bank_und_sparkassen",
                "hypo_alpeadriabank_international_ag",
                "hypo_bank_burgenland_aktiengesellschaft",
                "hypo_noe_lb_fur_niederosterreich_u_wien",
                "hypo_oberosterreich_salzburg_steiermark",
                "hypo_tirol_bank_ag",
                "hypo_vorarlberg_bank_ag",
                "marchfelder_bank",
                "oberbank_ag",
                "raiffeisen_bankengruppe_osterreich",
                "schoellerbank_ag",
                "sparda_bank_wien",
                "volksbank_gruppe",
                "volkskreditbank_ag",
                "vr_bank_braunau",
            ]
        ]
        """
        The customer's bank.
        """

    class ModifyParamsPaymentMethodDataFpx(TypedDict):
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        Account holder type for FPX transaction
        """
        bank: Literal[
            "affin_bank",
            "agrobank",
            "alliance_bank",
            "ambank",
            "bank_islam",
            "bank_muamalat",
            "bank_of_china",
            "bank_rakyat",
            "bsn",
            "cimb",
            "deutsche_bank",
            "hong_leong_bank",
            "hsbc",
            "kfh",
            "maybank2e",
            "maybank2u",
            "ocbc",
            "pb_enterprise",
            "public_bank",
            "rhb",
            "standard_chartered",
            "uob",
        ]
        """
        The customer's bank.
        """

    class ModifyParamsPaymentMethodDataGiropay(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataGrabpay(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataIdeal(TypedDict):
        bank: NotRequired[
            Literal[
                "abn_amro",
                "asn_bank",
                "bunq",
                "handelsbanken",
                "ing",
                "knab",
                "moneyou",
                "n26",
                "nn",
                "rabobank",
                "regiobank",
                "revolut",
                "sns_bank",
                "triodos_bank",
                "van_lanschot",
                "yoursafe",
            ]
        ]
        """
        The customer's bank.
        """

    class ModifyParamsPaymentMethodDataInteracPresent(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataKlarna(TypedDict):
        dob: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodDataKlarnaDob"
        ]
        """
        Customer's date of birth
        """

    class ModifyParamsPaymentMethodDataKlarnaDob(TypedDict):
        day: int
        """
        The day of birth, between 1 and 31.
        """
        month: int
        """
        The month of birth, between 1 and 12.
        """
        year: int
        """
        The four-digit year of birth.
        """

    class ModifyParamsPaymentMethodDataKonbini(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataLink(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataMobilepay(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataMultibanco(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataOxxo(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataP24(TypedDict):
        bank: NotRequired[
            Literal[
                "alior_bank",
                "bank_millennium",
                "bank_nowy_bfg_sa",
                "bank_pekao_sa",
                "banki_spbdzielcze",
                "blik",
                "bnp_paribas",
                "boz",
                "citi_handlowy",
                "credit_agricole",
                "envelobank",
                "etransfer_pocztowy24",
                "getin_bank",
                "ideabank",
                "ing",
                "inteligo",
                "mbank_mtransfer",
                "nest_przelew",
                "noble_pay",
                "pbac_z_ipko",
                "plus_bank",
                "santander_przelew24",
                "tmobile_usbugi_bankowe",
                "toyota_bank",
                "velobank",
                "volkswagen_bank",
            ]
        ]
        """
        The customer's bank.
        """

    class ModifyParamsPaymentMethodDataPaynow(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataPaypal(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataPix(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataPromptpay(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataRadarOptions(TypedDict):
        session: NotRequired[str]
        """
        A [Radar Session](https://stripe.com/docs/radar/radar-session) is a snapshot of the browser metadata and device details that help Radar make more accurate predictions on your payments.
        """

    class ModifyParamsPaymentMethodDataRevolutPay(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataSepaDebit(TypedDict):
        iban: str
        """
        IBAN of the bank account.
        """

    class ModifyParamsPaymentMethodDataSofort(TypedDict):
        country: Literal["AT", "BE", "DE", "ES", "IT", "NL"]
        """
        Two-letter ISO code representing the country the bank account is located in.
        """

    class ModifyParamsPaymentMethodDataSwish(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataTwint(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataUsBankAccount(TypedDict):
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        Account holder type: individual or company.
        """
        account_number: NotRequired[str]
        """
        Account number of the bank account.
        """
        account_type: NotRequired[Literal["checking", "savings"]]
        """
        Account type: checkings or savings. Defaults to checking if omitted.
        """
        financial_connections_account: NotRequired[str]
        """
        The ID of a Financial Connections Account to use as a payment method.
        """
        routing_number: NotRequired[str]
        """
        Routing number of the bank account.
        """

    class ModifyParamsPaymentMethodDataWechatPay(TypedDict):
        pass

    class ModifyParamsPaymentMethodDataZip(TypedDict):
        pass

    class ModifyParamsPaymentMethodOptions(TypedDict):
        acss_debit: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsAcssDebit"
        ]
        """
        If this is a `acss_debit` PaymentMethod, this sub-hash contains details about the ACSS Debit payment method options.
        """
        affirm: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this sub-hash contains details about the Affirm payment method options.
        """
        afterpay_clearpay: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsAfterpayClearpay"
        ]
        """
        If this is a `afterpay_clearpay` PaymentMethod, this sub-hash contains details about the Afterpay Clearpay payment method options.
        """
        alipay: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsAlipay"
        ]
        """
        If this is a `alipay` PaymentMethod, this sub-hash contains details about the Alipay payment method options.
        """
        amazon_pay: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsAmazonPay"
        ]
        """
        If this is a `amazon_pay` PaymentMethod, this sub-hash contains details about the Amazon Pay payment method options.
        """
        au_becs_debit: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsAuBecsDebit"
        ]
        """
        If this is a `au_becs_debit` PaymentMethod, this sub-hash contains details about the AU BECS Direct Debit payment method options.
        """
        bacs_debit: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this sub-hash contains details about the BACS Debit payment method options.
        """
        bancontact: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this sub-hash contains details about the Bancontact payment method options.
        """
        blik: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this sub-hash contains details about the BLIK payment method options.
        """
        boleto: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this sub-hash contains details about the Boleto payment method options.
        """
        card: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsCard"
        ]
        """
        Configuration for any card payments attempted on this PaymentIntent.
        """
        card_present: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsCardPresent"
        ]
        """
        If this is a `card_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        cashapp: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this sub-hash contains details about the Cash App Pay payment method options.
        """
        customer_balance: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsCustomerBalance"
        ]
        """
        If this is a `customer balance` PaymentMethod, this sub-hash contains details about the customer balance payment method options.
        """
        eps: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsEps"
        ]
        """
        If this is a `eps` PaymentMethod, this sub-hash contains details about the EPS payment method options.
        """
        fpx: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsFpx"
        ]
        """
        If this is a `fpx` PaymentMethod, this sub-hash contains details about the FPX payment method options.
        """
        giropay: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this sub-hash contains details about the Giropay payment method options.
        """
        grabpay: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this sub-hash contains details about the Grabpay payment method options.
        """
        ideal: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsIdeal"
        ]
        """
        If this is a `ideal` PaymentMethod, this sub-hash contains details about the Ideal payment method options.
        """
        interac_present: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsInteracPresent"
        ]
        """
        If this is a `interac_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        klarna: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this sub-hash contains details about the Klarna payment method options.
        """
        konbini: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this sub-hash contains details about the Konbini payment method options.
        """
        link: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsLink"
        ]
        """
        If this is a `link` PaymentMethod, this sub-hash contains details about the Link payment method options.
        """
        mobilepay: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsMobilepay"
        ]
        """
        If this is a `MobilePay` PaymentMethod, this sub-hash contains details about the MobilePay payment method options.
        """
        multibanco: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this sub-hash contains details about the Multibanco payment method options.
        """
        oxxo: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsOxxo"
        ]
        """
        If this is a `oxxo` PaymentMethod, this sub-hash contains details about the OXXO payment method options.
        """
        p24: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsP24"
        ]
        """
        If this is a `p24` PaymentMethod, this sub-hash contains details about the Przelewy24 payment method options.
        """
        paynow: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this sub-hash contains details about the PayNow payment method options.
        """
        paypal: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this sub-hash contains details about the PayPal payment method options.
        """
        pix: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsPix"
        ]
        """
        If this is a `pix` PaymentMethod, this sub-hash contains details about the Pix payment method options.
        """
        promptpay: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this sub-hash contains details about the PromptPay payment method options.
        """
        revolut_pay: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsRevolutPay"
        ]
        """
        If this is a `revolut_pay` PaymentMethod, this sub-hash contains details about the Revolut Pay payment method options.
        """
        sepa_debit: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentIntent, this sub-hash contains details about the SEPA Debit payment method options.
        """
        sofort: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this sub-hash contains details about the SOFORT payment method options.
        """
        swish: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsSwish"
        ]
        """
        If this is a `Swish` PaymentMethod, this sub-hash contains details about the Swish payment method options.
        """
        twint: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsTwint"
        ]
        """
        If this is a `twint` PaymentMethod, this sub-hash contains details about the TWINT payment method options.
        """
        us_bank_account: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If this is a `us_bank_account` PaymentMethod, this sub-hash contains details about the US bank account payment method options.
        """
        wechat_pay: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsWechatPay"
        ]
        """
        If this is a `wechat_pay` PaymentMethod, this sub-hash contains details about the WeChat Pay payment method options.
        """
        zip: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsZip"
        ]
        """
        If this is a `zip` PaymentMethod, this sub-hash contains details about the Zip payment method options.
        """

    class ModifyParamsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsAcssDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Bank account verification method.
        """

    class ModifyParamsPaymentMethodOptionsAcssDebitMandateOptions(TypedDict):
        custom_mandate_url: NotRequired["Literal['']|str"]
        """
        A URL for custom mandate text to render during confirmation step.
        The URL will be rendered with additional GET parameters `payment_intent` and `payment_intent_client_secret` when confirming a Payment Intent,
        or `setup_intent` and `setup_intent_client_secret` when confirming a Setup Intent.
        """
        interval_description: NotRequired[str]
        """
        Description of the mandate interval. Only required if 'payment_schedule' parameter is 'interval' or 'combined'.
        """
        payment_schedule: NotRequired[
            Literal["combined", "interval", "sporadic"]
        ]
        """
        Payment schedule for the mandate.
        """
        transaction_type: NotRequired[Literal["business", "personal"]]
        """
        Transaction type of the mandate.
        """

    class ModifyParamsPaymentMethodOptionsAffirm(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        preferred_locale: NotRequired[str]
        """
        Preferred language of the Affirm authorization page that the customer is redirected to.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsAfterpayClearpay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        reference: NotRequired[str]
        """
        An internal identifier or reference that this payment corresponds to. You must limit the identifier to 128 characters, and it can only contain letters, numbers, underscores, backslashes, and dashes.
        This field differs from the statement descriptor and item name.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsAlipay(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsAmazonPay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class ModifyParamsPaymentMethodOptionsAuBecsDebit(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsBacsDebit(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsBancontact(TypedDict):
        preferred_language: NotRequired[Literal["de", "en", "fr", "nl"]]
        """
        Preferred language of the Bancontact authorization page that the customer is redirected to.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsBlik(TypedDict):
        code: NotRequired[str]
        """
        The 6-digit BLIK code that a customer has generated using their banking application. Can only be set on confirmation.
        """
        setup_future_usage: NotRequired["Literal['']|Literal['none']"]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsBoleto(TypedDict):
        expires_after_days: NotRequired[int]
        """
        The number of calendar days before a Boleto voucher expires. For example, if you create a Boleto voucher on Monday and you set expires_after_days to 2, the Boleto invoice will expire on Wednesday at 23:59 America/Sao_Paulo time.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsCard(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        cvc_token: NotRequired[str]
        """
        A single-use `cvc_update` Token that represents a card CVC value. When provided, the CVC value will be verified during the card payment attempt. This parameter can only be provided during confirmation.
        """
        installments: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsCardInstallments"
        ]
        """
        Installment configuration for payments attempted on this PaymentIntent (Mexico Only).

        For more information, see the [installments integration guide](https://stripe.com/docs/payments/installments).
        """
        mandate_options: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsCardMandateOptions"
        ]
        """
        Configuration options for setting up an eMandate for cards issued in India.
        """
        moto: NotRequired[bool]
        """
        When specified, this parameter indicates that a transaction will be marked
        as MOTO (Mail Order Telephone Order) and thus out of scope for SCA. This
        parameter can only be provided during confirmation.
        """
        network: NotRequired[
            Literal[
                "amex",
                "cartes_bancaires",
                "diners",
                "discover",
                "eftpos_au",
                "girocard",
                "interac",
                "jcb",
                "mastercard",
                "unionpay",
                "unknown",
                "visa",
            ]
        ]
        """
        Selected network to process this PaymentIntent on. Depends on the available networks of the card attached to the PaymentIntent. Can be only set confirm-time.
        """
        request_extended_authorization: NotRequired[
            Literal["if_available", "never"]
        ]
        """
        Request ability to [capture beyond the standard authorization validity window](https://stripe.com/docs/payments/extended-authorization) for this PaymentIntent.
        """
        request_incremental_authorization: NotRequired[
            Literal["if_available", "never"]
        ]
        """
        Request ability to [increment the authorization](https://stripe.com/docs/payments/incremental-authorization) for this PaymentIntent.
        """
        request_multicapture: NotRequired[Literal["if_available", "never"]]
        """
        Request ability to make [multiple captures](https://stripe.com/docs/payments/multicapture) for this PaymentIntent.
        """
        request_overcapture: NotRequired[Literal["if_available", "never"]]
        """
        Request ability to [overcapture](https://stripe.com/docs/payments/overcapture) for this PaymentIntent.
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. If not provided, this value defaults to `automatic`. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """
        require_cvc_recollection: NotRequired[bool]
        """
        When enabled, using a card that is attached to a customer will require the CVC to be provided again (i.e. using the cvc_token parameter).
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        statement_descriptor_suffix_kana: NotRequired["Literal['']|str"]
        """
        Provides information about a card payment that customers see on their statements. Concatenated with the Kana prefix (shortened Kana descriptor) or Kana statement descriptor that's set on the account to form the complete statement descriptor. Maximum 22 characters. On card statements, the *concatenation* of both prefix and suffix (including separators) will appear truncated to 22 characters.
        """
        statement_descriptor_suffix_kanji: NotRequired["Literal['']|str"]
        """
        Provides information about a card payment that customers see on their statements. Concatenated with the Kanji prefix (shortened Kanji descriptor) or Kanji statement descriptor that's set on the account to form the complete statement descriptor. Maximum 17 characters. On card statements, the *concatenation* of both prefix and suffix (including separators) will appear truncated to 17 characters.
        """
        three_d_secure: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsCardThreeDSecure"
        ]
        """
        If 3D Secure authentication was performed with a third-party provider,
        the authentication details to use for this payment.
        """

    class ModifyParamsPaymentMethodOptionsCardInstallments(TypedDict):
        enabled: NotRequired[bool]
        """
        Setting to true enables installments for this PaymentIntent.
        This will cause the response to contain a list of available installment plans.
        Setting to false will prevent any selected plan from applying to a charge.
        """
        plan: NotRequired[
            "Literal['']|PaymentIntent.ModifyParamsPaymentMethodOptionsCardInstallmentsPlan"
        ]
        """
        The selected installment plan to use for this payment attempt.
        This parameter can only be provided during confirmation.
        """

    class ModifyParamsPaymentMethodOptionsCardInstallmentsPlan(TypedDict):
        count: NotRequired[int]
        """
        For `fixed_count` installment plans, this is required. It represents the number of installment payments your customer will make to their credit card.
        """
        interval: NotRequired[Literal["month"]]
        """
        For `fixed_count` installment plans, this is required. It represents the interval between installment payments your customer will make to their credit card.
        One of `month`.
        """
        type: Literal["fixed_count"]
        """
        Type of installment plan, one of `fixed_count`.
        """

    class ModifyParamsPaymentMethodOptionsCardMandateOptions(TypedDict):
        amount: int
        """
        Amount to be charged for future payments.
        """
        amount_type: Literal["fixed", "maximum"]
        """
        One of `fixed` or `maximum`. If `fixed`, the `amount` param refers to the exact amount to be charged in future payments. If `maximum`, the amount charged can be up to the value passed for the `amount` param.
        """
        description: NotRequired[str]
        """
        A description of the mandate or subscription that is meant to be displayed to the customer.
        """
        end_date: NotRequired[int]
        """
        End date of the mandate or subscription. If not provided, the mandate will be active until canceled. If provided, end date should be after start date.
        """
        interval: Literal["day", "month", "sporadic", "week", "year"]
        """
        Specifies payment frequency. One of `day`, `week`, `month`, `year`, or `sporadic`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between payments. For example, `interval=month` and `interval_count=3` indicates one payment every three months. Maximum of one year interval allowed (1 year, 12 months, or 52 weeks). This parameter is optional when `interval=sporadic`.
        """
        reference: str
        """
        Unique identifier for the mandate or subscription.
        """
        start_date: int
        """
        Start date of the mandate or subscription. Start date should not be lesser than yesterday.
        """
        supported_types: NotRequired[List[Literal["india"]]]
        """
        Specifies the type of mandates supported. Possible values are `india`.
        """

    class ModifyParamsPaymentMethodOptionsCardPresent(TypedDict):
        request_extended_authorization: NotRequired[bool]
        """
        Request ability to capture this payment beyond the standard [authorization validity window](https://stripe.com/docs/terminal/features/extended-authorizations#authorization-validity)
        """
        request_incremental_authorization_support: NotRequired[bool]
        """
        Request ability to [increment](https://stripe.com/docs/terminal/features/incremental-authorizations) this PaymentIntent if the combination of MCC and card brand is eligible. Check [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported) in the [Confirm](https://stripe.com/docs/api/payment_intents/confirm) response to verify support.
        """
        routing: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsCardPresentRouting"
        ]
        """
        Network routing priority on co-branded EMV cards supporting domestic debit and international card schemes.
        """

    class ModifyParamsPaymentMethodOptionsCardPresentRouting(TypedDict):
        requested_priority: NotRequired[Literal["domestic", "international"]]
        """
        Routing requested priority
        """

    class ModifyParamsPaymentMethodOptionsCardThreeDSecure(TypedDict):
        ares_trans_status: NotRequired[
            Literal["A", "C", "I", "N", "R", "U", "Y"]
        ]
        """
        The `transStatus` returned from the card Issuer's ACS in the ARes.
        """
        cryptogram: str
        """
        The cryptogram, also known as the "authentication value" (AAV, CAVV or
        AEVV). This value is 20 bytes, base64-encoded into a 28-character string.
        (Most 3D Secure providers will return the base64-encoded version, which
        is what you should specify here.)
        """
        electronic_commerce_indicator: NotRequired[
            Literal["01", "02", "05", "06", "07"]
        ]
        """
        The Electronic Commerce Indicator (ECI) is returned by your 3D Secure
        provider and indicates what degree of authentication was performed.
        """
        exemption_indicator: NotRequired[Literal["low_risk", "none"]]
        """
        The exemption requested via 3DS and accepted by the issuer at authentication time.
        """
        network_options: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions"
        ]
        """
        Network specific 3DS fields. Network specific arguments require an
        explicit card brand choice. The parameter `payment_method_options.card.network``
        must be populated accordingly
        """
        requestor_challenge_indicator: NotRequired[str]
        """
        The challenge indicator (`threeDSRequestorChallengeInd`) which was requested in the
        AReq sent to the card Issuer's ACS. A string containing 2 digits from 01-99.
        """
        transaction_id: str
        """
        For 3D Secure 1, the XID. For 3D Secure 2, the Directory Server
        Transaction ID (dsTransID).
        """
        version: Literal["1.0.2", "2.1.0", "2.2.0"]
        """
        The version of 3D Secure that was performed.
        """

    class ModifyParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions(
        TypedDict,
    ):
        cartes_bancaires: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires"
        ]
        """
        Cartes Bancaires-specific 3DS fields.
        """

    class ModifyParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires(
        TypedDict,
    ):
        cb_avalgo: Literal["0", "1", "2", "3", "4", "A"]
        """
        The cryptogram calculation algorithm used by the card Issuer's ACS
        to calculate the Authentication cryptogram. Also known as `cavvAlgorithm`.
        messageExtension: CB-AVALGO
        """
        cb_exemption: NotRequired[str]
        """
        The exemption indicator returned from Cartes Bancaires in the ARes.
        message extension: CB-EXEMPTION; string (4 characters)
        This is a 3 byte bitmap (low significant byte first and most significant
        bit first) that has been Base64 encoded
        """
        cb_score: NotRequired[int]
        """
        The risk score returned from Cartes Bancaires in the ARes.
        message extension: CB-SCORE; numeric value 0-99
        """

    class ModifyParamsPaymentMethodOptionsCashapp(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsCustomerBalance(TypedDict):
        bank_transfer: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsCustomerBalanceBankTransfer"
        ]
        """
        Configuration for the bank transfer funding type, if the `funding_type` is set to `bank_transfer`.
        """
        funding_type: NotRequired[Literal["bank_transfer"]]
        """
        The funding method type to be used when there are not enough funds in the customer balance. Permitted values include: `bank_transfer`.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsCustomerBalanceBankTransfer(
        TypedDict,
    ):
        eu_bank_transfer: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
        ]
        """
        Configuration for the eu_bank_transfer funding type.
        """
        requested_address_types: NotRequired[
            List[
                Literal[
                    "aba",
                    "iban",
                    "sepa",
                    "sort_code",
                    "spei",
                    "swift",
                    "zengin",
                ]
            ]
        ]
        """
        List of address types that should be returned in the financial_addresses response. If not specified, all valid types will be returned.

        Permitted values include: `sort_code`, `zengin`, `iban`, or `spei`.
        """
        type: Literal[
            "eu_bank_transfer",
            "gb_bank_transfer",
            "jp_bank_transfer",
            "mx_bank_transfer",
            "us_bank_transfer",
        ]
        """
        The list of bank transfer types that this PaymentIntent is allowed to use for funding Permitted values include: `eu_bank_transfer`, `gb_bank_transfer`, `jp_bank_transfer`, `mx_bank_transfer`, or `us_bank_transfer`.
        """

    class ModifyParamsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer(
        TypedDict,
    ):
        country: str
        """
        The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
        """

    class ModifyParamsPaymentMethodOptionsEps(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsFpx(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsGiropay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsGrabpay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsIdeal(TypedDict):
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsInteracPresent(TypedDict):
        pass

    class ModifyParamsPaymentMethodOptionsKlarna(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        preferred_locale: NotRequired[
            Literal[
                "cs-CZ",
                "da-DK",
                "de-AT",
                "de-CH",
                "de-DE",
                "el-GR",
                "en-AT",
                "en-AU",
                "en-BE",
                "en-CA",
                "en-CH",
                "en-CZ",
                "en-DE",
                "en-DK",
                "en-ES",
                "en-FI",
                "en-FR",
                "en-GB",
                "en-GR",
                "en-IE",
                "en-IT",
                "en-NL",
                "en-NO",
                "en-NZ",
                "en-PL",
                "en-PT",
                "en-RO",
                "en-SE",
                "en-US",
                "es-ES",
                "es-US",
                "fi-FI",
                "fr-BE",
                "fr-CA",
                "fr-CH",
                "fr-FR",
                "it-CH",
                "it-IT",
                "nb-NO",
                "nl-BE",
                "nl-NL",
                "pl-PL",
                "pt-PT",
                "ro-RO",
                "sv-FI",
                "sv-SE",
            ]
        ]
        """
        Preferred language of the Klarna authorization page that the customer is redirected to
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsKonbini(TypedDict):
        confirmation_number: NotRequired["Literal['']|str"]
        """
        An optional 10 to 11 digit numeric-only string determining the confirmation code at applicable convenience stores. Must not consist of only zeroes and could be rejected in case of insufficient uniqueness. We recommend to use the customer's phone number.
        """
        expires_after_days: NotRequired["Literal['']|int"]
        """
        The number of calendar days (between 1 and 60) after which Konbini payment instructions will expire. For example, if a PaymentIntent is confirmed with Konbini and `expires_after_days` set to 2 on Monday JST, the instructions will expire on Wednesday 23:59:59 JST. Defaults to 3 days.
        """
        expires_at: NotRequired["Literal['']|int"]
        """
        The timestamp at which the Konbini payment instructions will expire. Only one of `expires_after_days` or `expires_at` may be set.
        """
        product_description: NotRequired["Literal['']|str"]
        """
        A product descriptor of up to 22 characters, which will appear to customers at the convenience store.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsLink(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        persistent_token: NotRequired[str]
        """
        [Deprecated] This is a legacy parameter that no longer has any function.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsMobilepay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsMultibanco(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsOxxo(TypedDict):
        expires_after_days: NotRequired[int]
        """
        The number of calendar days before an OXXO voucher expires. For example, if you create an OXXO voucher on Monday and you set expires_after_days to 2, the OXXO invoice will expire on Wednesday at 23:59 America/Mexico_City time.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsP24(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        tos_shown_and_accepted: NotRequired[bool]
        """
        Confirm that the payer has accepted the P24 terms and conditions.
        """

    class ModifyParamsPaymentMethodOptionsPaynow(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsPaypal(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds will be captured from the customer's account.
        """
        preferred_locale: NotRequired[
            Literal[
                "cs-CZ",
                "da-DK",
                "de-AT",
                "de-DE",
                "de-LU",
                "el-GR",
                "en-GB",
                "en-US",
                "es-ES",
                "fi-FI",
                "fr-BE",
                "fr-FR",
                "fr-LU",
                "hu-HU",
                "it-IT",
                "nl-BE",
                "nl-NL",
                "pl-PL",
                "pt-PT",
                "sk-SK",
                "sv-SE",
            ]
        ]
        """
        [Preferred locale](https://stripe.com/docs/payments/paypal/supported-locales) of the PayPal checkout page that the customer is redirected to.
        """
        reference: NotRequired[str]
        """
        A reference of the PayPal transaction visible to customer which is mapped to PayPal's invoice ID. This must be a globally unique ID if you have configured in your PayPal settings to block multiple payments per invoice ID.
        """
        risk_correlation_id: NotRequired[str]
        """
        The risk correlation ID for an on-session payment using a saved PayPal payment method.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsPix(TypedDict):
        expires_after_seconds: NotRequired[int]
        """
        The number of seconds (between 10 and 1209600) after which Pix payment will expire. Defaults to 86400 seconds.
        """
        expires_at: NotRequired[int]
        """
        The timestamp at which the Pix expires (between 10 and 1209600 seconds in the future). Defaults to 1 day in the future.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsPromptpay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsRevolutPay(TypedDict):
        capture_method: NotRequired["Literal['']|Literal['manual']"]
        """
        Controls when the funds are captured from the customer's account.

        If provided, this parameter overrides the behavior of the top-level [capture_method](https://stripe.com/api/payment_intents/update#update_payment_intent-capture_method) for this payment method type when finalizing the payment with this payment method type.

        If `capture_method` is already set on the PaymentIntent, providing an empty value for this parameter unsets the stored value for this payment method type.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class ModifyParamsPaymentMethodOptionsSepaDebit(TypedDict):
        mandate_options: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsSepaDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsSepaDebitMandateOptions(TypedDict):
        pass

    class ModifyParamsPaymentMethodOptionsSofort(TypedDict):
        preferred_language: NotRequired[
            "Literal['']|Literal['de', 'en', 'es', 'fr', 'it', 'nl', 'pl']"
        ]
        """
        Language shown to the payer on redirect.
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsSwish(TypedDict):
        reference: NotRequired["Literal['']|str"]
        """
        The order ID displayed in the Swish app after the payment is authorized.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsTwint(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsUsBankAccount(TypedDict):
        financial_connections: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        mandate_options: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsUsBankAccountMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        networks: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsUsBankAccountNetworks"
        ]
        """
        Additional fields for network related functions
        """
        preferred_settlement_speed: NotRequired[
            "Literal['']|Literal['fastest', 'standard']"
        ]
        """
        Preferred transaction settlement speed
        """
        setup_future_usage: NotRequired[
            "Literal['']|Literal['none', 'off_session', 'on_session']"
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Bank account verification method.
        """

    class ModifyParamsPaymentMethodOptionsUsBankAccountFinancialConnections(
        TypedDict,
    ):
        filters: NotRequired[
            "PaymentIntent.ModifyParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
        ]
        """
        Provide filters for the linked accounts that the customer can select for the payment method
        """
        permissions: NotRequired[
            List[
                Literal[
                    "balances", "ownership", "payment_method", "transactions"
                ]
            ]
        ]
        """
        The list of permissions to request. If this parameter is passed, the `payment_method` permission must be included. Valid permissions include: `balances`, `ownership`, `payment_method`, and `transactions`.
        """
        prefetch: NotRequired[
            List[Literal["balances", "ownership", "transactions"]]
        ]
        """
        List of data features that you would like to retrieve upon account creation.
        """
        return_url: NotRequired[str]
        """
        For webview integrations only. Upon completing OAuth login in the native browser, the user will be redirected to this URL to return to your app.
        """

    class ModifyParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters(
        TypedDict,
    ):
        account_subcategories: NotRequired[
            List[Literal["checking", "savings"]]
        ]
        """
        The account subcategories to use to filter for selectable accounts. Valid subcategories are `checking` and `savings`.
        """

    class ModifyParamsPaymentMethodOptionsUsBankAccountMandateOptions(
        TypedDict,
    ):
        collection_method: NotRequired["Literal['']|Literal['paper']"]
        """
        The method used to collect offline mandate customer acceptance.
        """

    class ModifyParamsPaymentMethodOptionsUsBankAccountNetworks(TypedDict):
        requested: NotRequired[List[Literal["ach", "us_domestic_wire"]]]
        """
        Triggers validations to run across the selected networks
        """

    class ModifyParamsPaymentMethodOptionsWechatPay(TypedDict):
        app_id: NotRequired[str]
        """
        The app ID registered with WeChat Pay. Only required when client is ios or android.
        """
        client: Literal["android", "ios", "web"]
        """
        The client type that the end customer will pay from
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsPaymentMethodOptionsZip(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class ModifyParamsShipping(TypedDict):
        address: "PaymentIntent.ModifyParamsShippingAddress"
        """
        Shipping address.
        """
        carrier: NotRequired[str]
        """
        The delivery service that shipped a physical product, such as Fedex, UPS, USPS, etc.
        """
        name: str
        """
        Recipient name.
        """
        phone: NotRequired[str]
        """
        Recipient phone (including extension).
        """
        tracking_number: NotRequired[str]
        """
        The tracking number for a physical product, obtained from the delivery service. If multiple tracking numbers were generated for this purchase, please separate them with commas.
        """

    class ModifyParamsShippingAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Address line 1 (e.g., street, PO Box, or company name).
        """
        line2: NotRequired[str]
        """
        Address line 2 (e.g., apartment, suite, unit, or building).
        """
        postal_code: NotRequired[str]
        """
        ZIP or postal code.
        """
        state: NotRequired[str]
        """
        State, county, province, or region.
        """

    class ModifyParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount that will be transferred automatically when a charge succeeds.
        """

    class RetrieveParams(RequestOptions):
        client_secret: NotRequired[str]
        """
        The client secret of the PaymentIntent. We require it if you use a publishable key to retrieve the source.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class SearchParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        page: NotRequired[str]
        """
        A cursor for pagination across multiple pages of results. Don't include this parameter on the first call. Use the next_page value returned in a previous response to request subsequent results.
        """
        query: str
        """
        The search query string. See [search query language](https://stripe.com/docs/search#search-query-language) and the list of supported [query fields for payment intents](https://stripe.com/docs/search#query-fields-for-payment-intents).
        """

    class VerifyMicrodepositsParams(RequestOptions):
        amounts: NotRequired[List[int]]
        """
        Two positive integers, in *cents*, equal to the values of the microdeposits sent to the bank account.
        """
        descriptor_code: NotRequired[str]
        """
        A six-character code starting with SM present in the microdeposit sent to the bank account.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    amount: int
    """
    Amount intended to be collected by this PaymentIntent. A positive integer representing how much to charge in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) (e.g., 100 cents to charge $1.00 or 100 to charge ¥100, a zero-decimal currency). The minimum amount is $0.50 US or [equivalent in charge currency](https://stripe.com/docs/currencies#minimum-and-maximum-charge-amounts). The amount value supports up to eight digits (e.g., a value of 99999999 for a USD charge of $999,999.99).
    """
    amount_capturable: int
    """
    Amount that can be captured from this PaymentIntent.
    """
    amount_details: Optional[AmountDetails]
    amount_received: int
    """
    Amount that this PaymentIntent collects.
    """
    application: Optional[ExpandableField["Application"]]
    """
    ID of the Connect application that created the PaymentIntent.
    """
    application_fee_amount: Optional[int]
    """
    The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. The amount of the application fee collected will be capped at the total payment amount. For more information, see the PaymentIntents [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
    """
    automatic_payment_methods: Optional[AutomaticPaymentMethods]
    """
    Settings to configure compatible payment methods from the [Stripe Dashboard](https://dashboard.stripe.com/settings/payment_methods)
    """
    canceled_at: Optional[int]
    """
    Populated when `status` is `canceled`, this is the time at which the PaymentIntent was canceled. Measured in seconds since the Unix epoch.
    """
    cancellation_reason: Optional[
        Literal[
            "abandoned",
            "automatic",
            "duplicate",
            "failed_invoice",
            "fraudulent",
            "requested_by_customer",
            "void_invoice",
        ]
    ]
    """
    Reason for cancellation of this PaymentIntent, either user-provided (`duplicate`, `fraudulent`, `requested_by_customer`, or `abandoned`) or generated by Stripe internally (`failed_invoice`, `void_invoice`, or `automatic`).
    """
    capture_method: Literal["automatic", "automatic_async", "manual"]
    """
    Controls when the funds will be captured from the customer's account.
    """
    client_secret: Optional[str]
    """
    The client secret of this PaymentIntent. Used for client-side retrieval using a publishable key.

    The client secret can be used to complete a payment from your frontend. It should not be stored, logged, or exposed to anyone other than the customer. Make sure that you have TLS enabled on any page that includes the client secret.

    Refer to our docs to [accept a payment](https://stripe.com/docs/payments/accept-a-payment?ui=elements) and learn about how `client_secret` should be handled.
    """
    confirmation_method: Literal["automatic", "manual"]
    """
    Describes whether we can confirm this PaymentIntent automatically, or if it requires customer action to confirm the payment.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    customer: Optional[ExpandableField["Customer"]]
    """
    ID of the Customer this PaymentIntent belongs to, if one exists.

    Payment methods attached to other Customers cannot be used with this PaymentIntent.

    If [setup_future_usage](https://stripe.com/docs/api#payment_intent_object-setup_future_usage) is set and this PaymentIntent's payment method is not `card_present`, then the payment method attaches to the Customer after the PaymentIntent has been confirmed and any required actions from the user are complete. If the payment method is `card_present` and isn't a digital wallet, then a [generated_card](https://docs.corp.stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card is created and attached to the Customer instead.
    """
    description: Optional[str]
    """
    An arbitrary string attached to the object. Often useful for displaying to users.
    """
    id: str
    """
    Unique identifier for the object.
    """
    invoice: Optional[ExpandableField["Invoice"]]
    """
    ID of the invoice that created this PaymentIntent, if it exists.
    """
    last_payment_error: Optional[LastPaymentError]
    """
    The payment error encountered in the previous PaymentIntent confirmation. It will be cleared if the PaymentIntent is later updated for any reason.
    """
    latest_charge: Optional[ExpandableField["Charge"]]
    """
    ID of the latest [Charge object](https://stripe.com/docs/api/charges) created by this PaymentIntent. This property is `null` until PaymentIntent confirmation is attempted.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Learn more about [storing information in metadata](https://stripe.com/docs/payments/payment-intents/creating-payment-intents#storing-information-in-metadata).
    """
    next_action: Optional[NextAction]
    """
    If present, this property tells you what actions you need to take in order for your customer to fulfill a payment using the provided source.
    """
    object: Literal["payment_intent"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    on_behalf_of: Optional[ExpandableField["Account"]]
    """
    The account (if any) for which the funds of the PaymentIntent are intended. See the PaymentIntents [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts) for details.
    """
    payment_method: Optional[ExpandableField["PaymentMethod"]]
    """
    ID of the payment method used in this PaymentIntent.
    """
    payment_method_configuration_details: Optional[
        PaymentMethodConfigurationDetails
    ]
    """
    Information about the payment method configuration used for this PaymentIntent.
    """
    payment_method_options: Optional[PaymentMethodOptions]
    """
    Payment-method-specific configuration for this PaymentIntent.
    """
    payment_method_types: List[str]
    """
    The list of payment method types (e.g. card) that this PaymentIntent is allowed to use.
    """
    processing: Optional[Processing]
    """
    If present, this property tells you about the processing state of the payment.
    """
    receipt_email: Optional[str]
    """
    Email address that the receipt for the resulting payment will be sent to. If `receipt_email` is specified for a payment in live mode, a receipt will be sent regardless of your [email settings](https://dashboard.stripe.com/account/emails).
    """
    review: Optional[ExpandableField["Review"]]
    """
    ID of the review associated with this PaymentIntent, if any.
    """
    setup_future_usage: Optional[Literal["off_session", "on_session"]]
    """
    Indicates that you intend to make future payments with this PaymentIntent's payment method.

    If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

    If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

    When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
    """
    shipping: Optional[Shipping]
    """
    Shipping information for this PaymentIntent.
    """
    source: Optional[
        ExpandableField[
            Union["Account", "BankAccount", "CardResource", "Source"]
        ]
    ]
    """
    This is a legacy field that will be removed in the future. It is the ID of the Source object that is associated with this PaymentIntent, if one was supplied.
    """
    statement_descriptor: Optional[str]
    """
    Text that appears on the customer's statement as the [statement descriptor](https://docs.stripe.com/get-started/account/statement-descriptors) for a non-card charge. This value overrides the account's default statement descriptor. Setting this value for a card charge returns an error. For card charges, set the [statement_descriptor_suffix](https://docs.stripe.com/get-started/account/statement-descriptors#dynamic) instead.
    """
    statement_descriptor_suffix: Optional[str]
    """
    Provides information about a card charge. Concatenated to the account's [statement descriptor prefix](https://docs.corp.stripe.com/get-started/account/statement-descriptors#static) to form the complete statement descriptor that appears on the customer's statement.
    """
    status: Literal[
        "canceled",
        "processing",
        "requires_action",
        "requires_capture",
        "requires_confirmation",
        "requires_payment_method",
        "succeeded",
    ]
    """
    Status of this PaymentIntent, one of `requires_payment_method`, `requires_confirmation`, `requires_action`, `processing`, `requires_capture`, `canceled`, or `succeeded`. Read more about each PaymentIntent [status](https://stripe.com/docs/payments/intents#intent-statuses).
    """
    transfer_data: Optional[TransferData]
    """
    The data that automatically creates a Transfer after the payment finalizes. Learn more about the [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
    """
    transfer_group: Optional[str]
    """
    A string that identifies the resulting payment as part of a group. Learn more about the [use case for connected accounts](https://stripe.com/docs/connect/separate-charges-and-transfers).
    """

    @classmethod
    def _cls_apply_customer_balance(
        cls,
        intent: str,
        **params: Unpack["PaymentIntent.ApplyCustomerBalanceParams"],
    ) -> "PaymentIntent":
        """
        Manually reconcile the remaining amount for a customer_balance PaymentIntent.
        """
        return cast(
            "PaymentIntent",
            cls._static_request(
                "post",
                "/v1/payment_intents/{intent}/apply_customer_balance".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def apply_customer_balance(
        intent: str,
        **params: Unpack["PaymentIntent.ApplyCustomerBalanceParams"],
    ) -> "PaymentIntent":
        """
        Manually reconcile the remaining amount for a customer_balance PaymentIntent.
        """
        ...

    @overload
    def apply_customer_balance(
        self, **params: Unpack["PaymentIntent.ApplyCustomerBalanceParams"]
    ) -> "PaymentIntent":
        """
        Manually reconcile the remaining amount for a customer_balance PaymentIntent.
        """
        ...

    @class_method_variant("_cls_apply_customer_balance")
    def apply_customer_balance(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.ApplyCustomerBalanceParams"]
    ) -> "PaymentIntent":
        """
        Manually reconcile the remaining amount for a customer_balance PaymentIntent.
        """
        return cast(
            "PaymentIntent",
            self._request(
                "post",
                "/v1/payment_intents/{intent}/apply_customer_balance".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_apply_customer_balance_async(
        cls,
        intent: str,
        **params: Unpack["PaymentIntent.ApplyCustomerBalanceParams"],
    ) -> "PaymentIntent":
        """
        Manually reconcile the remaining amount for a customer_balance PaymentIntent.
        """
        return cast(
            "PaymentIntent",
            await cls._static_request_async(
                "post",
                "/v1/payment_intents/{intent}/apply_customer_balance".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def apply_customer_balance_async(
        intent: str,
        **params: Unpack["PaymentIntent.ApplyCustomerBalanceParams"],
    ) -> "PaymentIntent":
        """
        Manually reconcile the remaining amount for a customer_balance PaymentIntent.
        """
        ...

    @overload
    async def apply_customer_balance_async(
        self, **params: Unpack["PaymentIntent.ApplyCustomerBalanceParams"]
    ) -> "PaymentIntent":
        """
        Manually reconcile the remaining amount for a customer_balance PaymentIntent.
        """
        ...

    @class_method_variant("_cls_apply_customer_balance_async")
    async def apply_customer_balance_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.ApplyCustomerBalanceParams"]
    ) -> "PaymentIntent":
        """
        Manually reconcile the remaining amount for a customer_balance PaymentIntent.
        """
        return cast(
            "PaymentIntent",
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/apply_customer_balance".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def _cls_cancel(
        cls, intent: str, **params: Unpack["PaymentIntent.CancelParams"]
    ) -> "PaymentIntent":
        """
        You can cancel a PaymentIntent object when it's in one of these statuses: requires_payment_method, requires_capture, requires_confirmation, requires_action or, [in rare cases](https://stripe.com/docs/payments/intents), processing.

        After it's canceled, no additional charges are made by the PaymentIntent and any operations on the PaymentIntent fail with an error. For PaymentIntents with a status of requires_capture, the remaining amount_capturable is automatically refunded.

        You can't cancel the PaymentIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        return cast(
            "PaymentIntent",
            cls._static_request(
                "post",
                "/v1/payment_intents/{intent}/cancel".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def cancel(
        intent: str, **params: Unpack["PaymentIntent.CancelParams"]
    ) -> "PaymentIntent":
        """
        You can cancel a PaymentIntent object when it's in one of these statuses: requires_payment_method, requires_capture, requires_confirmation, requires_action or, [in rare cases](https://stripe.com/docs/payments/intents), processing.

        After it's canceled, no additional charges are made by the PaymentIntent and any operations on the PaymentIntent fail with an error. For PaymentIntents with a status of requires_capture, the remaining amount_capturable is automatically refunded.

        You can't cancel the PaymentIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        ...

    @overload
    def cancel(
        self, **params: Unpack["PaymentIntent.CancelParams"]
    ) -> "PaymentIntent":
        """
        You can cancel a PaymentIntent object when it's in one of these statuses: requires_payment_method, requires_capture, requires_confirmation, requires_action or, [in rare cases](https://stripe.com/docs/payments/intents), processing.

        After it's canceled, no additional charges are made by the PaymentIntent and any operations on the PaymentIntent fail with an error. For PaymentIntents with a status of requires_capture, the remaining amount_capturable is automatically refunded.

        You can't cancel the PaymentIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        ...

    @class_method_variant("_cls_cancel")
    def cancel(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.CancelParams"]
    ) -> "PaymentIntent":
        """
        You can cancel a PaymentIntent object when it's in one of these statuses: requires_payment_method, requires_capture, requires_confirmation, requires_action or, [in rare cases](https://stripe.com/docs/payments/intents), processing.

        After it's canceled, no additional charges are made by the PaymentIntent and any operations on the PaymentIntent fail with an error. For PaymentIntents with a status of requires_capture, the remaining amount_capturable is automatically refunded.

        You can't cancel the PaymentIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        return cast(
            "PaymentIntent",
            self._request(
                "post",
                "/v1/payment_intents/{intent}/cancel".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_cancel_async(
        cls, intent: str, **params: Unpack["PaymentIntent.CancelParams"]
    ) -> "PaymentIntent":
        """
        You can cancel a PaymentIntent object when it's in one of these statuses: requires_payment_method, requires_capture, requires_confirmation, requires_action or, [in rare cases](https://stripe.com/docs/payments/intents), processing.

        After it's canceled, no additional charges are made by the PaymentIntent and any operations on the PaymentIntent fail with an error. For PaymentIntents with a status of requires_capture, the remaining amount_capturable is automatically refunded.

        You can't cancel the PaymentIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        return cast(
            "PaymentIntent",
            await cls._static_request_async(
                "post",
                "/v1/payment_intents/{intent}/cancel".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def cancel_async(
        intent: str, **params: Unpack["PaymentIntent.CancelParams"]
    ) -> "PaymentIntent":
        """
        You can cancel a PaymentIntent object when it's in one of these statuses: requires_payment_method, requires_capture, requires_confirmation, requires_action or, [in rare cases](https://stripe.com/docs/payments/intents), processing.

        After it's canceled, no additional charges are made by the PaymentIntent and any operations on the PaymentIntent fail with an error. For PaymentIntents with a status of requires_capture, the remaining amount_capturable is automatically refunded.

        You can't cancel the PaymentIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        ...

    @overload
    async def cancel_async(
        self, **params: Unpack["PaymentIntent.CancelParams"]
    ) -> "PaymentIntent":
        """
        You can cancel a PaymentIntent object when it's in one of these statuses: requires_payment_method, requires_capture, requires_confirmation, requires_action or, [in rare cases](https://stripe.com/docs/payments/intents), processing.

        After it's canceled, no additional charges are made by the PaymentIntent and any operations on the PaymentIntent fail with an error. For PaymentIntents with a status of requires_capture, the remaining amount_capturable is automatically refunded.

        You can't cancel the PaymentIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        ...

    @class_method_variant("_cls_cancel_async")
    async def cancel_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.CancelParams"]
    ) -> "PaymentIntent":
        """
        You can cancel a PaymentIntent object when it's in one of these statuses: requires_payment_method, requires_capture, requires_confirmation, requires_action or, [in rare cases](https://stripe.com/docs/payments/intents), processing.

        After it's canceled, no additional charges are made by the PaymentIntent and any operations on the PaymentIntent fail with an error. For PaymentIntents with a status of requires_capture, the remaining amount_capturable is automatically refunded.

        You can't cancel the PaymentIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        return cast(
            "PaymentIntent",
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/cancel".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def _cls_capture(
        cls, intent: str, **params: Unpack["PaymentIntent.CaptureParams"]
    ) -> "PaymentIntent":
        """
        Capture the funds of an existing uncaptured PaymentIntent when its status is requires_capture.

        Uncaptured PaymentIntents are cancelled a set number of days (7 by default) after their creation.

        Learn more about [separate authorization and capture](https://stripe.com/docs/payments/capture-later).
        """
        return cast(
            "PaymentIntent",
            cls._static_request(
                "post",
                "/v1/payment_intents/{intent}/capture".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def capture(
        intent: str, **params: Unpack["PaymentIntent.CaptureParams"]
    ) -> "PaymentIntent":
        """
        Capture the funds of an existing uncaptured PaymentIntent when its status is requires_capture.

        Uncaptured PaymentIntents are cancelled a set number of days (7 by default) after their creation.

        Learn more about [separate authorization and capture](https://stripe.com/docs/payments/capture-later).
        """
        ...

    @overload
    def capture(
        self, **params: Unpack["PaymentIntent.CaptureParams"]
    ) -> "PaymentIntent":
        """
        Capture the funds of an existing uncaptured PaymentIntent when its status is requires_capture.

        Uncaptured PaymentIntents are cancelled a set number of days (7 by default) after their creation.

        Learn more about [separate authorization and capture](https://stripe.com/docs/payments/capture-later).
        """
        ...

    @class_method_variant("_cls_capture")
    def capture(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.CaptureParams"]
    ) -> "PaymentIntent":
        """
        Capture the funds of an existing uncaptured PaymentIntent when its status is requires_capture.

        Uncaptured PaymentIntents are cancelled a set number of days (7 by default) after their creation.

        Learn more about [separate authorization and capture](https://stripe.com/docs/payments/capture-later).
        """
        return cast(
            "PaymentIntent",
            self._request(
                "post",
                "/v1/payment_intents/{intent}/capture".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_capture_async(
        cls, intent: str, **params: Unpack["PaymentIntent.CaptureParams"]
    ) -> "PaymentIntent":
        """
        Capture the funds of an existing uncaptured PaymentIntent when its status is requires_capture.

        Uncaptured PaymentIntents are cancelled a set number of days (7 by default) after their creation.

        Learn more about [separate authorization and capture](https://stripe.com/docs/payments/capture-later).
        """
        return cast(
            "PaymentIntent",
            await cls._static_request_async(
                "post",
                "/v1/payment_intents/{intent}/capture".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def capture_async(
        intent: str, **params: Unpack["PaymentIntent.CaptureParams"]
    ) -> "PaymentIntent":
        """
        Capture the funds of an existing uncaptured PaymentIntent when its status is requires_capture.

        Uncaptured PaymentIntents are cancelled a set number of days (7 by default) after their creation.

        Learn more about [separate authorization and capture](https://stripe.com/docs/payments/capture-later).
        """
        ...

    @overload
    async def capture_async(
        self, **params: Unpack["PaymentIntent.CaptureParams"]
    ) -> "PaymentIntent":
        """
        Capture the funds of an existing uncaptured PaymentIntent when its status is requires_capture.

        Uncaptured PaymentIntents are cancelled a set number of days (7 by default) after their creation.

        Learn more about [separate authorization and capture](https://stripe.com/docs/payments/capture-later).
        """
        ...

    @class_method_variant("_cls_capture_async")
    async def capture_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.CaptureParams"]
    ) -> "PaymentIntent":
        """
        Capture the funds of an existing uncaptured PaymentIntent when its status is requires_capture.

        Uncaptured PaymentIntents are cancelled a set number of days (7 by default) after their creation.

        Learn more about [separate authorization and capture](https://stripe.com/docs/payments/capture-later).
        """
        return cast(
            "PaymentIntent",
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/capture".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def _cls_confirm(
        cls, intent: str, **params: Unpack["PaymentIntent.ConfirmParams"]
    ) -> "PaymentIntent":
        """
        Confirm that your customer intends to pay with current or provided
        payment method. Upon confirmation, the PaymentIntent will attempt to initiate
        a payment.
        If the selected payment method requires additional authentication steps, the
        PaymentIntent will transition to the requires_action status and
        suggest additional actions via next_action. If payment fails,
        the PaymentIntent transitions to the requires_payment_method status or the
        canceled status if the confirmation limit is reached. If
        payment succeeds, the PaymentIntent will transition to the succeeded
        status (or requires_capture, if capture_method is set to manual).
        If the confirmation_method is automatic, payment may be attempted
        using our [client SDKs](https://stripe.com/docs/stripe-js/reference#stripe-handle-card-payment)
        and the PaymentIntent's [client_secret](https://stripe.com/docs/api#payment_intent_object-client_secret).
        After next_actions are handled by the client, no additional
        confirmation is required to complete the payment.
        If the confirmation_method is manual, all payment attempts must be
        initiated using a secret key.
        If any actions are required for the payment, the PaymentIntent will
        return to the requires_confirmation state
        after those actions are completed. Your server needs to then
        explicitly re-confirm the PaymentIntent to initiate the next payment
        attempt.
        """
        return cast(
            "PaymentIntent",
            cls._static_request(
                "post",
                "/v1/payment_intents/{intent}/confirm".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def confirm(
        intent: str, **params: Unpack["PaymentIntent.ConfirmParams"]
    ) -> "PaymentIntent":
        """
        Confirm that your customer intends to pay with current or provided
        payment method. Upon confirmation, the PaymentIntent will attempt to initiate
        a payment.
        If the selected payment method requires additional authentication steps, the
        PaymentIntent will transition to the requires_action status and
        suggest additional actions via next_action. If payment fails,
        the PaymentIntent transitions to the requires_payment_method status or the
        canceled status if the confirmation limit is reached. If
        payment succeeds, the PaymentIntent will transition to the succeeded
        status (or requires_capture, if capture_method is set to manual).
        If the confirmation_method is automatic, payment may be attempted
        using our [client SDKs](https://stripe.com/docs/stripe-js/reference#stripe-handle-card-payment)
        and the PaymentIntent's [client_secret](https://stripe.com/docs/api#payment_intent_object-client_secret).
        After next_actions are handled by the client, no additional
        confirmation is required to complete the payment.
        If the confirmation_method is manual, all payment attempts must be
        initiated using a secret key.
        If any actions are required for the payment, the PaymentIntent will
        return to the requires_confirmation state
        after those actions are completed. Your server needs to then
        explicitly re-confirm the PaymentIntent to initiate the next payment
        attempt.
        """
        ...

    @overload
    def confirm(
        self, **params: Unpack["PaymentIntent.ConfirmParams"]
    ) -> "PaymentIntent":
        """
        Confirm that your customer intends to pay with current or provided
        payment method. Upon confirmation, the PaymentIntent will attempt to initiate
        a payment.
        If the selected payment method requires additional authentication steps, the
        PaymentIntent will transition to the requires_action status and
        suggest additional actions via next_action. If payment fails,
        the PaymentIntent transitions to the requires_payment_method status or the
        canceled status if the confirmation limit is reached. If
        payment succeeds, the PaymentIntent will transition to the succeeded
        status (or requires_capture, if capture_method is set to manual).
        If the confirmation_method is automatic, payment may be attempted
        using our [client SDKs](https://stripe.com/docs/stripe-js/reference#stripe-handle-card-payment)
        and the PaymentIntent's [client_secret](https://stripe.com/docs/api#payment_intent_object-client_secret).
        After next_actions are handled by the client, no additional
        confirmation is required to complete the payment.
        If the confirmation_method is manual, all payment attempts must be
        initiated using a secret key.
        If any actions are required for the payment, the PaymentIntent will
        return to the requires_confirmation state
        after those actions are completed. Your server needs to then
        explicitly re-confirm the PaymentIntent to initiate the next payment
        attempt.
        """
        ...

    @class_method_variant("_cls_confirm")
    def confirm(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.ConfirmParams"]
    ) -> "PaymentIntent":
        """
        Confirm that your customer intends to pay with current or provided
        payment method. Upon confirmation, the PaymentIntent will attempt to initiate
        a payment.
        If the selected payment method requires additional authentication steps, the
        PaymentIntent will transition to the requires_action status and
        suggest additional actions via next_action. If payment fails,
        the PaymentIntent transitions to the requires_payment_method status or the
        canceled status if the confirmation limit is reached. If
        payment succeeds, the PaymentIntent will transition to the succeeded
        status (or requires_capture, if capture_method is set to manual).
        If the confirmation_method is automatic, payment may be attempted
        using our [client SDKs](https://stripe.com/docs/stripe-js/reference#stripe-handle-card-payment)
        and the PaymentIntent's [client_secret](https://stripe.com/docs/api#payment_intent_object-client_secret).
        After next_actions are handled by the client, no additional
        confirmation is required to complete the payment.
        If the confirmation_method is manual, all payment attempts must be
        initiated using a secret key.
        If any actions are required for the payment, the PaymentIntent will
        return to the requires_confirmation state
        after those actions are completed. Your server needs to then
        explicitly re-confirm the PaymentIntent to initiate the next payment
        attempt.
        """
        return cast(
            "PaymentIntent",
            self._request(
                "post",
                "/v1/payment_intents/{intent}/confirm".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_confirm_async(
        cls, intent: str, **params: Unpack["PaymentIntent.ConfirmParams"]
    ) -> "PaymentIntent":
        """
        Confirm that your customer intends to pay with current or provided
        payment method. Upon confirmation, the PaymentIntent will attempt to initiate
        a payment.
        If the selected payment method requires additional authentication steps, the
        PaymentIntent will transition to the requires_action status and
        suggest additional actions via next_action. If payment fails,
        the PaymentIntent transitions to the requires_payment_method status or the
        canceled status if the confirmation limit is reached. If
        payment succeeds, the PaymentIntent will transition to the succeeded
        status (or requires_capture, if capture_method is set to manual).
        If the confirmation_method is automatic, payment may be attempted
        using our [client SDKs](https://stripe.com/docs/stripe-js/reference#stripe-handle-card-payment)
        and the PaymentIntent's [client_secret](https://stripe.com/docs/api#payment_intent_object-client_secret).
        After next_actions are handled by the client, no additional
        confirmation is required to complete the payment.
        If the confirmation_method is manual, all payment attempts must be
        initiated using a secret key.
        If any actions are required for the payment, the PaymentIntent will
        return to the requires_confirmation state
        after those actions are completed. Your server needs to then
        explicitly re-confirm the PaymentIntent to initiate the next payment
        attempt.
        """
        return cast(
            "PaymentIntent",
            await cls._static_request_async(
                "post",
                "/v1/payment_intents/{intent}/confirm".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def confirm_async(
        intent: str, **params: Unpack["PaymentIntent.ConfirmParams"]
    ) -> "PaymentIntent":
        """
        Confirm that your customer intends to pay with current or provided
        payment method. Upon confirmation, the PaymentIntent will attempt to initiate
        a payment.
        If the selected payment method requires additional authentication steps, the
        PaymentIntent will transition to the requires_action status and
        suggest additional actions via next_action. If payment fails,
        the PaymentIntent transitions to the requires_payment_method status or the
        canceled status if the confirmation limit is reached. If
        payment succeeds, the PaymentIntent will transition to the succeeded
        status (or requires_capture, if capture_method is set to manual).
        If the confirmation_method is automatic, payment may be attempted
        using our [client SDKs](https://stripe.com/docs/stripe-js/reference#stripe-handle-card-payment)
        and the PaymentIntent's [client_secret](https://stripe.com/docs/api#payment_intent_object-client_secret).
        After next_actions are handled by the client, no additional
        confirmation is required to complete the payment.
        If the confirmation_method is manual, all payment attempts must be
        initiated using a secret key.
        If any actions are required for the payment, the PaymentIntent will
        return to the requires_confirmation state
        after those actions are completed. Your server needs to then
        explicitly re-confirm the PaymentIntent to initiate the next payment
        attempt.
        """
        ...

    @overload
    async def confirm_async(
        self, **params: Unpack["PaymentIntent.ConfirmParams"]
    ) -> "PaymentIntent":
        """
        Confirm that your customer intends to pay with current or provided
        payment method. Upon confirmation, the PaymentIntent will attempt to initiate
        a payment.
        If the selected payment method requires additional authentication steps, the
        PaymentIntent will transition to the requires_action status and
        suggest additional actions via next_action. If payment fails,
        the PaymentIntent transitions to the requires_payment_method status or the
        canceled status if the confirmation limit is reached. If
        payment succeeds, the PaymentIntent will transition to the succeeded
        status (or requires_capture, if capture_method is set to manual).
        If the confirmation_method is automatic, payment may be attempted
        using our [client SDKs](https://stripe.com/docs/stripe-js/reference#stripe-handle-card-payment)
        and the PaymentIntent's [client_secret](https://stripe.com/docs/api#payment_intent_object-client_secret).
        After next_actions are handled by the client, no additional
        confirmation is required to complete the payment.
        If the confirmation_method is manual, all payment attempts must be
        initiated using a secret key.
        If any actions are required for the payment, the PaymentIntent will
        return to the requires_confirmation state
        after those actions are completed. Your server needs to then
        explicitly re-confirm the PaymentIntent to initiate the next payment
        attempt.
        """
        ...

    @class_method_variant("_cls_confirm_async")
    async def confirm_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.ConfirmParams"]
    ) -> "PaymentIntent":
        """
        Confirm that your customer intends to pay with current or provided
        payment method. Upon confirmation, the PaymentIntent will attempt to initiate
        a payment.
        If the selected payment method requires additional authentication steps, the
        PaymentIntent will transition to the requires_action status and
        suggest additional actions via next_action. If payment fails,
        the PaymentIntent transitions to the requires_payment_method status or the
        canceled status if the confirmation limit is reached. If
        payment succeeds, the PaymentIntent will transition to the succeeded
        status (or requires_capture, if capture_method is set to manual).
        If the confirmation_method is automatic, payment may be attempted
        using our [client SDKs](https://stripe.com/docs/stripe-js/reference#stripe-handle-card-payment)
        and the PaymentIntent's [client_secret](https://stripe.com/docs/api#payment_intent_object-client_secret).
        After next_actions are handled by the client, no additional
        confirmation is required to complete the payment.
        If the confirmation_method is manual, all payment attempts must be
        initiated using a secret key.
        If any actions are required for the payment, the PaymentIntent will
        return to the requires_confirmation state
        after those actions are completed. Your server needs to then
        explicitly re-confirm the PaymentIntent to initiate the next payment
        attempt.
        """
        return cast(
            "PaymentIntent",
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/confirm".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def create(
        cls, **params: Unpack["PaymentIntent.CreateParams"]
    ) -> "PaymentIntent":
        """
        Creates a PaymentIntent object.

        After the PaymentIntent is created, attach a payment method and [confirm](https://stripe.com/docs/api/payment_intents/confirm)
        to continue the payment. Learn more about <a href="/docs/payments/payment-intents">the available payment flows
        with the Payment Intents API.

        When you use confirm=true during creation, it's equivalent to creating
        and confirming the PaymentIntent in the same call. You can use any parameters
        available in the [confirm API](https://stripe.com/docs/api/payment_intents/confirm) when you supply
        confirm=true.
        """
        return cast(
            "PaymentIntent",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["PaymentIntent.CreateParams"]
    ) -> "PaymentIntent":
        """
        Creates a PaymentIntent object.

        After the PaymentIntent is created, attach a payment method and [confirm](https://stripe.com/docs/api/payment_intents/confirm)
        to continue the payment. Learn more about <a href="/docs/payments/payment-intents">the available payment flows
        with the Payment Intents API.

        When you use confirm=true during creation, it's equivalent to creating
        and confirming the PaymentIntent in the same call. You can use any parameters
        available in the [confirm API](https://stripe.com/docs/api/payment_intents/confirm) when you supply
        confirm=true.
        """
        return cast(
            "PaymentIntent",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_increment_authorization(
        cls,
        intent: str,
        **params: Unpack["PaymentIntent.IncrementAuthorizationParams"],
    ) -> "PaymentIntent":
        """
        Perform an incremental authorization on an eligible
        [PaymentIntent](https://stripe.com/docs/api/payment_intents/object). To be eligible, the
        PaymentIntent's status must be requires_capture and
        [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported)
        must be true.

        Incremental authorizations attempt to increase the authorized amount on
        your customer's card to the new, higher amount provided. Similar to the
        initial authorization, incremental authorizations can be declined. A
        single PaymentIntent can call this endpoint multiple times to further
        increase the authorized amount.

        If the incremental authorization succeeds, the PaymentIntent object
        returns with the updated
        [amount](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-amount).
        If the incremental authorization fails, a
        [card_declined](https://stripe.com/docs/error-codes#card-declined) error returns, and no other
        fields on the PaymentIntent or Charge update. The PaymentIntent
        object remains capturable for the previously authorized amount.

        Each PaymentIntent can have a maximum of 10 incremental authorization attempts, including declines.
        After it's captured, a PaymentIntent can no longer be incremented.

        Learn more about [incremental authorizations](https://stripe.com/docs/terminal/features/incremental-authorizations).
        """
        return cast(
            "PaymentIntent",
            cls._static_request(
                "post",
                "/v1/payment_intents/{intent}/increment_authorization".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def increment_authorization(
        intent: str,
        **params: Unpack["PaymentIntent.IncrementAuthorizationParams"],
    ) -> "PaymentIntent":
        """
        Perform an incremental authorization on an eligible
        [PaymentIntent](https://stripe.com/docs/api/payment_intents/object). To be eligible, the
        PaymentIntent's status must be requires_capture and
        [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported)
        must be true.

        Incremental authorizations attempt to increase the authorized amount on
        your customer's card to the new, higher amount provided. Similar to the
        initial authorization, incremental authorizations can be declined. A
        single PaymentIntent can call this endpoint multiple times to further
        increase the authorized amount.

        If the incremental authorization succeeds, the PaymentIntent object
        returns with the updated
        [amount](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-amount).
        If the incremental authorization fails, a
        [card_declined](https://stripe.com/docs/error-codes#card-declined) error returns, and no other
        fields on the PaymentIntent or Charge update. The PaymentIntent
        object remains capturable for the previously authorized amount.

        Each PaymentIntent can have a maximum of 10 incremental authorization attempts, including declines.
        After it's captured, a PaymentIntent can no longer be incremented.

        Learn more about [incremental authorizations](https://stripe.com/docs/terminal/features/incremental-authorizations).
        """
        ...

    @overload
    def increment_authorization(
        self, **params: Unpack["PaymentIntent.IncrementAuthorizationParams"]
    ) -> "PaymentIntent":
        """
        Perform an incremental authorization on an eligible
        [PaymentIntent](https://stripe.com/docs/api/payment_intents/object). To be eligible, the
        PaymentIntent's status must be requires_capture and
        [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported)
        must be true.

        Incremental authorizations attempt to increase the authorized amount on
        your customer's card to the new, higher amount provided. Similar to the
        initial authorization, incremental authorizations can be declined. A
        single PaymentIntent can call this endpoint multiple times to further
        increase the authorized amount.

        If the incremental authorization succeeds, the PaymentIntent object
        returns with the updated
        [amount](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-amount).
        If the incremental authorization fails, a
        [card_declined](https://stripe.com/docs/error-codes#card-declined) error returns, and no other
        fields on the PaymentIntent or Charge update. The PaymentIntent
        object remains capturable for the previously authorized amount.

        Each PaymentIntent can have a maximum of 10 incremental authorization attempts, including declines.
        After it's captured, a PaymentIntent can no longer be incremented.

        Learn more about [incremental authorizations](https://stripe.com/docs/terminal/features/incremental-authorizations).
        """
        ...

    @class_method_variant("_cls_increment_authorization")
    def increment_authorization(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.IncrementAuthorizationParams"]
    ) -> "PaymentIntent":
        """
        Perform an incremental authorization on an eligible
        [PaymentIntent](https://stripe.com/docs/api/payment_intents/object). To be eligible, the
        PaymentIntent's status must be requires_capture and
        [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported)
        must be true.

        Incremental authorizations attempt to increase the authorized amount on
        your customer's card to the new, higher amount provided. Similar to the
        initial authorization, incremental authorizations can be declined. A
        single PaymentIntent can call this endpoint multiple times to further
        increase the authorized amount.

        If the incremental authorization succeeds, the PaymentIntent object
        returns with the updated
        [amount](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-amount).
        If the incremental authorization fails, a
        [card_declined](https://stripe.com/docs/error-codes#card-declined) error returns, and no other
        fields on the PaymentIntent or Charge update. The PaymentIntent
        object remains capturable for the previously authorized amount.

        Each PaymentIntent can have a maximum of 10 incremental authorization attempts, including declines.
        After it's captured, a PaymentIntent can no longer be incremented.

        Learn more about [incremental authorizations](https://stripe.com/docs/terminal/features/incremental-authorizations).
        """
        return cast(
            "PaymentIntent",
            self._request(
                "post",
                "/v1/payment_intents/{intent}/increment_authorization".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_increment_authorization_async(
        cls,
        intent: str,
        **params: Unpack["PaymentIntent.IncrementAuthorizationParams"],
    ) -> "PaymentIntent":
        """
        Perform an incremental authorization on an eligible
        [PaymentIntent](https://stripe.com/docs/api/payment_intents/object). To be eligible, the
        PaymentIntent's status must be requires_capture and
        [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported)
        must be true.

        Incremental authorizations attempt to increase the authorized amount on
        your customer's card to the new, higher amount provided. Similar to the
        initial authorization, incremental authorizations can be declined. A
        single PaymentIntent can call this endpoint multiple times to further
        increase the authorized amount.

        If the incremental authorization succeeds, the PaymentIntent object
        returns with the updated
        [amount](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-amount).
        If the incremental authorization fails, a
        [card_declined](https://stripe.com/docs/error-codes#card-declined) error returns, and no other
        fields on the PaymentIntent or Charge update. The PaymentIntent
        object remains capturable for the previously authorized amount.

        Each PaymentIntent can have a maximum of 10 incremental authorization attempts, including declines.
        After it's captured, a PaymentIntent can no longer be incremented.

        Learn more about [incremental authorizations](https://stripe.com/docs/terminal/features/incremental-authorizations).
        """
        return cast(
            "PaymentIntent",
            await cls._static_request_async(
                "post",
                "/v1/payment_intents/{intent}/increment_authorization".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def increment_authorization_async(
        intent: str,
        **params: Unpack["PaymentIntent.IncrementAuthorizationParams"],
    ) -> "PaymentIntent":
        """
        Perform an incremental authorization on an eligible
        [PaymentIntent](https://stripe.com/docs/api/payment_intents/object). To be eligible, the
        PaymentIntent's status must be requires_capture and
        [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported)
        must be true.

        Incremental authorizations attempt to increase the authorized amount on
        your customer's card to the new, higher amount provided. Similar to the
        initial authorization, incremental authorizations can be declined. A
        single PaymentIntent can call this endpoint multiple times to further
        increase the authorized amount.

        If the incremental authorization succeeds, the PaymentIntent object
        returns with the updated
        [amount](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-amount).
        If the incremental authorization fails, a
        [card_declined](https://stripe.com/docs/error-codes#card-declined) error returns, and no other
        fields on the PaymentIntent or Charge update. The PaymentIntent
        object remains capturable for the previously authorized amount.

        Each PaymentIntent can have a maximum of 10 incremental authorization attempts, including declines.
        After it's captured, a PaymentIntent can no longer be incremented.

        Learn more about [incremental authorizations](https://stripe.com/docs/terminal/features/incremental-authorizations).
        """
        ...

    @overload
    async def increment_authorization_async(
        self, **params: Unpack["PaymentIntent.IncrementAuthorizationParams"]
    ) -> "PaymentIntent":
        """
        Perform an incremental authorization on an eligible
        [PaymentIntent](https://stripe.com/docs/api/payment_intents/object). To be eligible, the
        PaymentIntent's status must be requires_capture and
        [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported)
        must be true.

        Incremental authorizations attempt to increase the authorized amount on
        your customer's card to the new, higher amount provided. Similar to the
        initial authorization, incremental authorizations can be declined. A
        single PaymentIntent can call this endpoint multiple times to further
        increase the authorized amount.

        If the incremental authorization succeeds, the PaymentIntent object
        returns with the updated
        [amount](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-amount).
        If the incremental authorization fails, a
        [card_declined](https://stripe.com/docs/error-codes#card-declined) error returns, and no other
        fields on the PaymentIntent or Charge update. The PaymentIntent
        object remains capturable for the previously authorized amount.

        Each PaymentIntent can have a maximum of 10 incremental authorization attempts, including declines.
        After it's captured, a PaymentIntent can no longer be incremented.

        Learn more about [incremental authorizations](https://stripe.com/docs/terminal/features/incremental-authorizations).
        """
        ...

    @class_method_variant("_cls_increment_authorization_async")
    async def increment_authorization_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.IncrementAuthorizationParams"]
    ) -> "PaymentIntent":
        """
        Perform an incremental authorization on an eligible
        [PaymentIntent](https://stripe.com/docs/api/payment_intents/object). To be eligible, the
        PaymentIntent's status must be requires_capture and
        [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported)
        must be true.

        Incremental authorizations attempt to increase the authorized amount on
        your customer's card to the new, higher amount provided. Similar to the
        initial authorization, incremental authorizations can be declined. A
        single PaymentIntent can call this endpoint multiple times to further
        increase the authorized amount.

        If the incremental authorization succeeds, the PaymentIntent object
        returns with the updated
        [amount](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-amount).
        If the incremental authorization fails, a
        [card_declined](https://stripe.com/docs/error-codes#card-declined) error returns, and no other
        fields on the PaymentIntent or Charge update. The PaymentIntent
        object remains capturable for the previously authorized amount.

        Each PaymentIntent can have a maximum of 10 incremental authorization attempts, including declines.
        After it's captured, a PaymentIntent can no longer be incremented.

        Learn more about [incremental authorizations](https://stripe.com/docs/terminal/features/incremental-authorizations).
        """
        return cast(
            "PaymentIntent",
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/increment_authorization".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["PaymentIntent.ListParams"]
    ) -> ListObject["PaymentIntent"]:
        """
        Returns a list of PaymentIntents.
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
        cls, **params: Unpack["PaymentIntent.ListParams"]
    ) -> ListObject["PaymentIntent"]:
        """
        Returns a list of PaymentIntents.
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
        cls, id: str, **params: Unpack["PaymentIntent.ModifyParams"]
    ) -> "PaymentIntent":
        """
        Updates properties on a PaymentIntent object without confirming.

        Depending on which properties you update, you might need to confirm the
        PaymentIntent again. For example, updating the payment_method
        always requires you to confirm the PaymentIntent again. If you prefer to
        update and confirm at the same time, we recommend updating properties through
        the [confirm API](https://stripe.com/docs/api/payment_intents/confirm) instead.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "PaymentIntent",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["PaymentIntent.ModifyParams"]
    ) -> "PaymentIntent":
        """
        Updates properties on a PaymentIntent object without confirming.

        Depending on which properties you update, you might need to confirm the
        PaymentIntent again. For example, updating the payment_method
        always requires you to confirm the PaymentIntent again. If you prefer to
        update and confirm at the same time, we recommend updating properties through
        the [confirm API](https://stripe.com/docs/api/payment_intents/confirm) instead.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "PaymentIntent",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["PaymentIntent.RetrieveParams"]
    ) -> "PaymentIntent":
        """
        Retrieves the details of a PaymentIntent that has previously been created.

        You can retrieve a PaymentIntent client-side using a publishable key when the client_secret is in the query string.

        If you retrieve a PaymentIntent with a publishable key, it only returns a subset of properties. Refer to the [payment intent](https://stripe.com/docs/api#payment_intent_object) object reference for more details.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["PaymentIntent.RetrieveParams"]
    ) -> "PaymentIntent":
        """
        Retrieves the details of a PaymentIntent that has previously been created.

        You can retrieve a PaymentIntent client-side using a publishable key when the client_secret is in the query string.

        If you retrieve a PaymentIntent with a publishable key, it only returns a subset of properties. Refer to the [payment intent](https://stripe.com/docs/api#payment_intent_object) object reference for more details.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def _cls_verify_microdeposits(
        cls,
        intent: str,
        **params: Unpack["PaymentIntent.VerifyMicrodepositsParams"],
    ) -> "PaymentIntent":
        """
        Verifies microdeposits on a PaymentIntent object.
        """
        return cast(
            "PaymentIntent",
            cls._static_request(
                "post",
                "/v1/payment_intents/{intent}/verify_microdeposits".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def verify_microdeposits(
        intent: str,
        **params: Unpack["PaymentIntent.VerifyMicrodepositsParams"],
    ) -> "PaymentIntent":
        """
        Verifies microdeposits on a PaymentIntent object.
        """
        ...

    @overload
    def verify_microdeposits(
        self, **params: Unpack["PaymentIntent.VerifyMicrodepositsParams"]
    ) -> "PaymentIntent":
        """
        Verifies microdeposits on a PaymentIntent object.
        """
        ...

    @class_method_variant("_cls_verify_microdeposits")
    def verify_microdeposits(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.VerifyMicrodepositsParams"]
    ) -> "PaymentIntent":
        """
        Verifies microdeposits on a PaymentIntent object.
        """
        return cast(
            "PaymentIntent",
            self._request(
                "post",
                "/v1/payment_intents/{intent}/verify_microdeposits".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_verify_microdeposits_async(
        cls,
        intent: str,
        **params: Unpack["PaymentIntent.VerifyMicrodepositsParams"],
    ) -> "PaymentIntent":
        """
        Verifies microdeposits on a PaymentIntent object.
        """
        return cast(
            "PaymentIntent",
            await cls._static_request_async(
                "post",
                "/v1/payment_intents/{intent}/verify_microdeposits".format(
                    intent=sanitize_id(intent)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def verify_microdeposits_async(
        intent: str,
        **params: Unpack["PaymentIntent.VerifyMicrodepositsParams"],
    ) -> "PaymentIntent":
        """
        Verifies microdeposits on a PaymentIntent object.
        """
        ...

    @overload
    async def verify_microdeposits_async(
        self, **params: Unpack["PaymentIntent.VerifyMicrodepositsParams"]
    ) -> "PaymentIntent":
        """
        Verifies microdeposits on a PaymentIntent object.
        """
        ...

    @class_method_variant("_cls_verify_microdeposits_async")
    async def verify_microdeposits_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["PaymentIntent.VerifyMicrodepositsParams"]
    ) -> "PaymentIntent":
        """
        Verifies microdeposits on a PaymentIntent object.
        """
        return cast(
            "PaymentIntent",
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/verify_microdeposits".format(
                    intent=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def search(
        cls, *args, **kwargs: Unpack["PaymentIntent.SearchParams"]
    ) -> SearchResultObject["PaymentIntent"]:
        """
        Search for PaymentIntents you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cls._search(
            search_url="/v1/payment_intents/search", *args, **kwargs
        )

    @classmethod
    async def search_async(
        cls, *args, **kwargs: Unpack["PaymentIntent.SearchParams"]
    ) -> SearchResultObject["PaymentIntent"]:
        """
        Search for PaymentIntents you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return await cls._search_async(
            search_url="/v1/payment_intents/search", *args, **kwargs
        )

    @classmethod
    def search_auto_paging_iter(
        cls, *args, **kwargs: Unpack["PaymentIntent.SearchParams"]
    ) -> Iterator["PaymentIntent"]:
        return cls.search(*args, **kwargs).auto_paging_iter()

    @classmethod
    async def search_auto_paging_iter_async(
        cls, *args, **kwargs: Unpack["PaymentIntent.SearchParams"]
    ) -> AsyncIterator["PaymentIntent"]:
        return (await cls.search_async(*args, **kwargs)).auto_paging_iter()

    _inner_class_types = {
        "amount_details": AmountDetails,
        "automatic_payment_methods": AutomaticPaymentMethods,
        "last_payment_error": LastPaymentError,
        "next_action": NextAction,
        "payment_method_configuration_details": PaymentMethodConfigurationDetails,
        "payment_method_options": PaymentMethodOptions,
        "processing": Processing,
        "shipping": Shipping,
        "transfer_data": TransferData,
    }
