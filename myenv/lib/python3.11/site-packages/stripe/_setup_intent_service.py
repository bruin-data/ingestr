# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._setup_intent import SetupIntent
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class SetupIntentService(StripeService):
    class CancelParams(TypedDict):
        cancellation_reason: NotRequired[
            Literal["abandoned", "duplicate", "requested_by_customer"]
        ]
        """
        Reason for canceling this SetupIntent. Possible values are: `abandoned`, `requested_by_customer`, or `duplicate`
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ConfirmParams(TypedDict):
        confirmation_token: NotRequired[str]
        """
        ID of the ConfirmationToken used to confirm this SetupIntent.

        If the provided ConfirmationToken contains properties that are also being provided in this request, such as `payment_method`, then the values in this request will take precedence.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        mandate_data: NotRequired[
            "Literal['']|SetupIntentService.ConfirmParamsMandateData"
        ]
        payment_method: NotRequired[str]
        """
        ID of the payment method (a PaymentMethod, Card, or saved Source object) to attach to this SetupIntent.
        """
        payment_method_data: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodData"
        ]
        """
        When included, this hash creates a PaymentMethod that is set as the [`payment_method`](https://stripe.com/docs/api/setup_intents/object#setup_intent_object-payment_method)
        value in the SetupIntent.
        """
        payment_method_options: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptions"
        ]
        """
        Payment method-specific configuration for this SetupIntent.
        """
        return_url: NotRequired[str]
        """
        The URL to redirect your customer back to after they authenticate on the payment method's app or site.
        If you'd prefer to redirect to a mobile application, you can alternatively supply an application URI scheme.
        This parameter is only used for cards and other redirect-based payment methods.
        """
        use_stripe_sdk: NotRequired[bool]
        """
        Set to `true` when confirming server-side and using Stripe.js, iOS, or Android client-side SDKs to handle the next actions.
        """

    class ConfirmParamsMandateData(TypedDict):
        customer_acceptance: NotRequired[
            "SetupIntentService.ConfirmParamsMandateDataCustomerAcceptance"
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
            "SetupIntentService.ConfirmParamsMandateDataCustomerAcceptanceOffline"
        ]
        """
        If this is a Mandate accepted offline, this hash contains details about the offline acceptance.
        """
        online: NotRequired[
            "SetupIntentService.ConfirmParamsMandateDataCustomerAcceptanceOnline"
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
            "SetupIntentService.ConfirmParamsPaymentMethodDataAcssDebit"
        ]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataAlipay"
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
            "SetupIntentService.ConfirmParamsPaymentMethodDataAmazonPay"
        ]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        cashapp: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer_balance: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataEps"
        ]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        fpx: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataFpx"
        ]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataIdeal"
        ]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataLink"
        ]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataMobilepay"
        ]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataOxxo"
        ]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataP24"
        ]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        paynow: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataPix"
        ]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataRevolutPay"
        ]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataSwish"
        ]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataTwint"
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
            "SetupIntentService.ConfirmParamsPaymentMethodDataUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataWechatPay"
        ]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodDataZip"
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
            "Literal['']|SetupIntentService.ConfirmParamsPaymentMethodDataBillingDetailsAddress"
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
            "SetupIntentService.ConfirmParamsPaymentMethodDataKlarnaDob"
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
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsAcssDebit"
        ]
        """
        If this is a `acss_debit` SetupIntent, this sub-hash contains details about the ACSS Debit payment method options.
        """
        amazon_pay: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsAmazonPay"
        ]
        """
        If this is a `amazon_pay` SetupIntent, this sub-hash contains details about the AmazonPay payment method options.
        """
        card: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsCard"
        ]
        """
        Configuration for any card setup attempted on this SetupIntent.
        """
        card_present: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsCardPresent"
        ]
        """
        If this is a `card_present` PaymentMethod, this sub-hash contains details about the card-present payment method options.
        """
        link: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsLink"
        ]
        """
        If this is a `link` PaymentMethod, this sub-hash contains details about the Link payment method options.
        """
        paypal: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this sub-hash contains details about the PayPal payment method options.
        """
        sepa_debit: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsSepaDebit"
        ]
        """
        If this is a `sepa_debit` SetupIntent, this sub-hash contains details about the SEPA Debit payment method options.
        """
        us_bank_account: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If this is a `us_bank_account` SetupIntent, this sub-hash contains details about the US bank account payment method options.
        """

    class ConfirmParamsPaymentMethodOptionsAcssDebit(TypedDict):
        currency: NotRequired[Literal["cad", "usd"]]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        mandate_options: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsAcssDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
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
        default_for: NotRequired[List[Literal["invoice", "subscription"]]]
        """
        List of Stripe products where this mandate can be selected automatically.
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

    class ConfirmParamsPaymentMethodOptionsAmazonPay(TypedDict):
        pass

    class ConfirmParamsPaymentMethodOptionsCard(TypedDict):
        mandate_options: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsCardMandateOptions"
        ]
        """
        Configuration options for setting up an eMandate for cards issued in India.
        """
        moto: NotRequired[bool]
        """
        When specified, this parameter signals that a card has been collected
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
        Selected network to process this SetupIntent on. Depends on the available networks of the card attached to the SetupIntent. Can be only set confirm-time.
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. If not provided, this value defaults to `automatic`. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """
        three_d_secure: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsCardThreeDSecure"
        ]
        """
        If 3D Secure authentication was performed with a third-party provider,
        the authentication details to use for this setup.
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
        currency: str
        """
        Currency in which future payments will be charged. Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
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
        pass

    class ConfirmParamsPaymentMethodOptionsCardThreeDSecure(TypedDict):
        ares_trans_status: NotRequired[
            Literal["A", "C", "I", "N", "R", "U", "Y"]
        ]
        """
        The `transStatus` returned from the card Issuer's ACS in the ARes.
        """
        cryptogram: NotRequired[str]
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
        network_options: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions"
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
        transaction_id: NotRequired[str]
        """
        For 3D Secure 1, the XID. For 3D Secure 2, the Directory Server
        Transaction ID (dsTransID).
        """
        version: NotRequired[Literal["1.0.2", "2.1.0", "2.2.0"]]
        """
        The version of 3D Secure that was performed.
        """

    class ConfirmParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions(
        TypedDict,
    ):
        cartes_bancaires: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires"
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

    class ConfirmParamsPaymentMethodOptionsLink(TypedDict):
        persistent_token: NotRequired[str]
        """
        [Deprecated] This is a legacy parameter that no longer has any function.
        """

    class ConfirmParamsPaymentMethodOptionsPaypal(TypedDict):
        billing_agreement_id: NotRequired[str]
        """
        The PayPal Billing Agreement ID (BAID). This is an ID generated by PayPal which represents the mandate between the merchant and the customer.
        """

    class ConfirmParamsPaymentMethodOptionsSepaDebit(TypedDict):
        mandate_options: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsSepaDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """

    class ConfirmParamsPaymentMethodOptionsSepaDebitMandateOptions(TypedDict):
        pass

    class ConfirmParamsPaymentMethodOptionsUsBankAccount(TypedDict):
        financial_connections: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        mandate_options: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsUsBankAccountMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        networks: NotRequired[
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsUsBankAccountNetworks"
        ]
        """
        Additional fields for network related functions
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
            "SetupIntentService.ConfirmParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
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

    class CreateParams(TypedDict):
        attach_to_self: NotRequired[bool]
        """
        If present, the SetupIntent's payment method will be attached to the in-context Stripe Account.

        It can only be used for this Stripe Account's own money movement flows like InboundTransfer and OutboundTransfers. It cannot be set to true when setting up a PaymentMethod for a Customer, and defaults to false when attaching a PaymentMethod to a Customer.
        """
        automatic_payment_methods: NotRequired[
            "SetupIntentService.CreateParamsAutomaticPaymentMethods"
        ]
        """
        When you enable this parameter, this SetupIntent accepts payment methods that you enable in the Dashboard and that are compatible with its other parameters.
        """
        confirm: NotRequired[bool]
        """
        Set to `true` to attempt to confirm this SetupIntent immediately. This parameter defaults to `false`. If a card is the attached payment method, you can provide a `return_url` in case further authentication is necessary.
        """
        confirmation_token: NotRequired[str]
        """
        ID of the ConfirmationToken used to confirm this SetupIntent.

        If the provided ConfirmationToken contains properties that are also being provided in this request, such as `payment_method`, then the values in this request will take precedence.
        """
        customer: NotRequired[str]
        """
        ID of the Customer this SetupIntent belongs to, if one exists.

        If present, the SetupIntent's payment method will be attached to the Customer on successful setup. Payment methods attached to other Customers cannot be used with this SetupIntent.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        flow_directions: NotRequired[List[Literal["inbound", "outbound"]]]
        """
        Indicates the directions of money movement for which this payment method is intended to be used.

        Include `inbound` if you intend to use the payment method as the origin to pull funds from. Include `outbound` if you intend to use the payment method as the destination to send funds to. You can include both if you intend to use the payment method for both purposes.
        """
        mandate_data: NotRequired[
            "Literal['']|SetupIntentService.CreateParamsMandateData"
        ]
        """
        This hash contains details about the mandate to create. This parameter can only be used with [`confirm=true`](https://stripe.com/docs/api/setup_intents/create#create_setup_intent-confirm).
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        on_behalf_of: NotRequired[str]
        """
        The Stripe account ID created for this SetupIntent.
        """
        payment_method: NotRequired[str]
        """
        ID of the payment method (a PaymentMethod, Card, or saved Source object) to attach to this SetupIntent.
        """
        payment_method_configuration: NotRequired[str]
        """
        The ID of the payment method configuration to use with this SetupIntent.
        """
        payment_method_data: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodData"
        ]
        """
        When included, this hash creates a PaymentMethod that is set as the [`payment_method`](https://stripe.com/docs/api/setup_intents/object#setup_intent_object-payment_method)
        value in the SetupIntent.
        """
        payment_method_options: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptions"
        ]
        """
        Payment method-specific configuration for this SetupIntent.
        """
        payment_method_types: NotRequired[List[str]]
        """
        The list of payment method types (for example, card) that this SetupIntent can use. If you don't provide this, it defaults to ["card"].
        """
        return_url: NotRequired[str]
        """
        The URL to redirect your customer back to after they authenticate or cancel their payment on the payment method's app or site. To redirect to a mobile application, you can alternatively supply an application URI scheme. This parameter can only be used with [`confirm=true`](https://stripe.com/docs/api/setup_intents/create#create_setup_intent-confirm).
        """
        single_use: NotRequired["SetupIntentService.CreateParamsSingleUse"]
        """
        If you populate this hash, this SetupIntent generates a `single_use` mandate after successful completion.
        """
        usage: NotRequired[Literal["off_session", "on_session"]]
        """
        Indicates how the payment method is intended to be used in the future. If not provided, this value defaults to `off_session`.
        """
        use_stripe_sdk: NotRequired[bool]
        """
        Set to `true` when confirming server-side and using Stripe.js, iOS, or Android client-side SDKs to handle the next actions.
        """

    class CreateParamsAutomaticPaymentMethods(TypedDict):
        allow_redirects: NotRequired[Literal["always", "never"]]
        """
        Controls whether this SetupIntent will accept redirect-based payment methods.

        Redirect-based payment methods may require your customer to be redirected to a payment method's app or site for authentication or additional steps. To [confirm](https://stripe.com/docs/api/setup_intents/confirm) this SetupIntent, you may be required to provide a `return_url` to redirect customers back to your site after they authenticate or complete the setup.
        """
        enabled: bool
        """
        Whether this feature is enabled.
        """

    class CreateParamsMandateData(TypedDict):
        customer_acceptance: (
            "SetupIntentService.CreateParamsMandateDataCustomerAcceptance"
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
            "SetupIntentService.CreateParamsMandateDataCustomerAcceptanceOffline"
        ]
        """
        If this is a Mandate accepted offline, this hash contains details about the offline acceptance.
        """
        online: NotRequired[
            "SetupIntentService.CreateParamsMandateDataCustomerAcceptanceOnline"
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
            "SetupIntentService.CreateParamsPaymentMethodDataAcssDebit"
        ]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataAlipay"
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
            "SetupIntentService.CreateParamsPaymentMethodDataAmazonPay"
        ]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        cashapp: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer_balance: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired["SetupIntentService.CreateParamsPaymentMethodDataEps"]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        fpx: NotRequired["SetupIntentService.CreateParamsPaymentMethodDataFpx"]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataIdeal"
        ]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataLink"
        ]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataMobilepay"
        ]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataOxxo"
        ]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired["SetupIntentService.CreateParamsPaymentMethodDataP24"]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        paynow: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired["SetupIntentService.CreateParamsPaymentMethodDataPix"]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataRevolutPay"
        ]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataSwish"
        ]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataTwint"
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
            "SetupIntentService.CreateParamsPaymentMethodDataUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodDataWechatPay"
        ]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired["SetupIntentService.CreateParamsPaymentMethodDataZip"]
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
            "Literal['']|SetupIntentService.CreateParamsPaymentMethodDataBillingDetailsAddress"
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
            "SetupIntentService.CreateParamsPaymentMethodDataKlarnaDob"
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
            "SetupIntentService.CreateParamsPaymentMethodOptionsAcssDebit"
        ]
        """
        If this is a `acss_debit` SetupIntent, this sub-hash contains details about the ACSS Debit payment method options.
        """
        amazon_pay: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsAmazonPay"
        ]
        """
        If this is a `amazon_pay` SetupIntent, this sub-hash contains details about the AmazonPay payment method options.
        """
        card: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsCard"
        ]
        """
        Configuration for any card setup attempted on this SetupIntent.
        """
        card_present: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsCardPresent"
        ]
        """
        If this is a `card_present` PaymentMethod, this sub-hash contains details about the card-present payment method options.
        """
        link: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsLink"
        ]
        """
        If this is a `link` PaymentMethod, this sub-hash contains details about the Link payment method options.
        """
        paypal: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this sub-hash contains details about the PayPal payment method options.
        """
        sepa_debit: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsSepaDebit"
        ]
        """
        If this is a `sepa_debit` SetupIntent, this sub-hash contains details about the SEPA Debit payment method options.
        """
        us_bank_account: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If this is a `us_bank_account` SetupIntent, this sub-hash contains details about the US bank account payment method options.
        """

    class CreateParamsPaymentMethodOptionsAcssDebit(TypedDict):
        currency: NotRequired[Literal["cad", "usd"]]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        mandate_options: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsAcssDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
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
        default_for: NotRequired[List[Literal["invoice", "subscription"]]]
        """
        List of Stripe products where this mandate can be selected automatically.
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

    class CreateParamsPaymentMethodOptionsAmazonPay(TypedDict):
        pass

    class CreateParamsPaymentMethodOptionsCard(TypedDict):
        mandate_options: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsCardMandateOptions"
        ]
        """
        Configuration options for setting up an eMandate for cards issued in India.
        """
        moto: NotRequired[bool]
        """
        When specified, this parameter signals that a card has been collected
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
        Selected network to process this SetupIntent on. Depends on the available networks of the card attached to the SetupIntent. Can be only set confirm-time.
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. If not provided, this value defaults to `automatic`. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """
        three_d_secure: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsCardThreeDSecure"
        ]
        """
        If 3D Secure authentication was performed with a third-party provider,
        the authentication details to use for this setup.
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
        currency: str
        """
        Currency in which future payments will be charged. Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
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
        pass

    class CreateParamsPaymentMethodOptionsCardThreeDSecure(TypedDict):
        ares_trans_status: NotRequired[
            Literal["A", "C", "I", "N", "R", "U", "Y"]
        ]
        """
        The `transStatus` returned from the card Issuer's ACS in the ARes.
        """
        cryptogram: NotRequired[str]
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
        network_options: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions"
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
        transaction_id: NotRequired[str]
        """
        For 3D Secure 1, the XID. For 3D Secure 2, the Directory Server
        Transaction ID (dsTransID).
        """
        version: NotRequired[Literal["1.0.2", "2.1.0", "2.2.0"]]
        """
        The version of 3D Secure that was performed.
        """

    class CreateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions(
        TypedDict,
    ):
        cartes_bancaires: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires"
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

    class CreateParamsPaymentMethodOptionsLink(TypedDict):
        persistent_token: NotRequired[str]
        """
        [Deprecated] This is a legacy parameter that no longer has any function.
        """

    class CreateParamsPaymentMethodOptionsPaypal(TypedDict):
        billing_agreement_id: NotRequired[str]
        """
        The PayPal Billing Agreement ID (BAID). This is an ID generated by PayPal which represents the mandate between the merchant and the customer.
        """

    class CreateParamsPaymentMethodOptionsSepaDebit(TypedDict):
        mandate_options: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsSepaDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """

    class CreateParamsPaymentMethodOptionsSepaDebitMandateOptions(TypedDict):
        pass

    class CreateParamsPaymentMethodOptionsUsBankAccount(TypedDict):
        financial_connections: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        mandate_options: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsUsBankAccountMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        networks: NotRequired[
            "SetupIntentService.CreateParamsPaymentMethodOptionsUsBankAccountNetworks"
        ]
        """
        Additional fields for network related functions
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
            "SetupIntentService.CreateParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
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

    class CreateParamsSingleUse(TypedDict):
        amount: int
        """
        Amount the customer is granting permission to collect later. A positive integer representing how much to charge in the [smallest currency unit](https://stripe.com/docs/currencies#zero-decimal) (e.g., 100 cents to charge $1.00 or 100 to charge ¥100, a zero-decimal currency). The minimum amount is $0.50 US or [equivalent in charge currency](https://stripe.com/docs/currencies#minimum-and-maximum-charge-amounts). The amount value supports up to eight digits (e.g., a value of 99999999 for a USD charge of $999,999.99).
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """

    class ListParams(TypedDict):
        attach_to_self: NotRequired[bool]
        """
        If present, the SetupIntent's payment method will be attached to the in-context Stripe Account.

        It can only be used for this Stripe Account's own money movement flows like InboundTransfer and OutboundTransfers. It cannot be set to true when setting up a PaymentMethod for a Customer, and defaults to false when attaching a PaymentMethod to a Customer.
        """
        created: NotRequired["SetupIntentService.ListParamsCreated|int"]
        """
        A filter on the list, based on the object `created` field. The value can be a string with an integer Unix timestamp, or it can be a dictionary with a number of different query options.
        """
        customer: NotRequired[str]
        """
        Only return SetupIntents for the customer specified by this customer ID.
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
        payment_method: NotRequired[str]
        """
        Only return SetupIntents that associate with the specified payment method.
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
        The client secret of the SetupIntent. We require this string if you use a publishable key to retrieve the SetupIntent.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        attach_to_self: NotRequired[bool]
        """
        If present, the SetupIntent's payment method will be attached to the in-context Stripe Account.

        It can only be used for this Stripe Account's own money movement flows like InboundTransfer and OutboundTransfers. It cannot be set to true when setting up a PaymentMethod for a Customer, and defaults to false when attaching a PaymentMethod to a Customer.
        """
        customer: NotRequired[str]
        """
        ID of the Customer this SetupIntent belongs to, if one exists.

        If present, the SetupIntent's payment method will be attached to the Customer on successful setup. Payment methods attached to other Customers cannot be used with this SetupIntent.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        flow_directions: NotRequired[List[Literal["inbound", "outbound"]]]
        """
        Indicates the directions of money movement for which this payment method is intended to be used.

        Include `inbound` if you intend to use the payment method as the origin to pull funds from. Include `outbound` if you intend to use the payment method as the destination to send funds to. You can include both if you intend to use the payment method for both purposes.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        payment_method: NotRequired[str]
        """
        ID of the payment method (a PaymentMethod, Card, or saved Source object) to attach to this SetupIntent. To unset this field to null, pass in an empty string.
        """
        payment_method_configuration: NotRequired[str]
        """
        The ID of the payment method configuration to use with this SetupIntent.
        """
        payment_method_data: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodData"
        ]
        """
        When included, this hash creates a PaymentMethod that is set as the [`payment_method`](https://stripe.com/docs/api/setup_intents/object#setup_intent_object-payment_method)
        value in the SetupIntent.
        """
        payment_method_options: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptions"
        ]
        """
        Payment method-specific configuration for this SetupIntent.
        """
        payment_method_types: NotRequired[List[str]]
        """
        The list of payment method types (for example, card) that this SetupIntent can set up. If you don't provide this array, it defaults to ["card"].
        """

    class UpdateParamsPaymentMethodData(TypedDict):
        acss_debit: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataAcssDebit"
        ]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataAlipay"
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
            "SetupIntentService.UpdateParamsPaymentMethodDataAmazonPay"
        ]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        cashapp: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer_balance: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired["SetupIntentService.UpdateParamsPaymentMethodDataEps"]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        fpx: NotRequired["SetupIntentService.UpdateParamsPaymentMethodDataFpx"]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataIdeal"
        ]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataLink"
        ]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataMobilepay"
        ]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataOxxo"
        ]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired["SetupIntentService.UpdateParamsPaymentMethodDataP24"]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        paynow: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired["SetupIntentService.UpdateParamsPaymentMethodDataPix"]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataRevolutPay"
        ]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataSwish"
        ]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataTwint"
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
            "SetupIntentService.UpdateParamsPaymentMethodDataUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodDataWechatPay"
        ]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired["SetupIntentService.UpdateParamsPaymentMethodDataZip"]
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
            "Literal['']|SetupIntentService.UpdateParamsPaymentMethodDataBillingDetailsAddress"
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
            "SetupIntentService.UpdateParamsPaymentMethodDataKlarnaDob"
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
            "SetupIntentService.UpdateParamsPaymentMethodOptionsAcssDebit"
        ]
        """
        If this is a `acss_debit` SetupIntent, this sub-hash contains details about the ACSS Debit payment method options.
        """
        amazon_pay: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsAmazonPay"
        ]
        """
        If this is a `amazon_pay` SetupIntent, this sub-hash contains details about the AmazonPay payment method options.
        """
        card: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsCard"
        ]
        """
        Configuration for any card setup attempted on this SetupIntent.
        """
        card_present: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsCardPresent"
        ]
        """
        If this is a `card_present` PaymentMethod, this sub-hash contains details about the card-present payment method options.
        """
        link: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsLink"
        ]
        """
        If this is a `link` PaymentMethod, this sub-hash contains details about the Link payment method options.
        """
        paypal: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this sub-hash contains details about the PayPal payment method options.
        """
        sepa_debit: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsSepaDebit"
        ]
        """
        If this is a `sepa_debit` SetupIntent, this sub-hash contains details about the SEPA Debit payment method options.
        """
        us_bank_account: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsUsBankAccount"
        ]
        """
        If this is a `us_bank_account` SetupIntent, this sub-hash contains details about the US bank account payment method options.
        """

    class UpdateParamsPaymentMethodOptionsAcssDebit(TypedDict):
        currency: NotRequired[Literal["cad", "usd"]]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        mandate_options: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsAcssDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
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
        default_for: NotRequired[List[Literal["invoice", "subscription"]]]
        """
        List of Stripe products where this mandate can be selected automatically.
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

    class UpdateParamsPaymentMethodOptionsAmazonPay(TypedDict):
        pass

    class UpdateParamsPaymentMethodOptionsCard(TypedDict):
        mandate_options: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsCardMandateOptions"
        ]
        """
        Configuration options for setting up an eMandate for cards issued in India.
        """
        moto: NotRequired[bool]
        """
        When specified, this parameter signals that a card has been collected
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
        Selected network to process this SetupIntent on. Depends on the available networks of the card attached to the SetupIntent. Can be only set confirm-time.
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. If not provided, this value defaults to `automatic`. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """
        three_d_secure: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsCardThreeDSecure"
        ]
        """
        If 3D Secure authentication was performed with a third-party provider,
        the authentication details to use for this setup.
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
        currency: str
        """
        Currency in which future payments will be charged. Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
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
        pass

    class UpdateParamsPaymentMethodOptionsCardThreeDSecure(TypedDict):
        ares_trans_status: NotRequired[
            Literal["A", "C", "I", "N", "R", "U", "Y"]
        ]
        """
        The `transStatus` returned from the card Issuer's ACS in the ARes.
        """
        cryptogram: NotRequired[str]
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
        network_options: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions"
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
        transaction_id: NotRequired[str]
        """
        For 3D Secure 1, the XID. For 3D Secure 2, the Directory Server
        Transaction ID (dsTransID).
        """
        version: NotRequired[Literal["1.0.2", "2.1.0", "2.2.0"]]
        """
        The version of 3D Secure that was performed.
        """

    class UpdateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptions(
        TypedDict,
    ):
        cartes_bancaires: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsCardThreeDSecureNetworkOptionsCartesBancaires"
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

    class UpdateParamsPaymentMethodOptionsLink(TypedDict):
        persistent_token: NotRequired[str]
        """
        [Deprecated] This is a legacy parameter that no longer has any function.
        """

    class UpdateParamsPaymentMethodOptionsPaypal(TypedDict):
        billing_agreement_id: NotRequired[str]
        """
        The PayPal Billing Agreement ID (BAID). This is an ID generated by PayPal which represents the mandate between the merchant and the customer.
        """

    class UpdateParamsPaymentMethodOptionsSepaDebit(TypedDict):
        mandate_options: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsSepaDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """

    class UpdateParamsPaymentMethodOptionsSepaDebitMandateOptions(TypedDict):
        pass

    class UpdateParamsPaymentMethodOptionsUsBankAccount(TypedDict):
        financial_connections: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        mandate_options: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsUsBankAccountMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        networks: NotRequired[
            "SetupIntentService.UpdateParamsPaymentMethodOptionsUsBankAccountNetworks"
        ]
        """
        Additional fields for network related functions
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
            "SetupIntentService.UpdateParamsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
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
        params: "SetupIntentService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[SetupIntent]:
        """
        Returns a list of SetupIntents.
        """
        return cast(
            ListObject[SetupIntent],
            self._request(
                "get",
                "/v1/setup_intents",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "SetupIntentService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[SetupIntent]:
        """
        Returns a list of SetupIntents.
        """
        return cast(
            ListObject[SetupIntent],
            await self._request_async(
                "get",
                "/v1/setup_intents",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "SetupIntentService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        Creates a SetupIntent object.

        After you create the SetupIntent, attach a payment method and [confirm](https://stripe.com/docs/api/setup_intents/confirm)
        it to collect any required permissions to charge the payment method later.
        """
        return cast(
            SetupIntent,
            self._request(
                "post",
                "/v1/setup_intents",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "SetupIntentService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        Creates a SetupIntent object.

        After you create the SetupIntent, attach a payment method and [confirm](https://stripe.com/docs/api/setup_intents/confirm)
        it to collect any required permissions to charge the payment method later.
        """
        return cast(
            SetupIntent,
            await self._request_async(
                "post",
                "/v1/setup_intents",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        intent: str,
        params: "SetupIntentService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        Retrieves the details of a SetupIntent that has previously been created.

        Client-side retrieval using a publishable key is allowed when the client_secret is provided in the query string.

        When retrieved with a publishable key, only a subset of properties will be returned. Please refer to the [SetupIntent](https://stripe.com/docs/api#setup_intent_object) object reference for more details.
        """
        return cast(
            SetupIntent,
            self._request(
                "get",
                "/v1/setup_intents/{intent}".format(
                    intent=sanitize_id(intent)
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
        params: "SetupIntentService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        Retrieves the details of a SetupIntent that has previously been created.

        Client-side retrieval using a publishable key is allowed when the client_secret is provided in the query string.

        When retrieved with a publishable key, only a subset of properties will be returned. Please refer to the [SetupIntent](https://stripe.com/docs/api#setup_intent_object) object reference for more details.
        """
        return cast(
            SetupIntent,
            await self._request_async(
                "get",
                "/v1/setup_intents/{intent}".format(
                    intent=sanitize_id(intent)
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
        params: "SetupIntentService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        Updates a SetupIntent object.
        """
        return cast(
            SetupIntent,
            self._request(
                "post",
                "/v1/setup_intents/{intent}".format(
                    intent=sanitize_id(intent)
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
        params: "SetupIntentService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        Updates a SetupIntent object.
        """
        return cast(
            SetupIntent,
            await self._request_async(
                "post",
                "/v1/setup_intents/{intent}".format(
                    intent=sanitize_id(intent)
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
        params: "SetupIntentService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        You can cancel a SetupIntent object when it's in one of these statuses: requires_payment_method, requires_confirmation, or requires_action.

        After you cancel it, setup is abandoned and any operations on the SetupIntent fail with an error. You can't cancel the SetupIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        return cast(
            SetupIntent,
            self._request(
                "post",
                "/v1/setup_intents/{intent}/cancel".format(
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
        params: "SetupIntentService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        You can cancel a SetupIntent object when it's in one of these statuses: requires_payment_method, requires_confirmation, or requires_action.

        After you cancel it, setup is abandoned and any operations on the SetupIntent fail with an error. You can't cancel the SetupIntent for a Checkout Session. [Expire the Checkout Session](https://stripe.com/docs/api/checkout/sessions/expire) instead.
        """
        return cast(
            SetupIntent,
            await self._request_async(
                "post",
                "/v1/setup_intents/{intent}/cancel".format(
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
        params: "SetupIntentService.ConfirmParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        Confirm that your customer intends to set up the current or
        provided payment method. For example, you would confirm a SetupIntent
        when a customer hits the “Save” button on a payment method management
        page on your website.

        If the selected payment method does not require any additional
        steps from the customer, the SetupIntent will transition to the
        succeeded status.

        Otherwise, it will transition to the requires_action status and
        suggest additional actions via next_action. If setup fails,
        the SetupIntent will transition to the
        requires_payment_method status or the canceled status if the
        confirmation limit is reached.
        """
        return cast(
            SetupIntent,
            self._request(
                "post",
                "/v1/setup_intents/{intent}/confirm".format(
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
        params: "SetupIntentService.ConfirmParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        Confirm that your customer intends to set up the current or
        provided payment method. For example, you would confirm a SetupIntent
        when a customer hits the “Save” button on a payment method management
        page on your website.

        If the selected payment method does not require any additional
        steps from the customer, the SetupIntent will transition to the
        succeeded status.

        Otherwise, it will transition to the requires_action status and
        suggest additional actions via next_action. If setup fails,
        the SetupIntent will transition to the
        requires_payment_method status or the canceled status if the
        confirmation limit is reached.
        """
        return cast(
            SetupIntent,
            await self._request_async(
                "post",
                "/v1/setup_intents/{intent}/confirm".format(
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
        params: "SetupIntentService.VerifyMicrodepositsParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        Verifies microdeposits on a SetupIntent object.
        """
        return cast(
            SetupIntent,
            self._request(
                "post",
                "/v1/setup_intents/{intent}/verify_microdeposits".format(
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
        params: "SetupIntentService.VerifyMicrodepositsParams" = {},
        options: RequestOptions = {},
    ) -> SetupIntent:
        """
        Verifies microdeposits on a SetupIntent object.
        """
        return cast(
            SetupIntent,
            await self._request_async(
                "post",
                "/v1/setup_intents/{intent}/verify_microdeposits".format(
                    intent=sanitize_id(intent),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
