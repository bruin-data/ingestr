# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._test_helpers import APIResourceTestHelpers
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, Dict, List, Optional, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    Type,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._file import File
    from stripe.issuing._physical_bundle import PhysicalBundle


class PersonalizationDesign(
    CreateableAPIResource["PersonalizationDesign"],
    ListableAPIResource["PersonalizationDesign"],
    UpdateableAPIResource["PersonalizationDesign"],
):
    """
    A Personalization Design is a logical grouping of a Physical Bundle, card logo, and carrier text that represents a product line.
    """

    OBJECT_NAME: ClassVar[Literal["issuing.personalization_design"]] = (
        "issuing.personalization_design"
    )

    class CarrierText(StripeObject):
        footer_body: Optional[str]
        """
        The footer body text of the carrier letter.
        """
        footer_title: Optional[str]
        """
        The footer title text of the carrier letter.
        """
        header_body: Optional[str]
        """
        The header body text of the carrier letter.
        """
        header_title: Optional[str]
        """
        The header title text of the carrier letter.
        """

    class Preferences(StripeObject):
        is_default: bool
        """
        Whether we use this personalization design to create cards when one isn't specified. A connected account uses the Connect platform's default design if no personalization design is set as the default design.
        """
        is_platform_default: Optional[bool]
        """
        Whether this personalization design is used to create cards when one is not specified and a default for this connected account does not exist.
        """

    class RejectionReasons(StripeObject):
        card_logo: Optional[
            List[
                Literal[
                    "geographic_location",
                    "inappropriate",
                    "network_name",
                    "non_binary_image",
                    "non_fiat_currency",
                    "other",
                    "other_entity",
                    "promotional_material",
                ]
            ]
        ]
        """
        The reason(s) the card logo was rejected.
        """
        carrier_text: Optional[
            List[
                Literal[
                    "geographic_location",
                    "inappropriate",
                    "network_name",
                    "non_fiat_currency",
                    "other",
                    "other_entity",
                    "promotional_material",
                ]
            ]
        ]
        """
        The reason(s) the carrier text was rejected.
        """

    class ActivateParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class CreateParams(RequestOptions):
        card_logo: NotRequired[str]
        """
        The file for the card logo, for use with physical bundles that support card logos. Must have a `purpose` value of `issuing_logo`.
        """
        carrier_text: NotRequired[
            "PersonalizationDesign.CreateParamsCarrierText"
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
            "PersonalizationDesign.CreateParamsPreferences"
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

    class DeactivateParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
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
        lookup_keys: NotRequired[List[str]]
        """
        Only return personalization designs with the given lookup keys.
        """
        preferences: NotRequired["PersonalizationDesign.ListParamsPreferences"]
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

    class ModifyParams(RequestOptions):
        card_logo: NotRequired["Literal['']|str"]
        """
        The file for the card logo, for use with physical bundles that support card logos. Must have a `purpose` value of `issuing_logo`.
        """
        carrier_text: NotRequired[
            "Literal['']|PersonalizationDesign.ModifyParamsCarrierText"
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
            "PersonalizationDesign.ModifyParamsPreferences"
        ]
        """
        Information on whether this personalization design is used to create cards when one is not specified.
        """
        transfer_lookup_key: NotRequired[bool]
        """
        If set to true, will atomically remove the lookup key from the existing personalization design, and assign it to this personalization design.
        """

    class ModifyParamsCarrierText(TypedDict):
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

    class ModifyParamsPreferences(TypedDict):
        is_default: bool
        """
        Whether we use this personalization design to create cards when one isn't specified. A connected account uses the Connect platform's default design if no personalization design is set as the default design.
        """

    class RejectParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        rejection_reasons: "PersonalizationDesign.RejectParamsRejectionReasons"
        """
        The reason(s) the personalization design was rejected.
        """

    class RejectParamsRejectionReasons(TypedDict):
        card_logo: NotRequired[
            List[
                Literal[
                    "geographic_location",
                    "inappropriate",
                    "network_name",
                    "non_binary_image",
                    "non_fiat_currency",
                    "other",
                    "other_entity",
                    "promotional_material",
                ]
            ]
        ]
        """
        The reason(s) the card logo was rejected.
        """
        carrier_text: NotRequired[
            List[
                Literal[
                    "geographic_location",
                    "inappropriate",
                    "network_name",
                    "non_fiat_currency",
                    "other",
                    "other_entity",
                    "promotional_material",
                ]
            ]
        ]
        """
        The reason(s) the carrier text was rejected.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    card_logo: Optional[ExpandableField["File"]]
    """
    The file for the card logo to use with physical bundles that support card logos. Must have a `purpose` value of `issuing_logo`.
    """
    carrier_text: Optional[CarrierText]
    """
    Hash containing carrier text, for use with physical bundles that support carrier text.
    """
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
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
    A lookup key used to retrieve personalization designs dynamically from a static string. This may be up to 200 characters.
    """
    metadata: Dict[str, str]
    """
    Set of [key-value pairs](https://stripe.com/docs/api/metadata) that you can attach to an object. This can be useful for storing additional information about the object in a structured format.
    """
    name: Optional[str]
    """
    Friendly display name.
    """
    object: Literal["issuing.personalization_design"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    physical_bundle: ExpandableField["PhysicalBundle"]
    """
    The physical bundle object belonging to this personalization design.
    """
    preferences: Preferences
    rejection_reasons: RejectionReasons
    status: Literal["active", "inactive", "rejected", "review"]
    """
    Whether this personalization design can be used to create cards.
    """

    @classmethod
    def create(
        cls, **params: Unpack["PersonalizationDesign.CreateParams"]
    ) -> "PersonalizationDesign":
        """
        Creates a personalization design object.
        """
        return cast(
            "PersonalizationDesign",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["PersonalizationDesign.CreateParams"]
    ) -> "PersonalizationDesign":
        """
        Creates a personalization design object.
        """
        return cast(
            "PersonalizationDesign",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["PersonalizationDesign.ListParams"]
    ) -> ListObject["PersonalizationDesign"]:
        """
        Returns a list of personalization design objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
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
        cls, **params: Unpack["PersonalizationDesign.ListParams"]
    ) -> ListObject["PersonalizationDesign"]:
        """
        Returns a list of personalization design objects. The objects are sorted in descending order by creation date, with the most recently created object appearing first.
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
        cls, id: str, **params: Unpack["PersonalizationDesign.ModifyParams"]
    ) -> "PersonalizationDesign":
        """
        Updates a card personalization object.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "PersonalizationDesign",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["PersonalizationDesign.ModifyParams"]
    ) -> "PersonalizationDesign":
        """
        Updates a card personalization object.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "PersonalizationDesign",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["PersonalizationDesign.RetrieveParams"]
    ) -> "PersonalizationDesign":
        """
        Retrieves a personalization design object.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["PersonalizationDesign.RetrieveParams"]
    ) -> "PersonalizationDesign":
        """
        Retrieves a personalization design object.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    class TestHelpers(APIResourceTestHelpers["PersonalizationDesign"]):
        _resource_cls: Type["PersonalizationDesign"]

        @classmethod
        def _cls_activate(
            cls,
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.ActivateParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to active.
            """
            return cast(
                "PersonalizationDesign",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/activate".format(
                        personalization_design=sanitize_id(
                            personalization_design
                        )
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def activate(
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.ActivateParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to active.
            """
            ...

        @overload
        def activate(
            self, **params: Unpack["PersonalizationDesign.ActivateParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to active.
            """
            ...

        @class_method_variant("_cls_activate")
        def activate(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["PersonalizationDesign.ActivateParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to active.
            """
            return cast(
                "PersonalizationDesign",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/activate".format(
                        personalization_design=sanitize_id(
                            self.resource.get("id")
                        )
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_activate_async(
            cls,
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.ActivateParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to active.
            """
            return cast(
                "PersonalizationDesign",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/activate".format(
                        personalization_design=sanitize_id(
                            personalization_design
                        )
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def activate_async(
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.ActivateParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to active.
            """
            ...

        @overload
        async def activate_async(
            self, **params: Unpack["PersonalizationDesign.ActivateParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to active.
            """
            ...

        @class_method_variant("_cls_activate_async")
        async def activate_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["PersonalizationDesign.ActivateParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to active.
            """
            return cast(
                "PersonalizationDesign",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/activate".format(
                        personalization_design=sanitize_id(
                            self.resource.get("id")
                        )
                    ),
                    params=params,
                ),
            )

        @classmethod
        def _cls_deactivate(
            cls,
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.DeactivateParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to inactive.
            """
            return cast(
                "PersonalizationDesign",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/deactivate".format(
                        personalization_design=sanitize_id(
                            personalization_design
                        )
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def deactivate(
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.DeactivateParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to inactive.
            """
            ...

        @overload
        def deactivate(
            self, **params: Unpack["PersonalizationDesign.DeactivateParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to inactive.
            """
            ...

        @class_method_variant("_cls_deactivate")
        def deactivate(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["PersonalizationDesign.DeactivateParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to inactive.
            """
            return cast(
                "PersonalizationDesign",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/deactivate".format(
                        personalization_design=sanitize_id(
                            self.resource.get("id")
                        )
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_deactivate_async(
            cls,
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.DeactivateParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to inactive.
            """
            return cast(
                "PersonalizationDesign",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/deactivate".format(
                        personalization_design=sanitize_id(
                            personalization_design
                        )
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def deactivate_async(
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.DeactivateParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to inactive.
            """
            ...

        @overload
        async def deactivate_async(
            self, **params: Unpack["PersonalizationDesign.DeactivateParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to inactive.
            """
            ...

        @class_method_variant("_cls_deactivate_async")
        async def deactivate_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["PersonalizationDesign.DeactivateParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to inactive.
            """
            return cast(
                "PersonalizationDesign",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/deactivate".format(
                        personalization_design=sanitize_id(
                            self.resource.get("id")
                        )
                    ),
                    params=params,
                ),
            )

        @classmethod
        def _cls_reject(
            cls,
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.RejectParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to rejected.
            """
            return cast(
                "PersonalizationDesign",
                cls._static_request(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/reject".format(
                        personalization_design=sanitize_id(
                            personalization_design
                        )
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        def reject(
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.RejectParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to rejected.
            """
            ...

        @overload
        def reject(
            self, **params: Unpack["PersonalizationDesign.RejectParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to rejected.
            """
            ...

        @class_method_variant("_cls_reject")
        def reject(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["PersonalizationDesign.RejectParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to rejected.
            """
            return cast(
                "PersonalizationDesign",
                self.resource._request(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/reject".format(
                        personalization_design=sanitize_id(
                            self.resource.get("id")
                        )
                    ),
                    params=params,
                ),
            )

        @classmethod
        async def _cls_reject_async(
            cls,
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.RejectParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to rejected.
            """
            return cast(
                "PersonalizationDesign",
                await cls._static_request_async(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/reject".format(
                        personalization_design=sanitize_id(
                            personalization_design
                        )
                    ),
                    params=params,
                ),
            )

        @overload
        @staticmethod
        async def reject_async(
            personalization_design: str,
            **params: Unpack["PersonalizationDesign.RejectParams"],
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to rejected.
            """
            ...

        @overload
        async def reject_async(
            self, **params: Unpack["PersonalizationDesign.RejectParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to rejected.
            """
            ...

        @class_method_variant("_cls_reject_async")
        async def reject_async(  # pyright: ignore[reportGeneralTypeIssues]
            self, **params: Unpack["PersonalizationDesign.RejectParams"]
        ) -> "PersonalizationDesign":
            """
            Updates the status of the specified testmode personalization design object to rejected.
            """
            return cast(
                "PersonalizationDesign",
                await self.resource._request_async(
                    "post",
                    "/v1/test_helpers/issuing/personalization_designs/{personalization_design}/reject".format(
                        personalization_design=sanitize_id(
                            self.resource.get("id")
                        )
                    ),
                    params=params,
                ),
            )

    @property
    def test_helpers(self):
        return self.TestHelpers(self)

    _inner_class_types = {
        "carrier_text": CarrierText,
        "preferences": Preferences,
        "rejection_reasons": RejectionReasons,
    }


PersonalizationDesign.TestHelpers._resource_cls = PersonalizationDesign
