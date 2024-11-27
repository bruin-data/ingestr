# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.terminal._location import Location
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class LocationService(StripeService):
    class CreateParams(TypedDict):
        address: "LocationService.CreateParamsAddress"
        """
        The full address of the location.
        """
        configuration_overrides: NotRequired[str]
        """
        The ID of a configuration that will be used to customize all readers in this location.
        """
        display_name: str
        """
        A name for the location.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class CreateParamsAddress(TypedDict):
        city: NotRequired[str]
        """
        City, district, suburb, town, or village.
        """
        country: str
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
        State, county, province, or region.
        """

    class DeleteParams(TypedDict):
        pass

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

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        address: NotRequired["LocationService.UpdateParamsAddress"]
        """
        The full address of the location. If you're updating the `address` field, avoid changing the `country`. If you need to modify the `country` field, create a new `Location` object and re-register any existing readers to that location.
        """
        configuration_overrides: NotRequired["Literal['']|str"]
        """
        The ID of a configuration that will be used to customize all readers in this location.
        """
        display_name: NotRequired[str]
        """
        A name for the location.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        metadata: NotRequired["Literal['']|Dict[str, str]"]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """

    class UpdateParamsAddress(TypedDict):
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
        State, county, province, or region.
        """

    def delete(
        self,
        location: str,
        params: "LocationService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Location:
        """
        Deletes a Location object.
        """
        return cast(
            Location,
            self._request(
                "delete",
                "/v1/terminal/locations/{location}".format(
                    location=sanitize_id(location),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def delete_async(
        self,
        location: str,
        params: "LocationService.DeleteParams" = {},
        options: RequestOptions = {},
    ) -> Location:
        """
        Deletes a Location object.
        """
        return cast(
            Location,
            await self._request_async(
                "delete",
                "/v1/terminal/locations/{location}".format(
                    location=sanitize_id(location),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        location: str,
        params: "LocationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Location:
        """
        Retrieves a Location object.
        """
        return cast(
            Location,
            self._request(
                "get",
                "/v1/terminal/locations/{location}".format(
                    location=sanitize_id(location),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        location: str,
        params: "LocationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Location:
        """
        Retrieves a Location object.
        """
        return cast(
            Location,
            await self._request_async(
                "get",
                "/v1/terminal/locations/{location}".format(
                    location=sanitize_id(location),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        location: str,
        params: "LocationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Location:
        """
        Updates a Location object by setting the values of the parameters passed. Any parameters not provided will be left unchanged.
        """
        return cast(
            Location,
            self._request(
                "post",
                "/v1/terminal/locations/{location}".format(
                    location=sanitize_id(location),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        location: str,
        params: "LocationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Location:
        """
        Updates a Location object by setting the values of the parameters passed. Any parameters not provided will be left unchanged.
        """
        return cast(
            Location,
            await self._request_async(
                "post",
                "/v1/terminal/locations/{location}".format(
                    location=sanitize_id(location),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def list(
        self,
        params: "LocationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Location]:
        """
        Returns a list of Location objects.
        """
        return cast(
            ListObject[Location],
            self._request(
                "get",
                "/v1/terminal/locations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "LocationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Location]:
        """
        Returns a list of Location objects.
        """
        return cast(
            ListObject[Location],
            await self._request_async(
                "get",
                "/v1/terminal/locations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "LocationService.CreateParams",
        options: RequestOptions = {},
    ) -> Location:
        """
        Creates a new Location object.
        For further details, including which address fields are required in each country, see the [Manage locations](https://stripe.com/docs/terminal/fleet/locations) guide.
        """
        return cast(
            Location,
            self._request(
                "post",
                "/v1/terminal/locations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "LocationService.CreateParams",
        options: RequestOptions = {},
    ) -> Location:
        """
        Creates a new Location object.
        For further details, including which address fields are required in each country, see the [Manage locations](https://stripe.com/docs/terminal/fleet/locations) guide.
        """
        return cast(
            Location,
            await self._request_async(
                "post",
                "/v1/terminal/locations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
