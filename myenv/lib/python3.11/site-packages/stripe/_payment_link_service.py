# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._payment_link import PaymentLink
from stripe._payment_link_line_item_service import PaymentLinkLineItemService
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class PaymentLinkService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.line_items = PaymentLinkLineItemService(self._requestor)

    class CreateParams(TypedDict):
        after_completion: NotRequired[
            "PaymentLinkService.CreateParamsAfterCompletion"
        ]
        """
        Behavior after the purchase is complete.
        """
        allow_promotion_codes: NotRequired[bool]
        """
        Enables user redeemable promotion codes.
        """
        application_fee_amount: NotRequired[int]
        """
        The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. Can only be applied when there are no line items with recurring prices.
        """
        application_fee_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. There must be at least 1 line item with a recurring price to use this field.
        """
        automatic_tax: NotRequired[
            "PaymentLinkService.CreateParamsAutomaticTax"
        ]
        """
        Configuration for automatic tax collection.
        """
        billing_address_collection: NotRequired[Literal["auto", "required"]]
        """
        Configuration for collecting the customer's billing address. Defaults to `auto`.
        """
        consent_collection: NotRequired[
            "PaymentLinkService.CreateParamsConsentCollection"
        ]
        """
        Configure fields to gather active consent from customers.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies) and supported by each line item's price.
        """
        custom_fields: NotRequired[
            List["PaymentLinkService.CreateParamsCustomField"]
        ]
        """
        Collect additional information from your customer using custom fields. Up to 3 fields are supported.
        """
        custom_text: NotRequired["PaymentLinkService.CreateParamsCustomText"]
        """
        Display additional text for your customers using custom text.
        """
        customer_creation: NotRequired[Literal["always", "if_required"]]
        """
        Configures whether [checkout sessions](https://stripe.com/docs/api/checkout/sessions) created by this payment link create a [Customer](https://stripe.com/docs/api/customers).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        inactive_message: NotRequired[str]
        """
        The custom message to be displayed to a customer when a payment link is no longer active.
        """
        invoice_creation: NotRequired[
            "PaymentLinkService.CreateParamsInvoiceCreation"
        ]
        """
        Generate a post-purchase Invoice for one-time payments.
        """
        line_items: List["PaymentLinkService.CreateParamsLineItem"]
        """
        The line items representing what is being sold. Each line item represents an item being sold. Up to 20 line items are supported.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`. Metadata associated with this Payment Link will automatically be copied to [checkout sessions](https://stripe.com/docs/api/checkout/sessions) created by this payment link.
        """
        on_behalf_of: NotRequired[str]
        """
        The account on behalf of which to charge.
        """
        payment_intent_data: NotRequired[
            "PaymentLinkService.CreateParamsPaymentIntentData"
        ]
        """
        A subset of parameters to be passed to PaymentIntent creation for Checkout Sessions in `payment` mode.
        """
        payment_method_collection: NotRequired[
            Literal["always", "if_required"]
        ]
        """
        Specify whether Checkout should collect a payment method. When set to `if_required`, Checkout will not collect a payment method when the total due for the session is 0.This may occur if the Checkout Session includes a free trial or a discount.

        Can only be set in `subscription` mode. Defaults to `always`.

        If you'd like information on how to collect a payment method outside of Checkout, read the guide on [configuring subscriptions with a free trial](https://stripe.com/docs/payments/checkout/free-trials).
        """
        payment_method_types: NotRequired[
            List[
                Literal[
                    "affirm",
                    "afterpay_clearpay",
                    "alipay",
                    "au_becs_debit",
                    "bacs_debit",
                    "bancontact",
                    "blik",
                    "boleto",
                    "card",
                    "cashapp",
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
                    "sepa_debit",
                    "sofort",
                    "swish",
                    "twint",
                    "us_bank_account",
                    "wechat_pay",
                    "zip",
                ]
            ]
        ]
        """
        The list of payment method types that customers can use. If no value is passed, Stripe will dynamically show relevant payment methods from your [payment method settings](https://dashboard.stripe.com/settings/payment_methods) (20+ payment methods [supported](https://stripe.com/docs/payments/payment-methods/integration-options#payment-method-product-support)).
        """
        phone_number_collection: NotRequired[
            "PaymentLinkService.CreateParamsPhoneNumberCollection"
        ]
        """
        Controls phone number collection settings during checkout.

        We recommend that you review your privacy policy and check with your legal contacts.
        """
        restrictions: NotRequired[
            "PaymentLinkService.CreateParamsRestrictions"
        ]
        """
        Settings that restrict the usage of a payment link.
        """
        shipping_address_collection: NotRequired[
            "PaymentLinkService.CreateParamsShippingAddressCollection"
        ]
        """
        Configuration for collecting the customer's shipping address.
        """
        shipping_options: NotRequired[
            List["PaymentLinkService.CreateParamsShippingOption"]
        ]
        """
        The shipping rate options to apply to [checkout sessions](https://stripe.com/docs/api/checkout/sessions) created by this payment link.
        """
        submit_type: NotRequired[Literal["auto", "book", "donate", "pay"]]
        """
        Describes the type of transaction being performed in order to customize relevant text on the page, such as the submit button. Changing this value will also affect the hostname in the [url](https://stripe.com/docs/api/payment_links/payment_links/object#url) property (example: `donate.stripe.com`).
        """
        subscription_data: NotRequired[
            "PaymentLinkService.CreateParamsSubscriptionData"
        ]
        """
        When creating a subscription, the specified configuration data will be used. There must be at least one line item with a recurring price to use `subscription_data`.
        """
        tax_id_collection: NotRequired[
            "PaymentLinkService.CreateParamsTaxIdCollection"
        ]
        """
        Controls tax ID collection during checkout.
        """
        transfer_data: NotRequired[
            "PaymentLinkService.CreateParamsTransferData"
        ]
        """
        The account (if any) the payments will be attributed to for tax reporting, and where funds from each payment will be transferred to.
        """

    class CreateParamsAfterCompletion(TypedDict):
        hosted_confirmation: NotRequired[
            "PaymentLinkService.CreateParamsAfterCompletionHostedConfirmation"
        ]
        """
        Configuration when `type=hosted_confirmation`.
        """
        redirect: NotRequired[
            "PaymentLinkService.CreateParamsAfterCompletionRedirect"
        ]
        """
        Configuration when `type=redirect`.
        """
        type: Literal["hosted_confirmation", "redirect"]
        """
        The specified behavior after the purchase is complete. Either `redirect` or `hosted_confirmation`.
        """

    class CreateParamsAfterCompletionHostedConfirmation(TypedDict):
        custom_message: NotRequired[str]
        """
        A custom message to display to the customer after the purchase is complete.
        """

    class CreateParamsAfterCompletionRedirect(TypedDict):
        url: str
        """
        The URL the customer will be redirected to after the purchase is complete. You can embed `{CHECKOUT_SESSION_ID}` into the URL to have the `id` of the completed [checkout session](https://stripe.com/docs/api/checkout/sessions/object#checkout_session_object-id) included.
        """

    class CreateParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        If `true`, tax will be calculated automatically using the customer's location.
        """
        liability: NotRequired[
            "PaymentLinkService.CreateParamsAutomaticTaxLiability"
        ]
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

    class CreateParamsConsentCollection(TypedDict):
        payment_method_reuse_agreement: NotRequired[
            "PaymentLinkService.CreateParamsConsentCollectionPaymentMethodReuseAgreement"
        ]
        """
        Determines the display of payment method reuse agreement text in the UI. If set to `hidden`, it will hide legal text related to the reuse of a payment method.
        """
        promotions: NotRequired[Literal["auto", "none"]]
        """
        If set to `auto`, enables the collection of customer consent for promotional communications. The Checkout
        Session will determine whether to display an option to opt into promotional communication
        from the merchant depending on the customer's locale. Only available to US merchants.
        """
        terms_of_service: NotRequired[Literal["none", "required"]]
        """
        If set to `required`, it requires customers to check a terms of service checkbox before being able to pay.
        There must be a valid terms of service URL set in your [Dashboard settings](https://dashboard.stripe.com/settings/public).
        """

    class CreateParamsConsentCollectionPaymentMethodReuseAgreement(TypedDict):
        position: Literal["auto", "hidden"]
        """
        Determines the position and visibility of the payment method reuse agreement in the UI. When set to `auto`, Stripe's
        defaults will be used. When set to `hidden`, the payment method reuse agreement text will always be hidden in the UI.
        """

    class CreateParamsCustomField(TypedDict):
        dropdown: NotRequired[
            "PaymentLinkService.CreateParamsCustomFieldDropdown"
        ]
        """
        Configuration for `type=dropdown` fields.
        """
        key: str
        """
        String of your choice that your integration can use to reconcile this field. Must be unique to this field, alphanumeric, and up to 200 characters.
        """
        label: "PaymentLinkService.CreateParamsCustomFieldLabel"
        """
        The label for the field, displayed to the customer.
        """
        numeric: NotRequired[
            "PaymentLinkService.CreateParamsCustomFieldNumeric"
        ]
        """
        Configuration for `type=numeric` fields.
        """
        optional: NotRequired[bool]
        """
        Whether the customer is required to complete the field before completing the Checkout Session. Defaults to `false`.
        """
        text: NotRequired["PaymentLinkService.CreateParamsCustomFieldText"]
        """
        Configuration for `type=text` fields.
        """
        type: Literal["dropdown", "numeric", "text"]
        """
        The type of the field.
        """

    class CreateParamsCustomFieldDropdown(TypedDict):
        options: List[
            "PaymentLinkService.CreateParamsCustomFieldDropdownOption"
        ]
        """
        The options available for the customer to select. Up to 200 options allowed.
        """

    class CreateParamsCustomFieldDropdownOption(TypedDict):
        label: str
        """
        The label for the option, displayed to the customer. Up to 100 characters.
        """
        value: str
        """
        The value for this option, not displayed to the customer, used by your integration to reconcile the option selected by the customer. Must be unique to this option, alphanumeric, and up to 100 characters.
        """

    class CreateParamsCustomFieldLabel(TypedDict):
        custom: str
        """
        Custom text for the label, displayed to the customer. Up to 50 characters.
        """
        type: Literal["custom"]
        """
        The type of the label.
        """

    class CreateParamsCustomFieldNumeric(TypedDict):
        maximum_length: NotRequired[int]
        """
        The maximum character length constraint for the customer's input.
        """
        minimum_length: NotRequired[int]
        """
        The minimum character length requirement for the customer's input.
        """

    class CreateParamsCustomFieldText(TypedDict):
        maximum_length: NotRequired[int]
        """
        The maximum character length constraint for the customer's input.
        """
        minimum_length: NotRequired[int]
        """
        The minimum character length requirement for the customer's input.
        """

    class CreateParamsCustomText(TypedDict):
        after_submit: NotRequired[
            "Literal['']|PaymentLinkService.CreateParamsCustomTextAfterSubmit"
        ]
        """
        Custom text that should be displayed after the payment confirmation button.
        """
        shipping_address: NotRequired[
            "Literal['']|PaymentLinkService.CreateParamsCustomTextShippingAddress"
        ]
        """
        Custom text that should be displayed alongside shipping address collection.
        """
        submit: NotRequired[
            "Literal['']|PaymentLinkService.CreateParamsCustomTextSubmit"
        ]
        """
        Custom text that should be displayed alongside the payment confirmation button.
        """
        terms_of_service_acceptance: NotRequired[
            "Literal['']|PaymentLinkService.CreateParamsCustomTextTermsOfServiceAcceptance"
        ]
        """
        Custom text that should be displayed in place of the default terms of service agreement text.
        """

    class CreateParamsCustomTextAfterSubmit(TypedDict):
        message: str
        """
        Text may be up to 1200 characters in length.
        """

    class CreateParamsCustomTextShippingAddress(TypedDict):
        message: str
        """
        Text may be up to 1200 characters in length.
        """

    class CreateParamsCustomTextSubmit(TypedDict):
        message: str
        """
        Text may be up to 1200 characters in length.
        """

    class CreateParamsCustomTextTermsOfServiceAcceptance(TypedDict):
        message: str
        """
        Text may be up to 1200 characters in length.
        """

    class CreateParamsInvoiceCreation(TypedDict):
        enabled: bool
        """
        Whether the feature is enabled
        """
        invoice_data: NotRequired[
            "PaymentLinkService.CreateParamsInvoiceCreationInvoiceData"
        ]
        """
        Invoice PDF configuration.
        """

    class CreateParamsInvoiceCreationInvoiceData(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with the invoice.
        """
        custom_fields: NotRequired[
            "Literal['']|List[PaymentLinkService.CreateParamsInvoiceCreationInvoiceDataCustomField]"
        ]
        """
        Default custom fields to be displayed on invoices for this customer.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        footer: NotRequired[str]
        """
        Default footer to be displayed on invoices for this customer.
        """
        issuer: NotRequired[
            "PaymentLinkService.CreateParamsInvoiceCreationInvoiceDataIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        rendering_options: NotRequired[
            "Literal['']|PaymentLinkService.CreateParamsInvoiceCreationInvoiceDataRenderingOptions"
        ]
        """
        Default options for invoice PDF rendering for this customer.
        """

    class CreateParamsInvoiceCreationInvoiceDataCustomField(TypedDict):
        name: str
        """
        The name of the custom field. This may be up to 40 characters.
        """
        value: str
        """
        The value of the custom field. This may be up to 140 characters.
        """

    class CreateParamsInvoiceCreationInvoiceDataIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsInvoiceCreationInvoiceDataRenderingOptions(TypedDict):
        amount_tax_display: NotRequired[
            "Literal['']|Literal['exclude_tax', 'include_inclusive_tax']"
        ]
        """
        How line-item prices and amounts will be displayed with respect to tax on invoice PDFs. One of `exclude_tax` or `include_inclusive_tax`. `include_inclusive_tax` will include inclusive tax (and exclude exclusive tax) in invoice PDF amounts. `exclude_tax` will exclude all tax (inclusive and exclusive alike) from invoice PDF amounts.
        """

    class CreateParamsLineItem(TypedDict):
        adjustable_quantity: NotRequired[
            "PaymentLinkService.CreateParamsLineItemAdjustableQuantity"
        ]
        """
        When set, provides configuration for this item's quantity to be adjusted by the customer during checkout.
        """
        price: str
        """
        The ID of the [Price](https://stripe.com/docs/api/prices) or [Plan](https://stripe.com/docs/api/plans) object.
        """
        quantity: int
        """
        The quantity of the line item being purchased.
        """

    class CreateParamsLineItemAdjustableQuantity(TypedDict):
        enabled: bool
        """
        Set to true if the quantity can be adjusted to any non-negative Integer.
        """
        maximum: NotRequired[int]
        """
        The maximum quantity the customer can purchase. By default this value is 99. You can specify a value up to 999.
        """
        minimum: NotRequired[int]
        """
        The minimum quantity the customer can purchase. By default this value is 0. If there is only one item in the cart then that item's quantity cannot go down to 0.
        """

    class CreateParamsPaymentIntentData(TypedDict):
        capture_method: NotRequired[
            Literal["automatic", "automatic_async", "manual"]
        ]
        """
        Controls when the funds will be captured from the customer's account.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that will declaratively set metadata on [Payment Intents](https://stripe.com/docs/api/payment_intents) generated from this payment link. Unlike object-level metadata, this field is declarative. Updates will clear prior values.
        """
        setup_future_usage: NotRequired[Literal["off_session", "on_session"]]
        """
        Indicates that you intend to [make future payments](https://stripe.com/docs/payments/payment-intents#future-usage) with the payment method collected by this Checkout Session.

        When setting this to `on_session`, Checkout will show a notice to the customer that their payment details will be saved.

        When setting this to `off_session`, Checkout will show a notice to the customer that their payment details will be saved and used for future payments.

        If a Customer has been provided or Checkout creates a new Customer,Checkout will attach the payment method to the Customer.

        If Checkout does not create a Customer, the payment method is not attached to a Customer. To reuse the payment method, you can retrieve it from the Checkout Session's PaymentIntent.

        When processing card payments, Checkout also uses `setup_future_usage` to dynamically optimize your payment flow and comply with regional legislation and network rules, such as SCA.
        """
        statement_descriptor: NotRequired[str]
        """
        Text that appears on the customer's statement as the [statement descriptor](https://docs.stripe.com/get-started/account/statement-descriptors) for a non-card charge. This value overrides the account's default statement descriptor. Setting this value for a card charge returns an error. For card charges, set the [statement_descriptor_suffix](https://docs.stripe.com/get-started/account/statement-descriptors#dynamic) instead.
        """
        statement_descriptor_suffix: NotRequired[str]
        """
        Provides information about a card charge. Concatenated to the account's [statement descriptor prefix](https://docs.corp.stripe.com/get-started/account/statement-descriptors#static) to form the complete statement descriptor that appears on the customer's statement.
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies the resulting payment as part of a group. See the PaymentIntents [use case for connected accounts](https://stripe.com/docs/connect/separate-charges-and-transfers) for details.
        """

    class CreateParamsPhoneNumberCollection(TypedDict):
        enabled: bool
        """
        Set to `true` to enable phone number collection.
        """

    class CreateParamsRestrictions(TypedDict):
        completed_sessions: (
            "PaymentLinkService.CreateParamsRestrictionsCompletedSessions"
        )
        """
        Configuration for the `completed_sessions` restriction type.
        """

    class CreateParamsRestrictionsCompletedSessions(TypedDict):
        limit: int
        """
        The maximum number of checkout sessions that can be completed for the `completed_sessions` restriction to be met.
        """

    class CreateParamsShippingAddressCollection(TypedDict):
        allowed_countries: List[
            Literal[
                "AC",
                "AD",
                "AE",
                "AF",
                "AG",
                "AI",
                "AL",
                "AM",
                "AO",
                "AQ",
                "AR",
                "AT",
                "AU",
                "AW",
                "AX",
                "AZ",
                "BA",
                "BB",
                "BD",
                "BE",
                "BF",
                "BG",
                "BH",
                "BI",
                "BJ",
                "BL",
                "BM",
                "BN",
                "BO",
                "BQ",
                "BR",
                "BS",
                "BT",
                "BV",
                "BW",
                "BY",
                "BZ",
                "CA",
                "CD",
                "CF",
                "CG",
                "CH",
                "CI",
                "CK",
                "CL",
                "CM",
                "CN",
                "CO",
                "CR",
                "CV",
                "CW",
                "CY",
                "CZ",
                "DE",
                "DJ",
                "DK",
                "DM",
                "DO",
                "DZ",
                "EC",
                "EE",
                "EG",
                "EH",
                "ER",
                "ES",
                "ET",
                "FI",
                "FJ",
                "FK",
                "FO",
                "FR",
                "GA",
                "GB",
                "GD",
                "GE",
                "GF",
                "GG",
                "GH",
                "GI",
                "GL",
                "GM",
                "GN",
                "GP",
                "GQ",
                "GR",
                "GS",
                "GT",
                "GU",
                "GW",
                "GY",
                "HK",
                "HN",
                "HR",
                "HT",
                "HU",
                "ID",
                "IE",
                "IL",
                "IM",
                "IN",
                "IO",
                "IQ",
                "IS",
                "IT",
                "JE",
                "JM",
                "JO",
                "JP",
                "KE",
                "KG",
                "KH",
                "KI",
                "KM",
                "KN",
                "KR",
                "KW",
                "KY",
                "KZ",
                "LA",
                "LB",
                "LC",
                "LI",
                "LK",
                "LR",
                "LS",
                "LT",
                "LU",
                "LV",
                "LY",
                "MA",
                "MC",
                "MD",
                "ME",
                "MF",
                "MG",
                "MK",
                "ML",
                "MM",
                "MN",
                "MO",
                "MQ",
                "MR",
                "MS",
                "MT",
                "MU",
                "MV",
                "MW",
                "MX",
                "MY",
                "MZ",
                "NA",
                "NC",
                "NE",
                "NG",
                "NI",
                "NL",
                "NO",
                "NP",
                "NR",
                "NU",
                "NZ",
                "OM",
                "PA",
                "PE",
                "PF",
                "PG",
                "PH",
                "PK",
                "PL",
                "PM",
                "PN",
                "PR",
                "PS",
                "PT",
                "PY",
                "QA",
                "RE",
                "RO",
                "RS",
                "RU",
                "RW",
                "SA",
                "SB",
                "SC",
                "SE",
                "SG",
                "SH",
                "SI",
                "SJ",
                "SK",
                "SL",
                "SM",
                "SN",
                "SO",
                "SR",
                "SS",
                "ST",
                "SV",
                "SX",
                "SZ",
                "TA",
                "TC",
                "TD",
                "TF",
                "TG",
                "TH",
                "TJ",
                "TK",
                "TL",
                "TM",
                "TN",
                "TO",
                "TR",
                "TT",
                "TV",
                "TW",
                "TZ",
                "UA",
                "UG",
                "US",
                "UY",
                "UZ",
                "VA",
                "VC",
                "VE",
                "VG",
                "VN",
                "VU",
                "WF",
                "WS",
                "XK",
                "YE",
                "YT",
                "ZA",
                "ZM",
                "ZW",
                "ZZ",
            ]
        ]
        """
        An array of two-letter ISO country codes representing which countries Checkout should provide as options for
        shipping locations. Unsupported country codes: `AS, CX, CC, CU, HM, IR, KP, MH, FM, NF, MP, PW, SD, SY, UM, VI`.
        """

    class CreateParamsShippingOption(TypedDict):
        shipping_rate: NotRequired[str]
        """
        The ID of the Shipping Rate to use for this shipping option.
        """

    class CreateParamsSubscriptionData(TypedDict):
        description: NotRequired[str]
        """
        The subscription's description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        invoice_settings: NotRequired[
            "PaymentLinkService.CreateParamsSubscriptionDataInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that will declaratively set metadata on [Subscriptions](https://stripe.com/docs/api/subscriptions) generated from this payment link. Unlike object-level metadata, this field is declarative. Updates will clear prior values.
        """
        trial_period_days: NotRequired[int]
        """
        Integer representing the number of trial period days before the customer is charged for the first time. Has to be at least 1.
        """
        trial_settings: NotRequired[
            "PaymentLinkService.CreateParamsSubscriptionDataTrialSettings"
        ]
        """
        Settings related to subscription trials.
        """

    class CreateParamsSubscriptionDataInvoiceSettings(TypedDict):
        issuer: NotRequired[
            "PaymentLinkService.CreateParamsSubscriptionDataInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class CreateParamsSubscriptionDataInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsSubscriptionDataTrialSettings(TypedDict):
        end_behavior: "PaymentLinkService.CreateParamsSubscriptionDataTrialSettingsEndBehavior"
        """
        Defines how the subscription should behave when the user's free trial ends.
        """

    class CreateParamsSubscriptionDataTrialSettingsEndBehavior(TypedDict):
        missing_payment_method: Literal["cancel", "create_invoice", "pause"]
        """
        Indicates how the subscription should change when the trial ends if the user did not provide a payment method.
        """

    class CreateParamsTaxIdCollection(TypedDict):
        enabled: bool
        """
        Enable tax ID collection during checkout. Defaults to `false`.
        """

    class CreateParamsTransferData(TypedDict):
        amount: NotRequired[int]
        """
        The amount that will be transferred automatically when a charge succeeds.
        """
        destination: str
        """
        If specified, successful charges will be attributed to the destination
        account for tax reporting, and the funds from charges will be transferred
        to the destination account. The ID of the resulting transfer will be
        returned on the successful charge's `transfer` field.
        """

    class ListParams(TypedDict):
        active: NotRequired[bool]
        """
        Only return payment links that are active or inactive (e.g., pass `false` to list all inactive payment links).
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

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        active: NotRequired[bool]
        """
        Whether the payment link's `url` is active. If `false`, customers visiting the URL will be shown a page saying that the link has been deactivated.
        """
        after_completion: NotRequired[
            "PaymentLinkService.UpdateParamsAfterCompletion"
        ]
        """
        Behavior after the purchase is complete.
        """
        allow_promotion_codes: NotRequired[bool]
        """
        Enables user redeemable promotion codes.
        """
        automatic_tax: NotRequired[
            "PaymentLinkService.UpdateParamsAutomaticTax"
        ]
        """
        Configuration for automatic tax collection.
        """
        billing_address_collection: NotRequired[Literal["auto", "required"]]
        """
        Configuration for collecting the customer's billing address. Defaults to `auto`.
        """
        custom_fields: NotRequired[
            "Literal['']|List[PaymentLinkService.UpdateParamsCustomField]"
        ]
        """
        Collect additional information from your customer using custom fields. Up to 3 fields are supported.
        """
        custom_text: NotRequired["PaymentLinkService.UpdateParamsCustomText"]
        """
        Display additional text for your customers using custom text.
        """
        customer_creation: NotRequired[Literal["always", "if_required"]]
        """
        Configures whether [checkout sessions](https://stripe.com/docs/api/checkout/sessions) created by this payment link create a [Customer](https://stripe.com/docs/api/customers).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        inactive_message: NotRequired["Literal['']|str"]
        """
        The custom message to be displayed to a customer when a payment link is no longer active.
        """
        invoice_creation: NotRequired[
            "PaymentLinkService.UpdateParamsInvoiceCreation"
        ]
        """
        Generate a post-purchase Invoice for one-time payments.
        """
        line_items: NotRequired[
            List["PaymentLinkService.UpdateParamsLineItem"]
        ]
        """
        The line items representing what is being sold. Each line item represents an item being sold. Up to 20 line items are supported.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`. Metadata associated with this Payment Link will automatically be copied to [checkout sessions](https://stripe.com/docs/api/checkout/sessions) created by this payment link.
        """
        payment_intent_data: NotRequired[
            "PaymentLinkService.UpdateParamsPaymentIntentData"
        ]
        """
        A subset of parameters to be passed to PaymentIntent creation for Checkout Sessions in `payment` mode.
        """
        payment_method_collection: NotRequired[
            Literal["always", "if_required"]
        ]
        """
        Specify whether Checkout should collect a payment method. When set to `if_required`, Checkout will not collect a payment method when the total due for the session is 0.This may occur if the Checkout Session includes a free trial or a discount.

        Can only be set in `subscription` mode. Defaults to `always`.

        If you'd like information on how to collect a payment method outside of Checkout, read the guide on [configuring subscriptions with a free trial](https://stripe.com/docs/payments/checkout/free-trials).
        """
        payment_method_types: NotRequired[
            "Literal['']|List[Literal['affirm', 'afterpay_clearpay', 'alipay', 'au_becs_debit', 'bacs_debit', 'bancontact', 'blik', 'boleto', 'card', 'cashapp', 'eps', 'fpx', 'giropay', 'grabpay', 'ideal', 'klarna', 'konbini', 'link', 'mobilepay', 'multibanco', 'oxxo', 'p24', 'paynow', 'paypal', 'pix', 'promptpay', 'sepa_debit', 'sofort', 'swish', 'twint', 'us_bank_account', 'wechat_pay', 'zip']]"
        ]
        """
        The list of payment method types that customers can use. Pass an empty string to enable dynamic payment methods that use your [payment method settings](https://dashboard.stripe.com/settings/payment_methods).
        """
        restrictions: NotRequired[
            "Literal['']|PaymentLinkService.UpdateParamsRestrictions"
        ]
        """
        Settings that restrict the usage of a payment link.
        """
        shipping_address_collection: NotRequired[
            "Literal['']|PaymentLinkService.UpdateParamsShippingAddressCollection"
        ]
        """
        Configuration for collecting the customer's shipping address.
        """
        subscription_data: NotRequired[
            "PaymentLinkService.UpdateParamsSubscriptionData"
        ]
        """
        When creating a subscription, the specified configuration data will be used. There must be at least one line item with a recurring price to use `subscription_data`.
        """
        tax_id_collection: NotRequired[
            "PaymentLinkService.UpdateParamsTaxIdCollection"
        ]
        """
        Controls tax ID collection during checkout.
        """

    class UpdateParamsAfterCompletion(TypedDict):
        hosted_confirmation: NotRequired[
            "PaymentLinkService.UpdateParamsAfterCompletionHostedConfirmation"
        ]
        """
        Configuration when `type=hosted_confirmation`.
        """
        redirect: NotRequired[
            "PaymentLinkService.UpdateParamsAfterCompletionRedirect"
        ]
        """
        Configuration when `type=redirect`.
        """
        type: Literal["hosted_confirmation", "redirect"]
        """
        The specified behavior after the purchase is complete. Either `redirect` or `hosted_confirmation`.
        """

    class UpdateParamsAfterCompletionHostedConfirmation(TypedDict):
        custom_message: NotRequired[str]
        """
        A custom message to display to the customer after the purchase is complete.
        """

    class UpdateParamsAfterCompletionRedirect(TypedDict):
        url: str
        """
        The URL the customer will be redirected to after the purchase is complete. You can embed `{CHECKOUT_SESSION_ID}` into the URL to have the `id` of the completed [checkout session](https://stripe.com/docs/api/checkout/sessions/object#checkout_session_object-id) included.
        """

    class UpdateParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        If `true`, tax will be calculated automatically using the customer's location.
        """
        liability: NotRequired[
            "PaymentLinkService.UpdateParamsAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class UpdateParamsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpdateParamsCustomField(TypedDict):
        dropdown: NotRequired[
            "PaymentLinkService.UpdateParamsCustomFieldDropdown"
        ]
        """
        Configuration for `type=dropdown` fields.
        """
        key: str
        """
        String of your choice that your integration can use to reconcile this field. Must be unique to this field, alphanumeric, and up to 200 characters.
        """
        label: "PaymentLinkService.UpdateParamsCustomFieldLabel"
        """
        The label for the field, displayed to the customer.
        """
        numeric: NotRequired[
            "PaymentLinkService.UpdateParamsCustomFieldNumeric"
        ]
        """
        Configuration for `type=numeric` fields.
        """
        optional: NotRequired[bool]
        """
        Whether the customer is required to complete the field before completing the Checkout Session. Defaults to `false`.
        """
        text: NotRequired["PaymentLinkService.UpdateParamsCustomFieldText"]
        """
        Configuration for `type=text` fields.
        """
        type: Literal["dropdown", "numeric", "text"]
        """
        The type of the field.
        """

    class UpdateParamsCustomFieldDropdown(TypedDict):
        options: List[
            "PaymentLinkService.UpdateParamsCustomFieldDropdownOption"
        ]
        """
        The options available for the customer to select. Up to 200 options allowed.
        """

    class UpdateParamsCustomFieldDropdownOption(TypedDict):
        label: str
        """
        The label for the option, displayed to the customer. Up to 100 characters.
        """
        value: str
        """
        The value for this option, not displayed to the customer, used by your integration to reconcile the option selected by the customer. Must be unique to this option, alphanumeric, and up to 100 characters.
        """

    class UpdateParamsCustomFieldLabel(TypedDict):
        custom: str
        """
        Custom text for the label, displayed to the customer. Up to 50 characters.
        """
        type: Literal["custom"]
        """
        The type of the label.
        """

    class UpdateParamsCustomFieldNumeric(TypedDict):
        maximum_length: NotRequired[int]
        """
        The maximum character length constraint for the customer's input.
        """
        minimum_length: NotRequired[int]
        """
        The minimum character length requirement for the customer's input.
        """

    class UpdateParamsCustomFieldText(TypedDict):
        maximum_length: NotRequired[int]
        """
        The maximum character length constraint for the customer's input.
        """
        minimum_length: NotRequired[int]
        """
        The minimum character length requirement for the customer's input.
        """

    class UpdateParamsCustomText(TypedDict):
        after_submit: NotRequired[
            "Literal['']|PaymentLinkService.UpdateParamsCustomTextAfterSubmit"
        ]
        """
        Custom text that should be displayed after the payment confirmation button.
        """
        shipping_address: NotRequired[
            "Literal['']|PaymentLinkService.UpdateParamsCustomTextShippingAddress"
        ]
        """
        Custom text that should be displayed alongside shipping address collection.
        """
        submit: NotRequired[
            "Literal['']|PaymentLinkService.UpdateParamsCustomTextSubmit"
        ]
        """
        Custom text that should be displayed alongside the payment confirmation button.
        """
        terms_of_service_acceptance: NotRequired[
            "Literal['']|PaymentLinkService.UpdateParamsCustomTextTermsOfServiceAcceptance"
        ]
        """
        Custom text that should be displayed in place of the default terms of service agreement text.
        """

    class UpdateParamsCustomTextAfterSubmit(TypedDict):
        message: str
        """
        Text may be up to 1200 characters in length.
        """

    class UpdateParamsCustomTextShippingAddress(TypedDict):
        message: str
        """
        Text may be up to 1200 characters in length.
        """

    class UpdateParamsCustomTextSubmit(TypedDict):
        message: str
        """
        Text may be up to 1200 characters in length.
        """

    class UpdateParamsCustomTextTermsOfServiceAcceptance(TypedDict):
        message: str
        """
        Text may be up to 1200 characters in length.
        """

    class UpdateParamsInvoiceCreation(TypedDict):
        enabled: bool
        """
        Whether the feature is enabled
        """
        invoice_data: NotRequired[
            "PaymentLinkService.UpdateParamsInvoiceCreationInvoiceData"
        ]
        """
        Invoice PDF configuration.
        """

    class UpdateParamsInvoiceCreationInvoiceData(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with the invoice.
        """
        custom_fields: NotRequired[
            "Literal['']|List[PaymentLinkService.UpdateParamsInvoiceCreationInvoiceDataCustomField]"
        ]
        """
        Default custom fields to be displayed on invoices for this customer.
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        footer: NotRequired[str]
        """
        Default footer to be displayed on invoices for this customer.
        """
        issuer: NotRequired[
            "PaymentLinkService.UpdateParamsInvoiceCreationInvoiceDataIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        rendering_options: NotRequired[
            "Literal['']|PaymentLinkService.UpdateParamsInvoiceCreationInvoiceDataRenderingOptions"
        ]
        """
        Default options for invoice PDF rendering for this customer.
        """

    class UpdateParamsInvoiceCreationInvoiceDataCustomField(TypedDict):
        name: str
        """
        The name of the custom field. This may be up to 40 characters.
        """
        value: str
        """
        The value of the custom field. This may be up to 140 characters.
        """

    class UpdateParamsInvoiceCreationInvoiceDataIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpdateParamsInvoiceCreationInvoiceDataRenderingOptions(TypedDict):
        amount_tax_display: NotRequired[
            "Literal['']|Literal['exclude_tax', 'include_inclusive_tax']"
        ]
        """
        How line-item prices and amounts will be displayed with respect to tax on invoice PDFs. One of `exclude_tax` or `include_inclusive_tax`. `include_inclusive_tax` will include inclusive tax (and exclude exclusive tax) in invoice PDF amounts. `exclude_tax` will exclude all tax (inclusive and exclusive alike) from invoice PDF amounts.
        """

    class UpdateParamsLineItem(TypedDict):
        adjustable_quantity: NotRequired[
            "PaymentLinkService.UpdateParamsLineItemAdjustableQuantity"
        ]
        """
        When set, provides configuration for this item's quantity to be adjusted by the customer during checkout.
        """
        id: str
        """
        The ID of an existing line item on the payment link.
        """
        quantity: NotRequired[int]
        """
        The quantity of the line item being purchased.
        """

    class UpdateParamsLineItemAdjustableQuantity(TypedDict):
        enabled: bool
        """
        Set to true if the quantity can be adjusted to any non-negative Integer.
        """
        maximum: NotRequired[int]
        """
        The maximum quantity the customer can purchase. By default this value is 99. You can specify a value up to 999.
        """
        minimum: NotRequired[int]
        """
        The minimum quantity the customer can purchase. By default this value is 0. If there is only one item in the cart then that item's quantity cannot go down to 0.
        """

    class UpdateParamsPaymentIntentData(TypedDict):
        description: NotRequired["Literal['']|str"]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that will declaratively set metadata on [Payment Intents](https://stripe.com/docs/api/payment_intents) generated from this payment link. Unlike object-level metadata, this field is declarative. Updates will clear prior values.
        """
        statement_descriptor: NotRequired["Literal['']|str"]
        """
        Text that appears on the customer's statement as the [statement descriptor](https://docs.stripe.com/get-started/account/statement-descriptors) for a non-card charge. This value overrides the account's default statement descriptor. Setting this value for a card charge returns an error. For card charges, set the [statement_descriptor_suffix](https://docs.stripe.com/get-started/account/statement-descriptors#dynamic) instead.
        """
        statement_descriptor_suffix: NotRequired["Literal['']|str"]
        """
        Provides information about a card charge. Concatenated to the account's [statement descriptor prefix](https://docs.corp.stripe.com/get-started/account/statement-descriptors#static) to form the complete statement descriptor that appears on the customer's statement.
        """
        transfer_group: NotRequired["Literal['']|str"]
        """
        A string that identifies the resulting payment as part of a group. See the PaymentIntents [use case for connected accounts](https://stripe.com/docs/connect/separate-charges-and-transfers) for details.
        """

    class UpdateParamsRestrictions(TypedDict):
        completed_sessions: (
            "PaymentLinkService.UpdateParamsRestrictionsCompletedSessions"
        )
        """
        Configuration for the `completed_sessions` restriction type.
        """

    class UpdateParamsRestrictionsCompletedSessions(TypedDict):
        limit: int
        """
        The maximum number of checkout sessions that can be completed for the `completed_sessions` restriction to be met.
        """

    class UpdateParamsShippingAddressCollection(TypedDict):
        allowed_countries: List[
            Literal[
                "AC",
                "AD",
                "AE",
                "AF",
                "AG",
                "AI",
                "AL",
                "AM",
                "AO",
                "AQ",
                "AR",
                "AT",
                "AU",
                "AW",
                "AX",
                "AZ",
                "BA",
                "BB",
                "BD",
                "BE",
                "BF",
                "BG",
                "BH",
                "BI",
                "BJ",
                "BL",
                "BM",
                "BN",
                "BO",
                "BQ",
                "BR",
                "BS",
                "BT",
                "BV",
                "BW",
                "BY",
                "BZ",
                "CA",
                "CD",
                "CF",
                "CG",
                "CH",
                "CI",
                "CK",
                "CL",
                "CM",
                "CN",
                "CO",
                "CR",
                "CV",
                "CW",
                "CY",
                "CZ",
                "DE",
                "DJ",
                "DK",
                "DM",
                "DO",
                "DZ",
                "EC",
                "EE",
                "EG",
                "EH",
                "ER",
                "ES",
                "ET",
                "FI",
                "FJ",
                "FK",
                "FO",
                "FR",
                "GA",
                "GB",
                "GD",
                "GE",
                "GF",
                "GG",
                "GH",
                "GI",
                "GL",
                "GM",
                "GN",
                "GP",
                "GQ",
                "GR",
                "GS",
                "GT",
                "GU",
                "GW",
                "GY",
                "HK",
                "HN",
                "HR",
                "HT",
                "HU",
                "ID",
                "IE",
                "IL",
                "IM",
                "IN",
                "IO",
                "IQ",
                "IS",
                "IT",
                "JE",
                "JM",
                "JO",
                "JP",
                "KE",
                "KG",
                "KH",
                "KI",
                "KM",
                "KN",
                "KR",
                "KW",
                "KY",
                "KZ",
                "LA",
                "LB",
                "LC",
                "LI",
                "LK",
                "LR",
                "LS",
                "LT",
                "LU",
                "LV",
                "LY",
                "MA",
                "MC",
                "MD",
                "ME",
                "MF",
                "MG",
                "MK",
                "ML",
                "MM",
                "MN",
                "MO",
                "MQ",
                "MR",
                "MS",
                "MT",
                "MU",
                "MV",
                "MW",
                "MX",
                "MY",
                "MZ",
                "NA",
                "NC",
                "NE",
                "NG",
                "NI",
                "NL",
                "NO",
                "NP",
                "NR",
                "NU",
                "NZ",
                "OM",
                "PA",
                "PE",
                "PF",
                "PG",
                "PH",
                "PK",
                "PL",
                "PM",
                "PN",
                "PR",
                "PS",
                "PT",
                "PY",
                "QA",
                "RE",
                "RO",
                "RS",
                "RU",
                "RW",
                "SA",
                "SB",
                "SC",
                "SE",
                "SG",
                "SH",
                "SI",
                "SJ",
                "SK",
                "SL",
                "SM",
                "SN",
                "SO",
                "SR",
                "SS",
                "ST",
                "SV",
                "SX",
                "SZ",
                "TA",
                "TC",
                "TD",
                "TF",
                "TG",
                "TH",
                "TJ",
                "TK",
                "TL",
                "TM",
                "TN",
                "TO",
                "TR",
                "TT",
                "TV",
                "TW",
                "TZ",
                "UA",
                "UG",
                "US",
                "UY",
                "UZ",
                "VA",
                "VC",
                "VE",
                "VG",
                "VN",
                "VU",
                "WF",
                "WS",
                "XK",
                "YE",
                "YT",
                "ZA",
                "ZM",
                "ZW",
                "ZZ",
            ]
        ]
        """
        An array of two-letter ISO country codes representing which countries Checkout should provide as options for
        shipping locations. Unsupported country codes: `AS, CX, CC, CU, HM, IR, KP, MH, FM, NF, MP, PW, SD, SY, UM, VI`.
        """

    class UpdateParamsSubscriptionData(TypedDict):
        invoice_settings: NotRequired[
            "PaymentLinkService.UpdateParamsSubscriptionDataInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that will declaratively set metadata on [Subscriptions](https://stripe.com/docs/api/subscriptions) generated from this payment link. Unlike object-level metadata, this field is declarative. Updates will clear prior values.
        """
        trial_settings: NotRequired[
            "Literal['']|PaymentLinkService.UpdateParamsSubscriptionDataTrialSettings"
        ]
        """
        Settings related to subscription trials.
        """

    class UpdateParamsSubscriptionDataInvoiceSettings(TypedDict):
        issuer: NotRequired[
            "PaymentLinkService.UpdateParamsSubscriptionDataInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class UpdateParamsSubscriptionDataInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpdateParamsSubscriptionDataTrialSettings(TypedDict):
        end_behavior: "PaymentLinkService.UpdateParamsSubscriptionDataTrialSettingsEndBehavior"
        """
        Defines how the subscription should behave when the user's free trial ends.
        """

    class UpdateParamsSubscriptionDataTrialSettingsEndBehavior(TypedDict):
        missing_payment_method: Literal["cancel", "create_invoice", "pause"]
        """
        Indicates how the subscription should change when the trial ends if the user did not provide a payment method.
        """

    class UpdateParamsTaxIdCollection(TypedDict):
        enabled: bool
        """
        Enable tax ID collection during checkout. Defaults to `false`.
        """

    def list(
        self,
        params: "PaymentLinkService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentLink]:
        """
        Returns a list of your payment links.
        """
        return cast(
            ListObject[PaymentLink],
            self._request(
                "get",
                "/v1/payment_links",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "PaymentLinkService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PaymentLink]:
        """
        Returns a list of your payment links.
        """
        return cast(
            ListObject[PaymentLink],
            await self._request_async(
                "get",
                "/v1/payment_links",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "PaymentLinkService.CreateParams",
        options: RequestOptions = {},
    ) -> PaymentLink:
        """
        Creates a payment link.
        """
        return cast(
            PaymentLink,
            self._request(
                "post",
                "/v1/payment_links",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "PaymentLinkService.CreateParams",
        options: RequestOptions = {},
    ) -> PaymentLink:
        """
        Creates a payment link.
        """
        return cast(
            PaymentLink,
            await self._request_async(
                "post",
                "/v1/payment_links",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        payment_link: str,
        params: "PaymentLinkService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentLink:
        """
        Retrieve a payment link.
        """
        return cast(
            PaymentLink,
            self._request(
                "get",
                "/v1/payment_links/{payment_link}".format(
                    payment_link=sanitize_id(payment_link),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        payment_link: str,
        params: "PaymentLinkService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PaymentLink:
        """
        Retrieve a payment link.
        """
        return cast(
            PaymentLink,
            await self._request_async(
                "get",
                "/v1/payment_links/{payment_link}".format(
                    payment_link=sanitize_id(payment_link),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        payment_link: str,
        params: "PaymentLinkService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentLink:
        """
        Updates a payment link.
        """
        return cast(
            PaymentLink,
            self._request(
                "post",
                "/v1/payment_links/{payment_link}".format(
                    payment_link=sanitize_id(payment_link),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        payment_link: str,
        params: "PaymentLinkService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PaymentLink:
        """
        Updates a payment link.
        """
        return cast(
            PaymentLink,
            await self._request_async(
                "post",
                "/v1/payment_links/{payment_link}".format(
                    payment_link=sanitize_id(payment_link),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
