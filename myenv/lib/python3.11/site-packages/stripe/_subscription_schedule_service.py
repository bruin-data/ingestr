# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._subscription_schedule import SubscriptionSchedule
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class SubscriptionScheduleService(StripeService):
    class CancelParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        invoice_now: NotRequired[bool]
        """
        If the subscription schedule is `active`, indicates if a final invoice will be generated that contains any un-invoiced metered usage and new/pending proration invoice items. Defaults to `true`.
        """
        prorate: NotRequired[bool]
        """
        If the subscription schedule is `active`, indicates if the cancellation should be prorated. Defaults to `true`.
        """

    class CreateParams(TypedDict):
        customer: NotRequired[str]
        """
        The identifier of the customer to create the subscription schedule for.
        """
        default_settings: NotRequired[
            "SubscriptionScheduleService.CreateParamsDefaultSettings"
        ]
        """
        Object representing the subscription schedule's default settings.
        """
        end_behavior: NotRequired[
            Literal["cancel", "none", "release", "renew"]
        ]
        """
        Behavior of the subscription schedule and underlying subscription when it ends. Possible values are `release` or `cancel` with the default being `release`. `release` will end the subscription schedule and keep the underlying subscription running. `cancel` will end the subscription schedule and cancel the underlying subscription.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        from_subscription: NotRequired[str]
        """
        Migrate an existing subscription to be managed by a subscription schedule. If this parameter is set, a subscription schedule will be created using the subscription's item(s), set to auto-renew using the subscription's interval. When using this parameter, other parameters (such as phase values) cannot be set. To create a subscription schedule with other modifications, we recommend making two separate API calls.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        phases: NotRequired[
            List["SubscriptionScheduleService.CreateParamsPhase"]
        ]
        """
        List representing phases of the subscription schedule. Each phase can be customized to have different durations, plans, and coupons. If there are multiple phases, the `end_date` of one phase will always equal the `start_date` of the next phase.
        """
        start_date: NotRequired["int|Literal['now']"]
        """
        When the subscription schedule starts. We recommend using `now` so that it starts the subscription immediately. You can also use a Unix timestamp to backdate the subscription so that it starts on a past date, or set a future date for the subscription to start on.
        """

    class CreateParamsDefaultSettings(TypedDict):
        application_fee_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. The request must be made by a platform account on a connected account in order to set an application fee percentage. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        automatic_tax: NotRequired[
            "SubscriptionScheduleService.CreateParamsDefaultSettingsAutomaticTax"
        ]
        """
        Default settings for automatic tax computation.
        """
        billing_cycle_anchor: NotRequired[Literal["automatic", "phase_start"]]
        """
        Can be set to `phase_start` to set the anchor to the start of the phase or `automatic` to automatically change it if needed. Cannot be set to `phase_start` if this phase specifies a trial. For more information, see the billing cycle [documentation](https://stripe.com/docs/billing/subscriptions/billing-cycle).
        """
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionScheduleService.CreateParamsDefaultSettingsBillingThresholds"
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
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the subscription schedule. It must belong to the customer associated with the subscription schedule. If not set, invoices will use the default payment method in the customer's invoice settings.
        """
        description: NotRequired["Literal['']|str"]
        """
        Subscription description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        invoice_settings: NotRequired[
            "SubscriptionScheduleService.CreateParamsDefaultSettingsInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account on behalf of which to charge, for each of the associated subscription's invoices.
        """
        transfer_data: NotRequired[
            "Literal['']|SubscriptionScheduleService.CreateParamsDefaultSettingsTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the associated subscription's invoices.
        """

    class CreateParamsDefaultSettingsAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
        """
        liability: NotRequired[
            "SubscriptionScheduleService.CreateParamsDefaultSettingsAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class CreateParamsDefaultSettingsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsDefaultSettingsBillingThresholds(TypedDict):
        amount_gte: NotRequired[int]
        """
        Monetary threshold that triggers the subscription to advance to a new billing period
        """
        reset_billing_cycle_anchor: NotRequired[bool]
        """
        Indicates if the `billing_cycle_anchor` should be reset when a threshold is reached. If true, `billing_cycle_anchor` will be updated to the date/time the threshold was last reached; otherwise, the value will remain unchanged.
        """

    class CreateParamsDefaultSettingsInvoiceSettings(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with the subscription schedule. Will be set on invoices generated by the subscription schedule.
        """
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay invoices generated by this subscription schedule. This value will be `null` for subscription schedules where `collection_method=charge_automatically`.
        """
        issuer: NotRequired[
            "SubscriptionScheduleService.CreateParamsDefaultSettingsInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class CreateParamsDefaultSettingsInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsDefaultSettingsTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class CreateParamsPhase(TypedDict):
        add_invoice_items: NotRequired[
            List["SubscriptionScheduleService.CreateParamsPhaseAddInvoiceItem"]
        ]
        """
        A list of prices and quantities that will generate invoice items appended to the next invoice for this phase. You may pass up to 20 items.
        """
        application_fee_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. The request must be made by a platform account on a connected account in order to set an application fee percentage. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        automatic_tax: NotRequired[
            "SubscriptionScheduleService.CreateParamsPhaseAutomaticTax"
        ]
        """
        Automatic tax settings for this phase.
        """
        billing_cycle_anchor: NotRequired[Literal["automatic", "phase_start"]]
        """
        Can be set to `phase_start` to set the anchor to the start of the phase or `automatic` to automatically change it if needed. Cannot be set to `phase_start` if this phase specifies a trial. For more information, see the billing cycle [documentation](https://stripe.com/docs/billing/subscriptions/billing-cycle).
        """
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionScheduleService.CreateParamsPhaseBillingThresholds"
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
            "Literal['']|List[SubscriptionScheduleService.CreateParamsPhaseDiscount]"
        ]
        """
        The coupons to redeem into discounts for the schedule phase. If not specified, inherits the discount from the subscription's customer. Pass an empty string to avoid inheriting any discounts.
        """
        end_date: NotRequired[int]
        """
        The date at which this phase of the subscription schedule ends. If set, `iterations` must not be set.
        """
        invoice_settings: NotRequired[
            "SubscriptionScheduleService.CreateParamsPhaseInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        items: List["SubscriptionScheduleService.CreateParamsPhaseItem"]
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
        transfer_data: NotRequired[
            "SubscriptionScheduleService.CreateParamsPhaseTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the associated subscription's invoices.
        """
        trial: NotRequired[bool]
        """
        If set to true the entire phase is counted as a trial and the customer will not be charged for any fees.
        """
        trial_end: NotRequired[int]
        """
        Sets the phase to trialing from the start date to this date. Must be before the phase end date, can not be combined with `trial`
        """

    class CreateParamsPhaseAddInvoiceItem(TypedDict):
        discounts: NotRequired[
            List[
                "SubscriptionScheduleService.CreateParamsPhaseAddInvoiceItemDiscount"
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
            "SubscriptionScheduleService.CreateParamsPhaseAddInvoiceItemPriceData"
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

    class CreateParamsPhaseAddInvoiceItemDiscount(TypedDict):
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

    class CreateParamsPhaseAddInvoiceItemPriceData(TypedDict):
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

    class CreateParamsPhaseAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
        """
        liability: NotRequired[
            "SubscriptionScheduleService.CreateParamsPhaseAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class CreateParamsPhaseAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsPhaseBillingThresholds(TypedDict):
        amount_gte: NotRequired[int]
        """
        Monetary threshold that triggers the subscription to advance to a new billing period
        """
        reset_billing_cycle_anchor: NotRequired[bool]
        """
        Indicates if the `billing_cycle_anchor` should be reset when a threshold is reached. If true, `billing_cycle_anchor` will be updated to the date/time the threshold was last reached; otherwise, the value will remain unchanged.
        """

    class CreateParamsPhaseDiscount(TypedDict):
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

    class CreateParamsPhaseInvoiceSettings(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with this phase of the subscription schedule. Will be set on invoices generated by this phase of the subscription schedule.
        """
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay invoices generated by this subscription schedule. This value will be `null` for subscription schedules where `billing=charge_automatically`.
        """
        issuer: NotRequired[
            "SubscriptionScheduleService.CreateParamsPhaseInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class CreateParamsPhaseInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class CreateParamsPhaseItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionScheduleService.CreateParamsPhaseItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[SubscriptionScheduleService.CreateParamsPhaseItemDiscount]"
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
            "SubscriptionScheduleService.CreateParamsPhaseItemPriceData"
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

    class CreateParamsPhaseItemBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class CreateParamsPhaseItemDiscount(TypedDict):
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

    class CreateParamsPhaseItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "SubscriptionScheduleService.CreateParamsPhaseItemPriceDataRecurring"
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

    class CreateParamsPhaseItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class CreateParamsPhaseTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class ListParams(TypedDict):
        canceled_at: NotRequired[
            "SubscriptionScheduleService.ListParamsCanceledAt|int"
        ]
        """
        Only return subscription schedules that were created canceled the given date interval.
        """
        completed_at: NotRequired[
            "SubscriptionScheduleService.ListParamsCompletedAt|int"
        ]
        """
        Only return subscription schedules that completed during the given date interval.
        """
        created: NotRequired[
            "SubscriptionScheduleService.ListParamsCreated|int"
        ]
        """
        Only return subscription schedules that were created during the given date interval.
        """
        customer: NotRequired[str]
        """
        Only return subscription schedules for the given customer.
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
        released_at: NotRequired[
            "SubscriptionScheduleService.ListParamsReleasedAt|int"
        ]
        """
        Only return subscription schedules that were released during the given date interval.
        """
        scheduled: NotRequired[bool]
        """
        Only return subscription schedules that have not started yet.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """

    class ListParamsCanceledAt(TypedDict):
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

    class ListParamsCompletedAt(TypedDict):
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

    class ListParamsReleasedAt(TypedDict):
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

    class ReleaseParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        preserve_cancel_date: NotRequired[bool]
        """
        Keep any cancellation on the subscription that the schedule has set
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        default_settings: NotRequired[
            "SubscriptionScheduleService.UpdateParamsDefaultSettings"
        ]
        """
        Object representing the subscription schedule's default settings.
        """
        end_behavior: NotRequired[
            Literal["cancel", "none", "release", "renew"]
        ]
        """
        Behavior of the subscription schedule and underlying subscription when it ends. Possible values are `release` or `cancel` with the default being `release`. `release` will end the subscription schedule and keep the underlying subscription running. `cancel` will end the subscription schedule and cancel the underlying subscription.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        phases: NotRequired[
            List["SubscriptionScheduleService.UpdateParamsPhase"]
        ]
        """
        List representing phases of the subscription schedule. Each phase can be customized to have different durations, plans, and coupons. If there are multiple phases, the `end_date` of one phase will always equal the `start_date` of the next phase. Note that past phases can be omitted.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        If the update changes the current phase, indicates whether the changes should be prorated. The default value is `create_prorations`.
        """

    class UpdateParamsDefaultSettings(TypedDict):
        application_fee_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. The request must be made by a platform account on a connected account in order to set an application fee percentage. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        automatic_tax: NotRequired[
            "SubscriptionScheduleService.UpdateParamsDefaultSettingsAutomaticTax"
        ]
        """
        Default settings for automatic tax computation.
        """
        billing_cycle_anchor: NotRequired[Literal["automatic", "phase_start"]]
        """
        Can be set to `phase_start` to set the anchor to the start of the phase or `automatic` to automatically change it if needed. Cannot be set to `phase_start` if this phase specifies a trial. For more information, see the billing cycle [documentation](https://stripe.com/docs/billing/subscriptions/billing-cycle).
        """
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionScheduleService.UpdateParamsDefaultSettingsBillingThresholds"
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
        default_payment_method: NotRequired[str]
        """
        ID of the default payment method for the subscription schedule. It must belong to the customer associated with the subscription schedule. If not set, invoices will use the default payment method in the customer's invoice settings.
        """
        description: NotRequired["Literal['']|str"]
        """
        Subscription description, meant to be displayable to the customer. Use this field to optionally store an explanation of the subscription for rendering in Stripe surfaces and certain local payment methods UIs.
        """
        invoice_settings: NotRequired[
            "SubscriptionScheduleService.UpdateParamsDefaultSettingsInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        on_behalf_of: NotRequired["Literal['']|str"]
        """
        The account on behalf of which to charge, for each of the associated subscription's invoices.
        """
        transfer_data: NotRequired[
            "Literal['']|SubscriptionScheduleService.UpdateParamsDefaultSettingsTransferData"
        ]
        """
        The data with which to automatically create a Transfer for each of the associated subscription's invoices.
        """

    class UpdateParamsDefaultSettingsAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
        """
        liability: NotRequired[
            "SubscriptionScheduleService.UpdateParamsDefaultSettingsAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class UpdateParamsDefaultSettingsAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpdateParamsDefaultSettingsBillingThresholds(TypedDict):
        amount_gte: NotRequired[int]
        """
        Monetary threshold that triggers the subscription to advance to a new billing period
        """
        reset_billing_cycle_anchor: NotRequired[bool]
        """
        Indicates if the `billing_cycle_anchor` should be reset when a threshold is reached. If true, `billing_cycle_anchor` will be updated to the date/time the threshold was last reached; otherwise, the value will remain unchanged.
        """

    class UpdateParamsDefaultSettingsInvoiceSettings(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with the subscription schedule. Will be set on invoices generated by the subscription schedule.
        """
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay invoices generated by this subscription schedule. This value will be `null` for subscription schedules where `collection_method=charge_automatically`.
        """
        issuer: NotRequired[
            "SubscriptionScheduleService.UpdateParamsDefaultSettingsInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class UpdateParamsDefaultSettingsInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpdateParamsDefaultSettingsTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    class UpdateParamsPhase(TypedDict):
        add_invoice_items: NotRequired[
            List["SubscriptionScheduleService.UpdateParamsPhaseAddInvoiceItem"]
        ]
        """
        A list of prices and quantities that will generate invoice items appended to the next invoice for this phase. You may pass up to 20 items.
        """
        application_fee_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the application owner's Stripe account. The request must be made by a platform account on a connected account in order to set an application fee percentage. For more information, see the application fees [documentation](https://stripe.com/docs/connect/subscriptions#collecting-fees-on-subscriptions).
        """
        automatic_tax: NotRequired[
            "SubscriptionScheduleService.UpdateParamsPhaseAutomaticTax"
        ]
        """
        Automatic tax settings for this phase.
        """
        billing_cycle_anchor: NotRequired[Literal["automatic", "phase_start"]]
        """
        Can be set to `phase_start` to set the anchor to the start of the phase or `automatic` to automatically change it if needed. Cannot be set to `phase_start` if this phase specifies a trial. For more information, see the billing cycle [documentation](https://stripe.com/docs/billing/subscriptions/billing-cycle).
        """
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionScheduleService.UpdateParamsPhaseBillingThresholds"
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
            "Literal['']|List[SubscriptionScheduleService.UpdateParamsPhaseDiscount]"
        ]
        """
        The coupons to redeem into discounts for the schedule phase. If not specified, inherits the discount from the subscription's customer. Pass an empty string to avoid inheriting any discounts.
        """
        end_date: NotRequired["int|Literal['now']"]
        """
        The date at which this phase of the subscription schedule ends. If set, `iterations` must not be set.
        """
        invoice_settings: NotRequired[
            "SubscriptionScheduleService.UpdateParamsPhaseInvoiceSettings"
        ]
        """
        All invoices will be billed using the specified settings.
        """
        items: List["SubscriptionScheduleService.UpdateParamsPhaseItem"]
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
            "SubscriptionScheduleService.UpdateParamsPhaseTransferData"
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

    class UpdateParamsPhaseAddInvoiceItem(TypedDict):
        discounts: NotRequired[
            List[
                "SubscriptionScheduleService.UpdateParamsPhaseAddInvoiceItemDiscount"
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
            "SubscriptionScheduleService.UpdateParamsPhaseAddInvoiceItemPriceData"
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

    class UpdateParamsPhaseAddInvoiceItemDiscount(TypedDict):
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

    class UpdateParamsPhaseAddInvoiceItemPriceData(TypedDict):
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

    class UpdateParamsPhaseAutomaticTax(TypedDict):
        enabled: bool
        """
        Enabled automatic tax calculation which will automatically compute tax rates on all invoices generated by the subscription.
        """
        liability: NotRequired[
            "SubscriptionScheduleService.UpdateParamsPhaseAutomaticTaxLiability"
        ]
        """
        The account that's liable for tax. If set, the business address and tax registrations required to perform the tax calculation are loaded from this account. The tax transaction is returned in the report of the connected account.
        """

    class UpdateParamsPhaseAutomaticTaxLiability(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpdateParamsPhaseBillingThresholds(TypedDict):
        amount_gte: NotRequired[int]
        """
        Monetary threshold that triggers the subscription to advance to a new billing period
        """
        reset_billing_cycle_anchor: NotRequired[bool]
        """
        Indicates if the `billing_cycle_anchor` should be reset when a threshold is reached. If true, `billing_cycle_anchor` will be updated to the date/time the threshold was last reached; otherwise, the value will remain unchanged.
        """

    class UpdateParamsPhaseDiscount(TypedDict):
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

    class UpdateParamsPhaseInvoiceSettings(TypedDict):
        account_tax_ids: NotRequired["Literal['']|List[str]"]
        """
        The account tax IDs associated with this phase of the subscription schedule. Will be set on invoices generated by this phase of the subscription schedule.
        """
        days_until_due: NotRequired[int]
        """
        Number of days within which a customer must pay invoices generated by this subscription schedule. This value will be `null` for subscription schedules where `billing=charge_automatically`.
        """
        issuer: NotRequired[
            "SubscriptionScheduleService.UpdateParamsPhaseInvoiceSettingsIssuer"
        ]
        """
        The connected account that issues the invoice. The invoice is presented with the branding and support information of the specified account.
        """

    class UpdateParamsPhaseInvoiceSettingsIssuer(TypedDict):
        account: NotRequired[str]
        """
        The connected account being referenced when `type` is `account`.
        """
        type: Literal["account", "self"]
        """
        Type of the account referenced in the request.
        """

    class UpdateParamsPhaseItem(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionScheduleService.UpdateParamsPhaseItemBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[SubscriptionScheduleService.UpdateParamsPhaseItemDiscount]"
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
            "SubscriptionScheduleService.UpdateParamsPhaseItemPriceData"
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

    class UpdateParamsPhaseItemBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class UpdateParamsPhaseItemDiscount(TypedDict):
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

    class UpdateParamsPhaseItemPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "SubscriptionScheduleService.UpdateParamsPhaseItemPriceDataRecurring"
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

    class UpdateParamsPhaseItemPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class UpdateParamsPhaseTransferData(TypedDict):
        amount_percent: NotRequired[float]
        """
        A non-negative decimal between 0 and 100, with at most two decimal places. This represents the percentage of the subscription invoice total that will be transferred to the destination account. By default, the entire amount is transferred to the destination.
        """
        destination: str
        """
        ID of an existing, connected Stripe account.
        """

    def list(
        self,
        params: "SubscriptionScheduleService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[SubscriptionSchedule]:
        """
        Retrieves the list of your subscription schedules.
        """
        return cast(
            ListObject[SubscriptionSchedule],
            self._request(
                "get",
                "/v1/subscription_schedules",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "SubscriptionScheduleService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[SubscriptionSchedule]:
        """
        Retrieves the list of your subscription schedules.
        """
        return cast(
            ListObject[SubscriptionSchedule],
            await self._request_async(
                "get",
                "/v1/subscription_schedules",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "SubscriptionScheduleService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionSchedule:
        """
        Creates a new subscription schedule object. Each customer can have up to 500 active or scheduled subscriptions.
        """
        return cast(
            SubscriptionSchedule,
            self._request(
                "post",
                "/v1/subscription_schedules",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "SubscriptionScheduleService.CreateParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionSchedule:
        """
        Creates a new subscription schedule object. Each customer can have up to 500 active or scheduled subscriptions.
        """
        return cast(
            SubscriptionSchedule,
            await self._request_async(
                "post",
                "/v1/subscription_schedules",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        schedule: str,
        params: "SubscriptionScheduleService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionSchedule:
        """
        Retrieves the details of an existing subscription schedule. You only need to supply the unique subscription schedule identifier that was returned upon subscription schedule creation.
        """
        return cast(
            SubscriptionSchedule,
            self._request(
                "get",
                "/v1/subscription_schedules/{schedule}".format(
                    schedule=sanitize_id(schedule),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        schedule: str,
        params: "SubscriptionScheduleService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionSchedule:
        """
        Retrieves the details of an existing subscription schedule. You only need to supply the unique subscription schedule identifier that was returned upon subscription schedule creation.
        """
        return cast(
            SubscriptionSchedule,
            await self._request_async(
                "get",
                "/v1/subscription_schedules/{schedule}".format(
                    schedule=sanitize_id(schedule),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        schedule: str,
        params: "SubscriptionScheduleService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionSchedule:
        """
        Updates an existing subscription schedule.
        """
        return cast(
            SubscriptionSchedule,
            self._request(
                "post",
                "/v1/subscription_schedules/{schedule}".format(
                    schedule=sanitize_id(schedule),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        schedule: str,
        params: "SubscriptionScheduleService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionSchedule:
        """
        Updates an existing subscription schedule.
        """
        return cast(
            SubscriptionSchedule,
            await self._request_async(
                "post",
                "/v1/subscription_schedules/{schedule}".format(
                    schedule=sanitize_id(schedule),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def cancel(
        self,
        schedule: str,
        params: "SubscriptionScheduleService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionSchedule:
        """
        Cancels a subscription schedule and its associated subscription immediately (if the subscription schedule has an active subscription). A subscription schedule can only be canceled if its status is not_started or active.
        """
        return cast(
            SubscriptionSchedule,
            self._request(
                "post",
                "/v1/subscription_schedules/{schedule}/cancel".format(
                    schedule=sanitize_id(schedule),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def cancel_async(
        self,
        schedule: str,
        params: "SubscriptionScheduleService.CancelParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionSchedule:
        """
        Cancels a subscription schedule and its associated subscription immediately (if the subscription schedule has an active subscription). A subscription schedule can only be canceled if its status is not_started or active.
        """
        return cast(
            SubscriptionSchedule,
            await self._request_async(
                "post",
                "/v1/subscription_schedules/{schedule}/cancel".format(
                    schedule=sanitize_id(schedule),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def release(
        self,
        schedule: str,
        params: "SubscriptionScheduleService.ReleaseParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionSchedule:
        """
        Releases the subscription schedule immediately, which will stop scheduling of its phases, but leave any existing subscription in place. A schedule can only be released if its status is not_started or active. If the subscription schedule is currently associated with a subscription, releasing it will remove its subscription property and set the subscription's ID to the released_subscription property.
        """
        return cast(
            SubscriptionSchedule,
            self._request(
                "post",
                "/v1/subscription_schedules/{schedule}/release".format(
                    schedule=sanitize_id(schedule),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def release_async(
        self,
        schedule: str,
        params: "SubscriptionScheduleService.ReleaseParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionSchedule:
        """
        Releases the subscription schedule immediately, which will stop scheduling of its phases, but leave any existing subscription in place. A schedule can only be released if its status is not_started or active. If the subscription schedule is currently associated with a subscription, releasing it will remove its subscription property and set the subscription's ID to the released_subscription property.
        """
        return cast(
            SubscriptionSchedule,
            await self._request_async(
                "post",
                "/v1/subscription_schedules/{schedule}/release".format(
                    schedule=sanitize_id(schedule),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
