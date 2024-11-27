# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._price import Price
from stripe._request_options import RequestOptions
from stripe._search_result_object import SearchResultObject
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from typing import Dict, List, Union, cast
from typing_extensions import Literal, NotRequired, TypedDict


class PriceService(StripeService):
    class CreateParams(TypedDict):
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
            Dict[str, "PriceService.CreateParamsCurrencyOptions"]
        ]
        """
        Prices defined in each available currency option. Each key must be a three-letter [ISO currency code](https://www.iso.org/iso-4217-currency-codes.html) and a [supported currency](https://stripe.com/docs/currencies).
        """
        custom_unit_amount: NotRequired[
            "PriceService.CreateParamsCustomUnitAmount"
        ]
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
        product_data: NotRequired["PriceService.CreateParamsProductData"]
        """
        These fields can be used to create a new product that this price will belong to.
        """
        recurring: NotRequired["PriceService.CreateParamsRecurring"]
        """
        The recurring components of a price such as `interval` and `usage_type`.
        """
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "unspecified"]
        ]
        """
        Only required if a [default tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#setting-a-default-tax-behavior-(recommended)) was not provided in the Stripe Tax settings. Specifies whether the price is considered inclusive of taxes or exclusive of taxes. One of `inclusive`, `exclusive`, or `unspecified`. Once specified as either `inclusive` or `exclusive`, it cannot be changed.
        """
        tiers: NotRequired[List["PriceService.CreateParamsTier"]]
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
        transform_quantity: NotRequired[
            "PriceService.CreateParamsTransformQuantity"
        ]
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
            "PriceService.CreateParamsCurrencyOptionsCustomUnitAmount"
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
        tiers: NotRequired[
            List["PriceService.CreateParamsCurrencyOptionsTier"]
        ]
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

    class ListParams(TypedDict):
        active: NotRequired[bool]
        """
        Only return prices that are active or inactive (e.g., pass `false` to list all inactive prices).
        """
        created: NotRequired["PriceService.ListParamsCreated|int"]
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
        recurring: NotRequired["PriceService.ListParamsRecurring"]
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
        The search query string. See [search query language](https://stripe.com/docs/search#search-query-language) and the list of supported [query fields for prices](https://stripe.com/docs/search#query-fields-for-prices).
        """

    class UpdateParams(TypedDict):
        active: NotRequired[bool]
        """
        Whether the price can be used for new purchases. Defaults to `true`.
        """
        currency_options: NotRequired[
            "Literal['']|Dict[str, PriceService.UpdateParamsCurrencyOptions]"
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

    class UpdateParamsCurrencyOptions(TypedDict):
        custom_unit_amount: NotRequired[
            "PriceService.UpdateParamsCurrencyOptionsCustomUnitAmount"
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
        tiers: NotRequired[
            List["PriceService.UpdateParamsCurrencyOptionsTier"]
        ]
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

    class UpdateParamsCurrencyOptionsCustomUnitAmount(TypedDict):
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

    class UpdateParamsCurrencyOptionsTier(TypedDict):
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

    def list(
        self,
        params: "PriceService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Price]:
        """
        Returns a list of your active prices, excluding [inline prices](https://stripe.com/docs/products-prices/pricing-models#inline-pricing). For the list of inactive prices, set active to false.
        """
        return cast(
            ListObject[Price],
            self._request(
                "get",
                "/v1/prices",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "PriceService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Price]:
        """
        Returns a list of your active prices, excluding [inline prices](https://stripe.com/docs/products-prices/pricing-models#inline-pricing). For the list of inactive prices, set active to false.
        """
        return cast(
            ListObject[Price],
            await self._request_async(
                "get",
                "/v1/prices",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self, params: "PriceService.CreateParams", options: RequestOptions = {}
    ) -> Price:
        """
        Creates a new price for an existing product. The price can be recurring or one-time.
        """
        return cast(
            Price,
            self._request(
                "post",
                "/v1/prices",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self, params: "PriceService.CreateParams", options: RequestOptions = {}
    ) -> Price:
        """
        Creates a new price for an existing product. The price can be recurring or one-time.
        """
        return cast(
            Price,
            await self._request_async(
                "post",
                "/v1/prices",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        price: str,
        params: "PriceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Price:
        """
        Retrieves the price with the given ID.
        """
        return cast(
            Price,
            self._request(
                "get",
                "/v1/prices/{price}".format(price=sanitize_id(price)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        price: str,
        params: "PriceService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Price:
        """
        Retrieves the price with the given ID.
        """
        return cast(
            Price,
            await self._request_async(
                "get",
                "/v1/prices/{price}".format(price=sanitize_id(price)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        price: str,
        params: "PriceService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Price:
        """
        Updates the specified price by setting the values of the parameters passed. Any parameters not provided are left unchanged.
        """
        return cast(
            Price,
            self._request(
                "post",
                "/v1/prices/{price}".format(price=sanitize_id(price)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        price: str,
        params: "PriceService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Price:
        """
        Updates the specified price by setting the values of the parameters passed. Any parameters not provided are left unchanged.
        """
        return cast(
            Price,
            await self._request_async(
                "post",
                "/v1/prices/{price}".format(price=sanitize_id(price)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def search(
        self, params: "PriceService.SearchParams", options: RequestOptions = {}
    ) -> SearchResultObject[Price]:
        """
        Search for prices you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[Price],
            self._request(
                "get",
                "/v1/prices/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def search_async(
        self, params: "PriceService.SearchParams", options: RequestOptions = {}
    ) -> SearchResultObject[Price]:
        """
        Search for prices you've previously created using Stripe's [Search Query Language](https://stripe.com/docs/search#search-query-language).
        Don't use search in read-after-write flows where strict consistency is necessary. Under normal operating
        conditions, data is searchable in less than a minute. Occasionally, propagation of new or updated data can be up
        to an hour behind during outages. Search functionality is not available to merchants in India.
        """
        return cast(
            SearchResultObject[Price],
            await self._request_async(
                "get",
                "/v1/prices/search",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
