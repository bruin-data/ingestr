# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._payment_method import PaymentMethod
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class PaymentMethodService(StripeService):
    class AttachParams(TypedDict):
        customer: str
        """
        The ID of the customer to which to attach the PaymentMethod.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(TypedDict):
        acss_debit: NotRequired["PaymentMethodService.CreateParamsAcssDebit"]
        """
        If this is an `acss_debit` PaymentMethod, this hash contains details about the ACSS Debit payment method.
        """
        affirm: NotRequired["PaymentMethodService.CreateParamsAffirm"]
        """
        If this is an `affirm` PaymentMethod, this hash contains details about the Affirm payment method.
        """
        afterpay_clearpay: NotRequired[
            "PaymentMethodService.CreateParamsAfterpayClearpay"
        ]
        """
        If this is an `AfterpayClearpay` PaymentMethod, this hash contains details about the AfterpayClearpay payment method.
        """
        alipay: NotRequired["PaymentMethodService.CreateParamsAlipay"]
        """
        If this is an `Alipay` PaymentMethod, this hash contains details about the Alipay payment method.
        """
        allow_redisplay: NotRequired[
            Literal["always", "limited", "unspecified"]
        ]
        """
        This field indicates whether this payment method can be shown again to its customer in a checkout flow. Stripe products such as Checkout and Elements use this field to determine whether a payment method can be shown as a saved payment method in a checkout flow. The field defaults to `unspecified`.
        """
        amazon_pay: NotRequired["PaymentMethodService.CreateParamsAmazonPay"]
        """
        If this is a AmazonPay PaymentMethod, this hash contains details about the AmazonPay payment method.
        """
        au_becs_debit: NotRequired[
            "PaymentMethodService.CreateParamsAuBecsDebit"
        ]
        """
        If this is an `au_becs_debit` PaymentMethod, this hash contains details about the bank account.
        """
        bacs_debit: NotRequired["PaymentMethodService.CreateParamsBacsDebit"]
        """
        If this is a `bacs_debit` PaymentMethod, this hash contains details about the Bacs Direct Debit bank account.
        """
        bancontact: NotRequired["PaymentMethodService.CreateParamsBancontact"]
        """
        If this is a `bancontact` PaymentMethod, this hash contains details about the Bancontact payment method.
        """
        billing_details: NotRequired[
            "PaymentMethodService.CreateParamsBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        blik: NotRequired["PaymentMethodService.CreateParamsBlik"]
        """
        If this is a `blik` PaymentMethod, this hash contains details about the BLIK payment method.
        """
        boleto: NotRequired["PaymentMethodService.CreateParamsBoleto"]
        """
        If this is a `boleto` PaymentMethod, this hash contains details about the Boleto payment method.
        """
        card: NotRequired["PaymentMethodService.CreateParamsCard"]
        """
        If this is a `card` PaymentMethod, this hash contains the user's card details. For backwards compatibility, you can alternatively provide a Stripe token (e.g., for Apple Pay, Amex Express Checkout, or legacy Checkout) into the card hash with format `card: {token: "tok_visa"}`. When providing a card number, you must meet the requirements for [PCI compliance](https://stripe.com/docs/security#validating-pci-compliance). We strongly recommend using Stripe.js instead of interacting with this API directly.
        """
        cashapp: NotRequired["PaymentMethodService.CreateParamsCashapp"]
        """
        If this is a `cashapp` PaymentMethod, this hash contains details about the Cash App Pay payment method.
        """
        customer: NotRequired[str]
        """
        The `Customer` to whom the original PaymentMethod is attached.
        """
        customer_balance: NotRequired[
            "PaymentMethodService.CreateParamsCustomerBalance"
        ]
        """
        If this is a `customer_balance` PaymentMethod, this hash contains details about the CustomerBalance payment method.
        """
        eps: NotRequired["PaymentMethodService.CreateParamsEps"]
        """
        If this is an `eps` PaymentMethod, this hash contains details about the EPS payment method.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        fpx: NotRequired["PaymentMethodService.CreateParamsFpx"]
        """
        If this is an `fpx` PaymentMethod, this hash contains details about the FPX payment method.
        """
        giropay: NotRequired["PaymentMethodService.CreateParamsGiropay"]
        """
        If this is a `giropay` PaymentMethod, this hash contains details about the Giropay payment method.
        """
        grabpay: NotRequired["PaymentMethodService.CreateParamsGrabpay"]
        """
        If this is a `grabpay` PaymentMethod, this hash contains details about the GrabPay payment method.
        """
        ideal: NotRequired["PaymentMethodService.CreateParamsIdeal"]
        """
        If this is an `ideal` PaymentMethod, this hash contains details about the iDEAL payment method.
        """
        interac_present: NotRequired[
            "PaymentMethodService.CreateParamsInteracPresent"
        ]
        """
        If this is an `interac_present` PaymentMethod, this hash contains details about the Interac Present payment method.
        """
        klarna: NotRequired["PaymentMethodService.CreateParamsKlarna"]
        """
        If this is a `klarna` PaymentMethod, this hash contains details about the Klarna payment method.
        """
        konbini: NotRequired["PaymentMethodService.CreateParamsKonbini"]
        """
        If this is a `konbini` PaymentMethod, this hash contains details about the Konbini payment method.
        """
        link: NotRequired["PaymentMethodService.CreateParamsLink"]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mobilepay: NotRequired["PaymentMethodService.CreateParamsMobilepay"]
        """
        If this is a `mobilepay` PaymentMethod, this hash contains details about the MobilePay payment method.
        """
        multibanco: NotRequired["PaymentMethodService.CreateParamsMultibanco"]
        """
        If this is a `multibanco` PaymentMethod, this hash contains details about the Multibanco payment method.
        """
        oxxo: NotRequired["PaymentMethodService.CreateParamsOxxo"]
        """
        If this is an `oxxo` PaymentMethod, this hash contains details about the OXXO payment method.
        """
        p24: NotRequired["PaymentMethodService.CreateParamsP24"]
        """
        If this is a `p24` PaymentMethod, this hash contains details about the P24 payment method.
        """
        payment_method: NotRequired[str]
        """
        The PaymentMethod to share.
        """
        paynow: NotRequired["PaymentMethodService.CreateParamsPaynow"]
        """
        If this is a `paynow` PaymentMethod, this hash contains details about the PayNow payment method.
        """
        paypal: NotRequired["PaymentMethodService.CreateParamsPaypal"]
        """
        If this is a `paypal` PaymentMethod, this hash contains details about the PayPal payment method.
        """
        pix: NotRequired["PaymentMethodService.CreateParamsPix"]
        """
        If this is a `pix` PaymentMethod, this hash contains details about the Pix payment method.
        """
        promptpay: NotRequired["PaymentMethodService.CreateParamsPromptpay"]
        """
        If this is a `promptpay` PaymentMethod, this hash contains details about the PromptPay payment method.
        """
        radar_options: NotRequired[
            "PaymentMethodService.CreateParamsRadarOptions"
        ]
        """
        Options to configure Radar. See [Radar Session](https://stripe.com/docs/radar/radar-session) for more information.
        """
        revolut_pay: NotRequired["PaymentMethodService.CreateParamsRevolutPay"]
        """
        If this is a `Revolut Pay` PaymentMethod, this hash contains details about the Revolut Pay payment method.
        """
        sepa_debit: NotRequired["PaymentMethodService.CreateParamsSepaDebit"]
        """
        If this is a `sepa_debit` PaymentMethod, this hash contains details about the SEPA debit bank account.
        """
        sofort: NotRequired["PaymentMethodService.CreateParamsSofort"]
        """
        If this is a `sofort` PaymentMethod, this hash contains details about the SOFORT payment method.
        """
        swish: NotRequired["PaymentMethodService.CreateParamsSwish"]
        """
        If this is a `swish` PaymentMethod, this hash contains details about the Swish payment method.
        """
        twint: NotRequired["PaymentMethodService.CreateParamsTwint"]
        """
        If this is a TWINT PaymentMethod, this hash contains details about the TWINT payment method.
        """
        type: NotRequired[
            Literal[
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
        ]
        """
        The type of the PaymentMethod. An additional hash is included on the PaymentMethod with a name matching this value. It contains additional information specific to the PaymentMethod type.
        """
        us_bank_account: NotRequired[
            "PaymentMethodService.CreateParamsUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """
        wechat_pay: NotRequired["PaymentMethodService.CreateParamsWechatPay"]
        """
        If this is an `wechat_pay` PaymentMethod, this hash contains details about the wechat_pay payment method.
        """
        zip: NotRequired["PaymentMethodService.CreateParamsZip"]
        """
        If this is a `zip` PaymentMethod, this hash contains details about the Zip payment method.
        """

    class CreateParamsAcssDebit(TypedDict):
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

    class CreateParamsAffirm(TypedDict):
        pass

    class CreateParamsAfterpayClearpay(TypedDict):
        pass

    class CreateParamsAlipay(TypedDict):
        pass

    class CreateParamsAmazonPay(TypedDict):
        pass

    class CreateParamsAuBecsDebit(TypedDict):
        account_number: str
        """
        The account number for the bank account.
        """
        bsb_number: str
        """
        Bank-State-Branch number of the bank account.
        """

    class CreateParamsBacsDebit(TypedDict):
        account_number: NotRequired[str]
        """
        Account number of the bank account that the funds will be debited from.
        """
        sort_code: NotRequired[str]
        """
        Sort code of the bank account. (e.g., `10-20-30`)
        """

    class CreateParamsBancontact(TypedDict):
        pass

    class CreateParamsBillingDetails(TypedDict):
        address: NotRequired[
            "Literal['']|PaymentMethodService.CreateParamsBillingDetailsAddress"
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

    class CreateParamsBillingDetailsAddress(TypedDict):
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

    class CreateParamsBlik(TypedDict):
        pass

    class CreateParamsBoleto(TypedDict):
        tax_id: str
        """
        The tax ID of the customer (CPF for individual consumers or CNPJ for businesses consumers)
        """

    class CreateParamsCard(TypedDict):
        cvc: NotRequired[str]
        """
        The card's CVC. It is highly recommended to always include this value.
        """
        exp_month: NotRequired[int]
        """
        Two-digit number representing the card's expiration month.
        """
        exp_year: NotRequired[int]
        """
        Four-digit number representing the card's expiration year.
        """
        networks: NotRequired["PaymentMethodService.CreateParamsCardNetworks"]
        """
        Contains information about card networks used to process the payment.
        """
        number: NotRequired[str]
        """
        The card number, as a string without any separators.
        """
        token: NotRequired[str]
        """
        For backwards compatibility, you can alternatively provide a Stripe token (e.g., for Apple Pay, Amex Express Checkout, or legacy Checkout) into the card hash with format card: {token: "tok_visa"}.
        """

    class CreateParamsCardNetworks(TypedDict):
        preferred: NotRequired[
            Literal["cartes_bancaires", "mastercard", "visa"]
        ]
        """
        The customer's preferred card network for co-branded cards. Supports `cartes_bancaires`, `mastercard`, or `visa`. Selection of a network that does not apply to the card will be stored as `invalid_preference` on the card.
        """

    class CreateParamsCashapp(TypedDict):
        pass

    class CreateParamsCustomerBalance(TypedDict):
        pass

    class CreateParamsEps(TypedDict):
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

    class CreateParamsFpx(TypedDict):
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

    class CreateParamsGiropay(TypedDict):
        pass

    class CreateParamsGrabpay(TypedDict):
        pass

    class CreateParamsIdeal(TypedDict):
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

    class CreateParamsInteracPresent(TypedDict):
        pass

    class CreateParamsKlarna(TypedDict):
        dob: NotRequired["PaymentMethodService.CreateParamsKlarnaDob"]
        """
        Customer's date of birth
        """

    class CreateParamsKlarnaDob(TypedDict):
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

    class CreateParamsKonbini(TypedDict):
        pass

    class CreateParamsLink(TypedDict):
        pass

    class CreateParamsMobilepay(TypedDict):
        pass

    class CreateParamsMultibanco(TypedDict):
        pass

    class CreateParamsOxxo(TypedDict):
        pass

    class CreateParamsP24(TypedDict):
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

    class CreateParamsPaynow(TypedDict):
        pass

    class CreateParamsPaypal(TypedDict):
        pass

    class CreateParamsPix(TypedDict):
        pass

    class CreateParamsPromptpay(TypedDict):
        pass

    class CreateParamsRadarOptions(TypedDict):
        session: NotRequired[str]
        """
        A [Radar Session](https://stripe.com/docs/radar/radar-session) is a snapshot of the browser metadata and device details that help Radar make more accurate predictions on your payments.
        """

    class CreateParamsRevolutPay(TypedDict):
        pass

    class CreateParamsSepaDebit(TypedDict):
        iban: str
        """
        IBAN of the bank account.
        """

    class CreateParamsSofort(TypedDict):
        country: Literal["AT", "BE", "DE", "ES", "IT", "NL"]
        """
        Two-letter ISO code representing the country the bank account is located in.
        """

    class CreateParamsSwish(TypedDict):
        pass

    class CreateParamsTwint(TypedDict):
        pass

    class CreateParamsUsBankAccount(TypedDict):
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

    class CreateParamsWechatPay(TypedDict):
        pass

    class CreateParamsZip(TypedDict):
        pass

    class DetachParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListParams(TypedDict):
        customer: NotRequired[str]
        """
        The ID of the customer whose PaymentMethods will be retrieved.
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
        type: NotRequired[
            Literal[
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
        ]
        """
        An optional filter on the list, based on the object `type` field. Without the filter, the list includes all current and future payment method types. If your integration expects only one type of payment method in the response, make sure to provide a type value in the request.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        allow_redisplay: NotRequired[
            Literal["always", "limited", "unspecified"]
        ]
        """
        This field indicates whether this payment method can be shown again to its customer in a checkout flow. Stripe products such as Checkout and Elements use this field to determine whether a payment method can be shown as a saved payment method in a checkout flow. The field defaults to `unspecified`.
        """
        billing_details: NotRequired[
            "PaymentMethodService.UpdateParamsBillingDetails"
        ]
        """
        Billing information associated with the PaymentMethod that may be used or required by particular types of payment methods.
        """
        card: NotRequired["PaymentMethodService.UpdateParamsCard"]
        """
        If this is a `card` PaymentMethod, this hash contains the user's card details.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        link: NotRequired["PaymentMethodService.UpdateParamsLink"]
        """
        If this is an `Link` PaymentMethod, this hash contains details about the Link payment method.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        us_bank_account: NotRequired[
            "PaymentMethodService.UpdateParamsUsBankAccount"
        ]
        """
        If this is an `us_bank_account` PaymentMethod, this hash contains details about the US bank account payment method.
        """

    class UpdateParamsBillingDetails(TypedDict):
        address: NotRequired[
            "Literal['']|PaymentMethodService.UpdateParamsBillingDetailsAddress"
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

    class UpdateParamsBillingDetailsAddress(TypedDict):
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

    class UpdateParamsCard(TypedDict):
        exp_month: NotRequired[int]
        """
        Two-digit number representing the card's expiration month.
        """
        exp_year: NotRequired[int]
        """
        Four-digit number representing the card's expiration year.
        """
        networks: NotRequired["PaymentMethodService.UpdateParamsCardNetworks"]
        """
        Contains information about card networks used to process the payment.
        """

    class UpdateParamsCardNetworks(TypedDict):
        preferred: NotRequired[
            "Literal['']|Literal['cartes_bancaires', 'mastercard', 'visa']"
        ]
        """
        The customer's preferred card network for co-branded cards. Supports `cartes_bancaires`, `mastercard`, or `visa`. Selection of a network that does not apply to the card will be stored as `invalid_preference` on the card.
        """

    class UpdateParamsLink(TypedDict):
        pass

    class UpdateParamsUsBankAccount(TypedDict):
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        Bank account holder type.
        """
        account_type: NotRequired[Literal["checking", "savings"]]
        """
        Bank account type.
        """

    def list(
        self,
        params: "PaymentMethodService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentMethod]:
        """
        Returns a list of PaymentMethods for Treasury flows. If you want to list the PaymentMethods attached to a Customer for payments, you should use the [List a Customer's PaymentMethods](https://stripe.com/docs/api/payment_methods/customer_list) API instead.
        """
        return cast(
            ListObject[PaymentMethod],
            self._request(
                "get",
                "/v1/payment_methods",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "PaymentMethodService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentMethod]:
        """
        Returns a list of PaymentMethods for Treasury flows. If you want to list the PaymentMethods attached to a Customer for payments, you should use the [List a Customer's PaymentMethods](https://stripe.com/docs/api/payment_methods/customer_list) API instead.
        """
        return cast(
            ListObject[PaymentMethod],
            await self._request_async(
                "get",
                "/v1/payment_methods",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "PaymentMethodService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Creates a PaymentMethod object. Read the [Stripe.js reference](https://stripe.com/docs/stripe-js/reference#stripe-create-payment-method) to learn how to create PaymentMethods via Stripe.js.

        Instead of creating a PaymentMethod directly, we recommend using the [PaymentIntents API to accept a payment immediately or the <a href="/docs/payments/save-and-reuse">SetupIntent](https://stripe.com/docs/payments/accept-a-payment) API to collect payment method details ahead of a future payment.
        """
        return cast(
            PaymentMethod,
            self._request(
                "post",
                "/v1/payment_methods",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "PaymentMethodService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Creates a PaymentMethod object. Read the [Stripe.js reference](https://stripe.com/docs/stripe-js/reference#stripe-create-payment-method) to learn how to create PaymentMethods via Stripe.js.

        Instead of creating a PaymentMethod directly, we recommend using the [PaymentIntents API to accept a payment immediately or the <a href="/docs/payments/save-and-reuse">SetupIntent](https://stripe.com/docs/payments/accept-a-payment) API to collect payment method details ahead of a future payment.
        """
        return cast(
            PaymentMethod,
            await self._request_async(
                "post",
                "/v1/payment_methods",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        payment_method: str,
        params: "PaymentMethodService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Retrieves a PaymentMethod object attached to the StripeAccount. To retrieve a payment method attached to a Customer, you should use [Retrieve a Customer's PaymentMethods](https://stripe.com/docs/api/payment_methods/customer)
        """
        return cast(
            PaymentMethod,
            self._request(
                "get",
                "/v1/payment_methods/{payment_method}".format(
                    payment_method=sanitize_id(payment_method),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        payment_method: str,
        params: "PaymentMethodService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Retrieves a PaymentMethod object attached to the StripeAccount. To retrieve a payment method attached to a Customer, you should use [Retrieve a Customer's PaymentMethods](https://stripe.com/docs/api/payment_methods/customer)
        """
        return cast(
            PaymentMethod,
            await self._request_async(
                "get",
                "/v1/payment_methods/{payment_method}".format(
                    payment_method=sanitize_id(payment_method),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        payment_method: str,
        params: "PaymentMethodService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Updates a PaymentMethod object. A PaymentMethod must be attached a customer to be updated.
        """
        return cast(
            PaymentMethod,
            self._request(
                "post",
                "/v1/payment_methods/{payment_method}".format(
                    payment_method=sanitize_id(payment_method),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        payment_method: str,
        params: "PaymentMethodService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Updates a PaymentMethod object. A PaymentMethod must be attached a customer to be updated.
        """
        return cast(
            PaymentMethod,
            await self._request_async(
                "post",
                "/v1/payment_methods/{payment_method}".format(
                    payment_method=sanitize_id(payment_method),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def attach(
        self,
        payment_method: str,
        params: "PaymentMethodService.AttachParams",
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Attaches a PaymentMethod object to a Customer.

        To attach a new PaymentMethod to a customer for future payments, we recommend you use a [SetupIntent](https://stripe.com/docs/api/setup_intents)
        or a PaymentIntent with [setup_future_usage](https://stripe.com/docs/api/payment_intents/create#create_payment_intent-setup_future_usage).
        These approaches will perform any necessary steps to set up the PaymentMethod for future payments. Using the /v1/payment_methods/:id/attach
        endpoint without first using a SetupIntent or PaymentIntent with setup_future_usage does not optimize the PaymentMethod for
        future use, which makes later declines and payment friction more likely.
        See [Optimizing cards for future payments](https://stripe.com/docs/payments/payment-intents#future-usage) for more information about setting up
        future payments.

        To use this PaymentMethod as the default for invoice or subscription payments,
        set [invoice_settings.default_payment_method](https://stripe.com/docs/api/customers/update#update_customer-invoice_settings-default_payment_method),
        on the Customer to the PaymentMethod's ID.
        """
        return cast(
            PaymentMethod,
            self._request(
                "post",
                "/v1/payment_methods/{payment_method}/attach".format(
                    payment_method=sanitize_id(payment_method),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def attach_async(
        self,
        payment_method: str,
        params: "PaymentMethodService.AttachParams",
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Attaches a PaymentMethod object to a Customer.

        To attach a new PaymentMethod to a customer for future payments, we recommend you use a [SetupIntent](https://stripe.com/docs/api/setup_intents)
        or a PaymentIntent with [setup_future_usage](https://stripe.com/docs/api/payment_intents/create#create_payment_intent-setup_future_usage).
        These approaches will perform any necessary steps to set up the PaymentMethod for future payments. Using the /v1/payment_methods/:id/attach
        endpoint without first using a SetupIntent or PaymentIntent with setup_future_usage does not optimize the PaymentMethod for
        future use, which makes later declines and payment friction more likely.
        See [Optimizing cards for future payments](https://stripe.com/docs/payments/payment-intents#future-usage) for more information about setting up
        future payments.

        To use this PaymentMethod as the default for invoice or subscription payments,
        set [invoice_settings.default_payment_method](https://stripe.com/docs/api/customers/update#update_customer-invoice_settings-default_payment_method),
        on the Customer to the PaymentMethod's ID.
        """
        return cast(
            PaymentMethod,
            await self._request_async(
                "post",
                "/v1/payment_methods/{payment_method}/attach".format(
                    payment_method=sanitize_id(payment_method),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def detach(
        self,
        payment_method: str,
        params: "PaymentMethodService.DetachParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Detaches a PaymentMethod object from a Customer. After a PaymentMethod is detached, it can no longer be used for a payment or re-attached to a Customer.
        """
        return cast(
            PaymentMethod,
            self._request(
                "post",
                "/v1/payment_methods/{payment_method}/detach".format(
                    payment_method=sanitize_id(payment_method),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def detach_async(
        self,
        payment_method: str,
        params: "PaymentMethodService.DetachParams" = {},
        options: RequestOptions = {},
    ) -> PaymentMethod:
        """
        Detaches a PaymentMethod object from a Customer. After a PaymentMethod is detached, it can no longer be used for a payment or re-attached to a Customer.
        """
        return cast(
            PaymentMethod,
            await self._request_async(
                "post",
                "/v1/payment_methods/{payment_method}/detach".format(
                    payment_method=sanitize_id(payment_method),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
