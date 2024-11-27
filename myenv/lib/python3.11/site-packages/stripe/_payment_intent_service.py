# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._payment_intent import PaymentIntent
from stripe._request_options import RequestOptions
from stripe._search_result_object import SearchResultObject
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class PaymentIntentService(StripeService):
    class ApplyCustomerBalanceParams(TypedDict):
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

    class CancelParams(TypedDict):
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

    class CaptureParams(TypedDict):
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
        transfer_data: NotRequired[
            "PaymentIntentService.CaptureParamsTransferData"
        ]
        """
        The parameters that you can use to automatically create a transfer after the payment
        is captured. Learn more about the [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """

    class CaptureParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount that will be transferred automatically when a charge succeeds.
        """

    class ConfirmParams(TypedDict):
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
            "Literal['']|PaymentIntentService.ConfirmParamsMandateData"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodData"
        ]
        """
        If provided, this hash will be used to create a PaymentMethod. The new PaymentMethod will appear
        in the [payment_method](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-payment_method)
        property on the PaymentIntent.
        """
        payment_method_options: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodOptions"
        ]
        """
        Payment method-specific configuration for this PaymentIntent.
        """
        payment_method_types: NotRequired[List[str]]
        """
        The list of payment method types (for example, a card) that this PaymentIntent can use. Use `automatic_payment_methods` to manage payment methods from the [Stripe Dashboard](https://dashboard.stripe.com/settings/payment_methods).
        """
        radar_options: NotRequired[
            "PaymentIntentService.ConfirmParamsRadarOptions"
        ]
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
            "Literal['']|PaymentIntentService.ConfirmParamsShipping"
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
            "PaymentIntentService.ConfirmParamsMandateDataCustomerAcceptance"
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
            "PaymentIntentService.ConfirmParamsMandateDataCustomerAcceptanceOffline"
        ]
        """
        If this is a Mandate accepted offline, this hash contains details about the offline acceptance.
        """
        online: NotRequired[
            "PaymentIntentService.ConfirmParamsMandateDataCustomerAcceptanceOnline"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodDataAcssDebit"
        ]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataAlipay"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodDataAmazonPay"
        ]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        cashapp: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer_balance: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataEps"
        ]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        fpx: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataFpx"
        ]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataIdeal"
        ]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataLink"
        ]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataMobilepay"
        ]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataOxxo"
        ]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataP24"
        ]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        paynow: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataPix"
        ]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataRevolutPay"
        ]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataSwish"
        ]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataTwint"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodDataUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataWechatPay"
        ]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodDataZip"
        ]
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
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodDataBillingDetailsAddress"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodDataKlarnaDob"
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
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsAcssDebit"
        ]
        """
        If this is a `acss_debit` PaymentMethod, this sub-hash contains details about the ACSS Debit payment method options.
        """
        affirm: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this sub-hash contains details about the Affirm payment method options.
        """
        afterpay_clearpay: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsAfterpayClearpay"
        ]
        """
        If this is a `afterpay_clearpay` PaymentMethod, this sub-hash contains details about the Afterpay Clearpay payment method options.
        """
        alipay: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsAlipay"
        ]
        """
        If this is a `alipay` PaymentMethod, this sub-hash contains details about the Alipay payment method options.
        """
        amazon_pay: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsAmazonPay"
        ]
        """
        If this is a `amazon_pay` PaymentMethod, this sub-hash contains details about the Amazon Pay payment method options.
        """
        au_becs_debit: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsAuBecsDebit"
        ]
        """
        If this is a `au_becs_debit` PaymentMethod, this sub-hash contains details about the AU BECS Direct Debit payment method options.
        """
        bacs_debit: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this sub-hash contains details about the BACS Debit payment method options.
        """
        bancontact: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this sub-hash contains details about the Bancontact payment method options.
        """
        blik: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this sub-hash contains details about the BLIK payment method options.
        """
        boleto: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this sub-hash contains details about the Boleto payment method options.
        """
        card: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsCard"
        ]
        """
        Configuration for any card payments attempted on this PaymentIntent.
        """
        card_present: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsCardPresent"
        ]
        """
        If this is a `card_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        cashapp: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this sub-hash contains details about the Cash App Pay payment method options.
        """
        customer_balance: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsCustomerBalance"
        ]
        """
        If this is a `customer balance` PaymentMethod, this sub-hash contains details about the customer balance payment method options.
        """
        eps: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsEps"
        ]
        """
        If this is a `eps` PaymentMethod, this sub-hash contains details about the EPS payment method options.
        """
        fpx: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsFpx"
        ]
        """
        If this is a `fpx` PaymentMethod, this sub-hash contains details about the FPX payment method options.
        """
        giropay: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this sub-hash contains details about the Giropay payment method options.
        """
        grabpay: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this sub-hash contains details about the Grabpay payment method options.
        """
        ideal: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsIdeal"
        ]
        """
        If this is a `ideal` PaymentMethod, this sub-hash contains details about the Ideal payment method options.
        """
        interac_present: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsInteracPresent"
        ]
        """
        If this is a `interac_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        klarna: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this sub-hash contains details about the Klarna payment method options.
        """
        konbini: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this sub-hash contains details about the Konbini payment method options.
        """
        link: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsLink"
        ]
        """
        If this is a `link` PaymentMethod, this sub-hash contains details about the Link payment method options.
        """
        mobilepay: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsMobilepay"
        ]
        """
        If this is a `MobilePay` PaymentMethod, this sub-hash contains details about the MobilePay payment method options.
        """
        multibanco: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this sub-hash contains details about the Multibanco payment method options.
        """
        oxxo: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsOxxo"
        ]
        """
        If this is a `oxxo` PaymentMethod, this sub-hash contains details about the OXXO payment method options.
        """
        p24: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsP24"
        ]
        """
        If this is a `p24` PaymentMethod, this sub-hash contains details about the Przelewy24 payment method options.
        """
        paynow: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this sub-hash contains details about the PayNow payment method options.
        """
        paypal: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this sub-hash contains details about the PayPal payment method options.
        """
        pix: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsPix"
        ]
        """
        If this is a `pix` PaymentMethod, this sub-hash contains details about the Pix payment method options.
        """
        promptpay: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this sub-hash contains details about the PromptPay payment method options.
        """
        revolut_pay: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsRevolutPay"
        ]
        """
        If this is a `revolut_pay` PaymentMethod, this sub-hash contains details about the Revolut Pay payment method options.
        """
        sepa_debit: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentIntent, this sub-hash contains details about the SEPA Debit payment method options.
        """
        sofort: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this sub-hash contains details about the SOFORT payment method options.
        """
        swish: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsSwish"
        ]
        """
        If this is a `Swish` PaymentMethod, this sub-hash contains details about the Swish payment method options.
        """
        twint: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsTwint"
        ]
        """
        If this is a `twint` PaymentMethod, this sub-hash contains details about the TWINT payment method options.
        """
        us_bank_account: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If this is a `us_bank_account` PaymentMethod, this sub-hash contains details about the US bank account payment method options.
        """
        wechat_pay: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsWechatPay"
        ]
        """
        If this is a `wechat_pay` PaymentMethod, this sub-hash contains details about the WeChat Pay payment method options.
        """
        zip: NotRequired[
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsZip"
        ]
        """
        If this is a `zip` PaymentMethod, this sub-hash contains details about the Zip payment method options.
        """

    class ConfirmParamsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsAcssDebitMandateOptions"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsCardInstallments"
        ]
        """
        Installment configuration for payments attempted on this PaymentIntent (Mexico Only).

        For more information, see the [installments integration guide](https://stripe.com/docs/payments/installments).
        """
        mandate_options: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsCardMandateOptions"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsCardThreeDSecure"
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
            "Literal['']|PaymentIntentService.ConfirmParamsPaymentMethodOptionsCardInstallmentsPlan"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsCardPresentRouting"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsCustomerBalanceBankTransfer"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsSepaDebitMandateOptions"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        mandate_options: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsUsBankAccountMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        networks: NotRequired[
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsUsBankAccountNetworks"
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
            "PaymentIntentService.ConfirmParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
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
        address: "PaymentIntentService.ConfirmParamsShippingAddress"
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

    class CreateParams(TypedDict):
        amount: int
        """
        Amount intended to be collected by this PaymentIntent. A positive integer representing how much to charge in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) (e.g., 100 cents to charge $1.00 or 100 to charge ¥100, a zero-decimal currency). The minimum amount is $0.50 US or [equivalent in charge currency](https://stripe.com/docs/currencies#minimum-and-maximum-charge-amounts). The amount value supports up to eight digits (e.g., a value of 99999999 for a USD charge of $999,999.99).
        """
        application_fee_amount: NotRequired[int]
        """
        The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. The amount of the application fee collected will be capped at the total payment amount. For more information, see the PaymentIntents [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """
        automatic_payment_methods: NotRequired[
            "PaymentIntentService.CreateParamsAutomaticPaymentMethods"
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
            "Literal['']|PaymentIntentService.CreateParamsMandateData"
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
            "PaymentIntentService.CreateParamsPaymentMethodData"
        ]
        """
        If provided, this hash will be used to create a PaymentMethod. The new PaymentMethod will appear
        in the [payment_method](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-payment_method)
        property on the PaymentIntent.
        """
        payment_method_options: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodOptions"
        ]
        """
        Payment method-specific configuration for this PaymentIntent.
        """
        payment_method_types: NotRequired[List[str]]
        """
        The list of payment method types (for example, a card) that this PaymentIntent can use. If you don't provide this, it defaults to ["card"]. Use `automatic_payment_methods` to manage payment methods from the [Stripe Dashboard](https://dashboard.stripe.com/settings/payment_methods).
        """
        radar_options: NotRequired[
            "PaymentIntentService.CreateParamsRadarOptions"
        ]
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
        shipping: NotRequired["PaymentIntentService.CreateParamsShipping"]
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
        transfer_data: NotRequired[
            "PaymentIntentService.CreateParamsTransferData"
        ]
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
            "PaymentIntentService.CreateParamsMandateDataCustomerAcceptance"
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
            "PaymentIntentService.CreateParamsMandateDataCustomerAcceptanceOffline"
        ]
        """
        If this is a Mandate accepted offline, this hash contains details about the offline acceptance.
        """
        online: NotRequired[
            "PaymentIntentService.CreateParamsMandateDataCustomerAcceptanceOnline"
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
            "PaymentIntentService.CreateParamsPaymentMethodDataAcssDebit"
        ]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataAlipay"
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
            "PaymentIntentService.CreateParamsPaymentMethodDataAmazonPay"
        ]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        cashapp: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer_balance: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataEps"
        ]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        fpx: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataFpx"
        ]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataIdeal"
        ]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataLink"
        ]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataMobilepay"
        ]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataOxxo"
        ]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataP24"
        ]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        paynow: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataPix"
        ]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataRevolutPay"
        ]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataSwish"
        ]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataTwint"
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
            "PaymentIntentService.CreateParamsPaymentMethodDataUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataWechatPay"
        ]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodDataZip"
        ]
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
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodDataBillingDetailsAddress"
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
            "PaymentIntentService.CreateParamsPaymentMethodDataKlarnaDob"
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
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsAcssDebit"
        ]
        """
        If this is a `acss_debit` PaymentMethod, this sub-hash contains details about the ACSS Debit payment method options.
        """
        affirm: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this sub-hash contains details about the Affirm payment method options.
        """
        afterpay_clearpay: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsAfterpayClearpay"
        ]
        """
        If this is a `afterpay_clearpay` PaymentMethod, this sub-hash contains details about the Afterpay Clearpay payment method options.
        """
        alipay: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsAlipay"
        ]
        """
        If this is a `alipay` PaymentMethod, this sub-hash contains details about the Alipay payment method options.
        """
        amazon_pay: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsAmazonPay"
        ]
        """
        If this is a `amazon_pay` PaymentMethod, this sub-hash contains details about the Amazon Pay payment method options.
        """
        au_becs_debit: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsAuBecsDebit"
        ]
        """
        If this is a `au_becs_debit` PaymentMethod, this sub-hash contains details about the AU BECS Direct Debit payment method options.
        """
        bacs_debit: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this sub-hash contains details about the BACS Debit payment method options.
        """
        bancontact: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this sub-hash contains details about the Bancontact payment method options.
        """
        blik: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this sub-hash contains details about the BLIK payment method options.
        """
        boleto: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this sub-hash contains details about the Boleto payment method options.
        """
        card: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsCard"
        ]
        """
        Configuration for any card payments attempted on this PaymentIntent.
        """
        card_present: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsCardPresent"
        ]
        """
        If this is a `card_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        cashapp: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this sub-hash contains details about the Cash App Pay payment method options.
        """
        customer_balance: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsCustomerBalance"
        ]
        """
        If this is a `customer balance` PaymentMethod, this sub-hash contains details about the customer balance payment method options.
        """
        eps: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsEps"
        ]
        """
        If this is a `eps` PaymentMethod, this sub-hash contains details about the EPS payment method options.
        """
        fpx: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsFpx"
        ]
        """
        If this is a `fpx` PaymentMethod, this sub-hash contains details about the FPX payment method options.
        """
        giropay: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this sub-hash contains details about the Giropay payment method options.
        """
        grabpay: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this sub-hash contains details about the Grabpay payment method options.
        """
        ideal: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsIdeal"
        ]
        """
        If this is a `ideal` PaymentMethod, this sub-hash contains details about the Ideal payment method options.
        """
        interac_present: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsInteracPresent"
        ]
        """
        If this is a `interac_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        klarna: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this sub-hash contains details about the Klarna payment method options.
        """
        konbini: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this sub-hash contains details about the Konbini payment method options.
        """
        link: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsLink"
        ]
        """
        If this is a `link` PaymentMethod, this sub-hash contains details about the Link payment method options.
        """
        mobilepay: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsMobilepay"
        ]
        """
        If this is a `MobilePay` PaymentMethod, this sub-hash contains details about the MobilePay payment method options.
        """
        multibanco: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this sub-hash contains details about the Multibanco payment method options.
        """
        oxxo: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsOxxo"
        ]
        """
        If this is a `oxxo` PaymentMethod, this sub-hash contains details about the OXXO payment method options.
        """
        p24: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsP24"
        ]
        """
        If this is a `p24` PaymentMethod, this sub-hash contains details about the Przelewy24 payment method options.
        """
        paynow: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this sub-hash contains details about the PayNow payment method options.
        """
        paypal: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this sub-hash contains details about the PayPal payment method options.
        """
        pix: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsPix"
        ]
        """
        If this is a `pix` PaymentMethod, this sub-hash contains details about the Pix payment method options.
        """
        promptpay: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this sub-hash contains details about the PromptPay payment method options.
        """
        revolut_pay: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsRevolutPay"
        ]
        """
        If this is a `revolut_pay` PaymentMethod, this sub-hash contains details about the Revolut Pay payment method options.
        """
        sepa_debit: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentIntent, this sub-hash contains details about the SEPA Debit payment method options.
        """
        sofort: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this sub-hash contains details about the SOFORT payment method options.
        """
        swish: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsSwish"
        ]
        """
        If this is a `Swish` PaymentMethod, this sub-hash contains details about the Swish payment method options.
        """
        twint: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsTwint"
        ]
        """
        If this is a `twint` PaymentMethod, this sub-hash contains details about the TWINT payment method options.
        """
        us_bank_account: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If this is a `us_bank_account` PaymentMethod, this sub-hash contains details about the US bank account payment method options.
        """
        wechat_pay: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsWechatPay"
        ]
        """
        If this is a `wechat_pay` PaymentMethod, this sub-hash contains details about the WeChat Pay payment method options.
        """
        zip: NotRequired[
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsZip"
        ]
        """
        If this is a `zip` PaymentMethod, this sub-hash contains details about the Zip payment method options.
        """

    class CreateParamsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodOptionsAcssDebitMandateOptions"
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
            "PaymentIntentService.CreateParamsPaymentMethodOptionsCardInstallments"
        ]
        """
        Installment configuration for payments attempted on this PaymentIntent (Mexico Only).

        For more information, see the [installments integration guide](https://stripe.com/docs/payments/installments).
        """
        mandate_options: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodOptionsCardMandateOptions"
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
            "PaymentIntentService.CreateParamsPaymentMethodOptionsCardThreeDSecure"
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
            "Literal['']|PaymentIntentService.CreateParamsPaymentMethodOptionsCardInstallmentsPlan"
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
            "PaymentIntentService.CreateParamsPaymentMethodOptionsCardPresentRouting"
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
            "PaymentIntentService.CreateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions"
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
            "PaymentIntentService.CreateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires"
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
            "PaymentIntentService.CreateParamsPaymentMethodOptionsCustomerBalanceBankTransfer"
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
            "PaymentIntentService.CreateParamsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
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
            "PaymentIntentService.CreateParamsPaymentMethodOptionsSepaDebitMandateOptions"
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
            "PaymentIntentService.CreateParamsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        mandate_options: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodOptionsUsBankAccountMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        networks: NotRequired[
            "PaymentIntentService.CreateParamsPaymentMethodOptionsUsBankAccountNetworks"
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
            "PaymentIntentService.CreateParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
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
        address: "PaymentIntentService.CreateParamsShippingAddress"
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

    class IncrementAuthorizationParams(TypedDict):
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
            "PaymentIntentService.IncrementAuthorizationParamsTransferData"
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

    class ListParams(TypedDict):
        created: NotRequired["PaymentIntentService.ListParamsCreated|int"]
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

    class RetrieveParams(TypedDict):
        client_secret: NotRequired[str]
        """
        The client secret of the PaymentIntent. We require it if you use a publishable key to retrieve the source.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class SearchParams(TypedDict):
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

    class UpdateParams(TypedDict):
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
            "PaymentIntentService.UpdateParamsPaymentMethodData"
        ]
        """
        If provided, this hash will be used to create a PaymentMethod. The new PaymentMethod will appear
        in the [payment_method](https://stripe.com/docs/api/payment_intents/object#payment_intent_object-payment_method)
        property on the PaymentIntent.
        """
        payment_method_options: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptions"
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
        shipping: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsShipping"
        ]
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
        transfer_data: NotRequired[
            "PaymentIntentService.UpdateParamsTransferData"
        ]
        """
        Use this parameter to automatically create a Transfer when the payment succeeds. Learn more about the [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies the resulting payment as part of a group. You can only provide `transfer_group` if it hasn't been set. Learn more about the [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """

    class UpdateParamsPaymentMethodData(TypedDict):
        acss_debit: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataAcssDebit"
        ]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataAlipay"
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
            "PaymentIntentService.UpdateParamsPaymentMethodDataAmazonPay"
        ]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        cashapp: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer_balance: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataEps"
        ]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        fpx: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataFpx"
        ]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataIdeal"
        ]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataLink"
        ]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataMobilepay"
        ]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataOxxo"
        ]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataP24"
        ]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        paynow: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataPix"
        ]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataRevolutPay"
        ]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataSwish"
        ]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataTwint"
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
            "PaymentIntentService.UpdateParamsPaymentMethodDataUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataWechatPay"
        ]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataZip"
        ]
        """
        If this is a `zip` PaymentMethod, this hash contains details about the Zip payment method.
        """

    class UpdateParamsPaymentMethodDataAcssDebit(TypedDict):
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

    class UpdateParamsPaymentMethodDataAffirm(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataAfterpayClearpay(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataAlipay(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataAmazonPay(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataAuBecsDebit(TypedDict):
        account_number: str
        """
        The account number for the bank account.
        """
        bsb_number: str
        """
        Bank-State-Branch number of the bank account.
        """

    class UpdateParamsPaymentMethodDataBacsDebit(TypedDict):
        account_number: NotRequired[str]
        """
        Account number of the bank account that the funds will be debited from.
        """
        sort_code: NotRequired[str]
        """
        Sort code of the bank account. (e.g., `10-20-30`)
        """

    class UpdateParamsPaymentMethodDataBancontact(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataBillingDetails(TypedDict):
        address: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodDataBillingDetailsAddress"
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

    class UpdateParamsPaymentMethodDataBillingDetailsAddress(TypedDict):
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

    class UpdateParamsPaymentMethodDataBlik(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataBoleto(TypedDict):
        tax_id: str
        """
        The tax ID of the customer (CPF for individual consumers or CNPJ for businesses consumers)
        """

    class UpdateParamsPaymentMethodDataCashapp(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataCustomerBalance(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataEps(TypedDict):
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

    class UpdateParamsPaymentMethodDataFpx(TypedDict):
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

    class UpdateParamsPaymentMethodDataGiropay(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataGrabpay(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataIdeal(TypedDict):
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

    class UpdateParamsPaymentMethodDataInteracPresent(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataKlarna(TypedDict):
        dob: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodDataKlarnaDob"
        ]
        """
        Customer's date of birth
        """

    class UpdateParamsPaymentMethodDataKlarnaDob(TypedDict):
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

    class UpdateParamsPaymentMethodDataKonbini(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataLink(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataMobilepay(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataMultibanco(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataOxxo(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataP24(TypedDict):
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

    class UpdateParamsPaymentMethodDataPaynow(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataPaypal(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataPix(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataPromptpay(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataRadarOptions(TypedDict):
        session: NotRequired[str]
        """
        A [Radar Session](https://stripe.com/docs/radar/radar-session) is a snapshot of the browser metadata and device details that help Radar make more accurate predictions on your payments.
        """

    class UpdateParamsPaymentMethodDataRevolutPay(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataSepaDebit(TypedDict):
        iban: str
        """
        IBAN of the bank account.
        """

    class UpdateParamsPaymentMethodDataSofort(TypedDict):
        country: Literal["AT", "BE", "DE", "ES", "IT", "NL"]
        """
        Two-letter ISO code representing the country the bank account is located in.
        """

    class UpdateParamsPaymentMethodDataSwish(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataTwint(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataUsBankAccount(TypedDict):
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

    class UpdateParamsPaymentMethodDataWechatPay(TypedDict):
        pass

    class UpdateParamsPaymentMethodDataZip(TypedDict):
        pass

    class UpdateParamsPaymentMethodOptions(TypedDict):
        acss_debit: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsAcssDebit"
        ]
        """
        If this is a `acss_debit` PaymentMethod, this sub-hash contains details about the ACSS Debit payment method options.
        """
        affirm: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this sub-hash contains details about the Affirm payment method options.
        """
        afterpay_clearpay: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsAfterpayClearpay"
        ]
        """
        If this is a `afterpay_clearpay` PaymentMethod, this sub-hash contains details about the Afterpay Clearpay payment method options.
        """
        alipay: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsAlipay"
        ]
        """
        If this is a `alipay` PaymentMethod, this sub-hash contains details about the Alipay payment method options.
        """
        amazon_pay: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsAmazonPay"
        ]
        """
        If this is a `amazon_pay` PaymentMethod, this sub-hash contains details about the Amazon Pay payment method options.
        """
        au_becs_debit: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsAuBecsDebit"
        ]
        """
        If this is a `au_becs_debit` PaymentMethod, this sub-hash contains details about the AU BECS Direct Debit payment method options.
        """
        bacs_debit: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this sub-hash contains details about the BACS Debit payment method options.
        """
        bancontact: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this sub-hash contains details about the Bancontact payment method options.
        """
        blik: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this sub-hash contains details about the BLIK payment method options.
        """
        boleto: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this sub-hash contains details about the Boleto payment method options.
        """
        card: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsCard"
        ]
        """
        Configuration for any card payments attempted on this PaymentIntent.
        """
        card_present: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsCardPresent"
        ]
        """
        If this is a `card_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        cashapp: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this sub-hash contains details about the Cash App Pay payment method options.
        """
        customer_balance: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsCustomerBalance"
        ]
        """
        If this is a `customer balance` PaymentMethod, this sub-hash contains details about the customer balance payment method options.
        """
        eps: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsEps"
        ]
        """
        If this is a `eps` PaymentMethod, this sub-hash contains details about the EPS payment method options.
        """
        fpx: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsFpx"
        ]
        """
        If this is a `fpx` PaymentMethod, this sub-hash contains details about the FPX payment method options.
        """
        giropay: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this sub-hash contains details about the Giropay payment method options.
        """
        grabpay: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this sub-hash contains details about the Grabpay payment method options.
        """
        ideal: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsIdeal"
        ]
        """
        If this is a `ideal` PaymentMethod, this sub-hash contains details about the Ideal payment method options.
        """
        interac_present: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsInteracPresent"
        ]
        """
        If this is a `interac_present` PaymentMethod, this sub-hash contains details about the Card Present payment method options.
        """
        klarna: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this sub-hash contains details about the Klarna payment method options.
        """
        konbini: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this sub-hash contains details about the Konbini payment method options.
        """
        link: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsLink"
        ]
        """
        If this is a `link` PaymentMethod, this sub-hash contains details about the Link payment method options.
        """
        mobilepay: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsMobilepay"
        ]
        """
        If this is a `MobilePay` PaymentMethod, this sub-hash contains details about the MobilePay payment method options.
        """
        multibanco: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this sub-hash contains details about the Multibanco payment method options.
        """
        oxxo: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsOxxo"
        ]
        """
        If this is a `oxxo` PaymentMethod, this sub-hash contains details about the OXXO payment method options.
        """
        p24: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsP24"
        ]
        """
        If this is a `p24` PaymentMethod, this sub-hash contains details about the Przelewy24 payment method options.
        """
        paynow: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this sub-hash contains details about the PayNow payment method options.
        """
        paypal: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this sub-hash contains details about the PayPal payment method options.
        """
        pix: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsPix"
        ]
        """
        If this is a `pix` PaymentMethod, this sub-hash contains details about the Pix payment method options.
        """
        promptpay: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this sub-hash contains details about the PromptPay payment method options.
        """
        revolut_pay: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsRevolutPay"
        ]
        """
        If this is a `revolut_pay` PaymentMethod, this sub-hash contains details about the Revolut Pay payment method options.
        """
        sepa_debit: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentIntent, this sub-hash contains details about the SEPA Debit payment method options.
        """
        sofort: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this sub-hash contains details about the SOFORT payment method options.
        """
        swish: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsSwish"
        ]
        """
        If this is a `Swish` PaymentMethod, this sub-hash contains details about the Swish payment method options.
        """
        twint: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsTwint"
        ]
        """
        If this is a `twint` PaymentMethod, this sub-hash contains details about the TWINT payment method options.
        """
        us_bank_account: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If this is a `us_bank_account` PaymentMethod, this sub-hash contains details about the US bank account payment method options.
        """
        wechat_pay: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsWechatPay"
        ]
        """
        If this is a `wechat_pay` PaymentMethod, this sub-hash contains details about the WeChat Pay payment method options.
        """
        zip: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsZip"
        ]
        """
        If this is a `zip` PaymentMethod, this sub-hash contains details about the Zip payment method options.
        """

    class UpdateParamsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsAcssDebitMandateOptions"
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

    class UpdateParamsPaymentMethodOptionsAcssDebitMandateOptions(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsAffirm(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsAfterpayClearpay(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsAlipay(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsAmazonPay(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsAuBecsDebit(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsBacsDebit(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsBancontact(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsBlik(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsBoleto(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsCard(TypedDict):
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
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsCardInstallments"
        ]
        """
        Installment configuration for payments attempted on this PaymentIntent (Mexico Only).

        For more information, see the [installments integration guide](https://stripe.com/docs/payments/installments).
        """
        mandate_options: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsCardMandateOptions"
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
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsCardThreeDSecure"
        ]
        """
        If 3D Secure authentication was performed with a third-party provider,
        the authentication details to use for this payment.
        """

    class UpdateParamsPaymentMethodOptionsCardInstallments(TypedDict):
        enabled: NotRequired[bool]
        """
        Setting to true enables installments for this PaymentIntent.
        This will cause the response to contain a list of available installment plans.
        Setting to false will prevent any selected plan from applying to a charge.
        """
        plan: NotRequired[
            "Literal['']|PaymentIntentService.UpdateParamsPaymentMethodOptionsCardInstallmentsPlan"
        ]
        """
        The selected installment plan to use for this payment attempt.
        This parameter can only be provided during confirmation.
        """

    class UpdateParamsPaymentMethodOptionsCardInstallmentsPlan(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsCardMandateOptions(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsCardPresent(TypedDict):
        request_extended_authorization: NotRequired[bool]
        """
        Request ability to capture this payment beyond the standard [authorization validity window](https://stripe.com/docs/terminal/features/extended-authorizations#authorization-validity)
        """
        request_incremental_authorization_support: NotRequired[bool]
        """
        Request ability to [increment](https://stripe.com/docs/terminal/features/incremental-authorizations) this PaymentIntent if the combination of MCC and card brand is eligible. Check [incremental_authorization_supported](https://stripe.com/docs/api/charges/object#charge_object-payment_method_details-card_present-incremental_authorization_supported) in the [Confirm](https://stripe.com/docs/api/payment_intents/confirm) response to verify support.
        """
        routing: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsCardPresentRouting"
        ]
        """
        Network routing priority on co-branded EMV cards supporting domestic debit and international card schemes.
        """

    class UpdateParamsPaymentMethodOptionsCardPresentRouting(TypedDict):
        requested_priority: NotRequired[Literal["domestic", "international"]]
        """
        Routing requested priority
        """

    class UpdateParamsPaymentMethodOptionsCardThreeDSecure(TypedDict):
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
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions"
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

    class UpdateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions(
        TypedDict,
    ):
        cartes_bancaires: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires"
        ]
        """
        Cartes Bancaires-specific 3DS fields.
        """

    class UpdateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires(
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

    class UpdateParamsPaymentMethodOptionsCashapp(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsCustomerBalance(TypedDict):
        bank_transfer: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsCustomerBalanceBankTransfer"
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

    class UpdateParamsPaymentMethodOptionsCustomerBalanceBankTransfer(
        TypedDict,
    ):
        eu_bank_transfer: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
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

    class UpdateParamsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer(
        TypedDict,
    ):
        country: str
        """
        The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
        """

    class UpdateParamsPaymentMethodOptionsEps(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class UpdateParamsPaymentMethodOptionsFpx(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class UpdateParamsPaymentMethodOptionsGiropay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class UpdateParamsPaymentMethodOptionsGrabpay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class UpdateParamsPaymentMethodOptionsIdeal(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsInteracPresent(TypedDict):
        pass

    class UpdateParamsPaymentMethodOptionsKlarna(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsKonbini(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsLink(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsMobilepay(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsMultibanco(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class UpdateParamsPaymentMethodOptionsOxxo(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsP24(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsPaynow(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class UpdateParamsPaymentMethodOptionsPaypal(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsPix(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsPromptpay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class UpdateParamsPaymentMethodOptionsRevolutPay(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsSepaDebit(TypedDict):
        mandate_options: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsSepaDebitMandateOptions"
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

    class UpdateParamsPaymentMethodOptionsSepaDebitMandateOptions(TypedDict):
        pass

    class UpdateParamsPaymentMethodOptionsSofort(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsSwish(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsTwint(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class UpdateParamsPaymentMethodOptionsUsBankAccount(TypedDict):
        financial_connections: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        mandate_options: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsUsBankAccountMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        networks: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsUsBankAccountNetworks"
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

    class UpdateParamsPaymentMethodOptionsUsBankAccountFinancialConnections(
        TypedDict,
    ):
        filters: NotRequired[
            "PaymentIntentService.UpdateParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
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

    class UpdateParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters(
        TypedDict,
    ):
        account_subcategories: NotRequired[
            List[Literal["checking", "savings"]]
        ]
        """
        The account subcategories to use to filter for selectable accounts. Valid subcategories are `checking` and `savings`.
        """

    class UpdateParamsPaymentMethodOptionsUsBankAccountMandateOptions(
        TypedDict,
    ):
        collection_method: NotRequired["Literal['']|Literal['paper']"]
        """
        The method used to collect offline mandate customer acceptance.
        """

    class UpdateParamsPaymentMethodOptionsUsBankAccountNetworks(TypedDict):
        requested: NotRequired[List[Literal["ach", "us_domestic_wire"]]]
        """
        Triggers validations to run across the selected networks
        """

    class UpdateParamsPaymentMethodOptionsWechatPay(TypedDict):
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

    class UpdateParamsPaymentMethodOptionsZip(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).

        If you've already set `setup_future_usage` and you're performing a request using a publishable key, you can only update the value from `on_session` to `off_session`.
        """

    class UpdateParamsShipping(TypedDict):
        address: "PaymentIntentService.UpdateParamsShippingAddress"
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

    class UpdateParamsShippingAddress(TypedDict):
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

    class UpdateParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount that will be transferred automatically when a charge succeeds.
        """

    class VerifyMicrodepositsParams(TypedDict):
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

    def list(
        self,
        params: "PaymentIntentService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentIntent]:
        """
        Returns a list of PaymentIntents.
        """
        return cast(
            ListObject[PaymentIntent],
            self._request(
                "get",
                "/v1/payment_intents",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "PaymentIntentService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentIntent]:
        """
        Returns a list of PaymentIntents.
        """
        return cast(
            ListObject[PaymentIntent],
            await self._request_async(
                "get",
                "/v1/payment_intents",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "PaymentIntentService.CreateParams",
        options: RequestOptions = {},
    ) -> PaymentIntent:
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
            PaymentIntent,
            self._request(
                "post",
                "/v1/payment_intents",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "PaymentIntentService.CreateParams",
        options: RequestOptions = {},
    ) -> PaymentIntent:
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
            PaymentIntent,
            await self._request_async(
                "post",
                "/v1/payment_intents",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        intent: str,
        params: "PaymentIntentService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        Retrieves the details of a PaymentIntent that has previously been created.

        You can retrieve a PaymentIntent client-side using a publishable key when the client_secret is in the query string.

        If you retrieve a PaymentIntent with a publishable key, it only returns a subset of properties. Refer to the [payment intent](https://stripe.com/docs/api#payment_intent_object) object reference for more details.
        """
        return cast(
            PaymentIntent,
            self._request(
                "get",
                "/v1/payment_intents/{intent}".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        intent: str,
        params: "PaymentIntentService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        Retrieves the details of a PaymentIntent that has previously been created.

        You can retrieve a PaymentIntent client-side using a publishable key when the client_secret is in the query string.

        If you retrieve a PaymentIntent with a publishable key, it only returns a subset of properties. Refer to the [payment intent](https://stripe.com/docs/api#payment_intent_object) object reference for more details.
        """
        return cast(
            PaymentIntent,
            await self._request_async(
                "get",
                "/v1/payment_intents/{intent}".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        intent: str,
        params: "PaymentIntentService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        Updates properties on a PaymentIntent object without confirming.

        Depending on which properties you update, you might need to confirm the
        PaymentIntent again. For example, updating the payment_method
        always requires you to confirm the PaymentIntent again. If you prefer to
        update and confirm at the same time, we recommend updating properties through
        the [confirm API](https://stripe.com/docs/api/payment_intents/confirm) instead.
        """
        return cast(
            PaymentIntent,
            self._request(
                "post",
                "/v1/payment_intents/{intent}".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        intent: str,
        params: "PaymentIntentService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        Updates properties on a PaymentIntent object without confirming.

        Depending on which properties you update, you might need to confirm the
        PaymentIntent again. For example, updating the payment_method
        always requires you to confirm the PaymentIntent again. If you prefer to
        update and confirm at the same time, we recommend updating properties through
        the [confirm API](https://stripe.com/docs/api/payment_intents/confirm) instead.
        """
        return cast(
            PaymentIntent,
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def search(
        self,
        params: "PaymentIntentService.SearchParams",
        options: RequestOptions = {},
    ) -> SearchResultObject[PaymentIntent]:
        """
        Search for PaymentIntents you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[PaymentIntent],
            self._request(
                "get",
                "/v1/payment_intents/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def search_async(
        self,
        params: "PaymentIntentService.SearchParams",
        options: RequestOptions = {},
    ) -> SearchResultObject[PaymentIntent]:
        """
        Search for PaymentIntents you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[PaymentIntent],
            await self._request_async(
                "get",
                "/v1/payment_intents/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def apply_customer_balance(
        self,
        intent: str,
        params: "PaymentIntentService.ApplyCustomerBalanceParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        Manually reconcile the remaining amount for a customer_balance PaymentIntent.
        """
        return cast(
            PaymentIntent,
            self._request(
                "post",
                "/v1/payment_intents/{intent}/apply_customer_balance".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def apply_customer_balance_async(
        self,
        intent: str,
        params: "PaymentIntentService.ApplyCustomerBalanceParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        Manually reconcile the remaining amount for a customer_balance PaymentIntent.
        """
        return cast(
            PaymentIntent,
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/apply_customer_balance".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def cancel(
        self,
        intent: str,
        params: "PaymentIntentService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        You can cancel a PaymentIntent object when it's in one of these statuses: requires_payment_method, requires_capture, requires_confirmation, requires_action or, [in rare cases](https://stripe.com/docs/payments/intents), processing.

        After it's canceled, no additional charges are made by the PaymentIntent and any operations on the PaymentIntent fail with an error. For PaymentIntents with a status of requires_capture, the remaining amount_capturable is automatically refunded.

        You can't cancel the PaymentIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        return cast(
            PaymentIntent,
            self._request(
                "post",
                "/v1/payment_intents/{intent}/cancel".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        intent: str,
        params: "PaymentIntentService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        You can cancel a PaymentIntent object when it's in one of these statuses: requires_payment_method, requires_capture, requires_confirmation, requires_action or, [in rare cases](https://stripe.com/docs/payments/intents), processing.

        After it's canceled, no additional charges are made by the PaymentIntent and any operations on the PaymentIntent fail with an error. For PaymentIntents with a status of requires_capture, the remaining amount_capturable is automatically refunded.

        You can't cancel the PaymentIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        return cast(
            PaymentIntent,
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/cancel".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def capture(
        self,
        intent: str,
        params: "PaymentIntentService.CaptureParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        Capture the funds of an existing uncaptured PaymentIntent when its status is requires_capture.

        Uncaptured PaymentIntents are cancelled a set number of days (7 by default) after their creation.

        Learn more about [separate authorization and capture](https://stripe.com/docs/payments/capture-later).
        """
        return cast(
            PaymentIntent,
            self._request(
                "post",
                "/v1/payment_intents/{intent}/capture".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def capture_async(
        self,
        intent: str,
        params: "PaymentIntentService.CaptureParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        Capture the funds of an existing uncaptured PaymentIntent when its status is requires_capture.

        Uncaptured PaymentIntents are cancelled a set number of days (7 by default) after their creation.

        Learn more about [separate authorization and capture](https://stripe.com/docs/payments/capture-later).
        """
        return cast(
            PaymentIntent,
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/capture".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def confirm(
        self,
        intent: str,
        params: "PaymentIntentService.ConfirmParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
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
            PaymentIntent,
            self._request(
                "post",
                "/v1/payment_intents/{intent}/confirm".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def confirm_async(
        self,
        intent: str,
        params: "PaymentIntentService.ConfirmParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
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
            PaymentIntent,
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/confirm".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def increment_authorization(
        self,
        intent: str,
        params: "PaymentIntentService.IncrementAuthorizationParams",
        options: RequestOptions = {},
    ) -> PaymentIntent:
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
            PaymentIntent,
            self._request(
                "post",
                "/v1/payment_intents/{intent}/increment_authorization".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def increment_authorization_async(
        self,
        intent: str,
        params: "PaymentIntentService.IncrementAuthorizationParams",
        options: RequestOptions = {},
    ) -> PaymentIntent:
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
            PaymentIntent,
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/increment_authorization".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def verify_microdeposits(
        self,
        intent: str,
        params: "PaymentIntentService.VerifyMicrodepositsParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        Verifies microdeposits on a PaymentIntent object.
        """
        return cast(
            PaymentIntent,
            self._request(
                "post",
                "/v1/payment_intents/{intent}/verify_microdeposits".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def verify_microdeposits_async(
        self,
        intent: str,
        params: "PaymentIntentService.VerifyMicrodepositsParams" = {},
        options: RequestOptions = {},
    ) -> PaymentIntent:
        """
        Verifies microdeposits on a PaymentIntent object.
        """
        return cast(
            PaymentIntent,
            await self._request_async(
                "post",
                "/v1/payment_intents/{intent}/verify_microdeposits".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
