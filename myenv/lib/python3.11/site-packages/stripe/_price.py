# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._search_result_object import SearchResultObject
from stripe._searchable_api_resource import SearchableAPIResource
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import sanitize_id
from typing import (
    AsyncIterator,
    ClassVar,
    Dict,
    Iterator,
    List,
    Optional,
    Union,
    cast,
)
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._product import Product


class Price(
    CreateableAPIResource["Price"],
    ListableAPIResource["Price"],
    SearchableAPIResource["Price"],
    UpdateableAPIResource["Price"],
):
    """
    Prices define the unit cost, currency, and (optional) billing cycle for both recurring and one-time purchases of products.
    [Products](https://stripe.com/docs/api#products) help you track inventory or provisioning, and prices help you track payment terms. Different physical goods or levels of service should be represented by products, and pricing options should be represented by prices. This approach lets you change prices without having to change your provisioning scheme.

    For example, you might have a single "gold" product that has prices for $10/month, $100/year, and €9 once.

    Related guides: [Set up a subscription](https://stripe.com/docs/billing/subscriptions/set-up-subscription), [create an invoice](https://stripe.com/docs/billing/invoices/create), and more about [products and prices](https://stripe.com/docs/products-prices/overview).
    """

    OBJECT_NAME: ClassVar[Literal["price"]] = "price"

    class CurrencyOptions(StripeObject):
        class CustomUnitAmount(StripeObject):
            maximum: Optional[int]
            """
            The maximum unit amount the customer can specify for this item.
            """
            minimum: Optional[int]
            """
            The minimum unit amount the customer can specify for this item. Must be at least the minimum charge amount.
            """
            preset: Optional[int]
            """
            The starting unit amount which can be updated by the customer.
            """

        class Tier(StripeObject):
            flat_amount: Optional[int]
            """
            Price for the entire tier.
            """
            flat_amount_decimal: Optional[str]
            """
            Same as `flat_amount`, but contains a decimal value with at most 12 decimal places.
            """
            unit_amount: Optional[int]
            """
            Per unit price for units relevant to the tier.
            """
            unit_amount_decimal: Optional[str]
            """
            Same as `unit_amount`, but contains a decimal value with at most 12 decimal places.
            """
            up_to: Optional[int]
            """
            Up to and including to this quantity will be contained in the tier.
            """

        custom_unit_amount: Optional[CustomUnitAmount]
        """
        When set, provides configuration for the amount to be adjusted by the customer during Checkout Sessions and Payment Links.
        """
        tax_behavior: Optional[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tiers: Optional[List[Tier]]
        """
        Each element represents a pricing tier. This parameter requires `billing_scheme` to be set to `tiered`. See also the documentation for `billing_scheme`.
        """
        unit_amount: Optional[int]
        """
        The unit amount in cents (or local equivalent) to be charged, represented as a whole integer if possible. Only set if `billing_scheme=per_unit`.
        """
        unit_amount_decimal: Optional[str]
        """
        The unit amount in cents (or local equivalent) to be charged, represented as a decimal string with at most 12 decimal places. Only set if `billing_scheme=per_unit`.
        """
        _inner_class_types = {
            "custom_unit_amount": CustomUnitAmount,
            "tiers": Tier,
        }

    class CustomUnitAmount(StripeObject):
        maximum: Optional[int]
        """
        The maximum unit amount the customer can specify for this item.
        """
        minimum: Optional[int]
        """
        The minimum unit amount the customer can specify for this item. Must be at least the minimum charge amount.
        """
        preset: Optional[int]
        """
        The starting unit amount which can be updated by the customer.
        """

    class Recurring(StripeObject):
        aggregate_usage: Optional[
            Literal["last_during_period", "last_ever", "max", "sum"]
        ]
        """
        Specifies a usage aggregation strategy for prices of `usage_type=metered`. Defaults to `sum`.
        """
        interval: Literal["day", "month", "week", "year"]
        """
        The frequency at which a subscription is billed. One of `day`, `week`, `month` or `year`.
        """
        interval_count: int
        """
        The number of intervals (specified in the `interval` attribute) between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months.
        """
        meter: Optional[str]
        """
        The meter tracking the usage of a metered price
        """
        trial_period_days: Optional[int]
        """
        Default number of trial days when subscribing a customer to this price using [`trial_from_plan=true`](https://stripe.com/docs/api#create_subscription-trial_from_plan).
        """
        usage_type: Literal["licensed", "metered"]
        """
        Configures how the quantity per period should be determined. Can be either `metered` or `licensed`. `licensed` automatically bills the `quantity` set when adding it to a subscription. `metered` aggregates the total usage based on usage records. Defaults to `licensed`.
        """

    class Tier(StripeObject):
        flat_amount: Optional[int]
        """
        Price for the entire tier.
        """
        flat_amount_decimal: Optional[str]
        """
        Same as `flat_amount`, but contains a decimal value with at most 12 decimal places.
        """
        unit_amount: Optional[int]
        """
        Per unit price for units relevant to the tier.
        """
        unit_amount_decimal: Optional[str]
        """
        Same as `unit_amount`, but contains a decimal value with at most 12 decimal places.
        """
        up_to: Optional[int]
        """
        Up to and including to this quantity will be contained in the tier.
        """

    class TransformQuantity(StripeObject):
        divide_by: int
        """
        Divide usage by this number.
        """
        round: Literal["down", "up"]
        """
        After division, either round the result `up` or `down`.
        """

    class CreateParams(RequestOptions):
        active: NotRequired[bool]
        """
        Whether the price can be used for new purchases. Defaults to `true`.
        """
        billing_scheme: NotRequired[Literal["per_unit", "tiered"]]
        """
        Describes how to compute the price per period. Either `per_unit` or `tiered`. `per_unit` indicates that the fixed amount (specified in `unit_amount` or `unit_amount_decimal`) will be charged per unit in `quantity` (for prices with `usage_type=licensed`), or per unit of total usage (for prices with `usage_type=metered`). `tiered` indicates that the unit pricing will be computed using a tiering strategy as defined using the `tiers` and `tiers_mode` attributes.
        """
        currency: str
        """
        Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
        """
        currency_options: NotRequired[
            Dict[str, "Price.CreateParamsCurrencyOptions"]
        ]
        """
        Prices defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """
        custom_unit_amount: NotRequired["Price.CreateParamsCustomUnitAmount"]
        """
        When set, provides configuration for the amount to be adjusted by the customer during Checkout Sessions and Payment Links.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        lookup_key: NotRequired[str]
        """
        A lookup key used to retrieve prices dynamically from a static string. This may be up to 200 characters.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        nickname: NotRequired[str]
        """
        A brief description of the price, hidden from customers.
        """
        product: NotRequired[str]
        """
        The ID of the product that this price will belong to.
        """
        product_data: NotRequired["Price.CreateParamsProductData"]
        """
        These fields can be used to create a new product that this price will belong to.
        """
        recurring: NotRequired["Price.CreateParamsRecurring"]
        """
        The recurring components of a price such as `interval` and `usage_type`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tiers: NotRequired[List["Price.CreateParamsTier"]]
        """
        Each element represents a pricing tier. This parameter requires `billing_scheme` to be set to `tiered`. See also the documentation for `billing_scheme`.
        """
        tiers_mode: NotRequired[Literal["graduated", "volume"]]
        """
        Defines if the tiering price should be `graduated` or `volume` based. In `volume`-based tiering, the maximum quantity within a period determines the per unit price, in `graduated` tiering pricing can successively change as the quantity grows.
        """
        transfer_lookup_key: NotRequired[bool]
        """
        If set to true, will atomically remove the lookup key from the existing price, and assign it to this price.
        """
        transform_quantity: NotRequired["Price.CreateParamsTransformQuantity"]
        """
        Apply a transformation to the reported usage or set quantity before computing the billed price. Cannot be combined with `tiers`.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge. One of `unit_amount`, `unit_amount_decimal`, or `custom_unit_amount` is required, unless `billing_scheme=tiered`.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreateParamsCurrencyOptions(TypedDict):
        custom_unit_amount: NotRequired[
            "Price.CreateParamsCurrencyOptionsCustomUnitAmount"
        ]
        """
        When set, provides configuration for the amount to be adjusted by the customer during Checkout Sessions and Payment Links.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tiers: NotRequired[List["Price.CreateParamsCurrencyOptionsTier"]]
        """
        Each element represents a pricing tier. This parameter requires `billing_scheme` to be set to `tiered`. See also the documentation for `billing_scheme`.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class CreateParamsCurrencyOptionsCustomUnitAmount(TypedDict):
        enabled: bool
        """
        Pass in `true` to enable `custom_unit_amount`, otherwise omit `custom_unit_amount`.
        """
        maximum: NotRequired[int]
        """
        The maximum unit amount the customer can specify for this item.
        """
        minimum: NotRequired[int]
        """
        The minimum unit amount the customer can specify for this item. Must be at least the minimum charge amount.
        """
        preset: NotRequired[int]
        """
        The starting unit amount which can be updated by the customer.
        """

    class CreateParamsCurrencyOptionsTier(TypedDict):
        flat_amount: NotRequired[int]
        """
        The flat billing amount for an entire tier, regardless of the number of units in the tier.
        """
        flat_amount_decimal: NotRequired[str]
        """
        Same as `flat_amount`, but accepts a decimal value representing an integer in the minor units of the currency. Only one of `flat_amount` and `flat_amount_decimal` can be set.
        """
        unit_amount: NotRequired[int]
        """
        The per unit billing amount for each individual unit for which this tier applies.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """
        up_to: Union[Literal["inf"], int]
        """
        Specifies the upper bound of this tier. The lower bound of a tier is the upper bound of the previous tier adding one. Use `inf` to define a fallback tier.
        """

    class CreateParamsCustomUnitAmount(TypedDict):
        enabled: bool
        """
        Pass in `true` to enable `custom_unit_amount`, otherwise omit `custom_unit_amount`.
        """
        maximum: NotRequired[int]
        """
        The maximum unit amount the customer can specify for this item.
        """
        minimum: NotRequired[int]
        """
        The minimum unit amount the customer can specify for this item. Must be at least the minimum charge amount.
        """
        preset: NotRequired[int]
        """
        The starting unit amount which can be updated by the customer.
        """

    class CreateParamsProductData(TypedDict):
        active: NotRequired[bool]
        """
        Whether the product is currently available for purchase. Defaults to `true`.
        """
        id: NotRequired[str]
        """
        The identifier for the product. Must be unique. If not provided, an identifier will be randomly generated.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: str
        """
        The product's name, meant to be displayable to the customer.
        """
        statement_descriptor: NotRequired[str]
        """
        An arbitrary string to be displayed on your customer's credit card or bank statement. While most banks display this information consistently, some may display it incorrectly or not at all.

        This may be up to 22 characters. The statement description may not include `<`, `>`, `\\`, `"`, `'` characters, and will appear on your customer's statement in capital letters. Non-ASCII characters are automatically stripped.
        """
        tax_code: NotRequired[str]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """
        unit_label: NotRequired[str]
        """
        A label that represents units of this product. When set, this will be included in customers' receipts, invoices, Checkout, and the customer portal.
        """

    class CreateParamsRecurring(TypedDict):
        aggregate_usage: NotRequired[
            Literal["last_during_period", "last_ever", "max", "sum"]
        ]
        """
        Specifies a usage aggregation strategy for prices of `usage_type=metered`. Defaults to `sum`.
        """
        interval: Literal["day", "month", "week", "year"]
        """
        Specifies billing frequency. Either `day`, `week`, `month` or `year`.
        """
        interval_count: NotRequired[int]
        """
        The number of intervals between subscription billings. For example, `interval=month` and `interval_count=3` bills every 3 months. Maximum of three years interval allowed (3 years, 36 months, or 156 weeks).
        """
        meter: NotRequired[str]
        """
        The meter tracking the usage of a metered price
        """
        trial_period_days: NotRequired[int]
        """
        Default number of trial days when subscribing a customer to this price using [`trial_from_plan=true`](https://stripe.com/docs/api#create_subscription-trial_from_plan).
        """
        usage_type: NotRequired[Literal["licensed", "metered"]]
        """
        Configures how the quantity per period should be determined. Can be either `metered` or `licensed`. `licensed` automatically bills the `quantity` set when adding it to a subscription. `metered` aggregates the total usage based on usage records. Defaults to `licensed`.
        """

    class CreateParamsTier(TypedDict):
        flat_amount: NotRequired[int]
        """
        The flat billing amount for an entire tier, regardless of the number of units in the tier.
        """
        flat_amount_decimal: NotRequired[str]
        """
        Same as `flat_amount`, but accepts a decimal value representing an integer in the minor units of the currency. Only one of `flat_amount` and `flat_amount_decimal` can be set.
        """
        unit_amount: NotRequired[int]
        """
        The per unit billing amount for each individual unit for which this tier applies.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """
        up_to: Union[Literal["inf"], int]
        """
        Specifies the upper bound of this tier. The lower bound of a tier is the upper bound of the previous tier adding one. Use `inf` to define a fallback tier.
        """

    class CreateParamsTransformQuantity(TypedDict):
        divide_by: int
        """
        Divide usage by this number.
        """
        round: Literal["down", "up"]
        """
        After division, either round the result `up` or `down`.
        """

    class ListParams(RequestOptions):
        active: NotRequired[bool]
        """
        Only return prices that are active or inactive (e.g., pass `false` to list all inactive prices).
        """
        created: NotRequired["Price.ListParamsCreated|int"]
        """
        A filter on the list, based on the object `created` field. The value can be a string with an integer Unix timestamp, or it can be a dictionary with a number of different query options.
        """
        currency: NotRequired[str]
        """
        Only return prices for the given currency.
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
        lookup_keys: NotRequired[List[str]]
        """
        Only return the price with these lookup_keys, if any exist. You can specify up to 10 lookup_keys.
        """
        product: NotRequired[str]
        """
        Only return prices for the given product.
        """
        recurring: NotRequired["Price.ListParamsRecurring"]
        """
        Only return prices with these recurring fields.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        type: NotRequired[Literal["one_time", "recurring"]]
        """
        Only return prices of type `recurring` or `one_time`.
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

    class ListParamsRecurring(TypedDict):
        interval: NotRequired[Literal["day", "month", "week", "year"]]
        """
        Filter by billing frequency. Either `day`, `week`, `month` or `year`.
        """
        meter: NotRequired[str]
        """
        Filter by the price's meter.
        """
        usage_type: NotRequired[Literal["licensed", "metered"]]
        """
        Filter by the usage type for this price. Can be either `metered` or `licensed`.
        """

    class ModifyParams(RequestOptions):
        active: NotRequired[bool]
        """
        Whether the price can be used for new purchases. Defaults to `true`.
        """
        currency_options: NotRequired[
            "Literal['']|Dict[str, Price.ModifyParamsCurrencyOptions]"
        ]
        """
        Prices defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        lookup_key: NotRequired[str]
        """
        A lookup key used to retrieve prices dynamically from a static string. This may be up to 200 characters.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        nickname: NotRequired[str]
        """
        A brief description of the price, hidden from customers.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        transfer_lookup_key: NotRequired[bool]
        """
        If set to true, will atomically remove the lookup key from the existing price, and assign it to this price.
        """

    class ModifyParamsCurrencyOptions(TypedDict):
        custom_unit_amount: NotRequired[
            "Price.ModifyParamsCurrencyOptionsCustomUnitAmount"
        ]
        """
        When set, provides configuration for the amount to be adjusted by the customer during Checkout Sessions and Payment Links.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tiers: NotRequired[List["Price.ModifyParamsCurrencyOptionsTier"]]
        """
        Each element represents a pricing tier. This parameter requires `billing_scheme` to be set to `tiered`. See also the documentation for `billing_scheme`.
        """
        unit_amount: NotRequired[int]
        """
        A positive integer in cents (or local equivalent) (or 0 for a free price) representing how much to charge.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """

    class ModifyParamsCurrencyOptionsCustomUnitAmount(TypedDict):
        enabled: bool
        """
        Pass in `true` to enable `custom_unit_amount`, otherwise omit `custom_unit_amount`.
        """
        maximum: NotRequired[int]
        """
        The maximum unit amount the customer can specify for this item.
        """
        minimum: NotRequired[int]
        """
        The minimum unit amount the customer can specify for this item. Must be at least the minimum charge amount.
        """
        preset: NotRequired[int]
        """
        The starting unit amount which can be updated by the customer.
        """

    class ModifyParamsCurrencyOptionsTier(TypedDict):
        flat_amount: NotRequired[int]
        """
        The flat billing amount for an entire tier, regardless of the number of units in the tier.
        """
        flat_amount_decimal: NotRequired[str]
        """
        Same as `flat_amount`, but accepts a decimal value representing an integer in the minor units of the currency. Only one of `flat_amount` and `flat_amount_decimal` can be set.
        """
        unit_amount: NotRequired[int]
        """
        The per unit billing amount for each individual unit for which this tier applies.
        """
        unit_amount_decimal: NotRequired[str]
        """
        Same as `unit_amount`, but accepts a decimal value in cents (or local equivalent) with at most 12 decimal places. Only one of `unit_amount` and `unit_amount_decimal` can be set.
        """
        up_to: Union[Literal["inf"], int]
        """
        Specifies the upper bound of this tier. The lower bound of a tier is the upper bound of the previous tier adding one. Use `inf` to define a fallback tier.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class SearchParams(RequestOptions):
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
        The search query string. See [search query language](https://stripe.com/docs/search#search-query-language) and the list of supported [query fields for prices](https://stripe.com/docs/search#query-fields-for-prices).
        """

    active: bool
    """
    Whether the price can be used for new purchases.
    """
    billing_scheme: Literal["per_unit", "tiered"]
    """
    Describes how to compute the price per period. Either `per_unit` or `tiered`. `per_unit` indicates that the fixed amount (specified in `unit_amount` or `unit_amount_decimal`) will be charged per unit in `quantity` (for prices with `usage_type=licensed`), or per unit of total usage (for prices with `usage_type=metered`). `tiered` indicates that the unit pricing will be computed using a tiering strategy as defined using the `tiers` and `tiers_mode` attributes.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    currency: str
    """
    Three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html), in lowercase. Must be a [supported currency](https://stripe.com/docs/currencies).
    """
    currency_options: Optional[Dict[str, CurrencyOptions]]
    """
    Prices defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
    """
    custom_unit_amount: Optional[CustomUnitAmount]
    """
    When set, provides configuration for the amount to be adjusted by the customer during Checkout Sessions and Payment Links.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    lookup_key: Optional[str]
    """
    A lookup key used to retrieve prices dynamically from a static string. This may be up to 200 characters.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    nickname: Optional[str]
    """
    A brief description of the price, hidden from customers.
    """
    object: Literal["price"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    product: ExpandableField["Product"]
    """
    The ID of the product this price is associated with.
    """
    recurring: Optional[Recurring]
    """
    The recurring components of a price such as `interval` and `usage_type`.
    """
    tax_behavior: Optional[Literal["exclusive", "inclusive", "unspecified"]]
    """
    Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
    """
    tiers: Optional[List[Tier]]
    """
    Each element represents a pricing tier. This parameter requires `billing_scheme` to be set to `tiered`. See also the documentation for `billing_scheme`.
    """
    tiers_mode: Optional[Literal["graduated", "volume"]]
    """
    Defines if the tiering price should be `graduated` or `volume` based. In `volume`-based tiering, the maximum quantity within a period determines the per unit price. In `graduated` tiering, pricing can change as the quantity grows.
    """
    transform_quantity: Optional[TransformQuantity]
    """
    Apply a transformation to the reported usage or set quantity before computing the amount billed. Cannot be combined with `tiers`.
    """
    type: Literal["one_time", "recurring"]
    """
    One of `one_time` or `recurring` depending on whether the price is for a one-time purchase or a recurring (subscription) purchase.
    """
    unit_amount: Optional[int]
    """
    The unit amount in cents (or local equivalent) to be charged, represented as a whole integer if possible. Only set if `billing_scheme=per_unit`.
    """
    unit_amount_decimal: Optional[str]
    """
    The unit amount in cents (or local equivalent) to be charged, represented as a decimal string with at most 12 decimal places. Only set if `billing_scheme=per_unit`.
    """
    deleted: Optional[Literal[True]]
    """
    Always true for a deleted object
    """

    @classmethod
    def create(cls, **params: Unpack["Price.CreateParams"]) -> "Price":
        """
        Creates a new price for an existing product. The price can be recurring or one-time.
        """
        return cast(
            "Price",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Price.CreateParams"]
    ) -> "Price":
        """
        Creates a new price for an existing product. The price can be recurring or one-time.
        """
        return cast(
            "Price",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(cls, **params: Unpack["Price.ListParams"]) -> ListObject["Price"]:
        """
        Returns a list of your active prices, excluding [inline prices](https://stripe.com/docs/products-prices/pricing-models#inline-pricing). For the list of inactive prices, set active to false.
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
        cls, **params: Unpack["Price.ListParams"]
    ) -> ListObject["Price"]:
        """
        Returns a list of your active prices, excluding [inline prices](https://stripe.com/docs/products-prices/pricing-models#inline-pricing). For the list of inactive prices, set active to false.
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
        cls, id: str, **params: Unpack["Price.ModifyParams"]
    ) -> "Price":
        """
        Updates the specified price by setting the values of the parameters passed. Any parameters not provided are left unchanged.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Price",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Price.ModifyParams"]
    ) -> "Price":
        """
        Updates the specified price by setting the values of the parameters passed. Any parameters not provided are left unchanged.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Price",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Price.RetrieveParams"]
    ) -> "Price":
        """
        Retrieves the price with the given ID.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Price.RetrieveParams"]
    ) -> "Price":
        """
        Retrieves the price with the given ID.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def search(
        cls, *args, **kwargs: Unpack["Price.SearchParams"]
    ) -> SearchResultObject["Price"]:
        """
        Search for prices you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cls._search(search_url="/v1/prices/search", *args, **kwargs)

    @classmethod
    async def search_async(
        cls, *args, **kwargs: Unpack["Price.SearchParams"]
    ) -> SearchResultObject["Price"]:
        """
        Search for prices you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return await cls._search_async(
            search_url="/v1/prices/search", *args, **kwargs
        )

    @classmethod
    def search_auto_paging_iter(
        cls, *args, **kwargs: Unpack["Price.SearchParams"]
    ) -> Iterator["Price"]:
        return cls.search(*args, **kwargs).auto_paging_iter()

    @classmethod
    async def search_auto_paging_iter_async(
        cls, *args, **kwargs: Unpack["Price.SearchParams"]
    ) -> AsyncIterator["Price"]:
        return (await cls.search_async(*args, **kwargs)).auto_paging_iter()

    _inner_class_types = {
        "currency_options": CurrencyOptions,
        "custom_unit_amount": CustomUnitAmount,
        "recurring": Recurring,
        "tiers": Tier,
        "transform_quantity": TransformQuantity,
    }
