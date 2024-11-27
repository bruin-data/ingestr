# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._discount import Discount
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._search_result_object import SearchResultObject
from stripe._stripe_service import StripeService
from stripe._subscription import Subscription
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class SubscriptionService(StripeService):
    class CancelParams(TypedDict):
        cancellation_details: NotRequired[
            "SubscriptionService.CancelParamsCancellationDetails"
        ]
        """
        Details about why this subscription was cancelled
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_now: NotRequired[bool]
        """
        Will generate a final invoice that invoices for any un-invoiced metered usage and new/pending proration invoice items. Defaults to `true`.
        """
        prorate: NotRequired[bool]
        """
        Will generate a proration invoice item that credits remaining unused time until the subscription period end. Defaults to `false`.
        """

    class CancelParamsCancellationDetails(TypedDict):
        comment: NotRequired["Literal['']|str"]
        """
        Additional comments about why the user canceled the subscription, if the subscription was canceled explicitly by the user.
        """
        feedback: NotRequired[
            "Literal['']|Literal['customer_service', 'low_quality', 'missing_features', 'other', 'switched_service', 'too_complex', 'too_expensive', 'unused']"
        ]
        """
        The customer submitted reason for why they canceled, if the subscription was canceled explicitly by the user.
        """

    class CreateParams(TypedDict):
        add_invoice_items: NotRequired[
            List["SubscriptionService.CreateParamsAddInvoiceItem"]
        ]
        """
        A list of prices and quantities that will generate invoice items appended to the next invoice for this subscription. You may pass up to 20 items.
        """
        application_fee_percent: NotRequired["Literal['']|float"]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. The request must be made by a platform account on a connected account in order to set an application fee percentage. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        automatic_tax: NotRequired[
            "SubscriptionService.CreateParamsAutomaticTax"
        ]
        """
        Automatic tax settings for this subscription. We recommend you only include this parameter when the existing value is being changed.
        """
        backdate_start_date: NotRequired[int]
        """
        For new subscriptions, a past timestamp to backdate the subscription's start date to. If set, the first invoice will contain a proration for the timespan between the start date and the current time. Can be combined with trials and the billing cycle anchor.
        """
        billing_cycle_anchor: NotRequired[int]
        """
        A future timestamp in UTC format to anchor the subscription's [billing cycle](https://stripe.com/docs/subscriptions/billing-cycle). The anchor is the reference point that aligns future billing cycle dates. It sets the day of week for `week` intervals, the day of month for `month` and `year` intervals, and the month of year for `year` intervals.
        """
        billing_cycle_anchor_config: NotRequired[
            "SubscriptionService.CreateParamsBillingCycleAnchorConfig"
        ]
        """
        Mutually exclusive with billing_cycle_anchor and only valid with monthly and yearly price intervals. When provided, the billing_cycle_anchor is set to the next occurence of the day_of_month at the hour, minute, and second UTC.
        """
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionService.CreateParamsBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. Pass an empty string to remove previously-defined thresholds.
        """
        cancel_at: NotRequired[int]
        """
        A timestamp at which the subscription should cancel. If set to a date before the current period ends, this will cause a proration if prorations have been enabled using `proration_behavior`. If set during a future period, this will always cause a proration for that period.
        """
        cancel_at_period_end: NotRequired[bool]
        """
        Indicate whether this subscription should cancel at the end of the current period (`current_period_end`). Defaults to `false`.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay this subscription at the end of the cycle using the default source attached to the customer. When sending an invoice, Stripe will email your customer an invoice with payment instructions and mark the subscription as `active`. Defaults to `charge_automatically`.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this subscription. A coupon applied to a subscription will only affect invoices created for that particular subscription. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        currency: NotRequired[str]
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        customer: str
        """
        The identifier of the customer to subscribe.
        """
        days_until_due: NotRequired[int]
        """
        Number of days a customer has to pay invoices generated by this subscription. Valid only for subscriptions where `collection_method` is set to `send_invoice`.
        """
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the subscription. It must belong to the customer associated with the subscription. This takes precedence over `default_source`. If neither are set, invoices will use the customer's [invoice_settings.default_payment_method](https://stripe.com/docs/api/customers/object#customer_object-invoice_settings-default_payment_method) or [default_source](https://stripe.com/docs/api/customers/object#customer_object-default_source).
        """
        default_source: NotRequired[str]
        """
        ID of the default payment source for the subscription. It must belong to the customer associated with the subscription and be in a chargeable state. If `default_payment_method` is also set, `default_payment_method` will take precedence. If neither are set, invoices will use the customer's [invoice_settings.default_payment_method](https://stripe.com/docs/api/customers/object#customer_object-invoice_settings-default_payment_method) or [default_source](https://stripe.com/docs/api/customers/object#customer_object-default_source).
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates that will apply to any subscription item that does not have `tax_rates` set. Invoices created will have their `default_tax_rates` populated from the subscription.
        """
        description: NotRequired[str]
        """
        The subscription's description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        discounts: NotRequired[
            "Literal['']|List[SubscriptionService.CreateParamsDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription. If not specified or empty, inherits the discount from the subscription's customer.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_settings: NotRequired[
            "SubscriptionService.CreateParamsInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        items: NotRequired[List["SubscriptionService.CreateParamsItem"]]
        """
        A list of up to 20 subscription items, each with an attached price.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        off_session: NotRequired[bool]
        """
        Indicates if a customer is on or off-session while an invoice payment is attempted. Defaults to `false` (on-session).
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account on behalf of which to charge, for each of the subscription's invoices.
        """
        payment_behavior: NotRequired[
            Literal[
                "allow_incomplete",
                "default_incomplete",
                "error_if_incomplete",
                "pending_if_incomplete",
            ]
        ]
        """
        Only applies to subscriptions with `collection_method=charge_automatically`.

        Use `allow_incomplete` to create Subscriptions with `status=incomplete` if the first invoice can't be paid. Creating Subscriptions with this status allows you to manage scenarios where additional customer actions are needed to pay a subscription's invoice. For example, SCA regulation may require 3DS authentication to complete payment. See the [SCA Migration Guide](https://stripe.com/docs/billing/migration/strong-customer-authentication) for Billing to learn more. This is the default behavior.

        Use `default_incomplete` to create Subscriptions with `status=incomplete` when the first invoice requires payment, otherwise start as active. Subscriptions transition to `status=active` when successfully confirming the PaymentIntent on the first invoice. This allows simpler management of scenarios where additional customer actions are needed to pay a subscription's invoice, such as failed payments, [SCA regulation](https://stripe.com/docs/billing/migration/strong-customer-authentication), or collecting a mandate for a bank debit payment method. If the PaymentIntent is not confirmed within 23 hours Subscriptions transition to `status=incomplete_expired`, which is a terminal state.

        Use `error_if_incomplete` if you want Stripe to return an HTTP 402 status code if a subscription's first invoice can't be paid. For example, if a payment method requires 3DS authentication due to SCA regulation and further customer action is needed, this parameter doesn't create a Subscription and returns an error instead. This was the default behavior for API versions prior to 2019-03-14. See the [changelog](https://stripe.com/docs/upgrades#2019-03-14) to learn more.

        `pending_if_incomplete` is only used with updates and cannot be passed when creating a Subscription.

        Subscriptions with `collection_method=send_invoice` are automatically activated regardless of the first Invoice status.
        """
        payment_settings: NotRequired[
            "SubscriptionService.CreateParamsPaymentSettings"
        ]
        """
        Payment settings to pass to invoices created by the subscription.
        """
        pending_invoice_item_interval: NotRequired[
            "Literal['']|SubscriptionService.CreateParamsPendingInvoiceItemInterval"
        ]
        """
        Specifies an interval for how often to bill for any pending invoice items. It is analogous to calling [Create an invoice](https://stripe.com/docs/api#create_invoice) for the given subscription at the specified interval.
        """
        promotion_code: NotRequired[str]
        """
        The promotion code to apply to this subscription. A promotion code applied to a subscription will only affect invoices created for that particular subscription. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) resulting from the `billing_cycle_anchor`. If no value is passed, the default is `create_prorations`.
        """
        transfer_data: NotRequired[
            "SubscriptionService.CreateParamsTransferData"
        ]
        """
        If specified, the funds from the subscription's invoices will be transferred to the destination and the ID of the resulting transfers will be found on the resulting charges.
        """
        trial_end: NotRequired["Literal['now']|int"]
        """
        Unix timestamp representing the end of the trial period the customer will get before being charged for the first time. If set, trial_end will override the default trial period of the plan the customer is being subscribed to. The special value `now` can be provided to end the customer's trial immediately. Can be at most two years from `billing_cycle_anchor`. See [Using trial periods on subscriptions](https://stripe.com/docs/billing/subscriptions/trials) to learn more.
        """
        trial_from_plan: NotRequired[bool]
        """
        Indicates if a plan's `trial_period_days` should be applied to the subscription. Setting `trial_end` per subscription is preferred, and this defaults to `false`. Setting this flag to `true` together with `trial_end` is not allowed. See [Using trial periods on subscriptions](https://stripe.com/docs/billing/subscriptions/trials) to learn more.
        """
        trial_period_days: NotRequired[int]
        """
        Integer representing the number of trial period days before the customer is charged for the first time. This will always overwrite any trials that might apply via a subscribed plan. See [Using trial periods on subscriptions](https://stripe.com/docs/billing/subscriptions/trials) to learn more.
        """
        trial_settings: NotRequired[
            "SubscriptionService.CreateParamsTrialSettings"
        ]
        """
        Settings related to subscription trials.
        """

    class CreateParamsAddInvoiceItem(TypedDict):
        discounts: NotRequired[
            List["SubscriptionService.CreateParamsAddInvoiceItemDiscount"]
        ]
        """
        The coupons to redeem into discounts for the item.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "SubscriptionService.CreateParamsAddInvoiceItemPriceData"
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

    class CreateParamsAddInvoiceItemDiscount(TypedDict):
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

    class CreateParamsAddInvoiceItemPriceData(TypedDict):
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

    class CreateParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
        """
        liability: NotRequired[
            "SubscriptionService.CreateParamsAutomaticTaxLiability"
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

    class CreateParamsBillingCycleAnchorConfig(TypedDict):
        day_of_month: int
        """
        The day of the month the billing_cycle_anchor should be. Ranges from 1 to 31.
        """
        hour: NotRequired[int]
        """
        The hour of the day the billing_cycle_anchor should be. Ranges from 0 to 23.
        """
        minute: NotRequired[int]
        """
        The minute of the hour the billing_cycle_anchor should be. Ranges from 0 to 59.
        """
        month: NotRequired[int]
        """
        The month to start full cycle billing periods. Ranges from 1 to 12.
        """
        second: NotRequired[int]
        """
        The second of the minute the billing_cycle_anchor should be. Ranges from 0 to 59.
        """

    class CreateParamsBillingThresholds(TypedDict):
        amount_gte: NotRequired[int]
        """
        Monetary threshold that triggers the subscription to advance to a new billing period
        """
        reset_billing_cycle_anchor: NotRequired[bool]
        """
        Indicates if the `billing_cycle_anchor` should be reset when a threshold is reached. If true, `billing_cycle_anchor` will be updated to the date/time the threshold was last reached; otherwise, the value will remain unchanged.
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

    class CreateParamsInvoiceSettings(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with the subscription. Will be set on invoices generated by the subscription.
        """
        issuer: NotRequired[
            "SubscriptionService.CreateParamsInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class CreateParamsInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionService.CreateParamsItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[SubscriptionService.CreateParamsItemDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        plan: NotRequired[str]
        """
        Plan ID for this item, as a string.
        """
        price: NotRequired[str]
        """
        The ID of the price object.
        """
        price_data: NotRequired[
            "SubscriptionService.CreateParamsItemPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline.
        """
        quantity: NotRequired[int]
        """
        Quantity for this item.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class CreateParamsItemBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class CreateParamsItemDiscount(TypedDict):
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

    class CreateParamsItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "SubscriptionService.CreateParamsItemPriceDataRecurring"
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

    class CreateParamsItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class CreateParamsPaymentSettings(TypedDict):
        payment_method_options: NotRequired[
            "SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptions"
        ]
        """
        Payment-method-specific configuration to provide to invoices created by the subscription.
        """
        payment_method_types: NotRequired[
            "Literal['']|List[Literal['ach_credit_transfer', 'ach_debit', 'acss_debit', 'amazon_pay', 'au_becs_debit', 'bacs_debit', 'bancontact', 'boleto', 'card', 'cashapp', 'customer_balance', 'eps', 'fpx', 'giropay', 'grabpay', 'ideal', 'konbini', 'link', 'multibanco', 'p24', 'paynow', 'paypal', 'promptpay', 'revolut_pay', 'sepa_credit_transfer', 'sepa_debit', 'sofort', 'swish', 'us_bank_account', 'wechat_pay']]"
        ]
        """
        The list of payment method types (e.g. card) to provide to the invoice's PaymentIntent. If not set, Stripe attempts to automatically determine the types to use by looking at the invoice's default payment method, the subscription's default payment method, the customer's default payment method, and your [invoice template settings](https://dashboard.stripe.com/settings/billing/invoice).
        """
        save_default_payment_method: NotRequired[
            Literal["off", "on_subscription"]
        ]
        """
        Configure whether Stripe updates `subscription.default_payment_method` when payment succeeds. Defaults to `off` if unspecified.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptions(TypedDict):
        acss_debit: NotRequired[
            "Literal['']|SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsAcssDebit"
        ]
        """
        This sub-hash contains details about the Canadian pre-authorized debit payment method options to pass to the invoice's PaymentIntent.
        """
        bancontact: NotRequired[
            "Literal['']|SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsBancontact"
        ]
        """
        This sub-hash contains details about the Bancontact payment method options to pass to the invoice's PaymentIntent.
        """
        card: NotRequired[
            "Literal['']|SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsCard"
        ]
        """
        This sub-hash contains details about the Card payment method options to pass to the invoice's PaymentIntent.
        """
        customer_balance: NotRequired[
            "Literal['']|SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalance"
        ]
        """
        This sub-hash contains details about the Bank transfer payment method options to pass to the invoice's PaymentIntent.
        """
        konbini: NotRequired[
            "Literal['']|SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsKonbini"
        ]
        """
        This sub-hash contains details about the Konbini payment method options to pass to the invoice's PaymentIntent.
        """
        sepa_debit: NotRequired[
            "Literal['']|SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsSepaDebit"
        ]
        """
        This sub-hash contains details about the SEPA Direct Debit payment method options to pass to the invoice's PaymentIntent.
        """
        us_bank_account: NotRequired[
            "Literal['']|SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccount"
        ]
        """
        This sub-hash contains details about the ACH direct debit payment method options to pass to the invoice's PaymentIntent.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsAcssDebitMandateOptions"
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
        mandate_options: NotRequired[
            "SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsCardMandateOptions"
        ]
        """
        Configuration options for setting up an eMandate for cards issued in India.
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
        Selected network to process this Subscription on. Depends on the available networks of the card attached to the Subscription. Can be only set confirm-time.
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCardMandateOptions(
        TypedDict,
    ):
        amount: NotRequired[int]
        """
        Amount to be charged for future payments.
        """
        amount_type: NotRequired[Literal["fixed", "maximum"]]
        """
        One of `fixed` or `maximum`. If `fixed`, the `amount` param refers to the exact amount to be charged in future payments. If `maximum`, the amount charged can be up to the value passed for the `amount` param.
        """
        description: NotRequired[str]
        """
        A description of the mandate or subscription that is meant to be displayed to the customer.
        """

    class CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalance(
        TypedDict,
    ):
        bank_transfer: NotRequired[
            "SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransfer"
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
            "SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
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
            "SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnections"
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
            "SubscriptionService.CreateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
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

    class CreateParamsPendingInvoiceItemInterval(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies invoicing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between invoices. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of one year interval allowed (1 year, 12 months, or 52 weeks).
        """

    class CreateParamsTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class CreateParamsTrialSettings(TypedDict):
        end_behavior: (
            "SubscriptionService.CreateParamsTrialSettingsEndBehavior"
        )
        """
        Defines how the subscription should behave when the user's free trial ends.
        """

    class CreateParamsTrialSettingsEndBehavior(TypedDict):
        missing_payment_method: Literal["cancel", "create_invoice", "pause"]
        """
        Indicates how the subscription should change when the trial ends if the user did not provide a payment method.
        """

    class DeleteDiscountParams(TypedDict):
        pass

    class ListParams(TypedDict):
        automatic_tax: NotRequired[
            "SubscriptionService.ListParamsAutomaticTax"
        ]
        """
        Filter subscriptions by their automatic tax settings.
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        The collection method of the subscriptions to retrieve. Either `charge_automatically` or `send_invoice`.
        """
        created: NotRequired["SubscriptionService.ListParamsCreated|int"]
        """
        Only return subscriptions that were created during the given date interval.
        """
        current_period_end: NotRequired[
            "SubscriptionService.ListParamsCurrentPeriodEnd|int"
        ]
        current_period_start: NotRequired[
            "SubscriptionService.ListParamsCurrentPeriodStart|int"
        ]
        customer: NotRequired[str]
        """
        The ID of the customer whose subscriptions will be retrieved.
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
        plan: NotRequired[str]
        """
        The ID of the plan whose subscriptions will be retrieved.
        """
        price: NotRequired[str]
        """
        Filter for subscriptions that contain this recurring price ID.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[
            Literal[
                "active",
                "all",
                "canceled",
                "ended",
                "incomplete",
                "incomplete_expired",
                "past_due",
                "paused",
                "trialing",
                "unpaid",
            ]
        ]
        """
        The status of the subscriptions to retrieve. Passing in a value of `canceled` will return all canceled subscriptions, including those belonging to deleted customers. Pass `ended` to find subscriptions that are canceled and subscriptions that are expired due to [incomplete payment](https://stripe.com/docs/billing/subscriptions/overview#subscription-statuses). Passing in a value of `all` will return subscriptions of all statuses. If no value is supplied, all subscriptions that have not been canceled are returned.
        """
        test_clock: NotRequired[str]
        """
        Filter for subscriptions that are associated with the specified test clock. The response will not include subscriptions with test clocks if this and the customer parameter is not set.
        """

    class ListParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
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

    class ListParamsCurrentPeriodEnd(TypedDict):
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

    class ListParamsCurrentPeriodStart(TypedDict):
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

    class ResumeParams(TypedDict):
        billing_cycle_anchor: NotRequired[Literal["now", "unchanged"]]
        """
        Either `now` or `unchanged`. Setting the value to `now` resets the subscription's billing cycle anchor to the current time (in UTC). Setting the value to `unchanged` advances the subscription's billing cycle anchor to the period that surrounds the current time. For more information, see the billing cycle [documentation](https://stripe.com/docs/billing/subscriptions/billing-cycle).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`.
        """
        proration_date: NotRequired[int]
        """
        If set, the proration will be calculated as though the subscription was resumed at the given time. This can be used to apply exactly the same proration that was previewed with [upcoming invoice](https://stripe.com/docs/api#retrieve_customer_invoice) endpoint.
        """

    class RetrieveParams(TypedDict):
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
        The search query string. See [search query language](https://stripe.com/docs/search#search-query-language) and the list of supported [query fields for subscriptions](https://stripe.com/docs/search#query-fields-for-subscriptions).
        """

    class UpdateParams(TypedDict):
        add_invoice_items: NotRequired[
            List["SubscriptionService.UpdateParamsAddInvoiceItem"]
        ]
        """
        A list of prices and quantities that will generate invoice items appended to the next invoice for this subscription. You may pass up to 20 items.
        """
        application_fee_percent: NotRequired["Literal['']|float"]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. The request must be made by a platform account on a connected account in order to set an application fee percentage. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        automatic_tax: NotRequired[
            "SubscriptionService.UpdateParamsAutomaticTax"
        ]
        """
        Automatic tax settings for this subscription. We recommend you only include this parameter when the existing value is being changed.
        """
        billing_cycle_anchor: NotRequired[Literal["now", "unchanged"]]
        """
        Either `now` or `unchanged`. Setting the value to `now` resets the subscription's billing cycle anchor to the current time (in UTC). For more information, see the billing cycle [documentation](https://stripe.com/docs/billing/subscriptions/billing-cycle).
        """
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. Pass an empty string to remove previously-defined thresholds.
        """
        cancel_at: NotRequired["Literal['']|int"]
        """
        A timestamp at which the subscription should cancel. If set to a date before the current period ends, this will cause a proration if prorations have been enabled using `proration_behavior`. If set during a future period, this will always cause a proration for that period.
        """
        cancel_at_period_end: NotRequired[bool]
        """
        Indicate whether this subscription should cancel at the end of the current period (`current_period_end`). Defaults to `false`.
        """
        cancellation_details: NotRequired[
            "SubscriptionService.UpdateParamsCancellationDetails"
        ]
        """
        Details about why this subscription was cancelled
        """
        collection_method: NotRequired[
            Literal["charge_automatically", "send_invoice"]
        ]
        """
        Either `charge_automatically`, or `send_invoice`. When charging automatically, Stripe will attempt to pay this subscription at the end of the cycle using the default source attached to the customer. When sending an invoice, Stripe will email your customer an invoice with payment instructions and mark the subscription as `active`. Defaults to `charge_automatically`.
        """
        coupon: NotRequired[str]
        """
        The ID of the coupon to apply to this subscription. A coupon applied to a subscription will only affect invoices created for that particular subscription. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        days_until_due: NotRequired[int]
        """
        Number of days a customer has to pay invoices generated by this subscription. Valid only for subscriptions where `collection_method` is set to `send_invoice`.
        """
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the subscription. It must belong to the customer associated with the subscription. This takes precedence over `default_source`. If neither are set, invoices will use the customer's [invoice_settings.default_payment_method](https://stripe.com/docs/api/customers/object#customer_object-invoice_settings-default_payment_method) or [default_source](https://stripe.com/docs/api/customers/object#customer_object-default_source).
        """
        default_source: NotRequired["Literal['']|str"]
        """
        ID of the default payment source for the subscription. It must belong to the customer associated with the subscription and be in a chargeable state. If `default_payment_method` is also set, `default_payment_method` will take precedence. If neither are set, invoices will use the customer's [invoice_settings.default_payment_method](https://stripe.com/docs/api/customers/object#customer_object-invoice_settings-default_payment_method) or [default_source](https://stripe.com/docs/api/customers/object#customer_object-default_source).
        """
        default_tax_rates: NotRequired["Literal['']|List[str]"]
        """
        The tax rates that will apply to any subscription item that does not have `tax_rates` set. Invoices created will have their `default_tax_rates` populated from the subscription. Pass an empty string to remove previously-defined tax rates.
        """
        description: NotRequired["Literal['']|str"]
        """
        The subscription's description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        discounts: NotRequired[
            "Literal['']|List[SubscriptionService.UpdateParamsDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription. If not specified or empty, inherits the discount from the subscription's customer.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_settings: NotRequired[
            "SubscriptionService.UpdateParamsInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        items: NotRequired[List["SubscriptionService.UpdateParamsItem"]]
        """
        A list of up to 20 subscription items, each with an attached price.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        off_session: NotRequired[bool]
        """
        Indicates if a customer is on or off-session while an invoice payment is attempted. Defaults to `false` (on-session).
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account on behalf of which to charge, for each of the subscription's invoices.
        """
        pause_collection: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsPauseCollection"
        ]
        """
        If specified, payment collection for this subscription will be paused. Note that the subscription status will be unchanged and will not be updated to `paused`. Learn more about [pausing collection](https://stripe.com/billing/subscriptions/pause-payment).
        """
        payment_behavior: NotRequired[
            Literal[
                "allow_incomplete",
                "default_incomplete",
                "error_if_incomplete",
                "pending_if_incomplete",
            ]
        ]
        """
        Use `allow_incomplete` to transition the subscription to `status=past_due` if a payment is required but cannot be paid. This allows you to manage scenarios where additional user actions are needed to pay a subscription's invoice. For example, SCA regulation may require 3DS authentication to complete payment. See the [SCA Migration Guide](https://stripe.com/docs/billing/migration/strong-customer-authentication) for Billing to learn more. This is the default behavior.

        Use `default_incomplete` to transition the subscription to `status=past_due` when payment is required and await explicit confirmation of the invoice's payment intent. This allows simpler management of scenarios where additional user actions are needed to pay a subscription's invoice. Such as failed payments, [SCA regulation](https://stripe.com/docs/billing/migration/strong-customer-authentication), or collecting a mandate for a bank debit payment method.

        Use `pending_if_incomplete` to update the subscription using [pending updates](https://stripe.com/docs/billing/subscriptions/pending-updates). When you use `pending_if_incomplete` you can only pass the parameters [supported by pending updates](https://stripe.com/docs/billing/pending-updates-reference#supported-attributes).

        Use `error_if_incomplete` if you want Stripe to return an HTTP 402 status code if a subscription's invoice cannot be paid. For example, if a payment method requires 3DS authentication due to SCA regulation and further user action is needed, this parameter does not update the subscription and returns an error instead. This was the default behavior for API versions prior to 2019-03-14. See the [changelog](https://stripe.com/docs/upgrades#2019-03-14) to learn more.
        """
        payment_settings: NotRequired[
            "SubscriptionService.UpdateParamsPaymentSettings"
        ]
        """
        Payment settings to pass to invoices created by the subscription.
        """
        pending_invoice_item_interval: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsPendingInvoiceItemInterval"
        ]
        """
        Specifies an interval for how often to bill for any pending invoice items. It is analogous to calling [Create an invoice](https://stripe.com/docs/api#create_invoice) for the given subscription at the specified interval.
        """
        promotion_code: NotRequired[str]
        """
        The promotion code to apply to this subscription. A promotion code applied to a subscription will only affect invoices created for that particular subscription. This field has been deprecated and will be removed in a future API version. Use `discounts` instead.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`.
        """
        proration_date: NotRequired[int]
        """
        If set, the proration will be calculated as though the subscription was updated at the given time. This can be used to apply exactly the same proration that was previewed with [upcoming invoice](https://stripe.com/docs/api#upcoming_invoice) endpoint. It can also be used to implement custom proration logic, such as prorating by day instead of by second, by providing the time that you wish to use for proration calculations.
        """
        transfer_data: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsTransferData"
        ]
        """
        If specified, the funds from the subscription's invoices will be transferred to the destination and the ID of the resulting transfers will be found on the resulting charges. This will be unset if you POST an empty value.
        """
        trial_end: NotRequired["Literal['now']|int"]
        """
        Unix timestamp representing the end of the trial period the customer will get before being charged for the first time. This will always overwrite any trials that might apply via a subscribed plan. If set, trial_end will override the default trial period of the plan the customer is being subscribed to. The special value `now` can be provided to end the customer's trial immediately. Can be at most two years from `billing_cycle_anchor`.
        """
        trial_from_plan: NotRequired[bool]
        """
        Indicates if a plan's `trial_period_days` should be applied to the subscription. Setting `trial_end` per subscription is preferred, and this defaults to `false`. Setting this flag to `true` together with `trial_end` is not allowed. See [Using trial periods on subscriptions](https://stripe.com/docs/billing/subscriptions/trials) to learn more.
        """
        trial_settings: NotRequired[
            "SubscriptionService.UpdateParamsTrialSettings"
        ]
        """
        Settings related to subscription trials.
        """

    class UpdateParamsAddInvoiceItem(TypedDict):
        discounts: NotRequired[
            List["SubscriptionService.UpdateParamsAddInvoiceItemDiscount"]
        ]
        """
        The coupons to redeem into discounts for the item.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required.
        """
        price_data: NotRequired[
            "SubscriptionService.UpdateParamsAddInvoiceItemPriceData"
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

    class UpdateParamsAddInvoiceItemDiscount(TypedDict):
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

    class UpdateParamsAddInvoiceItemPriceData(TypedDict):
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

    class UpdateParamsAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
        """
        liability: NotRequired[
            "SubscriptionService.UpdateParamsAutomaticTaxLiability"
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

    class UpdateParamsBillingThresholds(TypedDict):
        amount_gte: NotRequired[int]
        """
        Monetary threshold that triggers the subscription to advance to a new billing period
        """
        reset_billing_cycle_anchor: NotRequired[bool]
        """
        Indicates if the `billing_cycle_anchor` should be reset when a threshold is reached. If true, `billing_cycle_anchor` will be updated to the date/time the threshold was last reached; otherwise, the value will remain unchanged.
        """

    class UpdateParamsCancellationDetails(TypedDict):
        comment: NotRequired["Literal['']|str"]
        """
        Additional comments about why the user canceled the subscription, if the subscription was canceled explicitly by the user.
        """
        feedback: NotRequired[
            "Literal['']|Literal['customer_service', 'low_quality', 'missing_features', 'other', 'switched_service', 'too_complex', 'too_expensive', 'unused']"
        ]
        """
        The customer submitted reason for why they canceled, if the subscription was canceled explicitly by the user.
        """

    class UpdateParamsDiscount(TypedDict):
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

    class UpdateParamsInvoiceSettings(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with the subscription. Will be set on invoices generated by the subscription.
        """
        issuer: NotRequired[
            "SubscriptionService.UpdateParamsInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class UpdateParamsInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpdateParamsItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsItemBillingThresholds"
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
            "Literal['']|List[SubscriptionService.UpdateParamsItemDiscount]"
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
            "SubscriptionService.UpdateParamsItemPriceData"
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

    class UpdateParamsItemBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class UpdateParamsItemDiscount(TypedDict):
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

    class UpdateParamsItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "SubscriptionService.UpdateParamsItemPriceDataRecurring"
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

    class UpdateParamsItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpdateParamsPauseCollection(TypedDict):
        behavior: Literal["keep_as_draft", "mark_uncollectible", "void"]
        """
        The payment collection behavior for this subscription while paused. One of `keep_as_draft`, `mark_uncollectible`, or `void`.
        """
        resumes_at: NotRequired[int]
        """
        The time after which the subscription will resume collecting payments.
        """

    class UpdateParamsPaymentSettings(TypedDict):
        payment_method_options: NotRequired[
            "SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptions"
        ]
        """
        Payment-method-specific configuration to provide to invoices created by the subscription.
        """
        payment_method_types: NotRequired[
            "Literal['']|List[Literal['ach_credit_transfer', 'ach_debit', 'acss_debit', 'amazon_pay', 'au_becs_debit', 'bacs_debit', 'bancontact', 'boleto', 'card', 'cashapp', 'customer_balance', 'eps', 'fpx', 'giropay', 'grabpay', 'ideal', 'konbini', 'link', 'multibanco', 'p24', 'paynow', 'paypal', 'promptpay', 'revolut_pay', 'sepa_credit_transfer', 'sepa_debit', 'sofort', 'swish', 'us_bank_account', 'wechat_pay']]"
        ]
        """
        The list of payment method types (e.g. card) to provide to the invoice's PaymentIntent. If not set, Stripe attempts to automatically determine the types to use by looking at the invoice's default payment method, the subscription's default payment method, the customer's default payment method, and your [invoice template settings](https://dashboard.stripe.com/settings/billing/invoice).
        """
        save_default_payment_method: NotRequired[
            Literal["off", "on_subscription"]
        ]
        """
        Configure whether Stripe updates `subscription.default_payment_method` when payment succeeds. Defaults to `off` if unspecified.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptions(TypedDict):
        acss_debit: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsAcssDebit"
        ]
        """
        This sub-hash contains details about the Canadian pre-authorized debit payment method options to pass to the invoice's PaymentIntent.
        """
        bancontact: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsBancontact"
        ]
        """
        This sub-hash contains details about the Bancontact payment method options to pass to the invoice's PaymentIntent.
        """
        card: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsCard"
        ]
        """
        This sub-hash contains details about the Card payment method options to pass to the invoice's PaymentIntent.
        """
        customer_balance: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalance"
        ]
        """
        This sub-hash contains details about the Bank transfer payment method options to pass to the invoice's PaymentIntent.
        """
        konbini: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsKonbini"
        ]
        """
        This sub-hash contains details about the Konbini payment method options to pass to the invoice's PaymentIntent.
        """
        sepa_debit: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsSepaDebit"
        ]
        """
        This sub-hash contains details about the SEPA Direct Debit payment method options to pass to the invoice's PaymentIntent.
        """
        us_bank_account: NotRequired[
            "Literal['']|SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccount"
        ]
        """
        This sub-hash contains details about the ACH direct debit payment method options to pass to the invoice's PaymentIntent.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsAcssDebit(TypedDict):
        mandate_options: NotRequired[
            "SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsAcssDebitMandateOptions"
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

    class UpdateParamsPaymentSettingsPaymentMethodOptionsAcssDebitMandateOptions(
        TypedDict,
    ):
        transaction_type: NotRequired[Literal["business", "personal"]]
        """
        Transaction type of the mandate.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsBancontact(TypedDict):
        preferred_language: NotRequired[Literal["de", "en", "fr", "nl"]]
        """
        Preferred language of the Bancontact authorization page that the customer is redirected to.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsCard(TypedDict):
        mandate_options: NotRequired[
            "SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsCardMandateOptions"
        ]
        """
        Configuration options for setting up an eMandate for cards issued in India.
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
        Selected network to process this Subscription on. Depends on the available networks of the card attached to the Subscription. Can be only set confirm-time.
        """
        request_three_d_secure: NotRequired[
            Literal["any", "automatic", "challenge"]
        ]
        """
        We strongly recommend that you rely on our SCA Engine to automatically prompt your customers for authentication based on risk level and [other requirements](https://stripe.com/docs/strong-customer-authentication). However, if you wish to request 3D Secure based on logic from your own fraud engine, provide this option. Read our guide on [manually requesting 3D Secure](https://stripe.com/docs/payments/3d-secure/authentication-flow#manual-three-ds) for more information on how this configuration interacts with Radar and our SCA Engine.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsCardMandateOptions(
        TypedDict,
    ):
        amount: NotRequired[int]
        """
        Amount to be charged for future payments.
        """
        amount_type: NotRequired[Literal["fixed", "maximum"]]
        """
        One of `fixed` or `maximum`. If `fixed`, the `amount` param refers to the exact amount to be charged in future payments. If `maximum`, the amount charged can be up to the value passed for the `amount` param.
        """
        description: NotRequired[str]
        """
        A description of the mandate or subscription that is meant to be displayed to the customer.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalance(
        TypedDict,
    ):
        bank_transfer: NotRequired[
            "SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransfer"
        ]
        """
        Configuration for the bank transfer funding type, if the `funding_type` is set to `bank_transfer`.
        """
        funding_type: NotRequired[str]
        """
        The funding method type to be used when there are not enough funds in the customer balance. Permitted values include: `bank_transfer`.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransfer(
        TypedDict,
    ):
        eu_bank_transfer: NotRequired[
            "SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer"
        ]
        """
        Configuration for eu_bank_transfer funding type.
        """
        type: NotRequired[str]
        """
        The bank transfer type that can be used for funding. Permitted values include: `eu_bank_transfer`, `gb_bank_transfer`, `jp_bank_transfer`, `mx_bank_transfer`, or `us_bank_transfer`.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsCustomerBalanceBankTransferEuBankTransfer(
        TypedDict,
    ):
        country: str
        """
        The desired country code of the bank account information. Permitted values include: `BE`, `DE`, `ES`, `FR`, `IE`, or `NL`.
        """

    class UpdateParamsPaymentSettingsPaymentMethodOptionsKonbini(TypedDict):
        pass

    class UpdateParamsPaymentSettingsPaymentMethodOptionsSepaDebit(TypedDict):
        pass

    class UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccount(
        TypedDict,
    ):
        financial_connections: NotRequired[
            "SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnections"
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

    class UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnections(
        TypedDict,
    ):
        filters: NotRequired[
            "SubscriptionService.UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters"
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

    class UpdateParamsPaymentSettingsPaymentMethodOptionsUsBankAccountFinancialConnectionsFilters(
        TypedDict,
    ):
        account_subcategories: NotRequired[
            List[Literal["checking", "savings"]]
        ]
        """
        The account subcategories to use to filter for selectable accounts. Valid subcategories are `checking` and `savings`.
        """

    class UpdateParamsPendingInvoiceItemInterval(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies invoicing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between invoices. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of one year interval allowed (1 year, 12 months, or 52 weeks).
        """

    class UpdateParamsTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class UpdateParamsTrialSettings(TypedDict):
        end_behavior: (
            "SubscriptionService.UpdateParamsTrialSettingsEndBehavior"
        )
        """
        Defines how the subscription should behave when the user's free trial ends.
        """

    class UpdateParamsTrialSettingsEndBehavior(TypedDict):
        missing_payment_method: Literal["cancel", "create_invoice", "pause"]
        """
        Indicates how the subscription should change when the trial ends if the user did not provide a payment method.
        """

    def cancel(
        self,
        subscription_exposed_id: str,
        params: "SubscriptionService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Subscription:
        """
        Cancels a customer's subscription immediately. The customer will not be charged again for the subscription.

        Note, however, that any pending invoice items that you've created will still be charged for at the end of the period, unless manually [deleted](https://stripe.com/docs/api#delete_invoiceitem). If you've set the subscription to cancel at the end of the period, any pending prorations will also be left in place and collected at the end of the period. But if the subscription is set to cancel immediately, pending prorations will be removed.

        By default, upon subscription cancellation, Stripe will stop automatic collection of all finalized invoices for the customer. This is intended to prevent unexpected payment attempts after the customer has canceled a subscription. However, you can resume automatic collection of the invoices manually after subscription cancellation to have us proceed. Or, you could check for unpaid invoices before allowing the customer to cancel the subscription at all.
        """
        return cast(
            Subscription,
            self._request(
                "delete",
                "/v1/subscriptions/{subscription_exposed_id}".format(
                    subscription_exposed_id=sanitize_id(
                        subscription_exposed_id
                    ),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        subscription_exposed_id: str,
        params: "SubscriptionService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> Subscription:
        """
        Cancels a customer's subscription immediately. The customer will not be charged again for the subscription.

        Note, however, that any pending invoice items that you've created will still be charged for at the end of the period, unless manually [deleted](https://stripe.com/docs/api#delete_invoiceitem). If you've set the subscription to cancel at the end of the period, any pending prorations will also be left in place and collected at the end of the period. But if the subscription is set to cancel immediately, pending prorations will be removed.

        By default, upon subscription cancellation, Stripe will stop automatic collection of all finalized invoices for the customer. This is intended to prevent unexpected payment attempts after the customer has canceled a subscription. However, you can resume automatic collection of the invoices manually after subscription cancellation to have us proceed. Or, you could check for unpaid invoices before allowing the customer to cancel the subscription at all.
        """
        return cast(
            Subscription,
            await self._request_async(
                "delete",
                "/v1/subscriptions/{subscription_exposed_id}".format(
                    subscription_exposed_id=sanitize_id(
                        subscription_exposed_id
                    ),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        subscription_exposed_id: str,
        params: "SubscriptionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Subscription:
        """
        Retrieves the subscription with the given ID.
        """
        return cast(
            Subscription,
            self._request(
                "get",
                "/v1/subscriptions/{subscription_exposed_id}".format(
                    subscription_exposed_id=sanitize_id(
                        subscription_exposed_id
                    ),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        subscription_exposed_id: str,
        params: "SubscriptionService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Subscription:
        """
        Retrieves the subscription with the given ID.
        """
        return cast(
            Subscription,
            await self._request_async(
                "get",
                "/v1/subscriptions/{subscription_exposed_id}".format(
                    subscription_exposed_id=sanitize_id(
                        subscription_exposed_id
                    ),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        subscription_exposed_id: str,
        params: "SubscriptionService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Subscription:
        """
        Updates an existing subscription to match the specified parameters.
        When changing prices or quantities, we optionally prorate the price we charge next month to make up for any price changes.
        To preview how the proration is calculated, use the [create preview](https://stripe.com/docs/api/invoices/create_preview) endpoint.

        By default, we prorate subscription changes. For example, if a customer signs up on May 1 for a 100 price, they'll be billed 100 immediately. If on May 15 they switch to a 200 price, then on June 1 they'll be billed 250 (200 for a renewal of her subscription, plus a 50 prorating adjustment for half of the previous month's 100 difference). Similarly, a downgrade generates a credit that is applied to the next invoice. We also prorate when you make quantity changes.

        Switching prices does not normally change the billing date or generate an immediate charge unless:


        The billing interval is changed (for example, from monthly to yearly).
        The subscription moves from free to paid.
        A trial starts or ends.


        In these cases, we apply a credit for the unused time on the previous price, immediately charge the customer using the new price, and reset the billing date. Learn about how [Stripe immediately attempts payment for subscription changes](https://stripe.com/billing/subscriptions/upgrade-downgrade#immediate-payment).

        If you want to charge for an upgrade immediately, pass proration_behavior as always_invoice to create prorations, automatically invoice the customer for those proration adjustments, and attempt to collect payment. If you pass create_prorations, the prorations are created but not automatically invoiced. If you want to bill the customer for the prorations before the subscription's renewal date, you need to manually [invoice the customer](https://stripe.com/docs/api/invoices/create).

        If you don't want to prorate, set the proration_behavior option to none. With this option, the customer is billed 100 on May 1 and 200 on June 1. Similarly, if you set proration_behavior to none when switching between different billing intervals (for example, from monthly to yearly), we don't generate any credits for the old subscription's unused time. We still reset the billing date and bill immediately for the new subscription.

        Updating the quantity on a subscription many times in an hour may result in [rate limiting. If you need to bill for a frequently changing quantity, consider integrating <a href="/docs/billing/subscriptions/usage-based">usage-based billing](https://stripe.com/docs/rate-limits) instead.
        """
        return cast(
            Subscription,
            self._request(
                "post",
                "/v1/subscriptions/{subscription_exposed_id}".format(
                    subscription_exposed_id=sanitize_id(
                        subscription_exposed_id
                    ),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        subscription_exposed_id: str,
        params: "SubscriptionService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Subscription:
        """
        Updates an existing subscription to match the specified parameters.
        When changing prices or quantities, we optionally prorate the price we charge next month to make up for any price changes.
        To preview how the proration is calculated, use the [create preview](https://stripe.com/docs/api/invoices/create_preview) endpoint.

        By default, we prorate subscription changes. For example, if a customer signs up on May 1 for a 100 price, they'll be billed 100 immediately. If on May 15 they switch to a 200 price, then on June 1 they'll be billed 250 (200 for a renewal of her subscription, plus a 50 prorating adjustment for half of the previous month's 100 difference). Similarly, a downgrade generates a credit that is applied to the next invoice. We also prorate when you make quantity changes.

        Switching prices does not normally change the billing date or generate an immediate charge unless:


        The billing interval is changed (for example, from monthly to yearly).
        The subscription moves from free to paid.
        A trial starts or ends.


        In these cases, we apply a credit for the unused time on the previous price, immediately charge the customer using the new price, and reset the billing date. Learn about how [Stripe immediately attempts payment for subscription changes](https://stripe.com/billing/subscriptions/upgrade-downgrade#immediate-payment).

        If you want to charge for an upgrade immediately, pass proration_behavior as always_invoice to create prorations, automatically invoice the customer for those proration adjustments, and attempt to collect payment. If you pass create_prorations, the prorations are created but not automatically invoiced. If you want to bill the customer for the prorations before the subscription's renewal date, you need to manually [invoice the customer](https://stripe.com/docs/api/invoices/create).

        If you don't want to prorate, set the proration_behavior option to none. With this option, the customer is billed 100 on May 1 and 200 on June 1. Similarly, if you set proration_behavior to none when switching between different billing intervals (for example, from monthly to yearly), we don't generate any credits for the old subscription's unused time. We still reset the billing date and bill immediately for the new subscription.

        Updating the quantity on a subscription many times in an hour may result in [rate limiting. If you need to bill for a frequently changing quantity, consider integrating <a href="/docs/billing/subscriptions/usage-based">usage-based billing](https://stripe.com/docs/rate-limits) instead.
        """
        return cast(
            Subscription,
            await self._request_async(
                "post",
                "/v1/subscriptions/{subscription_exposed_id}".format(
                    subscription_exposed_id=sanitize_id(
                        subscription_exposed_id
                    ),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def delete_discount(
        self,
        subscription_exposed_id: str,
        params: "SubscriptionService.DeleteDiscountParams" = {},
        options: RequestOptions = {},
    ) -> Discount:
        """
        Removes the currently applied discount on a subscription.
        """
        return cast(
            Discount,
            self._request(
                "delete",
                "/v1/subscriptions/{subscription_exposed_id}/discount".format(
                    subscription_exposed_id=sanitize_id(
                        subscription_exposed_id
                    ),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_discount_async(
        self,
        subscription_exposed_id: str,
        params: "SubscriptionService.DeleteDiscountParams" = {},
        options: RequestOptions = {},
    ) -> Discount:
        """
        Removes the currently applied discount on a subscription.
        """
        return cast(
            Discount,
            await self._request_async(
                "delete",
                "/v1/subscriptions/{subscription_exposed_id}/discount".format(
                    subscription_exposed_id=sanitize_id(
                        subscription_exposed_id
                    ),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "SubscriptionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Subscription]:
        """
        By default, returns a list of subscriptions that have not been canceled. In order to list canceled subscriptions, specify status=canceled.
        """
        return cast(
            ListObject[Subscription],
            self._request(
                "get",
                "/v1/subscriptions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "SubscriptionService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Subscription]:
        """
        By default, returns a list of subscriptions that have not been canceled. In order to list canceled subscriptions, specify status=canceled.
        """
        return cast(
            ListObject[Subscription],
            await self._request_async(
                "get",
                "/v1/subscriptions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "SubscriptionService.CreateParams",
        options: RequestOptions = {},
    ) -> Subscription:
        """
        Creates a new subscription on an existing customer. Each customer can have up to 500 active or scheduled subscriptions.

        When you create a subscription with collection_method=charge_automatically, the first invoice is finalized as part of the request.
        The payment_behavior parameter determines the exact behavior of the initial payment.

        To start subscriptions where the first invoice always begins in a draft status, use [subscription schedules](https://stripe.com/docs/billing/subscriptions/subscription-schedules#managing) instead.
        Schedules provide the flexibility to model more complex billing configurations that change over time.
        """
        return cast(
            Subscription,
            self._request(
                "post",
                "/v1/subscriptions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "SubscriptionService.CreateParams",
        options: RequestOptions = {},
    ) -> Subscription:
        """
        Creates a new subscription on an existing customer. Each customer can have up to 500 active or scheduled subscriptions.

        When you create a subscription with collection_method=charge_automatically, the first invoice is finalized as part of the request.
        The payment_behavior parameter determines the exact behavior of the initial payment.

        To start subscriptions where the first invoice always begins in a draft status, use [subscription schedules](https://stripe.com/docs/billing/subscriptions/subscription-schedules#managing) instead.
        Schedules provide the flexibility to model more complex billing configurations that change over time.
        """
        return cast(
            Subscription,
            await self._request_async(
                "post",
                "/v1/subscriptions",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def search(
        self,
        params: "SubscriptionService.SearchParams",
        options: RequestOptions = {},
    ) -> SearchResultObject[Subscription]:
        """
        Search for subscriptions you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[Subscription],
            self._request(
                "get",
                "/v1/subscriptions/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def search_async(
        self,
        params: "SubscriptionService.SearchParams",
        options: RequestOptions = {},
    ) -> SearchResultObject[Subscription]:
        """
        Search for subscriptions you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[Subscription],
            await self._request_async(
                "get",
                "/v1/subscriptions/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def resume(
        self,
        subscription: str,
        params: "SubscriptionService.ResumeParams" = {},
        options: RequestOptions = {},
    ) -> Subscription:
        """
        Initiates resumption of a paused subscription, optionally resetting the billing cycle anchor and creating prorations. If a resumption invoice is generated, it must be paid or marked uncollectible before the subscription will be unpaused. If payment succeeds the subscription will become active, and if payment fails the subscription will be past_due. The resumption invoice will void automatically if not paid by the expiration date.
        """
        return cast(
            Subscription,
            self._request(
                "post",
                "/v1/subscriptions/{subscription}/resume".format(
                    subscription=sanitize_id(subscription),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def resume_async(
        self,
        subscription: str,
        params: "SubscriptionService.ResumeParams" = {},
        options: RequestOptions = {},
    ) -> Subscription:
        """
        Initiates resumption of a paused subscription, optionally resetting the billing cycle anchor and creating prorations. If a resumption invoice is generated, it must be paid or marked uncollectible before the subscription will be unpaused. If payment succeeds the subscription will become active, and if payment fails the subscription will be past_due. The resumption invoice will void automatically if not paid by the expiration date.
        """
        return cast(
            Subscription,
            await self._request_async(
                "post",
                "/v1/subscriptions/{subscription}/resume".format(
                    subscription=sanitize_id(subscription),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
