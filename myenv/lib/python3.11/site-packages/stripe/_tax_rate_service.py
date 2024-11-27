# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._tax_rate import TaxRate
from stripe._util import sanitize_id
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class TaxRateService(StripeService):
    class CreateParams(TypedDict):
        active: NotRequired[bool]
        """
        Flag determining whether the tax rate is active or inactive (archived). Inactive tax rates cannot be used with new applications or Checkout Sessions, but will still work for subscriptions and invoices that already have it set.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the tax rate for your internal use only. It will not be visible to your customers.
        """
        display_name: str
        """
        The display name of the tax rate, which will be shown to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        inclusive: bool
        """
        This specifies if the tax rate is inclusive or exclusive.
        """
        jurisdiction: NotRequired[str]
        """
        The jurisdiction for the tax rate. You can use this label field for tax reporting purposes. It also appears on your customer's invoice.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        percentage: float
        """
        This represents the tax rate percent out of 100.
        """
        state: NotRequired[str]
        """
        [ISO 3166-2 subdivision code](https://en.wikipedia.org/wiki/ISO_3166-2:US), without country prefix. For example, "NY" for New York, United States.
        """
        tax_type: NotRequired[
            Literal[
                "amusement_tax",
                "communications_tax",
                "gst",
                "hst",
                "igst",
                "jct",
                "lease_tax",
                "pst",
                "qst",
                "rst",
                "sales_tax",
                "vat",
            ]
        ]
        """
        The high-level tax type, such as `vat` or `sales_tax`.
        """

    class ListParams(TypedDict):
        active: NotRequired[bool]
        """
        Optional flag to filter by tax rates that are either active or inactive (archived).
        """
        created: NotRequired["TaxRateService.ListParamsCreated|int"]
        """
        Optional range for filtering created date.
        """
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        inclusive: NotRequired[bool]
        """
        Optional flag to filter by tax rates that are inclusive (or those that are not inclusive).
        """
        limit: NotRequired[int]
        """
        A limit on the number of objects to be returned. Limit can range between 1 and 100, and the default is 10.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
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

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        active: NotRequired[bool]
        """
        Flag determining whether the tax rate is active or inactive (archived). Inactive tax rates cannot be used with new applications or Checkout Sessions, but will still work for subscriptions and invoices that already have it set.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        description: NotRequired[str]
        """
        An arbitrary string attached to the tax rate for your internal use only. It will not be visible to your customers.
        """
        display_name: NotRequired[str]
        """
        The display name of the tax rate, which will be shown to users.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        jurisdiction: NotRequired[str]
        """
        The jurisdiction for the tax rate. You can use this label field for tax reporting purposes. It also appears on your customer's invoice.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        state: NotRequired[str]
        """
        [ISO 3166-2 subdivision code](https://en.wikipedia.org/wiki/ISO_3166-2:US), without country prefix. For example, "NY" for New York, United States.
        """
        tax_type: NotRequired[
            Literal[
                "amusement_tax",
                "communications_tax",
                "gst",
                "hst",
                "igst",
                "jct",
                "lease_tax",
                "pst",
                "qst",
                "rst",
                "sales_tax",
                "vat",
            ]
        ]
        """
        The high-level tax type, such as `vat` or `sales_tax`.
        """

    def list(
        self,
        params: "TaxRateService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[TaxRate]:
        """
        Returns a list of your tax rates. Tax rates are returned sorted by creation date, with the most recently created tax rates appearing first.
        """
        return cast(
            ListObject[TaxRate],
            self._request(
                "get",
                "/v1/tax_rates",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "TaxRateService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[TaxRate]:
        """
        Returns a list of your tax rates. Tax rates are returned sorted by creation date, with the most recently created tax rates appearing first.
        """
        return cast(
            ListObject[TaxRate],
            await self._request_async(
                "get",
                "/v1/tax_rates",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "TaxRateService.CreateParams",
        options: RequestOptions = {},
    ) -> TaxRate:
        """
        Creates a new tax rate.
        """
        return cast(
            TaxRate,
            self._request(
                "post",
                "/v1/tax_rates",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "TaxRateService.CreateParams",
        options: RequestOptions = {},
    ) -> TaxRate:
        """
        Creates a new tax rate.
        """
        return cast(
            TaxRate,
            await self._request_async(
                "post",
                "/v1/tax_rates",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        tax_rate: str,
        params: "TaxRateService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> TaxRate:
        """
        Retrieves a tax rate with the given ID
        """
        return cast(
            TaxRate,
            self._request(
                "get",
                "/v1/tax_rates/{tax_rate}".format(
                    tax_rate=sanitize_id(tax_rate),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        tax_rate: str,
        params: "TaxRateService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> TaxRate:
        """
        Retrieves a tax rate with the given ID
        """
        return cast(
            TaxRate,
            await self._request_async(
                "get",
                "/v1/tax_rates/{tax_rate}".format(
                    tax_rate=sanitize_id(tax_rate),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        tax_rate: str,
        params: "TaxRateService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> TaxRate:
        """
        Updates an existing tax rate.
        """
        return cast(
            TaxRate,
            self._request(
                "post",
                "/v1/tax_rates/{tax_rate}".format(
                    tax_rate=sanitize_id(tax_rate),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        tax_rate: str,
        params: "TaxRateService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> TaxRate:
        """
        Updates an existing tax rate.
        """
        return cast(
            TaxRate,
            await self._request_async(
                "post",
                "/v1/tax_rates/{tax_rate}".format(
                    tax_rate=sanitize_id(tax_rate),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
