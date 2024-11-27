# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.checkout._session import Session
from stripe.checkout._session_line_item_service import SessionLineItemService
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class SessionService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.line_items = SessionLineItemService(self._requestor)

    class CreateParams(TypedDict):
        after_expiration: NotRequired[
            "SessionService.CreateParamsAfterExpiration"
        ]
        """
        Configure actions after a Checkout Session has expired.
        """
        allow_promotion_codes: NotRequired[bool]
        """
        Enables user redeemable promotion codes.
        """
        automatic_tax: NotRequired["SessionService.CreateParamsAutomaticTax"]
        """
        Settings for automatic tax lookup for this session and resulting payments, invoices, and subscriptions.
        """
        billing_address_collection: NotRequired[Literal["auto", "required"]]
        """
        Specify whether Checkout should collect the customer's billing address. Defaults to `auto`.
        """
        cancel_url: NotRequired[str]
        """
        If set, Checkout displays a back button and customers will be directed to this URL if they decide to cancel payment and return to your website.
        """
        client_reference_id: NotRequired[str]
        """
        A unique string to reference the Checkout Session. This can be a
        customer ID, a cart ID, or similar, and can be used to reconcile the
        session with your internal systems.
        """
        consent_collection: NotRequired[
            "SessionService.CreateParamsConsentCollection"
        ]
        """
        Configure fields for the Checkout Session to gather active consent from customers.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies). Required in `setup` mode when `payment_method_types` is not set.
        """
        custom_fields: NotRequired[
            List["SessionService.CreateParamsCustomField"]
        ]
        """
        Collect additional information from your customer using custom fields. Up to 3 fields are supported.
        """
        custom_text: NotRequired["SessionService.CreateParamsCustomText"]
        """
        Display additional text for your customers using custom text.
        """
        customer: NotRequired[str]
        """
        ID of an existing Customer, if one exists. In `payment` mode, the customer's most recently saved card
        payment method will be used to prefill the email, name, card details, and billing address
        on the Checkout page. In `subscription` mode, the customer's [default payment method](https://stripe.com/docs/api/customers/update#update_customer-invoice_settings-default_payment_method)
        will be used if it's a card, otherwise the most recently saved card will be used. A valid billing address, billing name and billing email are required on the payment method for Checkout to prefill the customer's card details.

        If the Customer already has a valid [email](https://stripe.com/docs/api/customers/object#customer_object-email) set, the email will be prefilled and not editable in Checkout.
        If the Customer does not have a valid `email`, Checkout will set the email entered during the session on the Customer.

        If blank for Checkout Sessions in `subscription` mode or with `customer_creation` set as `always` in `payment` mode, Checkout will create a new Customer object based on information provided during the payment flow.

        You can set [`payment_intent_data.setup_future_usage`](https://stripe.com/docs/api/checkout/sessions/create#create_checkout_session-payment_intent_data-setup_future_usage) to have Checkout automatically attach the payment method to the Customer you pass in for future reuse.
        """
        customer_creation: NotRequired[Literal["always", "if_required"]]
        """
        Configure whether a Checkout Session creates a [Customer](https://stripe.com/docs/api/customers) during Session confirmation.

        When a Customer is not created, you can still retrieve email, address, and other customer data entered in Checkout
        with [customer_details](https://stripe.com/docs/api/checkout/sessions/object#checkout_session_object-customer_details).

        Sessions that don't create Customers instead are grouped by [guest customers](https://stripe.com/docs/payments/checkout/guest-customers)
        in the Dashboard. Promotion codes limited to first time customers will return invalid for these Sessions.

        Can only be set in `payment` and `setup` mode.
        """
        customer_email: NotRequired[str]
        """
        If provided, this value will be used when the Customer object is created.
        If not provided, customers will be asked to enter their email address.
        Use this parameter to prefill customer data if you already have an email
        on file. To access information about the customer once a session is
        complete, use the `customer` field.
        """
        customer_update: NotRequired[
            "SessionService.CreateParamsCustomerUpdate"
        ]
        """
        Controls what fields on Customer can be updated by the Checkout Session. Can only be provided when `customer` is provided.
        """
        discounts: NotRequired[List["SessionService.CreateParamsDiscount"]]
        """
        The coupon or promotion code to apply to this Session. Currently, only up to one may be specified.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expires_at: NotRequired[int]
        """
        The Epoch time in seconds at which the Checkout Session will expire. It can be anywhere from 30 minutes to 24 hours after Checkout Session creation. By default, this value is 24 hours from creation.
        """
        invoice_creation: NotRequired[
            "SessionService.CreateParamsInvoiceCreation"
        ]
        """
        Generate a post-purchase Invoice for one-time payments.
        """
        line_items: NotRequired[List["SessionService.CreateParamsLineItem"]]
        """
        A list of items the customer is purchasing. Use this parameter to pass one-time or recurring [Prices](https://stripe.com/docs/api/prices).

        For `payment` mode, there is a maximum of 100 line items, however it is recommended to consolidate line items if there are more than a few dozen.

        For `subscription` mode, there is a maximum of 20 line items with recurring Prices and 20 line items with one-time Prices. Line items with one-time Prices will be on the initial invoice only.
        """
        locale: NotRequired[
            Literal[
                "auto",
                "bg",
                "cs",
                "da",
                "de",
                "el",
                "en",
                "en-GB",
                "es",
                "es-419",
                "et",
                "fi",
                "fil",
                "fr",
                "fr-CA",
                "hr",
                "hu",
                "id",
                "it",
                "ja",
                "ko",
                "lt",
                "lv",
                "ms",
                "mt",
                "nb",
                "nl",
                "pl",
                "pt",
                "pt-BR",
                "ro",
                "ru",
                "sk",
                "sl",
                "sv",
                "th",
                "tr",
                "vi",
                "zh",
                "zh-HK",
                "zh-TW",
            ]
        ]
        """
        The IETF language tag of the locale Checkout is displayed in. If blank or `auto`, the browser's locale is used.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        mode: NotRequired[Literal["payment", "setup", "subscription"]]
        """
        The mode of the Checkout Session. Pass `subscription` if the Checkout Session includes at least one recurring item.
        """
        payment_intent_data: NotRequired[
            "SessionService.CreateParamsPaymentIntentData"
        ]
        """
        A subset of parameters to be passed to PaymentIntent creation for Checkout Sessions in `payment` mode.
        """
        payment_method_collection: NotRequired[
            Literal["always", "if_required"]
        ]
        """
        Specify whether Checkout should collect a payment method. When set to `if_required`, Checkout will not collect a payment method when the total due for the session is 0.
        This may occur if the Checkout Session includes a free trial or a discount.

        Can only be set in `subscription` mode. Defaults to `always`.

        If you'd like information on how to collect a payment method outside of Checkout, read the guide on configuring [subscriptions with a free trial](https://stripe.com/docs/payments/checkout/free-trials).
        """
        payment_method_configuration: NotRequired[str]
        """
        The ID of the payment method configuration to use with this Checkout session.
        """
        payment_method_data: NotRequired[
            "SessionService.CreateParamsPaymentMethodData"
        ]
        """
        This parameter allows you to set some attributes on the payment method created during a Checkout session.
        """
        payment_method_options: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptions"
        ]
        """
        Payment-method-specific configuration.
        """
        payment_method_types: NotRequired[
            List[
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
        ]
        """
        A list of the types of payment methods (e.g., `card`) this Checkout Session can accept.

        You can omit this attribute to manage your payment methods from the [Stripe Dashboard](https://dashboard.stripe.com/settings/payment_methods).
        See [Dynamic Payment Methods](https://stripe.com/docs/payments/payment-methods/integration-options#using-dynamic-payment-methods) for more details.

        Read more about the supported payment methods and their requirements in our [payment
        method details guide](https://stripe.com/docs/payments/checkout/payment-methods).

        If multiple payment methods are passed, Checkout will dynamically reorder them to
        prioritize the most relevant payment methods based on the customer's location and
        other characteristics.
        """
        phone_number_collection: NotRequired[
            "SessionService.CreateParamsPhoneNumberCollection"
        ]
        """
        Controls phone number collection settings for the session.

        We recommend that you review your privacy policy and check with your legal contacts
        before using this feature. Learn more about [collecting phone numbers with Checkout](https://stripe.com/docs/payments/checkout/phone-numbers).
        """
        redirect_on_completion: NotRequired[
            Literal["always", "if_required", "never"]
        ]
        """
        This parameter applies to `ui_mode: embedded`. Learn more about the [redirect behavior](https://stripe.com/docs/payments/checkout/custom-redirect-behavior) of embedded sessions. Defaults to `always`.
        """
        return_url: NotRequired[str]
        """
        The URL to redirect your customer back to after they authenticate or cancel their payment on the
        payment method's app or site. This parameter is required if ui_mode is `embedded`
        and redirect-based payment methods are enabled on the session.
        """
        saved_payment_method_options: NotRequired[
            "SessionService.CreateParamsSavedPaymentMethodOptions"
        ]
        """
        Controls saved payment method settings for the session. Only available in `payment` and `subscription` mode.
        """
        setup_intent_data: NotRequired[
            "SessionService.CreateParamsSetupIntentData"
        ]
        """
        A subset of parameters to be passed to SetupIntent creation for Checkout Sessions in `setup` mode.
        """
        shipping_address_collection: NotRequired[
            "SessionService.CreateParamsShippingAddressCollection"
        ]
        """
        When set, provides configuration for Checkout to collect a shipping address from a customer.
        """
        shipping_options: NotRequired[
            List["SessionService.CreateParamsShippingOption"]
        ]
        """
        The shipping rate options to apply to this Session. Up to a maximum of 5.
        """
        submit_type: NotRequired[Literal["auto", "book", "donate", "pay"]]
        """
        Describes the type of transaction being performed by Checkout in order to customize
        relevant text on the page, such as the submit button. `submit_type` can only be
        specified on Checkout Sessions in `payment` mode. If blank or `auto`, `pay` is used.
        """
        subscription_data: NotRequired[
            "SessionService.CreateParamsSubscriptionData"
        ]
        """
        A subset of parameters to be passed to subscription creation for Checkout Sessions in `subscription` mode.
        """
        success_url: NotRequired[str]
        """
        The URL to which Stripe should send customers when payment or setup
        is complete.
        This parameter is not allowed if ui_mode is `embedded`. If you'd like to use
        information from the successful Checkout Session on your page, read the
        guide on [customizing your success page](https://stripe.com/docs/payments/checkout/custom-success-page).
        """
        tax_id_collection: NotRequired[
            "SessionService.CreateParamsTaxIdCollection"
        ]
        """
        Controls tax ID collection during checkout.
        """
        ui_mode: NotRequired[Literal["embedded", "hosted"]]
        """
        The UI mode of the Session. Defaults to `hosted`.
        """

    class CreateParamsAfterExpiration(TypedDict):
        recovery: NotRequired[
            "SessionService.CreateParamsAfterExpirationRecovery"
        ]
        """
        Configure a Checkout Session that can be used to recover an expired session.
        """

    class CreateParamsAfterExpirationRecovery(TypedDict):
        allow_promotion_codes: NotRequired[bool]
        """
        Enables user redeemable promotion codes on the recovered Checkout Sessions. Defaults to `false`
        """
        enabled: bool
        """
        If `true`, a recovery URL will be generated to recover this Checkout Session if it
        expires before a successful transaction is completed. It will be attached to the
        Checkout Session object upon expiration.
        """

    class CreateParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Set to true to enable automatic taxes.
        """
        liability: NotRequired[
            "SessionService.CreateParamsAutomaticTaxLiability"
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
            "SessionService.CreateParamsConsentCollectionPaymentMethodReuseAgreement"
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
        dropdown: NotRequired["SessionService.CreateParamsCustomFieldDropdown"]
        """
        Configuration for `type=dropdown` fields.
        """
        key: str
        """
        String of your choice that your integration can use to reconcile this field. Must be unique to this field, alphanumeric, and up to 200 characters.
        """
        label: "SessionService.CreateParamsCustomFieldLabel"
        """
        The label for the field, displayed to the customer.
        """
        numeric: NotRequired["SessionService.CreateParamsCustomFieldNumeric"]
        """
        Configuration for `type=numeric` fields.
        """
        optional: NotRequired[bool]
        """
        Whether the customer is required to complete the field before completing the Checkout Session. Defaults to `false`.
        """
        text: NotRequired["SessionService.CreateParamsCustomFieldText"]
        """
        Configuration for `type=text` fields.
        """
        type: Literal["dropdown", "numeric", "text"]
        """
        The type of the field.
        """

    class CreateParamsCustomFieldDropdown(TypedDict):
        default_value: NotRequired[str]
        """
        The value that will pre-fill the field on the payment page.Must match a `value` in the `options` array.
        """
        options: List["SessionService.CreateParamsCustomFieldDropdownOption"]
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
        default_value: NotRequired[str]
        """
        The value that will pre-fill the field on the payment page.
        """
        maximum_length: NotRequired[int]
        """
        The maximum character length constraint for the customer's input.
        """
        minimum_length: NotRequired[int]
        """
        The minimum character length requirement for the customer's input.
        """

    class CreateParamsCustomFieldText(TypedDict):
        default_value: NotRequired[str]
        """
        The value that will pre-fill the field on the payment page.
        """
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
            "Literal['']|SessionService.CreateParamsCustomTextAfterSubmit"
        ]
        """
        Custom text that should be displayed after the payment confirmation button.
        """
        shipping_address: NotRequired[
            "Literal['']|SessionService.CreateParamsCustomTextShippingAddress"
        ]
        """
        Custom text that should be displayed alongside shipping address collection.
        """
        submit: NotRequired[
            "Literal['']|SessionService.CreateParamsCustomTextSubmit"
        ]
        """
        Custom text that should be displayed alongside the payment confirmation button.
        """
        terms_of_service_acceptance: NotRequired[
            "Literal['']|SessionService.CreateParamsCustomTextTermsOfServiceAcceptance"
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

    class CreateParamsCustomerUpdate(TypedDict):
        address: NotRequired[Literal["auto", "never"]]
        """
        Describes whether Checkout saves the billing address onto `customer.address`.
        To always collect a full billing address, use `billing_address_collection`. Defaults to `never`.
        """
        name: NotRequired[Literal["auto", "never"]]
        """
        Describes whether Checkout saves the name onto `customer.name`. Defaults to `never`.
        """
        shipping: NotRequired[Literal["auto", "never"]]
        """
        Describes whether Checkout saves shipping information onto `customer.shipping`.
        To collect shipping information, use `shipping_address_collection`. Defaults to `never`.
        """

    class CreateParamsDiscount(TypedDict):
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this Session.
        """
        promotion_code: NotRequired[str]
        """
        The ID of a promotion code to apply to this Session.
        """

    class CreateParamsInvoiceCreation(TypedDict):
        enabled: bool
        """
        Set to `true` to enable invoice creation.
        """
        invoice_data: NotRequired[
            "SessionService.CreateParamsInvoiceCreationInvoiceData"
        ]
        """
        Parameters passed when creating invoices for payment-mode Checkout Sessions.
        """

    class CreateParamsInvoiceCreationInvoiceData(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with the invoice.
        """
        custom_fields: NotRequired[
            "Literal['']|List[SessionService.CreateParamsInvoiceCreationInvoiceDataCustomField]"
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
            "SessionService.CreateParamsInvoiceCreationInvoiceDataIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        rendering_options: NotRequired[
            "Literal['']|SessionService.CreateParamsInvoiceCreationInvoiceDataRenderingOptions"
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
            "SessionService.CreateParamsLineItemAdjustableQuantity"
        ]
        """
        When set, provides configuration for this item's quantity to be adjusted by the customer during Checkout.
        """
        dynamic_tax_rates: NotRequired[List[str]]
        """
        The [tax rates](https://stripe.com/docs/api/tax_rates) that will be applied to this line item depending on the customer's billing/shipping address. We currently support the following countries: US, GB, AU, and all countries in the EU.
        """
        price: NotRequired[str]
        """
        The ID of the [Price](https://stripe.com/docs/api/prices) or [Plan](https://stripe.com/docs/api/plans) object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired["SessionService.CreateParamsLineItemPriceData"]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        quantity: NotRequired[int]
        """
        The quantity of the line item being purchased. Quantity should not be defined when `recurring.usage_type=metered`.
        """
        tax_rates: NotRequired[List[str]]
        """
        The [tax rates](https://stripe.com/docs/api/tax_rates) which apply to this line item.
        """

    class CreateParamsLineItemAdjustableQuantity(TypedDict):
        enabled: bool
        """
        Set to true if the quantity can be adjusted to any non-negative integer. By default customers will be able to remove the line item by setting the quantity to 0.
        """
        maximum: NotRequired[int]
        """
        The maximum quantity the customer can purchase for the Checkout Session. By default this value is 99. You can specify a value up to 999999.
        """
        minimum: NotRequired[int]
        """
        The minimum quantity the customer must purchase for the Checkout Session. By default this value is 0.
        """

    class CreateParamsLineItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: NotRequired[str]
        """
        The ID of the product that this price will belong to. One of `product` or `product_data` is required.
        """
        product_data: NotRequired[
            "SessionService.CreateParamsLineItemPriceDataProductData"
        ]
        """
        Data used to generate a new product object inline. One of `product` or `product_data` is required.
        """
        recurring: NotRequired[
            "SessionService.CreateParamsLineItemPriceDataRecurring"
        ]
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
        A non-negative integer in cents (or local equivalent) representing how much to charge. One of `unit_amount` or `unit_amount_decimal` is required.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreateParamsLineItemPriceDataProductData(TypedDict):
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

    class CreateParamsLineItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class CreateParamsPaymentIntentData(TypedDict):
        application_fee_amount: NotRequired[int]
        """
        The amount of the application fee (if any) that will be requested to be applied to the payment and transferred to the application owner's Stripe account. The amount of the application fee collected will be capped at the total payment amount. For more information, see the PaymentIntents [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """
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
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        on_behalf_of: NotRequired[str]
        """
        The Stripe account ID for which these funds are intended. For details,
        see the PaymentIntents [use case for connected
        accounts](https://stripe.com/docs/payments/connected-accounts).
        """
        receipt_email: NotRequired[str]
        """
        Email address that the receipt for the resulting payment will be sent to. If `receipt_email` is specified for a payment in live mode, a receipt will be sent regardless of your [email settings](https://dashboard.stripe.com/account/emails).
        """
        setup_future_usage: NotRequired[Literal["off_session", "on_session"]]
        """
        Indicates that you intend to [make future payments](https://stripe.com/docs/payments/payment-intents#future-usage) with the payment
        method collected by this Checkout Session.

        When setting this to `on_session`, Checkout will show a notice to the
        customer that their payment details will be saved.

        When setting this to `off_session`, Checkout will show a notice to the
        customer that their payment details will be saved and used for future
        payments.

        If a Customer has been provided or Checkout creates a new Customer,
        Checkout will attach the payment method to the Customer.

        If Checkout does not create a Customer, the payment method is not attached
        to a Customer. To reuse the payment method, you can retrieve it from the
        Checkout Session's PaymentIntent.

        When processing card payments, Checkout also uses `setup_future_usage`
        to dynamically optimize your payment flow and comply with regional
        legislation and network rules, such as SCA.
        """
        shipping: NotRequired[
            "SessionService.CreateParamsPaymentIntentDataShipping"
        ]
        """
        Shipping information for this payment.
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
            "SessionService.CreateParamsPaymentIntentDataTransferData"
        ]
        """
        The parameters used to automatically create a Transfer when the payment succeeds.
        For more information, see the PaymentIntents [use case for connected accounts](https://stripe.com/docs/payments/connected-accounts).
        """
        transfer_group: NotRequired[str]
        """
        A string that identifies the resulting payment as part of a group. See the PaymentIntents [use case for connected accounts](https://stripe.com/docs/connect/separate-charges-and-transfers) for details.
        """

    class CreateParamsPaymentIntentDataShipping(TypedDict):
        address: "SessionService.CreateParamsPaymentIntentDataShippingAddress"
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

    class CreateParamsPaymentIntentDataShippingAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: str
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

    class CreateParamsPaymentIntentDataTransferData(TypedDict):
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

    class CreateParamsPaymentMethodData(TypedDict):
        allow_redisplay: NotRequired[
            Literal["always", "limited", "unspecified"]
        ]
        """
        Allow redisplay will be set on the payment method on confirmation and indicates whether this payment method can be shown again to the customer in a checkout flow. Only set this field if you wish to override the allow_redisplay value determined by Checkout.
        """

    class CreateParamsPaymentMethodOptions(TypedDict):
        acss_debit: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsAcssDebit"
        ]
        """
        contains details about the ACSS Debit payment method options.
        """
        affirm: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsAffirm"
        ]
        """
        contains details about the Affirm payment method options.
        """
        afterpay_clearpay: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsAfterpayClearpay"
        ]
        """
        contains details about the Afterpay Clearpay payment method options.
        """
        alipay: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsAlipay"
        ]
        """
        contains details about the Alipay payment method options.
        """
        amazon_pay: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsAmazonPay"
        ]
        """
        contains details about the AmazonPay payment method options.
        """
        au_becs_debit: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsAuBecsDebit"
        ]
        """
        contains details about the AU Becs Debit payment method options.
        """
        bacs_debit: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsBacsDebit"
        ]
        """
        contains details about the Bacs Debit payment method options.
        """
        bancontact: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsBancontact"
        ]
        """
        contains details about the Bancontact payment method options.
        """
        boleto: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsBoleto"
        ]
        """
        contains details about the Boleto payment method options.
        """
        card: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsCard"
        ]
        """
        contains details about the Card payment method options.
        """
        cashapp: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsCashapp"
        ]
        """
        contains details about the Cashapp Pay payment method options.
        """
        customer_balance: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsCustomerBalance"
        ]
        """
        contains details about the Customer Balance payment method options.
        """
        eps: NotRequired["SessionService.CreateParamsPaymentMethodOptionsEps"]
        """
        contains details about the EPS payment method options.
        """
        fpx: NotRequired["SessionService.CreateParamsPaymentMethodOptionsFpx"]
        """
        contains details about the FPX payment method options.
        """
        giropay: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsGiropay"
        ]
        """
        contains details about the Giropay payment method options.
        """
        grabpay: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsGrabpay"
        ]
        """
        contains details about the Grabpay payment method options.
        """
        ideal: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsIdeal"
        ]
        """
        contains details about the Ideal payment method options.
        """
        klarna: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsKlarna"
        ]
        """
        contains details about the Klarna payment method options.
        """
        konbini: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsKonbini"
        ]
        """
        contains details about the Konbini payment method options.
        """
        link: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsLink"
        ]
        """
        contains details about the Link payment method options.
        """
        mobilepay: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsMobilepay"
        ]
        """
        contains details about the Mobilepay payment method options.
        """
        multibanco: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsMultibanco"
        ]
        """
        contains details about the Multibanco payment method options.
        """
        oxxo: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsOxxo"
        ]
        """
        contains details about the OXXO payment method options.
        """
        p24: NotRequired["SessionService.CreateParamsPaymentMethodOptionsP24"]
        """
        contains details about the P24 payment method options.
        """
        paynow: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsPaynow"
        ]
        """
        contains details about the PayNow payment method options.
        """
        paypal: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsPaypal"
        ]
        """
        contains details about the PayPal payment method options.
        """
        pix: NotRequired["SessionService.CreateParamsPaymentMethodOptionsPix"]
        """
        contains details about the Pix payment method options.
        """
        revolut_pay: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsRevolutPay"
        ]
        """
        contains details about the RevolutPay payment method options.
        """
        sepa_debit: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsSepaDebit"
        ]
        """
        contains details about the Sepa Debit payment method options.
        """
        sofort: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsSofort"
        ]
        """
        contains details about the Sofort payment method options.
        """
        swish: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsSwish"
        ]
        """
        contains details about the Swish payment method options.
        """
        us_bank_account: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsUsBankAccount"
        ]
        """
        contains details about the Us Bank Account payment method options.
        """
        wechat_pay: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsWechatPay"
        ]
        """
        contains details about the WeChat Pay payment method options.
        """

    class CreateParamsPaymentMethodOptionsAcssDebit(TypedDict):
        currency: NotRequired[Literal["cad", "usd"]]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies). This is only accepted for Checkout Sessions in `setup` mode.
        """
        mandate_options: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsAcssDebitMandateOptions"
        ]
        """
        Additional fields for Mandate creation
        """
        setup_future_usage: NotRequired[
            Literal["none", "off_session", "on_session"]
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """
        verification_method: NotRequired[
            Literal["automatic", "instant", "microdeposits"]
        ]
        """
        Verification method for the intent
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
        List of Stripe products where this mandate can be selected automatically. Only usable in `setup` mode.
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
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsAfterpayClearpay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsAlipay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsAmazonPay(TypedDict):
        setup_future_usage: NotRequired[Literal["none", "off_session"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsAuBecsDebit(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsBacsDebit(TypedDict):
        setup_future_usage: NotRequired[
            Literal["none", "off_session", "on_session"]
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsBancontact(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsBoleto(TypedDict):
        expires_after_days: NotRequired[int]
        """
        The number of calendar days before a Boleto voucher expires. For example, if you create a Boleto voucher on Monday and you set expires_after_days to 2, the Boleto invoice will expire on Wednesday at 23:59 America/Sao_Paulo time.
        """
        setup_future_usage: NotRequired[
            Literal["none", "off_session", "on_session"]
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsCard(TypedDict):
        installments: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsCardInstallments"
        ]
        """
        Installment options for card payments
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. If not provided, this value defaults to `automatic`. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """
        setup_future_usage: NotRequired[Literal["off_session", "on_session"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """
        statement_descriptor_suffix_kana: NotRequired[str]
        """
        Provides information about a card payment that customers see on their statements. Concatenated with the Kana prefix (shortened Kana descriptor) or Kana statement descriptor that's set on the account to form the complete statement descriptor. Maximum 22 characters. On card statements, the *concatenation* of both prefix and suffix (including separators) will appear truncated to 22 characters.
        """
        statement_descriptor_suffix_kanji: NotRequired[str]
        """
        Provides information about a card payment that customers see on their statements. Concatenated with the Kanji prefix (shortened Kanji descriptor) or Kanji statement descriptor that's set on the account to form the complete statement descriptor. Maximum 17 characters. On card statements, the *concatenation* of both prefix and suffix (including separators) will appear truncated to 17 characters.
        """

    class CreateParamsPaymentMethodOptionsCardInstallments(TypedDict):
        enabled: NotRequired[bool]
        """
        Setting to true enables installments for this Checkout Session.
        Setting to false will prevent any installment plan from applying to a payment.
        """

    class CreateParamsPaymentMethodOptionsCashapp(TypedDict):
        setup_future_usage: NotRequired[
            Literal["none", "off_session", "on_session"]
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsCustomerBalance(TypedDict):
        bank_transfer: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsCustomerBalanceBankTransfer"
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
        """

    class CreateParamsPaymentMethodOptionsCustomerBalanceBankTransfer(
        TypedDict,
    ):
        eu_bank_transfer: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
        ]
        """
        Configuration for eu_bank_transfer funding type.
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
        The list of bank transfer types that this PaymentIntent is allowed to use for funding.
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
        """

    class CreateParamsPaymentMethodOptionsFpx(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsGiropay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsGrabpay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsIdeal(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsKlarna(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsKonbini(TypedDict):
        expires_after_days: NotRequired[int]
        """
        The number of calendar days (between 1 and 60) after which Konbini payment instructions will expire. For example, if a PaymentIntent is confirmed with Konbini and `expires_after_days` set to 2 on Monday JST, the instructions will expire on Wednesday 23:59:59 JST. Defaults to 3 days.
        """
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsLink(TypedDict):
        setup_future_usage: NotRequired[Literal["none", "off_session"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsMobilepay(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsMultibanco(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
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
        """

    class CreateParamsPaymentMethodOptionsP24(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
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

    class CreateParamsPaymentMethodOptionsRevolutPay(TypedDict):
        setup_future_usage: NotRequired[Literal["none", "off_session"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsSepaDebit(TypedDict):
        setup_future_usage: NotRequired[
            Literal["none", "off_session", "on_session"]
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsSofort(TypedDict):
        setup_future_usage: NotRequired[Literal["none"]]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """

    class CreateParamsPaymentMethodOptionsSwish(TypedDict):
        reference: NotRequired[str]
        """
        The order reference that will be displayed to customers in the Swish application. Defaults to the `id` of the Payment Intent.
        """

    class CreateParamsPaymentMethodOptionsUsBankAccount(TypedDict):
        financial_connections: NotRequired[
            "SessionService.CreateParamsPaymentMethodOptionsUsBankAccountFinancialConnections"
        ]
        """
        Additional fields for Financial Connections Session creation
        """
        setup_future_usage: NotRequired[
            Literal["none", "off_session", "on_session"]
        ]
        """
        Indicates that you intend to make future payments with this PaymentIntent's payment method.

        If you provide a Customer with the PaymentIntent, you can use this parameter to [attach the payment method](https://stripe.com/payments/save-during-payment) to the Customer after the PaymentIntent is confirmed and the customer completes any required actions. If you don't provide a Customer, you can still [attach](https://stripe.com/api/payment_methods/attach) the payment method to a Customer after the transaction completes.

        If the payment method is `card_present` and isn't a digital wallet, Stripe creates and attaches a [generated_card](https://stripe.com/api/charges/object#charge_object-payment_method_details-card_present-generated_card) payment method representing the card to the Customer instead.

        When processing card payments, Stripe uses `setup_future_usage` to help you comply with regional legislation and network rules, such as [SCA](https://stripe.com/strong-customer-authentication).
        """
        verification_method: NotRequired[Literal["automatic", "instant"]]
        """
        Verification method for the intent
        """

    class CreateParamsPaymentMethodOptionsUsBankAccountFinancialConnections(
        TypedDict,
    ):
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
        """

    class CreateParamsPhoneNumberCollection(TypedDict):
        enabled: bool
        """
        Set to `true` to enable phone number collection.
        """

    class CreateParamsSavedPaymentMethodOptions(TypedDict):
        allow_redisplay_filters: NotRequired[
            List[Literal["always", "limited", "unspecified"]]
        ]
        """
        Uses the `allow_redisplay` value of each saved payment method to filter the set presented to a returning customer. By default, only saved payment methods with 'allow_redisplay: ‘always' are shown in Checkout.
        """
        payment_method_save: NotRequired[Literal["disabled", "enabled"]]
        """
        Enable customers to choose if they wish to save their payment method for future use. Disabled by default.
        """

    class CreateParamsSetupIntentData(TypedDict):
        description: NotRequired[str]
        """
        An arbitrary string attached to the object. Often useful for displaying to users.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        on_behalf_of: NotRequired[str]
        """
        The Stripe account for which the setup is intended.
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
        shipping_rate_data: NotRequired[
            "SessionService.CreateParamsShippingOptionShippingRateData"
        ]
        """
        Parameters to be passed to Shipping Rate creation for this shipping option.
        """

    class CreateParamsShippingOptionShippingRateData(TypedDict):
        delivery_estimate: NotRequired[
            "SessionService.CreateParamsShippingOptionShippingRateDataDeliveryEstimate"
        ]
        """
        The estimated range for how long shipping will take, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        display_name: str
        """
        The name of the shipping rate, meant to be displayable to the customer. This will appear on CheckoutSessions.
        """
        fixed_amount: NotRequired[
            "SessionService.CreateParamsShippingOptionShippingRateDataFixedAmount"
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

    class CreateParamsShippingOptionShippingRateDataDeliveryEstimate(
        TypedDict
    ):
        maximum: NotRequired[
            "SessionService.CreateParamsShippingOptionShippingRateDataDeliveryEstimateMaximum"
        ]
        """
        The upper bound of the estimated range. If empty, represents no upper bound i.e., infinite.
        """
        minimum: NotRequired[
            "SessionService.CreateParamsShippingOptionShippingRateDataDeliveryEstimateMinimum"
        ]
        """
        The lower bound of the estimated range. If empty, represents no lower bound.
        """

    class CreateParamsShippingOptionShippingRateDataDeliveryEstimateMaximum(
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

    class CreateParamsShippingOptionShippingRateDataDeliveryEstimateMinimum(
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

    class CreateParamsShippingOptionShippingRateDataFixedAmount(TypedDict):
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
                "SessionService.CreateParamsShippingOptionShippingRateDataFixedAmountCurrencyOptions",
            ]
        ]
        """
        Shipping rates defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """

    class CreateParamsShippingOptionShippingRateDataFixedAmountCurrencyOptions(
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

    class CreateParamsSubscriptionData(TypedDict):
        application_fee_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. To use an application fee percent, the request must be made on behalf of another account, using the `Stripe-Account` header or an OAuth key. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        billing_cycle_anchor: NotRequired[int]
        """
        A future timestamp to anchor the subscription's billing cycle for new subscriptions.
        """
        default_tax_rates: NotRequired[List[str]]
        """
        The tax rates that will apply to any subscription item that does not have
        `tax_rates` set. Invoices created will have their `default_tax_rates` populated
        from the subscription.
        """
        description: NotRequired[str]
        """
        The subscription's description, meant to be displayable to the customer.
        Use this field to optionally store an explanation of the subscription
        for rendering in the [customer portal](https://stripe.com/docs/customer-management).
        """
        invoice_settings: NotRequired[
            "SessionService.CreateParamsSubscriptionDataInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        on_behalf_of: NotRequired[str]
        """
        The account on behalf of which to charge, for each of the subscription's invoices.
        """
        proration_behavior: NotRequired[Literal["create_prorations", "none"]]
        """
        Determines how to handle prorations resulting from the `billing_cycle_anchor`. If no value is passed, the default is `create_prorations`.
        """
        transfer_data: NotRequired[
            "SessionService.CreateParamsSubscriptionDataTransferData"
        ]
        """
        If specified, the funds from the subscription's invoices will be transferred to the destination and the ID of the resulting transfers will be found on the resulting charges.
        """
        trial_end: NotRequired[int]
        """
        Unix timestamp representing the end of the trial period the customer
        will get before being charged for the first time. Has to be at least
        48 hours in the future.
        """
        trial_period_days: NotRequired[int]
        """
        Integer representing the number of trial period days before the
        customer is charged for the first time. Has to be at least 1.
        """
        trial_settings: NotRequired[
            "SessionService.CreateParamsSubscriptionDataTrialSettings"
        ]
        """
        Settings related to subscription trials.
        """

    class CreateParamsSubscriptionDataInvoiceSettings(TypedDict):
        issuer: NotRequired[
            "SessionService.CreateParamsSubscriptionDataInvoiceSettingsIssuer"
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

    class CreateParamsSubscriptionDataTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class CreateParamsSubscriptionDataTrialSettings(TypedDict):
        end_behavior: "SessionService.CreateParamsSubscriptionDataTrialSettingsEndBehavior"
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

    class ExpireParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class ListParams(TypedDict):
        created: NotRequired["SessionService.ListParamsCreated|int"]
        """
        Only return Checkout Sessions that were created during the given date interval.
        """
        customer: NotRequired[str]
        """
        Only return the Checkout Sessions for the Customer specified.
        """
        customer_details: NotRequired[
            "SessionService.ListParamsCustomerDetails"
        ]
        """
        Only return the Checkout Sessions for the Customer details specified.
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
        payment_intent: NotRequired[str]
        """
        Only return the Checkout Session for the PaymentIntent specified.
        """
        payment_link: NotRequired[str]
        """
        Only return the Checkout Sessions for the Payment Link specified.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[Literal["complete", "expired", "open"]]
        """
        Only return the Checkout Sessions matching the given status.
        """
        subscription: NotRequired[str]
        """
        Only return the Checkout Session for the subscription specified.
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

    class ListParamsCustomerDetails(TypedDict):
        email: str
        """
        Customer's email address.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    def list(
        self,
        params: "SessionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Session]:
        """
        Returns a list of Checkout Sessions.
        """
        return cast(
            ListObject[Session],
            self._request(
                "get",
                "/v1/checkout/sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "SessionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Session]:
        """
        Returns a list of Checkout Sessions.
        """
        return cast(
            ListObject[Session],
            await self._request_async(
                "get",
                "/v1/checkout/sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "SessionService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Session:
        """
        Creates a Session object.
        """
        return cast(
            Session,
            self._request(
                "post",
                "/v1/checkout/sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "SessionService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> Session:
        """
        Creates a Session object.
        """
        return cast(
            Session,
            await self._request_async(
                "post",
                "/v1/checkout/sessions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        session: str,
        params: "SessionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Session:
        """
        Retrieves a Session object.
        """
        return cast(
            Session,
            self._request(
                "get",
                "/v1/checkout/sessions/{session}".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        session: str,
        params: "SessionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Session:
        """
        Retrieves a Session object.
        """
        return cast(
            Session,
            await self._request_async(
                "get",
                "/v1/checkout/sessions/{session}".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        session: str,
        params: "SessionService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Session:
        """
        Updates a Session object.
        """
        return cast(
            Session,
            self._request(
                "post",
                "/v1/checkout/sessions/{session}".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        session: str,
        params: "SessionService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Session:
        """
        Updates a Session object.
        """
        return cast(
            Session,
            await self._request_async(
                "post",
                "/v1/checkout/sessions/{session}".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def expire(
        self,
        session: str,
        params: "SessionService.ExpireParams" = {},
        options: RequestOptions = {},
    ) -> Session:
        """
        A Session can be expired when it is in one of these statuses: open

        After it expires, a customer can't complete a Session and customers loading the Session see a message saying the Session is expired.
        """
        return cast(
            Session,
            self._request(
                "post",
                "/v1/checkout/sessions/{session}/expire".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def expire_async(
        self,
        session: str,
        params: "SessionService.ExpireParams" = {},
        options: RequestOptions = {},
    ) -> Session:
        """
        A Session can be expired when it is in one of these statuses: open

        After it expires, a customer can't complete a Session and customers loading the Session see a message saying the Session is expired.
        """
        return cast(
            Session,
            await self._request_async(
                "post",
                "/v1/checkout/sessions/{session}/expire".format(
                    session=sanitize_id(session),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
