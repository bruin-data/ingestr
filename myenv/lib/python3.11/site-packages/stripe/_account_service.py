# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._account import Account
from stripe._account_capability_service import AccountCapabilityService
from stripe._account_external_account_service import (
    AccountExternalAccountService,
)
from stripe._account_login_link_service import AccountLoginLinkService
from stripe._account_person_service import AccountPersonService
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class AccountService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.capabilities = AccountCapabilityService(self._requestor)
        self.external_accounts = AccountExternalAccountService(self._requestor)
        self.login_links = AccountLoginLinkService(self._requestor)
        self.persons = AccountPersonService(self._requestor)

    class CreateParams(TypedDict):
        account_token: NotRequired[str]
        """
        An [account token](https://stripe.com/docs/api#create_account_token), used to securely provide details to the account.
        """
        business_profile: NotRequired[
            "AccountService.CreateParamsBusinessProfile"
        ]
        """
        Business information about the account.
        """
        business_type: NotRequired[
            Literal["company", "government_entity", "individual", "non_profit"]
        ]
        """
        The business type. Once you create an [Account Link](https://stripe.com/api/account_links) or [Account Session](https://stripe.com/api/account_sessions), this property can only be updated for accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts.
        """
        capabilities: NotRequired["AccountService.CreateParamsCapabilities"]
        """
        Each key of the dictionary represents a capability, and each capability
        maps to its settings (for example, whether it has been requested or not). Each
        capability is inactive until you have provided its specific
        requirements and Stripe has verified them. An account might have some
        of its requested capabilities be active and some be inactive.

        Required when [account.controller.stripe_dashboard.type](https://stripe.com/api/accounts/create#create_account-controller-dashboard-type)
        is `none`, which includes Custom accounts.
        """
        company: NotRequired["AccountService.CreateParamsCompany"]
        """
        Information about the company or business. This field is available for any `business_type`. Once you create an [Account Link](https://stripe.com/api/account_links) or [Account Session](https://stripe.com/api/account_sessions), this property can only be updated for accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts.
        """
        controller: NotRequired["AccountService.CreateParamsController"]
        """
        A hash of configuration describing the account controller's attributes.
        """
        country: NotRequired[str]
        """
        The country in which the account holder resides, or in which the business is legally established. This should be an ISO 3166-1 alpha-2 country code. For example, if you are in the United States and the business for which you're creating an account is legally represented in Canada, you would use `CA` as the country for the account being created. Available countries include [Stripe's global markets](https://stripe.com/global) as well as countries where [cross-border payouts](https://stripe.com/docs/connect/cross-border-payouts) are supported.
        """
        default_currency: NotRequired[str]
        """
        Three-letter ISO currency code representing the default currency for the account. This must be a currency that [Stripe supports in the account's country](https://docs.stripe.com/payouts).
        """
        documents: NotRequired["AccountService.CreateParamsDocuments"]
        """
        Documents that may be submitted to satisfy various informational requests.
        """
        email: NotRequired[str]
        """
        The email address of the account holder. This is only to make the account easier to identify to you. If [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts, Stripe doesn't email the account without your consent.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        external_account: NotRequired[
            "str|AccountService.CreateParamsBankAccount|AccountService.CreateParamsCard|AccountService.CreateParamsCardToken"
        ]
        """
        A card or bank account to attach to the account for receiving [payouts](https://stripe.com/connect/bank-debit-card-payouts) (you won't be able to use it for top-ups). You can provide either a token, like the ones returned by [Stripe.js](https://stripe.com/js), or a dictionary, as documented in the `external_account` parameter for [bank account](https://stripe.com/api#account_create_bank_account) creation.

        By default, providing an external account sets it as the new default external account for its currency, and deletes the old default if one exists. To add additional external accounts without replacing the existing default for the currency, use the [bank account](https://stripe.com/api#account_create_bank_account) or [card creation](https://stripe.com/api#account_create_card) APIs. After you create an [Account Link](https://stripe.com/api/account_links) or [Account Session](https://stripe.com/api/account_sessions), this property can only be updated for accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts.
        """
        individual: NotRequired["AccountService.CreateParamsIndividual"]
        """
        Information about the person represented by the account. This field is null unless `business_type` is set to `individual`. Once you create an [Account Link](https://stripe.com/api/account_links) or [Account Session](https://stripe.com/api/account_sessions), this property can only be updated for accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        settings: NotRequired["AccountService.CreateParamsSettings"]
        """
        Options for customizing how the account functions within Stripe.
        """
        tos_acceptance: NotRequired["AccountService.CreateParamsTosAcceptance"]
        """
        Details on the account's acceptance of the [Stripe Services Agreement](https://stripe.com/connect/updating-accounts#tos-acceptance). This property can only be updated for accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts. This property defaults to a `full` service agreement when empty.
        """
        type: NotRequired[Literal["custom", "express", "standard"]]
        """
        The type of Stripe account to create. May be one of `custom`, `express` or `standard`.
        """

    class CreateParamsBankAccount(TypedDict):
        object: Literal["bank_account"]
        account_holder_name: NotRequired[str]
        """
        The name of the person or business that owns the bank account.This field is required when attaching the bank account to a `Customer` object.
        """
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        The type of entity that holds the account. It can be `company` or `individual`. This field is required when attaching the bank account to a `Customer` object.
        """
        account_number: str
        """
        The account number for the bank account, in string form. Must be a checking account.
        """
        country: str
        """
        The country in which the bank account is located.
        """
        currency: NotRequired[str]
        """
        The currency the bank account is in. This must be a country/currency pairing that [Stripe supports.](docs/payouts)
        """
        routing_number: NotRequired[str]
        """
        The routing number, sort code, or other country-appropriateinstitution number for the bank account. For US bank accounts, this is required and should bethe ACH routing number, not the wire routing number. If you are providing an IBAN for`account_number`, this field is not required.
        """

    class CreateParamsBusinessProfile(TypedDict):
        annual_revenue: NotRequired[
            "AccountService.CreateParamsBusinessProfileAnnualRevenue"
        ]
        """
        The applicant's gross annual revenue for its preceding fiscal year.
        """
        estimated_worker_count: NotRequired[int]
        """
        An estimated upper bound of employees, contractors, vendors, etc. currently working for the business.
        """
        mcc: NotRequired[str]
        """
        [The merchant category code for the account](https://stripe.com/connect/setting-mcc). MCCs are used to classify businesses based on the goods or services they provide.
        """
        monthly_estimated_revenue: NotRequired[
            "AccountService.CreateParamsBusinessProfileMonthlyEstimatedRevenue"
        ]
        """
        An estimate of the monthly revenue of the business. Only accepted for accounts in Brazil and India.
        """
        name: NotRequired[str]
        """
        The customer-facing business name.
        """
        product_description: NotRequired[str]
        """
        Internal-only description of the product sold by, or service provided by, the business. Used by Stripe for risk and underwriting purposes.
        """
        support_address: NotRequired[
            "AccountService.CreateParamsBusinessProfileSupportAddress"
        ]
        """
        A publicly available mailing address for sending support issues to.
        """
        support_email: NotRequired[str]
        """
        A publicly available email address for sending support issues to.
        """
        support_phone: NotRequired[str]
        """
        A publicly available phone number to call with support issues.
        """
        support_url: NotRequired["Literal['']|str"]
        """
        A publicly available website for handling support issues.
        """
        url: NotRequired[str]
        """
        The business's publicly available website.
        """

    class CreateParamsBusinessProfileAnnualRevenue(TypedDict):
        amount: int
        """
        A non-negative integer representing the amount in the [smallest currency unit](https://stripe.com/currencies#zero-decimal).
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        fiscal_year_end: str
        """
        The close-out date of the preceding fiscal year in ISO 8601 format. E.g. 2023-12-31 for the 31st of December, 2023.
        """

    class CreateParamsBusinessProfileMonthlyEstimatedRevenue(TypedDict):
        amount: int
        """
        A non-negative integer representing how much to charge in the [smallest currency unit](https://stripe.com/currencies#zero-decimal).
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """

    class CreateParamsBusinessProfileSupportAddress(TypedDict):
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

    class CreateParamsCapabilities(TypedDict):
        acss_debit_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesAcssDebitPayments"
        ]
        """
        The acss_debit_payments capability.
        """
        affirm_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesAffirmPayments"
        ]
        """
        The affirm_payments capability.
        """
        afterpay_clearpay_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesAfterpayClearpayPayments"
        ]
        """
        The afterpay_clearpay_payments capability.
        """
        amazon_pay_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesAmazonPayPayments"
        ]
        """
        The amazon_pay_payments capability.
        """
        au_becs_debit_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesAuBecsDebitPayments"
        ]
        """
        The au_becs_debit_payments capability.
        """
        bacs_debit_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesBacsDebitPayments"
        ]
        """
        The bacs_debit_payments capability.
        """
        bancontact_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesBancontactPayments"
        ]
        """
        The bancontact_payments capability.
        """
        bank_transfer_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesBankTransferPayments"
        ]
        """
        The bank_transfer_payments capability.
        """
        blik_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesBlikPayments"
        ]
        """
        The blik_payments capability.
        """
        boleto_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesBoletoPayments"
        ]
        """
        The boleto_payments capability.
        """
        card_issuing: NotRequired[
            "AccountService.CreateParamsCapabilitiesCardIssuing"
        ]
        """
        The card_issuing capability.
        """
        card_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesCardPayments"
        ]
        """
        The card_payments capability.
        """
        cartes_bancaires_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesCartesBancairesPayments"
        ]
        """
        The cartes_bancaires_payments capability.
        """
        cashapp_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesCashappPayments"
        ]
        """
        The cashapp_payments capability.
        """
        eps_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesEpsPayments"
        ]
        """
        The eps_payments capability.
        """
        fpx_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesFpxPayments"
        ]
        """
        The fpx_payments capability.
        """
        gb_bank_transfer_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesGbBankTransferPayments"
        ]
        """
        The gb_bank_transfer_payments capability.
        """
        giropay_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesGiropayPayments"
        ]
        """
        The giropay_payments capability.
        """
        grabpay_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesGrabpayPayments"
        ]
        """
        The grabpay_payments capability.
        """
        ideal_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesIdealPayments"
        ]
        """
        The ideal_payments capability.
        """
        india_international_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesIndiaInternationalPayments"
        ]
        """
        The india_international_payments capability.
        """
        jcb_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesJcbPayments"
        ]
        """
        The jcb_payments capability.
        """
        jp_bank_transfer_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesJpBankTransferPayments"
        ]
        """
        The jp_bank_transfer_payments capability.
        """
        klarna_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesKlarnaPayments"
        ]
        """
        The klarna_payments capability.
        """
        konbini_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesKonbiniPayments"
        ]
        """
        The konbini_payments capability.
        """
        legacy_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesLegacyPayments"
        ]
        """
        The legacy_payments capability.
        """
        link_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesLinkPayments"
        ]
        """
        The link_payments capability.
        """
        mobilepay_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesMobilepayPayments"
        ]
        """
        The mobilepay_payments capability.
        """
        multibanco_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesMultibancoPayments"
        ]
        """
        The multibanco_payments capability.
        """
        mx_bank_transfer_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesMxBankTransferPayments"
        ]
        """
        The mx_bank_transfer_payments capability.
        """
        oxxo_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesOxxoPayments"
        ]
        """
        The oxxo_payments capability.
        """
        p24_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesP24Payments"
        ]
        """
        The p24_payments capability.
        """
        paynow_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesPaynowPayments"
        ]
        """
        The paynow_payments capability.
        """
        promptpay_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesPromptpayPayments"
        ]
        """
        The promptpay_payments capability.
        """
        revolut_pay_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesRevolutPayPayments"
        ]
        """
        The revolut_pay_payments capability.
        """
        sepa_bank_transfer_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesSepaBankTransferPayments"
        ]
        """
        The sepa_bank_transfer_payments capability.
        """
        sepa_debit_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesSepaDebitPayments"
        ]
        """
        The sepa_debit_payments capability.
        """
        sofort_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesSofortPayments"
        ]
        """
        The sofort_payments capability.
        """
        swish_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesSwishPayments"
        ]
        """
        The swish_payments capability.
        """
        tax_reporting_us_1099_k: NotRequired[
            "AccountService.CreateParamsCapabilitiesTaxReportingUs1099K"
        ]
        """
        The tax_reporting_us_1099_k capability.
        """
        tax_reporting_us_1099_misc: NotRequired[
            "AccountService.CreateParamsCapabilitiesTaxReportingUs1099Misc"
        ]
        """
        The tax_reporting_us_1099_misc capability.
        """
        transfers: NotRequired[
            "AccountService.CreateParamsCapabilitiesTransfers"
        ]
        """
        The transfers capability.
        """
        treasury: NotRequired[
            "AccountService.CreateParamsCapabilitiesTreasury"
        ]
        """
        The treasury capability.
        """
        twint_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesTwintPayments"
        ]
        """
        The twint_payments capability.
        """
        us_bank_account_ach_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesUsBankAccountAchPayments"
        ]
        """
        The us_bank_account_ach_payments capability.
        """
        us_bank_transfer_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesUsBankTransferPayments"
        ]
        """
        The us_bank_transfer_payments capability.
        """
        zip_payments: NotRequired[
            "AccountService.CreateParamsCapabilitiesZipPayments"
        ]
        """
        The zip_payments capability.
        """

    class CreateParamsCapabilitiesAcssDebitPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesAffirmPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesAfterpayClearpayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesAmazonPayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesAuBecsDebitPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesBacsDebitPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesBancontactPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesBlikPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesBoletoPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesCardIssuing(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesCardPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesCartesBancairesPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesCashappPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesEpsPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesFpxPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesGbBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesGiropayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesGrabpayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesIdealPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesIndiaInternationalPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesJcbPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesJpBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesKlarnaPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesKonbiniPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesLegacyPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesLinkPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesMobilepayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesMultibancoPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesMxBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesOxxoPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesP24Payments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesPaynowPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesPromptpayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesRevolutPayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesSepaBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesSepaDebitPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesSofortPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesSwishPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesTaxReportingUs1099K(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesTaxReportingUs1099Misc(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesTransfers(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesTreasury(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesTwintPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesUsBankAccountAchPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesUsBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCapabilitiesZipPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class CreateParamsCard(TypedDict):
        object: Literal["card"]
        address_city: NotRequired[str]
        address_country: NotRequired[str]
        address_line1: NotRequired[str]
        address_line2: NotRequired[str]
        address_state: NotRequired[str]
        address_zip: NotRequired[str]
        currency: NotRequired[str]
        cvc: NotRequired[str]
        exp_month: int
        exp_year: int
        name: NotRequired[str]
        number: str
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
        """
        default_for_currency: NotRequired[bool]

    class CreateParamsCardToken(TypedDict):
        object: Literal["card"]
        currency: NotRequired[str]
        token: str

    class CreateParamsCompany(TypedDict):
        address: NotRequired["AccountService.CreateParamsCompanyAddress"]
        """
        The company's primary address.
        """
        address_kana: NotRequired[
            "AccountService.CreateParamsCompanyAddressKana"
        ]
        """
        The Kana variation of the company's primary address (Japan only).
        """
        address_kanji: NotRequired[
            "AccountService.CreateParamsCompanyAddressKanji"
        ]
        """
        The Kanji variation of the company's primary address (Japan only).
        """
        directors_provided: NotRequired[bool]
        """
        Whether the company's directors have been provided. Set this Boolean to `true` after creating all the company's directors with [the Persons API](https://stripe.com/api/persons) for accounts with a `relationship.director` requirement. This value is not automatically set to `true` after creating directors, so it needs to be updated to indicate all directors have been provided.
        """
        executives_provided: NotRequired[bool]
        """
        Whether the company's executives have been provided. Set this Boolean to `true` after creating all the company's executives with [the Persons API](https://stripe.com/api/persons) for accounts with a `relationship.executive` requirement.
        """
        export_license_id: NotRequired[str]
        """
        The export license ID number of the company, also referred as Import Export Code (India only).
        """
        export_purpose_code: NotRequired[str]
        """
        The purpose code to use for export transactions (India only).
        """
        name: NotRequired[str]
        """
        The company's legal name.
        """
        name_kana: NotRequired[str]
        """
        The Kana variation of the company's legal name (Japan only).
        """
        name_kanji: NotRequired[str]
        """
        The Kanji variation of the company's legal name (Japan only).
        """
        owners_provided: NotRequired[bool]
        """
        Whether the company's owners have been provided. Set this Boolean to `true` after creating all the company's owners with [the Persons API](https://stripe.com/api/persons) for accounts with a `relationship.owner` requirement.
        """
        ownership_declaration: NotRequired[
            "AccountService.CreateParamsCompanyOwnershipDeclaration"
        ]
        """
        This hash is used to attest that the beneficial owner information provided to Stripe is both current and correct.
        """
        phone: NotRequired[str]
        """
        The company's phone number (used for verification).
        """
        registration_number: NotRequired[str]
        """
        The identification number given to a company when it is registered or incorporated, if distinct from the identification number used for filing taxes. (Examples are the CIN for companies and LLP IN for partnerships in India, and the Company Registration Number in Hong Kong).
        """
        structure: NotRequired[
            "Literal['']|Literal['free_zone_establishment', 'free_zone_llc', 'government_instrumentality', 'governmental_unit', 'incorporated_non_profit', 'incorporated_partnership', 'limited_liability_partnership', 'llc', 'multi_member_llc', 'private_company', 'private_corporation', 'private_partnership', 'public_company', 'public_corporation', 'public_partnership', 'registered_charity', 'single_member_llc', 'sole_establishment', 'sole_proprietorship', 'tax_exempt_government_instrumentality', 'unincorporated_association', 'unincorporated_non_profit', 'unincorporated_partnership']"
        ]
        """
        The category identifying the legal structure of the company or legal entity. See [Business structure](https://stripe.com/connect/identity-verification#business-structure) for more details. Pass an empty string to unset this value.
        """
        tax_id: NotRequired[str]
        """
        The business ID number of the company, as appropriate for the company's country. (Examples are an Employer ID Number in the U.S., a Business Number in Canada, or a Company Number in the UK.)
        """
        tax_id_registrar: NotRequired[str]
        """
        The jurisdiction in which the `tax_id` is registered (Germany-based companies only).
        """
        vat_id: NotRequired[str]
        """
        The VAT number of the company.
        """
        verification: NotRequired[
            "AccountService.CreateParamsCompanyVerification"
        ]
        """
        Information on the verification state of the company.
        """

    class CreateParamsCompanyAddress(TypedDict):
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

    class CreateParamsCompanyAddressKana(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class CreateParamsCompanyAddressKanji(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class CreateParamsCompanyOwnershipDeclaration(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp marking when the beneficial owner attestation was made.
        """
        ip: NotRequired[str]
        """
        The IP address from which the beneficial owner attestation was made.
        """
        user_agent: NotRequired[str]
        """
        The user agent of the browser from which the beneficial owner attestation was made.
        """

    class CreateParamsCompanyVerification(TypedDict):
        document: NotRequired[
            "AccountService.CreateParamsCompanyVerificationDocument"
        ]
        """
        A document verifying the business.
        """

    class CreateParamsCompanyVerificationDocument(TypedDict):
        back: NotRequired[str]
        """
        The back of a document returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `additional_verification`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """
        front: NotRequired[str]
        """
        The front of a document returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `additional_verification`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """

    class CreateParamsController(TypedDict):
        fees: NotRequired["AccountService.CreateParamsControllerFees"]
        """
        A hash of configuration for who pays Stripe fees for product usage on this account.
        """
        losses: NotRequired["AccountService.CreateParamsControllerLosses"]
        """
        A hash of configuration for products that have negative balance liability, and whether Stripe or a Connect application is responsible for them.
        """
        requirement_collection: NotRequired[Literal["application", "stripe"]]
        """
        A value indicating responsibility for collecting updated information when requirements on the account are due or change. Defaults to `stripe`.
        """
        stripe_dashboard: NotRequired[
            "AccountService.CreateParamsControllerStripeDashboard"
        ]
        """
        A hash of configuration for Stripe-hosted dashboards.
        """

    class CreateParamsControllerFees(TypedDict):
        payer: NotRequired[Literal["account", "application"]]
        """
        A value indicating the responsible payer of Stripe fees on this account. Defaults to `account`. Learn more about [fee behavior on connected accounts](https://docs.stripe.com/connect/direct-charges-fee-payer-behavior).
        """

    class CreateParamsControllerLosses(TypedDict):
        payments: NotRequired[Literal["application", "stripe"]]
        """
        A value indicating who is liable when this account can't pay back negative balances resulting from payments. Defaults to `stripe`.
        """

    class CreateParamsControllerStripeDashboard(TypedDict):
        type: NotRequired[Literal["express", "full", "none"]]
        """
        Whether this account should have access to the full Stripe Dashboard (`full`), to the Express Dashboard (`express`), or to no Stripe-hosted dashboard (`none`). Defaults to `full`.
        """

    class CreateParamsDocuments(TypedDict):
        bank_account_ownership_verification: NotRequired[
            "AccountService.CreateParamsDocumentsBankAccountOwnershipVerification"
        ]
        """
        One or more documents that support the [Bank account ownership verification](https://support.stripe.com/questions/bank-account-ownership-verification) requirement. Must be a document associated with the account's primary active bank account that displays the last 4 digits of the account number, either a statement or a voided check.
        """
        company_license: NotRequired[
            "AccountService.CreateParamsDocumentsCompanyLicense"
        ]
        """
        One or more documents that demonstrate proof of a company's license to operate.
        """
        company_memorandum_of_association: NotRequired[
            "AccountService.CreateParamsDocumentsCompanyMemorandumOfAssociation"
        ]
        """
        One or more documents showing the company's Memorandum of Association.
        """
        company_ministerial_decree: NotRequired[
            "AccountService.CreateParamsDocumentsCompanyMinisterialDecree"
        ]
        """
        (Certain countries only) One or more documents showing the ministerial decree legalizing the company's establishment.
        """
        company_registration_verification: NotRequired[
            "AccountService.CreateParamsDocumentsCompanyRegistrationVerification"
        ]
        """
        One or more documents that demonstrate proof of a company's registration with the appropriate local authorities.
        """
        company_tax_id_verification: NotRequired[
            "AccountService.CreateParamsDocumentsCompanyTaxIdVerification"
        ]
        """
        One or more documents that demonstrate proof of a company's tax ID.
        """
        proof_of_registration: NotRequired[
            "AccountService.CreateParamsDocumentsProofOfRegistration"
        ]
        """
        One or more documents showing the company's proof of registration with the national business registry.
        """

    class CreateParamsDocumentsBankAccountOwnershipVerification(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class CreateParamsDocumentsCompanyLicense(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class CreateParamsDocumentsCompanyMemorandumOfAssociation(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class CreateParamsDocumentsCompanyMinisterialDecree(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class CreateParamsDocumentsCompanyRegistrationVerification(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class CreateParamsDocumentsCompanyTaxIdVerification(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class CreateParamsDocumentsProofOfRegistration(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class CreateParamsIndividual(TypedDict):
        address: NotRequired["AccountService.CreateParamsIndividualAddress"]
        """
        The individual's primary address.
        """
        address_kana: NotRequired[
            "AccountService.CreateParamsIndividualAddressKana"
        ]
        """
        The Kana variation of the individual's primary address (Japan only).
        """
        address_kanji: NotRequired[
            "AccountService.CreateParamsIndividualAddressKanji"
        ]
        """
        The Kanji variation of the individual's primary address (Japan only).
        """
        dob: NotRequired[
            "Literal['']|AccountService.CreateParamsIndividualDob"
        ]
        """
        The individual's date of birth.
        """
        email: NotRequired[str]
        """
        The individual's email address.
        """
        first_name: NotRequired[str]
        """
        The individual's first name.
        """
        first_name_kana: NotRequired[str]
        """
        The Kana variation of the individual's first name (Japan only).
        """
        first_name_kanji: NotRequired[str]
        """
        The Kanji variation of the individual's first name (Japan only).
        """
        full_name_aliases: NotRequired["Literal['']|List[str]"]
        """
        A list of alternate names or aliases that the individual is known by.
        """
        gender: NotRequired[str]
        """
        The individual's gender (International regulations require either "male" or "female").
        """
        id_number: NotRequired[str]
        """
        The government-issued ID number of the individual, as appropriate for the representative's country. (Examples are a Social Security Number in the U.S., or a Social Insurance Number in Canada). Instead of the number itself, you can also provide a [PII token created with Stripe.js](https://stripe.com/js/tokens/create_token?type=pii).
        """
        id_number_secondary: NotRequired[str]
        """
        The government-issued secondary ID number of the individual, as appropriate for the representative's country, will be used for enhanced verification checks. In Thailand, this would be the laser code found on the back of an ID card. Instead of the number itself, you can also provide a [PII token created with Stripe.js](https://stripe.com/js/tokens/create_token?type=pii).
        """
        last_name: NotRequired[str]
        """
        The individual's last name.
        """
        last_name_kana: NotRequired[str]
        """
        The Kana variation of the individual's last name (Japan only).
        """
        last_name_kanji: NotRequired[str]
        """
        The Kanji variation of the individual's last name (Japan only).
        """
        maiden_name: NotRequired[str]
        """
        The individual's maiden name.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        phone: NotRequired[str]
        """
        The individual's phone number.
        """
        political_exposure: NotRequired[Literal["existing", "none"]]
        """
        Indicates if the person or any of their representatives, family members, or other closely related persons, declares that they hold or have held an important public job or function, in any jurisdiction.
        """
        registered_address: NotRequired[
            "AccountService.CreateParamsIndividualRegisteredAddress"
        ]
        """
        The individual's registered address.
        """
        relationship: NotRequired[
            "AccountService.CreateParamsIndividualRelationship"
        ]
        """
        Describes the person's relationship to the account.
        """
        ssn_last_4: NotRequired[str]
        """
        The last four digits of the individual's Social Security Number (U.S. only).
        """
        verification: NotRequired[
            "AccountService.CreateParamsIndividualVerification"
        ]
        """
        The individual's verification document information.
        """

    class CreateParamsIndividualAddress(TypedDict):
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

    class CreateParamsIndividualAddressKana(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class CreateParamsIndividualAddressKanji(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class CreateParamsIndividualDob(TypedDict):
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

    class CreateParamsIndividualRegisteredAddress(TypedDict):
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

    class CreateParamsIndividualRelationship(TypedDict):
        director: NotRequired[bool]
        """
        Whether the person is a director of the account's legal entity. Directors are typically members of the governing board of the company, or responsible for ensuring the company meets its regulatory obligations.
        """
        executive: NotRequired[bool]
        """
        Whether the person has significant responsibility to control, manage, or direct the organization.
        """
        owner: NotRequired[bool]
        """
        Whether the person is an owner of the account's legal entity.
        """
        percent_ownership: NotRequired["Literal['']|float"]
        """
        The percent owned by the person of the account's legal entity.
        """
        title: NotRequired[str]
        """
        The person's title (e.g., CEO, Support Engineer).
        """

    class CreateParamsIndividualVerification(TypedDict):
        additional_document: NotRequired[
            "AccountService.CreateParamsIndividualVerificationAdditionalDocument"
        ]
        """
        A document showing address, either a passport, local ID card, or utility bill from a well-known utility company.
        """
        document: NotRequired[
            "AccountService.CreateParamsIndividualVerificationDocument"
        ]
        """
        An identifying document, either a passport or local ID card.
        """

    class CreateParamsIndividualVerificationAdditionalDocument(TypedDict):
        back: NotRequired[str]
        """
        The back of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """
        front: NotRequired[str]
        """
        The front of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """

    class CreateParamsIndividualVerificationDocument(TypedDict):
        back: NotRequired[str]
        """
        The back of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """
        front: NotRequired[str]
        """
        The front of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """

    class CreateParamsSettings(TypedDict):
        bacs_debit_payments: NotRequired[
            "AccountService.CreateParamsSettingsBacsDebitPayments"
        ]
        """
        Settings specific to Bacs Direct Debit.
        """
        branding: NotRequired["AccountService.CreateParamsSettingsBranding"]
        """
        Settings used to apply the account's branding to email receipts, invoices, Checkout, and other products.
        """
        card_issuing: NotRequired[
            "AccountService.CreateParamsSettingsCardIssuing"
        ]
        """
        Settings specific to the account's use of the Card Issuing product.
        """
        card_payments: NotRequired[
            "AccountService.CreateParamsSettingsCardPayments"
        ]
        """
        Settings specific to card charging on the account.
        """
        payments: NotRequired["AccountService.CreateParamsSettingsPayments"]
        """
        Settings that apply across payment methods for charging on the account.
        """
        payouts: NotRequired["AccountService.CreateParamsSettingsPayouts"]
        """
        Settings specific to the account's payouts.
        """
        treasury: NotRequired["AccountService.CreateParamsSettingsTreasury"]
        """
        Settings specific to the account's Treasury FinancialAccounts.
        """

    class CreateParamsSettingsBacsDebitPayments(TypedDict):
        display_name: NotRequired[str]
        """
        The Bacs Direct Debit Display Name for this account. For payments made with Bacs Direct Debit, this name appears on the mandate as the statement descriptor. Mobile banking apps display it as the name of the business. To use custom branding, set the Bacs Direct Debit Display Name during or right after creation. Custom branding incurs an additional monthly fee for the platform. If you don't set the display name before requesting Bacs capability, it's automatically set as "Stripe" and the account is onboarded to Stripe branding, which is free.
        """

    class CreateParamsSettingsBranding(TypedDict):
        icon: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) An icon for the account. Must be square and at least 128px x 128px.
        """
        logo: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) A logo for the account that will be used in Checkout instead of the icon and without the account's name next to it if provided. Must be at least 128px x 128px.
        """
        primary_color: NotRequired[str]
        """
        A CSS hex color value representing the primary branding color for this account.
        """
        secondary_color: NotRequired[str]
        """
        A CSS hex color value representing the secondary branding color for this account.
        """

    class CreateParamsSettingsCardIssuing(TypedDict):
        tos_acceptance: NotRequired[
            "AccountService.CreateParamsSettingsCardIssuingTosAcceptance"
        ]
        """
        Details on the account's acceptance of the [Stripe Issuing Terms and Disclosures](https://stripe.com/issuing/connect/tos_acceptance).
        """

    class CreateParamsSettingsCardIssuingTosAcceptance(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp marking when the account representative accepted the service agreement.
        """
        ip: NotRequired[str]
        """
        The IP address from which the account representative accepted the service agreement.
        """
        user_agent: NotRequired["Literal['']|str"]
        """
        The user agent of the browser from which the account representative accepted the service agreement.
        """

    class CreateParamsSettingsCardPayments(TypedDict):
        decline_on: NotRequired[
            "AccountService.CreateParamsSettingsCardPaymentsDeclineOn"
        ]
        """
        Automatically declines certain charge types regardless of whether the card issuer accepted or declined the charge.
        """
        statement_descriptor_prefix: NotRequired[str]
        """
        The default text that appears on credit card statements when a charge is made. This field prefixes any dynamic `statement_descriptor` specified on the charge. `statement_descriptor_prefix` is useful for maximizing descriptor space for the dynamic portion.
        """
        statement_descriptor_prefix_kana: NotRequired["Literal['']|str"]
        """
        The Kana variation of the default text that appears on credit card statements when a charge is made (Japan only). This field prefixes any dynamic `statement_descriptor_suffix_kana` specified on the charge. `statement_descriptor_prefix_kana` is useful for maximizing descriptor space for the dynamic portion.
        """
        statement_descriptor_prefix_kanji: NotRequired["Literal['']|str"]
        """
        The Kanji variation of the default text that appears on credit card statements when a charge is made (Japan only). This field prefixes any dynamic `statement_descriptor_suffix_kanji` specified on the charge. `statement_descriptor_prefix_kanji` is useful for maximizing descriptor space for the dynamic portion.
        """

    class CreateParamsSettingsCardPaymentsDeclineOn(TypedDict):
        avs_failure: NotRequired[bool]
        """
        Whether Stripe automatically declines charges with an incorrect ZIP or postal code. This setting only applies when a ZIP or postal code is provided and they fail bank verification.
        """
        cvc_failure: NotRequired[bool]
        """
        Whether Stripe automatically declines charges with an incorrect CVC. This setting only applies when a CVC is provided and it fails bank verification.
        """

    class CreateParamsSettingsPayments(TypedDict):
        statement_descriptor: NotRequired[str]
        """
        The default text that appears on statements for non-card charges outside of Japan. For card charges, if you don't set a `statement_descriptor_prefix`, this text is also used as the statement descriptor prefix. In that case, if concatenating the statement descriptor suffix causes the combined statement descriptor to exceed 22 characters, we truncate the `statement_descriptor` text to limit the full descriptor to 22 characters. For more information about statement descriptors and their requirements, see the [account settings documentation](https://docs.stripe.com/get-started/account/statement-descriptors).
        """
        statement_descriptor_kana: NotRequired[str]
        """
        The Kana variation of `statement_descriptor` used for charges in Japan. Japanese statement descriptors have [special requirements](https://docs.stripe.com/get-started/account/statement-descriptors#set-japanese-statement-descriptors).
        """
        statement_descriptor_kanji: NotRequired[str]
        """
        The Kanji variation of `statement_descriptor` used for charges in Japan. Japanese statement descriptors have [special requirements](https://docs.stripe.com/get-started/account/statement-descriptors#set-japanese-statement-descriptors).
        """

    class CreateParamsSettingsPayouts(TypedDict):
        debit_negative_balances: NotRequired[bool]
        """
        A Boolean indicating whether Stripe should try to reclaim negative balances from an attached bank account. For details, see [Understanding Connect Account Balances](https://stripe.com/connect/account-balances).
        """
        schedule: NotRequired[
            "AccountService.CreateParamsSettingsPayoutsSchedule"
        ]
        """
        Details on when funds from charges are available, and when they are paid out to an external account. For details, see our [Setting Bank and Debit Card Payouts](https://stripe.com/connect/bank-transfers#payout-information) documentation.
        """
        statement_descriptor: NotRequired[str]
        """
        The text that appears on the bank account statement for payouts. If not set, this defaults to the platform's bank descriptor as set in the Dashboard.
        """

    class CreateParamsSettingsPayoutsSchedule(TypedDict):
        delay_days: NotRequired["Literal['minimum']|int"]
        """
        The number of days charge funds are held before being paid out. May also be set to `minimum`, representing the lowest available value for the account country. Default is `minimum`. The `delay_days` parameter remains at the last configured value if `interval` is `manual`. [Learn more about controlling payout delay days](https://stripe.com/connect/manage-payout-schedule).
        """
        interval: NotRequired[Literal["daily", "manual", "monthly", "weekly"]]
        """
        How frequently available funds are paid out. One of: `daily`, `manual`, `weekly`, or `monthly`. Default is `daily`.
        """
        monthly_anchor: NotRequired[int]
        """
        The day of the month when available funds are paid out, specified as a number between 1--31. Payouts nominally scheduled between the 29th and 31st of the month are instead sent on the last day of a shorter month. Required and applicable only if `interval` is `monthly`.
        """
        weekly_anchor: NotRequired[
            Literal[
                "friday",
                "monday",
                "saturday",
                "sunday",
                "thursday",
                "tuesday",
                "wednesday",
            ]
        ]
        """
        The day of the week when available funds are paid out, specified as `monday`, `tuesday`, etc. (required and applicable only if `interval` is `weekly`.)
        """

    class CreateParamsSettingsTreasury(TypedDict):
        tos_acceptance: NotRequired[
            "AccountService.CreateParamsSettingsTreasuryTosAcceptance"
        ]
        """
        Details on the account's acceptance of the Stripe Treasury Services Agreement.
        """

    class CreateParamsSettingsTreasuryTosAcceptance(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp marking when the account representative accepted the service agreement.
        """
        ip: NotRequired[str]
        """
        The IP address from which the account representative accepted the service agreement.
        """
        user_agent: NotRequired["Literal['']|str"]
        """
        The user agent of the browser from which the account representative accepted the service agreement.
        """

    class CreateParamsTosAcceptance(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp marking when the account representative accepted their service agreement.
        """
        ip: NotRequired[str]
        """
        The IP address from which the account representative accepted their service agreement.
        """
        service_agreement: NotRequired[str]
        """
        The user's service agreement type.
        """
        user_agent: NotRequired[str]
        """
        The user agent of the browser from which the account representative accepted their service agreement.
        """

    class DeleteParams(TypedDict):
        pass

    class ListParams(TypedDict):
        created: NotRequired["AccountService.ListParamsCreated|int"]
        """
        Only return connected accounts that were created during the given date interval.
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

    class RejectParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        reason: str
        """
        The reason for rejecting the account. Can be `fraud`, `terms_of_service`, or `other`.
        """

    class RetrieveCurrentParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        account_token: NotRequired[str]
        """
        An [account token](https://stripe.com/docs/api#create_account_token), used to securely provide details to the account.
        """
        business_profile: NotRequired[
            "AccountService.UpdateParamsBusinessProfile"
        ]
        """
        Business information about the account.
        """
        business_type: NotRequired[
            Literal["company", "government_entity", "individual", "non_profit"]
        ]
        """
        The business type. Once you create an [Account Link](https://stripe.com/api/account_links) or [Account Session](https://stripe.com/api/account_sessions), this property can only be updated for accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts.
        """
        capabilities: NotRequired["AccountService.UpdateParamsCapabilities"]
        """
        Each key of the dictionary represents a capability, and each capability
        maps to its settings (for example, whether it has been requested or not). Each
        capability is inactive until you have provided its specific
        requirements and Stripe has verified them. An account might have some
        of its requested capabilities be active and some be inactive.

        Required when [account.controller.stripe_dashboard.type](https://stripe.com/api/accounts/create#create_account-controller-dashboard-type)
        is `none`, which includes Custom accounts.
        """
        company: NotRequired["AccountService.UpdateParamsCompany"]
        """
        Information about the company or business. This field is available for any `business_type`. Once you create an [Account Link](https://stripe.com/api/account_links) or [Account Session](https://stripe.com/api/account_sessions), this property can only be updated for accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts.
        """
        default_currency: NotRequired[str]
        """
        Three-letter ISO currency code representing the default currency for the account. This must be a currency that [Stripe supports in the account's country](https://docs.stripe.com/payouts).
        """
        documents: NotRequired["AccountService.UpdateParamsDocuments"]
        """
        Documents that may be submitted to satisfy various informational requests.
        """
        email: NotRequired[str]
        """
        The email address of the account holder. This is only to make the account easier to identify to you. If [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts, Stripe doesn't email the account without your consent.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        external_account: NotRequired[
            "Literal['']|str|AccountService.UpdateParamsBankAccount|AccountService.UpdateParamsCard|AccountService.UpdateParamsCardToken"
        ]
        """
        A card or bank account to attach to the account for receiving [payouts](https://stripe.com/connect/bank-debit-card-payouts) (you won't be able to use it for top-ups). You can provide either a token, like the ones returned by [Stripe.js](https://stripe.com/js), or a dictionary, as documented in the `external_account` parameter for [bank account](https://stripe.com/api#account_create_bank_account) creation.

        By default, providing an external account sets it as the new default external account for its currency, and deletes the old default if one exists. To add additional external accounts without replacing the existing default for the currency, use the [bank account](https://stripe.com/api#account_create_bank_account) or [card creation](https://stripe.com/api#account_create_card) APIs. After you create an [Account Link](https://stripe.com/api/account_links) or [Account Session](https://stripe.com/api/account_sessions), this property can only be updated for accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts.
        """
        individual: NotRequired["AccountService.UpdateParamsIndividual"]
        """
        Information about the person represented by the account. This field is null unless `business_type` is set to `individual`. Once you create an [Account Link](https://stripe.com/api/account_links) or [Account Session](https://stripe.com/api/account_sessions), this property can only be updated for accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        settings: NotRequired["AccountService.UpdateParamsSettings"]
        """
        Options for customizing how the account functions within Stripe.
        """
        tos_acceptance: NotRequired["AccountService.UpdateParamsTosAcceptance"]
        """
        Details on the account's acceptance of the [Stripe Services Agreement](https://stripe.com/connect/updating-accounts#tos-acceptance). This property can only be updated for accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection) is `application`, which includes Custom accounts. This property defaults to a `full` service agreement when empty.
        """

    class UpdateParamsBankAccount(TypedDict):
        object: Literal["bank_account"]
        account_holder_name: NotRequired[str]
        """
        The name of the person or business that owns the bank account.This field is required when attaching the bank account to a `Customer` object.
        """
        account_holder_type: NotRequired[Literal["company", "individual"]]
        """
        The type of entity that holds the account. It can be `company` or `individual`. This field is required when attaching the bank account to a `Customer` object.
        """
        account_number: str
        """
        The account number for the bank account, in string form. Must be a checking account.
        """
        country: str
        """
        The country in which the bank account is located.
        """
        currency: NotRequired[str]
        """
        The currency the bank account is in. This must be a country/currency pairing that [Stripe supports.](docs/payouts)
        """
        routing_number: NotRequired[str]
        """
        The routing number, sort code, or other country-appropriateinstitution number for the bank account. For US bank accounts, this is required and should bethe ACH routing number, not the wire routing number. If you are providing an IBAN for`account_number`, this field is not required.
        """

    class UpdateParamsBusinessProfile(TypedDict):
        annual_revenue: NotRequired[
            "AccountService.UpdateParamsBusinessProfileAnnualRevenue"
        ]
        """
        The applicant's gross annual revenue for its preceding fiscal year.
        """
        estimated_worker_count: NotRequired[int]
        """
        An estimated upper bound of employees, contractors, vendors, etc. currently working for the business.
        """
        mcc: NotRequired[str]
        """
        [The merchant category code for the account](https://stripe.com/connect/setting-mcc). MCCs are used to classify businesses based on the goods or services they provide.
        """
        monthly_estimated_revenue: NotRequired[
            "AccountService.UpdateParamsBusinessProfileMonthlyEstimatedRevenue"
        ]
        """
        An estimate of the monthly revenue of the business. Only accepted for accounts in Brazil and India.
        """
        name: NotRequired[str]
        """
        The customer-facing business name.
        """
        product_description: NotRequired[str]
        """
        Internal-only description of the product sold by, or service provided by, the business. Used by Stripe for risk and underwriting purposes.
        """
        support_address: NotRequired[
            "AccountService.UpdateParamsBusinessProfileSupportAddress"
        ]
        """
        A publicly available mailing address for sending support issues to.
        """
        support_email: NotRequired[str]
        """
        A publicly available email address for sending support issues to.
        """
        support_phone: NotRequired[str]
        """
        A publicly available phone number to call with support issues.
        """
        support_url: NotRequired["Literal['']|str"]
        """
        A publicly available website for handling support issues.
        """
        url: NotRequired[str]
        """
        The business's publicly available website.
        """

    class UpdateParamsBusinessProfileAnnualRevenue(TypedDict):
        amount: int
        """
        A non-negative integer representing the amount in the [smallest currency unit](https://stripe.com/currencies#zero-decimal).
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        fiscal_year_end: str
        """
        The close-out date of the preceding fiscal year in ISO 8601 format. E.g. 2023-12-31 for the 31st of December, 2023.
        """

    class UpdateParamsBusinessProfileMonthlyEstimatedRevenue(TypedDict):
        amount: int
        """
        A non-negative integer representing how much to charge in the [smallest currency unit](https://stripe.com/currencies#zero-decimal).
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """

    class UpdateParamsBusinessProfileSupportAddress(TypedDict):
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

    class UpdateParamsCapabilities(TypedDict):
        acss_debit_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesAcssDebitPayments"
        ]
        """
        The acss_debit_payments capability.
        """
        affirm_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesAffirmPayments"
        ]
        """
        The affirm_payments capability.
        """
        afterpay_clearpay_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesAfterpayClearpayPayments"
        ]
        """
        The afterpay_clearpay_payments capability.
        """
        amazon_pay_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesAmazonPayPayments"
        ]
        """
        The amazon_pay_payments capability.
        """
        au_becs_debit_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesAuBecsDebitPayments"
        ]
        """
        The au_becs_debit_payments capability.
        """
        bacs_debit_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesBacsDebitPayments"
        ]
        """
        The bacs_debit_payments capability.
        """
        bancontact_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesBancontactPayments"
        ]
        """
        The bancontact_payments capability.
        """
        bank_transfer_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesBankTransferPayments"
        ]
        """
        The bank_transfer_payments capability.
        """
        blik_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesBlikPayments"
        ]
        """
        The blik_payments capability.
        """
        boleto_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesBoletoPayments"
        ]
        """
        The boleto_payments capability.
        """
        card_issuing: NotRequired[
            "AccountService.UpdateParamsCapabilitiesCardIssuing"
        ]
        """
        The card_issuing capability.
        """
        card_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesCardPayments"
        ]
        """
        The card_payments capability.
        """
        cartes_bancaires_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesCartesBancairesPayments"
        ]
        """
        The cartes_bancaires_payments capability.
        """
        cashapp_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesCashappPayments"
        ]
        """
        The cashapp_payments capability.
        """
        eps_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesEpsPayments"
        ]
        """
        The eps_payments capability.
        """
        fpx_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesFpxPayments"
        ]
        """
        The fpx_payments capability.
        """
        gb_bank_transfer_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesGbBankTransferPayments"
        ]
        """
        The gb_bank_transfer_payments capability.
        """
        giropay_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesGiropayPayments"
        ]
        """
        The giropay_payments capability.
        """
        grabpay_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesGrabpayPayments"
        ]
        """
        The grabpay_payments capability.
        """
        ideal_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesIdealPayments"
        ]
        """
        The ideal_payments capability.
        """
        india_international_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesIndiaInternationalPayments"
        ]
        """
        The india_international_payments capability.
        """
        jcb_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesJcbPayments"
        ]
        """
        The jcb_payments capability.
        """
        jp_bank_transfer_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesJpBankTransferPayments"
        ]
        """
        The jp_bank_transfer_payments capability.
        """
        klarna_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesKlarnaPayments"
        ]
        """
        The klarna_payments capability.
        """
        konbini_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesKonbiniPayments"
        ]
        """
        The konbini_payments capability.
        """
        legacy_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesLegacyPayments"
        ]
        """
        The legacy_payments capability.
        """
        link_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesLinkPayments"
        ]
        """
        The link_payments capability.
        """
        mobilepay_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesMobilepayPayments"
        ]
        """
        The mobilepay_payments capability.
        """
        multibanco_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesMultibancoPayments"
        ]
        """
        The multibanco_payments capability.
        """
        mx_bank_transfer_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesMxBankTransferPayments"
        ]
        """
        The mx_bank_transfer_payments capability.
        """
        oxxo_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesOxxoPayments"
        ]
        """
        The oxxo_payments capability.
        """
        p24_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesP24Payments"
        ]
        """
        The p24_payments capability.
        """
        paynow_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesPaynowPayments"
        ]
        """
        The paynow_payments capability.
        """
        promptpay_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesPromptpayPayments"
        ]
        """
        The promptpay_payments capability.
        """
        revolut_pay_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesRevolutPayPayments"
        ]
        """
        The revolut_pay_payments capability.
        """
        sepa_bank_transfer_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesSepaBankTransferPayments"
        ]
        """
        The sepa_bank_transfer_payments capability.
        """
        sepa_debit_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesSepaDebitPayments"
        ]
        """
        The sepa_debit_payments capability.
        """
        sofort_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesSofortPayments"
        ]
        """
        The sofort_payments capability.
        """
        swish_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesSwishPayments"
        ]
        """
        The swish_payments capability.
        """
        tax_reporting_us_1099_k: NotRequired[
            "AccountService.UpdateParamsCapabilitiesTaxReportingUs1099K"
        ]
        """
        The tax_reporting_us_1099_k capability.
        """
        tax_reporting_us_1099_misc: NotRequired[
            "AccountService.UpdateParamsCapabilitiesTaxReportingUs1099Misc"
        ]
        """
        The tax_reporting_us_1099_misc capability.
        """
        transfers: NotRequired[
            "AccountService.UpdateParamsCapabilitiesTransfers"
        ]
        """
        The transfers capability.
        """
        treasury: NotRequired[
            "AccountService.UpdateParamsCapabilitiesTreasury"
        ]
        """
        The treasury capability.
        """
        twint_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesTwintPayments"
        ]
        """
        The twint_payments capability.
        """
        us_bank_account_ach_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesUsBankAccountAchPayments"
        ]
        """
        The us_bank_account_ach_payments capability.
        """
        us_bank_transfer_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesUsBankTransferPayments"
        ]
        """
        The us_bank_transfer_payments capability.
        """
        zip_payments: NotRequired[
            "AccountService.UpdateParamsCapabilitiesZipPayments"
        ]
        """
        The zip_payments capability.
        """

    class UpdateParamsCapabilitiesAcssDebitPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesAffirmPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesAfterpayClearpayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesAmazonPayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesAuBecsDebitPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesBacsDebitPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesBancontactPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesBlikPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesBoletoPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesCardIssuing(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesCardPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesCartesBancairesPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesCashappPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesEpsPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesFpxPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesGbBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesGiropayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesGrabpayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesIdealPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesIndiaInternationalPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesJcbPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesJpBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesKlarnaPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesKonbiniPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesLegacyPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesLinkPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesMobilepayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesMultibancoPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesMxBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesOxxoPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesP24Payments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesPaynowPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesPromptpayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesRevolutPayPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesSepaBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesSepaDebitPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesSofortPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesSwishPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesTaxReportingUs1099K(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesTaxReportingUs1099Misc(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesTransfers(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesTreasury(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesTwintPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesUsBankAccountAchPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesUsBankTransferPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCapabilitiesZipPayments(TypedDict):
        requested: NotRequired[bool]
        """
        Passing true requests the capability for the account, if it is not already requested. A requested capability may not immediately become active. Any requirements to activate the capability are returned in the `requirements` arrays.
        """

    class UpdateParamsCard(TypedDict):
        object: Literal["card"]
        address_city: NotRequired[str]
        address_country: NotRequired[str]
        address_line1: NotRequired[str]
        address_line2: NotRequired[str]
        address_state: NotRequired[str]
        address_zip: NotRequired[str]
        currency: NotRequired[str]
        cvc: NotRequired[str]
        exp_month: int
        exp_year: int
        name: NotRequired[str]
        number: str
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
        """
        default_for_currency: NotRequired[bool]

    class UpdateParamsCardToken(TypedDict):
        object: Literal["card"]
        currency: NotRequired[str]
        token: str

    class UpdateParamsCompany(TypedDict):
        address: NotRequired["AccountService.UpdateParamsCompanyAddress"]
        """
        The company's primary address.
        """
        address_kana: NotRequired[
            "AccountService.UpdateParamsCompanyAddressKana"
        ]
        """
        The Kana variation of the company's primary address (Japan only).
        """
        address_kanji: NotRequired[
            "AccountService.UpdateParamsCompanyAddressKanji"
        ]
        """
        The Kanji variation of the company's primary address (Japan only).
        """
        directors_provided: NotRequired[bool]
        """
        Whether the company's directors have been provided. Set this Boolean to `true` after creating all the company's directors with [the Persons API](https://stripe.com/api/persons) for accounts with a `relationship.director` requirement. This value is not automatically set to `true` after creating directors, so it needs to be updated to indicate all directors have been provided.
        """
        executives_provided: NotRequired[bool]
        """
        Whether the company's executives have been provided. Set this Boolean to `true` after creating all the company's executives with [the Persons API](https://stripe.com/api/persons) for accounts with a `relationship.executive` requirement.
        """
        export_license_id: NotRequired[str]
        """
        The export license ID number of the company, also referred as Import Export Code (India only).
        """
        export_purpose_code: NotRequired[str]
        """
        The purpose code to use for export transactions (India only).
        """
        name: NotRequired[str]
        """
        The company's legal name.
        """
        name_kana: NotRequired[str]
        """
        The Kana variation of the company's legal name (Japan only).
        """
        name_kanji: NotRequired[str]
        """
        The Kanji variation of the company's legal name (Japan only).
        """
        owners_provided: NotRequired[bool]
        """
        Whether the company's owners have been provided. Set this Boolean to `true` after creating all the company's owners with [the Persons API](https://stripe.com/api/persons) for accounts with a `relationship.owner` requirement.
        """
        ownership_declaration: NotRequired[
            "AccountService.UpdateParamsCompanyOwnershipDeclaration"
        ]
        """
        This hash is used to attest that the beneficial owner information provided to Stripe is both current and correct.
        """
        phone: NotRequired[str]
        """
        The company's phone number (used for verification).
        """
        registration_number: NotRequired[str]
        """
        The identification number given to a company when it is registered or incorporated, if distinct from the identification number used for filing taxes. (Examples are the CIN for companies and LLP IN for partnerships in India, and the Company Registration Number in Hong Kong).
        """
        structure: NotRequired[
            "Literal['']|Literal['free_zone_establishment', 'free_zone_llc', 'government_instrumentality', 'governmental_unit', 'incorporated_non_profit', 'incorporated_partnership', 'limited_liability_partnership', 'llc', 'multi_member_llc', 'private_company', 'private_corporation', 'private_partnership', 'public_company', 'public_corporation', 'public_partnership', 'registered_charity', 'single_member_llc', 'sole_establishment', 'sole_proprietorship', 'tax_exempt_government_instrumentality', 'unincorporated_association', 'unincorporated_non_profit', 'unincorporated_partnership']"
        ]
        """
        The category identifying the legal structure of the company or legal entity. See [Business structure](https://stripe.com/connect/identity-verification#business-structure) for more details. Pass an empty string to unset this value.
        """
        tax_id: NotRequired[str]
        """
        The business ID number of the company, as appropriate for the company's country. (Examples are an Employer ID Number in the U.S., a Business Number in Canada, or a Company Number in the UK.)
        """
        tax_id_registrar: NotRequired[str]
        """
        The jurisdiction in which the `tax_id` is registered (Germany-based companies only).
        """
        vat_id: NotRequired[str]
        """
        The VAT number of the company.
        """
        verification: NotRequired[
            "AccountService.UpdateParamsCompanyVerification"
        ]
        """
        Information on the verification state of the company.
        """

    class UpdateParamsCompanyAddress(TypedDict):
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

    class UpdateParamsCompanyAddressKana(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class UpdateParamsCompanyAddressKanji(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class UpdateParamsCompanyOwnershipDeclaration(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp marking when the beneficial owner attestation was made.
        """
        ip: NotRequired[str]
        """
        The IP address from which the beneficial owner attestation was made.
        """
        user_agent: NotRequired[str]
        """
        The user agent of the browser from which the beneficial owner attestation was made.
        """

    class UpdateParamsCompanyVerification(TypedDict):
        document: NotRequired[
            "AccountService.UpdateParamsCompanyVerificationDocument"
        ]
        """
        A document verifying the business.
        """

    class UpdateParamsCompanyVerificationDocument(TypedDict):
        back: NotRequired[str]
        """
        The back of a document returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `additional_verification`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """
        front: NotRequired[str]
        """
        The front of a document returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `additional_verification`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """

    class UpdateParamsDocuments(TypedDict):
        bank_account_ownership_verification: NotRequired[
            "AccountService.UpdateParamsDocumentsBankAccountOwnershipVerification"
        ]
        """
        One or more documents that support the [Bank account ownership verification](https://support.stripe.com/questions/bank-account-ownership-verification) requirement. Must be a document associated with the account's primary active bank account that displays the last 4 digits of the account number, either a statement or a voided check.
        """
        company_license: NotRequired[
            "AccountService.UpdateParamsDocumentsCompanyLicense"
        ]
        """
        One or more documents that demonstrate proof of a company's license to operate.
        """
        company_memorandum_of_association: NotRequired[
            "AccountService.UpdateParamsDocumentsCompanyMemorandumOfAssociation"
        ]
        """
        One or more documents showing the company's Memorandum of Association.
        """
        company_ministerial_decree: NotRequired[
            "AccountService.UpdateParamsDocumentsCompanyMinisterialDecree"
        ]
        """
        (Certain countries only) One or more documents showing the ministerial decree legalizing the company's establishment.
        """
        company_registration_verification: NotRequired[
            "AccountService.UpdateParamsDocumentsCompanyRegistrationVerification"
        ]
        """
        One or more documents that demonstrate proof of a company's registration with the appropriate local authorities.
        """
        company_tax_id_verification: NotRequired[
            "AccountService.UpdateParamsDocumentsCompanyTaxIdVerification"
        ]
        """
        One or more documents that demonstrate proof of a company's tax ID.
        """
        proof_of_registration: NotRequired[
            "AccountService.UpdateParamsDocumentsProofOfRegistration"
        ]
        """
        One or more documents showing the company's proof of registration with the national business registry.
        """

    class UpdateParamsDocumentsBankAccountOwnershipVerification(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class UpdateParamsDocumentsCompanyLicense(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class UpdateParamsDocumentsCompanyMemorandumOfAssociation(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class UpdateParamsDocumentsCompanyMinisterialDecree(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class UpdateParamsDocumentsCompanyRegistrationVerification(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class UpdateParamsDocumentsCompanyTaxIdVerification(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class UpdateParamsDocumentsProofOfRegistration(TypedDict):
        files: NotRequired[List[str]]
        """
        One or more document ids returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `account_requirement`.
        """

    class UpdateParamsIndividual(TypedDict):
        address: NotRequired["AccountService.UpdateParamsIndividualAddress"]
        """
        The individual's primary address.
        """
        address_kana: NotRequired[
            "AccountService.UpdateParamsIndividualAddressKana"
        ]
        """
        The Kana variation of the individual's primary address (Japan only).
        """
        address_kanji: NotRequired[
            "AccountService.UpdateParamsIndividualAddressKanji"
        ]
        """
        The Kanji variation of the individual's primary address (Japan only).
        """
        dob: NotRequired[
            "Literal['']|AccountService.UpdateParamsIndividualDob"
        ]
        """
        The individual's date of birth.
        """
        email: NotRequired[str]
        """
        The individual's email address.
        """
        first_name: NotRequired[str]
        """
        The individual's first name.
        """
        first_name_kana: NotRequired[str]
        """
        The Kana variation of the individual's first name (Japan only).
        """
        first_name_kanji: NotRequired[str]
        """
        The Kanji variation of the individual's first name (Japan only).
        """
        full_name_aliases: NotRequired["Literal['']|List[str]"]
        """
        A list of alternate names or aliases that the individual is known by.
        """
        gender: NotRequired[str]
        """
        The individual's gender (International regulations require either "male" or "female").
        """
        id_number: NotRequired[str]
        """
        The government-issued ID number of the individual, as appropriate for the representative's country. (Examples are a Social Security Number in the U.S., or a Social Insurance Number in Canada). Instead of the number itself, you can also provide a [PII token created with Stripe.js](https://stripe.com/js/tokens/create_token?type=pii).
        """
        id_number_secondary: NotRequired[str]
        """
        The government-issued secondary ID number of the individual, as appropriate for the representative's country, will be used for enhanced verification checks. In Thailand, this would be the laser code found on the back of an ID card. Instead of the number itself, you can also provide a [PII token created with Stripe.js](https://stripe.com/js/tokens/create_token?type=pii).
        """
        last_name: NotRequired[str]
        """
        The individual's last name.
        """
        last_name_kana: NotRequired[str]
        """
        The Kana variation of the individual's last name (Japan only).
        """
        last_name_kanji: NotRequired[str]
        """
        The Kanji variation of the individual's last name (Japan only).
        """
        maiden_name: NotRequired[str]
        """
        The individual's maiden name.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        phone: NotRequired[str]
        """
        The individual's phone number.
        """
        political_exposure: NotRequired[Literal["existing", "none"]]
        """
        Indicates if the person or any of their representatives, family members, or other closely related persons, declares that they hold or have held an important public job or function, in any jurisdiction.
        """
        registered_address: NotRequired[
            "AccountService.UpdateParamsIndividualRegisteredAddress"
        ]
        """
        The individual's registered address.
        """
        relationship: NotRequired[
            "AccountService.UpdateParamsIndividualRelationship"
        ]
        """
        Describes the person's relationship to the account.
        """
        ssn_last_4: NotRequired[str]
        """
        The last four digits of the individual's Social Security Number (U.S. only).
        """
        verification: NotRequired[
            "AccountService.UpdateParamsIndividualVerification"
        ]
        """
        The individual's verification document information.
        """

    class UpdateParamsIndividualAddress(TypedDict):
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

    class UpdateParamsIndividualAddressKana(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class UpdateParamsIndividualAddressKanji(TypedDict):
        city: NotRequired[str]
        """
        City or ward.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
        """
        Block or building number.
        """
        line2: NotRequired[str]
        """
        Building details.
        """
        postal_code: NotRequired[str]
        """
        Postal code.
        """
        state: NotRequired[str]
        """
        Prefecture.
        """
        town: NotRequired[str]
        """
        Town or cho-me.
        """

    class UpdateParamsIndividualDob(TypedDict):
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

    class UpdateParamsIndividualRegisteredAddress(TypedDict):
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

    class UpdateParamsIndividualRelationship(TypedDict):
        director: NotRequired[bool]
        """
        Whether the person is a director of the account's legal entity. Directors are typically members of the governing board of the company, or responsible for ensuring the company meets its regulatory obligations.
        """
        executive: NotRequired[bool]
        """
        Whether the person has significant responsibility to control, manage, or direct the organization.
        """
        owner: NotRequired[bool]
        """
        Whether the person is an owner of the account's legal entity.
        """
        percent_ownership: NotRequired["Literal['']|float"]
        """
        The percent owned by the person of the account's legal entity.
        """
        title: NotRequired[str]
        """
        The person's title (e.g., CEO, Support Engineer).
        """

    class UpdateParamsIndividualVerification(TypedDict):
        additional_document: NotRequired[
            "AccountService.UpdateParamsIndividualVerificationAdditionalDocument"
        ]
        """
        A document showing address, either a passport, local ID card, or utility bill from a well-known utility company.
        """
        document: NotRequired[
            "AccountService.UpdateParamsIndividualVerificationDocument"
        ]
        """
        An identifying document, either a passport or local ID card.
        """

    class UpdateParamsIndividualVerificationAdditionalDocument(TypedDict):
        back: NotRequired[str]
        """
        The back of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """
        front: NotRequired[str]
        """
        The front of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """

    class UpdateParamsIndividualVerificationDocument(TypedDict):
        back: NotRequired[str]
        """
        The back of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """
        front: NotRequired[str]
        """
        The front of an ID returned by a [file upload](https://stripe.com/docs/api#create_file) with a `purpose` value of `identity_document`. The uploaded file needs to be a color image (smaller than 8,000px by 8,000px), in JPG, PNG, or PDF format, and less than 10 MB in size.
        """

    class UpdateParamsSettings(TypedDict):
        bacs_debit_payments: NotRequired[
            "AccountService.UpdateParamsSettingsBacsDebitPayments"
        ]
        """
        Settings specific to Bacs Direct Debit payments.
        """
        branding: NotRequired["AccountService.UpdateParamsSettingsBranding"]
        """
        Settings used to apply the account's branding to email receipts, invoices, Checkout, and other products.
        """
        card_issuing: NotRequired[
            "AccountService.UpdateParamsSettingsCardIssuing"
        ]
        """
        Settings specific to the account's use of the Card Issuing product.
        """
        card_payments: NotRequired[
            "AccountService.UpdateParamsSettingsCardPayments"
        ]
        """
        Settings specific to card charging on the account.
        """
        invoices: NotRequired["AccountService.UpdateParamsSettingsInvoices"]
        """
        Settings specific to the account's use of Invoices.
        """
        payments: NotRequired["AccountService.UpdateParamsSettingsPayments"]
        """
        Settings that apply across payment methods for charging on the account.
        """
        payouts: NotRequired["AccountService.UpdateParamsSettingsPayouts"]
        """
        Settings specific to the account's payouts.
        """
        treasury: NotRequired["AccountService.UpdateParamsSettingsTreasury"]
        """
        Settings specific to the account's Treasury FinancialAccounts.
        """

    class UpdateParamsSettingsBacsDebitPayments(TypedDict):
        display_name: NotRequired[str]
        """
        The Bacs Direct Debit Display Name for this account. For payments made with Bacs Direct Debit, this name appears on the mandate as the statement descriptor. Mobile banking apps display it as the name of the business. To use custom branding, set the Bacs Direct Debit Display Name during or right after creation. Custom branding incurs an additional monthly fee for the platform. If you don't set the display name before requesting Bacs capability, it's automatically set as "Stripe" and the account is onboarded to Stripe branding, which is free.
        """

    class UpdateParamsSettingsBranding(TypedDict):
        icon: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) An icon for the account. Must be square and at least 128px x 128px.
        """
        logo: NotRequired[str]
        """
        (ID of a [file upload](https://stripe.com/docs/guides/file-upload)) A logo for the account that will be used in Checkout instead of the icon and without the account's name next to it if provided. Must be at least 128px x 128px.
        """
        primary_color: NotRequired[str]
        """
        A CSS hex color value representing the primary branding color for this account.
        """
        secondary_color: NotRequired[str]
        """
        A CSS hex color value representing the secondary branding color for this account.
        """

    class UpdateParamsSettingsCardIssuing(TypedDict):
        tos_acceptance: NotRequired[
            "AccountService.UpdateParamsSettingsCardIssuingTosAcceptance"
        ]
        """
        Details on the account's acceptance of the [Stripe Issuing Terms and Disclosures](https://stripe.com/issuing/connect/tos_acceptance).
        """

    class UpdateParamsSettingsCardIssuingTosAcceptance(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp marking when the account representative accepted the service agreement.
        """
        ip: NotRequired[str]
        """
        The IP address from which the account representative accepted the service agreement.
        """
        user_agent: NotRequired["Literal['']|str"]
        """
        The user agent of the browser from which the account representative accepted the service agreement.
        """

    class UpdateParamsSettingsCardPayments(TypedDict):
        decline_on: NotRequired[
            "AccountService.UpdateParamsSettingsCardPaymentsDeclineOn"
        ]
        """
        Automatically declines certain charge types regardless of whether the card issuer accepted or declined the charge.
        """
        statement_descriptor_prefix: NotRequired[str]
        """
        The default text that appears on credit card statements when a charge is made. This field prefixes any dynamic `statement_descriptor` specified on the charge. `statement_descriptor_prefix` is useful for maximizing descriptor space for the dynamic portion.
        """
        statement_descriptor_prefix_kana: NotRequired["Literal['']|str"]
        """
        The Kana variation of the default text that appears on credit card statements when a charge is made (Japan only). This field prefixes any dynamic `statement_descriptor_suffix_kana` specified on the charge. `statement_descriptor_prefix_kana` is useful for maximizing descriptor space for the dynamic portion.
        """
        statement_descriptor_prefix_kanji: NotRequired["Literal['']|str"]
        """
        The Kanji variation of the default text that appears on credit card statements when a charge is made (Japan only). This field prefixes any dynamic `statement_descriptor_suffix_kanji` specified on the charge. `statement_descriptor_prefix_kanji` is useful for maximizing descriptor space for the dynamic portion.
        """

    class UpdateParamsSettingsCardPaymentsDeclineOn(TypedDict):
        avs_failure: NotRequired[bool]
        """
        Whether Stripe automatically declines charges with an incorrect ZIP or postal code. This setting only applies when a ZIP or postal code is provided and they fail bank verification.
        """
        cvc_failure: NotRequired[bool]
        """
        Whether Stripe automatically declines charges with an incorrect CVC. This setting only applies when a CVC is provided and it fails bank verification.
        """

    class UpdateParamsSettingsInvoices(TypedDict):
        default_account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The list of default Account Tax IDs to automatically include on invoices. Account Tax IDs get added when an invoice is finalized.
        """

    class UpdateParamsSettingsPayments(TypedDict):
        statement_descriptor: NotRequired[str]
        """
        The default text that appears on statements for non-card charges outside of Japan. For card charges, if you don't set a `statement_descriptor_prefix`, this text is also used as the statement descriptor prefix. In that case, if concatenating the statement descriptor suffix causes the combined statement descriptor to exceed 22 characters, we truncate the `statement_descriptor` text to limit the full descriptor to 22 characters. For more information about statement descriptors and their requirements, see the [account settings documentation](https://docs.stripe.com/get-started/account/statement-descriptors).
        """
        statement_descriptor_kana: NotRequired[str]
        """
        The Kana variation of `statement_descriptor` used for charges in Japan. Japanese statement descriptors have [special requirements](https://docs.stripe.com/get-started/account/statement-descriptors#set-japanese-statement-descriptors).
        """
        statement_descriptor_kanji: NotRequired[str]
        """
        The Kanji variation of `statement_descriptor` used for charges in Japan. Japanese statement descriptors have [special requirements](https://docs.stripe.com/get-started/account/statement-descriptors#set-japanese-statement-descriptors).
        """

    class UpdateParamsSettingsPayouts(TypedDict):
        debit_negative_balances: NotRequired[bool]
        """
        A Boolean indicating whether Stripe should try to reclaim negative balances from an attached bank account. For details, see [Understanding Connect Account Balances](https://stripe.com/connect/account-balances).
        """
        schedule: NotRequired[
            "AccountService.UpdateParamsSettingsPayoutsSchedule"
        ]
        """
        Details on when funds from charges are available, and when they are paid out to an external account. For details, see our [Setting Bank and Debit Card Payouts](https://stripe.com/connect/bank-transfers#payout-information) documentation.
        """
        statement_descriptor: NotRequired[str]
        """
        The text that appears on the bank account statement for payouts. If not set, this defaults to the platform's bank descriptor as set in the Dashboard.
        """

    class UpdateParamsSettingsPayoutsSchedule(TypedDict):
        delay_days: NotRequired["Literal['minimum']|int"]
        """
        The number of days charge funds are held before being paid out. May also be set to `minimum`, representing the lowest available value for the account country. Default is `minimum`. The `delay_days` parameter remains at the last configured value if `interval` is `manual`. [Learn more about controlling payout delay days](https://stripe.com/connect/manage-payout-schedule).
        """
        interval: NotRequired[Literal["daily", "manual", "monthly", "weekly"]]
        """
        How frequently available funds are paid out. One of: `daily`, `manual`, `weekly`, or `monthly`. Default is `daily`.
        """
        monthly_anchor: NotRequired[int]
        """
        The day of the month when available funds are paid out, specified as a number between 1--31. Payouts nominally scheduled between the 29th and 31st of the month are instead sent on the last day of a shorter month. Required and applicable only if `interval` is `monthly`.
        """
        weekly_anchor: NotRequired[
            Literal[
                "friday",
                "monday",
                "saturday",
                "sunday",
                "thursday",
                "tuesday",
                "wednesday",
            ]
        ]
        """
        The day of the week when available funds are paid out, specified as `monday`, `tuesday`, etc. (required and applicable only if `interval` is `weekly`.)
        """

    class UpdateParamsSettingsTreasury(TypedDict):
        tos_acceptance: NotRequired[
            "AccountService.UpdateParamsSettingsTreasuryTosAcceptance"
        ]
        """
        Details on the account's acceptance of the Stripe Treasury Services Agreement.
        """

    class UpdateParamsSettingsTreasuryTosAcceptance(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp marking when the account representative accepted the service agreement.
        """
        ip: NotRequired[str]
        """
        The IP address from which the account representative accepted the service agreement.
        """
        user_agent: NotRequired["Literal['']|str"]
        """
        The user agent of the browser from which the account representative accepted the service agreement.
        """

    class UpdateParamsTosAcceptance(TypedDict):
        date: NotRequired[int]
        """
        The Unix timestamp marking when the account representative accepted their service agreement.
        """
        ip: NotRequired[str]
        """
        The IP address from which the account representative accepted their service agreement.
        """
        service_agreement: NotRequired[str]
        """
        The user's service agreement type.
        """
        user_agent: NotRequired[str]
        """
        The user agent of the browser from which the account representative accepted their service agreement.
        """

    def delete(
        self,
        account: str,
        params: "AccountService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        With [Connect](https://stripe.com/connect), you can delete accounts you manage.

        Test-mode accounts can be deleted at any time.

        Live-mode accounts where Stripe is responsible for negative account balances cannot be deleted, which includes Standard accounts. Live-mode accounts where your platform is liable for negative account balances, which includes Custom and Express accounts, can be deleted when all [balances](https://stripe.com/api/balance/balanace_object) are zero.

        If you want to delete your own account, use the [account information tab in your account settings](https://dashboard.stripe.com/settings/account) instead.
        """
        return cast(
            Account,
            self._request(
                "delete",
                "/v1/accounts/{account}".format(account=sanitize_id(account)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        account: str,
        params: "AccountService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        With [Connect](https://stripe.com/connect), you can delete accounts you manage.

        Test-mode accounts can be deleted at any time.

        Live-mode accounts where Stripe is responsible for negative account balances cannot be deleted, which includes Standard accounts. Live-mode accounts where your platform is liable for negative account balances, which includes Custom and Express accounts, can be deleted when all [balances](https://stripe.com/api/balance/balanace_object) are zero.

        If you want to delete your own account, use the [account information tab in your account settings](https://dashboard.stripe.com/settings/account) instead.
        """
        return cast(
            Account,
            await self._request_async(
                "delete",
                "/v1/accounts/{account}".format(account=sanitize_id(account)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        account: str,
        params: "AccountService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        Retrieves the details of an account.
        """
        return cast(
            Account,
            self._request(
                "get",
                "/v1/accounts/{account}".format(account=sanitize_id(account)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        account: str,
        params: "AccountService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        Retrieves the details of an account.
        """
        return cast(
            Account,
            await self._request_async(
                "get",
                "/v1/accounts/{account}".format(account=sanitize_id(account)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        account: str,
        params: "AccountService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        Updates a [connected account](https://stripe.com/connect/accounts) by setting the values of the parameters passed. Any parameters not provided are
        left unchanged.

        For accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection)
        is application, which includes Custom accounts, you can update any information on the account.

        For accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection)
        is stripe, which includes Standard and Express accounts, you can update all information until you create
        an [Account Link or <a href="/api/account_sessions">Account Session](https://stripe.com/api/account_links) to start Connect onboarding,
        after which some properties can no longer be updated.

        To update your own account, use the [Dashboard](https://dashboard.stripe.com/settings/account). Refer to our
        [Connect](https://stripe.com/docs/connect/updating-accounts) documentation to learn more about updating accounts.
        """
        return cast(
            Account,
            self._request(
                "post",
                "/v1/accounts/{account}".format(account=sanitize_id(account)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        account: str,
        params: "AccountService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        Updates a [connected account](https://stripe.com/connect/accounts) by setting the values of the parameters passed. Any parameters not provided are
        left unchanged.

        For accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection)
        is application, which includes Custom accounts, you can update any information on the account.

        For accounts where [controller.requirement_collection](https://stripe.com/api/accounts/object#account_object-controller-requirement_collection)
        is stripe, which includes Standard and Express accounts, you can update all information until you create
        an [Account Link or <a href="/api/account_sessions">Account Session](https://stripe.com/api/account_links) to start Connect onboarding,
        after which some properties can no longer be updated.

        To update your own account, use the [Dashboard](https://dashboard.stripe.com/settings/account). Refer to our
        [Connect](https://stripe.com/docs/connect/updating-accounts) documentation to learn more about updating accounts.
        """
        return cast(
            Account,
            await self._request_async(
                "post",
                "/v1/accounts/{account}".format(account=sanitize_id(account)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve_current(
        self,
        params: "AccountService.RetrieveCurrentParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        Retrieves the details of an account.
        """
        return cast(
            Account,
            self._request(
                "get",
                "/v1/account",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_current_async(
        self,
        params: "AccountService.RetrieveCurrentParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        Retrieves the details of an account.
        """
        return cast(
            Account,
            await self._request_async(
                "get",
                "/v1/account",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "AccountService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Account]:
        """
        Returns a list of accounts connected to your platform via [Connect](https://stripe.com/docs/connect). If you're not a platform, the list is empty.
        """
        return cast(
            ListObject[Account],
            self._request(
                "get",
                "/v1/accounts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "AccountService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Account]:
        """
        Returns a list of accounts connected to your platform via [Connect](https://stripe.com/docs/connect). If you're not a platform, the list is empty.
        """
        return cast(
            ListObject[Account],
            await self._request_async(
                "get",
                "/v1/accounts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "AccountService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        With [Connect](https://stripe.com/docs/connect), you can create Stripe accounts for your users.
        To do this, you'll first need to [register your platform](https://dashboard.stripe.com/account/applications/settings).

        If you've already collected information for your connected accounts, you [can prefill that information](https://stripe.com/docs/connect/best-practices#onboarding) when
        creating the account. Connect Onboarding won't ask for the prefilled information during account onboarding.
        You can prefill any information on the account.
        """
        return cast(
            Account,
            self._request(
                "post",
                "/v1/accounts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "AccountService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Account:
        """
        With [Connect](https://stripe.com/docs/connect), you can create Stripe accounts for your users.
        To do this, you'll first need to [register your platform](https://dashboard.stripe.com/account/applications/settings).

        If you've already collected information for your connected accounts, you [can prefill that information](https://stripe.com/docs/connect/best-practices#onboarding) when
        creating the account. Connect Onboarding won't ask for the prefilled information during account onboarding.
        You can prefill any information on the account.
        """
        return cast(
            Account,
            await self._request_async(
                "post",
                "/v1/accounts",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def reject(
        self,
        account: str,
        params: "AccountService.RejectParams",
        options: RequestOptions = {},
    ) -> Account:
        """
        With [Connect](https://stripe.com/connect), you can reject accounts that you have flagged as suspicious.

        Only accounts where your platform is liable for negative account balances, which includes Custom and Express accounts, can be rejected. Test-mode accounts can be rejected at any time. Live-mode accounts can only be rejected after all balances are zero.
        """
        return cast(
            Account,
            self._request(
                "post",
                "/v1/accounts/{account}/reject".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def reject_async(
        self,
        account: str,
        params: "AccountService.RejectParams",
        options: RequestOptions = {},
    ) -> Account:
        """
        With [Connect](https://stripe.com/connect), you can reject accounts that you have flagged as suspicious.

        Only accounts where your platform is liable for negative account balances, which includes Custom and Express accounts, can be rejected. Test-mode accounts can be rejected at any time. Live-mode accounts can only be rejected after all balances are zero.
        """
        return cast(
            Account,
            await self._request_async(
                "post",
                "/v1/accounts/{account}/reject".format(
                    account=sanitize_id(account),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
