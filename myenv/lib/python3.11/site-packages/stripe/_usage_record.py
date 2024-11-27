# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from typing import ClassVar
from typing_extensions import Literal


class UsageRecord(CreateableAPIResource["UsageRecord"]):
    """
    Usage records allow you to report customer usage and metrics to Stripe for
    metered billing of subscription prices.

    Related guide: [Metered billing](https://stripe.com/docs/billing/subscriptions/metered-billing)

    This is our legacy usage-based billing API. See the [updated usage-based billing docs](https://docs.stripe.com/billing/subscriptions/usage-based).
    """

    OBJECT_NAME: ClassVar[Literal["usage_record"]] = "usage_record"
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["usage_record"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    quantity: int
    """
    The usage quantity for the specified date.
    """
    subscription_item: str
    """
    The ID of the subscription item this usage record contains data for.
    """
    timestamp: int
    """
    The timestamp when this usage occurred.
    """

    @classmethod
    def create(cls, **params):
        if "subscription_item" not in params:
            raise ValueError("Params must have a subscription_item key")

        subscription_item = params.pop("subscription_item")

        url = "/v1/subscription_items/%s/usage_records" % subscription_item
        return cls._static_request(
            "post",
            url,
            params=params,
            base_address="api",
            api_mode="V1",
        )
