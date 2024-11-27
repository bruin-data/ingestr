# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe.tax._settings import Settings
from typing import List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class SettingsService(StripeService):
    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        defaults: NotRequired["SettingsService.UpdateParamsDefaults"]
        """
        Default configuration to be used on Stripe Tax calculations.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        head_office: NotRequired["SettingsService.UpdateParamsHeadOffice"]
        """
        The place where your business is located.
        """

    class UpdateParamsDefaults(TypedDict):
        tax_behavior: NotRequired[
            Literal["exclusive", "inclusive", "inferred_by_currency"]
        ]
        """
        Specifies the default [tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#tax-behavior) to be used when the item's price has unspecified tax behavior. One of inclusive, exclusive, or inferred_by_currency. Once specified, it cannot be changed back to null.
        """
        tax_code: NotRequired[str]
        """
        A [tax code](https://stripe.com/docs/tax/tax-categories) ID.
        """

    class UpdateParamsHeadOffice(TypedDict):
        address: "SettingsService.UpdateParamsHeadOfficeAddress"
        """
        The location of the business for tax purposes.
        """

    class UpdateParamsHeadOfficeAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: NotRequired[str]
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        line1: NotRequired[str]
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
        State/province as an [ISO 3166-2](https://en.wikipedia.org/wiki/ISO_3166-2) subdivision code, without country prefix. Example: "NY" or "TX".
        """

    def retrieve(
        self,
        params: "SettingsService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Settings:
        """
        Retrieves Tax Settings for a merchant.
        """
        return cast(
            Settings,
            self._request(
                "get",
                "/v1/tax/settings",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        params: "SettingsService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Settings:
        """
        Retrieves Tax Settings for a merchant.
        """
        return cast(
            Settings,
            await self._request_async(
                "get",
                "/v1/tax/settings",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        params: "SettingsService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Settings:
        """
        Updates Tax Settings parameters used in tax calculations. All parameters are editable but none can be removed once set.
        """
        return cast(
            Settings,
            self._request(
                "post",
                "/v1/tax/settings",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        params: "SettingsService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Settings:
        """
        Updates Tax Settings parameters used in tax calculations. All parameters are editable but none can be removed once set.
        """
        return cast(
            Settings,
            await self._request_async(
                "post",
                "/v1/tax/settings",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
