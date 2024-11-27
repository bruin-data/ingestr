# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._subscription_item import SubscriptionItem
from stripe._subscription_item_usage_record_service import (
    SubscriptionItemUsageRecordService,
)
from stripe._subscription_item_usage_record_summary_service import (
    SubscriptionItemUsageRecordSummaryService,
)
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class SubscriptionItemService(StripeService):
    def __init__(self, requestor):
        super().__init__(requestor)
        self.usage_records = SubscriptionItemUsageRecordService(
            self._requestor
        )
        self.usage_record_summaries = (
            SubscriptionItemUsageRecordSummaryService(
                self._requestor,
            )
        )

    class CreateParams(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionItemService.CreateParamsBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[SubscriptionItemService.CreateParamsDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
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
        plan: NotRequired[str]
        """
        The identifier of the plan to add to the subscription.
        """
        price: NotRequired[str]
        """
        The ID of the price object.
        """
        price_data: NotRequired[
            "SubscriptionItemService.CreateParamsPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`.
        """
        proration_date: NotRequired[int]
        """
        If set, the proration will be calculated as though the subscription was updated at the given time. This can be used to apply the same proration that was previewed with the [upcoming invoice](https://stripe.com/docs/api#retrieve_customer_invoice) endpoint.
        """
        quantity: NotRequired[int]
        """
        The quantity you'd like to apply to the subscription item you're creating.
        """
        subscription: str
        """
        The identifier of the subscription to modify.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class CreateParamsBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
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

    class CreateParamsPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "SubscriptionItemService.CreateParamsPriceDataRecurring"
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

    class CreateParamsPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class DeleteParams(TypedDict):
        clear_usage: NotRequired[bool]
        """
        Delete all usage for the given subscription item. Allowed only when the current plan's `usage_type` is `metered`.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`.
        """
        proration_date: NotRequired[int]
        """
        If set, the proration will be calculated as though the subscription was updated at the given time. This can be used to apply the same proration that was previewed with the [upcoming invoice](https://stripe.com/docs/api#retrieve_customer_invoice) endpoint.
        """

    class ListParams(TypedDict):
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
        subscription: str
        """
        The ID of the subscription whose items will be retrieved.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionItemService.UpdateParamsBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[SubscriptionItemService.UpdateParamsDiscount]"
        ]
        """
        The coupons to redeem into discounts for the subscription item.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        off_session: NotRequired[bool]
        """
        Indicates if a customer is on or off-session while an invoice payment is attempted. Defaults to `false` (on-session).
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
        plan: NotRequired[str]
        """
        The identifier of the new plan for this subscription item.
        """
        price: NotRequired[str]
        """
        The ID of the price object. One of `price` or `price_data` is required. When changing a subscription item's price, `quantity` is set to 1 unless a `quantity` parameter is provided.
        """
        price_data: NotRequired[
            "SubscriptionItemService.UpdateParamsPriceData"
        ]
        """
        Data used to generate a new [Price](https://stripe.com/docs/api/prices) object inline. One of `price` or `price_data` is required.
        """
        proration_behavior: NotRequired[
            Literal["always_invoice", "create_prorations", "none"]
        ]
        """
        Determines how to handle [prorations](https://stripe.com/docs/billing/subscriptions/prorations) when the billing cycle changes (e.g., when switching plans, resetting `billing_cycle_anchor=now`, or starting a trial), or if an item's `quantity` changes. The default value is `create_prorations`.
        """
        proration_date: NotRequired[int]
        """
        If set, the proration will be calculated as though the subscription was updated at the given time. This can be used to apply the same proration that was previewed with the [upcoming invoice](https://stripe.com/docs/api#retrieve_customer_invoice) endpoint.
        """
        quantity: NotRequired[int]
        """
        The quantity you'd like to apply to the subscription item you're creating.
        """
        tax_rates: NotRequired["Literal['']|List[str]"]
        """
        A list of [Tax Rate](https://stripe.com/docs/api/tax_rates) ids. These Tax Rates will override the [`default_tax_rates`](https://stripe.com/docs/api/subscriptions/create#create_subscription-default_tax_rates) on the Subscription. When updating, pass an empty string to remove previously-defined tax rates.
        """

    class UpdateParamsBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
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

    class UpdateParamsPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "SubscriptionItemService.UpdateParamsPriceDataRecurring"
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

    class UpdateParamsPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    def delete(
        self,
        item: str,
        params: "SubscriptionItemService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionItem:
        """
        Deletes an item from the subscription. Removing a subscription item from a subscription will not cancel the subscription.
        """
        return cast(
            SubscriptionItem,
            self._request(
                "delete",
                "/v1/subscription_items/{item}".format(item=sanitize_id(item)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        item: str,
        params: "SubscriptionItemService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionItem:
        """
        Deletes an item from the subscription. Removing a subscription item from a subscription will not cancel the subscription.
        """
        return cast(
            SubscriptionItem,
            await self._request_async(
                "delete",
                "/v1/subscription_items/{item}".format(item=sanitize_id(item)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        item: str,
        params: "SubscriptionItemService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionItem:
        """
        Retrieves the subscription item with the given ID.
        """
        return cast(
            SubscriptionItem,
            self._request(
                "get",
                "/v1/subscription_items/{item}".format(item=sanitize_id(item)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        item: str,
        params: "SubscriptionItemService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionItem:
        """
        Retrieves the subscription item with the given ID.
        """
        return cast(
            SubscriptionItem,
            await self._request_async(
                "get",
                "/v1/subscription_items/{item}".format(item=sanitize_id(item)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        item: str,
        params: "SubscriptionItemService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionItem:
        """
        Updates the plan or quantity of an item on a current subscription.
        """
        return cast(
            SubscriptionItem,
            self._request(
                "post",
                "/v1/subscription_items/{item}".format(item=sanitize_id(item)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        item: str,
        params: "SubscriptionItemService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> SubscriptionItem:
        """
        Updates the plan or quantity of an item on a current subscription.
        """
        return cast(
            SubscriptionItem,
            await self._request_async(
                "post",
                "/v1/subscription_items/{item}".format(item=sanitize_id(item)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "SubscriptionItemService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[SubscriptionItem]:
        """
        Returns a list of your subscription items for a given subscription.
        """
        return cast(
            ListObject[SubscriptionItem],
            self._request(
                "get",
                "/v1/subscription_items",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "SubscriptionItemService.ListParams",
        options: RequestOptions = {},
    ) -> ListObject[SubscriptionItem]:
        """
        Returns a list of your subscription items for a given subscription.
        """
        return cast(
            ListObject[SubscriptionItem],
            await self._request_async(
                "get",
                "/v1/subscription_items",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "SubscriptionItemService.CreateParams",
        options: RequestOptions = {},
    ) -> SubscriptionItem:
        """
        Adds a new item to an existing subscription. No existing items will be changed or replaced.
        """
        return cast(
            SubscriptionItem,
            self._request(
                "post",
                "/v1/subscription_items",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "SubscriptionItemService.CreateParams",
        options: RequestOptions = {},
    ) -> SubscriptionItem:
        """
        Adds a new item to an existing subscription. No existing items will be changed or replaced.
        """
        return cast(
            SubscriptionItem,
            await self._request_async(
                "post",
                "/v1/subscription_items",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
