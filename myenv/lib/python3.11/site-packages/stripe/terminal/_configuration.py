# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._deletable_api_resource import DeletableAPIResource
from stripe._expandable_field import ExpandableField
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import class_method_variant, sanitize_id
from typing import ClassVar, List, Optional, cast, overload
from typing_extensions import (
    Literal,
    NotRequired,
    TypedDict,
    Unpack,
    TYPE_CHECKING,
)

if TYPE_CHECKING:
    from stripe._file import File


class Configuration(
    CreateableAPIResource["Configuration"],
    DeletableAPIResource["Configuration"],
    ListableAPIResource["Configuration"],
    UpdateableAPIResource["Configuration"],
):
    """
    A Configurations object represents how features should be configured for terminal readers.
    """

    OBJECT_NAME: ClassVar[Literal["terminal.configuration"]] = (
        "terminal.configuration"
    )

    class BbposWiseposE(StripeObject):
        splashscreen: Optional[ExpandableField["File"]]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class Offline(StripeObject):
        enabled: Optional[bool]
        """
        Determines whether to allow transactions to be collected while reader is offline. Defaults to false.
        """

    class RebootWindow(StripeObject):
        end_hour: int
        """
        Integer between 0 to 23 that represents the end hour of the reboot time window. The value must be different than the start_hour.
        """
        start_hour: int
        """
        Integer between 0 to 23 that represents the start hour of the reboot time window.
        """

    class StripeS700(StripeObject):
        splashscreen: Optional[ExpandableField["File"]]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class Tipping(StripeObject):
        class Aud(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Cad(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Chf(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Czk(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Dkk(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Eur(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Gbp(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Hkd(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Myr(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Nok(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Nzd(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Sek(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Sgd(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        class Usd(StripeObject):
            fixed_amounts: Optional[List[int]]
            """
            Fixed amounts displayed when collecting a tip
            """
            percentages: Optional[List[int]]
            """
            Percentages displayed when collecting a tip
            """
            smart_tip_threshold: Optional[int]
            """
            Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
            """

        aud: Optional[Aud]
        cad: Optional[Cad]
        chf: Optional[Chf]
        czk: Optional[Czk]
        dkk: Optional[Dkk]
        eur: Optional[Eur]
        gbp: Optional[Gbp]
        hkd: Optional[Hkd]
        myr: Optional[Myr]
        nok: Optional[Nok]
        nzd: Optional[Nzd]
        sek: Optional[Sek]
        sgd: Optional[Sgd]
        usd: Optional[Usd]
        _inner_class_types = {
            "aud": Aud,
            "cad": Cad,
            "chf": Chf,
            "czk": Czk,
            "dkk": Dkk,
            "eur": Eur,
            "gbp": Gbp,
            "hkd": Hkd,
            "myr": Myr,
            "nok": Nok,
            "nzd": Nzd,
            "sek": Sek,
            "sgd": Sgd,
            "usd": Usd,
        }

    class VerifoneP400(StripeObject):
        splashscreen: Optional[ExpandableField["File"]]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class CreateParams(RequestOptions):
        bbpos_wisepos_e: NotRequired["Configuration.CreateParamsBbposWiseposE"]
        """
        An object containing device type specific settings for BBPOS WisePOS E readers
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        name: NotRequired[str]
        """
        Name of the configuration
        """
        offline: NotRequired["Literal['']|Configuration.CreateParamsOffline"]
        """
        Configurations for collecting transactions offline.
        """
        reboot_window: NotRequired["Configuration.CreateParamsRebootWindow"]
        """
        Reboot time settings for readers that support customized reboot time configuration.
        """
        stripe_s700: NotRequired["Configuration.CreateParamsStripeS700"]
        """
        An object containing device type specific settings for Stripe S700 readers
        """
        tipping: NotRequired["Literal['']|Configuration.CreateParamsTipping"]
        """
        Tipping configurations for readers supporting on-reader tips
        """
        verifone_p400: NotRequired["Configuration.CreateParamsVerifoneP400"]
        """
        An object containing device type specific settings for Verifone P400 readers
        """

    class CreateParamsBbposWiseposE(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class CreateParamsOffline(TypedDict):
        enabled: bool
        """
        Determines whether to allow transactions to be collected while reader is offline. Defaults to false.
        """

    class CreateParamsRebootWindow(TypedDict):
        end_hour: int
        """
        Integer between 0 to 23 that represents the end hour of the reboot time window. The value must be different than the start_hour.
        """
        start_hour: int
        """
        Integer between 0 to 23 that represents the start hour of the reboot time window.
        """

    class CreateParamsStripeS700(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class CreateParamsTipping(TypedDict):
        aud: NotRequired["Configuration.CreateParamsTippingAud"]
        """
        Tipping configuration for AUD
        """
        cad: NotRequired["Configuration.CreateParamsTippingCad"]
        """
        Tipping configuration for CAD
        """
        chf: NotRequired["Configuration.CreateParamsTippingChf"]
        """
        Tipping configuration for CHF
        """
        czk: NotRequired["Configuration.CreateParamsTippingCzk"]
        """
        Tipping configuration for CZK
        """
        dkk: NotRequired["Configuration.CreateParamsTippingDkk"]
        """
        Tipping configuration for DKK
        """
        eur: NotRequired["Configuration.CreateParamsTippingEur"]
        """
        Tipping configuration for EUR
        """
        gbp: NotRequired["Configuration.CreateParamsTippingGbp"]
        """
        Tipping configuration for GBP
        """
        hkd: NotRequired["Configuration.CreateParamsTippingHkd"]
        """
        Tipping configuration for HKD
        """
        myr: NotRequired["Configuration.CreateParamsTippingMyr"]
        """
        Tipping configuration for MYR
        """
        nok: NotRequired["Configuration.CreateParamsTippingNok"]
        """
        Tipping configuration for NOK
        """
        nzd: NotRequired["Configuration.CreateParamsTippingNzd"]
        """
        Tipping configuration for NZD
        """
        sek: NotRequired["Configuration.CreateParamsTippingSek"]
        """
        Tipping configuration for SEK
        """
        sgd: NotRequired["Configuration.CreateParamsTippingSgd"]
        """
        Tipping configuration for SGD
        """
        usd: NotRequired["Configuration.CreateParamsTippingUsd"]
        """
        Tipping configuration for USD
        """

    class CreateParamsTippingAud(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingCad(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingChf(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingCzk(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingDkk(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingEur(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingGbp(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingHkd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingMyr(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingNok(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingNzd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingSek(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingSgd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsTippingUsd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class CreateParamsVerifoneP400(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class DeleteParams(RequestOptions):
        pass

    class ListParams(RequestOptions):
        ending_before: NotRequired[str]
        """
        A cursor for use in pagination. `ending_before` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, starting with `obj_bar`, your subsequent call can include `ending_before=obj_bar` in order to fetch the previous page of the list.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        is_account_default: NotRequired[bool]
        """
        if present, only return the account default or non-default configurations.
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
        bbpos_wisepos_e: NotRequired[
            "Literal['']|Configuration.ModifyParamsBbposWiseposE"
        ]
        """
        An object containing device type specific settings for BBPOS WisePOS E readers
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        name: NotRequired[str]
        """
        Name of the configuration
        """
        offline: NotRequired["Literal['']|Configuration.ModifyParamsOffline"]
        """
        Configurations for collecting transactions offline.
        """
        reboot_window: NotRequired[
            "Literal['']|Configuration.ModifyParamsRebootWindow"
        ]
        """
        Reboot time settings for readers that support customized reboot time configuration.
        """
        stripe_s700: NotRequired[
            "Literal['']|Configuration.ModifyParamsStripeS700"
        ]
        """
        An object containing device type specific settings for Stripe S700 readers
        """
        tipping: NotRequired["Literal['']|Configuration.ModifyParamsTipping"]
        """
        Tipping configurations for readers supporting on-reader tips
        """
        verifone_p400: NotRequired[
            "Literal['']|Configuration.ModifyParamsVerifoneP400"
        ]
        """
        An object containing device type specific settings for Verifone P400 readers
        """

    class ModifyParamsBbposWiseposE(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class ModifyParamsOffline(TypedDict):
        enabled: bool
        """
        Determines whether to allow transactions to be collected while reader is offline. Defaults to false.
        """

    class ModifyParamsRebootWindow(TypedDict):
        end_hour: int
        """
        Integer between 0 to 23 that represents the end hour of the reboot time window. The value must be different than the start_hour.
        """
        start_hour: int
        """
        Integer between 0 to 23 that represents the start hour of the reboot time window.
        """

    class ModifyParamsStripeS700(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class ModifyParamsTipping(TypedDict):
        aud: NotRequired["Configuration.ModifyParamsTippingAud"]
        """
        Tipping configuration for AUD
        """
        cad: NotRequired["Configuration.ModifyParamsTippingCad"]
        """
        Tipping configuration for CAD
        """
        chf: NotRequired["Configuration.ModifyParamsTippingChf"]
        """
        Tipping configuration for CHF
        """
        czk: NotRequired["Configuration.ModifyParamsTippingCzk"]
        """
        Tipping configuration for CZK
        """
        dkk: NotRequired["Configuration.ModifyParamsTippingDkk"]
        """
        Tipping configuration for DKK
        """
        eur: NotRequired["Configuration.ModifyParamsTippingEur"]
        """
        Tipping configuration for EUR
        """
        gbp: NotRequired["Configuration.ModifyParamsTippingGbp"]
        """
        Tipping configuration for GBP
        """
        hkd: NotRequired["Configuration.ModifyParamsTippingHkd"]
        """
        Tipping configuration for HKD
        """
        myr: NotRequired["Configuration.ModifyParamsTippingMyr"]
        """
        Tipping configuration for MYR
        """
        nok: NotRequired["Configuration.ModifyParamsTippingNok"]
        """
        Tipping configuration for NOK
        """
        nzd: NotRequired["Configuration.ModifyParamsTippingNzd"]
        """
        Tipping configuration for NZD
        """
        sek: NotRequired["Configuration.ModifyParamsTippingSek"]
        """
        Tipping configuration for SEK
        """
        sgd: NotRequired["Configuration.ModifyParamsTippingSgd"]
        """
        Tipping configuration for SGD
        """
        usd: NotRequired["Configuration.ModifyParamsTippingUsd"]
        """
        Tipping configuration for USD
        """

    class ModifyParamsTippingAud(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingCad(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingChf(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingCzk(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingDkk(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingEur(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingGbp(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingHkd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingMyr(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingNok(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingNzd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingSek(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingSgd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsTippingUsd(TypedDict):
        fixed_amounts: NotRequired[List[int]]
        """
        Fixed amounts displayed when collecting a tip
        """
        percentages: NotRequired[List[int]]
        """
        Percentages displayed when collecting a tip
        """
        smart_tip_threshold: NotRequired[int]
        """
        Below this amount, fixed amounts will be displayed; above it, percentages will be displayed
        """

    class ModifyParamsVerifoneP400(TypedDict):
        splashscreen: NotRequired["Literal['']|str"]
        """
        A File ID representing an image you would like displayed on the reader.
        """

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    bbpos_wisepos_e: Optional[BbposWiseposE]
    id: str
    """
    Unique identifier for the object.
    """
    is_account_default: Optional[bool]
    """
    Whether this Configuration is the default for your account
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    name: Optional[str]
    """
    String indicating the name of the Configuration object, set by the user
    """
    object: Literal["terminal.configuration"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    offline: Optional[Offline]
    reboot_window: Optional[RebootWindow]
    stripe_s700: Optional[StripeS700]
    tipping: Optional[Tipping]
    verifone_p400: Optional[VerifoneP400]
    deleted: Optional[Literal[True]]
    """
    Always true for a deleted object
    """

    @classmethod
    def create(
        cls, **params: Unpack["Configuration.CreateParams"]
    ) -> "Configuration":
        """
        Creates a new Configuration object.
        """
        return cast(
            "Configuration",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Configuration.CreateParams"]
    ) -> "Configuration":
        """
        Creates a new Configuration object.
        """
        return cast(
            "Configuration",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def _cls_delete(
        cls, sid: str, **params: Unpack["Configuration.DeleteParams"]
    ) -> "Configuration":
        """
        Deletes a Configuration object.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "Configuration",
            cls._static_request(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    def delete(
        sid: str, **params: Unpack["Configuration.DeleteParams"]
    ) -> "Configuration":
        """
        Deletes a Configuration object.
        """
        ...

    @overload
    def delete(
        self, **params: Unpack["Configuration.DeleteParams"]
    ) -> "Configuration":
        """
        Deletes a Configuration object.
        """
        ...

    @class_method_variant("_cls_delete")
    def delete(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Configuration.DeleteParams"]
    ) -> "Configuration":
        """
        Deletes a Configuration object.
        """
        return self._request_and_refresh(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    async def _cls_delete_async(
        cls, sid: str, **params: Unpack["Configuration.DeleteParams"]
    ) -> "Configuration":
        """
        Deletes a Configuration object.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(sid))
        return cast(
            "Configuration",
            await cls._static_request_async(
                "delete",
                url,
                params=params,
            ),
        )

    @overload
    @staticmethod
    async def delete_async(
        sid: str, **params: Unpack["Configuration.DeleteParams"]
    ) -> "Configuration":
        """
        Deletes a Configuration object.
        """
        ...

    @overload
    async def delete_async(
        self, **params: Unpack["Configuration.DeleteParams"]
    ) -> "Configuration":
        """
        Deletes a Configuration object.
        """
        ...

    @class_method_variant("_cls_delete_async")
    async def delete_async(  # pyright: ignore[reportGeneralTypeIssues]
        self, **params: Unpack["Configuration.DeleteParams"]
    ) -> "Configuration":
        """
        Deletes a Configuration object.
        """
        return await self._request_and_refresh_async(
            "delete",
            self.instance_url(),
            params=params,
        )

    @classmethod
    def list(
        cls, **params: Unpack["Configuration.ListParams"]
    ) -> ListObject["Configuration"]:
        """
        Returns a list of Configuration objects.
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
        cls, **params: Unpack["Configuration.ListParams"]
    ) -> ListObject["Configuration"]:
        """
        Returns a list of Configuration objects.
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
        cls, id: str, **params: Unpack["Configuration.ModifyParams"]
    ) -> "Configuration":
        """
        Updates a new Configuration object.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Configuration",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Configuration.ModifyParams"]
    ) -> "Configuration":
        """
        Updates a new Configuration object.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Configuration",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Configuration.RetrieveParams"]
    ) -> "Configuration":
        """
        Retrieves a Configuration object.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Configuration.RetrieveParams"]
    ) -> "Configuration":
        """
        Retrieves a Configuration object.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {
        "bbpos_wisepos_e": BbposWiseposE,
        "offline": Offline,
        "reboot_window": RebootWindow,
        "stripe_s700": StripeS700,
        "tipping": Tipping,
        "verifone_p400": VerifoneP400,
    }
