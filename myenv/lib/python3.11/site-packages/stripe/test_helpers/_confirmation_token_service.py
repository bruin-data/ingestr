# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._confirmation_token import ConfirmationToken
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class ConfirmationTokenService(StripeService):
    class CreateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        payment_method: NotRequired[str]
        """
        ID of an existing PaymentMethod.
        """
        payment_method_data: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodData"
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
        shipping: NotRequired["ConfirmationTokenService.CreateParamsShipping"]
        """
        Shipping information for this ConfirmationToken.
        """

    class CreateParamsPaymentMethodData(TypedDict):
        acss_debit: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataAcssDebit"
        ]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataAffirm"
        ]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataAlipay"
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
            "ConfirmationTokenService.CreateParamsPaymentMethodDataAmazonPay"
        ]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataBacsDebit"
        ]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataBancontact"
        ]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataBlik"
        ]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataBoleto"
        ]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        cashapp: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataCashapp"
        ]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer_balance: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataEps"
        ]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        fpx: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataFpx"
        ]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataGiropay"
        ]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataGrabpay"
        ]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataIdeal"
        ]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataKlarna"
        ]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataKonbini"
        ]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataLink"
        ]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataMobilepay"
        ]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataMultibanco"
        ]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataOxxo"
        ]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataP24"
        ]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        paynow: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataPaynow"
        ]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataPaypal"
        ]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataPix"
        ]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataPromptpay"
        ]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataRevolutPay"
        ]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataSepaDebit"
        ]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataSofort"
        ]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataSwish"
        ]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataTwint"
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
            "ConfirmationTokenService.CreateParamsPaymentMethodDataUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataWechatPay"
        ]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired[
            "ConfirmationTokenService.CreateParamsPaymentMethodDataZip"
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
            "Literal['']|ConfirmationTokenService.CreateParamsPaymentMethodDataBillingDetailsAddress"
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
            "ConfirmationTokenService.CreateParamsPaymentMethodDataKlarnaDob"
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
        address: "ConfirmationTokenService.CreateParamsShippingAddress"
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

    def create(
        self,
        params: "ConfirmationTokenService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> ConfirmationToken:
        """
        Creates a test mode Confirmation Token server side for your integration tests.
        """
        return cast(
            ConfirmationToken,
            self._request(
                "post",
                "/v1/test_helpers/confirmation_tokens",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "ConfirmationTokenService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> ConfirmationToken:
        """
        Creates a test mode Confirmation Token server side for your integration tests.
        """
        return cast(
            ConfirmationToken,
            await self._request_async(
                "post",
                "/v1/test_helpers/confirmation_tokens",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
