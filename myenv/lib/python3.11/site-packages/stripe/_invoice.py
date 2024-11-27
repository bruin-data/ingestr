# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._deletable_api_resource import DeletableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._nested_resource_class_methods import nested_resource_class_methods
from stripe._request_options import RequestOptions
from stripe._search_result_object import SearchResultObject
from stripe._searchable_api_resource import SearchableAPIResource
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import (
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
    from stripe._discount import Discount
    from stripe._invoice_line_item import InvoiceLineItem
    from stripe._payment_intent import PaymentIntent
    from stripe._payment_method import PaymentMethod
    from stripe._quote import Quote
    from stripe._setup_intent import SetupIntent
    from stripe._shipping_rate import ShippingRate
    from stripe._source import Source
    from stripe._subscription import Subscription
    from stripe._tax_id import TaxId
    from stripe._tax_rate import TaxRate
    from stripe.test_helpers._test_clock import TestClock


@nested_resource_class_methods("line")
class Invoice(
    CreateableAPIResource["Invoice"],
    DeletableAPIResource["Invoice"],
    ListableAPIResource["Invoice"],
    SearchableAPIResource["Invoice"],
    UpdateableAPIResource["Invoice"],
):
    """
    Invoices are statements of amounts owed by a customer, and are either
    generated one-off, or generated periodically from a subscription.

    They contain [invoice items](https://stripe.com/docs/api#invoiceitems), and proration adjustments
    that may be caused by subscription upgrades/downgrades (if necessary).

    If your invoice is configured to be billed through automatic charges,
    Stripe automatically finalizes your invoice and attempts payment. Note
    that finalizing the invoice,
    [when automatic](https://stripe.com/docs/invoicing/integration/automatic-advancement-collection), does
    not happen immediately as the invoice is created. Stripe waits
    until one hour after the last webhook was successfully sent (or the last
    webhook timed out after failing). If you (and the platforms you may have
    connected to) have no webhooks configured, Stripe waits one hour after
    creation to finalize the invoice.

    If your invoice is configured to be billed by sending an email, then based on your
    [email settings](https://dashboard.stripe.com/account/billing/automatic),
    Stripe will email the invoice to your customer and await payment. These
    emails can contain a link to a hosted page to pay the invoice.

    Stripe applies any customer credit on the account before determining the
    amount due for the invoice (i.e., the amount that will be actually
    charged). If the amount due for the invoice is less than Stripe's [minimum allowed charge
    per currency](https://stripe.com/docs/currencies#minimum-and-maximum-charge-amounts), the
    invoice is automatically marked paid, and we add the amount due to the
    customer's credit balance which is applied to the next invoice.

    More details on the customer's credit balance are
    [here](https://stripe.com/docs/billing/customer/balance).

    Related guide: [Send invoices to customers](https://stripe.com/docs/billing/invoices/sending)
    """

    OBJECT_NAME: ClassVar[Literal["invoice"]] = "invoice"

    class AutomaticTax(StripeObject):
        class Liability(StripeObject):
            account: Optional[ExpandableField["Account"]]
            """
            The connected account being referenced when `type` is `account`.
            """
            type: Literal["account", "self"]
            """
            Type of the account referenced.
            """

        enabled: bool
        """
        Whether Stripe automatically computes tax on this invoice. Note that incompatible invoice items (invoice items with manually specified [tax rates](https://stripe.com/docs/api/tax_rates), negative amounts, or `tax_behavior=unspecified`) cannot be added to automatic tax invoices.
        """
        liability: Optional[Liability]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """
        status: Optional[
            Literal["complete", "failed", "requires_location_inputs"]
        ]
        """
        The status of the most recent automated tax calculation for this invoice.
        """
        _inner_class_types = {"liability": Liability}

    class CustomField(StripeObject):
        name: str
        """
        The name of the custom field.
        """
        value: str
        """
        The value of the custom field.
        """

    class CustomerAddress(StripeObject):
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

    class CustomerShipping(StripeObject):
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

    class CustomerTaxId(StripeObject):
        type: Literal[
            "ad_nrt",
            "ae_trn",
            "ar_cuit",
            "au_abn",
            "au_arn",
            "bg_uic",
            "bh_vat",
            "bo_tin",
            "br_cnpj",
            "br_cpf",
            "ca_bn",
            "ca_gst_hst",
            "ca_pst_bc",
            "ca_pst_mb",
            "ca_pst_sk",
            "ca_qst",
            "ch_uid",
            "ch_vat",
            "cl_tin",
            "cn_tin",
            "co_nit",
            "cr_tin",
            "de_stn",
            "do_rcn",
            "ec_ruc",
            "eg_tin",
            "es_cif",
            "eu_oss_vat",
            "eu_vat",
            "gb_vat",
            "ge_vat",
            "hk_br",
            "hu_tin",
            "id_npwp",
            "il_vat",
            "in_gst",
            "is_vat",
            "jp_cn",
            "jp_rn",
            "jp_trn",
            "ke_pin",
            "kr_brn",
            "kz_bin",
            "li_uid",
            "mx_rfc",
            "my_frp",
            "my_itn",
            "my_sst",
            "ng_tin",
            "no_vat",
            "no_voec",
            "nz_gst",
            "om_vat",
            "pe_ruc",
            "ph_tin",
            "ro_tin",
            "rs_pib",
            "ru_inn",
            "ru_kpp",
            "sa_vat",
            "sg_gst",
            "sg_uen",
            "si_tin",
            "sv_nit",
            "th_vat",
            "tr_tin",
            "tw_vat",
            "ua_vat",
            "unknown",
            "us_ein",
            "uy_ruc",
            "ve_rif",
            "vn_tin",
            "za_vat",
        ]
        """
        The type of the tax ID, one of `ad_nrt`, `ar_cuit`, `eu_vat`, `bo_tin`, `br_cnpj`, `br_cpf`, `cn_tin`, `co_nit`, `cr_tin`, `do_rcn`, `ec_ruc`, `eu_oss_vat`, `pe_ruc`, `ro_tin`, `rs_pib`, `sv_nit`, `uy_ruc`, `ve_rif`, `vn_tin`, `gb_vat`, `nz_gst`, `au_abn`, `au_arn`, `in_gst`, `no_vat`, `no_voec`, `za_vat`, `ch_vat`, `mx_rfc`, `sg_uen`, `ru_inn`, `ru_kpp`, `ca_bn`, `hk_br`, `es_cif`, `tw_vat`, `th_vat`, `jp_cn`, `jp_rn`, `jp_trn`, `li_uid`, `my_itn`, `us_ein`, `kr_brn`, `ca_qst`, `ca_gst_hst`, `ca_pst_bc`, `ca_pst_mb`, `ca_pst_sk`, `my_sst`, `sg_gst`, `ae_trn`, `cl_tin`, `sa_vat`, `id_npwp`, `my_frp`, `il_vat`, `ge_vat`, `ua_vat`, `is_vat`, `bg_uic`, `hu_tin`, `si_tin`, `ke_pin`, `tr_tin`, `eg_tin`, `ph_tin`, `bh_vat`, `kz_bin`, `ng_tin`, `om_vat`, `de_stn`, `ch_uid`, or `unknown`
        """
        value: Optional[str]
        """
        The value of the tax ID.
        """

    class FromInvoice(StripeObject):
        action: str
        """
        The relation between this invoice and the cloned invoice
        """
        invoice: ExpandableField["Invoice"]
        """
        The invoice that was cloned.
        """

    class Issuer(StripeObject):
        account: Optional[ExpandableField["Account"]]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced.
        """

    class LastFinalizationError(StripeObject):
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

    class PaymentSettings(StripeObject):
        class PaymentMethodOptions(StripeObject):
            class AcssDebit(StripeObject):
                class MandateOptions(StripeObject):
                    transaction_type: Optional[Literal["business", "personal"]]
                    """
                    Transaction type of the mandate.
                    """

                mandate_options: Optional[MandateOptions]
                verification_method: Optional[
                    Literal["automatic", "instant", "microdeposits"]
                ]
                """
                Bank account verification method.
                """
                _inner_class_types = {"mandate_options": MandateOptions}

            class Bancontact(StripeObject):
                preferred_language: Literal["de", "en", "fr", "nl"]
                """
                Preferred language of the Bancontact authorization page that the customer is redirected to.
                """

            class Card(StripeObject):
                class Installments(StripeObject):
                    enabled: Optional[bool]
                    """
                    Whether Installments are enabled for this Invoice.
                    """

                installments: Optional[Installments]
                request_three_d_secure: Optional[
                    Literal["any", "automatic", "challenge"]
                ]
                """
                We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
                """
                _inner_class_types = {"installments": Installments}

            class CustomerBalance(StripeObject):
                class BankTransfer(StripeObject):
                    class EuBankTransfer(StripeObject):
                        country: Literal["BE", "DE", "ES", "FR", "IE", "NL"]
                        """
                        The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
                        """

                    eu_bank_transfer: Optional[EuBankTransfer]
                    type: Optional[str]
                    """
                    The bank transfer type that can be used for funding. Permitted values include: `eu_bank_transfer`, `gb_bank_transfer`, `jp_bank_transfer`, `mx_bank_transfer`, or `us_bank_transfer`.
                    """
                    _inner_class_types = {"eu_bank_transfer": EuBankTransfer}

                bank_transfer: Optional[BankTransfer]
                funding_type: Optional[Literal["bank_transfer"]]
                """
                The funding method type to be used when there are not enough funds in the customer balance. Permitted values include: `bank_transfer`.
                """
                _inner_class_types = {"bank_transfer": BankTransfer}

            class Konbini(StripeObject):
                pass

            class SepaDebit(StripeObject):
                pass

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
                    _inner_class_types = {"filters": Filters}

                financial_connections: Optional[FinancialConnections]
                verification_method: Optional[
                    Literal["automatic", "instant", "microdeposits"]
                ]
                """
                Bank account verification method.
                """
                _inner_class_types = {
                    "financial_connections": FinancialConnections,
                }

            acss_debit: Optional[AcssDebit]
            """
            If paying by `acss_debit`, this sub-hash contains details about the Canadian pre-authorized debit payment method options to pass to the invoice's PaymentIntent.
            """
            bancontact: Optional[Bancontact]
            """
            If paying by `bancontact`, this sub-hash contains details about the Bancontact payment method options to pass to the invoice's PaymentIntent.
            """
            card: Optional[Card]
            """
            If paying by `card`, this sub-hash contains details about the Card payment method options to pass to the invoice's PaymentIntent.
            """
            customer_balance: Optional[CustomerBalance]
            """
            If paying by `customer_balance`, this sub-hash contains details about the Bank transfer payment method options to pass to the invoice's PaymentIntent.
            """
            konbini: Optional[Konbini]
            """
            If paying by `konbini`, this sub-hash contains details about the Konbini payment method options to pass to the invoice's PaymentIntent.
            """
            sepa_debit: Optional[SepaDebit]
            """
            If paying by `sepa_debit`, this sub-hash contains details about the SEPA Direct Debit payment method options to pass to the invoice's PaymentIntent.
            """
            us_bank_account: Optional[UsBankAccount]
            """
            If paying by `us_bank_account`, this sub-hash contains details about the ACH direct debit payment method options to pass to the invoice's PaymentIntent.
            """
            _inner_class_types = {
                "acss_debit": AcssDebit,
                "bancontact": Bancontact,
                "card": Card,
                "customer_balance": CustomerBalance,
                "konbini": Konbini,
                "sepa_debit": SepaDebit,
                "us_bank_account": UsBankAccount,
            }

        default_mandate: Optional[str]
        """
        ID of the mandate to be used for this invoice. It must correspond to the payment method used to pay the invoice, including the invoice's default_payment_method or default_source, if set.
        """
        payment_method_options: Optional[PaymentMethodOptions]
        """
        Payment-method-specific configuration to provide to the invoice's PaymentIntent.
        """
        payment_method_types: Optional[
            List[
                Literal[
                    "ach_credit_transfer",
                    "ach_debit",
                    "acss_debit",
                    "amazon_pay",
                    "au_becs_debit",
                    "bacs_debit",
                    "bancontact",
                    "boleto",
                    "card",
                    "cashapp",
                    "customer_balance",
                    "eps",
                    "fpx",
                    "giropay",
                    "grabpay",
                    "ideal",
                    "konbini",
                    "link",
                    "multibanco",
                    "p24",
                    "paynow",
                    "paypal",
                    "promptpay",
                    "revolut_pay",
                    "sepa_credit_transfer",
                    "sepa_debit",
                    "sofort",
                    "swish",
                    "us_bank_account",
                    "wechat_pay",
                ]
            ]
        ]
        """
        The list of payment method types (e.g. card) to provide to the invoice's PaymentIntent. If not set, Stripe attempts to automatically determine the types to use by looking at the invoice's default payment method, the subscription's default payment method, the customer's default payment method, and your [invoice template settings](https://dashboard.stripe.com/settings/billing/invoice).
        """
        _inner_class_types = {"payment_method_options": PaymentMethodOptions}

    class Rendering(StripeObject):
        class Pdf(StripeObject):
            page_size: Optional[Literal["a4", "auto", "letter"]]
            """
            Page size of invoice pdf. Options include a4, letter, and auto. If set to auto, page size will be switched to a4 or letter based on customer locale.
            """

        amount_tax_display: Optional[str]
        """
        How line-item prices and amounts will be displayed with respect to tax on invoice PDFs.
        """
        pdf: Optional[Pdf]
        """
        Invoice pdf rendering options
        """
        _inner_class_types = {"pdf": Pdf}

    class ShippingCost(StripeObject):
        class Tax(StripeObject):
            amount: int
            """
            Amount of tax applied for this rate.
            """
            rate: "TaxRate"
            """
            Tax rates can be applied to [invoices](https://stripe.com/docs/billing/invoices/tax-rates), [subscriptions](https://stripe.com/docs/billing/subscriptions/taxes) and [Checkout Sessions](https://stripe.com/docs/payments/checkout/set-up-a-subscription#tax-rates) to collect tax.

            Related guide: [Tax rates](https://stripe.com/docs/billing/taxes/tax-rates)
            """
            taxability_reason: Optional[
                Literal[
                    "customer_exempt",
                    "not_collecting",
                    "not_subject_to_tax",
                    "not_supported",
                    "portion_product_exempt",
                    "portion_reduced_rated",
                    "portion_standard_rated",
                    "product_exempt",
                    "product_exempt_holiday",
                    "proportionally_rated",
                    "reduced_rated",
                    "reverse_charge",
                    "standard_rated",
                    "taxable_basis_reduced",
                    "zero_rated",
                ]
            ]
            """
            The reasoning behind this tax, for example, if the product is tax exempt. The possible values for this field may be extended as new tax rules are supported.
            """
            taxable_amount: Optional[int]
            """
            The amount on which tax is calculated, in cents (or local equivalent).
            """

        amount_subtotal: int
        """
        Total shipping cost before any taxes are applied.
        """
        amount_tax: int
        """
        Total tax amount applied due to shipping costs. If no tax was applied, defaults to 0.
        """
        amount_total: int
        """
        Total shipping cost after taxes are applied.
        """
        shipping_rate: Optional[ExpandableField["ShippingRate"]]
        """
        The ID of the ShippingRate for this invoice.
        """
        taxes: Optional[List[Tax]]
        """
        The taxes applied to the shipping rate.
        """
        _inner_class_types = {"taxes": Tax}

    class ShippingDetails(StripeObject):
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

    class StatusTransitions(StripeObject):
        finalized_at: Optional[int]
        """
        The time that the invoice draft was finalized.
        """
        marked_uncollectible_at: Optional[int]
        """
        The time that the invoice was marked uncollectible.
        """
        paid_at: Optional[int]
        """
        The time that the invoice was paid.
        """
        voided_at: Optional[int]
        """
        The time that the invoice was voided.
        """

    class SubscriptionDetails(StripeObject):
        metadata: Optional[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) defined as subscription metadata when an invoice is created. Becomes an immutable snapshot of the subscription metadata at the time of invoice finalization.
         *Note: This attribute is populated only for invoices created on or after June 29, 2023.*
        """

    class ThresholdReason(StripeObject):
        class ItemReason(StripeObject):
            line_item_ids: List[str]
            """
            The IDs of the line items that triggered the threshold invoice.
            """
            usage_gte: int
            """
            The quantity threshold boundary that applied to the given line item.
            """

        amount_gte: Optional[int]
        """
        The total invoice amount threshold boundary if it triggered the threshold invoice.
        """
        item_reasons: List[ItemReason]
        """
        Indicates which line items triggered a threshold invoice.
        """
        _inner_class_types = {"item_reasons": ItemReason}

    class TotalDiscountAmount(StripeObject):
        amount: int
        """
        The amount, in cents (or local equivalent), of the discount.
        """
        discount: ExpandableField["Discount"]
        """
        The discount that was applied to get this discount amount.
        """

    class TotalTaxAmount(StripeObject):
        amount: int
        """
        The amount, in cents (or local equivalent), of the tax.
        """
        inclusive: bool
        """
        Whether this tax amount is inclusive or exclusive.
        """
        tax_rate: ExpandableField["TaxRate"]
        """
        The tax rate that was applied to get this tax amount.
        """
        taxability_reason: Optional[
            Literal[
                "customer_exempt",
                "not_collecting",
                "not_subject_to_tax",
                "not_supported",
                "portion_product_exempt",
                "portion_reduced_rated",
                "portion_standard_rated",
                "product_exempt",
                "product_exempt_holiday",
                "proportionally_rated",
                "reduced_rated",
                "reverse_charge",
                "standard_rated",
                "taxable_basis_reduced",
                "zero_rated",
            ]
        ]
        """
        The reasoning behind this tax, for example, if the product is tax exempt. The possible values for this field may be extended as new tax rules are supported.
        """
        taxable_amount: Optional[int]
        """
        The amount on which tax is calculated, in cents (or local equivalent).
        """

    class TransferData(StripeObject):
        amount: Optional[int]
        """
        The amount in cents (or local equivalent) that will be transferred to the destination account when the invoice is paid. By default, the entire amount is transferred to the destination.
        """
        destination: ExpandableField["Account"]
        """
        The account where funds from the payment will be transferred to upon payment success.
        """

    class AddLinesParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        lines: List["Invoice.AddLinesParamsLine"]
        """
        The line items to add.
        """

    class AddLinesParamsLine(TypedDict):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. If you want to apply a credit to the customer's account, pass a negative amount.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to the invoice item. The description is displayed in the invoice for easy tracking.
        """
        discountable: NotRequired[bool]
        """
        Controls whether discounts apply to this line item. Defaults to false for prorations or negative line items, and true for all other line items. Cannot be set to true for prorations.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.AddLinesParamsLineDiscount]"
        ]
        """
        The coupons, promotion codes & existing discounts which apply to the line item. Item discounts are applied before invoice discounts. Pass an empty string to remove previously-defined discounts.
        """
        invoice_item: NotRequired[str]
        """
        ID of an unassigned invoice item to assign to this invoice. If not provided, a new item will be created.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        period: NotRequired["Invoice.AddLinesParamsLinePeriod"]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired["Invoice.AddLinesParamsLinePriceData"]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the line item.
        """
        tax_amounts: NotRequired[
            "Literal['']|List[Invoice.AddLinesParamsLineTaxAmount]"
        ]
        """
        A list of up to 10 tax amounts for this line item. This can be useful if you calculate taxes on your own or use a third-party to calculate them. You cannot set tax amounts if any line item has [tax_rates](https://stripe.com/docs/api/invoices/line_item#invoice_line_item_object-tax_rates) or if the invoice has [default_tax_rates](https://stripe.com/docs/api/invoices/object#invoice_object-default_tax_rates) or uses [automatic tax](https://stripe.com/docs/tax/invoicing). Pass an empty string to remove previously defined tax amounts.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the line item. When set, the `default_tax_rates` on the invoice do not apply to this line item. Pass an empty string to remove previously-defined tax rates.
        """

    class AddLinesParamsLineDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class AddLinesParamsLinePeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class AddLinesParamsLinePriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: NotRequired[str]
        """
        The ID of the product that this price will belong to. One of `product` or `product_data` is required.
        """
        product_data: NotRequired[
            "Invoice.AddLinesParamsLinePriceDataProductData"
        ]
        """
        Data used to generate a new product object inline. One of `product` or `product_data` is required.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A non-negative integer in cents (or local equivalent) representing how much to charge. One of `unit_amount` or `unit_amount_decimal` is required.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class AddLinesParamsLinePriceDataProductData(TypedDict):
        description: NotRequired[str]
        """
        The product's description, meant to be displayable to the customer. Use this field to optionally store a long form explanation of the product being sold for your own rendering purposes.
        """
        images: NotRequired[List[str]]
        """
        A list of up to 8 URLs of images for this product, meant to be displayable to the customer.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: str
        """
        The product's name, meant to be displayable to the customer.
        """
        tax_code: NotRequired[str]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """

    class AddLinesParamsLineTaxAmount(TypedDict):
        amount: int
        """
        The amount, in cents (or local equivalent), of the tax.
        """
        tax_rate_data: "Invoice.AddLinesParamsLineTaxAmountTaxRateData"
        """
        Data to find or create a TaxRate object.

        Stripe automatically creates or reuses a TaxRate object for each tax amount. If the `tax_rate_data` exactly matches a previous value, Stripe will reuse the TaxRate object. TaxRate objects created automatically by Stripe are immediately archived, do not appear in the line item's `tax_rates`, and cannot be directly added to invoices, payments, or line items.
        """
        taxable_amount: int
        """
        The amount on which tax is calculated, in cents (or local equivalent).
        """

    class AddLinesParamsLineTaxAmountTaxRateData(TypedDict):
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the tax rate for your internal use only. It will not be visible to your customers.
        """
        display_name: str
        """
        The display name of the tax rate, which will be shown to users.
        """
        inclusive: bool
        """
        This specifies if the tax rate is inclusive or exclusive.
        """
        jurisdiction: NotRequired[str]
        """
        The jurisdiction for the tax rate. You can use this label field for tax reporting purposes. It also appears on your customer's invoice.
        """
        percentage: float
        """
        The statutory tax rate percent. This field accepts decimal values between 0 and 100 inclusive with at most 4 decimal places. To accommodate fixed-amount taxes, set the percentage to zero. Stripe will not display zero percentages on the invoice unless the `amount` of the tax is also zero.
        """
        state: NotRequired[str]
        """
        [ISO 3166-2 subdivision code](https://en.wikipedia.org/wiki/ISO_3166-2:US), without country prefix. For example, "NY" for New York, United States.
        """
        tax_type: NotRequired[
            Literal[
                "amusement_tax",
                "communications_tax",
                "gst",
                "hst",
                "igst",
                "jct",
                "lease_tax",
                "pst",
                "qst",
                "rst",
                "sales_tax",
                "vat",
            ]
        ]
        """
        The high-level tax type, such as `vat` or `sales_tax`.
        """

    class CreateParams(RequestOptions):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with the invoice. Only editable when the invoice is a draft.
        """
        application_fee_amount: NotRequired[int]
        """
        A fee in cents (or local equivalent) that will be applied to the invoice and transferred to the application owner's Stripe account. The request must be made with an OAuth key or the Stripe-Account header in order to take an application fee. For more information, see the application fees [documentation](https://stripe.com/docs/billing/invoices/connect#collecting-fees).
        """
        auto_advance: NotRequired[bool]
        """
        Controls whether Stripe performs [automatic collection](https://stripe.com/docs/invoicing/integration/automatic-advancement-collection) of the invoice. If `false`, the invoice's state doesn't automatically advance without an explicit action.
        """
        automatic_tax: NotRequired["Invoice.CreateParamsAutomaticTax"]
        """
        Settings for automatic tax lookup for this invoice.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay this invoice using the default source attached to the customer. When sending an invoice, Stripe will email this invoice to the customer with payment instructions. Defaults to `charge_automatically`.
        """
        currency: NotRequired[str]
        """
        The currency to create this invoice in. Defaults to that of `customer` if not specified.
        """
        custom_fields: NotRequired[
            "Literal['']|List[Invoice.CreateParamsCustomField]"
        ]
        """
        A list of up to 4 custom fields to be displayed on the invoice.
        """
        customer: NotRequired[str]
        """
        The ID of the customer who will be billed.
        """
        days_until_due: NotRequired[int]
        """
        The number of days from when the invoice is created until it is due. Valid only for invoices where `collection_method=send_invoice`.
        """
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the invoice. It must belong to the customer associated with the invoice. If not set, defaults to the subscription's default payment method, if any, or to the default payment method in the customer's invoice settings.
        """
        default_source: NotRequired[str]
        """
        ID of the default payment source for the invoice. It must belong to the customer associated with the invoice and be in a chargeable state. If not set, defaults to the subscription's default source, if any, or to the customer's default source.
        """
        default_tax_rates: NotRequired[List[str]]
        """
        The tax rates that will apply to any line item that does not have `tax_rates` set.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users. Referenced as 'memo' in the Dashboard.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.CreateParamsDiscount]"
        ]
        """
        The coupons and promotion codes to redeem into discounts for the invoice. If not specified, inherits the discount from the invoice's customer. Pass an empty string to avoid inheriting any discounts.
        """
        due_date: NotRequired[int]
        """
        The date on which payment for this invoice is due. Valid only for invoices where `collection_method=send_invoice`.
        """
        effective_at: NotRequired[int]
        """
        The date when this invoice is in effect. Same as `finalized_at` unless overwritten. When defined, this value replaces the system-generated 'Date of issue' printed on the invoice PDF and receipt.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        footer: NotRequired[str]
        """
        Footer to be displayed on the invoice.
        """
        from_invoice: NotRequired["Invoice.CreateParamsFromInvoice"]
        """
        Revise an existing invoice. The new invoice will be created in `status=draft`. See the [revision documentation](https://stripe.com/docs/invoicing/invoice-revisions) for more details.
        """
        issuer: NotRequired["Invoice.CreateParamsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        number: NotRequired[str]
        """
        Set the number for this invoice. If no number is present then a number will be assigned automatically when the invoice is finalized. In many markets, regulations require invoices to be unique, sequential and / or gapless. You are responsible for ensuring this is true across all your different invoicing systems in the event that you edit the invoice number using our API. If you use only Stripe for your invoices and do not change invoice numbers, Stripe handles this aspect of compliance for you automatically.
        """
        on_behalf_of: NotRequired[str]
        """
        The account (if any) for which the funds of the invoice payment are intended. If set, the invoice will be presented with the branding and support information of the specified account. See the [Invoices with Connect](https://stripe.com/docs/billing/invoices/connect) documentation for details.
        """
        payment_settings: NotRequired["Invoice.CreateParamsPaymentSettings"]
        """
        Configuration settings for the PaymentIntent that is generated when the invoice is finalized.
        """
        pending_invoice_items_behavior: NotRequired[
            Literal["exclude", "include"]
        ]
        """
        How to handle pending invoice items on invoice creation. Defaults to `exclude` if the parameter is omitted.
        """
        rendering: NotRequired["Invoice.CreateParamsRendering"]
        """
        The rendering-related settings that control how the invoice is displayed on customer-facing surfaces such as PDF and Hosted Invoice Page.
        """
        shipping_cost: NotRequired["Invoice.CreateParamsShippingCost"]
        """
        Settings for the cost of shipping for this invoice.
        """
        shipping_details: NotRequired["Invoice.CreateParamsShippingDetails"]
        """
        Shipping details for the invoice. The Invoice PDF will use the `shipping_details` value if it is set, otherwise the PDF will render the shipping address from the customer.
        """
        statement_descriptor: NotRequired[str]
        """
        Extra information about a charge for the customer's credit card statement. It must contain at least one letter. If not specified and this invoice is part of a subscription, the default `statement_descriptor` will be set to the first subscription item's product's `statement_descriptor`.
        """
        subscription: NotRequired[str]
        """
        The ID of the subscription to invoice, if any. If set, the created invoice will only include pending invoice items for that subscription. The subscription's billing cycle and regular subscription events won't be affected.
        """
        transfer_data: NotRequired["Invoice.CreateParamsTransferData"]
        """
        If specified, the funds from the invoice will be transferred to the destination and the ID of the resulting transfer will be found on the invoice's charge.
        """

    class CreateParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Whether Stripe automatically computes tax on this invoice. Note that incompatible invoice items (invoice items with manually specified [tax rates](https://stripe.com/docs/api/tax_rates), negative amounts, or `tax_behavior=unspecified`) cannot be added to automatic tax invoices.
        """
        liability: NotRequired["Invoice.CreateParamsAutomaticTaxLiability"]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class CreateParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsCustomField(TypedDict):
        name: str
        """
        The name of the custom field. This may be up to 40 characters.
        """
        value: str
        """
        The value of the custom field. This may be up to 140 characters.
        """

    class CreateParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreateParamsFromInvoice(TypedDict):
        action: Literal["revision"]
        """
        The relation between the new invoice and the original invoice. Currently, only 'revision' is permitted
        """
        invoice: str
        """
        The `id` of the invoice that will be cloned.
        """

    class CreateParamsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsPaymentSettings(TypedDict):
        default_mandate: NotRequired["Literal['']|str"]
        """
        ID of the mandate to be used for this invoice. It must correspond to the payment method used to pay the invoice, including the invoice's default_payment_method or default_source, if set.
        """
        payment_method_options: NotRequired[
            "Invoice.CreateParamsPaymentSettingsPaymentMethodOptions"
        ]
        """
        Payment-method-specific configuration to provide to the invoice's PaymentIntent.
        """
        payment_method_types: NotRequired[
            "Literal['']|List[Literal['ach_credit_transfer', 'ach_debit', 'acss_debit', 'amazon_pay', 'au_becs_debit', 'bacs_debit', 'bancontact', 'boleto', 'card', 'cashapp', 'customer_balance', 'eps', 'fpx', 'giropay', 'grabpay', 'ideal', 'konbini', 'link', 'multibanco', 'p24', 'paynow', 'paypal', 'promptpay', 'revolut_pay', 'sepa_credit_transfer', 'sepa_debit', 'sofort', 'swish', 'us_bank_account', 'wechat_pay']]"
        ]
        """
        The list of payment method types (e.g. card) to provide to the invoice's PaymentIntent. If not set, Stripe attempts to automatically determine the types to use by looking at the invoice's default payment method, the subscription's default payment method, the customer's default payment method, and your [invoice template settings](https://dashboard.stripe.com/settings/billing/invoice).
        """

    class CreateParamsPaymentSettingsPaymentMethodOptions(TypedDict):
        acss_debit: NotRequired[
            "Literal['']|Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsAcssDebit"
        ]
        """
        If paying by `acss_debit`, this sub-hash contains details about the Canadian pre-authorized debit payment method options to pass to the invoice's PaymentIntent.
        """
        bancontact: NotRequired[
            "Literal['']|Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsBancontact"
        ]
        """
        If paying by `bancontact`, this sub-hash contains details about the Bancontact payment method options to pass to the invoice's PaymentIntent.
        """
        card: NotRequired[
            "Literal['']|Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsCard"
        ]
        """
        If paying by `card`, this sub-hash contains details about the Card payment method options to pass to the invoice's PaymentIntent.
        """
        customer_balance: NotRequired[
            "Literal['']|Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalance"
        ]
        """
        If paying by `customer_balance`, this sub-hash contains details about the Bank transfer payment method options to pass to the invoice's PaymentIntent.
        """
        konbini: NotRequired[
            "Literal['']|Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsKonbini"
        ]
        """
        If paying by `konbini`, this sub-hash contains details about the Konbini payment method options to pass to the invoice's PaymentIntent.
        """
        sepa_debit: NotRequired[
            "Literal['']|Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsSepaDebit"
        ]
        """
        If paying by `sepa_debit`, this sub-hash contains details about the SEPA Direct Debit payment method options to pass to the invoice's PaymentIntent.
        """
        us_bank_account: NotRequired[
            "Literal['']|Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If paying by `us_bank_account`, this sub-hash contains details about the ACH direct debit payment method options to pass to the invoice's PaymentIntent.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsAcssDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Verification method for the intent
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsAcssDebitMandateOptions(
        TypedDict,
    ):
        transaction_type: NotRequired[Literal["business", "personal"]]
        """
        Transaction type of the mandate.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsBancontact(TypedDict):
        preferred_language: NotRequired[Literal["de", "en", "fr", "nl"]]
        """
        Preferred language of the Bancontact authorization page that the customer is redirected to.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCard(TypedDict):
        installments: NotRequired[
            "Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsCardInstallments"
        ]
        """
        Installment configuration for payments attempted on this invoice (Mexico Only).

        For more information, see the [installments integration guide](https://stripe.com/docs/payments/installments).
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCardInstallments(
        TypedDict,
    ):
        enabled: NotRequired[bool]
        """
        Setting to true enables installments for this invoice.
        Setting to false will prevent any selected plan from applying to a payment.
        """
        plan: NotRequired[
            "Literal['']|Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsCardInstallmentsPlan"
        ]
        """
        The selected installment plan to use for this invoice.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCardInstallmentsPlan(
        TypedDict,
    ):
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

    class CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalance(
        TypedDict,
    ):
        bank_transfer: NotRequired[
            "Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransfer"
        ]
        """
        Configuration for the bank transfer funding type, if the `funding_type` is set to `bank_transfer`.
        """
        funding_type: NotRequired[str]
        """
        The funding method type to be used when there are not enough funds in the customer balance. Permitted values include: `bank_transfer`.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransfer(
        TypedDict,
    ):
        eu_bank_transfer: NotRequired[
            "Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
        ]
        """
        Configuration for eu_bank_transfer funding type.
        """
        type: NotRequired[str]
        """
        The bank transfer type that can be used for funding. Permitted values include: `eu_bank_transfer`, `gb_bank_transfer`, `jp_bank_transfer`, `mx_bank_transfer`, or `us_bank_transfer`.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer(
        TypedDict,
    ):
        country: str
        """
        The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsKonbini(TypedDict):
        pass

    class CreateParamsPaymentSettingsPaymentMethodOptionsSepaDebit(TypedDict):
        pass

    class CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccount(
        TypedDict,
    ):
        financial_connections: NotRequired[
            "Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Verification method for the intent
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnections(
        TypedDict,
    ):
        filters: NotRequired[
            "Invoice.CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
        ]
        """
        Provide filters for the linked accounts that the customer can select for the payment method.
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

    class CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters(
        TypedDict,
    ):
        account_subcategories: NotRequired[
            List[Literal["checking", "savings"]]
        ]
        """
        The account subcategories to use to filter for selectable accounts. Valid subcategories are `checking` and `savings`.
        """

    class CreateParamsRendering(TypedDict):
        amount_tax_display: NotRequired[
            "Literal['']|Literal['exclude_tax', 'include_inclusive_tax']"
        ]
        """
        How line-item prices and amounts will be displayed with respect to tax on invoice PDFs. One of `exclude_tax` or `include_inclusive_tax`. `include_inclusive_tax` will include inclusive tax (and exclude exclusive tax) in invoice PDF amounts. `exclude_tax` will exclude all tax (inclusive and exclusive alike) from invoice PDF amounts.
        """
        pdf: NotRequired["Invoice.CreateParamsRenderingPdf"]
        """
        Invoice pdf rendering options
        """

    class CreateParamsRenderingPdf(TypedDict):
        page_size: NotRequired[Literal["a4", "auto", "letter"]]
        """
        Page size for invoice PDF. Can be set to `a4`, `letter`, or `auto`.
         If set to `auto`, invoice PDF page size defaults to `a4` for customers with
         Japanese locale and `letter` for customers with other locales.
        """

    class CreateParamsShippingCost(TypedDict):
        shipping_rate: NotRequired[str]
        """
        The ID of the shipping rate to use for this order.
        """
        shipping_rate_data: NotRequired[
            "Invoice.CreateParamsShippingCostShippingRateData"
        ]
        """
        Parameters to create a new ad-hoc shipping rate for this order.
        """

    class CreateParamsShippingCostShippingRateData(TypedDict):
        delivery_estimate: NotRequired[
            "Invoice.CreateParamsShippingCostShippingRateDataDeliveryEstimate"
        ]
        """
        The estimated range for how long shipping will take, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        display_name: str
        """
        The name of the shipping rate, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        fixed_amount: NotRequired[
            "Invoice.CreateParamsShippingCostShippingRateDataFixedAmount"
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

    class CreateParamsShippingCostShippingRateDataDeliveryEstimate(TypedDict):
        maximum: NotRequired[
            "Invoice.CreateParamsShippingCostShippingRateDataDeliveryEstimateMaximum"
        ]
        """
        The upper bound of the estimated range. If empty, represents no upper bound i.e., infinite.
        """
        minimum: NotRequired[
            "Invoice.CreateParamsShippingCostShippingRateDataDeliveryEstimateMinimum"
        ]
        """
        The lower bound of the estimated range. If empty, represents no lower bound.
        """

    class CreateParamsShippingCostShippingRateDataDeliveryEstimateMaximum(
        TypedDict,
    ):
        unit: Literal["business_day", "day", "hour", "month", "week"]
        """
        A unit of time.
        """
        value: int
        """
        Must be greater than 0.
        """

    class CreateParamsShippingCostShippingRateDataDeliveryEstimateMinimum(
        TypedDict,
    ):
        unit: Literal["business_day", "day", "hour", "month", "week"]
        """
        A unit of time.
        """
        value: int
        """
        Must be greater than 0.
        """

    class CreateParamsShippingCostShippingRateDataFixedAmount(TypedDict):
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
                "Invoice.CreateParamsShippingCostShippingRateDataFixedAmountCurrencyOptions",
            ]
        ]
        """
        Shipping rates defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """

    class CreateParamsShippingCostShippingRateDataFixedAmountCurrencyOptions(
        TypedDict,
    ):
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

    class CreateParamsShippingDetails(TypedDict):
        address: "Invoice.CreateParamsShippingDetailsAddress"
        """
        Shipping address
        """
        name: str
        """
        Recipient name.
        """
        phone: NotRequired["Literal['']|str"]
        """
        Recipient phone (including extension)
        """

    class CreateParamsShippingDetailsAddress(TypedDict):
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
        The amount that will be transferred automatically when the invoice is paid. If no amount is set, the full amount is transferred.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class CreatePreviewParams(RequestOptions):
        automatic_tax: NotRequired["Invoice.CreatePreviewParamsAutomaticTax"]
        """
        Settings for automatic tax lookup for this invoice preview.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this phase of the subscription schedule. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        currency: NotRequired[str]
        """
        The currency to preview this invoice in. Defaults to that of `customer` if not specified.
        """
        customer: NotRequired[str]
        """
        The identifier of the customer whose upcoming invoice you'd like to retrieve. If `automatic_tax` is enabled then one of `customer`, `customer_details`, `subscription`, or `schedule` must be set.
        """
        customer_details: NotRequired[
            "Invoice.CreatePreviewParamsCustomerDetails"
        ]
        """
        Details about the customer you want to invoice or overrides for an existing customer. If `automatic_tax` is enabled then one of `customer`, `customer_details`, `subscription`, or `schedule` must be set.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.CreatePreviewParamsDiscount]"
        ]
        """
        The coupons to redeem into discounts for the invoice preview. If not specified, inherits the discount from the subscription or customer. This works for both coupons directly applied to an invoice and coupons applied to a subscription. Pass an empty string to avoid inheriting any discounts.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_items: NotRequired[
            List["Invoice.CreatePreviewParamsInvoiceItem"]
        ]
        """
        List of invoice items to add or update in the upcoming invoice preview.
        """
        issuer: NotRequired["Invoice.CreatePreviewParamsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account (if any) for which the funds of the invoice payment are intended. If set, the invoice will be presented with the branding and support information of the specified account. See the [Invoices with Connect](https://stripe.com/docs/billing/invoices/connect) documentation for details.
        """
        preview_mode: NotRequired[Literal["next", "recurring"]]
        """
        Customizes the types of values to include when calculating the invoice. Defaults to `next` if unspecified.
        """
        schedule: NotRequired[str]
        """
        The identifier of the schedule whose upcoming invoice you'd like to retrieve. Cannot be used with subscription or subscription fields.
        """
        schedule_details: NotRequired[
            "Invoice.CreatePreviewParamsScheduleDetails"
        ]
        """
        The schedule creation or modification params to apply as a preview. Cannot be used with `subscription` or `subscription_` prefixed fields.
        """
        subscription: NotRequired[str]
        """
        The identifier of the subscription for which you'd like to retrieve the upcoming invoice. If not provided, but a `subscription_details.items` is provided, you will preview creating a subscription with those items. If neither `subscription` nor `subscription_details.items` is provided, you will retrieve the next upcoming invoice from among the customer's subscriptions.
        """
        subscription_details: NotRequired[
            "Invoice.CreatePreviewParamsSubscriptionDetails"
        ]
        """
        The subscription creation or modification params to apply as a preview. Cannot be used with `schedule` or `schedule_details` fields.
        """

    class CreatePreviewParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Whether Stripe automatically computes tax on this invoice. Note that incompatible invoice items (invoice items with manually specified [tax rates](https://stripe.com/docs/api/tax_rates), negative amounts, or `tax_behavior=unspecified`) cannot be added to automatic tax invoices.
        """
        liability: NotRequired[
            "Invoice.CreatePreviewParamsAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class CreatePreviewParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreatePreviewParamsCustomerDetails(TypedDict):
        address: NotRequired[
            "Literal['']|Invoice.CreatePreviewParamsCustomerDetailsAddress"
        ]
        """
        The customer's address.
        """
        shipping: NotRequired[
            "Literal['']|Invoice.CreatePreviewParamsCustomerDetailsShipping"
        ]
        """
        The customer's shipping information. Appears on invoices emailed to this customer.
        """
        tax: NotRequired["Invoice.CreatePreviewParamsCustomerDetailsTax"]
        """
        Tax details about the customer.
        """
        tax_exempt: NotRequired[
            "Literal['']|Literal['exempt', 'none', 'reverse']"
        ]
        """
        The customer's tax exemption. One of `none`, `exempt`, or `reverse`.
        """
        tax_ids: NotRequired[
            List["Invoice.CreatePreviewParamsCustomerDetailsTaxId"]
        ]
        """
        The customer's tax IDs.
        """

    class CreatePreviewParamsCustomerDetailsAddress(TypedDict):
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

    class CreatePreviewParamsCustomerDetailsShipping(TypedDict):
        address: "Invoice.CreatePreviewParamsCustomerDetailsShippingAddress"
        """
        Customer shipping address.
        """
        name: str
        """
        Customer name.
        """
        phone: NotRequired[str]
        """
        Customer phone (including extension).
        """

    class CreatePreviewParamsCustomerDetailsShippingAddress(TypedDict):
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

    class CreatePreviewParamsCustomerDetailsTax(TypedDict):
        ip_address: NotRequired["Literal['']|str"]
        """
        A recent IP address of the customer used for tax reporting and tax location inference. Stripe recommends updating the IP address when a new PaymentMethod is attached or the address field on the customer is updated. We recommend against updating this field more frequently since it could result in unexpected tax location/reporting outcomes.
        """

    class CreatePreviewParamsCustomerDetailsTaxId(TypedDict):
        type: Literal[
            "ad_nrt",
            "ae_trn",
            "ar_cuit",
            "au_abn",
            "au_arn",
            "bg_uic",
            "bh_vat",
            "bo_tin",
            "br_cnpj",
            "br_cpf",
            "ca_bn",
            "ca_gst_hst",
            "ca_pst_bc",
            "ca_pst_mb",
            "ca_pst_sk",
            "ca_qst",
            "ch_uid",
            "ch_vat",
            "cl_tin",
            "cn_tin",
            "co_nit",
            "cr_tin",
            "de_stn",
            "do_rcn",
            "ec_ruc",
            "eg_tin",
            "es_cif",
            "eu_oss_vat",
            "eu_vat",
            "gb_vat",
            "ge_vat",
            "hk_br",
            "hu_tin",
            "id_npwp",
            "il_vat",
            "in_gst",
            "is_vat",
            "jp_cn",
            "jp_rn",
            "jp_trn",
            "ke_pin",
            "kr_brn",
            "kz_bin",
            "li_uid",
            "mx_rfc",
            "my_frp",
            "my_itn",
            "my_sst",
            "ng_tin",
            "no_vat",
            "no_voec",
            "nz_gst",
            "om_vat",
            "pe_ruc",
            "ph_tin",
            "ro_tin",
            "rs_pib",
            "ru_inn",
            "ru_kpp",
            "sa_vat",
            "sg_gst",
            "sg_uen",
            "si_tin",
            "sv_nit",
            "th_vat",
            "tr_tin",
            "tw_vat",
            "ua_vat",
            "us_ein",
            "uy_ruc",
            "ve_rif",
            "vn_tin",
            "za_vat",
        ]
        """
        Type of the tax ID, one of `ad_nrt`, `ae_trn`, `ar_cuit`, `au_abn`, `au_arn`, `bg_uic`, `bh_vat`, `bo_tin`, `br_cnpj`, `br_cpf`, `ca_bn`, `ca_gst_hst`, `ca_pst_bc`, `ca_pst_mb`, `ca_pst_sk`, `ca_qst`, `ch_uid`, `ch_vat`, `cl_tin`, `cn_tin`, `co_nit`, `cr_tin`, `de_stn`, `do_rcn`, `ec_ruc`, `eg_tin`, `es_cif`, `eu_oss_vat`, `eu_vat`, `gb_vat`, `ge_vat`, `hk_br`, `hu_tin`, `id_npwp`, `il_vat`, `in_gst`, `is_vat`, `jp_cn`, `jp_rn`, `jp_trn`, `ke_pin`, `kr_brn`, `kz_bin`, `li_uid`, `mx_rfc`, `my_frp`, `my_itn`, `my_sst`, `ng_tin`, `no_vat`, `no_voec`, `nz_gst`, `om_vat`, `pe_ruc`, `ph_tin`, `ro_tin`, `rs_pib`, `ru_inn`, `ru_kpp`, `sa_vat`, `sg_gst`, `sg_uen`, `si_tin`, `sv_nit`, `th_vat`, `tr_tin`, `tw_vat`, `ua_vat`, `us_ein`, `uy_ruc`, `ve_rif`, `vn_tin`, or `za_vat`
        """
        value: str
        """
        Value of the tax ID.
        """

    class CreatePreviewParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsInvoiceItem(TypedDict):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) of previewed invoice item.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies). Only applicable to new invoice items.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to the invoice item. The description is displayed in the invoice for easy tracking.
        """
        discountable: NotRequired[bool]
        """
        Explicitly controls whether discounts apply to this invoice item. Defaults to true, except for negative invoice items.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.CreatePreviewParamsInvoiceItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the invoice item in the preview.
        """
        invoiceitem: NotRequired[str]
        """
        The ID of the invoice item to update in preview. If not specified, a new invoice item will be added to the preview of the upcoming invoice.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        period: NotRequired["Invoice.CreatePreviewParamsInvoiceItemPeriod"]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "Invoice.CreatePreviewParamsInvoiceItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the invoice item.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tax_code: NotRequired["Literal['']|str"]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates that apply to the item. When set, any `default_tax_rates` do not apply to this item.
        """
        unit_amount: NotRequired[int]
        """
        The integer unit amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. This unit_amount will be multiplied by the quantity to get the full amount. If you want to apply a credit to the customer's account, pass a negative unit_amount.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreatePreviewParamsInvoiceItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsInvoiceItemPeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class CreatePreviewParamsInvoiceItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreatePreviewParamsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreatePreviewParamsScheduleDetails(TypedDict):
        end_behavior: NotRequired[Literal["cancel", "release"]]
        """
        Behavior of the subscription schedule and underlying subscription when it ends. Possible values are `release` or `cancel` with the default being `release`. `release` will end the subscription schedule and keep the underlying subscription running. `cancel` will end the subscription schedule and cancel the underlying subscription.
        """
        phases: NotRequired[
            List["Invoice.CreatePreviewParamsScheduleDetailsPhase"]
        ]
        """
        List representing phases of the subscription schedule. Each phase can be customized to have different durations, plans, and coupons. If there are multiple phases, the `end_date` of one phase will always equal the `start_date` of the next phase.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        In cases where the `schedule_details` params update the currently active phase, specifies if and how to prorate at the time of the request.
        """

    class CreatePreviewParamsScheduleDetailsPhase(TypedDict):
        add_invoice_items: NotRequired[
            List[
                "Invoice.CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItem"
            ]
        ]
        """
        A list of prices and quantities that will generate invoice items appended to the next invoice for this phase. You may pass up to 20 items.
        """
        application_fee_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. The request must be made by a platform account on a connected account in order to set an application fee percentage. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        automatic_tax: NotRequired[
            "Invoice.CreatePreviewParamsScheduleDetailsPhaseAutomaticTax"
        ]
        """
        Automatic tax settings for this phase.
        """
        billing_cycle_anchor: NotRequired[Literal["automatic", "phase_start"]]
        """
        Can be set to `phase_start` to set the anchor to the start of the phase or `automatic` to automatically change it if needed. Cannot be set to `phase_start` if this phase specifies a trial. For more information, see the billing cycle [documentation](https://stripe.com/docs/billing/subscriptions/billing-cycle).
        """
        billing_thresholds: NotRequired[
            "Literal['']|Invoice.CreatePreviewParamsScheduleDetailsPhaseBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. Pass an empty string to remove previously-defined thresholds.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay the underlying subscription at the end of each billing cycle using the default source attached to the customer. When sending an invoice, Stripe will email your customer an invoice with payment instructions and mark the subscription as `active`. Defaults to `charge_automatically` on creation.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this phase of the subscription schedule. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the subscription schedule. It must belong to the customer associated with the subscription schedule. If not set, invoices will use the default payment method in the customer's invoice settings.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will set the Subscription's [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates), which means they will be the Invoice's [`default_tax_rates`](https://stripe.com/docs/api/invoices/create#create_invoice-default_tax_rates) for any Invoices issued by the Subscription during this Phase.
        """
        description: NotRequired["Literal['']|str"]
        """
        Subscription description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.CreatePreviewParamsScheduleDetailsPhaseDiscount]"
        ]
        """
        The coupons to redeem into discounts for the schedule phase. If not specified, inherits the discount from the subscription's customer. Pass an empty string to avoid inheriting any discounts.
        """
        end_date: NotRequired["int|Literal['now']"]
        """
        The date at which this phase of the subscription schedule ends. If set, `iterations` must not be set.
        """
        invoice_settings: NotRequired[
            "Invoice.CreatePreviewParamsScheduleDetailsPhaseInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        items: List["Invoice.CreatePreviewParamsScheduleDetailsPhaseItem"]
        """
        List of configuration items, each with an attached price, to apply during this phase of the subscription schedule.
        """
        iterations: NotRequired[int]
        """
        Integer representing the multiplier applied to the price interval. For example, `iterations=2` applied to a price with `interval=month` and `interval_count=3` results in a phase of duration `2 * 3 months = 6 months`. If set, `end_date` must not be set.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to a phase. Metadata on a schedule's phase will update the underlying subscription's `metadata` when the phase is entered, adding new keys and replacing existing keys in the subscription's `metadata`. Individual keys in the subscription's `metadata` can be unset by posting an empty value to them in the phase's `metadata`. To unset all keys in the subscription's `metadata`, update the subscription directly or unset every key individually from the phase's `metadata`.
        """
        on_behalf_of: NotRequired[str]
        """
        The account on behalf of which to charge, for each of the associated subscription's invoices.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Whether the subscription schedule will create [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when transitioning to this phase. The default value is `create_prorations`. This setting controls prorations when a phase is started asynchronously and it is persisted as a field on the phase. It's different from the request-level [proration_behavior](https://stripe.com/docs/api/subscription_schedules/update#update_subscription_schedule-proration_behavior) parameter which controls what happens if the update request affects the billing configuration of the current phase.
        """
        start_date: NotRequired["int|Literal['now']"]
        """
        The date at which this phase of the subscription schedule starts or `now`. Must be set on the first phase.
        """
        transfer_data: NotRequired[
            "Invoice.CreatePreviewParamsScheduleDetailsPhaseTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the associated subscription's invoices.
        """
        trial: NotRequired[bool]
        """
        If set to true the entire phase is counted as a trial and the customer will not be charged for any fees.
        """
        trial_end: NotRequired["int|Literal['now']"]
        """
        Sets the phase to trialing from the start date to this date. Must be before the phase end date, can not be combined with `trial`
        """

    class CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItem(TypedDict):
        discounts: NotRequired[
            List[
                "Invoice.CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItemDiscount"
            ]
        ]
        """
        The coupons to redeem into discounts for the item.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "Invoice.CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item. Defaults to 1.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the item. When set, the `default_tax_rates` do not apply to this item.
        """

    class CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItemDiscount(
        TypedDict,
    ):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsScheduleDetailsPhaseAddInvoiceItemPriceData(
        TypedDict,
    ):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreatePreviewParamsScheduleDetailsPhaseAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
        """
        liability: NotRequired[
            "Invoice.CreatePreviewParamsScheduleDetailsPhaseAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class CreatePreviewParamsScheduleDetailsPhaseAutomaticTaxLiability(
        TypedDict,
    ):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreatePreviewParamsScheduleDetailsPhaseBillingThresholds(TypedDict):
        amount_gte: NotRequired[int]
        """
        Monetary threshold that triggers the subscription to advance to a new billing period
        """
        reset_billing_cycle_anchor: NotRequired[bool]
        """
        Indicates if the `billing_cycle_anchor` should be reset when a threshold is reached. If true, `billing_cycle_anchor` will be updated to the date/time the threshold was last reached; otherwise, the value will remain unchanged.
        """

    class CreatePreviewParamsScheduleDetailsPhaseDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsScheduleDetailsPhaseInvoiceSettings(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with this phase of the subscription schedule. Will be set on invoices generated by this phase of the subscription schedule.
        """
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay invoices generated by this subscription schedule. This value will be `null` for subscription schedules where `billing=charge_automatically`.
        """
        issuer: NotRequired[
            "Invoice.CreatePreviewParamsScheduleDetailsPhaseInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class CreatePreviewParamsScheduleDetailsPhaseInvoiceSettingsIssuer(
        TypedDict,
    ):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreatePreviewParamsScheduleDetailsPhaseItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|Invoice.CreatePreviewParamsScheduleDetailsPhaseItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.CreatePreviewParamsScheduleDetailsPhaseItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to a configuration item. Metadata on a configuration item will update the underlying subscription item's `metadata` when the phase is entered, adding new keys and replacing existing keys. Individual keys in the subscription item's `metadata` can be unset by posting an empty value to them in the configuration item's `metadata`. To unset all keys in the subscription item's `metadata`, update the subscription item directly or unset every key individually from the configuration item's `metadata`.
        """
        plan: NotRequired[str]
        """
        The plan ID to subscribe to. You may specify the same ID in `plan` and `price`.
        """
        price: NotRequired[str]
        """
        The ID of the price object.
        """
        price_data: NotRequired[
            "Invoice.CreatePreviewParamsScheduleDetailsPhaseItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline.
        """
        quantity: NotRequired[int]
        """
        Quantity for the given price. Can be set only if the price's `usage_type` is `licensed` and not `metered`.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class CreatePreviewParamsScheduleDetailsPhaseItemBillingThresholds(
        TypedDict,
    ):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class CreatePreviewParamsScheduleDetailsPhaseItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsScheduleDetailsPhaseItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "Invoice.CreatePreviewParamsScheduleDetailsPhaseItemPriceDataRecurring"
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreatePreviewParamsScheduleDetailsPhaseItemPriceDataRecurring(
        TypedDict,
    ):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class CreatePreviewParamsScheduleDetailsPhaseTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class CreatePreviewParamsSubscriptionDetails(TypedDict):
        billing_cycle_anchor: NotRequired["Literal['now', 'unchanged']|int"]
        """
        For new subscriptions, a future timestamp to anchor the subscription's [billing cycle](https://stripe.com/docs/subscriptions/billing-cycle). This is used to determine the date of the first full invoice, and, for plans with `month` or `year` intervals, the day of the month for subsequent invoices. For existing subscriptions, the value can only be set to `now` or `unchanged`.
        """
        cancel_at: NotRequired["Literal['']|int"]
        """
        A timestamp at which the subscription should cancel. If set to a date before the current period ends, this will cause a proration if prorations have been enabled using `proration_behavior`. If set during a future period, this will always cause a proration for that period.
        """
        cancel_at_period_end: NotRequired[bool]
        """
        Indicate whether this subscription should cancel at the end of the current period (`current_period_end`). Defaults to `false`.
        """
        cancel_now: NotRequired[bool]
        """
        This simulates the subscription being canceled or expired immediately.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with these default tax rates. The default tax rates will apply to any line item that does not have `tax_rates` set.
        """
        items: NotRequired[
            List["Invoice.CreatePreviewParamsSubscriptionDetailsItem"]
        ]
        """
        A list of up to 20 subscription items, each with an attached price.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`.
        """
        proration_date: NotRequired[int]
        """
        If previewing an update to a subscription, and doing proration, `subscription_details.proration_date` forces the proration to be calculated as though the update was done at the specified time. The time given must be within the current subscription period and within the current phase of the schedule backing this subscription, if the schedule exists. If set, `subscription`, and one of `subscription_details.items`, or `subscription_details.trial_end` are required. Also, `subscription_details.proration_behavior` cannot be set to 'none'.
        """
        resume_at: NotRequired[Literal["now"]]
        """
        For paused subscriptions, setting `subscription_details.resume_at` to `now` will preview the invoice that will be generated if the subscription is resumed.
        """
        start_date: NotRequired[int]
        """
        Date a subscription is intended to start (can be future or past).
        """
        trial_end: NotRequired["Literal['now']|int"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with that trial end. If set, one of `subscription_details.items` or `subscription` is required.
        """

    class CreatePreviewParamsSubscriptionDetailsItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|Invoice.CreatePreviewParamsSubscriptionDetailsItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        clear_usage: NotRequired[bool]
        """
        Delete all usage for a given subscription item. Allowed only when `deleted` is set to `true` and the current plan's `usage_type` is `metered`.
        """
        deleted: NotRequired[bool]
        """
        A flag that, if set to `true`, will delete the specified item.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.CreatePreviewParamsSubscriptionDetailsItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        id: NotRequired[str]
        """
        Subscription item to update.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        plan: NotRequired[str]
        """
        Plan ID for this item, as a string.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required. When changing a subscription item's price, `quantity` is set to 1 unless a `quantity` parameter is provided.
        """
        price_data: NotRequired[
            "Invoice.CreatePreviewParamsSubscriptionDetailsItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class CreatePreviewParamsSubscriptionDetailsItemBillingThresholds(
        TypedDict,
    ):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class CreatePreviewParamsSubscriptionDetailsItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class CreatePreviewParamsSubscriptionDetailsItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "Invoice.CreatePreviewParamsSubscriptionDetailsItemPriceDataRecurring"
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreatePreviewParamsSubscriptionDetailsItemPriceDataRecurring(
        TypedDict,
    ):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class DeleteParams(RequestOptions):
        pass

    class FinalizeInvoiceParams(RequestOptions):
        auto_advance: NotRequired[bool]
        """
        Controls whether Stripe performs [automatic collection](https://stripe.com/docs/invoicing/integration/automatic-advancement-collection) of the invoice. If `false`, the invoice's state doesn't automatically advance without an explicit action.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListLinesParams(RequestOptions):
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

    class ListParams(RequestOptions):
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        The collection method of the invoice to retrieve. Either `charge_automatically` or `send_invoice`.
        """
        created: NotRequired["Invoice.ListParamsCreated|int"]
        """
        Only return invoices that were created during the given date interval.
        """
        customer: NotRequired[str]
        """
        Only return invoices for the customer specified by this customer ID.
        """
        due_date: NotRequired["Invoice.ListParamsDueDate|int"]
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
        status: NotRequired[
            Literal["draft", "open", "paid", "uncollectible", "void"]
        ]
        """
        The status of the invoice, one of `draft`, `open`, `paid`, `uncollectible`, or `void`. [Learn more](https://stripe.com/docs/billing/invoices/workflow#workflow-overview)
        """
        subscription: NotRequired[str]
        """
        Only return invoices for the subscription specified by this subscription ID.
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

    class ListParamsDueDate(TypedDict):
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

    class MarkUncollectibleParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ModifyParams(RequestOptions):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with the invoice. Only editable when the invoice is a draft.
        """
        application_fee_amount: NotRequired[int]
        """
        A fee in cents (or local equivalent) that will be applied to the invoice and transferred to the application owner's Stripe account. The request must be made with an OAuth key or the Stripe-Account header in order to take an application fee. For more information, see the application fees [documentation](https://stripe.com/docs/billing/invoices/connect#collecting-fees).
        """
        auto_advance: NotRequired[bool]
        """
        Controls whether Stripe performs [automatic collection](https://stripe.com/docs/invoicing/integration/automatic-advancement-collection) of the invoice.
        """
        automatic_tax: NotRequired["Invoice.ModifyParamsAutomaticTax"]
        """
        Settings for automatic tax lookup for this invoice.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically` or `send_invoice`. This field can be updated only on `draft` invoices.
        """
        custom_fields: NotRequired[
            "Literal['']|List[Invoice.ModifyParamsCustomField]"
        ]
        """
        A list of up to 4 custom fields to be displayed on the invoice. If a value for `custom_fields` is specified, the list specified will replace the existing custom field list on this invoice. Pass an empty string to remove previously-defined fields.
        """
        days_until_due: NotRequired[int]
        """
        The number of days from which the invoice is created until it is due. Only valid for invoices where `collection_method=send_invoice`. This field can only be updated on `draft` invoices.
        """
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the invoice. It must belong to the customer associated with the invoice. If not set, defaults to the subscription's default payment method, if any, or to the default payment method in the customer's invoice settings.
        """
        default_source: NotRequired["Literal['']|str"]
        """
        ID of the default payment source for the invoice. It must belong to the customer associated with the invoice and be in a chargeable state. If not set, defaults to the subscription's default source, if any, or to the customer's default source.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates that will apply to any line item that does not have `tax_rates` set. Pass an empty string to remove previously-defined tax rates.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users. Referenced as 'memo' in the Dashboard.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.ModifyParamsDiscount]"
        ]
        """
        The discounts that will apply to the invoice. Pass an empty string to remove previously-defined discounts.
        """
        due_date: NotRequired[int]
        """
        The date on which payment for this invoice is due. Only valid for invoices where `collection_method=send_invoice`. This field can only be updated on `draft` invoices.
        """
        effective_at: NotRequired["Literal['']|int"]
        """
        The date when this invoice is in effect. Same as `finalized_at` unless overwritten. When defined, this value replaces the system-generated 'Date of issue' printed on the invoice PDF and receipt.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        footer: NotRequired[str]
        """
        Footer to be displayed on the invoice.
        """
        issuer: NotRequired["Invoice.ModifyParamsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        number: NotRequired["Literal['']|str"]
        """
        Set the number for this invoice. If no number is present then a number will be assigned automatically when the invoice is finalized. In many markets, regulations require invoices to be unique, sequential and / or gapless. You are responsible for ensuring this is true across all your different invoicing systems in the event that you edit the invoice number using our API. If you use only Stripe for your invoices and do not change invoice numbers, Stripe handles this aspect of compliance for you automatically.
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account (if any) for which the funds of the invoice payment are intended. If set, the invoice will be presented with the branding and support information of the specified account. See the [Invoices with Connect](https://stripe.com/docs/billing/invoices/connect) documentation for details.
        """
        payment_settings: NotRequired["Invoice.ModifyParamsPaymentSettings"]
        """
        Configuration settings for the PaymentIntent that is generated when the invoice is finalized.
        """
        rendering: NotRequired["Invoice.ModifyParamsRendering"]
        """
        The rendering-related settings that control how the invoice is displayed on customer-facing surfaces such as PDF and Hosted Invoice Page.
        """
        shipping_cost: NotRequired[
            "Literal['']|Invoice.ModifyParamsShippingCost"
        ]
        """
        Settings for the cost of shipping for this invoice.
        """
        shipping_details: NotRequired[
            "Literal['']|Invoice.ModifyParamsShippingDetails"
        ]
        """
        Shipping details for the invoice. The Invoice PDF will use the `shipping_details` value if it is set, otherwise the PDF will render the shipping address from the customer.
        """
        statement_descriptor: NotRequired[str]
        """
        Extra information about a charge for the customer's credit card statement. It must contain at least one letter. If not specified and this invoice is part of a subscription, the default `statement_descriptor` will be set to the first subscription item's product's `statement_descriptor`.
        """
        transfer_data: NotRequired[
            "Literal['']|Invoice.ModifyParamsTransferData"
        ]
        """
        If specified, the funds from the invoice will be transferred to the destination and the ID of the resulting transfer will be found on the invoice's charge. This will be unset if you POST an empty value.
        """

    class ModifyParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Whether Stripe automatically computes tax on this invoice. Note that incompatible invoice items (invoice items with manually specified [tax rates](https://stripe.com/docs/api/tax_rates), negative amounts, or `tax_behavior=unspecified`) cannot be added to automatic tax invoices.
        """
        liability: NotRequired["Invoice.ModifyParamsAutomaticTaxLiability"]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class ModifyParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class ModifyParamsCustomField(TypedDict):
        name: str
        """
        The name of the custom field. This may be up to 40 characters.
        """
        value: str
        """
        The value of the custom field. This may be up to 140 characters.
        """

    class ModifyParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class ModifyParamsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class ModifyParamsPaymentSettings(TypedDict):
        default_mandate: NotRequired["Literal['']|str"]
        """
        ID of the mandate to be used for this invoice. It must correspond to the payment method used to pay the invoice, including the invoice's default_payment_method or default_source, if set.
        """
        payment_method_options: NotRequired[
            "Invoice.ModifyParamsPaymentSettingsPaymentMethodOptions"
        ]
        """
        Payment-method-specific configuration to provide to the invoice's PaymentIntent.
        """
        payment_method_types: NotRequired[
            "Literal['']|List[Literal['ach_credit_transfer', 'ach_debit', 'acss_debit', 'amazon_pay', 'au_becs_debit', 'bacs_debit', 'bancontact', 'boleto', 'card', 'cashapp', 'customer_balance', 'eps', 'fpx', 'giropay', 'grabpay', 'ideal', 'konbini', 'link', 'multibanco', 'p24', 'paynow', 'paypal', 'promptpay', 'revolut_pay', 'sepa_credit_transfer', 'sepa_debit', 'sofort', 'swish', 'us_bank_account', 'wechat_pay']]"
        ]
        """
        The list of payment method types (e.g. card) to provide to the invoice's PaymentIntent. If not set, Stripe attempts to automatically determine the types to use by looking at the invoice's default payment method, the subscription's default payment method, the customer's default payment method, and your [invoice template settings](https://dashboard.stripe.com/settings/billing/invoice).
        """

    class ModifyParamsPaymentSettingsPaymentMethodOptions(TypedDict):
        acss_debit: NotRequired[
            "Literal['']|Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsAcssDebit"
        ]
        """
        If paying by `acss_debit`, this sub-hash contains details about the Canadian pre-authorized debit payment method options to pass to the invoice's PaymentIntent.
        """
        bancontact: NotRequired[
            "Literal['']|Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsBancontact"
        ]
        """
        If paying by `bancontact`, this sub-hash contains details about the Bancontact payment method options to pass to the invoice's PaymentIntent.
        """
        card: NotRequired[
            "Literal['']|Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsCard"
        ]
        """
        If paying by `card`, this sub-hash contains details about the Card payment method options to pass to the invoice's PaymentIntent.
        """
        customer_balance: NotRequired[
            "Literal['']|Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsCustomerBalance"
        ]
        """
        If paying by `customer_balance`, this sub-hash contains details about the Bank transfer payment method options to pass to the invoice's PaymentIntent.
        """
        konbini: NotRequired[
            "Literal['']|Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsKonbini"
        ]
        """
        If paying by `konbini`, this sub-hash contains details about the Konbini payment method options to pass to the invoice's PaymentIntent.
        """
        sepa_debit: NotRequired[
            "Literal['']|Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsSepaDebit"
        ]
        """
        If paying by `sepa_debit`, this sub-hash contains details about the SEPA Direct Debit payment method options to pass to the invoice's PaymentIntent.
        """
        us_bank_account: NotRequired[
            "Literal['']|Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If paying by `us_bank_account`, this sub-hash contains details about the ACH direct debit payment method options to pass to the invoice's PaymentIntent.
        """

    class ModifyParamsPaymentSettingsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsAcssDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Verification method for the intent
        """

    class ModifyParamsPaymentSettingsPaymentMethodOptionsAcssDebitMandateOptions(
        TypedDict,
    ):
        transaction_type: NotRequired[Literal["business", "personal"]]
        """
        Transaction type of the mandate.
        """

    class ModifyParamsPaymentSettingsPaymentMethodOptionsBancontact(TypedDict):
        preferred_language: NotRequired[Literal["de", "en", "fr", "nl"]]
        """
        Preferred language of the Bancontact authorization page that the customer is redirected to.
        """

    class ModifyParamsPaymentSettingsPaymentMethodOptionsCard(TypedDict):
        installments: NotRequired[
            "Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsCardInstallments"
        ]
        """
        Installment configuration for payments attempted on this invoice (Mexico Only).

        For more information, see the [installments integration guide](https://stripe.com/docs/payments/installments).
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """

    class ModifyParamsPaymentSettingsPaymentMethodOptionsCardInstallments(
        TypedDict,
    ):
        enabled: NotRequired[bool]
        """
        Setting to true enables installments for this invoice.
        Setting to false will prevent any selected plan from applying to a payment.
        """
        plan: NotRequired[
            "Literal['']|Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsCardInstallmentsPlan"
        ]
        """
        The selected installment plan to use for this invoice.
        """

    class ModifyParamsPaymentSettingsPaymentMethodOptionsCardInstallmentsPlan(
        TypedDict,
    ):
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

    class ModifyParamsPaymentSettingsPaymentMethodOptionsCustomerBalance(
        TypedDict,
    ):
        bank_transfer: NotRequired[
            "Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransfer"
        ]
        """
        Configuration for the bank transfer funding type, if the `funding_type` is set to `bank_transfer`.
        """
        funding_type: NotRequired[str]
        """
        The funding method type to be used when there are not enough funds in the customer balance. Permitted values include: `bank_transfer`.
        """

    class ModifyParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransfer(
        TypedDict,
    ):
        eu_bank_transfer: NotRequired[
            "Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
        ]
        """
        Configuration for eu_bank_transfer funding type.
        """
        type: NotRequired[str]
        """
        The bank transfer type that can be used for funding. Permitted values include: `eu_bank_transfer`, `gb_bank_transfer`, `jp_bank_transfer`, `mx_bank_transfer`, or `us_bank_transfer`.
        """

    class ModifyParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer(
        TypedDict,
    ):
        country: str
        """
        The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
        """

    class ModifyParamsPaymentSettingsPaymentMethodOptionsKonbini(TypedDict):
        pass

    class ModifyParamsPaymentSettingsPaymentMethodOptionsSepaDebit(TypedDict):
        pass

    class ModifyParamsPaymentSettingsPaymentMethodOptionsUsBankAccount(
        TypedDict,
    ):
        financial_connections: NotRequired[
            "Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Verification method for the intent
        """

    class ModifyParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnections(
        TypedDict,
    ):
        filters: NotRequired[
            "Invoice.ModifyParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
        ]
        """
        Provide filters for the linked accounts that the customer can select for the payment method.
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

    class ModifyParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters(
        TypedDict,
    ):
        account_subcategories: NotRequired[
            List[Literal["checking", "savings"]]
        ]
        """
        The account subcategories to use to filter for selectable accounts. Valid subcategories are `checking` and `savings`.
        """

    class ModifyParamsRendering(TypedDict):
        amount_tax_display: NotRequired[
            "Literal['']|Literal['exclude_tax', 'include_inclusive_tax']"
        ]
        """
        How line-item prices and amounts will be displayed with respect to tax on invoice PDFs. One of `exclude_tax` or `include_inclusive_tax`. `include_inclusive_tax` will include inclusive tax (and exclude exclusive tax) in invoice PDF amounts. `exclude_tax` will exclude all tax (inclusive and exclusive alike) from invoice PDF amounts.
        """
        pdf: NotRequired["Invoice.ModifyParamsRenderingPdf"]
        """
        Invoice pdf rendering options
        """

    class ModifyParamsRenderingPdf(TypedDict):
        page_size: NotRequired[Literal["a4", "auto", "letter"]]
        """
        Page size for invoice PDF. Can be set to `a4`, `letter`, or `auto`.
         If set to `auto`, invoice PDF page size defaults to `a4` for customers with
         Japanese locale and `letter` for customers with other locales.
        """

    class ModifyParamsShippingCost(TypedDict):
        shipping_rate: NotRequired[str]
        """
        The ID of the shipping rate to use for this order.
        """
        shipping_rate_data: NotRequired[
            "Invoice.ModifyParamsShippingCostShippingRateData"
        ]
        """
        Parameters to create a new ad-hoc shipping rate for this order.
        """

    class ModifyParamsShippingCostShippingRateData(TypedDict):
        delivery_estimate: NotRequired[
            "Invoice.ModifyParamsShippingCostShippingRateDataDeliveryEstimate"
        ]
        """
        The estimated range for how long shipping will take, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        display_name: str
        """
        The name of the shipping rate, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        fixed_amount: NotRequired[
            "Invoice.ModifyParamsShippingCostShippingRateDataFixedAmount"
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

    class ModifyParamsShippingCostShippingRateDataDeliveryEstimate(TypedDict):
        maximum: NotRequired[
            "Invoice.ModifyParamsShippingCostShippingRateDataDeliveryEstimateMaximum"
        ]
        """
        The upper bound of the estimated range. If empty, represents no upper bound i.e., infinite.
        """
        minimum: NotRequired[
            "Invoice.ModifyParamsShippingCostShippingRateDataDeliveryEstimateMinimum"
        ]
        """
        The lower bound of the estimated range. If empty, represents no lower bound.
        """

    class ModifyParamsShippingCostShippingRateDataDeliveryEstimateMaximum(
        TypedDict,
    ):
        unit: Literal["business_day", "day", "hour", "month", "week"]
        """
        A unit of time.
        """
        value: int
        """
        Must be greater than 0.
        """

    class ModifyParamsShippingCostShippingRateDataDeliveryEstimateMinimum(
        TypedDict,
    ):
        unit: Literal["business_day", "day", "hour", "month", "week"]
        """
        A unit of time.
        """
        value: int
        """
        Must be greater than 0.
        """

    class ModifyParamsShippingCostShippingRateDataFixedAmount(TypedDict):
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
                "Invoice.ModifyParamsShippingCostShippingRateDataFixedAmountCurrencyOptions",
            ]
        ]
        """
        Shipping rates defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """

    class ModifyParamsShippingCostShippingRateDataFixedAmountCurrencyOptions(
        TypedDict,
    ):
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

    class ModifyParamsShippingDetails(TypedDict):
        address: "Invoice.ModifyParamsShippingDetailsAddress"
        """
        Shipping address
        """
        name: str
        """
        Recipient name.
        """
        phone: NotRequired["Literal['']|str"]
        """
        Recipient phone (including extension)
        """

    class ModifyParamsShippingDetailsAddress(TypedDict):
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
        The amount that will be transferred automatically when the invoice is paid. If no amount is set, the full amount is transferred.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class PayParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        forgive: NotRequired[bool]
        """
        In cases where the source used to pay the invoice has insufficient funds, passing `forgive=true` controls whether a charge should be attempted for the full amount available on the source, up to the amount to fully pay the invoice. This effectively forgives the difference between the amount available on the source and the amount due.

        Passing `forgive=false` will fail the charge if the source hasn't been pre-funded with the right amount. An example for this case is with ACH Credit Transfers and wires: if the amount wired is less than the amount due by a small amount, you might want to forgive the difference. Defaults to `false`.
        """
        mandate: NotRequired["Literal['']|str"]
        """
        ID of the mandate to be used for this invoice. It must correspond to the payment method used to pay the invoice, including the payment_method param or the invoice's default_payment_method or default_source, if set.
        """
        off_session: NotRequired[bool]
        """
        Indicates if a customer is on or off-session while an invoice payment is attempted. Defaults to `true` (off-session).
        """
        paid_out_of_band: NotRequired[bool]
        """
        Boolean representing whether an invoice is paid outside of Stripe. This will result in no charge being made. Defaults to `false`.
        """
        payment_method: NotRequired[str]
        """
        A PaymentMethod to be charged. The PaymentMethod must be the ID of a PaymentMethod belonging to the customer associated with the invoice being paid.
        """
        source: NotRequired[str]
        """
        A payment source to be charged. The source must be the ID of a source belonging to the customer associated with the invoice being paid.
        """

    class RemoveLinesParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        lines: List["Invoice.RemoveLinesParamsLine"]
        """
        The line items to remove.
        """

    class RemoveLinesParamsLine(TypedDict):
        behavior: Literal["delete", "unassign"]
        """
        Either `delete` or `unassign`. Deleted line items are permanently deleted. Unassigned line items can be reassigned to an invoice.
        """
        id: str
        """
        ID of an existing line item to remove from this invoice.
        """

    class RetrieveParams(RequestOptions):
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
        The search query string. See [search query language](https://stripe.com/docs/search#search-query-language) and the list of supported [query fields for invoices](https://stripe.com/docs/search#query-fields-for-invoices).
        """

    class SendInvoiceParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpcomingLinesParams(RequestOptions):
        automatic_tax: NotRequired["Invoice.UpcomingLinesParamsAutomaticTax"]
        """
        Settings for automatic tax lookup for this invoice preview.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this phase of the subscription schedule. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        currency: NotRequired[str]
        """
        The currency to preview this invoice in. Defaults to that of `customer` if not specified.
        """
        customer: NotRequired[str]
        """
        The identifier of the customer whose upcoming invoice you'd like to retrieve. If `automatic_tax` is enabled then one of `customer`, `customer_details`, `subscription`, or `schedule` must be set.
        """
        customer_details: NotRequired[
            "Invoice.UpcomingLinesParamsCustomerDetails"
        ]
        """
        Details about the customer you want to invoice or overrides for an existing customer. If `automatic_tax` is enabled then one of `customer`, `customer_details`, `subscription`, or `schedule` must be set.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingLinesParamsDiscount]"
        ]
        """
        The coupons to redeem into discounts for the invoice preview. If not specified, inherits the discount from the subscription or customer. This works for both coupons directly applied to an invoice and coupons applied to a subscription. Pass an empty string to avoid inheriting any discounts.
        """
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_items: NotRequired[
            List["Invoice.UpcomingLinesParamsInvoiceItem"]
        ]
        """
        List of invoice items to add or update in the upcoming invoice preview.
        """
        issuer: NotRequired["Invoice.UpcomingLinesParamsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account (if any) for which the funds of the invoice payment are intended. If set, the invoice will be presented with the branding and support information of the specified account. See the [Invoices with Connect](https://stripe.com/docs/billing/invoices/connect) documentation for details.
        """
        preview_mode: NotRequired[Literal["next", "recurring"]]
        """
        Customizes the types of values to include when calculating the invoice. Defaults to `next` if unspecified.
        """
        schedule: NotRequired[str]
        """
        The identifier of the schedule whose upcoming invoice you'd like to retrieve. Cannot be used with subscription or subscription fields.
        """
        schedule_details: NotRequired[
            "Invoice.UpcomingLinesParamsScheduleDetails"
        ]
        """
        The schedule creation or modification params to apply as a preview. Cannot be used with `subscription` or `subscription_` prefixed fields.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        subscription: NotRequired[str]
        """
        The identifier of the subscription for which you'd like to retrieve the upcoming invoice. If not provided, but a `subscription_details.items` is provided, you will preview creating a subscription with those items. If neither `subscription` nor `subscription_details.items` is provided, you will retrieve the next upcoming invoice from among the customer's subscriptions.
        """
        subscription_billing_cycle_anchor: NotRequired[
            "Literal['now', 'unchanged']|int"
        ]
        """
        For new subscriptions, a future timestamp to anchor the subscription's [billing cycle](https://stripe.com/docs/subscriptions/billing-cycle). This is used to determine the date of the first full invoice, and, for plans with `month` or `year` intervals, the day of the month for subsequent invoices. For existing subscriptions, the value can only be set to `now` or `unchanged`. This field has been deprecated and will be removed in a future API version. Use `subscription_details.billing_cycle_anchor` instead.
        """
        subscription_cancel_at: NotRequired["Literal['']|int"]
        """
        A timestamp at which the subscription should cancel. If set to a date before the current period ends, this will cause a proration if prorations have been enabled using `proration_behavior`. If set during a future period, this will always cause a proration for that period. This field has been deprecated and will be removed in a future API version. Use `subscription_details.cancel_at` instead.
        """
        subscription_cancel_at_period_end: NotRequired[bool]
        """
        Indicate whether this subscription should cancel at the end of the current period (`current_period_end`). Defaults to `false`. This field has been deprecated and will be removed in a future API version. Use `subscription_details.cancel_at_period_end` instead.
        """
        subscription_cancel_now: NotRequired[bool]
        """
        This simulates the subscription being canceled or expired immediately. This field has been deprecated and will be removed in a future API version. Use `subscription_details.cancel_now` instead.
        """
        subscription_default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with these default tax rates. The default tax rates will apply to any line item that does not have `tax_rates` set. This field has been deprecated and will be removed in a future API version. Use `subscription_details.default_tax_rates` instead.
        """
        subscription_details: NotRequired[
            "Invoice.UpcomingLinesParamsSubscriptionDetails"
        ]
        """
        The subscription creation or modification params to apply as a preview. Cannot be used with `schedule` or `schedule_details` fields.
        """
        subscription_items: NotRequired[
            List["Invoice.UpcomingLinesParamsSubscriptionItem"]
        ]
        """
        A list of up to 20 subscription items, each with an attached price. This field has been deprecated and will be removed in a future API version. Use `subscription_details.items` instead.
        """
        subscription_proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`. This field has been deprecated and will be removed in a future API version. Use `subscription_details.proration_behavior` instead.
        """
        subscription_proration_date: NotRequired[int]
        """
        If previewing an update to a subscription, and doing proration, `subscription_proration_date` forces the proration to be calculated as though the update was done at the specified time. The time given must be within the current subscription period and within the current phase of the schedule backing this subscription, if the schedule exists. If set, `subscription`, and one of `subscription_items`, or `subscription_trial_end` are required. Also, `subscription_proration_behavior` cannot be set to 'none'. This field has been deprecated and will be removed in a future API version. Use `subscription_details.proration_date` instead.
        """
        subscription_resume_at: NotRequired[Literal["now"]]
        """
        For paused subscriptions, setting `subscription_resume_at` to `now` will preview the invoice that will be generated if the subscription is resumed. This field has been deprecated and will be removed in a future API version. Use `subscription_details.resume_at` instead.
        """
        subscription_start_date: NotRequired[int]
        """
        Date a subscription is intended to start (can be future or past). This field has been deprecated and will be removed in a future API version. Use `subscription_details.start_date` instead.
        """
        subscription_trial_end: NotRequired["Literal['now']|int"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with that trial end. If set, one of `subscription_items` or `subscription` is required. This field has been deprecated and will be removed in a future API version. Use `subscription_details.trial_end` instead.
        """
        subscription_trial_from_plan: NotRequired[bool]
        """
        Indicates if a plan's `trial_period_days` should be applied to the subscription. Setting `subscription_trial_end` per subscription is preferred, and this defaults to `false`. Setting this flag to `true` together with `subscription_trial_end` is not allowed. See [Using trial periods on subscriptions](https://stripe.com/docs/billing/subscriptions/trials) to learn more.
        """

    class UpcomingLinesParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Whether Stripe automatically computes tax on this invoice. Note that incompatible invoice items (invoice items with manually specified [tax rates](https://stripe.com/docs/api/tax_rates), negative amounts, or `tax_behavior=unspecified`) cannot be added to automatic tax invoices.
        """
        liability: NotRequired[
            "Invoice.UpcomingLinesParamsAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class UpcomingLinesParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingLinesParamsCustomerDetails(TypedDict):
        address: NotRequired[
            "Literal['']|Invoice.UpcomingLinesParamsCustomerDetailsAddress"
        ]
        """
        The customer's address.
        """
        shipping: NotRequired[
            "Literal['']|Invoice.UpcomingLinesParamsCustomerDetailsShipping"
        ]
        """
        The customer's shipping information. Appears on invoices emailed to this customer.
        """
        tax: NotRequired["Invoice.UpcomingLinesParamsCustomerDetailsTax"]
        """
        Tax details about the customer.
        """
        tax_exempt: NotRequired[
            "Literal['']|Literal['exempt', 'none', 'reverse']"
        ]
        """
        The customer's tax exemption. One of `none`, `exempt`, or `reverse`.
        """
        tax_ids: NotRequired[
            List["Invoice.UpcomingLinesParamsCustomerDetailsTaxId"]
        ]
        """
        The customer's tax IDs.
        """

    class UpcomingLinesParamsCustomerDetailsAddress(TypedDict):
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

    class UpcomingLinesParamsCustomerDetailsShipping(TypedDict):
        address: "Invoice.UpcomingLinesParamsCustomerDetailsShippingAddress"
        """
        Customer shipping address.
        """
        name: str
        """
        Customer name.
        """
        phone: NotRequired[str]
        """
        Customer phone (including extension).
        """

    class UpcomingLinesParamsCustomerDetailsShippingAddress(TypedDict):
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

    class UpcomingLinesParamsCustomerDetailsTax(TypedDict):
        ip_address: NotRequired["Literal['']|str"]
        """
        A recent IP address of the customer used for tax reporting and tax location inference. Stripe recommends updating the IP address when a new PaymentMethod is attached or the address field on the customer is updated. We recommend against updating this field more frequently since it could result in unexpected tax location/reporting outcomes.
        """

    class UpcomingLinesParamsCustomerDetailsTaxId(TypedDict):
        type: Literal[
            "ad_nrt",
            "ae_trn",
            "ar_cuit",
            "au_abn",
            "au_arn",
            "bg_uic",
            "bh_vat",
            "bo_tin",
            "br_cnpj",
            "br_cpf",
            "ca_bn",
            "ca_gst_hst",
            "ca_pst_bc",
            "ca_pst_mb",
            "ca_pst_sk",
            "ca_qst",
            "ch_uid",
            "ch_vat",
            "cl_tin",
            "cn_tin",
            "co_nit",
            "cr_tin",
            "de_stn",
            "do_rcn",
            "ec_ruc",
            "eg_tin",
            "es_cif",
            "eu_oss_vat",
            "eu_vat",
            "gb_vat",
            "ge_vat",
            "hk_br",
            "hu_tin",
            "id_npwp",
            "il_vat",
            "in_gst",
            "is_vat",
            "jp_cn",
            "jp_rn",
            "jp_trn",
            "ke_pin",
            "kr_brn",
            "kz_bin",
            "li_uid",
            "mx_rfc",
            "my_frp",
            "my_itn",
            "my_sst",
            "ng_tin",
            "no_vat",
            "no_voec",
            "nz_gst",
            "om_vat",
            "pe_ruc",
            "ph_tin",
            "ro_tin",
            "rs_pib",
            "ru_inn",
            "ru_kpp",
            "sa_vat",
            "sg_gst",
            "sg_uen",
            "si_tin",
            "sv_nit",
            "th_vat",
            "tr_tin",
            "tw_vat",
            "ua_vat",
            "us_ein",
            "uy_ruc",
            "ve_rif",
            "vn_tin",
            "za_vat",
        ]
        """
        Type of the tax ID, one of `ad_nrt`, `ae_trn`, `ar_cuit`, `au_abn`, `au_arn`, `bg_uic`, `bh_vat`, `bo_tin`, `br_cnpj`, `br_cpf`, `ca_bn`, `ca_gst_hst`, `ca_pst_bc`, `ca_pst_mb`, `ca_pst_sk`, `ca_qst`, `ch_uid`, `ch_vat`, `cl_tin`, `cn_tin`, `co_nit`, `cr_tin`, `de_stn`, `do_rcn`, `ec_ruc`, `eg_tin`, `es_cif`, `eu_oss_vat`, `eu_vat`, `gb_vat`, `ge_vat`, `hk_br`, `hu_tin`, `id_npwp`, `il_vat`, `in_gst`, `is_vat`, `jp_cn`, `jp_rn`, `jp_trn`, `ke_pin`, `kr_brn`, `kz_bin`, `li_uid`, `mx_rfc`, `my_frp`, `my_itn`, `my_sst`, `ng_tin`, `no_vat`, `no_voec`, `nz_gst`, `om_vat`, `pe_ruc`, `ph_tin`, `ro_tin`, `rs_pib`, `ru_inn`, `ru_kpp`, `sa_vat`, `sg_gst`, `sg_uen`, `si_tin`, `sv_nit`, `th_vat`, `tr_tin`, `tw_vat`, `ua_vat`, `us_ein`, `uy_ruc`, `ve_rif`, `vn_tin`, or `za_vat`
        """
        value: str
        """
        Value of the tax ID.
        """

    class UpcomingLinesParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingLinesParamsInvoiceItem(TypedDict):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) of previewed invoice item.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies). Only applicable to new invoice items.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to the invoice item. The description is displayed in the invoice for easy tracking.
        """
        discountable: NotRequired[bool]
        """
        Explicitly controls whether discounts apply to this invoice item. Defaults to true, except for negative invoice items.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingLinesParamsInvoiceItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the invoice item in the preview.
        """
        invoiceitem: NotRequired[str]
        """
        The ID of the invoice item to update in preview. If not specified, a new invoice item will be added to the preview of the upcoming invoice.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        period: NotRequired["Invoice.UpcomingLinesParamsInvoiceItemPeriod"]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "Invoice.UpcomingLinesParamsInvoiceItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the invoice item.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tax_code: NotRequired["Literal['']|str"]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates that apply to the item. When set, any `default_tax_rates` do not apply to this item.
        """
        unit_amount: NotRequired[int]
        """
        The integer unit amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. This unit_amount will be multiplied by the quantity to get the full amount. If you want to apply a credit to the customer's account, pass a negative unit_amount.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingLinesParamsInvoiceItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingLinesParamsInvoiceItemPeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class UpcomingLinesParamsInvoiceItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingLinesParamsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingLinesParamsScheduleDetails(TypedDict):
        end_behavior: NotRequired[Literal["cancel", "release"]]
        """
        Behavior of the subscription schedule and underlying subscription when it ends. Possible values are `release` or `cancel` with the default being `release`. `release` will end the subscription schedule and keep the underlying subscription running. `cancel` will end the subscription schedule and cancel the underlying subscription.
        """
        phases: NotRequired[
            List["Invoice.UpcomingLinesParamsScheduleDetailsPhase"]
        ]
        """
        List representing phases of the subscription schedule. Each phase can be customized to have different durations, plans, and coupons. If there are multiple phases, the `end_date` of one phase will always equal the `start_date` of the next phase.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        In cases where the `schedule_details` params update the currently active phase, specifies if and how to prorate at the time of the request.
        """

    class UpcomingLinesParamsScheduleDetailsPhase(TypedDict):
        add_invoice_items: NotRequired[
            List[
                "Invoice.UpcomingLinesParamsScheduleDetailsPhaseAddInvoiceItem"
            ]
        ]
        """
        A list of prices and quantities that will generate invoice items appended to the next invoice for this phase. You may pass up to 20 items.
        """
        application_fee_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. The request must be made by a platform account on a connected account in order to set an application fee percentage. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        automatic_tax: NotRequired[
            "Invoice.UpcomingLinesParamsScheduleDetailsPhaseAutomaticTax"
        ]
        """
        Automatic tax settings for this phase.
        """
        billing_cycle_anchor: NotRequired[Literal["automatic", "phase_start"]]
        """
        Can be set to `phase_start` to set the anchor to the start of the phase or `automatic` to automatically change it if needed. Cannot be set to `phase_start` if this phase specifies a trial. For more information, see the billing cycle [documentation](https://stripe.com/docs/billing/subscriptions/billing-cycle).
        """
        billing_thresholds: NotRequired[
            "Literal['']|Invoice.UpcomingLinesParamsScheduleDetailsPhaseBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. Pass an empty string to remove previously-defined thresholds.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay the underlying subscription at the end of each billing cycle using the default source attached to the customer. When sending an invoice, Stripe will email your customer an invoice with payment instructions and mark the subscription as `active`. Defaults to `charge_automatically` on creation.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this phase of the subscription schedule. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the subscription schedule. It must belong to the customer associated with the subscription schedule. If not set, invoices will use the default payment method in the customer's invoice settings.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will set the Subscription's [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates), which means they will be the Invoice's [`default_tax_rates`](https://stripe.com/docs/api/invoices/create#create_invoice-default_tax_rates) for any Invoices issued by the Subscription during this Phase.
        """
        description: NotRequired["Literal['']|str"]
        """
        Subscription description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingLinesParamsScheduleDetailsPhaseDiscount]"
        ]
        """
        The coupons to redeem into discounts for the schedule phase. If not specified, inherits the discount from the subscription's customer. Pass an empty string to avoid inheriting any discounts.
        """
        end_date: NotRequired["int|Literal['now']"]
        """
        The date at which this phase of the subscription schedule ends. If set, `iterations` must not be set.
        """
        invoice_settings: NotRequired[
            "Invoice.UpcomingLinesParamsScheduleDetailsPhaseInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        items: List["Invoice.UpcomingLinesParamsScheduleDetailsPhaseItem"]
        """
        List of configuration items, each with an attached price, to apply during this phase of the subscription schedule.
        """
        iterations: NotRequired[int]
        """
        Integer representing the multiplier applied to the price interval. For example, `iterations=2` applied to a price with `interval=month` and `interval_count=3` results in a phase of duration `2 * 3 months = 6 months`. If set, `end_date` must not be set.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to a phase. Metadata on a schedule's phase will update the underlying subscription's `metadata` when the phase is entered, adding new keys and replacing existing keys in the subscription's `metadata`. Individual keys in the subscription's `metadata` can be unset by posting an empty value to them in the phase's `metadata`. To unset all keys in the subscription's `metadata`, update the subscription directly or unset every key individually from the phase's `metadata`.
        """
        on_behalf_of: NotRequired[str]
        """
        The account on behalf of which to charge, for each of the associated subscription's invoices.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Whether the subscription schedule will create [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when transitioning to this phase. The default value is `create_prorations`. This setting controls prorations when a phase is started asynchronously and it is persisted as a field on the phase. It's different from the request-level [proration_behavior](https://stripe.com/docs/api/subscription_schedules/update#update_subscription_schedule-proration_behavior) parameter which controls what happens if the update request affects the billing configuration of the current phase.
        """
        start_date: NotRequired["int|Literal['now']"]
        """
        The date at which this phase of the subscription schedule starts or `now`. Must be set on the first phase.
        """
        transfer_data: NotRequired[
            "Invoice.UpcomingLinesParamsScheduleDetailsPhaseTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the associated subscription's invoices.
        """
        trial: NotRequired[bool]
        """
        If set to true the entire phase is counted as a trial and the customer will not be charged for any fees.
        """
        trial_end: NotRequired["int|Literal['now']"]
        """
        Sets the phase to trialing from the start date to this date. Must be before the phase end date, can not be combined with `trial`
        """

    class UpcomingLinesParamsScheduleDetailsPhaseAddInvoiceItem(TypedDict):
        discounts: NotRequired[
            List[
                "Invoice.UpcomingLinesParamsScheduleDetailsPhaseAddInvoiceItemDiscount"
            ]
        ]
        """
        The coupons to redeem into discounts for the item.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "Invoice.UpcomingLinesParamsScheduleDetailsPhaseAddInvoiceItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item. Defaults to 1.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the item. When set, the `default_tax_rates` do not apply to this item.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseAddInvoiceItemDiscount(
        TypedDict,
    ):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseAddInvoiceItemPriceData(
        TypedDict,
    ):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
        """
        liability: NotRequired[
            "Invoice.UpcomingLinesParamsScheduleDetailsPhaseAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseAutomaticTaxLiability(
        TypedDict,
    ):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseBillingThresholds(TypedDict):
        amount_gte: NotRequired[int]
        """
        Monetary threshold that triggers the subscription to advance to a new billing period
        """
        reset_billing_cycle_anchor: NotRequired[bool]
        """
        Indicates if the `billing_cycle_anchor` should be reset when a threshold is reached. If true, `billing_cycle_anchor` will be updated to the date/time the threshold was last reached; otherwise, the value will remain unchanged.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseInvoiceSettings(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with this phase of the subscription schedule. Will be set on invoices generated by this phase of the subscription schedule.
        """
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay invoices generated by this subscription schedule. This value will be `null` for subscription schedules where `billing=charge_automatically`.
        """
        issuer: NotRequired[
            "Invoice.UpcomingLinesParamsScheduleDetailsPhaseInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseInvoiceSettingsIssuer(
        TypedDict,
    ):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|Invoice.UpcomingLinesParamsScheduleDetailsPhaseItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingLinesParamsScheduleDetailsPhaseItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to a configuration item. Metadata on a configuration item will update the underlying subscription item's `metadata` when the phase is entered, adding new keys and replacing existing keys. Individual keys in the subscription item's `metadata` can be unset by posting an empty value to them in the configuration item's `metadata`. To unset all keys in the subscription item's `metadata`, update the subscription item directly or unset every key individually from the configuration item's `metadata`.
        """
        plan: NotRequired[str]
        """
        The plan ID to subscribe to. You may specify the same ID in `plan` and `price`.
        """
        price: NotRequired[str]
        """
        The ID of the price object.
        """
        price_data: NotRequired[
            "Invoice.UpcomingLinesParamsScheduleDetailsPhaseItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline.
        """
        quantity: NotRequired[int]
        """
        Quantity for the given price. Can be set only if the price's `usage_type` is `licensed` and not `metered`.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseItemBillingThresholds(
        TypedDict,
    ):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class UpcomingLinesParamsScheduleDetailsPhaseItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "Invoice.UpcomingLinesParamsScheduleDetailsPhaseItemPriceDataRecurring"
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingLinesParamsScheduleDetailsPhaseItemPriceDataRecurring(
        TypedDict,
    ):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpcomingLinesParamsScheduleDetailsPhaseTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class UpcomingLinesParamsSubscriptionDetails(TypedDict):
        billing_cycle_anchor: NotRequired["Literal['now', 'unchanged']|int"]
        """
        For new subscriptions, a future timestamp to anchor the subscription's [billing cycle](https://stripe.com/docs/subscriptions/billing-cycle). This is used to determine the date of the first full invoice, and, for plans with `month` or `year` intervals, the day of the month for subsequent invoices. For existing subscriptions, the value can only be set to `now` or `unchanged`.
        """
        cancel_at: NotRequired["Literal['']|int"]
        """
        A timestamp at which the subscription should cancel. If set to a date before the current period ends, this will cause a proration if prorations have been enabled using `proration_behavior`. If set during a future period, this will always cause a proration for that period.
        """
        cancel_at_period_end: NotRequired[bool]
        """
        Indicate whether this subscription should cancel at the end of the current period (`current_period_end`). Defaults to `false`.
        """
        cancel_now: NotRequired[bool]
        """
        This simulates the subscription being canceled or expired immediately.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with these default tax rates. The default tax rates will apply to any line item that does not have `tax_rates` set.
        """
        items: NotRequired[
            List["Invoice.UpcomingLinesParamsSubscriptionDetailsItem"]
        ]
        """
        A list of up to 20 subscription items, each with an attached price.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`.
        """
        proration_date: NotRequired[int]
        """
        If previewing an update to a subscription, and doing proration, `subscription_details.proration_date` forces the proration to be calculated as though the update was done at the specified time. The time given must be within the current subscription period and within the current phase of the schedule backing this subscription, if the schedule exists. If set, `subscription`, and one of `subscription_details.items`, or `subscription_details.trial_end` are required. Also, `subscription_details.proration_behavior` cannot be set to 'none'.
        """
        resume_at: NotRequired[Literal["now"]]
        """
        For paused subscriptions, setting `subscription_details.resume_at` to `now` will preview the invoice that will be generated if the subscription is resumed.
        """
        start_date: NotRequired[int]
        """
        Date a subscription is intended to start (can be future or past).
        """
        trial_end: NotRequired["Literal['now']|int"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with that trial end. If set, one of `subscription_details.items` or `subscription` is required.
        """

    class UpcomingLinesParamsSubscriptionDetailsItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|Invoice.UpcomingLinesParamsSubscriptionDetailsItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        clear_usage: NotRequired[bool]
        """
        Delete all usage for a given subscription item. Allowed only when `deleted` is set to `true` and the current plan's `usage_type` is `metered`.
        """
        deleted: NotRequired[bool]
        """
        A flag that, if set to `true`, will delete the specified item.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingLinesParamsSubscriptionDetailsItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        id: NotRequired[str]
        """
        Subscription item to update.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        plan: NotRequired[str]
        """
        Plan ID for this item, as a string.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required. When changing a subscription item's price, `quantity` is set to 1 unless a `quantity` parameter is provided.
        """
        price_data: NotRequired[
            "Invoice.UpcomingLinesParamsSubscriptionDetailsItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class UpcomingLinesParamsSubscriptionDetailsItemBillingThresholds(
        TypedDict,
    ):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class UpcomingLinesParamsSubscriptionDetailsItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingLinesParamsSubscriptionDetailsItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "Invoice.UpcomingLinesParamsSubscriptionDetailsItemPriceDataRecurring"
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingLinesParamsSubscriptionDetailsItemPriceDataRecurring(
        TypedDict,
    ):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpcomingLinesParamsSubscriptionItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|Invoice.UpcomingLinesParamsSubscriptionItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        clear_usage: NotRequired[bool]
        """
        Delete all usage for a given subscription item. Allowed only when `deleted` is set to `true` and the current plan's `usage_type` is `metered`.
        """
        deleted: NotRequired[bool]
        """
        A flag that, if set to `true`, will delete the specified item.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingLinesParamsSubscriptionItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        id: NotRequired[str]
        """
        Subscription item to update.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        plan: NotRequired[str]
        """
        Plan ID for this item, as a string.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required. When changing a subscription item's price, `quantity` is set to 1 unless a `quantity` parameter is provided.
        """
        price_data: NotRequired[
            "Invoice.UpcomingLinesParamsSubscriptionItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class UpcomingLinesParamsSubscriptionItemBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class UpcomingLinesParamsSubscriptionItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingLinesParamsSubscriptionItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: (
            "Invoice.UpcomingLinesParamsSubscriptionItemPriceDataRecurring"
        )
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingLinesParamsSubscriptionItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpcomingParams(RequestOptions):
        automatic_tax: NotRequired["Invoice.UpcomingParamsAutomaticTax"]
        """
        Settings for automatic tax lookup for this invoice preview.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this phase of the subscription schedule. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        currency: NotRequired[str]
        """
        The currency to preview this invoice in. Defaults to that of `customer` if not specified.
        """
        customer: NotRequired[str]
        """
        The identifier of the customer whose upcoming invoice you'd like to retrieve. If `automatic_tax` is enabled then one of `customer`, `customer_details`, `subscription`, or `schedule` must be set.
        """
        customer_details: NotRequired["Invoice.UpcomingParamsCustomerDetails"]
        """
        Details about the customer you want to invoice or overrides for an existing customer. If `automatic_tax` is enabled then one of `customer`, `customer_details`, `subscription`, or `schedule` must be set.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingParamsDiscount]"
        ]
        """
        The coupons to redeem into discounts for the invoice preview. If not specified, inherits the discount from the subscription or customer. This works for both coupons directly applied to an invoice and coupons applied to a subscription. Pass an empty string to avoid inheriting any discounts.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_items: NotRequired[List["Invoice.UpcomingParamsInvoiceItem"]]
        """
        List of invoice items to add or update in the upcoming invoice preview.
        """
        issuer: NotRequired["Invoice.UpcomingParamsIssuer"]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account (if any) for which the funds of the invoice payment are intended. If set, the invoice will be presented with the branding and support information of the specified account. See the [Invoices with Connect](https://stripe.com/docs/billing/invoices/connect) documentation for details.
        """
        preview_mode: NotRequired[Literal["next", "recurring"]]
        """
        Customizes the types of values to include when calculating the invoice. Defaults to `next` if unspecified.
        """
        schedule: NotRequired[str]
        """
        The identifier of the schedule whose upcoming invoice you'd like to retrieve. Cannot be used with subscription or subscription fields.
        """
        schedule_details: NotRequired["Invoice.UpcomingParamsScheduleDetails"]
        """
        The schedule creation or modification params to apply as a preview. Cannot be used with `subscription` or `subscription_` prefixed fields.
        """
        subscription: NotRequired[str]
        """
        The identifier of the subscription for which you'd like to retrieve the upcoming invoice. If not provided, but a `subscription_details.items` is provided, you will preview creating a subscription with those items. If neither `subscription` nor `subscription_details.items` is provided, you will retrieve the next upcoming invoice from among the customer's subscriptions.
        """
        subscription_billing_cycle_anchor: NotRequired[
            "Literal['now', 'unchanged']|int"
        ]
        """
        For new subscriptions, a future timestamp to anchor the subscription's [billing cycle](https://stripe.com/docs/subscriptions/billing-cycle). This is used to determine the date of the first full invoice, and, for plans with `month` or `year` intervals, the day of the month for subsequent invoices. For existing subscriptions, the value can only be set to `now` or `unchanged`. This field has been deprecated and will be removed in a future API version. Use `subscription_details.billing_cycle_anchor` instead.
        """
        subscription_cancel_at: NotRequired["Literal['']|int"]
        """
        A timestamp at which the subscription should cancel. If set to a date before the current period ends, this will cause a proration if prorations have been enabled using `proration_behavior`. If set during a future period, this will always cause a proration for that period. This field has been deprecated and will be removed in a future API version. Use `subscription_details.cancel_at` instead.
        """
        subscription_cancel_at_period_end: NotRequired[bool]
        """
        Indicate whether this subscription should cancel at the end of the current period (`current_period_end`). Defaults to `false`. This field has been deprecated and will be removed in a future API version. Use `subscription_details.cancel_at_period_end` instead.
        """
        subscription_cancel_now: NotRequired[bool]
        """
        This simulates the subscription being canceled or expired immediately. This field has been deprecated and will be removed in a future API version. Use `subscription_details.cancel_now` instead.
        """
        subscription_default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with these default tax rates. The default tax rates will apply to any line item that does not have `tax_rates` set. This field has been deprecated and will be removed in a future API version. Use `subscription_details.default_tax_rates` instead.
        """
        subscription_details: NotRequired[
            "Invoice.UpcomingParamsSubscriptionDetails"
        ]
        """
        The subscription creation or modification params to apply as a preview. Cannot be used with `schedule` or `schedule_details` fields.
        """
        subscription_items: NotRequired[
            List["Invoice.UpcomingParamsSubscriptionItem"]
        ]
        """
        A list of up to 20 subscription items, each with an attached price. This field has been deprecated and will be removed in a future API version. Use `subscription_details.items` instead.
        """
        subscription_proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`. This field has been deprecated and will be removed in a future API version. Use `subscription_details.proration_behavior` instead.
        """
        subscription_proration_date: NotRequired[int]
        """
        If previewing an update to a subscription, and doing proration, `subscription_proration_date` forces the proration to be calculated as though the update was done at the specified time. The time given must be within the current subscription period and within the current phase of the schedule backing this subscription, if the schedule exists. If set, `subscription`, and one of `subscription_items`, or `subscription_trial_end` are required. Also, `subscription_proration_behavior` cannot be set to 'none'. This field has been deprecated and will be removed in a future API version. Use `subscription_details.proration_date` instead.
        """
        subscription_resume_at: NotRequired[Literal["now"]]
        """
        For paused subscriptions, setting `subscription_resume_at` to `now` will preview the invoice that will be generated if the subscription is resumed. This field has been deprecated and will be removed in a future API version. Use `subscription_details.resume_at` instead.
        """
        subscription_start_date: NotRequired[int]
        """
        Date a subscription is intended to start (can be future or past). This field has been deprecated and will be removed in a future API version. Use `subscription_details.start_date` instead.
        """
        subscription_trial_end: NotRequired["Literal['now']|int"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with that trial end. If set, one of `subscription_items` or `subscription` is required. This field has been deprecated and will be removed in a future API version. Use `subscription_details.trial_end` instead.
        """
        subscription_trial_from_plan: NotRequired[bool]
        """
        Indicates if a plan's `trial_period_days` should be applied to the subscription. Setting `subscription_trial_end` per subscription is preferred, and this defaults to `false`. Setting this flag to `true` together with `subscription_trial_end` is not allowed. See [Using trial periods on subscriptions](https://stripe.com/docs/billing/subscriptions/trials) to learn more.
        """

    class UpcomingParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Whether Stripe automatically computes tax on this invoice. Note that incompatible invoice items (invoice items with manually specified [tax rates](https://stripe.com/docs/api/tax_rates), negative amounts, or `tax_behavior=unspecified`) cannot be added to automatic tax invoices.
        """
        liability: NotRequired["Invoice.UpcomingParamsAutomaticTaxLiability"]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class UpcomingParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingParamsCustomerDetails(TypedDict):
        address: NotRequired[
            "Literal['']|Invoice.UpcomingParamsCustomerDetailsAddress"
        ]
        """
        The customer's address.
        """
        shipping: NotRequired[
            "Literal['']|Invoice.UpcomingParamsCustomerDetailsShipping"
        ]
        """
        The customer's shipping information. Appears on invoices emailed to this customer.
        """
        tax: NotRequired["Invoice.UpcomingParamsCustomerDetailsTax"]
        """
        Tax details about the customer.
        """
        tax_exempt: NotRequired[
            "Literal['']|Literal['exempt', 'none', 'reverse']"
        ]
        """
        The customer's tax exemption. One of `none`, `exempt`, or `reverse`.
        """
        tax_ids: NotRequired[
            List["Invoice.UpcomingParamsCustomerDetailsTaxId"]
        ]
        """
        The customer's tax IDs.
        """

    class UpcomingParamsCustomerDetailsAddress(TypedDict):
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

    class UpcomingParamsCustomerDetailsShipping(TypedDict):
        address: "Invoice.UpcomingParamsCustomerDetailsShippingAddress"
        """
        Customer shipping address.
        """
        name: str
        """
        Customer name.
        """
        phone: NotRequired[str]
        """
        Customer phone (including extension).
        """

    class UpcomingParamsCustomerDetailsShippingAddress(TypedDict):
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

    class UpcomingParamsCustomerDetailsTax(TypedDict):
        ip_address: NotRequired["Literal['']|str"]
        """
        A recent IP address of the customer used for tax reporting and tax location inference. Stripe recommends updating the IP address when a new PaymentMethod is attached or the address field on the customer is updated. We recommend against updating this field more frequently since it could result in unexpected tax location/reporting outcomes.
        """

    class UpcomingParamsCustomerDetailsTaxId(TypedDict):
        type: Literal[
            "ad_nrt",
            "ae_trn",
            "ar_cuit",
            "au_abn",
            "au_arn",
            "bg_uic",
            "bh_vat",
            "bo_tin",
            "br_cnpj",
            "br_cpf",
            "ca_bn",
            "ca_gst_hst",
            "ca_pst_bc",
            "ca_pst_mb",
            "ca_pst_sk",
            "ca_qst",
            "ch_uid",
            "ch_vat",
            "cl_tin",
            "cn_tin",
            "co_nit",
            "cr_tin",
            "de_stn",
            "do_rcn",
            "ec_ruc",
            "eg_tin",
            "es_cif",
            "eu_oss_vat",
            "eu_vat",
            "gb_vat",
            "ge_vat",
            "hk_br",
            "hu_tin",
            "id_npwp",
            "il_vat",
            "in_gst",
            "is_vat",
            "jp_cn",
            "jp_rn",
            "jp_trn",
            "ke_pin",
            "kr_brn",
            "kz_bin",
            "li_uid",
            "mx_rfc",
            "my_frp",
            "my_itn",
            "my_sst",
            "ng_tin",
            "no_vat",
            "no_voec",
            "nz_gst",
            "om_vat",
            "pe_ruc",
            "ph_tin",
            "ro_tin",
            "rs_pib",
            "ru_inn",
            "ru_kpp",
            "sa_vat",
            "sg_gst",
            "sg_uen",
            "si_tin",
            "sv_nit",
            "th_vat",
            "tr_tin",
            "tw_vat",
            "ua_vat",
            "us_ein",
            "uy_ruc",
            "ve_rif",
            "vn_tin",
            "za_vat",
        ]
        """
        Type of the tax ID, one of `ad_nrt`, `ae_trn`, `ar_cuit`, `au_abn`, `au_arn`, `bg_uic`, `bh_vat`, `bo_tin`, `br_cnpj`, `br_cpf`, `ca_bn`, `ca_gst_hst`, `ca_pst_bc`, `ca_pst_mb`, `ca_pst_sk`, `ca_qst`, `ch_uid`, `ch_vat`, `cl_tin`, `cn_tin`, `co_nit`, `cr_tin`, `de_stn`, `do_rcn`, `ec_ruc`, `eg_tin`, `es_cif`, `eu_oss_vat`, `eu_vat`, `gb_vat`, `ge_vat`, `hk_br`, `hu_tin`, `id_npwp`, `il_vat`, `in_gst`, `is_vat`, `jp_cn`, `jp_rn`, `jp_trn`, `ke_pin`, `kr_brn`, `kz_bin`, `li_uid`, `mx_rfc`, `my_frp`, `my_itn`, `my_sst`, `ng_tin`, `no_vat`, `no_voec`, `nz_gst`, `om_vat`, `pe_ruc`, `ph_tin`, `ro_tin`, `rs_pib`, `ru_inn`, `ru_kpp`, `sa_vat`, `sg_gst`, `sg_uen`, `si_tin`, `sv_nit`, `th_vat`, `tr_tin`, `tw_vat`, `ua_vat`, `us_ein`, `uy_ruc`, `ve_rif`, `vn_tin`, or `za_vat`
        """
        value: str
        """
        Value of the tax ID.
        """

    class UpcomingParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsInvoiceItem(TypedDict):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) of previewed invoice item.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies). Only applicable to new invoice items.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to the invoice item. The description is displayed in the invoice for easy tracking.
        """
        discountable: NotRequired[bool]
        """
        Explicitly controls whether discounts apply to this invoice item. Defaults to true, except for negative invoice items.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingParamsInvoiceItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the invoice item in the preview.
        """
        invoiceitem: NotRequired[str]
        """
        The ID of the invoice item to update in preview. If not specified, a new invoice item will be added to the preview of the upcoming invoice.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        period: NotRequired["Invoice.UpcomingParamsInvoiceItemPeriod"]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired["Invoice.UpcomingParamsInvoiceItemPriceData"]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the invoice item.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tax_code: NotRequired["Literal['']|str"]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates that apply to the item. When set, any `default_tax_rates` do not apply to this item.
        """
        unit_amount: NotRequired[int]
        """
        The integer unit amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. This unit_amount will be multiplied by the quantity to get the full amount. If you want to apply a credit to the customer's account, pass a negative unit_amount.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsInvoiceItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsInvoiceItemPeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class UpcomingParamsInvoiceItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingParamsScheduleDetails(TypedDict):
        end_behavior: NotRequired[Literal["cancel", "release"]]
        """
        Behavior of the subscription schedule and underlying subscription when it ends. Possible values are `release` or `cancel` with the default being `release`. `release` will end the subscription schedule and keep the underlying subscription running. `cancel` will end the subscription schedule and cancel the underlying subscription.
        """
        phases: NotRequired[List["Invoice.UpcomingParamsScheduleDetailsPhase"]]
        """
        List representing phases of the subscription schedule. Each phase can be customized to have different durations, plans, and coupons. If there are multiple phases, the `end_date` of one phase will always equal the `start_date` of the next phase.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        In cases where the `schedule_details` params update the currently active phase, specifies if and how to prorate at the time of the request.
        """

    class UpcomingParamsScheduleDetailsPhase(TypedDict):
        add_invoice_items: NotRequired[
            List["Invoice.UpcomingParamsScheduleDetailsPhaseAddInvoiceItem"]
        ]
        """
        A list of prices and quantities that will generate invoice items appended to the next invoice for this phase. You may pass up to 20 items.
        """
        application_fee_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. The request must be made by a platform account on a connected account in order to set an application fee percentage. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        automatic_tax: NotRequired[
            "Invoice.UpcomingParamsScheduleDetailsPhaseAutomaticTax"
        ]
        """
        Automatic tax settings for this phase.
        """
        billing_cycle_anchor: NotRequired[Literal["automatic", "phase_start"]]
        """
        Can be set to `phase_start` to set the anchor to the start of the phase or `automatic` to automatically change it if needed. Cannot be set to `phase_start` if this phase specifies a trial. For more information, see the billing cycle [documentation](https://stripe.com/docs/billing/subscriptions/billing-cycle).
        """
        billing_thresholds: NotRequired[
            "Literal['']|Invoice.UpcomingParamsScheduleDetailsPhaseBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. Pass an empty string to remove previously-defined thresholds.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay the underlying subscription at the end of each billing cycle using the default source attached to the customer. When sending an invoice, Stripe will email your customer an invoice with payment instructions and mark the subscription as `active`. Defaults to `charge_automatically` on creation.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this phase of the subscription schedule. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the subscription schedule. It must belong to the customer associated with the subscription schedule. If not set, invoices will use the default payment method in the customer's invoice settings.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will set the Subscription's [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates), which means they will be the Invoice's [`default_tax_rates`](https://stripe.com/docs/api/invoices/create#create_invoice-default_tax_rates) for any Invoices issued by the Subscription during this Phase.
        """
        description: NotRequired["Literal['']|str"]
        """
        Subscription description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingParamsScheduleDetailsPhaseDiscount]"
        ]
        """
        The coupons to redeem into discounts for the schedule phase. If not specified, inherits the discount from the subscription's customer. Pass an empty string to avoid inheriting any discounts.
        """
        end_date: NotRequired["int|Literal['now']"]
        """
        The date at which this phase of the subscription schedule ends. If set, `iterations` must not be set.
        """
        invoice_settings: NotRequired[
            "Invoice.UpcomingParamsScheduleDetailsPhaseInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        items: List["Invoice.UpcomingParamsScheduleDetailsPhaseItem"]
        """
        List of configuration items, each with an attached price, to apply during this phase of the subscription schedule.
        """
        iterations: NotRequired[int]
        """
        Integer representing the multiplier applied to the price interval. For example, `iterations=2` applied to a price with `interval=month` and `interval_count=3` results in a phase of duration `2 * 3 months = 6 months`. If set, `end_date` must not be set.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to a phase. Metadata on a schedule's phase will update the underlying subscription's `metadata` when the phase is entered, adding new keys and replacing existing keys in the subscription's `metadata`. Individual keys in the subscription's `metadata` can be unset by posting an empty value to them in the phase's `metadata`. To unset all keys in the subscription's `metadata`, update the subscription directly or unset every key individually from the phase's `metadata`.
        """
        on_behalf_of: NotRequired[str]
        """
        The account on behalf of which to charge, for each of the associated subscription's invoices.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Whether the subscription schedule will create [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when transitioning to this phase. The default value is `create_prorations`. This setting controls prorations when a phase is started asynchronously and it is persisted as a field on the phase. It's different from the request-level [proration_behavior](https://stripe.com/docs/api/subscription_schedules/update#update_subscription_schedule-proration_behavior) parameter which controls what happens if the update request affects the billing configuration of the current phase.
        """
        start_date: NotRequired["int|Literal['now']"]
        """
        The date at which this phase of the subscription schedule starts or `now`. Must be set on the first phase.
        """
        transfer_data: NotRequired[
            "Invoice.UpcomingParamsScheduleDetailsPhaseTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the associated subscription's invoices.
        """
        trial: NotRequired[bool]
        """
        If set to true the entire phase is counted as a trial and the customer will not be charged for any fees.
        """
        trial_end: NotRequired["int|Literal['now']"]
        """
        Sets the phase to trialing from the start date to this date. Must be before the phase end date, can not be combined with `trial`
        """

    class UpcomingParamsScheduleDetailsPhaseAddInvoiceItem(TypedDict):
        discounts: NotRequired[
            List[
                "Invoice.UpcomingParamsScheduleDetailsPhaseAddInvoiceItemDiscount"
            ]
        ]
        """
        The coupons to redeem into discounts for the item.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "Invoice.UpcomingParamsScheduleDetailsPhaseAddInvoiceItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item. Defaults to 1.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the item. When set, the `default_tax_rates` do not apply to this item.
        """

    class UpcomingParamsScheduleDetailsPhaseAddInvoiceItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsScheduleDetailsPhaseAddInvoiceItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsScheduleDetailsPhaseAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
        """
        liability: NotRequired[
            "Invoice.UpcomingParamsScheduleDetailsPhaseAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class UpcomingParamsScheduleDetailsPhaseAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingParamsScheduleDetailsPhaseBillingThresholds(TypedDict):
        amount_gte: NotRequired[int]
        """
        Monetary threshold that triggers the subscription to advance to a new billing period
        """
        reset_billing_cycle_anchor: NotRequired[bool]
        """
        Indicates if the `billing_cycle_anchor` should be reset when a threshold is reached. If true, `billing_cycle_anchor` will be updated to the date/time the threshold was last reached; otherwise, the value will remain unchanged.
        """

    class UpcomingParamsScheduleDetailsPhaseDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsScheduleDetailsPhaseInvoiceSettings(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with this phase of the subscription schedule. Will be set on invoices generated by this phase of the subscription schedule.
        """
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay invoices generated by this subscription schedule. This value will be `null` for subscription schedules where `billing=charge_automatically`.
        """
        issuer: NotRequired[
            "Invoice.UpcomingParamsScheduleDetailsPhaseInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class UpcomingParamsScheduleDetailsPhaseInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpcomingParamsScheduleDetailsPhaseItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|Invoice.UpcomingParamsScheduleDetailsPhaseItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingParamsScheduleDetailsPhaseItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to a configuration item. Metadata on a configuration item will update the underlying subscription item's `metadata` when the phase is entered, adding new keys and replacing existing keys. Individual keys in the subscription item's `metadata` can be unset by posting an empty value to them in the configuration item's `metadata`. To unset all keys in the subscription item's `metadata`, update the subscription item directly or unset every key individually from the configuration item's `metadata`.
        """
        plan: NotRequired[str]
        """
        The plan ID to subscribe to. You may specify the same ID in `plan` and `price`.
        """
        price: NotRequired[str]
        """
        The ID of the price object.
        """
        price_data: NotRequired[
            "Invoice.UpcomingParamsScheduleDetailsPhaseItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline.
        """
        quantity: NotRequired[int]
        """
        Quantity for the given price. Can be set only if the price's `usage_type` is `licensed` and not `metered`.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class UpcomingParamsScheduleDetailsPhaseItemBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class UpcomingParamsScheduleDetailsPhaseItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsScheduleDetailsPhaseItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: (
            "Invoice.UpcomingParamsScheduleDetailsPhaseItemPriceDataRecurring"
        )
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsScheduleDetailsPhaseItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpcomingParamsScheduleDetailsPhaseTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class UpcomingParamsSubscriptionDetails(TypedDict):
        billing_cycle_anchor: NotRequired["Literal['now', 'unchanged']|int"]
        """
        For new subscriptions, a future timestamp to anchor the subscription's [billing cycle](https://stripe.com/docs/subscriptions/billing-cycle). This is used to determine the date of the first full invoice, and, for plans with `month` or `year` intervals, the day of the month for subsequent invoices. For existing subscriptions, the value can only be set to `now` or `unchanged`.
        """
        cancel_at: NotRequired["Literal['']|int"]
        """
        A timestamp at which the subscription should cancel. If set to a date before the current period ends, this will cause a proration if prorations have been enabled using `proration_behavior`. If set during a future period, this will always cause a proration for that period.
        """
        cancel_at_period_end: NotRequired[bool]
        """
        Indicate whether this subscription should cancel at the end of the current period (`current_period_end`). Defaults to `false`.
        """
        cancel_now: NotRequired[bool]
        """
        This simulates the subscription being canceled or expired immediately.
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with these default tax rates. The default tax rates will apply to any line item that does not have `tax_rates` set.
        """
        items: NotRequired[
            List["Invoice.UpcomingParamsSubscriptionDetailsItem"]
        ]
        """
        A list of up to 20 subscription items, each with an attached price.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`.
        """
        proration_date: NotRequired[int]
        """
        If previewing an update to a subscription, and doing proration, `subscription_details.proration_date` forces the proration to be calculated as though the update was done at the specified time. The time given must be within the current subscription period and within the current phase of the schedule backing this subscription, if the schedule exists. If set, `subscription`, and one of `subscription_details.items`, or `subscription_details.trial_end` are required. Also, `subscription_details.proration_behavior` cannot be set to 'none'.
        """
        resume_at: NotRequired[Literal["now"]]
        """
        For paused subscriptions, setting `subscription_details.resume_at` to `now` will preview the invoice that will be generated if the subscription is resumed.
        """
        start_date: NotRequired[int]
        """
        Date a subscription is intended to start (can be future or past).
        """
        trial_end: NotRequired["Literal['now']|int"]
        """
        If provided, the invoice returned will preview updating or creating a subscription with that trial end. If set, one of `subscription_details.items` or `subscription` is required.
        """

    class UpcomingParamsSubscriptionDetailsItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|Invoice.UpcomingParamsSubscriptionDetailsItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        clear_usage: NotRequired[bool]
        """
        Delete all usage for a given subscription item. Allowed only when `deleted` is set to `true` and the current plan's `usage_type` is `metered`.
        """
        deleted: NotRequired[bool]
        """
        A flag that, if set to `true`, will delete the specified item.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingParamsSubscriptionDetailsItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        id: NotRequired[str]
        """
        Subscription item to update.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        plan: NotRequired[str]
        """
        Plan ID for this item, as a string.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required. When changing a subscription item's price, `quantity` is set to 1 unless a `quantity` parameter is provided.
        """
        price_data: NotRequired[
            "Invoice.UpcomingParamsSubscriptionDetailsItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class UpcomingParamsSubscriptionDetailsItemBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class UpcomingParamsSubscriptionDetailsItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsSubscriptionDetailsItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: (
            "Invoice.UpcomingParamsSubscriptionDetailsItemPriceDataRecurring"
        )
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsSubscriptionDetailsItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpcomingParamsSubscriptionItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|Invoice.UpcomingParamsSubscriptionItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        clear_usage: NotRequired[bool]
        """
        Delete all usage for a given subscription item. Allowed only when `deleted` is set to `true` and the current plan's `usage_type` is `metered`.
        """
        deleted: NotRequired[bool]
        """
        A flag that, if set to `true`, will delete the specified item.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpcomingParamsSubscriptionItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        id: NotRequired[str]
        """
        Subscription item to update.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        plan: NotRequired[str]
        """
        Plan ID for this item, as a string.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required. When changing a subscription item's price, `quantity` is set to 1 unless a `quantity` parameter is provided.
        """
        price_data: NotRequired[
            "Invoice.UpcomingParamsSubscriptionItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class UpcomingParamsSubscriptionItemBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class UpcomingParamsSubscriptionItemDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpcomingParamsSubscriptionItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "Invoice.UpcomingParamsSubscriptionItemPriceDataRecurring"
        """
        The recurring components of a price such as `interval` and `interval_count`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpcomingParamsSubscriptionItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpdateLinesParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`. For [type=subscription](https://stripe.com/docs/api/invoices/line_item#invoice_line_item_object-type) line items, the incoming metadata specified on the request is directly used to set this value, in contrast to [type=invoiceitem](api/invoices/line_item#invoice_line_item_object-type) line items, where any existing metadata on the invoice line is merged with the incoming data.
        """
        lines: List["Invoice.UpdateLinesParamsLine"]
        """
        The line items to update.
        """

    class UpdateLinesParamsLine(TypedDict):
        amount: NotRequired[int]
        """
        The integer amount in cents (or local equivalent) of the charge to be applied to the upcoming invoice. If you want to apply a credit to the customer's account, pass a negative amount.
        """
        description: NotRequired[str]
        """
        An arbitrary string which you can attach to the invoice item. The description is displayed in the invoice for easy tracking.
        """
        discountable: NotRequired[bool]
        """
        Controls whether discounts apply to this line item. Defaults to false for prorations or negative line items, and true for all other line items. Cannot be set to true for prorations.
        """
        discounts: NotRequired[
            "Literal['']|List[Invoice.UpdateLinesParamsLineDiscount]"
        ]
        """
        The coupons, promotion codes & existing discounts which apply to the line item. Item discounts are applied before invoice discounts. Pass an empty string to remove previously-defined discounts.
        """
        id: str
        """
        ID of an existing line item on the invoice.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`. For [type=subscription](https://stripe.com/docs/api/invoices/line_item#invoice_line_item_object-type) line items, the incoming metadata specified on the request is directly used to set this value, in contrast to [type=invoiceitem](api/invoices/line_item#invoice_line_item_object-type) line items, where any existing metadata on the invoice line is merged with the incoming data.
        """
        period: NotRequired["Invoice.UpdateLinesParamsLinePeriod"]
        """
        The period associated with this invoice item. When set to different values, the period will be rendered on the invoice. If you have [Stripe Revenue Recognition](https://stripe.com/docs/revenue-recognition) enabled, the period will be used to recognize and defer revenue. See the [Revenue Recognition documentation](https://stripe.com/docs/revenue-recognition/methodology/subscriptions-and-invoicing) for details.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired["Invoice.UpdateLinesParamsLinePriceData"]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        Non-negative integer. The quantity of units for the line item.
        """
        tax_amounts: NotRequired[
            "Literal['']|List[Invoice.UpdateLinesParamsLineTaxAmount]"
        ]
        """
        A list of up to 10 tax amounts for this line item. This can be useful if you calculate taxes on your own or use a third-party to calculate them. You cannot set tax amounts if any line item has [tax_rates](https://stripe.com/docs/api/invoices/line_item#invoice_line_item_object-tax_rates) or if the invoice has [default_tax_rates](https://stripe.com/docs/api/invoices/object#invoice_object-default_tax_rates) or uses [automatic tax](https://stripe.com/docs/tax/invoicing). Pass an empty string to remove previously defined tax amounts.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates which apply to the line item. When set, the `default_tax_rates` on the invoice do not apply to this line item. Pass an empty string to remove previously-defined tax rates.
        """

    class UpdateLinesParamsLineDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        ID of the coupon to create a new discount for.
        """
        discount: NotRequired[str]
        """
        ID of an existing discount on the object (or one of its ancestors) to reuse.
        """
        promotion_code: NotRequired[str]
        """
        ID of the promotion code to create a new discount for.
        """

    class UpdateLinesParamsLinePeriod(TypedDict):
        end: int
        """
        The end of the period, which must be greater than or equal to the start. This value is inclusive.
        """
        start: int
        """
        The start of the period. This value is inclusive.
        """

    class UpdateLinesParamsLinePriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: NotRequired[str]
        """
        The ID of the product that this price will belong to. One of `product` or `product_data` is required.
        """
        product_data: NotRequired[
            "Invoice.UpdateLinesParamsLinePriceDataProductData"
        ]
        """
        Data used to generate a new product object inline. One of `product` or `product_data` is required.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        unit_amount: NotRequired[int]
        """
        A non-negative integer in cents (or local equivalent) representing how much to charge. One of `unit_amount` or `unit_amount_decimal` is required.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class UpdateLinesParamsLinePriceDataProductData(TypedDict):
        description: NotRequired[str]
        """
        The product's description, meant to be displayable to the customer. Use this field to optionally store a long form explanation of the product being sold for your own rendering purposes.
        """
        images: NotRequired[List[str]]
        """
        A list of up to 8 URLs of images for this product, meant to be displayable to the customer.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: str
        """
        The product's name, meant to be displayable to the customer.
        """
        tax_code: NotRequired[str]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """

    class UpdateLinesParamsLineTaxAmount(TypedDict):
        amount: int
        """
        The amount, in cents (or local equivalent), of the tax.
        """
        tax_rate_data: "Invoice.UpdateLinesParamsLineTaxAmountTaxRateData"
        """
        Data to find or create a TaxRate object.

        Stripe automatically creates or reuses a TaxRate object for each tax amount. If the `tax_rate_data` exactly matches a previous value, Stripe will reuse the TaxRate object. TaxRate objects created automatically by Stripe are immediately archived, do not appear in the line item's `tax_rates`, and cannot be directly added to invoices, payments, or line items.
        """
        taxable_amount: int
        """
        The amount on which tax is calculated, in cents (or local equivalent).
        """

    class UpdateLinesParamsLineTaxAmountTaxRateData(TypedDict):
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the tax rate for your internal use only. It will not be visible to your customers.
        """
        display_name: str
        """
        The display name of the tax rate, which will be shown to users.
        """
        inclusive: bool
        """
        This specifies if the tax rate is inclusive or exclusive.
        """
        jurisdiction: NotRequired[str]
        """
        The jurisdiction for the tax rate. You can use this label field for tax reporting purposes. It also appears on your customer's invoice.
        """
        percentage: float
        """
        The statutory tax rate percent. This field accepts decimal values between 0 and 100 inclusive with at most 4 decimal places. To accommodate fixed-amount taxes, set the percentage to zero. Stripe will not display zero percentages on the invoice unless the `amount` of the tax is also zero.
        """
        state: NotRequired[str]
        """
        [ISO 3166-2 subdivision code](https://en.wikipedia.org/wiki/ISO_3166-2:US), without country prefix. For example, "NY" for New York, United States.
        """
        tax_type: NotRequired[
            Literal[
                "amusement_tax",
                "communications_tax",
                "gst",
                "hst",
                "igst",
                "jct",
                "lease_tax",
                "pst",
                "qst",
                "rst",
                "sales_tax",
                "vat",
            ]
        ]
        """
        The high-level tax type, such as `vat` or `sales_tax`.
        """

    class VoidInvoiceParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    account_country: Optional[str]
    """
    The country of the business associated with this invoice, most often the business creating the invoice.
    """
    account_name: Optional[str]
    """
    The public name of the business associated with this invoice, most often the business creating the invoice.
    """
    account_tax_ids: Optional[List[ExpandableField["TaxId"]]]
    """
    The account tax IDs associated with the invoice. Only editable when the invoice is a draft.
    """
    amount_due: int
    """
    Final amount due at this time for this invoice. If the invoice's total is smaller than the minimum charge amount, for example, or if there is account credit that can be applied to the invoice, the `amount_due` may be 0. If there is a positive `starting_balance` for the invoice (the customer owes money), the `amount_due` will also take that into account. The charge that gets generated for the invoice will be for the amount specified in `amount_due`.
    """
    amount_paid: int
    """
    The amount, in cents (or local equivalent), that was paid.
    """
    amount_remaining: int
    """
    The difference between amount_due and amount_paid, in cents (or local equivalent).
    """
    amount_shipping: int
    """
    This is the sum of all the shipping amounts.
    """
    application: Optional[ExpandableField["Application"]]
    """
    ID of the Connect Application that created the invoice.
    """
    application_fee_amount: Optional[int]
    """
    The fee in cents (or local equivalent) that will be applied to the invoice and transferred to the application owner's Stripe account when the invoice is paid.
    """
    attempt_count: int
    """
    Number of payment attempts made for this invoice, from the perspective of the payment retry schedule. Any payment attempt counts as the first attempt, and subsequently only automatic retries increment the attempt count. In other words, manual payment attempts after the first attempt do not affect the retry schedule. If a failure is returned with a non-retryable return code, the invoice can no longer be retried unless a new payment method is obtained. Retries will continue to be scheduled, and attempt_count will continue to increment, but retries will only be executed if a new payment method is obtained.
    """
    attempted: bool
    """
    Whether an attempt has been made to pay the invoice. An invoice is not attempted until 1 hour after the `invoice.created` webhook, for example, so you might not want to display that invoice as unpaid to your users.
    """
    auto_advance: Optional[bool]
    """
    Controls whether Stripe performs [automatic collection](https://stripe.com/docs/invoicing/integration/automatic-advancement-collection) of the invoice. If `false`, the invoice's state doesn't automatically advance without an explicit action.
    """
    automatic_tax: AutomaticTax
    billing_reason: Optional[
        Literal[
            "automatic_pending_invoice_item_invoice",
            "manual",
            "quote_accept",
            "subscription",
            "subscription_create",
            "subscription_cycle",
            "subscription_threshold",
            "subscription_update",
            "upcoming",
        ]
    ]
    """
    Indicates the reason why the invoice was created.

    * `manual`: Unrelated to a subscription, for example, created via the invoice editor.
    * `subscription`: No longer in use. Applies to subscriptions from before May 2018 where no distinction was made between updates, cycles, and thresholds.
    * `subscription_create`: A new subscription was created.
    * `subscription_cycle`: A subscription advanced into a new period.
    * `subscription_threshold`: A subscription reached a billing threshold.
    * `subscription_update`: A subscription was updated.
    * `upcoming`: Reserved for simulated invoices, per the upcoming invoice endpoint.
    """
    charge: Optional[ExpandableField["Charge"]]
    """
    ID of the latest charge generated for this invoice, if any.
    """
    collection_method: Literal["charge_automatically", "send_invoice"]
    """
    Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay this invoice using the default source attached to the customer. When sending an invoice, Stripe will email this invoice to the customer with payment instructions.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    custom_fields: Optional[List[CustomField]]
    """
    Custom fields displayed on the invoice.
    """
    customer: Optional[ExpandableField["Customer"]]
    """
    The ID of the customer who will be billed.
    """
    customer_address: Optional[CustomerAddress]
    """
    The customer's address. Until the invoice is finalized, this field will equal `customer.address`. Once the invoice is finalized, this field will no longer be updated.
    """
    customer_email: Optional[str]
    """
    The customer's email. Until the invoice is finalized, this field will equal `customer.email`. Once the invoice is finalized, this field will no longer be updated.
    """
    customer_name: Optional[str]
    """
    The customer's name. Until the invoice is finalized, this field will equal `customer.name`. Once the invoice is finalized, this field will no longer be updated.
    """
    customer_phone: Optional[str]
    """
    The customer's phone number. Until the invoice is finalized, this field will equal `customer.phone`. Once the invoice is finalized, this field will no longer be updated.
    """
    customer_shipping: Optional[CustomerShipping]
    """
    The customer's shipping information. Until the invoice is finalized, this field will equal `customer.shipping`. Once the invoice is finalized, this field will no longer be updated.
    """
    customer_tax_exempt: Optional[Literal["exempt", "none", "reverse"]]
    """
    The customer's tax exempt status. Until the invoice is finalized, this field will equal `customer.tax_exempt`. Once the invoice is finalized, this field will no longer be updated.
    """
    customer_tax_ids: Optional[List[CustomerTaxId]]
    """
    The customer's tax IDs. Until the invoice is finalized, this field will contain the same tax IDs as `customer.tax_ids`. Once the invoice is finalized, this field will no longer be updated.
    """
    default_payment_method: Optional[ExpandableField["PaymentMethod"]]
    """
    ID of the default payment method for the invoice. It must belong to the customer associated with the invoice. If not set, defaults to the subscription's default payment method, if any, or to the default payment method in the customer's invoice settings.
    """
    default_source: Optional[
        ExpandableField[
            Union["Account", "BankAccount", "CardResource", "Source"]
        ]
    ]
    """
    ID of the default payment source for the invoice. It must belong to the customer associated with the invoice and be in a chargeable state. If not set, defaults to the subscription's default source, if any, or to the customer's default source.
    """
    default_tax_rates: List["TaxRate"]
    """
    The tax rates applied to this invoice, if any.
    """
    description: Optional[str]
    """
    An arbitrary string attached to the object. Often useful for displaying to users. Referenced as 'memo' in the Dashboard.
    """
    discount: Optional["Discount"]
    """
    Describes the current discount applied to this invoice, if there is one. Not populated if there are multiple discounts.
    """
    discounts: List[ExpandableField["Discount"]]
    """
    The discounts applied to the invoice. Line item discounts are applied before invoice discounts. Use `expand[]=discounts` to expand each discount.
    """
    due_date: Optional[int]
    """
    The date on which payment for this invoice is due. This value will be `null` for invoices where `collection_method=charge_automatically`.
    """
    effective_at: Optional[int]
    """
    The date when this invoice is in effect. Same as `finalized_at` unless overwritten. When defined, this value replaces the system-generated 'Date of issue' printed on the invoice PDF and receipt.
    """
    ending_balance: Optional[int]
    """
    Ending customer balance after the invoice is finalized. Invoices are finalized approximately an hour after successful webhook delivery or when payment collection is attempted for the invoice. If the invoice has not been finalized yet, this will be null.
    """
    footer: Optional[str]
    """
    Footer displayed on the invoice.
    """
    from_invoice: Optional[FromInvoice]
    """
    Details of the invoice that was cloned. See the [revision documentation](https://stripe.com/docs/invoicing/invoice-revisions) for more details.
    """
    hosted_invoice_url: Optional[str]
    """
    The URL for the hosted invoice page, which allows customers to view and pay an invoice. If the invoice has not been finalized yet, this will be null.
    """
    id: Optional[str]
    """
    Unique identifier for the object. This property is always present unless the invoice is an upcoming invoice. See [Retrieve an upcoming invoice](https://stripe.com/docs/api/invoices/upcoming) for more details.
    """
    invoice_pdf: Optional[str]
    """
    The link to download the PDF for the invoice. If the invoice has not been finalized yet, this will be null.
    """
    issuer: Issuer
    last_finalization_error: Optional[LastFinalizationError]
    """
    The error encountered during the previous attempt to finalize the invoice. This field is cleared when the invoice is successfully finalized.
    """
    latest_revision: Optional[ExpandableField["Invoice"]]
    """
    The ID of the most recent non-draft revision of this invoice
    """
    lines: ListObject["InvoiceLineItem"]
    """
    The individual line items that make up the invoice. `lines` is sorted as follows: (1) pending invoice items (including prorations) in reverse chronological order, (2) subscription items in reverse chronological order, and (3) invoice items added after invoice creation in chronological order.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    metadata: Optional[Dict[str, str]]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    next_payment_attempt: Optional[int]
    """
    The time at which payment will next be attempted. This value will be `null` for invoices where `collection_method=send_invoice`.
    """
    number: Optional[str]
    """
    A unique, identifying string that appears on emails sent to the customer for this invoice. This starts with the customer's unique invoice_prefix if it is specified.
    """
    object: Literal["invoice"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    on_behalf_of: Optional[ExpandableField["Account"]]
    """
    The account (if any) for which the funds of the invoice payment are intended. If set, the invoice will be presented with the branding and support information of the specified account. See the [Invoices with Connect](https://stripe.com/docs/billing/invoices/connect) documentation for details.
    """
    paid: bool
    """
    Whether payment was successfully collected for this invoice. An invoice can be paid (most commonly) with a charge or with credit from the customer's account balance.
    """
    paid_out_of_band: bool
    """
    Returns true if the invoice was manually marked paid, returns false if the invoice hasn't been paid yet or was paid on Stripe.
    """
    payment_intent: Optional[ExpandableField["PaymentIntent"]]
    """
    The PaymentIntent associated with this invoice. The PaymentIntent is generated when the invoice is finalized, and can then be used to pay the invoice. Note that voiding an invoice will cancel the PaymentIntent.
    """
    payment_settings: PaymentSettings
    period_end: int
    """
    End of the usage period during which invoice items were added to this invoice. This looks back one period for a subscription invoice. Use the [line item period](https://stripe.com/api/invoices/line_item#invoice_line_item_object-period) to get the service period for each price.
    """
    period_start: int
    """
    Start of the usage period during which invoice items were added to this invoice. This looks back one period for a subscription invoice. Use the [line item period](https://stripe.com/api/invoices/line_item#invoice_line_item_object-period) to get the service period for each price.
    """
    post_payment_credit_notes_amount: int
    """
    Total amount of all post-payment credit notes issued for this invoice.
    """
    pre_payment_credit_notes_amount: int
    """
    Total amount of all pre-payment credit notes issued for this invoice.
    """
    quote: Optional[ExpandableField["Quote"]]
    """
    The quote this invoice was generated from.
    """
    receipt_number: Optional[str]
    """
    This is the transaction number that appears on email receipts sent for this invoice.
    """
    rendering: Optional[Rendering]
    """
    The rendering-related settings that control how the invoice is displayed on customer-facing surfaces such as PDF and Hosted Invoice Page.
    """
    shipping_cost: Optional[ShippingCost]
    """
    The details of the cost of shipping, including the ShippingRate applied on the invoice.
    """
    shipping_details: Optional[ShippingDetails]
    """
    Shipping details for the invoice. The Invoice PDF will use the `shipping_details` value if it is set, otherwise the PDF will render the shipping address from the customer.
    """
    starting_balance: int
    """
    Starting customer balance before the invoice is finalized. If the invoice has not been finalized yet, this will be the current customer balance. For revision invoices, this also includes any customer balance that was applied to the original invoice.
    """
    statement_descriptor: Optional[str]
    """
    Extra information about an invoice for the customer's credit card statement.
    """
    status: Optional[Literal["draft", "open", "paid", "uncollectible", "void"]]
    """
    The status of the invoice, one of `draft`, `open`, `paid`, `uncollectible`, or `void`. [Learn more](https://stripe.com/docs/billing/invoices/workflow#workflow-overview)
    """
    status_transitions: StatusTransitions
    subscription: Optional[ExpandableField["Subscription"]]
    """
    The subscription that this invoice was prepared for, if any.
    """
    subscription_details: Optional[SubscriptionDetails]
    """
    Details about the subscription that created this invoice.
    """
    subscription_proration_date: Optional[int]
    """
    Only set for upcoming invoices that preview prorations. The time used to calculate prorations.
    """
    subtotal: int
    """
    Total of all subscriptions, invoice items, and prorations on the invoice before any invoice level discount or exclusive tax is applied. Item discounts are already incorporated
    """
    subtotal_excluding_tax: Optional[int]
    """
    The integer amount in cents (or local equivalent) representing the subtotal of the invoice before any invoice level discount or tax is applied. Item discounts are already incorporated
    """
    tax: Optional[int]
    """
    The amount of tax on this invoice. This is the sum of all the tax amounts on this invoice.
    """
    test_clock: Optional[ExpandableField["TestClock"]]
    """
    ID of the test clock this invoice belongs to.
    """
    threshold_reason: Optional[ThresholdReason]
    total: int
    """
    Total after discounts and taxes.
    """
    total_discount_amounts: Optional[List[TotalDiscountAmount]]
    """
    The aggregate amounts calculated per discount across all line items.
    """
    total_excluding_tax: Optional[int]
    """
    The integer amount in cents (or local equivalent) representing the total amount of the invoice including all discounts but excluding all tax.
    """
    total_tax_amounts: List[TotalTaxAmount]
    """
    The aggregate amounts calculated per tax rate for all line items.
    """
    transfer_data: Optional[TransferData]
    """
    The account (if any) the payment will be attributed to for tax reporting, and where funds from the payment will be transferred to for the invoice.
    """
    webhooks_delivered_at: Optional[int]
    """
    Invoices are automatically paid or sent 1 hour after webhooks are delivered, or until all webhook delivery attempts have [been exhausted](https://stripe.com/docs/billing/webhooks#understand). This field tracks the time when webhooks for this invoice were successfully delivered. If the invoice had no webhooks to deliver, this will be set while the invoice is being created.
    """
    deleted: Optional[Literal[True]]
    """
    Always true for a deleted object
    """

    @classmethod
    def _cls_add_lines(
        cls, invoice: str, **params: Unpack["Invoice.AddLinesParams"]
    ) -> "Invoice":
        """
        Adds multiple line items to an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            cls._static_request(
                "post",
                "/v1/invoices/{invoice}/add_lines".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def add_lines(
        invoice: str, **params: Unpack["Invoice.AddLinesParams"]
    ) -> "Invoice":
        """
        Adds multiple line items to an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @overload
    def add_lines(
        self, **params: Unpack["Invoice.AddLinesParams"]
    ) -> "Invoice":
        """
        Adds multiple line items to an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @class_method_variant("_cls_add_lines")
    def add_lines(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.AddLinesParams"]
    ) -> "Invoice":
        """
        Adds multiple line items to an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            self._request(
                "post",
                "/v1/invoices/{invoice}/add_lines".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_add_lines_async(
        cls, invoice: str, **params: Unpack["Invoice.AddLinesParams"]
    ) -> "Invoice":
        """
        Adds multiple line items to an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            await cls._static_request_async(
                "post",
                "/v1/invoices/{invoice}/add_lines".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def add_lines_async(
        invoice: str, **params: Unpack["Invoice.AddLinesParams"]
    ) -> "Invoice":
        """
        Adds multiple line items to an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @overload
    async def add_lines_async(
        self, **params: Unpack["Invoice.AddLinesParams"]
    ) -> "Invoice":
        """
        Adds multiple line items to an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @class_method_variant("_cls_add_lines_async")
    async def add_lines_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.AddLinesParams"]
    ) -> "Invoice":
        """
        Adds multiple line items to an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/add_lines".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def create(cls, **params: Unpack["Invoice.CreateParams"]) -> "Invoice":
        """
        This endpoint creates a draft invoice for a given customer. The invoice remains a draft until you [finalize the invoice, which allows you to [pay](#pay_invoice) or <a href="#send_invoice">send](https://stripe.com/docs/api#finalize_invoice) the invoice to your customers.
        """
        return cast(
            "Invoice",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Invoice.CreateParams"]
    ) -> "Invoice":
        """
        This endpoint creates a draft invoice for a given customer. The invoice remains a draft until you [finalize the invoice, which allows you to [pay](#pay_invoice) or <a href="#send_invoice">send](https://stripe.com/docs/api#finalize_invoice) the invoice to your customers.
        """
        return cast(
            "Invoice",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def create_preview(
        cls, **params: Unpack["Invoice.CreatePreviewParams"]
    ) -> "Invoice":
        """
        At any time, you can preview the upcoming invoice for a customer. This will show you all the charges that are pending, including subscription renewal charges, invoice item charges, etc. It will also show you any discounts that are applicable to the invoice.

        Note that when you are viewing an upcoming invoice, you are simply viewing a preview – the invoice has not yet been created. As such, the upcoming invoice will not show up in invoice listing calls, and you cannot use the API to pay or edit the invoice. If you want to change the amount that your customer will be billed, you can add, remove, or update pending invoice items, or update the customer's discount.

        You can preview the effects of updating a subscription, including a preview of what proration will take place. To ensure that the actual proration is calculated exactly the same as the previewed proration, you should pass the subscription_details.proration_date parameter when doing the actual subscription update. The recommended way to get only the prorations being previewed is to consider only proration line items where period[start] is equal to the subscription_details.proration_date value passed in the request.

        Note: Currency conversion calculations use the latest exchange rates. Exchange rates may vary between the time of the preview and the time of the actual invoice creation. [Learn more](https://docs.stripe.com/currencies/conversions)
        """
        return cast(
            "Invoice",
            cls._static_request(
                "post",
                "/v1/invoices/create_preview",
                params=params,
            ),
        )

    @classmethod
    async def create_preview_async(
        cls, **params: Unpack["Invoice.CreatePreviewParams"]
    ) -> "Invoice":
        """
        At any time, you can preview the upcoming invoice for a customer. This will show you all the charges that are pending, including subscription renewal charges, invoice item charges, etc. It will also show you any discounts that are applicable to the invoice.

        Note that when you are viewing an upcoming invoice, you are simply viewing a preview – the invoice has not yet been created. As such, the upcoming invoice will not show up in invoice listing calls, and you cannot use the API to pay or edit the invoice. If you want to change the amount that your customer will be billed, you can add, remove, or update pending invoice items, or update the customer's discount.

        You can preview the effects of updating a subscription, including a preview of what proration will take place. To ensure that the actual proration is calculated exactly the same as the previewed proration, you should pass the subscription_details.proration_date parameter when doing the actual subscription update. The recommended way to get only the prorations being previewed is to consider only proration line items where period[start] is equal to the subscription_details.proration_date value passed in the request.

        Note: Currency conversion calculations use the latest exchange rates. Exchange rates may vary between the time of the preview and the time of the actual invoice creation. [Learn more](https://docs.stripe.com/currencies/conversions)
        """
        return cast(
            "Invoice",
            await cls._static_request_async(
                "post",
                "/v1/invoices/create_preview",
                params=params,
            ),
        )

    @classmethod
    def _cls_delete(
        cls, sid: str, **params: Unpack["Invoice.DeleteParams"]
    ) -> "Invoice":
        """
        Permanently deletes a one-off invoice draft. This cannot be undone. Attempts to delete invoices that are no longer in a draft state will fail; once an invoice has been finalized or if an invoice is for a subscription, it must be [voided](https://stripe.com/docs/api#void_invoice).
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "Invoice",
            cls._static_request(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    def delete(
        sid: str, **params: Unpack["Invoice.DeleteParams"]
    ) -> "Invoice":
        """
        Permanently deletes a one-off invoice draft. This cannot be undone. Attempts to delete invoices that are no longer in a draft state will fail; once an invoice has been finalized or if an invoice is for a subscription, it must be [voided](https://stripe.com/docs/api#void_invoice).
        """
        ...

    @overload
    def delete(self, **params: Unpack["Invoice.DeleteParams"]) -> "Invoice":
        """
        Permanently deletes a one-off invoice draft. This cannot be undone. Attempts to delete invoices that are no longer in a draft state will fail; once an invoice has been finalized or if an invoice is for a subscription, it must be [voided](https://stripe.com/docs/api#void_invoice).
        """
        ...

    @class_method_variant("_cls_delete")
    def delete(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.DeleteParams"]
    ) -> "Invoice":
        """
        Permanently deletes a one-off invoice draft. This cannot be undone. Attempts to delete invoices that are no longer in a draft state will fail; once an invoice has been finalized or if an invoice is for a subscription, it must be [voided](https://stripe.com/docs/api#void_invoice).
        """
        return self._request_and_refresh(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    async def _cls_delete_async(
        cls, sid: str, **params: Unpack["Invoice.DeleteParams"]
    ) -> "Invoice":
        """
        Permanently deletes a one-off invoice draft. This cannot be undone. Attempts to delete invoices that are no longer in a draft state will fail; once an invoice has been finalized or if an invoice is for a subscription, it must be [voided](https://stripe.com/docs/api#void_invoice).
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "Invoice",
            await cls._static_request_async(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def delete_async(
        sid: str, **params: Unpack["Invoice.DeleteParams"]
    ) -> "Invoice":
        """
        Permanently deletes a one-off invoice draft. This cannot be undone. Attempts to delete invoices that are no longer in a draft state will fail; once an invoice has been finalized or if an invoice is for a subscription, it must be [voided](https://stripe.com/docs/api#void_invoice).
        """
        ...

    @overload
    async def delete_async(
        self, **params: Unpack["Invoice.DeleteParams"]
    ) -> "Invoice":
        """
        Permanently deletes a one-off invoice draft. This cannot be undone. Attempts to delete invoices that are no longer in a draft state will fail; once an invoice has been finalized or if an invoice is for a subscription, it must be [voided](https://stripe.com/docs/api#void_invoice).
        """
        ...

    @class_method_variant("_cls_delete_async")
    async def delete_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.DeleteParams"]
    ) -> "Invoice":
        """
        Permanently deletes a one-off invoice draft. This cannot be undone. Attempts to delete invoices that are no longer in a draft state will fail; once an invoice has been finalized or if an invoice is for a subscription, it must be [voided](https://stripe.com/docs/api#void_invoice).
        """
        return await self._request_and_refresh_async(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    def _cls_finalize_invoice(
        cls, invoice: str, **params: Unpack["Invoice.FinalizeInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe automatically finalizes drafts before sending and attempting payment on invoices. However, if you'd like to finalize a draft invoice manually, you can do so using this method.
        """
        return cast(
            "Invoice",
            cls._static_request(
                "post",
                "/v1/invoices/{invoice}/finalize".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def finalize_invoice(
        invoice: str, **params: Unpack["Invoice.FinalizeInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe automatically finalizes drafts before sending and attempting payment on invoices. However, if you'd like to finalize a draft invoice manually, you can do so using this method.
        """
        ...

    @overload
    def finalize_invoice(
        self, **params: Unpack["Invoice.FinalizeInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe automatically finalizes drafts before sending and attempting payment on invoices. However, if you'd like to finalize a draft invoice manually, you can do so using this method.
        """
        ...

    @class_method_variant("_cls_finalize_invoice")
    def finalize_invoice(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.FinalizeInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe automatically finalizes drafts before sending and attempting payment on invoices. However, if you'd like to finalize a draft invoice manually, you can do so using this method.
        """
        return cast(
            "Invoice",
            self._request(
                "post",
                "/v1/invoices/{invoice}/finalize".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_finalize_invoice_async(
        cls, invoice: str, **params: Unpack["Invoice.FinalizeInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe automatically finalizes drafts before sending and attempting payment on invoices. However, if you'd like to finalize a draft invoice manually, you can do so using this method.
        """
        return cast(
            "Invoice",
            await cls._static_request_async(
                "post",
                "/v1/invoices/{invoice}/finalize".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def finalize_invoice_async(
        invoice: str, **params: Unpack["Invoice.FinalizeInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe automatically finalizes drafts before sending and attempting payment on invoices. However, if you'd like to finalize a draft invoice manually, you can do so using this method.
        """
        ...

    @overload
    async def finalize_invoice_async(
        self, **params: Unpack["Invoice.FinalizeInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe automatically finalizes drafts before sending and attempting payment on invoices. However, if you'd like to finalize a draft invoice manually, you can do so using this method.
        """
        ...

    @class_method_variant("_cls_finalize_invoice_async")
    async def finalize_invoice_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.FinalizeInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe automatically finalizes drafts before sending and attempting payment on invoices. However, if you'd like to finalize a draft invoice manually, you can do so using this method.
        """
        return cast(
            "Invoice",
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/finalize".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["Invoice.ListParams"]
    ) -> ListObject["Invoice"]:
        """
        You can list all invoices, or list the invoices for a specific customer. The invoices are returned sorted by creation date, with the most recently created invoices appearing first.
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
        cls, **params: Unpack["Invoice.ListParams"]
    ) -> ListObject["Invoice"]:
        """
        You can list all invoices, or list the invoices for a specific customer. The invoices are returned sorted by creation date, with the most recently created invoices appearing first.
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
    def _cls_mark_uncollectible(
        cls, invoice: str, **params: Unpack["Invoice.MarkUncollectibleParams"]
    ) -> "Invoice":
        """
        Marking an invoice as uncollectible is useful for keeping track of bad debts that can be written off for accounting purposes.
        """
        return cast(
            "Invoice",
            cls._static_request(
                "post",
                "/v1/invoices/{invoice}/mark_uncollectible".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def mark_uncollectible(
        invoice: str, **params: Unpack["Invoice.MarkUncollectibleParams"]
    ) -> "Invoice":
        """
        Marking an invoice as uncollectible is useful for keeping track of bad debts that can be written off for accounting purposes.
        """
        ...

    @overload
    def mark_uncollectible(
        self, **params: Unpack["Invoice.MarkUncollectibleParams"]
    ) -> "Invoice":
        """
        Marking an invoice as uncollectible is useful for keeping track of bad debts that can be written off for accounting purposes.
        """
        ...

    @class_method_variant("_cls_mark_uncollectible")
    def mark_uncollectible(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.MarkUncollectibleParams"]
    ) -> "Invoice":
        """
        Marking an invoice as uncollectible is useful for keeping track of bad debts that can be written off for accounting purposes.
        """
        return cast(
            "Invoice",
            self._request(
                "post",
                "/v1/invoices/{invoice}/mark_uncollectible".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_mark_uncollectible_async(
        cls, invoice: str, **params: Unpack["Invoice.MarkUncollectibleParams"]
    ) -> "Invoice":
        """
        Marking an invoice as uncollectible is useful for keeping track of bad debts that can be written off for accounting purposes.
        """
        return cast(
            "Invoice",
            await cls._static_request_async(
                "post",
                "/v1/invoices/{invoice}/mark_uncollectible".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def mark_uncollectible_async(
        invoice: str, **params: Unpack["Invoice.MarkUncollectibleParams"]
    ) -> "Invoice":
        """
        Marking an invoice as uncollectible is useful for keeping track of bad debts that can be written off for accounting purposes.
        """
        ...

    @overload
    async def mark_uncollectible_async(
        self, **params: Unpack["Invoice.MarkUncollectibleParams"]
    ) -> "Invoice":
        """
        Marking an invoice as uncollectible is useful for keeping track of bad debts that can be written off for accounting purposes.
        """
        ...

    @class_method_variant("_cls_mark_uncollectible_async")
    async def mark_uncollectible_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.MarkUncollectibleParams"]
    ) -> "Invoice":
        """
        Marking an invoice as uncollectible is useful for keeping track of bad debts that can be written off for accounting purposes.
        """
        return cast(
            "Invoice",
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/mark_uncollectible".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def modify(
        cls, id: str, **params: Unpack["Invoice.ModifyParams"]
    ) -> "Invoice":
        """
        Draft invoices are fully editable. Once an invoice is [finalized](https://stripe.com/docs/billing/invoices/workflow#finalized),
        monetary values, as well as collection_method, become uneditable.

        If you would like to stop the Stripe Billing engine from automatically finalizing, reattempting payments on,
        sending reminders for, or [automatically reconciling](https://stripe.com/docs/billing/invoices/reconciliation) invoices, pass
        auto_advance=false.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Invoice",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Invoice.ModifyParams"]
    ) -> "Invoice":
        """
        Draft invoices are fully editable. Once an invoice is [finalized](https://stripe.com/docs/billing/invoices/workflow#finalized),
        monetary values, as well as collection_method, become uneditable.

        If you would like to stop the Stripe Billing engine from automatically finalizing, reattempting payments on,
        sending reminders for, or [automatically reconciling](https://stripe.com/docs/billing/invoices/reconciliation) invoices, pass
        auto_advance=false.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Invoice",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def _cls_pay(
        cls, invoice: str, **params: Unpack["Invoice.PayParams"]
    ) -> "Invoice":
        """
        Stripe automatically creates and then attempts to collect payment on invoices for customers on subscriptions according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to attempt payment on an invoice out of the normal collection schedule or for some other reason, you can do so.
        """
        return cast(
            "Invoice",
            cls._static_request(
                "post",
                "/v1/invoices/{invoice}/pay".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def pay(invoice: str, **params: Unpack["Invoice.PayParams"]) -> "Invoice":
        """
        Stripe automatically creates and then attempts to collect payment on invoices for customers on subscriptions according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to attempt payment on an invoice out of the normal collection schedule or for some other reason, you can do so.
        """
        ...

    @overload
    def pay(self, **params: Unpack["Invoice.PayParams"]) -> "Invoice":
        """
        Stripe automatically creates and then attempts to collect payment on invoices for customers on subscriptions according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to attempt payment on an invoice out of the normal collection schedule or for some other reason, you can do so.
        """
        ...

    @class_method_variant("_cls_pay")
    def pay(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.PayParams"]
    ) -> "Invoice":
        """
        Stripe automatically creates and then attempts to collect payment on invoices for customers on subscriptions according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to attempt payment on an invoice out of the normal collection schedule or for some other reason, you can do so.
        """
        return cast(
            "Invoice",
            self._request(
                "post",
                "/v1/invoices/{invoice}/pay".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_pay_async(
        cls, invoice: str, **params: Unpack["Invoice.PayParams"]
    ) -> "Invoice":
        """
        Stripe automatically creates and then attempts to collect payment on invoices for customers on subscriptions according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to attempt payment on an invoice out of the normal collection schedule or for some other reason, you can do so.
        """
        return cast(
            "Invoice",
            await cls._static_request_async(
                "post",
                "/v1/invoices/{invoice}/pay".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def pay_async(
        invoice: str, **params: Unpack["Invoice.PayParams"]
    ) -> "Invoice":
        """
        Stripe automatically creates and then attempts to collect payment on invoices for customers on subscriptions according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to attempt payment on an invoice out of the normal collection schedule or for some other reason, you can do so.
        """
        ...

    @overload
    async def pay_async(
        self, **params: Unpack["Invoice.PayParams"]
    ) -> "Invoice":
        """
        Stripe automatically creates and then attempts to collect payment on invoices for customers on subscriptions according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to attempt payment on an invoice out of the normal collection schedule or for some other reason, you can do so.
        """
        ...

    @class_method_variant("_cls_pay_async")
    async def pay_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.PayParams"]
    ) -> "Invoice":
        """
        Stripe automatically creates and then attempts to collect payment on invoices for customers on subscriptions according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to attempt payment on an invoice out of the normal collection schedule or for some other reason, you can do so.
        """
        return cast(
            "Invoice",
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/pay".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def _cls_remove_lines(
        cls, invoice: str, **params: Unpack["Invoice.RemoveLinesParams"]
    ) -> "Invoice":
        """
        Removes multiple line items from an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            cls._static_request(
                "post",
                "/v1/invoices/{invoice}/remove_lines".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def remove_lines(
        invoice: str, **params: Unpack["Invoice.RemoveLinesParams"]
    ) -> "Invoice":
        """
        Removes multiple line items from an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @overload
    def remove_lines(
        self, **params: Unpack["Invoice.RemoveLinesParams"]
    ) -> "Invoice":
        """
        Removes multiple line items from an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @class_method_variant("_cls_remove_lines")
    def remove_lines(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.RemoveLinesParams"]
    ) -> "Invoice":
        """
        Removes multiple line items from an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            self._request(
                "post",
                "/v1/invoices/{invoice}/remove_lines".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_remove_lines_async(
        cls, invoice: str, **params: Unpack["Invoice.RemoveLinesParams"]
    ) -> "Invoice":
        """
        Removes multiple line items from an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            await cls._static_request_async(
                "post",
                "/v1/invoices/{invoice}/remove_lines".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def remove_lines_async(
        invoice: str, **params: Unpack["Invoice.RemoveLinesParams"]
    ) -> "Invoice":
        """
        Removes multiple line items from an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @overload
    async def remove_lines_async(
        self, **params: Unpack["Invoice.RemoveLinesParams"]
    ) -> "Invoice":
        """
        Removes multiple line items from an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @class_method_variant("_cls_remove_lines_async")
    async def remove_lines_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.RemoveLinesParams"]
    ) -> "Invoice":
        """
        Removes multiple line items from an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/remove_lines".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Invoice.RetrieveParams"]
    ) -> "Invoice":
        """
        Retrieves the invoice with the given ID.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Invoice.RetrieveParams"]
    ) -> "Invoice":
        """
        Retrieves the invoice with the given ID.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def _cls_send_invoice(
        cls, invoice: str, **params: Unpack["Invoice.SendInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe will automatically send invoices to customers according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to manually send an invoice to your customer out of the normal schedule, you can do so. When sending invoices that have already been paid, there will be no reference to the payment in the email.

        Requests made in test-mode result in no emails being sent, despite sending an invoice.sent event.
        """
        return cast(
            "Invoice",
            cls._static_request(
                "post",
                "/v1/invoices/{invoice}/send".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def send_invoice(
        invoice: str, **params: Unpack["Invoice.SendInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe will automatically send invoices to customers according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to manually send an invoice to your customer out of the normal schedule, you can do so. When sending invoices that have already been paid, there will be no reference to the payment in the email.

        Requests made in test-mode result in no emails being sent, despite sending an invoice.sent event.
        """
        ...

    @overload
    def send_invoice(
        self, **params: Unpack["Invoice.SendInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe will automatically send invoices to customers according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to manually send an invoice to your customer out of the normal schedule, you can do so. When sending invoices that have already been paid, there will be no reference to the payment in the email.

        Requests made in test-mode result in no emails being sent, despite sending an invoice.sent event.
        """
        ...

    @class_method_variant("_cls_send_invoice")
    def send_invoice(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.SendInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe will automatically send invoices to customers according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to manually send an invoice to your customer out of the normal schedule, you can do so. When sending invoices that have already been paid, there will be no reference to the payment in the email.

        Requests made in test-mode result in no emails being sent, despite sending an invoice.sent event.
        """
        return cast(
            "Invoice",
            self._request(
                "post",
                "/v1/invoices/{invoice}/send".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_send_invoice_async(
        cls, invoice: str, **params: Unpack["Invoice.SendInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe will automatically send invoices to customers according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to manually send an invoice to your customer out of the normal schedule, you can do so. When sending invoices that have already been paid, there will be no reference to the payment in the email.

        Requests made in test-mode result in no emails being sent, despite sending an invoice.sent event.
        """
        return cast(
            "Invoice",
            await cls._static_request_async(
                "post",
                "/v1/invoices/{invoice}/send".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def send_invoice_async(
        invoice: str, **params: Unpack["Invoice.SendInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe will automatically send invoices to customers according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to manually send an invoice to your customer out of the normal schedule, you can do so. When sending invoices that have already been paid, there will be no reference to the payment in the email.

        Requests made in test-mode result in no emails being sent, despite sending an invoice.sent event.
        """
        ...

    @overload
    async def send_invoice_async(
        self, **params: Unpack["Invoice.SendInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe will automatically send invoices to customers according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to manually send an invoice to your customer out of the normal schedule, you can do so. When sending invoices that have already been paid, there will be no reference to the payment in the email.

        Requests made in test-mode result in no emails being sent, despite sending an invoice.sent event.
        """
        ...

    @class_method_variant("_cls_send_invoice_async")
    async def send_invoice_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.SendInvoiceParams"]
    ) -> "Invoice":
        """
        Stripe will automatically send invoices to customers according to your [subscriptions settings](https://dashboard.stripe.com/account/billing/automatic). However, if you'd like to manually send an invoice to your customer out of the normal schedule, you can do so. When sending invoices that have already been paid, there will be no reference to the payment in the email.

        Requests made in test-mode result in no emails being sent, despite sending an invoice.sent event.
        """
        return cast(
            "Invoice",
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/send".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def upcoming(cls, **params: Unpack["Invoice.UpcomingParams"]) -> "Invoice":
        """
        At any time, you can preview the upcoming invoice for a customer. This will show you all the charges that are pending, including subscription renewal charges, invoice item charges, etc. It will also show you any discounts that are applicable to the invoice.

        Note that when you are viewing an upcoming invoice, you are simply viewing a preview – the invoice has not yet been created. As such, the upcoming invoice will not show up in invoice listing calls, and you cannot use the API to pay or edit the invoice. If you want to change the amount that your customer will be billed, you can add, remove, or update pending invoice items, or update the customer's discount.

        You can preview the effects of updating a subscription, including a preview of what proration will take place. To ensure that the actual proration is calculated exactly the same as the previewed proration, you should pass the subscription_details.proration_date parameter when doing the actual subscription update. The recommended way to get only the prorations being previewed is to consider only proration line items where period[start] is equal to the subscription_details.proration_date value passed in the request.

        Note: Currency conversion calculations use the latest exchange rates. Exchange rates may vary between the time of the preview and the time of the actual invoice creation. [Learn more](https://docs.stripe.com/currencies/conversions)
        """
        return cast(
            "Invoice",
            cls._static_request(
                "get",
                "/v1/invoices/upcoming",
                params=params,
            ),
        )

    @classmethod
    async def upcoming_async(
        cls, **params: Unpack["Invoice.UpcomingParams"]
    ) -> "Invoice":
        """
        At any time, you can preview the upcoming invoice for a customer. This will show you all the charges that are pending, including subscription renewal charges, invoice item charges, etc. It will also show you any discounts that are applicable to the invoice.

        Note that when you are viewing an upcoming invoice, you are simply viewing a preview – the invoice has not yet been created. As such, the upcoming invoice will not show up in invoice listing calls, and you cannot use the API to pay or edit the invoice. If you want to change the amount that your customer will be billed, you can add, remove, or update pending invoice items, or update the customer's discount.

        You can preview the effects of updating a subscription, including a preview of what proration will take place. To ensure that the actual proration is calculated exactly the same as the previewed proration, you should pass the subscription_details.proration_date parameter when doing the actual subscription update. The recommended way to get only the prorations being previewed is to consider only proration line items where period[start] is equal to the subscription_details.proration_date value passed in the request.

        Note: Currency conversion calculations use the latest exchange rates. Exchange rates may vary between the time of the preview and the time of the actual invoice creation. [Learn more](https://docs.stripe.com/currencies/conversions)
        """
        return cast(
            "Invoice",
            await cls._static_request_async(
                "get",
                "/v1/invoices/upcoming",
                params=params,
            ),
        )

    @classmethod
    def upcoming_lines(
        cls, **params: Unpack["Invoice.UpcomingLinesParams"]
    ) -> ListObject["InvoiceLineItem"]:
        """
        When retrieving an upcoming invoice, you'll get a lines property containing the total count of line items and the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["InvoiceLineItem"],
            cls._static_request(
                "get",
                "/v1/invoices/upcoming/lines",
                params=params,
            ),
        )

    @classmethod
    async def upcoming_lines_async(
        cls, **params: Unpack["Invoice.UpcomingLinesParams"]
    ) -> ListObject["InvoiceLineItem"]:
        """
        When retrieving an upcoming invoice, you'll get a lines property containing the total count of line items and the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["InvoiceLineItem"],
            await cls._static_request_async(
                "get",
                "/v1/invoices/upcoming/lines",
                params=params,
            ),
        )

    @classmethod
    def _cls_update_lines(
        cls, invoice: str, **params: Unpack["Invoice.UpdateLinesParams"]
    ) -> "Invoice":
        """
        Updates multiple line items on an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            cls._static_request(
                "post",
                "/v1/invoices/{invoice}/update_lines".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def update_lines(
        invoice: str, **params: Unpack["Invoice.UpdateLinesParams"]
    ) -> "Invoice":
        """
        Updates multiple line items on an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @overload
    def update_lines(
        self, **params: Unpack["Invoice.UpdateLinesParams"]
    ) -> "Invoice":
        """
        Updates multiple line items on an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @class_method_variant("_cls_update_lines")
    def update_lines(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.UpdateLinesParams"]
    ) -> "Invoice":
        """
        Updates multiple line items on an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            self._request(
                "post",
                "/v1/invoices/{invoice}/update_lines".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_update_lines_async(
        cls, invoice: str, **params: Unpack["Invoice.UpdateLinesParams"]
    ) -> "Invoice":
        """
        Updates multiple line items on an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            await cls._static_request_async(
                "post",
                "/v1/invoices/{invoice}/update_lines".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def update_lines_async(
        invoice: str, **params: Unpack["Invoice.UpdateLinesParams"]
    ) -> "Invoice":
        """
        Updates multiple line items on an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @overload
    async def update_lines_async(
        self, **params: Unpack["Invoice.UpdateLinesParams"]
    ) -> "Invoice":
        """
        Updates multiple line items on an invoice. This is only possible when an invoice is still a draft.
        """
        ...

    @class_method_variant("_cls_update_lines_async")
    async def update_lines_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.UpdateLinesParams"]
    ) -> "Invoice":
        """
        Updates multiple line items on an invoice. This is only possible when an invoice is still a draft.
        """
        return cast(
            "Invoice",
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/update_lines".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def _cls_void_invoice(
        cls, invoice: str, **params: Unpack["Invoice.VoidInvoiceParams"]
    ) -> "Invoice":
        """
        Mark a finalized invoice as void. This cannot be undone. Voiding an invoice is similar to [deletion](https://stripe.com/docs/api#delete_invoice), however it only applies to finalized invoices and maintains a papertrail where the invoice can still be found.

        Consult with local regulations to determine whether and how an invoice might be amended, canceled, or voided in the jurisdiction you're doing business in. You might need to [issue another invoice or <a href="#create_credit_note">credit note](https://stripe.com/docs/api#create_invoice) instead. Stripe recommends that you consult with your legal counsel for advice specific to your business.
        """
        return cast(
            "Invoice",
            cls._static_request(
                "post",
                "/v1/invoices/{invoice}/void".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    def void_invoice(
        invoice: str, **params: Unpack["Invoice.VoidInvoiceParams"]
    ) -> "Invoice":
        """
        Mark a finalized invoice as void. This cannot be undone. Voiding an invoice is similar to [deletion](https://stripe.com/docs/api#delete_invoice), however it only applies to finalized invoices and maintains a papertrail where the invoice can still be found.

        Consult with local regulations to determine whether and how an invoice might be amended, canceled, or voided in the jurisdiction you're doing business in. You might need to [issue another invoice or <a href="#create_credit_note">credit note](https://stripe.com/docs/api#create_invoice) instead. Stripe recommends that you consult with your legal counsel for advice specific to your business.
        """
        ...

    @overload
    def void_invoice(
        self, **params: Unpack["Invoice.VoidInvoiceParams"]
    ) -> "Invoice":
        """
        Mark a finalized invoice as void. This cannot be undone. Voiding an invoice is similar to [deletion](https://stripe.com/docs/api#delete_invoice), however it only applies to finalized invoices and maintains a papertrail where the invoice can still be found.

        Consult with local regulations to determine whether and how an invoice might be amended, canceled, or voided in the jurisdiction you're doing business in. You might need to [issue another invoice or <a href="#create_credit_note">credit note](https://stripe.com/docs/api#create_invoice) instead. Stripe recommends that you consult with your legal counsel for advice specific to your business.
        """
        ...

    @class_method_variant("_cls_void_invoice")
    def void_invoice(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.VoidInvoiceParams"]
    ) -> "Invoice":
        """
        Mark a finalized invoice as void. This cannot be undone. Voiding an invoice is similar to [deletion](https://stripe.com/docs/api#delete_invoice), however it only applies to finalized invoices and maintains a papertrail where the invoice can still be found.

        Consult with local regulations to determine whether and how an invoice might be amended, canceled, or voided in the jurisdiction you're doing business in. You might need to [issue another invoice or <a href="#create_credit_note">credit note](https://stripe.com/docs/api#create_invoice) instead. Stripe recommends that you consult with your legal counsel for advice specific to your business.
        """
        return cast(
            "Invoice",
            self._request(
                "post",
                "/v1/invoices/{invoice}/void".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    async def _cls_void_invoice_async(
        cls, invoice: str, **params: Unpack["Invoice.VoidInvoiceParams"]
    ) -> "Invoice":
        """
        Mark a finalized invoice as void. This cannot be undone. Voiding an invoice is similar to [deletion](https://stripe.com/docs/api#delete_invoice), however it only applies to finalized invoices and maintains a papertrail where the invoice can still be found.

        Consult with local regulations to determine whether and how an invoice might be amended, canceled, or voided in the jurisdiction you're doing business in. You might need to [issue another invoice or <a href="#create_credit_note">credit note](https://stripe.com/docs/api#create_invoice) instead. Stripe recommends that you consult with your legal counsel for advice specific to your business.
        """
        return cast(
            "Invoice",
            await cls._static_request_async(
                "post",
                "/v1/invoices/{invoice}/void".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def void_invoice_async(
        invoice: str, **params: Unpack["Invoice.VoidInvoiceParams"]
    ) -> "Invoice":
        """
        Mark a finalized invoice as void. This cannot be undone. Voiding an invoice is similar to [deletion](https://stripe.com/docs/api#delete_invoice), however it only applies to finalized invoices and maintains a papertrail where the invoice can still be found.

        Consult with local regulations to determine whether and how an invoice might be amended, canceled, or voided in the jurisdiction you're doing business in. You might need to [issue another invoice or <a href="#create_credit_note">credit note](https://stripe.com/docs/api#create_invoice) instead. Stripe recommends that you consult with your legal counsel for advice specific to your business.
        """
        ...

    @overload
    async def void_invoice_async(
        self, **params: Unpack["Invoice.VoidInvoiceParams"]
    ) -> "Invoice":
        """
        Mark a finalized invoice as void. This cannot be undone. Voiding an invoice is similar to [deletion](https://stripe.com/docs/api#delete_invoice), however it only applies to finalized invoices and maintains a papertrail where the invoice can still be found.

        Consult with local regulations to determine whether and how an invoice might be amended, canceled, or voided in the jurisdiction you're doing business in. You might need to [issue another invoice or <a href="#create_credit_note">credit note](https://stripe.com/docs/api#create_invoice) instead. Stripe recommends that you consult with your legal counsel for advice specific to your business.
        """
        ...

    @class_method_variant("_cls_void_invoice_async")
    async def void_invoice_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Invoice.VoidInvoiceParams"]
    ) -> "Invoice":
        """
        Mark a finalized invoice as void. This cannot be undone. Voiding an invoice is similar to [deletion](https://stripe.com/docs/api#delete_invoice), however it only applies to finalized invoices and maintains a papertrail where the invoice can still be found.

        Consult with local regulations to determine whether and how an invoice might be amended, canceled, or voided in the jurisdiction you're doing business in. You might need to [issue another invoice or <a href="#create_credit_note">credit note](https://stripe.com/docs/api#create_invoice) instead. Stripe recommends that you consult with your legal counsel for advice specific to your business.
        """
        return cast(
            "Invoice",
            await self._request_async(
                "post",
                "/v1/invoices/{invoice}/void".format(
                    invoice=sanitize_id(self.get("id"))
                ),
                params=params,
            ),
        )

    @classmethod
    def search(
        cls, *args, **kwargs: Unpack["Invoice.SearchParams"]
    ) -> SearchResultObject["Invoice"]:
        """
        Search for invoices you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cls._search(search_url="/v1/invoices/search", *args, **kwargs)

    @classmethod
    async def search_async(
        cls, *args, **kwargs: Unpack["Invoice.SearchParams"]
    ) -> SearchResultObject["Invoice"]:
        """
        Search for invoices you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return await cls._search_async(
            search_url="/v1/invoices/search", *args, **kwargs
        )

    @classmethod
    def search_auto_paging_iter(
        cls, *args, **kwargs: Unpack["Invoice.SearchParams"]
    ) -> Iterator["Invoice"]:
        return cls.search(*args, **kwargs).auto_paging_iter()

    @classmethod
    async def search_auto_paging_iter_async(
        cls, *args, **kwargs: Unpack["Invoice.SearchParams"]
    ) -> AsyncIterator["Invoice"]:
        return (await cls.search_async(*args, **kwargs)).auto_paging_iter()

    @classmethod
    def list_lines(
        cls, invoice: str, **params: Unpack["Invoice.ListLinesParams"]
    ) -> ListObject["InvoiceLineItem"]:
        """
        When retrieving an invoice, you'll get a lines property containing the total count of line items and the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["InvoiceLineItem"],
            cls._static_request(
                "get",
                "/v1/invoices/{invoice}/lines".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    @classmethod
    async def list_lines_async(
        cls, invoice: str, **params: Unpack["Invoice.ListLinesParams"]
    ) -> ListObject["InvoiceLineItem"]:
        """
        When retrieving an invoice, you'll get a lines property containing the total count of line items and the first handful of those items. There is also a URL where you can retrieve the full (paginated) list of line items.
        """
        return cast(
            ListObject["InvoiceLineItem"],
            await cls._static_request_async(
                "get",
                "/v1/invoices/{invoice}/lines".format(
                    invoice=sanitize_id(invoice)
                ),
                params=params,
            ),
        )

    _inner_class_types = {
        "automatic_tax": AutomaticTax,
        "custom_fields": CustomField,
        "customer_address": CustomerAddress,
        "customer_shipping": CustomerShipping,
        "customer_tax_ids": CustomerTaxId,
        "from_invoice": FromInvoice,
        "issuer": Issuer,
        "last_finalization_error": LastFinalizationError,
        "payment_settings": PaymentSettings,
        "rendering": Rendering,
        "shipping_cost": ShippingCost,
        "shipping_details": ShippingDetails,
        "status_transitions": StatusTransitions,
        "subscription_details": SubscriptionDetails,
        "threshold_reason": ThresholdReason,
        "total_discount_amounts": TotalDiscountAmount,
        "total_tax_amounts": TotalTaxAmount,
        "transfer_data": TransferData,
    }
