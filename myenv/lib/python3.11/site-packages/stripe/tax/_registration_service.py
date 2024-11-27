# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._list_object import ListObject
from stripe._request_options import RequestOptions
from stripe._stripe_service import StripeService
from stripe._util import sanitize_id
from stripe.tax._registration import Registration
from typing import List, Union, cast
from typing_extensions import Literal, NotRequired, TypedDict


class RegistrationService(StripeService):
    class CreateParams(TypedDict):
        active_from: Union[Literal["now"], int]
        """
        Time at which the Tax Registration becomes active. It can be either `now` to indicate the current time, or a future timestamp measured in seconds since the Unix epoch.
        """
        country: str
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        country_options: "RegistrationService.CreateParamsCountryOptions"
        """
        Specific options for a registration in the specified `country`.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expires_at: NotRequired[int]
        """
        If set, the Tax Registration stops being active at this time. If not set, the Tax Registration will be active indefinitely. Timestamp measured in seconds since the Unix epoch.
        """

    _CreateParamsCountryOptionsBase = TypedDict(
        "CreateParamsCountryOptions",
        {
            "is": NotRequired[
                "RegistrationService.CreateParamsCountryOptionsIs"
            ]
        },
    )

    class CreateParamsCountryOptions(_CreateParamsCountryOptionsBase):
        ae: NotRequired["RegistrationService.CreateParamsCountryOptionsAe"]
        """
        Options for the registration in AE.
        """
        at: NotRequired["RegistrationService.CreateParamsCountryOptionsAt"]
        """
        Options for the registration in AT.
        """
        au: NotRequired["RegistrationService.CreateParamsCountryOptionsAu"]
        """
        Options for the registration in AU.
        """
        be: NotRequired["RegistrationService.CreateParamsCountryOptionsBe"]
        """
        Options for the registration in BE.
        """
        bg: NotRequired["RegistrationService.CreateParamsCountryOptionsBg"]
        """
        Options for the registration in BG.
        """
        bh: NotRequired["RegistrationService.CreateParamsCountryOptionsBh"]
        """
        Options for the registration in BH.
        """
        ca: NotRequired["RegistrationService.CreateParamsCountryOptionsCa"]
        """
        Options for the registration in CA.
        """
        ch: NotRequired["RegistrationService.CreateParamsCountryOptionsCh"]
        """
        Options for the registration in CH.
        """
        cl: NotRequired["RegistrationService.CreateParamsCountryOptionsCl"]
        """
        Options for the registration in CL.
        """
        co: NotRequired["RegistrationService.CreateParamsCountryOptionsCo"]
        """
        Options for the registration in CO.
        """
        cy: NotRequired["RegistrationService.CreateParamsCountryOptionsCy"]
        """
        Options for the registration in CY.
        """
        cz: NotRequired["RegistrationService.CreateParamsCountryOptionsCz"]
        """
        Options for the registration in CZ.
        """
        de: NotRequired["RegistrationService.CreateParamsCountryOptionsDe"]
        """
        Options for the registration in DE.
        """
        dk: NotRequired["RegistrationService.CreateParamsCountryOptionsDk"]
        """
        Options for the registration in DK.
        """
        ee: NotRequired["RegistrationService.CreateParamsCountryOptionsEe"]
        """
        Options for the registration in EE.
        """
        eg: NotRequired["RegistrationService.CreateParamsCountryOptionsEg"]
        """
        Options for the registration in EG.
        """
        es: NotRequired["RegistrationService.CreateParamsCountryOptionsEs"]
        """
        Options for the registration in ES.
        """
        fi: NotRequired["RegistrationService.CreateParamsCountryOptionsFi"]
        """
        Options for the registration in FI.
        """
        fr: NotRequired["RegistrationService.CreateParamsCountryOptionsFr"]
        """
        Options for the registration in FR.
        """
        gb: NotRequired["RegistrationService.CreateParamsCountryOptionsGb"]
        """
        Options for the registration in GB.
        """
        ge: NotRequired["RegistrationService.CreateParamsCountryOptionsGe"]
        """
        Options for the registration in GE.
        """
        gr: NotRequired["RegistrationService.CreateParamsCountryOptionsGr"]
        """
        Options for the registration in GR.
        """
        hr: NotRequired["RegistrationService.CreateParamsCountryOptionsHr"]
        """
        Options for the registration in HR.
        """
        hu: NotRequired["RegistrationService.CreateParamsCountryOptionsHu"]
        """
        Options for the registration in HU.
        """
        id: NotRequired["RegistrationService.CreateParamsCountryOptionsId"]
        """
        Options for the registration in ID.
        """
        ie: NotRequired["RegistrationService.CreateParamsCountryOptionsIe"]
        """
        Options for the registration in IE.
        """
        it: NotRequired["RegistrationService.CreateParamsCountryOptionsIt"]
        """
        Options for the registration in IT.
        """
        jp: NotRequired["RegistrationService.CreateParamsCountryOptionsJp"]
        """
        Options for the registration in JP.
        """
        ke: NotRequired["RegistrationService.CreateParamsCountryOptionsKe"]
        """
        Options for the registration in KE.
        """
        kr: NotRequired["RegistrationService.CreateParamsCountryOptionsKr"]
        """
        Options for the registration in KR.
        """
        kz: NotRequired["RegistrationService.CreateParamsCountryOptionsKz"]
        """
        Options for the registration in KZ.
        """
        lt: NotRequired["RegistrationService.CreateParamsCountryOptionsLt"]
        """
        Options for the registration in LT.
        """
        lu: NotRequired["RegistrationService.CreateParamsCountryOptionsLu"]
        """
        Options for the registration in LU.
        """
        lv: NotRequired["RegistrationService.CreateParamsCountryOptionsLv"]
        """
        Options for the registration in LV.
        """
        mt: NotRequired["RegistrationService.CreateParamsCountryOptionsMt"]
        """
        Options for the registration in MT.
        """
        mx: NotRequired["RegistrationService.CreateParamsCountryOptionsMx"]
        """
        Options for the registration in MX.
        """
        my: NotRequired["RegistrationService.CreateParamsCountryOptionsMy"]
        """
        Options for the registration in MY.
        """
        ng: NotRequired["RegistrationService.CreateParamsCountryOptionsNg"]
        """
        Options for the registration in NG.
        """
        nl: NotRequired["RegistrationService.CreateParamsCountryOptionsNl"]
        """
        Options for the registration in NL.
        """
        no: NotRequired["RegistrationService.CreateParamsCountryOptionsNo"]
        """
        Options for the registration in NO.
        """
        nz: NotRequired["RegistrationService.CreateParamsCountryOptionsNz"]
        """
        Options for the registration in NZ.
        """
        om: NotRequired["RegistrationService.CreateParamsCountryOptionsOm"]
        """
        Options for the registration in OM.
        """
        pl: NotRequired["RegistrationService.CreateParamsCountryOptionsPl"]
        """
        Options for the registration in PL.
        """
        pt: NotRequired["RegistrationService.CreateParamsCountryOptionsPt"]
        """
        Options for the registration in PT.
        """
        ro: NotRequired["RegistrationService.CreateParamsCountryOptionsRo"]
        """
        Options for the registration in RO.
        """
        sa: NotRequired["RegistrationService.CreateParamsCountryOptionsSa"]
        """
        Options for the registration in SA.
        """
        se: NotRequired["RegistrationService.CreateParamsCountryOptionsSe"]
        """
        Options for the registration in SE.
        """
        sg: NotRequired["RegistrationService.CreateParamsCountryOptionsSg"]
        """
        Options for the registration in SG.
        """
        si: NotRequired["RegistrationService.CreateParamsCountryOptionsSi"]
        """
        Options for the registration in SI.
        """
        sk: NotRequired["RegistrationService.CreateParamsCountryOptionsSk"]
        """
        Options for the registration in SK.
        """
        th: NotRequired["RegistrationService.CreateParamsCountryOptionsTh"]
        """
        Options for the registration in TH.
        """
        tr: NotRequired["RegistrationService.CreateParamsCountryOptionsTr"]
        """
        Options for the registration in TR.
        """
        us: NotRequired["RegistrationService.CreateParamsCountryOptionsUs"]
        """
        Options for the registration in US.
        """
        vn: NotRequired["RegistrationService.CreateParamsCountryOptionsVn"]
        """
        Options for the registration in VN.
        """
        za: NotRequired["RegistrationService.CreateParamsCountryOptionsZa"]
        """
        Options for the registration in ZA.
        """

    class CreateParamsCountryOptionsAe(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsAt(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsAtStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsAtStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsAu(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsBe(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsBeStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsBeStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsBg(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsBgStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsBgStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsBh(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsCa(TypedDict):
        province_standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsCaProvinceStandard"
        ]
        """
        Options for the provincial tax registration.
        """
        type: Literal["province_standard", "simplified", "standard"]
        """
        Type of registration to be created in Canada.
        """

    class CreateParamsCountryOptionsCaProvinceStandard(TypedDict):
        province: str
        """
        Two-letter CA province code ([ISO 3166-2](https://en.wikipedia.org/wiki/ISO_3166-2)).
        """

    class CreateParamsCountryOptionsCh(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsCl(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsCo(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsCy(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsCyStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsCyStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsCz(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsCzStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsCzStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsDe(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsDeStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsDeStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsDk(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsDkStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsDkStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsEe(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsEeStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsEeStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsEg(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsEs(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsEsStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsEsStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsFi(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsFiStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsFiStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsFr(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsFrStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsFrStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsGb(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsGe(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsGr(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsGrStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsGrStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsHr(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsHrStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsHrStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsHu(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsHuStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsHuStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsId(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsIe(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsIeStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsIeStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsIs(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsIt(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsItStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsItStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsJp(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsKe(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsKr(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsKz(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsLt(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsLtStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsLtStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsLu(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsLuStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsLuStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsLv(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsLvStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsLvStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsMt(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsMtStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsMtStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsMx(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsMy(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsNg(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsNl(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsNlStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsNlStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsNo(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsNz(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsOm(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsPl(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsPlStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsPlStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsPt(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsPtStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsPtStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsRo(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsRoStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsRoStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsSa(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsSe(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsSeStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsSeStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsSg(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsSi(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsSiStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsSiStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsSk(TypedDict):
        standard: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsSkStandard"
        ]
        """
        Options for the standard registration.
        """
        type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
        """
        Type of registration to be created in an EU country.
        """

    class CreateParamsCountryOptionsSkStandard(TypedDict):
        place_of_supply_scheme: Literal["small_seller", "standard"]
        """
        Place of supply scheme used in an EU standard registration.
        """

    class CreateParamsCountryOptionsTh(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsTr(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsUs(TypedDict):
        local_amusement_tax: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsUsLocalAmusementTax"
        ]
        """
        Options for the local amusement tax registration.
        """
        local_lease_tax: NotRequired[
            "RegistrationService.CreateParamsCountryOptionsUsLocalLeaseTax"
        ]
        """
        Options for the local lease tax registration.
        """
        state: str
        """
        Two-letter US state code ([ISO 3166-2](https://en.wikipedia.org/wiki/ISO_3166-2)).
        """
        type: Literal[
            "local_amusement_tax",
            "local_lease_tax",
            "state_communications_tax",
            "state_sales_tax",
        ]
        """
        Type of registration to be created in the US.
        """

    class CreateParamsCountryOptionsUsLocalAmusementTax(TypedDict):
        jurisdiction: str
        """
        A [FIPS code](https://www.census.gov/library/reference/code-lists/ansi.html) representing the local jurisdiction. Supported FIPS codes are: `14000` (Chicago), `06613` (Bloomington), `21696` (East Dundee), `24582` (Evanston), and `68081` (Schiller Park).
        """

    class CreateParamsCountryOptionsUsLocalLeaseTax(TypedDict):
        jurisdiction: str
        """
        A [FIPS code](https://www.census.gov/library/reference/code-lists/ansi.html) representing the local jurisdiction. Supported FIPS codes are: `14000` (Chicago).
        """

    class CreateParamsCountryOptionsVn(TypedDict):
        type: Literal["simplified"]
        """
        Type of registration to be created in `country`.
        """

    class CreateParamsCountryOptionsZa(TypedDict):
        type: Literal["standard"]
        """
        Type of registration to be created in `country`.
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
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[Literal["active", "all", "expired", "scheduled"]]
        """
        The status of the Tax Registration.
        """

    class RetrieveParams(TypedDict):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    class UpdateParams(TypedDict):
        active_from: NotRequired["Literal['now']|int"]
        """
        Time at which the registration becomes active. It can be either `now` to indicate the current time, or a timestamp measured in seconds since the Unix epoch.
        """
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """
        expires_at: NotRequired["Literal['']|Literal['now']|int"]
        """
        If set, the registration stops being active at this time. If not set, the registration will be active indefinitely. It can be either `now` to indicate the current time, or a timestamp measured in seconds since the Unix epoch.
        """

    def list(
        self,
        params: "RegistrationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Registration]:
        """
        Returns a list of Tax Registration objects.
        """
        return cast(
            ListObject[Registration],
            self._request(
                "get",
                "/v1/tax/registrations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def list_async(
        self,
        params: "RegistrationService.ListParams" = {},
        options: RequestOptions = {},
    ) -> ListObject[Registration]:
        """
        Returns a list of Tax Registration objects.
        """
        return cast(
            ListObject[Registration],
            await self._request_async(
                "get",
                "/v1/tax/registrations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def create(
        self,
        params: "RegistrationService.CreateParams",
        options: RequestOptions = {},
    ) -> Registration:
        """
        Creates a new Tax Registration object.
        """
        return cast(
            Registration,
            self._request(
                "post",
                "/v1/tax/registrations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def create_async(
        self,
        params: "RegistrationService.CreateParams",
        options: RequestOptions = {},
    ) -> Registration:
        """
        Creates a new Tax Registration object.
        """
        return cast(
            Registration,
            await self._request_async(
                "post",
                "/v1/tax/registrations",
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def retrieve(
        self,
        id: str,
        params: "RegistrationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Registration:
        """
        Returns a Tax Registration object.
        """
        return cast(
            Registration,
            self._request(
                "get",
                "/v1/tax/registrations/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def retrieve_async(
        self,
        id: str,
        params: "RegistrationService.RetrieveParams" = {},
        options: RequestOptions = {},
    ) -> Registration:
        """
        Returns a Tax Registration object.
        """
        return cast(
            Registration,
            await self._request_async(
                "get",
                "/v1/tax/registrations/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    def update(
        self,
        id: str,
        params: "RegistrationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Registration:
        """
        Updates an existing Tax Registration object.

        A registration cannot be deleted after it has been created. If you wish to end a registration you may do so by setting expires_at.
        """
        return cast(
            Registration,
            self._request(
                "post",
                "/v1/tax/registrations/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )

    async def update_async(
        self,
        id: str,
        params: "RegistrationService.UpdateParams" = {},
        options: RequestOptions = {},
    ) -> Registration:
        """
        Updates an existing Tax Registration object.

        A registration cannot be deleted after it has been created. If you wish to end a registration you may do so by setting expires_at.
        """
        return cast(
            Registration,
            await self._request_async(
                "post",
                "/v1/tax/registrations/{id}".format(id=sanitize_id(id)),
                api_mode="V1",
                base_address="api",
                params=params,
                options=options,
            ),
        )
