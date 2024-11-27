# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._deletable_api_resource import DeletableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._nested_resource_class_methods import nested_resource_class_methods
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, Dict, List, Optional, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._discount import Discount
    from stripe._plan import Plan
    from stripe._price import Price
    from stripe._tax_rate import TaxRate
    from stripe._usage_record import UsageRecord
    from stripe._usage_record_summary import UsageRecordSummary


@nested_resource_class_methods("usage_record")
@nested_resource_class_methods("usage_record_summary")
class SubscriptionItem(
    CreateableAPIResource["SubscriptionItem"],
    DeletableAPIResource["SubscriptionItem"],
    ListableAPIResource["SubscriptionItem"],
    UpdateableAPIResource["SubscriptionItem"],
):
    """
    Subscription items allow you to create customer subscriptions with more than
    one plan, making it easy to represent complex billing relationships.
    """

    OBJECT_NAME: ClassVar[Literal["subscription_item"]] = "subscription_item"

    class BillingThresholds(StripeObject):
        usage_gte: Optional[int]
        """
        Usage threshold that triggers the subscription to create an invoice
        """

    class CreateParams(RequestOptions):
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionItem.CreateParamsBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[SubscriptionItem.CreateParamsDiscount]"
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
        price_data: NotRequired["SubscriptionItem.CreateParamsPriceData"]
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
        recurring: "SubscriptionItem.CreateParamsPriceDataRecurring"
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

    class CreateUsageRecordParams(RequestOptions):
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

    class DeleteParams(RequestOptions):
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

    class ListParams(RequestOptions):
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

    class ListUsageRecordSummariesParams(RequestOptions):
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

    class ModifyParams(RequestOptions):
        billing_thresholds: NotRequired[
            "Literal['']|SubscriptionItem.ModifyParamsBillingThresholds"
        ]
        """
        Define thresholds at which an invoice will be sent, and the subscription advanced to a new billing period. When updating, pass an empty string to remove previously-defined thresholds.
        """
        discounts: NotRequired[
            "Literal['']|List[SubscriptionItem.ModifyParamsDiscount]"
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
        price_data: NotRequired["SubscriptionItem.ModifyParamsPriceData"]
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

    class ModifyParamsBillingThresholds(TypedDict):
        usage_gte: int
        """
        Number of units that meets the billing threshold to advance the subscription to a new billing period (e.g., it takes 10 $5 units to meet a $50 [monetary threshold](https://stripe.com/docs/api/subscriptions/update#update_subscription-billing_thresholds-amount_gte))
        """

    class ModifyParamsDiscount(TypedDict):
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

    class ModifyParamsPriceData(TypedDict):
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        product: str
        """
        The ID of the product that this price will belong to.
        """
        recurring: "SubscriptionItem.ModifyParamsPriceDataRecurring"
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

    class ModifyParamsPriceDataRecurring(TypedDict):
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    billing_thresholds: Optional[BillingThresholds]
    """
    Define thresholds at which an invoice will be sent, and the related subscription advanced to a new billing period
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    discounts: List[ExpandableField["Discount"]]
    """
    The discounts applied to the subscription item. Subscription item discounts are applied before subscription discounts. Use `expand[]=discounts` to expand each discount.
    """
    id: str
    """
    Unique identifier for the object.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    object: Literal["subscription_item"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    plan: "Plan"
    """
    You can now model subscriptions more flexibly using the [Prices API](https://stripe.com/docs/api#prices). It replaces the Plans API and is backwards compatible to simplify your migration.

    Plans define the base price, currency, and billing cycle for recurring purchases of products.
    [Products](https://stripe.com/docs/api#products) help you track inventory or provisioning, and plans help you track pricing. Different physical goods or levels of service should be represented by products, and pricing options should be represented by plans. This approach lets you change prices without having to change your provisioning scheme.

    For example, you might have a single "gold" product that has plans for $10/month, $100/year, €9/month, and €90/year.

    Related guides: [Set up a subscription](https://stripe.com/docs/billing/subscriptions/set-up-subscription) and more about [products and prices](https://stripe.com/docs/products-prices/overview).
    """
    price: "Price"
    """
    Prices define the unit cost, currency, and (optional) billing cycle for both recurring and one-time purchases of products.
    [Products](https://stripe.com/docs/api#products) help you track inventory or provisioning, and prices help you track payment terms. Different physical goods or levels of service should be represented by products, and pricing options should be represented by prices. This approach lets you change prices without having to change your provisioning scheme.

    For example, you might have a single "gold" product that has prices for $10/month, $100/year, and €9 once.

    Related guides: [Set up a subscription](https://stripe.com/docs/billing/subscriptions/set-up-subscription), [create an invoice](https://stripe.com/docs/billing/invoices/create), and more about [products and prices](https://stripe.com/docs/products-prices/overview).
    """
    quantity: Optional[int]
    """
    The [quantity](https://stripe.com/docs/subscriptions/quantities) of the plan to which the customer should be subscribed.
    """
    subscription: str
    """
    The `subscription` this `subscription_item` belongs to.
    """
    tax_rates: Optional[List["TaxRate"]]
    """
    The tax rates which apply to this `subscription_item`. When set, the `default_tax_rates` on the subscription do not apply to this `subscription_item`.
    """
    deleted: Optional[Literal[True]]
    """
    Always true for a deleted object
    """

    @classmethod
    def create(
        cls, **params: Unpack["SubscriptionItem.CreateParams"]
    ) -> "SubscriptionItem":
        """
        Adds a new item to an existing subscription. No existing items will be changed or replaced.
        """
        return cast(
            "SubscriptionItem",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["SubscriptionItem.CreateParams"]
    ) -> "SubscriptionItem":
        """
        Adds a new item to an existing subscription. No existing items will be changed or replaced.
        """
        return cast(
            "SubscriptionItem",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_delete(
        cls, sid: str, **params: Unpack["SubscriptionItem.DeleteParams"]
    ) -> "SubscriptionItem":
        """
        Deletes an item from the subscription. Removing a subscription item from a subscription will not cancel the subscription.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "SubscriptionItem",
            cls._static_request(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    def delete(
        sid: str, **params: Unpack["SubscriptionItem.DeleteParams"]
    ) -> "SubscriptionItem":
        """
        Deletes an item from the subscription. Removing a subscription item from a subscription will not cancel the subscription.
        """
        ...

    @overload
    def delete(
        self, **params: Unpack["SubscriptionItem.DeleteParams"]
    ) -> "SubscriptionItem":
        """
        Deletes an item from the subscription. Removing a subscription item from a subscription will not cancel the subscription.
        """
        ...

    @class_method_variant("_cls_delete")
    def delete(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["SubscriptionItem.DeleteParams"]
    ) -> "SubscriptionItem":
        """
        Deletes an item from the subscription. Removing a subscription item from a subscription will not cancel the subscription.
        """
        return self._request_and_refresh(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    async def _cls_delete_async(
        cls, sid: str, **params: Unpack["SubscriptionItem.DeleteParams"]
    ) -> "SubscriptionItem":
        """
        Deletes an item from the subscription. Removing a subscription item from a subscription will not cancel the subscription.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "SubscriptionItem",
            await cls._static_request_async(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def delete_async(
        sid: str, **params: Unpack["SubscriptionItem.DeleteParams"]
    ) -> "SubscriptionItem":
        """
        Deletes an item from the subscription. Removing a subscription item from a subscription will not cancel the subscription.
        """
        ...

    @overload
    async def delete_async(
        self, **params: Unpack["SubscriptionItem.DeleteParams"]
    ) -> "SubscriptionItem":
        """
        Deletes an item from the subscription. Removing a subscription item from a subscription will not cancel the subscription.
        """
        ...

    @class_method_variant("_cls_delete_async")
    async def delete_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["SubscriptionItem.DeleteParams"]
    ) -> "SubscriptionItem":
        """
        Deletes an item from the subscription. Removing a subscription item from a subscription will not cancel the subscription.
        """
        return await self._request_and_refresh_async(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    def list(
        cls, **params: Unpack["SubscriptionItem.ListParams"]
    ) -> ListObject["SubscriptionItem"]:
        """
        Returns a list of your subscription items for a given subscription.
        """
        result = cls._static_request(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    async def list_async(
        cls, **params: Unpack["SubscriptionItem.ListParams"]
    ) -> ListObject["SubscriptionItem"]:
        """
        Returns a list of your subscription items for a given subscription.
        """
        result = await cls._static_request_async(
            "get",
            cls.class_url(),
            params=params,
        )
        if not isinstance(result, ListObject):
            raise TypeError(
                "Expected list object from API, got %s"
                % (type(result).__name__)
            )

        return result

    @classmethod
    def modify(
        cls, id: str, **params: Unpack["SubscriptionItem.ModifyParams"]
    ) -> "SubscriptionItem":
        """
        Updates the plan or quantity of an item on a current subscription.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "SubscriptionItem",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["SubscriptionItem.ModifyParams"]
    ) -> "SubscriptionItem":
        """
        Updates the plan or quantity of an item on a current subscription.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "SubscriptionItem",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["SubscriptionItem.RetrieveParams"]
    ) -> "SubscriptionItem":
        """
        Retrieves the subscription item with the given ID.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["SubscriptionItem.RetrieveParams"]
    ) -> "SubscriptionItem":
        """
        Retrieves the subscription item with the given ID.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def create_usage_record(
        cls,
        subscription_item: str,
        **params: Unpack["SubscriptionItem.CreateUsageRecordParams"],
    ) -> "UsageRecord":
        """
        Creates a usage record for a specified subscription item and date, and fills it with a quantity.

        Usage records provide quantity information that Stripe uses to track how much a customer is using your service. With usage information and the pricing model set up by the [metered billing](https://stripe.com/docs/billing/subscriptions/metered-billing) plan, Stripe helps you send accurate invoices to your customers.

        The default calculation for usage is to add up all the quantity values of the usage records within a billing period. You can change this default behavior with the billing plan's aggregate_usage [parameter](https://stripe.com/docs/api/plans/create#create_plan-aggregate_usage). When there is more than one usage record with the same timestamp, Stripe adds the quantity values together. In most cases, this is the desired resolution, however, you can change this behavior with the action parameter.

        The default pricing model for metered billing is [per-unit pricing. For finer granularity, you can configure metered billing to have a <a href="https://stripe.com/docs/billing/subscriptions/tiers">tiered pricing](https://stripe.com/docs/api/plans/object#plan_object-billing_scheme) model.
        """
        return cast(
            "UsageRecord",
            cls._static_request(
                "post",
                "/v1/subscription_items/{subscription_item}/usage_records".format(
                    subscription_item=sanitize_id(subscription_item)
                ),
                params=params,
            ),
        )

    @classmethod
    async def create_usage_record_async(
        cls,
        subscription_item: str,
        **params: Unpack["SubscriptionItem.CreateUsageRecordParams"],
    ) -> "UsageRecord":
        """
        Creates a usage record for a specified subscription item and date, and fills it with a quantity.

        Usage records provide quantity information that Stripe uses to track how much a customer is using your service. With usage information and the pricing model set up by the [metered billing](https://stripe.com/docs/billing/subscriptions/metered-billing) plan, Stripe helps you send accurate invoices to your customers.

        The default calculation for usage is to add up all the quantity values of the usage records within a billing period. You can change this default behavior with the billing plan's aggregate_usage [parameter](https://stripe.com/docs/api/plans/create#create_plan-aggregate_usage). When there is more than one usage record with the same timestamp, Stripe adds the quantity values together. In most cases, this is the desired resolution, however, you can change this behavior with the action parameter.

        The default pricing model for metered billing is [per-unit pricing. For finer granularity, you can configure metered billing to have a <a href="https://stripe.com/docs/billing/subscriptions/tiers">tiered pricing](https://stripe.com/docs/api/plans/object#plan_object-billing_scheme) model.
        """
        return cast(
            "UsageRecord",
            await cls._static_request_async(
                "post",
                "/v1/subscription_items/{subscription_item}/usage_records".format(
                    subscription_item=sanitize_id(subscription_item)
                ),
                params=params,
            ),
        )

    @classmethod
    def list_usage_record_summaries(
        cls,
        subscription_item: str,
        **params: Unpack["SubscriptionItem.ListUsageRecordSummariesParams"],
    ) -> ListObject["UsageRecordSummary"]:
        """
        For the specified subscription item, returns a list of summary objects. Each object in the list provides usage information that's been summarized from multiple usage records and over a subscription billing period (e.g., 15 usage records in the month of September).

        The list is sorted in reverse-chronological order (newest first). The first list item represents the most current usage period that hasn't ended yet. Since new usage records can still be added, the returned summary information for the subscription item's ID should be seen as unstable until the subscription billing period ends.
        """
        return cast(
            ListObject["UsageRecordSummary"],
            cls._static_request(
                "get",
                "/v1/subscription_items/{subscription_item}/usage_record_summaries".format(
                    subscription_item=sanitize_id(subscription_item)
                ),
                params=params,
            ),
        )

    @classmethod
    async def list_usage_record_summaries_async(
        cls,
        subscription_item: str,
        **params: Unpack["SubscriptionItem.ListUsageRecordSummariesParams"],
    ) -> ListObject["UsageRecordSummary"]:
        """
        For the specified subscription item, returns a list of summary objects. Each object in the list provides usage information that's been summarized from multiple usage records and over a subscription billing period (e.g., 15 usage records in the month of September).

        The list is sorted in reverse-chronological order (newest first). The first list item represents the most current usage period that hasn't ended yet. Since new usage records can still be added, the returned summary information for the subscription item's ID should be seen as unstable until the subscription billing period ends.
        """
        return cast(
            ListObject["UsageRecordSummary"],
            await cls._static_request_async(
                "get",
                "/v1/subscription_items/{subscription_item}/usage_record_summaries".format(
                    subscription_item=sanitize_id(subscription_item)
                ),
                params=params,
            ),
        )

    _inner_class_types = {"billing_thresholds": BillingThresholds}
