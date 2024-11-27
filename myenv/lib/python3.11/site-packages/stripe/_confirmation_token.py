# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._api_resource import APIResource
from stripe._expandable_field import ExpandableField
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._test_helpers import APIResourceTestHelpers
from typing import ClassVar, Dict, List, Optional, cast
from typing_extensions import (
    Literal,
    NotRequired,
    Type,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._charge import Charge
    from stripe._customer import Customer
    from stripe._setup_attempt import SetupAttempt


class ConfirmationToken(APIResource["ConfirmationToken"]):
    """
    ConfirmationTokens help transport client side data collected by Stripe JS over
    to your server for confirming a PaymentIntent or SetupIntent. If the confirmation
    is successful, values present on the ConfirmationToken are written onto the Intent.

    To learn more about how to use ConfirmationToken, visit the related guides:
    - [Finalize payments on the server](https://stripe.com/docs/payments/finalize-payments-on-the-server)
    - [Build two-step confirmation](https://stripe.com/docs/payments/build-a-two-step-confirmation).
    """

    OBJECT_NAME: ClassVar[Literal["confirmation_token"]] = "confirmation_token"

    class MandateData(StripeObject):
        class CustomerAcceptance(StripeObject):
            class Online(StripeObject):
                ip_address: Optional[str]
                """
                The IP address from which the Mandate was accepted by the customer.
                """
                user_agent: Optional[str]
                """
                The user agent of the browser from which the Mandate was accepted by the customer.
                """

            online: Optional[Online]
            """
            If this is a Mandate accepted online, this hash contains details about the online acceptance.
            """
            type: str
            """
            The type of customer acceptance information included with the Mandate.
            """
            _inner_class_types = {"online": Online}

        customer_acceptance: CustomerAcceptance
        """
        This hash contains details about the customer acceptance of the Mandate.
        """
        _inner_class_types = {"customer_acceptance": CustomerAcceptance}

    class PaymentMethodOptions(StripeObject):
        class Card(StripeObject):
            cvc_token: Optional[str]
            """
            The `cvc_update` Token collected from the Payment Element.
            """

        card: Optional[Card]
        """
        This hash contains the card payment method options.
        """
        _inner_class_types = {"card": Card}

    class PaymentMethodPreview(StripeObject):
        class AcssDebit(StripeObject):
            bank_name: Optional[str]
            """
            Name of the bank associated with the bank account.
            """
            fingerprint: Optional[str]
            """
            Uniquely identifies this particular bank account. You can use this attribute to check whether two bank accounts are the same.
            """
            institution_number: Optional[str]
            """
            Institution number of the bank account.
            """
            last4: Optional[str]
            """
            Last four digits of the bank account number.
            """
            transit_number: Optional[str]
            """
            Transit number of the bank account.
            """

        class Affirm(StripeObject):
            pass

        class AfterpayClearpay(StripeObject):
            pass

        class Alipay(StripeObject):
            pass

        class AmazonPay(StripeObject):
            pass

        class AuBecsDebit(StripeObject):
            bsb_number: Optional[str]
            """
            Six-digit number identifying bank and branch associated with this bank account.
            """
            fingerprint: Optional[str]
            """
            Uniquely identifies this particular bank account. You can use this attribute to check whether two bank accounts are the same.
            """
            last4: Optional[str]
            """
            Last four digits of the bank account number.
            """

        class BacsDebit(StripeObject):
            fingerprint: Optional[str]
            """
            Uniquely identifies this particular bank account. You can use this attribute to check whether two bank accounts are the same.
            """
            last4: Optional[str]
            """
            Last four digits of the bank account number.
            """
            sort_code: Optional[str]
            """
            Sort code of the bank account. (e.g., `10-20-30`)
            """

        class Bancontact(StripeObject):
            pass

        class BillingDetails(StripeObject):
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
            """
            Billing address.
            """
            email: Optional[str]
            """
            Email address.
            """
            name: Optional[str]
            """
            Full name.
            """
            phone: Optional[str]
            """
            Billing phone number (including extension).
            """
            _inner_class_types = {"address": Address}

        class Blik(StripeObject):
            pass

        class Boleto(StripeObject):
            tax_id: str
            """
            Uniquely identifies the customer tax id (CNPJ or CPF)
            """

        class Card(StripeObject):
            class Checks(StripeObject):
                address_line1_check: Optional[str]
                """
                If a address line1 was provided, results of the check, one of `pass`, `fail`, `unavailable`, or `unchecked`.
                """
                address_postal_code_check: Optional[str]
                """
                If a address postal code was provided, results of the check, one of `pass`, `fail`, `unavailable`, or `unchecked`.
                """
                cvc_check: Optional[str]
                """
                If a CVC was provided, results of the check, one of `pass`, `fail`, `unavailable`, or `unchecked`.
                """

            class GeneratedFrom(StripeObject):
                class PaymentMethodDetails(StripeObject):
                    class CardPresent(StripeObject):
                        class Offline(StripeObject):
                            stored_at: Optional[int]
                            """
                            Time at which the payment was collected while offline
                            """
                            type: Optional[Literal["deferred"]]
                            """
                            The method used to process this payment method offline. Only deferred is allowed.
                            """

                        class Receipt(StripeObject):
                            account_type: Optional[
                                Literal[
                                    "checking", "credit", "prepaid", "unknown"
                                ]
                            ]
                            """
                            The type of account being debited or credited
                            """
                            application_cryptogram: Optional[str]
                            """
                            EMV tag 9F26, cryptogram generated by the integrated circuit chip.
                            """
                            application_preferred_name: Optional[str]
                            """
                            Mnenomic of the Application Identifier.
                            """
                            authorization_code: Optional[str]
                            """
                            Identifier for this transaction.
                            """
                            authorization_response_code: Optional[str]
                            """
                            EMV tag 8A. A code returned by the card issuer.
                            """
                            cardholder_verification_method: Optional[str]
                            """
                            Describes the method used by the cardholder to verify ownership of the card. One of the following: `approval`, `failure`, `none`, `offline_pin`, `offline_pin_and_signature`, `online_pin`, or `signature`.
                            """
                            dedicated_file_name: Optional[str]
                            """
                            EMV tag 84. Similar to the application identifier stored on the integrated circuit chip.
                            """
                            terminal_verification_results: Optional[str]
                            """
                            The outcome of a series of EMV functions performed by the card reader.
                            """
                            transaction_status_information: Optional[str]
                            """
                            An indication of various EMV functions performed during the transaction.
                            """

                        amount_authorized: Optional[int]
                        """
                        The authorized amount
                        """
                        brand: Optional[str]
                        """
                        Card brand. Can be `amex`, `diners`, `discover`, `eftpos_au`, `jcb`, `mastercard`, `unionpay`, `visa`, or `unknown`.
                        """
                        brand_product: Optional[str]
                        """
                        The [product code](https://stripe.com/docs/card-product-codes) that identifies the specific program or product associated with a card.
                        """
                        capture_before: Optional[int]
                        """
                        When using manual capture, a future timestamp after which the charge will be automatically refunded if uncaptured.
                        """
                        cardholder_name: Optional[str]
                        """
                        The cardholder name as read from the card, in [ISO 7813](https://en.wikipedia.org/wiki/ISO/IEC_7813) format. May include alphanumeric characters, special characters and first/last name separator (`/`). In some cases, the cardholder name may not be available depending on how the issuer has configured the card. Cardholder name is typically not available on swipe or contactless payments, such as those made with Apple Pay and Google Pay.
                        """
                        country: Optional[str]
                        """
                        Two-letter ISO code representing the country of the card. You could use this attribute to get a sense of the international breakdown of cards you've collected.
                        """
                        description: Optional[str]
                        """
                        A high-level description of the type of cards issued in this range. (For internal use only and not typically available in standard API requests.)
                        """
                        emv_auth_data: Optional[str]
                        """
                        Authorization response cryptogram.
                        """
                        exp_month: int
                        """
                        Two-digit number representing the card's expiration month.
                        """
                        exp_year: int
                        """
                        Four-digit number representing the card's expiration year.
                        """
                        fingerprint: Optional[str]
                        """
                        Uniquely identifies this particular card number. You can use this attribute to check whether two customers who've signed up with you are using the same card number, for example. For payment methods that tokenize card information (Apple Pay, Google Pay), the tokenized number might be provided instead of the underlying card number.

                        *As of May 1, 2021, card fingerprint in India for Connect changed to allow two fingerprints for the same card---one for India and one for the rest of the world.*
                        """
                        funding: Optional[str]
                        """
                        Card funding type. Can be `credit`, `debit`, `prepaid`, or `unknown`.
                        """
                        generated_card: Optional[str]
                        """
                        ID of a card PaymentMethod generated from the card_present PaymentMethod that may be attached to a Customer for future transactions. Only present if it was possible to generate a card PaymentMethod.
                        """
                        iin: Optional[str]
                        """
                        Issuer identification number of the card. (For internal use only and not typically available in standard API requests.)
                        """
                        incremental_authorization_supported: bool
                        """
                        Whether this [PaymentIntent](https://stripe.com/docs/api/payment_intents) is eligible for incremental authorizations. Request support using [request_incremental_authorization_support](https://stripe.com/docs/api/payment_intents/create#create_payment_intent-payment_method_options-card_present-request_incremental_authorization_support).
                        """
                        issuer: Optional[str]
                        """
                        The name of the card's issuing bank. (For internal use only and not typically available in standard API requests.)
                        """
                        last4: Optional[str]
                        """
                        The last four digits of the card.
                        """
                        network: Optional[str]
                        """
                        Identifies which network this charge was processed on. Can be `amex`, `cartes_bancaires`, `diners`, `discover`, `eftpos_au`, `interac`, `jcb`, `mastercard`, `unionpay`, `visa`, or `unknown`.
                        """
                        network_transaction_id: Optional[str]
                        """
                        This is used by the financial networks to identify a transaction.
                        Visa calls this the Transaction ID, Mastercard calls this the Trace ID, and American Express calls this the Acquirer Reference Data.
                        The first three digits of the Trace ID is the Financial Network Code, the next 6 digits is the Banknet Reference Number, and the last 4 digits represent the date (MM/DD).
                        This field will be available for successful Visa, Mastercard, or American Express transactions and always null for other card brands.
                        """
                        offline: Optional[Offline]
                        """
                        Details about payments collected offline.
                        """
                        overcapture_supported: bool
                        """
                        Defines whether the authorized amount can be over-captured or not
                        """
                        preferred_locales: Optional[List[str]]
                        """
                        EMV tag 5F2D. Preferred languages specified by the integrated circuit chip.
                        """
                        read_method: Optional[
                            Literal[
                                "contact_emv",
                                "contactless_emv",
                                "contactless_magstripe_mode",
                                "magnetic_stripe_fallback",
                                "magnetic_stripe_track2",
                            ]
                        ]
                        """
                        How card details were read in this transaction.
                        """
                        receipt: Optional[Receipt]
                        """
                        A collection of fields required to be displayed on receipts. Only required for EMV transactions.
                        """
                        _inner_class_types = {
                            "offline": Offline,
                            "receipt": Receipt,
                        }

                    card_present: Optional[CardPresent]
                    type: str
                    """
                    The type of payment method transaction-specific details from the transaction that generated this `card` payment method. Always `card_present`.
                    """
                    _inner_class_types = {"card_present": CardPresent}

                charge: Optional[str]
                """
                The charge that created this object.
                """
                payment_method_details: Optional[PaymentMethodDetails]
                """
                Transaction-specific details of the payment method used in the payment.
                """
                setup_attempt: Optional[ExpandableField["SetupAttempt"]]
                """
                The ID of the SetupAttempt that generated this PaymentMethod, if any.
                """
                _inner_class_types = {
                    "payment_method_details": PaymentMethodDetails,
                }

            class Networks(StripeObject):
                available: List[str]
                """
                All available networks for the card.
                """
                preferred: Optional[str]
                """
                The preferred network for co-branded cards. Can be `cartes_bancaires`, `mastercard`, `visa` or `invalid_preference` if requested network is not valid for the card.
                """

            class ThreeDSecureUsage(StripeObject):
                supported: bool
                """
                Whether 3D Secure is supported on this card.
                """

            class Wallet(StripeObject):
                class AmexExpressCheckout(StripeObject):
                    pass

                class ApplePay(StripeObject):
                    pass

                class GooglePay(StripeObject):
                    pass

                class Link(StripeObject):
                    pass

                class Masterpass(StripeObject):
                    class BillingAddress(StripeObject):
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

                    class ShippingAddress(StripeObject):
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

                    billing_address: Optional[BillingAddress]
                    """
                    Owner's verified billing address. Values are verified or provided by the wallet directly (if supported) at the time of authorization or settlement. They cannot be set or mutated.
                    """
                    email: Optional[str]
                    """
                    Owner's verified email. Values are verified or provided by the wallet directly (if supported) at the time of authorization or settlement. They cannot be set or mutated.
                    """
                    name: Optional[str]
                    """
                    Owner's verified full name. Values are verified or provided by the wallet directly (if supported) at the time of authorization or settlement. They cannot be set or mutated.
                    """
                    shipping_address: Optional[ShippingAddress]
                    """
                    Owner's verified shipping address. Values are verified or provided by the wallet directly (if supported) at the time of authorization or settlement. They cannot be set or mutated.
                    """
                    _inner_class_types = {
                        "billing_address": BillingAddress,
                        "shipping_address": ShippingAddress,
                    }

                class SamsungPay(StripeObject):
                    pass

                class VisaCheckout(StripeObject):
                    class BillingAddress(StripeObject):
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

                    class ShippingAddress(StripeObject):
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

                    billing_address: Optional[BillingAddress]
                    """
                    Owner's verified billing address. Values are verified or provided by the wallet directly (if supported) at the time of authorization or settlement. They cannot be set or mutated.
                    """
                    email: Optional[str]
                    """
                    Owner's verified email. Values are verified or provided by the wallet directly (if supported) at the time of authorization or settlement. They cannot be set or mutated.
                    """
                    name: Optional[str]
                    """
                    Owner's verified full name. Values are verified or provided by the wallet directly (if supported) at the time of authorization or settlement. They cannot be set or mutated.
                    """
                    shipping_address: Optional[ShippingAddress]
                    """
                    Owner's verified shipping address. Values are verified or provided by the wallet directly (if supported) at the time of authorization or settlement. They cannot be set or mutated.
                    """
                    _inner_class_types = {
                        "billing_address": BillingAddress,
                        "shipping_address": ShippingAddress,
                    }

                amex_express_checkout: Optional[AmexExpressCheckout]
                apple_pay: Optional[ApplePay]
                dynamic_last4: Optional[str]
                """
                (For tokenized numbers only.) The last four digits of the device account number.
                """
                google_pay: Optional[GooglePay]
                link: Optional[Link]
                masterpass: Optional[Masterpass]
                samsung_pay: Optional[SamsungPay]
                type: Literal[
                    "amex_express_checkout",
                    "apple_pay",
                    "google_pay",
                    "link",
                    "masterpass",
                    "samsung_pay",
                    "visa_checkout",
                ]
                """
                The type of the card wallet, one of `amex_express_checkout`, `apple_pay`, `google_pay`, `masterpass`, `samsung_pay`, `visa_checkout`, or `link`. An additional hash is included on the Wallet subhash with a name matching this value. It contains additional information specific to the card wallet type.
                """
                visa_checkout: Optional[VisaCheckout]
                _inner_class_types = {
                    "amex_express_checkout": AmexExpressCheckout,
                    "apple_pay": ApplePay,
                    "google_pay": GooglePay,
                    "link": Link,
                    "masterpass": Masterpass,
                    "samsung_pay": SamsungPay,
                    "visa_checkout": VisaCheckout,
                }

            brand: str
            """
            Card brand. Can be `amex`, `diners`, `discover`, `eftpos_au`, `jcb`, `mastercard`, `unionpay`, `visa`, or `unknown`.
            """
            checks: Optional[Checks]
            """
            Checks on Card address and CVC if provided.
            """
            country: Optional[str]
            """
            Two-letter ISO code representing the country of the card. You could use this attribute to get a sense of the international breakdown of cards you've collected.
            """
            description: Optional[str]
            """
            A high-level description of the type of cards issued in this range. (For internal use only and not typically available in standard API requests.)
            """
            display_brand: Optional[str]
            """
            The brand to use when displaying the card, this accounts for customer's brand choice on dual-branded cards. Can be `american_express`, `cartes_bancaires`, `diners_club`, `discover`, `eftpos_australia`, `interac`, `jcb`, `mastercard`, `union_pay`, `visa`, or `other` and may contain more values in the future.
            """
            exp_month: int
            """
            Two-digit number representing the card's expiration month.
            """
            exp_year: int
            """
            Four-digit number representing the card's expiration year.
            """
            fingerprint: Optional[str]
            """
            Uniquely identifies this particular card number. You can use this attribute to check whether two customers who've signed up with you are using the same card number, for example. For payment methods that tokenize card information (Apple Pay, Google Pay), the tokenized number might be provided instead of the underlying card number.

            *As of May 1, 2021, card fingerprint in India for Connect changed to allow two fingerprints for the same card---one for India and one for the rest of the world.*
            """
            funding: str
            """
            Card funding type. Can be `credit`, `debit`, `prepaid`, or `unknown`.
            """
            generated_from: Optional[GeneratedFrom]
            """
            Details of the original PaymentMethod that created this object.
            """
            iin: Optional[str]
            """
            Issuer identification number of the card. (For internal use only and not typically available in standard API requests.)
            """
            issuer: Optional[str]
            """
            The name of the card's issuing bank. (For internal use only and not typically available in standard API requests.)
            """
            last4: str
            """
            The last four digits of the card.
            """
            networks: Optional[Networks]
            """
            Contains information about card networks that can be used to process the payment.
            """
            three_d_secure_usage: Optional[ThreeDSecureUsage]
            """
            Contains details on how this Card may be used for 3D Secure authentication.
            """
            wallet: Optional[Wallet]
            """
            If this Card is part of a card wallet, this contains the details of the card wallet.
            """
            _inner_class_types = {
                "checks": Checks,
                "generated_from": GeneratedFrom,
                "networks": Networks,
                "three_d_secure_usage": ThreeDSecureUsage,
                "wallet": Wallet,
            }

        class CardPresent(StripeObject):
            class Networks(StripeObject):
                available: List[str]
                """
                All available networks for the card.
                """
                preferred: Optional[str]
                """
                The preferred network for the card.
                """

            class Offline(StripeObject):
                stored_at: Optional[int]
                """
                Time at which the payment was collected while offline
                """
                type: Optional[Literal["deferred"]]
                """
                The method used to process this payment method offline. Only deferred is allowed.
                """

            brand: Optional[str]
            """
            Card brand. Can be `amex`, `diners`, `discover`, `eftpos_au`, `jcb`, `mastercard`, `unionpay`, `visa`, or `unknown`.
            """
            brand_product: Optional[str]
            """
            The [product code](https://stripe.com/docs/card-product-codes) that identifies the specific program or product associated with a card.
            """
            cardholder_name: Optional[str]
            """
            The cardholder name as read from the card, in [ISO 7813](https://en.wikipedia.org/wiki/ISO/IEC_7813) format. May include alphanumeric characters, special characters and first/last name separator (`/`). In some cases, the cardholder name may not be available depending on how the issuer has configured the card. Cardholder name is typically not available on swipe or contactless payments, such as those made with Apple Pay and Google Pay.
            """
            country: Optional[str]
            """
            Two-letter ISO code representing the country of the card. You could use this attribute to get a sense of the international breakdown of cards you've collected.
            """
            description: Optional[str]
            """
            A high-level description of the type of cards issued in this range. (For internal use only and not typically available in standard API requests.)
            """
            exp_month: int
            """
            Two-digit number representing the card's expiration month.
            """
            exp_year: int
            """
            Four-digit number representing the card's expiration year.
            """
            fingerprint: Optional[str]
            """
            Uniquely identifies this particular card number. You can use this attribute to check whether two customers who've signed up with you are using the same card number, for example. For payment methods that tokenize card information (Apple Pay, Google Pay), the tokenized number might be provided instead of the underlying card number.

            *As of May 1, 2021, card fingerprint in India for Connect changed to allow two fingerprints for the same card---one for India and one for the rest of the world.*
            """
            funding: Optional[str]
            """
            Card funding type. Can be `credit`, `debit`, `prepaid`, or `unknown`.
            """
            iin: Optional[str]
            """
            Issuer identification number of the card. (For internal use only and not typically available in standard API requests.)
            """
            issuer: Optional[str]
            """
            The name of the card's issuing bank. (For internal use only and not typically available in standard API requests.)
            """
            last4: Optional[str]
            """
            The last four digits of the card.
            """
            networks: Optional[Networks]
            """
            Contains information about card networks that can be used to process the payment.
            """
            offline: Optional[Offline]
            """
            Details about payment methods collected offline.
            """
            preferred_locales: Optional[List[str]]
            """
            EMV tag 5F2D. Preferred languages specified by the integrated circuit chip.
            """
            read_method: Optional[
                Literal[
                    "contact_emv",
                    "contactless_emv",
                    "contactless_magstripe_mode",
                    "magnetic_stripe_fallback",
                    "magnetic_stripe_track2",
                ]
            ]
            """
            How card details were read in this transaction.
            """
            _inner_class_types = {"networks": Networks, "offline": Offline}

        class Cashapp(StripeObject):
            buyer_id: Optional[str]
            """
            A unique and immutable identifier assigned by Cash App to every buyer.
            """
            cashtag: Optional[str]
            """
            A public identifier for buyers using Cash App.
            """

        class CustomerBalance(StripeObject):
            pass

        class Eps(StripeObject):
            bank: Optional[
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
            The customer's bank. Should be one of `arzte_und_apotheker_bank`, `austrian_anadi_bank_ag`, `bank_austria`, `bankhaus_carl_spangler`, `bankhaus_schelhammer_und_schattera_ag`, `bawag_psk_ag`, `bks_bank_ag`, `brull_kallmus_bank_ag`, `btv_vier_lander_bank`, `capital_bank_grawe_gruppe_ag`, `deutsche_bank_ag`, `dolomitenbank`, `easybank_ag`, `erste_bank_und_sparkassen`, `hypo_alpeadriabank_international_ag`, `hypo_noe_lb_fur_niederosterreich_u_wien`, `hypo_oberosterreich_salzburg_steiermark`, `hypo_tirol_bank_ag`, `hypo_vorarlberg_bank_ag`, `hypo_bank_burgenland_aktiengesellschaft`, `marchfelder_bank`, `oberbank_ag`, `raiffeisen_bankengruppe_osterreich`, `schoellerbank_ag`, `sparda_bank_wien`, `volksbank_gruppe`, `volkskreditbank_ag`, or `vr_bank_braunau`.
            """

        class Fpx(StripeObject):
            account_holder_type: Optional[Literal["company", "individual"]]
            """
            Account holder type, if provided. Can be one of `individual` or `company`.
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
            The customer's bank, if provided. Can be one of `affin_bank`, `agrobank`, `alliance_bank`, `ambank`, `bank_islam`, `bank_muamalat`, `bank_rakyat`, `bsn`, `cimb`, `hong_leong_bank`, `hsbc`, `kfh`, `maybank2u`, `ocbc`, `public_bank`, `rhb`, `standard_chartered`, `uob`, `deutsche_bank`, `maybank2e`, `pb_enterprise`, or `bank_of_china`.
            """

        class Giropay(StripeObject):
            pass

        class Grabpay(StripeObject):
            pass

        class Ideal(StripeObject):
            bank: Optional[
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
            The customer's bank, if provided. Can be one of `abn_amro`, `asn_bank`, `bunq`, `handelsbanken`, `ing`, `knab`, `moneyou`, `n26`, `nn`, `rabobank`, `regiobank`, `revolut`, `sns_bank`, `triodos_bank`, `van_lanschot`, or `yoursafe`.
            """
            bic: Optional[
                Literal[
                    "ABNANL2A",
                    "ASNBNL21",
                    "BITSNL2A",
                    "BUNQNL2A",
                    "FVLBNL22",
                    "HANDNL2A",
                    "INGBNL2A",
                    "KNABNL2H",
                    "MOYONL21",
                    "NNBANL2G",
                    "NTSBDEB1",
                    "RABONL2U",
                    "RBRBNL21",
                    "REVOIE23",
                    "REVOLT21",
                    "SNSBNL2A",
                    "TRIONL2U",
                ]
            ]
            """
            The Bank Identifier Code of the customer's bank, if the bank was provided.
            """

        class InteracPresent(StripeObject):
            class Networks(StripeObject):
                available: List[str]
                """
                All available networks for the card.
                """
                preferred: Optional[str]
                """
                The preferred network for the card.
                """

            brand: Optional[str]
            """
            Card brand. Can be `interac`, `mastercard` or `visa`.
            """
            cardholder_name: Optional[str]
            """
            The cardholder name as read from the card, in [ISO 7813](https://en.wikipedia.org/wiki/ISO/IEC_7813) format. May include alphanumeric characters, special characters and first/last name separator (`/`). In some cases, the cardholder name may not be available depending on how the issuer has configured the card. Cardholder name is typically not available on swipe or contactless payments, such as those made with Apple Pay and Google Pay.
            """
            country: Optional[str]
            """
            Two-letter ISO code representing the country of the card. You could use this attribute to get a sense of the international breakdown of cards you've collected.
            """
            description: Optional[str]
            """
            A high-level description of the type of cards issued in this range. (For internal use only and not typically available in standard API requests.)
            """
            exp_month: int
            """
            Two-digit number representing the card's expiration month.
            """
            exp_year: int
            """
            Four-digit number representing the card's expiration year.
            """
            fingerprint: Optional[str]
            """
            Uniquely identifies this particular card number. You can use this attribute to check whether two customers who've signed up with you are using the same card number, for example. For payment methods that tokenize card information (Apple Pay, Google Pay), the tokenized number might be provided instead of the underlying card number.

            *As of May 1, 2021, card fingerprint in India for Connect changed to allow two fingerprints for the same card---one for India and one for the rest of the world.*
            """
            funding: Optional[str]
            """
            Card funding type. Can be `credit`, `debit`, `prepaid`, or `unknown`.
            """
            iin: Optional[str]
            """
            Issuer identification number of the card. (For internal use only and not typically available in standard API requests.)
            """
            issuer: Optional[str]
            """
            The name of the card's issuing bank. (For internal use only and not typically available in standard API requests.)
            """
            last4: Optional[str]
            """
            The last four digits of the card.
            """
            networks: Optional[Networks]
            """
            Contains information about card networks that can be used to process the payment.
            """
            preferred_locales: Optional[List[str]]
            """
            EMV tag 5F2D. Preferred languages specified by the integrated circuit chip.
            """
            read_method: Optional[
                Literal[
                    "contact_emv",
                    "contactless_emv",
                    "contactless_magstripe_mode",
                    "magnetic_stripe_fallback",
                    "magnetic_stripe_track2",
                ]
            ]
            """
            How card details were read in this transaction.
            """
            _inner_class_types = {"networks": Networks}

        class Klarna(StripeObject):
            class Dob(StripeObject):
                day: Optional[int]
                """
                The day of birth, between 1 and 31.
                """
                month: Optional[int]
                """
                The month of birth, between 1 and 12.
                """
                year: Optional[int]
                """
                The four-digit year of birth.
                """

            dob: Optional[Dob]
            """
            The customer's date of birth, if provided.
            """
            _inner_class_types = {"dob": Dob}

        class Konbini(StripeObject):
            pass

        class Link(StripeObject):
            email: Optional[str]
            """
            Account owner's email address.
            """
            persistent_token: Optional[str]
            """
            [Deprecated] This is a legacy parameter that no longer has any function.
            """

        class Mobilepay(StripeObject):
            pass

        class Multibanco(StripeObject):
            pass

        class Oxxo(StripeObject):
            pass

        class P24(StripeObject):
            bank: Optional[
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
            The customer's bank, if provided.
            """

        class Paynow(StripeObject):
            pass

        class Paypal(StripeObject):
            payer_email: Optional[str]
            """
            Owner's email. Values are provided by PayPal directly
            (if supported) at the time of authorization or settlement. They cannot be set or mutated.
            """
            payer_id: Optional[str]
            """
            PayPal account PayerID. This identifier uniquely identifies the PayPal customer.
            """

        class Pix(StripeObject):
            pass

        class Promptpay(StripeObject):
            pass

        class RevolutPay(StripeObject):
            pass

        class SepaDebit(StripeObject):
            class GeneratedFrom(StripeObject):
                charge: Optional[ExpandableField["Charge"]]
                """
                The ID of the Charge that generated this PaymentMethod, if any.
                """
                setup_attempt: Optional[ExpandableField["SetupAttempt"]]
                """
                The ID of the SetupAttempt that generated this PaymentMethod, if any.
                """

            bank_code: Optional[str]
            """
            Bank code of bank associated with the bank account.
            """
            branch_code: Optional[str]
            """
            Branch code of bank associated with the bank account.
            """
            country: Optional[str]
            """
            Two-letter ISO code representing the country the bank account is located in.
            """
            fingerprint: Optional[str]
            """
            Uniquely identifies this particular bank account. You can use this attribute to check whether two bank accounts are the same.
            """
            generated_from: Optional[GeneratedFrom]
            """
            Information about the object that generated this PaymentMethod.
            """
            last4: Optional[str]
            """
            Last four characters of the IBAN.
            """
            _inner_class_types = {"generated_from": GeneratedFrom}

        class Sofort(StripeObject):
            country: Optional[str]
            """
            Two-letter ISO code representing the country the bank account is located in.
            """

        class Swish(StripeObject):
            pass

        class Twint(StripeObject):
            pass

        class UsBankAccount(StripeObject):
            class Networks(StripeObject):
                preferred: Optional[str]
                """
                The preferred network.
                """
                supported: List[Literal["ach", "us_domestic_wire"]]
                """
                All supported networks.
                """

            class StatusDetails(StripeObject):
                class Blocked(StripeObject):
                    network_code: Optional[
                        Literal[
                            "R02",
                            "R03",
                            "R04",
                            "R05",
                            "R07",
                            "R08",
                            "R10",
                            "R11",
                            "R16",
                            "R20",
                            "R29",
                            "R31",
                        ]
                    ]
                    """
                    The ACH network code that resulted in this block.
                    """
                    reason: Optional[
                        Literal[
                            "bank_account_closed",
                            "bank_account_frozen",
                            "bank_account_invalid_details",
                            "bank_account_restricted",
                            "bank_account_unusable",
                            "debit_not_authorized",
                        ]
                    ]
                    """
                    The reason why this PaymentMethod's fingerprint has been blocked
                    """

                blocked: Optional[Blocked]
                _inner_class_types = {"blocked": Blocked}

            account_holder_type: Optional[Literal["company", "individual"]]
            """
            Account holder type: individual or company.
            """
            account_type: Optional[Literal["checking", "savings"]]
            """
            Account type: checkings or savings. Defaults to checking if omitted.
            """
            bank_name: Optional[str]
            """
            The name of the bank.
            """
            financial_connections_account: Optional[str]
            """
            The ID of the Financial Connections Account used to create the payment method.
            """
            fingerprint: Optional[str]
            """
            Uniquely identifies this particular bank account. You can use this attribute to check whether two bank accounts are the same.
            """
            last4: Optional[str]
            """
            Last four digits of the bank account number.
            """
            networks: Optional[Networks]
            """
            Contains information about US bank account networks that can be used.
            """
            routing_number: Optional[str]
            """
            Routing number of the bank account.
            """
            status_details: Optional[StatusDetails]
            """
            Contains information about the future reusability of this PaymentMethod.
            """
            _inner_class_types = {
                "networks": Networks,
                "status_details": StatusDetails,
            }

        class WechatPay(StripeObject):
            pass

        class Zip(StripeObject):
            pass

        acss_debit: Optional[AcssDebit]
        affirm: Optional[Affirm]
        afterpay_clearpay: Optional[AfterpayClearpay]
        alipay: Optional[Alipay]
        allow_redisplay: Optional[Literal["always", "limited", "unspecified"]]
        """
        This field indicates whether this payment method can be shown again to its customer in a checkout flow. Stripe products such as Checkout and Elements use this field to determine whether a payment method can be shown as a saved payment method in a checkout flow. The field defaults to “unspecified”.
        """
        amazon_pay: Optional[AmazonPay]
        au_becs_debit: Optional[AuBecsDebit]
        bacs_debit: Optional[BacsDebit]
        bancontact: Optional[Bancontact]
        billing_details: BillingDetails
        blik: Optional[Blik]
        boleto: Optional[Boleto]
        card: Optional[Card]
        card_present: Optional[CardPresent]
        cashapp: Optional[Cashapp]
        customer: Optional[ExpandableField["Customer"]]
        """
        The ID of the Customer to which this PaymentMethod is saved. This will not be set when the PaymentMethod has not been saved to a Customer.
        """
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
            "card",
            "card_present",
            "cashapp",
            "customer_balance",
            "eps",
            "fpx",
            "giropay",
            "grabpay",
            "ideal",
            "interac_present",
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
            "billing_details": BillingDetails,
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

        address: Address
        name: str
        """
        Recipient name.
        """
        phone: Optional[str]
        """
        Recipient phone (including extension).
        """
        _inner_class_types = {"address": Address}

    class CreateParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        payment_method: NotRequired[str]
        """
        ID of an existing PaymentMethod.
        """
        payment_method_data: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodData"
        ]
        """
        If provided, this hash will be used to create a PaymentMethod.
        """
        return_url: NotRequired[str]
        """
        Return URL used to confirm the Intent.
        """
        setup_future_usage: NotRequired[Literal["off_session", "on_session"]]
        """
        Indicates that you intend to make future payments with this ConfirmationToken's payment method.

        The presence of this property will [attach the payment method](https://stripe.com/docs/payments/save-during-payment) to the PaymentIntent's Customer, if present, after the PaymentIntent is confirmed and any required actions from the user are complete.
        """
        shipping: NotRequired["ConfirmationToken.CreateParamsShipping"]
        """
        Shipping information for this ConfirmationToken.
        """

    class CreateParamsPaymentMethodData(TypedDict):
        acss_debit: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataAcssDebit"
        ]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataAlipay"
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
            "ConfirmationToken.CreateParamsPaymentMethodDataAmazonPay"
        ]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        cashapp: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer_balance: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired["ConfirmationToken.CreateParamsPaymentMethodDataEps"]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        fpx: NotRequired["ConfirmationToken.CreateParamsPaymentMethodDataFpx"]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataIdeal"
        ]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataLink"
        ]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataMobilepay"
        ]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataOxxo"
        ]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired["ConfirmationToken.CreateParamsPaymentMethodDataP24"]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        paynow: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired["ConfirmationToken.CreateParamsPaymentMethodDataPix"]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataRevolutPay"
        ]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataSwish"
        ]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataTwint"
        ]
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
            "ConfirmationToken.CreateParamsPaymentMethodDataUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired[
            "ConfirmationToken.CreateParamsPaymentMethodDataWechatPay"
        ]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired["ConfirmationToken.CreateParamsPaymentMethodDataZip"]
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
            "Literal['']|ConfirmationToken.CreateParamsPaymentMethodDataBillingDetailsAddress"
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
            "ConfirmationToken.CreateParamsPaymentMethodDataKlarnaDob"
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

    class CreateParamsShipping(TypedDict):
        address: "ConfirmationToken.CreateParamsShippingAddress"
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    expires_at: Optional[int]
    """
    Time at which this ConfirmationToken expires and can no longer be used to confirm a PaymentIntent or SetupIntent.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    mandate_data: Optional[MandateData]
    """
    Data used for generating a Mandate.
    """
    object: Literal["confirmation_token"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    payment_intent: Optional[str]
    """
    ID of the PaymentIntent that this ConfirmationToken was used to confirm, or null if this ConfirmationToken has not yet been used.
    """
    payment_method_options: Optional[PaymentMethodOptions]
    """
    Payment-method-specific configuration for this ConfirmationToken.
    """
    payment_method_preview: Optional[PaymentMethodPreview]
    """
    Payment details collected by the Payment Element, used to create a PaymentMethod when a PaymentIntent or SetupIntent is confirmed with this ConfirmationToken.
    """
    return_url: Optional[str]
    """
    Return URL used to confirm the Intent.
    """
    setup_future_usage: Optional[Literal["off_session", "on_session"]]
    """
    Indicates that you intend to make future payments with this ConfirmationToken's payment method.

    The presence of this property will [attach the payment method](https://stripe.com/docs/payments/save-during-payment) to the PaymentIntent's Customer, if present, after the PaymentIntent is confirmed and any required actions from the user are complete.
    """
    setup_intent: Optional[str]
    """
    ID of the SetupIntent that this ConfirmationToken was used to confirm, or null if this ConfirmationToken has not yet been used.
    """
    shipping: Optional[Shipping]
    """
    Shipping information collected on this ConfirmationToken.
    """
    use_stripe_sdk: bool
    """
    Indicates whether the Stripe SDK is used to handle confirmation flow. Defaults to `true` on ConfirmationToken.
    """

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["ConfirmationToken.RetrieveParams"]
    ) -> "ConfirmationToken":
        """
        Retrieves an existing ConfirmationToken object
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["ConfirmationToken.RetrieveParams"]
    ) -> "ConfirmationToken":
        """
        Retrieves an existing ConfirmationToken object
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    class TestHelpers(APIResourceTestHelpers["ConfirmationToken"]):
        _resource_cls: Type["ConfirmationToken"]

        @classmethod
        def create(
            cls, **params: Unpack["ConfirmationToken.CreateParams"]
        ) -> "ConfirmationToken":
            """
            Creates a test mode Confirmation Token server side for your integration tests.
            """
            return cast(
                "ConfirmationToken",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/confirmation_tokens",
                    params=params,
                ),
            )

        @classmethod
        async def create_async(
            cls, **params: Unpack["ConfirmationToken.CreateParams"]
        ) -> "ConfirmationToken":
            """
            Creates a test mode Confirmation Token server side for your integration tests.
            """
            return cast(
                "ConfirmationToken",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/confirmation_tokens",
                    params=params,
                ),
            )

    @property
    def test_helpers(self):
        return self.TestHelpers(self)

    _inner_class_types = {
        "mandate_data": MandateData,
        "payment_method_options": PaymentMethodOptions,
        "payment_method_preview": PaymentMethodPreview,
        "shipping": Shipping,
    }


ConfirmationToken.TestHelpers._resource_cls = ConfirmationToken
