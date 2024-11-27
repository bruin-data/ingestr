# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._usage_record import UsageRecord
from stripe._util import sanitize_id
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class SubscriptionItemUsageRecordService(StripeService):
    class CreateParams(TypedDict):
        action: NotRequired[Literal["increment", "set"]]
        """
        Valid values are `increment` (default) or `set`. When using `increment` the specified `quantity` will be added to the usage at the specified timestamp. The `set` action will overwrite the usage quantity at that timestamp. If the subscription has [billing thresholds](https://stripe.com/docs/api/subscriptions/object#subscription_object-billing_thresholds), `increment` is the only allowed value.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        quantity: int
        """
        The usage quantity for the specified timestamp.
        """
        timestamp: NotRequired["Literal['now']|int"]
        """
        The timestamp for the usage event. This timestamp must be within the current billing period of the subscription of the provided `subscription_item`, and must not be in the future. When passing `"now"`, Stripe records usage for the current time. Default is `"now"` if a value is not provided.
        """

    def create(
        self,
        subscription_item: str,
        params: "SubscriptionItemUsageRecordService.CreateParams",
        options: RequestOptions = {},
    ) -> UsageRecord:
        """
        Creates a usage record for a specified subscription item and date, and fills it with a quantity.

        Usage records provide quantity information that Stripe uses to track how much a customer is using your service. With usage information and the pricing model set up by the [metered billing](https://stripe.com/docs/billing/subscriptions/metered-billing) plan, Stripe helps you send accurate invoices to your customers.

        The default calculation for usage is to add up all the quantity values of the usage records within a billing period. You can change this default behavior with the billing plan's aggregate_usage [parameter](https://stripe.com/docs/api/plans/create#create_plan-aggregate_usage). When there is more than one usage record with the same timestamp, Stripe adds the quantity values together. In most cases, this is the desired resolution, however, you can change this behavior with the action parameter.

        The default pricing model for metered billing is [per-unit pricing. For finer granularity, you can configure metered billing to have a <a href="https://stripe.com/docs/billing/subscriptions/tiers">tiered pricing](https://stripe.com/docs/api/plans/object#plan_object-billing_scheme) model.
        """
        return cast(
            UsageRecord,
            self._request(
                "post",
                "/v1/subscription_items/{subscription_item}/usage_records".format(
                    subscription_item=sanitize_id(subscription_item),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        subscription_item: str,
        params: "SubscriptionItemUsageRecordService.CreateParams",
        options: RequestOptions = {},
    ) -> UsageRecord:
        """
        Creates a usage record for a specified subscription item and date, and fills it with a quantity.

        Usage records provide quantity information that Stripe uses to track how much a customer is using your service. With usage information and the pricing model set up by the [metered billing](https://stripe.com/docs/billing/subscriptions/metered-billing) plan, Stripe helps you send accurate invoices to your customers.

        The default calculation for usage is to add up all the quantity values of the usage records within a billing period. You can change this default behavior with the billing plan's aggregate_usage [parameter](https://stripe.com/docs/api/plans/create#create_plan-aggregate_usage). When there is more than one usage record with the same timestamp, Stripe adds the quantity values together. In most cases, this is the desired resolution, however, you can change this behavior with the action parameter.

        The default pricing model for metered billing is [per-unit pricing. For finer granularity, you can configure metered billing to have a <a href="https://stripe.com/docs/billing/subscriptions/tiers">tiered pricing](https://stripe.com/docs/api/plans/object#plan_object-billing_scheme) model.
        """
        return cast(
            UsageRecord,
            await self._request_async(
                "post",
                "/v1/subscription_items/{subscription_item}/usage_records".format(
                    subscription_item=sanitize_id(subscription_item),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
