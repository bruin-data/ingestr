# -*- coding: utf-8 -*-
# File generated from our OpenAPI spec
from stripe._createable_api_resource import CreateableAPIResource
from stripe._list_object import ListObject
from stripe._listable_api_resource import ListableAPIResource
from stripe._request_options import RequestOptions
from stripe._stripe_object import StripeObject
from stripe._updateable_api_resource import UpdateableAPIResource
from stripe._util import sanitize_id
from typing import ClassVar, List, Optional, Union, cast
from typing_extensions import Literal, NotRequired, TypedDict, Unpack


class Registration(
    CreateableAPIResource["Registration"],
    ListableAPIResource["Registration"],
    UpdateableAPIResource["Registration"],
):
    """
    A Tax `Registration` lets us know that your business is registered to collect tax on payments within a region, enabling you to [automatically collect tax](https://stripe.com/docs/tax).

    Stripe doesn't register on your behalf with the relevant authorities when you create a Tax `Registration` object. For more information on how to register to collect tax, see [our guide](https://stripe.com/docs/tax/registering).

    Related guide: [Using the Registrations API](https://stripe.com/docs/tax/registrations-api)
    """

    OBJECT_NAME: ClassVar[Literal["tax.registration"]] = "tax.registration"

    class CountryOptions(StripeObject):
        class Ae(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        class At(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Au(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        class Be(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Bg(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Bh(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        class Ca(StripeObject):
            class ProvinceStandard(StripeObject):
                province: str
                """
                Two-letter CA province code ([ISO 3166-2](https://en.wikipedia.org/wiki/ISO_3166-2)).
                """

            province_standard: Optional[ProvinceStandard]
            type: Literal["province_standard", "simplified", "standard"]
            """
            Type of registration in Canada.
            """
            _inner_class_types = {"province_standard": ProvinceStandard}

        class Ch(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        class Cl(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Co(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Cy(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Cz(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class De(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Dk(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Ee(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Eg(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Es(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Fi(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Fr(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Gb(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        class Ge(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Gr(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Hr(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Hu(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Id(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Ie(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Is(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        class It(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Jp(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        class Ke(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Kr(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Kz(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Lt(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Lu(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Lv(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Mt(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Mx(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class My(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Ng(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Nl(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class No(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        class Nz(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        class Om(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        class Pl(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Pt(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Ro(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Sa(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Se(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Sg(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        class Si(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Sk(StripeObject):
            class Standard(StripeObject):
                place_of_supply_scheme: Literal["small_seller", "standard"]
                """
                Place of supply scheme used in an EU standard registration.
                """

            standard: Optional[Standard]
            type: Literal["ioss", "oss_non_union", "oss_union", "standard"]
            """
            Type of registration in an EU country.
            """
            _inner_class_types = {"standard": Standard}

        class Th(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Tr(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Us(StripeObject):
            class LocalAmusementTax(StripeObject):
                jurisdiction: str
                """
                A [FIPS code](https://www.census.gov/library/reference/code-lists/ansi.html) representing the local jurisdiction.
                """

            class LocalLeaseTax(StripeObject):
                jurisdiction: str
                """
                A [FIPS code](https://www.census.gov/library/reference/code-lists/ansi.html) representing the local jurisdiction.
                """

            local_amusement_tax: Optional[LocalAmusementTax]
            local_lease_tax: Optional[LocalLeaseTax]
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
            Type of registration in the US.
            """
            _inner_class_types = {
                "local_amusement_tax": LocalAmusementTax,
                "local_lease_tax": LocalLeaseTax,
            }

        class Vn(StripeObject):
            type: Literal["simplified"]
            """
            Type of registration in `country`.
            """

        class Za(StripeObject):
            type: Literal["standard"]
            """
            Type of registration in `country`.
            """

        ae: Optional[Ae]
        at: Optional[At]
        au: Optional[Au]
        be: Optional[Be]
        bg: Optional[Bg]
        bh: Optional[Bh]
        ca: Optional[Ca]
        ch: Optional[Ch]
        cl: Optional[Cl]
        co: Optional[Co]
        cy: Optional[Cy]
        cz: Optional[Cz]
        de: Optional[De]
        dk: Optional[Dk]
        ee: Optional[Ee]
        eg: Optional[Eg]
        es: Optional[Es]
        fi: Optional[Fi]
        fr: Optional[Fr]
        gb: Optional[Gb]
        ge: Optional[Ge]
        gr: Optional[Gr]
        hr: Optional[Hr]
        hu: Optional[Hu]
        id: Optional[Id]
        ie: Optional[Ie]
        is_: Optional[Is]
        it: Optional[It]
        jp: Optional[Jp]
        ke: Optional[Ke]
        kr: Optional[Kr]
        kz: Optional[Kz]
        lt: Optional[Lt]
        lu: Optional[Lu]
        lv: Optional[Lv]
        mt: Optional[Mt]
        mx: Optional[Mx]
        my: Optional[My]
        ng: Optional[Ng]
        nl: Optional[Nl]
        no: Optional[No]
        nz: Optional[Nz]
        om: Optional[Om]
        pl: Optional[Pl]
        pt: Optional[Pt]
        ro: Optional[Ro]
        sa: Optional[Sa]
        se: Optional[Se]
        sg: Optional[Sg]
        si: Optional[Si]
        sk: Optional[Sk]
        th: Optional[Th]
        tr: Optional[Tr]
        us: Optional[Us]
        vn: Optional[Vn]
        za: Optional[Za]
        _inner_class_types = {
            "ae": Ae,
            "at": At,
            "au": Au,
            "be": Be,
            "bg": Bg,
            "bh": Bh,
            "ca": Ca,
            "ch": Ch,
            "cl": Cl,
            "co": Co,
            "cy": Cy,
            "cz": Cz,
            "de": De,
            "dk": Dk,
            "ee": Ee,
            "eg": Eg,
            "es": Es,
            "fi": Fi,
            "fr": Fr,
            "gb": Gb,
            "ge": Ge,
            "gr": Gr,
            "hr": Hr,
            "hu": Hu,
            "id": Id,
            "ie": Ie,
            "is": Is,
            "it": It,
            "jp": Jp,
            "ke": Ke,
            "kr": Kr,
            "kz": Kz,
            "lt": Lt,
            "lu": Lu,
            "lv": Lv,
            "mt": Mt,
            "mx": Mx,
            "my": My,
            "ng": Ng,
            "nl": Nl,
            "no": No,
            "nz": Nz,
            "om": Om,
            "pl": Pl,
            "pt": Pt,
            "ro": Ro,
            "sa": Sa,
            "se": Se,
            "sg": Sg,
            "si": Si,
            "sk": Sk,
            "th": Th,
            "tr": Tr,
            "us": Us,
            "vn": Vn,
            "za": Za,
        }
        _field_remappings = {"is_": "is"}

    class CreateParams(RequestOptions):
        active_from: Union[Literal["now"], int]
        """
        Time at which the Tax Registration becomes active. It can be either `now` to indicate the current time, or a future timestamp measured in seconds since the Unix epoch.
        """
        country: str
        """
        Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
        """
        country_options: "Registration.CreateParamsCountryOptions"
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
        {"is": NotRequired["Registration.CreateParamsCountryOptionsIs"]},
    )

    class CreateParamsCountryOptions(_CreateParamsCountryOptionsBase):
        ae: NotRequired["Registration.CreateParamsCountryOptionsAe"]
        """
        Options for the registration in AE.
        """
        at: NotRequired["Registration.CreateParamsCountryOptionsAt"]
        """
        Options for the registration in AT.
        """
        au: NotRequired["Registration.CreateParamsCountryOptionsAu"]
        """
        Options for the registration in AU.
        """
        be: NotRequired["Registration.CreateParamsCountryOptionsBe"]
        """
        Options for the registration in BE.
        """
        bg: NotRequired["Registration.CreateParamsCountryOptionsBg"]
        """
        Options for the registration in BG.
        """
        bh: NotRequired["Registration.CreateParamsCountryOptionsBh"]
        """
        Options for the registration in BH.
        """
        ca: NotRequired["Registration.CreateParamsCountryOptionsCa"]
        """
        Options for the registration in CA.
        """
        ch: NotRequired["Registration.CreateParamsCountryOptionsCh"]
        """
        Options for the registration in CH.
        """
        cl: NotRequired["Registration.CreateParamsCountryOptionsCl"]
        """
        Options for the registration in CL.
        """
        co: NotRequired["Registration.CreateParamsCountryOptionsCo"]
        """
        Options for the registration in CO.
        """
        cy: NotRequired["Registration.CreateParamsCountryOptionsCy"]
        """
        Options for the registration in CY.
        """
        cz: NotRequired["Registration.CreateParamsCountryOptionsCz"]
        """
        Options for the registration in CZ.
        """
        de: NotRequired["Registration.CreateParamsCountryOptionsDe"]
        """
        Options for the registration in DE.
        """
        dk: NotRequired["Registration.CreateParamsCountryOptionsDk"]
        """
        Options for the registration in DK.
        """
        ee: NotRequired["Registration.CreateParamsCountryOptionsEe"]
        """
        Options for the registration in EE.
        """
        eg: NotRequired["Registration.CreateParamsCountryOptionsEg"]
        """
        Options for the registration in EG.
        """
        es: NotRequired["Registration.CreateParamsCountryOptionsEs"]
        """
        Options for the registration in ES.
        """
        fi: NotRequired["Registration.CreateParamsCountryOptionsFi"]
        """
        Options for the registration in FI.
        """
        fr: NotRequired["Registration.CreateParamsCountryOptionsFr"]
        """
        Options for the registration in FR.
        """
        gb: NotRequired["Registration.CreateParamsCountryOptionsGb"]
        """
        Options for the registration in GB.
        """
        ge: NotRequired["Registration.CreateParamsCountryOptionsGe"]
        """
        Options for the registration in GE.
        """
        gr: NotRequired["Registration.CreateParamsCountryOptionsGr"]
        """
        Options for the registration in GR.
        """
        hr: NotRequired["Registration.CreateParamsCountryOptionsHr"]
        """
        Options for the registration in HR.
        """
        hu: NotRequired["Registration.CreateParamsCountryOptionsHu"]
        """
        Options for the registration in HU.
        """
        id: NotRequired["Registration.CreateParamsCountryOptionsId"]
        """
        Options for the registration in ID.
        """
        ie: NotRequired["Registration.CreateParamsCountryOptionsIe"]
        """
        Options for the registration in IE.
        """
        it: NotRequired["Registration.CreateParamsCountryOptionsIt"]
        """
        Options for the registration in IT.
        """
        jp: NotRequired["Registration.CreateParamsCountryOptionsJp"]
        """
        Options for the registration in JP.
        """
        ke: NotRequired["Registration.CreateParamsCountryOptionsKe"]
        """
        Options for the registration in KE.
        """
        kr: NotRequired["Registration.CreateParamsCountryOptionsKr"]
        """
        Options for the registration in KR.
        """
        kz: NotRequired["Registration.CreateParamsCountryOptionsKz"]
        """
        Options for the registration in KZ.
        """
        lt: NotRequired["Registration.CreateParamsCountryOptionsLt"]
        """
        Options for the registration in LT.
        """
        lu: NotRequired["Registration.CreateParamsCountryOptionsLu"]
        """
        Options for the registration in LU.
        """
        lv: NotRequired["Registration.CreateParamsCountryOptionsLv"]
        """
        Options for the registration in LV.
        """
        mt: NotRequired["Registration.CreateParamsCountryOptionsMt"]
        """
        Options for the registration in MT.
        """
        mx: NotRequired["Registration.CreateParamsCountryOptionsMx"]
        """
        Options for the registration in MX.
        """
        my: NotRequired["Registration.CreateParamsCountryOptionsMy"]
        """
        Options for the registration in MY.
        """
        ng: NotRequired["Registration.CreateParamsCountryOptionsNg"]
        """
        Options for the registration in NG.
        """
        nl: NotRequired["Registration.CreateParamsCountryOptionsNl"]
        """
        Options for the registration in NL.
        """
        no: NotRequired["Registration.CreateParamsCountryOptionsNo"]
        """
        Options for the registration in NO.
        """
        nz: NotRequired["Registration.CreateParamsCountryOptionsNz"]
        """
        Options for the registration in NZ.
        """
        om: NotRequired["Registration.CreateParamsCountryOptionsOm"]
        """
        Options for the registration in OM.
        """
        pl: NotRequired["Registration.CreateParamsCountryOptionsPl"]
        """
        Options for the registration in PL.
        """
        pt: NotRequired["Registration.CreateParamsCountryOptionsPt"]
        """
        Options for the registration in PT.
        """
        ro: NotRequired["Registration.CreateParamsCountryOptionsRo"]
        """
        Options for the registration in RO.
        """
        sa: NotRequired["Registration.CreateParamsCountryOptionsSa"]
        """
        Options for the registration in SA.
        """
        se: NotRequired["Registration.CreateParamsCountryOptionsSe"]
        """
        Options for the registration in SE.
        """
        sg: NotRequired["Registration.CreateParamsCountryOptionsSg"]
        """
        Options for the registration in SG.
        """
        si: NotRequired["Registration.CreateParamsCountryOptionsSi"]
        """
        Options for the registration in SI.
        """
        sk: NotRequired["Registration.CreateParamsCountryOptionsSk"]
        """
        Options for the registration in SK.
        """
        th: NotRequired["Registration.CreateParamsCountryOptionsTh"]
        """
        Options for the registration in TH.
        """
        tr: NotRequired["Registration.CreateParamsCountryOptionsTr"]
        """
        Options for the registration in TR.
        """
        us: NotRequired["Registration.CreateParamsCountryOptionsUs"]
        """
        Options for the registration in US.
        """
        vn: NotRequired["Registration.CreateParamsCountryOptionsVn"]
        """
        Options for the registration in VN.
        """
        za: NotRequired["Registration.CreateParamsCountryOptionsZa"]
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
            "Registration.CreateParamsCountryOptionsAtStandard"
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
            "Registration.CreateParamsCountryOptionsBeStandard"
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
            "Registration.CreateParamsCountryOptionsBgStandard"
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
            "Registration.CreateParamsCountryOptionsCaProvinceStandard"
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
            "Registration.CreateParamsCountryOptionsCyStandard"
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
            "Registration.CreateParamsCountryOptionsCzStandard"
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
            "Registration.CreateParamsCountryOptionsDeStandard"
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
            "Registration.CreateParamsCountryOptionsDkStandard"
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
            "Registration.CreateParamsCountryOptionsEeStandard"
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
            "Registration.CreateParamsCountryOptionsEsStandard"
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
            "Registration.CreateParamsCountryOptionsFiStandard"
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
            "Registration.CreateParamsCountryOptionsFrStandard"
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
            "Registration.CreateParamsCountryOptionsGrStandard"
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
            "Registration.CreateParamsCountryOptionsHrStandard"
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
            "Registration.CreateParamsCountryOptionsHuStandard"
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
            "Registration.CreateParamsCountryOptionsIeStandard"
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
            "Registration.CreateParamsCountryOptionsItStandard"
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
            "Registration.CreateParamsCountryOptionsLtStandard"
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
            "Registration.CreateParamsCountryOptionsLuStandard"
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
            "Registration.CreateParamsCountryOptionsLvStandard"
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
            "Registration.CreateParamsCountryOptionsMtStandard"
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
            "Registration.CreateParamsCountryOptionsNlStandard"
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
            "Registration.CreateParamsCountryOptionsPlStandard"
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
            "Registration.CreateParamsCountryOptionsPtStandard"
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
            "Registration.CreateParamsCountryOptionsRoStandard"
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
            "Registration.CreateParamsCountryOptionsSeStandard"
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
            "Registration.CreateParamsCountryOptionsSiStandard"
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
            "Registration.CreateParamsCountryOptionsSkStandard"
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
            "Registration.CreateParamsCountryOptionsUsLocalAmusementTax"
        ]
        """
        Options for the local amusement tax registration.
        """
        local_lease_tax: NotRequired[
            "Registration.CreateParamsCountryOptionsUsLocalLeaseTax"
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
        starting_after: NotRequired[str]
        """
        A cursor for use in pagination. `starting_after` is an object ID that defines your place in the list. For instance, if you make a list request and receive 100 objects, ending with `obj_foo`, your subsequent call can include `starting_after=obj_foo` in order to fetch the next page of the list.
        """
        status: NotRequired[Literal["active", "all", "expired", "scheduled"]]
        """
        The status of the Tax Registration.
        """

    class ModifyParams(RequestOptions):
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

    class RetrieveParams(RequestOptions):
        expand: NotRequired[List[str]]
        """
        Specifies which fields in the response should be expanded.
        """

    active_from: int
    """
    Time at which the registration becomes active. Measured in seconds since the Unix epoch.
    """
    country: str
    """
    Two-letter country code ([ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)).
    """
    country_options: CountryOptions
    created: int
    """
    Time at which the object was created. Measured in seconds since the Unix epoch.
    """
    expires_at: Optional[int]
    """
    If set, the registration stops being active at this time. If not set, the registration will be active indefinitely. Measured in seconds since the Unix epoch.
    """
    id: str
    """
    Unique identifier for the object.
    """
    livemode: bool
    """
    Has the value `true` if the object exists in live mode or the value `false` if the object exists in test mode.
    """
    object: Literal["tax.registration"]
    """
    String representing the object's type. Objects of the same type share the same value.
    """
    status: Literal["active", "expired", "scheduled"]
    """
    The status of the registration. This field is present for convenience and can be deduced from `active_from` and `expires_at`.
    """

    @classmethod
    def create(
        cls, **params: Unpack["Registration.CreateParams"]
    ) -> "Registration":
        """
        Creates a new Tax Registration object.
        """
        return cast(
            "Registration",
            cls._static_request(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    async def create_async(
        cls, **params: Unpack["Registration.CreateParams"]
    ) -> "Registration":
        """
        Creates a new Tax Registration object.
        """
        return cast(
            "Registration",
            await cls._static_request_async(
                "post",
                cls.class_url(),
                params=params,
            ),
        )

    @classmethod
    def list(
        cls, **params: Unpack["Registration.ListParams"]
    ) -> ListObject["Registration"]:
        """
        Returns a list of Tax Registration objects.
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
        cls, **params: Unpack["Registration.ListParams"]
    ) -> ListObject["Registration"]:
        """
        Returns a list of Tax Registration objects.
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
        cls, id: str, **params: Unpack["Registration.ModifyParams"]
    ) -> "Registration":
        """
        Updates an existing Tax Registration object.

        A registration cannot be deleted after it has been created. If you wish to end a registration you may do so by setting expires_at.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Registration",
            cls._static_request(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    async def modify_async(
        cls, id: str, **params: Unpack["Registration.ModifyParams"]
    ) -> "Registration":
        """
        Updates an existing Tax Registration object.

        A registration cannot be deleted after it has been created. If you wish to end a registration you may do so by setting expires_at.
        """
        url = "%s/%s" % (cls.class_url(), sanitize_id(id))
        return cast(
            "Registration",
            await cls._static_request_async(
                "post",
                url,
                params=params,
            ),
        )

    @classmethod
    def retrieve(
        cls, id: str, **params: Unpack["Registration.RetrieveParams"]
    ) -> "Registration":
        """
        Returns a Tax Registration object.
        """
        instance = cls(id, **params)
        instance.refresh()
        return instance

    @classmethod
    async def retrieve_async(
        cls, id: str, **params: Unpack["Registration.RetrieveParams"]
    ) -> "Registration":
        """
        Returns a Tax Registration object.
        """
        instance = cls(id, **params)
        await instance.refresh_async()
        return instance

    _inner_class_types = {"country_options": CountryOptions}
