# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._request_options import RequestOptions
from stripe._singleton_api_resource import SingletonAPIResource
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from typing import ClassVar, List, Optional, cast
from typing_extensions import Literal, NotRequired, TypedDict, Unpack


class Settings(
    SingletonAPIResource["Settings"],
    UpdateableAPIResource["Settings"],
):
    """
    You can use Tax `Settings` to manage configurations used by Stripe Tax calculations.

    Related guide: [Using the Settings API](https://stripe.com/docs/tax/settings-api)
    """

    OBJECT_NAME: ClassVar[Literal["tax.settings"]] = "tax.settings"

    class Defaults(StripeObject):
        tax_behavior: Optional[
            Literal["exclusive", "inclusive", "inferred_by_currency"]
        ]
        """
        Default [tax behavior](https://stripe.com/docs/tax/products-prices-tax-categories-tax-behavior#tax-behavior) used to specify whether the price is considered inclusive of taxes or exclusive of taxes. If the item's price has a tax behavior set, it will take precedence over the default tax behavior.
        """
        tax_code: Optional[str]
        """
        Default [tax code](https://stripe.com/docs/tax/tax-categories) used to classify your products and prices.
        """

    class HeadOffice(StripeObject):
        class Address(StripeObject):
            city: Optional[str]
            """
            City, district, suburb, town, or village.
            """
            country: Optional[str]
            """
            Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
            """
            line1: Optional[str]
            """
            Address line 1 (e.g., street, PO Box, or company name).
            """
            line2: Optional[str]
            """
            Address line 2 (e.g., apartment, suite, unit, or building).
            """
            postal_code: Optional[str]
            """
            ZIP or postal code.
            """
            state: Optional[str]
            """
            State, county, province, or region.
            """

        address: Address
        _inner_class_types = {"address": Address}

    class StatusDetails(StripeObject):
        class Active(StripeObject):
            pass

        class Pending(StripeObject):
            missing_fields: Optional[List[str]]
            """
            The list of missing fields that are required to perform calculations. It includes the entry `head_office` when the status is `pending`. It is recommended to set the optional values even if they aren't listed as required for calculating taxes. Calculations can fail if missing fields aren't explicitly provided on every call.
            """

        active: Optional[Active]
        pending: Optional[Pending]
        _inner_class_types = {"active": Active, "pending": Pending}

    class ModifyParams(RequestOptions):
        defaults: NotRequired["Settings.ModifyParamsDefaults"]
        """
        Default configuration to be used on Stripe Tax calculations.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        head_office: NotRequired["Settings.ModifyParamsHeadOffice"]
        """
        The place where your business is located.
        """

    class ModifyParamsDefaults(TypedDict):
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

    class ModifyParamsHeadOffice(TypedDict):
        address: "Settings.ModifyParamsHeadOfficeAddress"
        """
        The location of the business for tax purposes.
        """

    class ModifyParamsHeadOfficeAddress(TypedDict):
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    defaults: Defaults
    head_office: Optional[HeadOffice]
    """
    The place where your business is located.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["tax.settings"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    status: Literal["active", "pending"]
    """
    The `active` status indicates you have all required settings to calculate tax. A status can transition out of `active` when new required settings are introduced.
    """
    status_details: StatusDetails

    @classmethod
    def modify(cls, **params: Unpack["Settings.ModifyParams"]) -> "Settings":
        """
        Updates Tax Settings parameters used in tax calculations. All parameters are editable but none can be removed once set.
        """
        return cast(
            "Settings",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, **params: Unpack["Settings.ModifyParams"]
    ) -> "Settings":
        """
        Updates Tax Settings parameters used in tax calculations. All parameters are editable but none can be removed once set.
        """
        return cast(
            "Settings",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, **params: Unpack["Settings.RetrieveParams"]
    ) -> "Settings":
        """
        Retrieves Tax Settings for a merchant.
        """
        instance = cls(None, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, **params: Unpack["Settings.RetrieveParams"]
    ) -> "Settings":
        """
        Retrieves Tax Settings for a merchant.
        """
        instance = cls(None, **params)
        await instance.refresh_async()
        return instance

    @classmethod
    def class_url(cls):
        return "/v1/tax/settings"

    _inner_class_types = {
        "defaults": Defaults,
        "head_office": HeadOffice,
        "status_details": StatusDetails,
    }
