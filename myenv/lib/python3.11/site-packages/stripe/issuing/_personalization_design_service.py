# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.issuing._personalization_design import PersonalizationDesign
from typing import Dict, List, cast
from typing_extensions import Literal, NotRequired, TypedDict


class PersonalizationDesignService(StripeService):
    class CreateParams(TypedDict):
        card_logo: NotRequired[str]
        """
        The file for the card logo, for use with physical bundles that support card logos. Must have a `purpose` value of `issuing_logo`.
        """
        carrier_text: NotRequired[
            "PersonalizationDesignService.CreateParamsCarrierText"
        ]
        """
        Hash containing carrier text, for use with physical bundles that support carrier text.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        lookup_key: NotRequired[str]
        """
        A lookup key used to retrieve personalization designs dynamically from a static string. This may be up to 200 characters.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: NotRequired[str]
        """
        Friendly display name.
        """
        physical_bundle: str
        """
        The physical bundle object belonging to this personalization design.
        """
        preferences: NotRequired[
            "PersonalizationDesignService.CreateParamsPreferences"
        ]
        """
        Information on whether this personalization design is used to create cards when one is not specified.
        """
        transfer_lookup_key: NotRequired[bool]
        """
        If set to true, will atomically remove the lookup key from the existing personalization design, and assign it to this personalization design.
        """

    class CreateParamsCarrierText(TypedDict):
        footer_body: NotRequired["Literal['']|str"]
        """
        The footer body text of the carrier letter.
        """
        footer_title: NotRequired["Literal['']|str"]
        """
        The footer title text of the carrier letter.
        """
        header_body: NotRequired["Literal['']|str"]
        """
        The header body text of the carrier letter.
        """
        header_title: NotRequired["Literal['']|str"]
        """
        The header title text of the carrier letter.
        """

    class CreateParamsPreferences(TypedDict):
        is_default: bool
        """
        Whether we use this personalization design to create cards when one isn't specified. A connected account uses the Connect platform's default design if no personalization design is set as the default design.
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
        lookup_keys: NotRequired[List[str]]
        """
        Only return personalization designs with the given lookup keys.
        """
        preferences: NotRequired[
            "PersonalizationDesignService.ListParamsPreferences"
        ]
        """
        Only return personalization designs with the given preferences.
        """
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[
            Literal["active", "inactive", "rejected", "review"]
        ]
        """
        Only return personalization designs with the given status.
        """

    class ListParamsPreferences(TypedDict):
        is_default: NotRequired[bool]
        """
        Only return the personalization design that's set as the default. A connected account uses the Connect platform's default design if no personalization design is set as the default.
        """
        is_platform_default: NotRequired[bool]
        """
        Only return the personalization design that is set as the Connect platform's default. This parameter is only applicable to connected accounts.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        card_logo: NotRequired["Literal['']|str"]
        """
        The file for the card logo, for use with physical bundles that support card logos. Must have a `purpose` value of `issuing_logo`.
        """
        carrier_text: NotRequired[
            "Literal['']|PersonalizationDesignService.UpdateParamsCarrierText"
        ]
        """
        Hash containing carrier text, for use with physical bundles that support carrier text.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        lookup_key: NotRequired["Literal['']|str"]
        """
        A lookup key used to retrieve personalization designs dynamically from a static string. This may be up to 200 characters.
        """
        metadata: NotRequired[Dict[str, str]]
        """
        Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format. Individual keys can be unset by posting an empty value to them. All keys can be unset by posting an empty value to `metadata`.
        """
        name: NotRequired["Literal['']|str"]
        """
        Friendly display name. Providing an empty string will set the field to null.
        """
        physical_bundle: NotRequired[str]
        """
        The physical bundle object belonging to this personalization design.
        """
        preferences: NotRequired[
            "PersonalizationDesignService.UpdateParamsPreferences"
        ]
        """
        Information on whether this personalization design is used to create cards when one is not specified.
        """
        transfer_lookup_key: NotRequired[bool]
        """
        If set to true, will atomically remove the lookup key from the existing personalization design, and assign it to this personalization design.
        """

    class UpdateParamsCarrierText(TypedDict):
        footer_body: NotRequired["Literal['']|str"]
        """
        The footer body text of the carrier letter.
        """
        footer_title: NotRequired["Literal['']|str"]
        """
        The footer title text of the carrier letter.
        """
        header_body: NotRequired["Literal['']|str"]
        """
        The header body text of the carrier letter.
        """
        header_title: NotRequired["Literal['']|str"]
        """
        The header title text of the carrier letter.
        """

    class UpdateParamsPreferences(TypedDict):
        is_default: bool
        """
        Whether we use this personalization design to create cards when one isn't specified. A connected account uses the Connect platform's default design if no personalization design is set as the default design.
        """

    def list(
        self,
        params: "PersonalizationDesignService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PersonalizationDesign]:
        """
        Returns a list of personalization design objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[PersonalizationDesign],
            self._request(
                "get",
                "/v1/issuing/personalization_designs",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "PersonalizationDesignService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[PersonalizationDesign]:
        """
        Returns a list of personalization design objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
        """
        return cast(
            ListObject[PersonalizationDesign],
            await self._request_async(
                "get",
                "/v1/issuing/personalization_designs",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "PersonalizationDesignService.CreateParams",
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Creates a personalization design object.
        """
        return cast(
            PersonalizationDesign,
            self._request(
                "post",
                "/v1/issuing/personalization_designs",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "PersonalizationDesignService.CreateParams",
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Creates a personalization design object.
        """
        return cast(
            PersonalizationDesign,
            await self._request_async(
                "post",
                "/v1/issuing/personalization_designs",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        personalization_design: str,
        params: "PersonalizationDesignService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Retrieves a personalization design object.
        """
        return cast(
            PersonalizationDesign,
            self._request(
                "get",
                "/v1/issuing/personalization_designs/{personalization_design}".format(
                    personalization_design=sanitize_id(personalization_design),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        personalization_design: str,
        params: "PersonalizationDesignService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Retrieves a personalization design object.
        """
        return cast(
            PersonalizationDesign,
            await self._request_async(
                "get",
                "/v1/issuing/personalization_designs/{personalization_design}".format(
                    personalization_design=sanitize_id(personalization_design),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        personalization_design: str,
        params: "PersonalizationDesignService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Updates a card personalization object.
        """
        return cast(
            PersonalizationDesign,
            self._request(
                "post",
                "/v1/issuing/personalization_designs/{personalization_design}".format(
                    personalization_design=sanitize_id(personalization_design),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        personalization_design: str,
        params: "PersonalizationDesignService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> PersonalizationDesign:
        """
        Updates a card personalization object.
        """
        return cast(
            PersonalizationDesign,
            await self._request_async(
                "post",
                "/v1/issuing/personalization_designs/{personalization_design}".format(
                    personalization_design=sanitize_id(personalization_design),
                ),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
